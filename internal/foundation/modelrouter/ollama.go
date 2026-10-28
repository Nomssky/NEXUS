package modelrouter

// Ollama provider: the initial supported LOCAL inference runtime.
//
// Discovery and metadata use Ollama's native API (/api/version, /api/tags,
// /api/show). Inference uses Ollama's native /api/chat endpoint, which
// supports the top-level "think" parameter required to disable thinking on
// thinking-capable models (qwen35 family). The OpenAI-compatible
// /v1/chat/completions endpoint does not support "think" and its
// "options.thinking" is ineffective for these models.
//
// Boundaries:
//   - No API key is required for a local Ollama; an authenticated proxy can add
//     static headers.
//   - The provider never pulls/downloads a model. Installing a model is an
//     explicit owner action performed outside NEXUS.
//   - "Installed" (present locally) is distinguished from "available to
//     download". Ollama's local API only enumerates installed models; a model
//     that is merely downloadable is never invented as a candidate.
//   - Timeout, cancellation, bounded response size and error redaction follow
//     the same guarantees as the shipped OpenAI provider.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultOllamaBaseURL is the documented local default.
const DefaultOllamaBaseURL = "http://127.0.0.1:11434"

// OllamaConfig configures the local Ollama provider.
type OllamaConfig struct {
	// ID is the provider identity; empty defaults to "ollama".
	ID string
	// BaseURL is the Ollama root (no trailing slash), e.g.
	// "http://127.0.0.1:11434". Empty uses DefaultOllamaBaseURL.
	BaseURL string
	// Timeout bounds a single inference/discovery call (default 120s).
	Timeout time.Duration
	// MaxResponseBytes bounds any response body (default 1 MiB).
	MaxResponseBytes int64
	// Headers are optional static request headers (e.g. for an authenticated
	// reverse proxy). They must not be used to smuggle credentials into logs.
	Headers map[string]string
	// AllowInsecureHTTP permits a plain-HTTP non-loopback base URL. It is only
	// required for unusual setups; a loopback http URL is always allowed.
	AllowInsecureHTTP bool
	// DisableThinking disables the model's internal reasoning/thinking channel
	// when the local Ollama model supports it (Ollama >= 0.40, thinking-capable
	// models such as qwen35). Thinking models can exhaust their token budget on
	// reasoning and emit no visible content; disabling thinking yields a final
	// answer faster. Default false preserves existing behavior.
	DisableThinking bool
	// Client overrides the HTTP client (tests).
	Client *http.Client
}

// OllamaProvider implements Provider.
type OllamaProvider struct {
	config   OllamaConfig
	base     *url.URL
	client   *http.Client
	status   ProviderStatus
	timeout  time.Duration
	maxBytes int64
}

// NewOllamaProvider validates configuration and returns a provider.
func NewOllamaProvider(cfg OllamaConfig) (*OllamaProvider, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		cfg.ID = "ollama"
	}
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = DefaultOllamaBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("ollama provider %s: base url is not a valid url", cfg.ID)
	}
	if u.User != nil {
		return nil, fmt.Errorf("ollama provider %s: base url must not embed credentials", cfg.ID)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !loopbackURL(u.Hostname()) && !cfg.AllowInsecureHTTP {
			return nil, fmt.Errorf("ollama provider %s: plain http is only permitted for loopback (use https or AllowInsecureHTTP)", cfg.ID)
		}
	default:
		return nil, fmt.Errorf("ollama provider %s: unsupported scheme %q", cfg.ID, u.Scheme)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	maxBytes := cfg.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &OllamaProvider{
		config: cfg, base: u, client: client, status: ProviderStatusHealthy,
		timeout: timeout, maxBytes: maxBytes,
	}, nil
}

// Identify implements Provider.
func (p *OllamaProvider) Identify() string { return p.config.ID }

// HealthCheck implements Provider: it verifies the runtime answers /api/version.
func (p *OllamaProvider) HealthCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	var out struct {
		Version string `json:"version"`
	}
	if err := p.getJSON(ctx, "/api/version", &out); err != nil {
		return err
	}
	if strings.TrimSpace(out.Version) == "" {
		return &ProviderError{Kind: "malformed_output", Provider: p.config.ID, Cause: fmt.Errorf("ollama version response is empty")}
	}
	return nil
}

// ListModels implements Provider: it returns the names of INSTALLED models.
func (p *OllamaProvider) ListModels() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	models, err := p.Discover(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Name)
	}
	return names, nil
}

// InstalledModel is one locally present model.
type InstalledModel struct {
	Name          string `json:"name"`
	Size          int64  `json:"size,omitempty"`
	Digest        string `json:"digest,omitempty"`
	ModifiedAt    string `json:"modified_at,omitempty"`
	Family        string `json:"family,omitempty"`
	ParameterSize string `json:"parameter_size,omitempty"`
	Quantization  string `json:"quantization_level,omitempty"`
	Format        string `json:"format,omitempty"`
	// Installed is always true: the local API enumerates only present models.
	// It exists to make the installed-vs-downloadable distinction explicit.
	Installed bool `json:"installed"`
}

type ollamaTagsResponse struct {
	Models []struct {
		Name       string `json:"name"`
		Size       int64  `json:"size"`
		Digest     string `json:"digest"`
		ModifiedAt string `json:"modified_at"`
		Details    struct {
			Family        string `json:"family"`
			ParameterSize string `json:"parameter_size"`
			Quantization  string `json:"quantization_level"`
			Format        string `json:"format"`
		} `json:"details"`
	} `json:"models"`
}

// Discover returns the locally installed models with best-effort metadata.
func (p *OllamaProvider) Discover(ctx context.Context) ([]InstalledModel, error) {
	var tags ollamaTagsResponse
	if err := p.getJSON(ctx, "/api/tags", &tags); err != nil {
		return nil, err
	}
	out := make([]InstalledModel, 0, len(tags.Models))
	for _, m := range tags.Models {
		out = append(out, InstalledModel{
			Name: m.Name, Size: m.Size, Digest: m.Digest, ModifiedAt: m.ModifiedAt,
			Family: m.Details.Family, ParameterSize: m.Details.ParameterSize,
			Quantization: m.Details.Quantization, Format: m.Details.Format,
			Installed: true,
		})
	}
	return out, nil
}

// ModelMetadata is best-effort metadata from /api/show.
type ModelMetadata struct {
	Name            string `json:"name"`
	Family          string `json:"family,omitempty"`
	ParameterSize   string `json:"parameter_size,omitempty"`
	Quantization    string `json:"quantization_level,omitempty"`
	Format          string `json:"format,omitempty"`
	ContextLength   int    `json:"context_length,omitempty"`
	EmbeddingLength int    `json:"embedding_length,omitempty"`
}

type ollamaShowResponse struct {
	Details struct {
		Family        string `json:"family"`
		ParameterSize string `json:"parameter_size"`
		Quantization  string `json:"quantization_level"`
		Format        string `json:"format"`
	} `json:"details"`
	ModelInfo map[string]any `json:"model_info"`
}

// ShowModel returns metadata for one installed model. A missing model is
// reported as ErrModelNotFound-equivalent (Kind "model_missing").
func (p *OllamaProvider) ShowModel(ctx context.Context, name string) (ModelMetadata, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ModelMetadata{}, &ProviderError{Kind: "invalid_request", Provider: p.config.ID, Cause: fmt.Errorf("model name is required")}
	}
	body, _ := json.Marshal(map[string]string{"name": name})
	var show ollamaShowResponse
	if err := p.postJSON(ctx, "/api/show", body, &show); err != nil {
		return ModelMetadata{}, err
	}
	md := ModelMetadata{
		Name: name, Family: show.Details.Family, ParameterSize: show.Details.ParameterSize,
		Quantization: show.Details.Quantization, Format: show.Details.Format,
	}
	for k, v := range show.ModelInfo {
		n, ok := asInt(v)
		if !ok {
			continue
		}
		switch {
		case strings.HasSuffix(k, ".context_length"):
			if n > md.ContextLength {
				md.ContextLength = n
			}
		case strings.HasSuffix(k, ".embedding_length"):
			if n > md.EmbeddingLength {
				md.EmbeddingLength = n
			}
		}
	}
	return md, nil
}

// Invoke implements Provider using Ollama's native /api/chat endpoint.
// The native endpoint supports the top-level "think" parameter which is
// required to disable thinking on thinking-capable models (qwen35 family).
// The OpenAI-compatible /v1/chat/completions endpoint does not support
// "think" and its "options.thinking" is ineffective for these models, causing
// them to exhaust their token budget on reasoning and emit no visible content.
func (p *OllamaProvider) Invoke(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if req == nil {
		return nil, &ProviderError{Kind: "invalid_request", Provider: p.config.ID, Cause: fmt.Errorf("nil request")}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	model := req.ModelID
	if strings.TrimSpace(model) == "" {
		return nil, &ProviderError{Kind: "invalid_request", Provider: p.config.ID, Cause: fmt.Errorf("model_id is required")}
	}
	msgs := make([]nativeChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		wire := nativeChatMessage{Role: m.Role, Content: m.Content}
		for _, call := range m.ToolCalls {
			wire.ToolCalls = append(wire.ToolCalls, nativeChatToolCall{ID: call.ID, Type: "function",
				Function: nativeChatToolFunction{
					Name:      call.Name,
					Arguments: json.RawMessage(call.Arguments),
				}})
		}
		msgs = append(msgs, wire)
	}
	// Token budget: Ollama's native endpoint honors options.num_predict.
	maxTokens := req.MaxTokens
	if maxTokens < 8192 {
		maxTokens = 8192
	}
	opts := make(map[string]any, 1)
	opts["num_predict"] = maxTokens
	var think *bool
	if p.config.DisableThinking {
		t := false
		think = &t
	}
	nativeReq := nativeChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   false,
		Think:    think,
		Options:  opts,
	}
	for _, def := range req.Tools {
		if strings.TrimSpace(def.Name) == "" {
			return nil, &ProviderError{Kind: "invalid_request", Provider: p.config.ID, Cause: fmt.Errorf("tool name is required")}
		}
		params, err := json.Marshal(def.Parameters)
		if err != nil || !json.Valid(params) {
			return nil, &ProviderError{Kind: "invalid_request", Provider: p.config.ID, Cause: fmt.Errorf("tool %q has invalid parameters", def.Name)}
		}
		wire := nativeChatToolDef{Type: "function"}
		wire.Function.Name = def.Name
		wire.Function.Description = def.Description
		wire.Function.Parameters = json.RawMessage(params)
		nativeReq.Tools = append(nativeReq.Tools, wire)
	}
	if req.Structured != nil {
		if strings.TrimSpace(req.Structured.Name) == "" || len(req.Structured.Schema) == 0 {
			return nil, &ProviderError{Kind: "invalid_request", Provider: p.config.ID, Cause: fmt.Errorf("structured output schema is incomplete")}
		}
		// The native /api/chat endpoint accepts a "format" field for JSON
		// output. The installed local model (bonsai-2:9b, qwen35 family) does
		// not support complex JSON Schema constraints via the format parameter;
		// sending the full decision/plan schema causes HTTP 500. We request
		// "json" mode to ensure the model emits valid JSON, which the strict
		// boundary parser (ParsePlan/ParseAction) then validates against the
		// full canonical schema. This does NOT weaken validation.
		nativeReq.Format = "json"
	}

	body, err := json.Marshal(nativeReq)
	if err != nil {
		return nil, &ProviderError{Kind: "invalid_request", Provider: p.config.ID, Cause: fmt.Errorf("serialize request failed")}
	}
	resp, err := p.do(ctx, "/api/chat", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, p.maxBytes+1))
	if err != nil {
		return nil, &ProviderError{Kind: "unavailable", Provider: p.config.ID, Cause: fmt.Errorf("read response failed")}
	}
	if int64(len(raw)) > p.maxBytes {
		return nil, &ProviderError{Kind: "malformed_output", Provider: p.config.ID, Cause: fmt.Errorf("response exceeds %d bytes", p.maxBytes)}
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, &ProviderError{Kind: "model_missing", Provider: p.config.ID, Cause: fmt.Errorf("model %q is not installed", model)}
	}
	if resp.StatusCode != http.StatusOK {
		// The body may echo the prompt; keep it out of the error.
		return nil, &ProviderError{Kind: "unavailable", Provider: p.config.ID, Cause: fmt.Errorf("http %d", resp.StatusCode)}
	}
	// Unmarshal into map[string]interface{} which works reliably with
	// nested structures in this Go build.
	var respMap map[string]interface{}
	if err := json.Unmarshal(raw, &respMap); err != nil {
		return nil, &ProviderError{Kind: "malformed_output", Provider: p.config.ID, Cause: fmt.Errorf("malformed response: %w", err)}
	}
	msgMap, ok := respMap["message"].(map[string]interface{})
	if !ok {
		return nil, &ProviderError{Kind: "malformed_output", Provider: p.config.ID, Cause: fmt.Errorf("malformed response: missing message")}
	}
	content, _ := msgMap["content"].(string)
	doneReason, _ := respMap["done_reason"].(string)
	promptEvalCount, _ := respMap["prompt_eval_count"].(float64)
	evalCount, _ := respMap["eval_count"].(float64)

	// Parse tool calls from the message.
	var toolCalls []ToolCall
	if tcList, ok := msgMap["tool_calls"].([]interface{}); ok {
		for _, tc := range tcList {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}
			id, _ := tcMap["id"].(string)
			fnMap, ok := tcMap["function"].(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := fnMap["name"].(string)
			argsMap, ok := fnMap["arguments"].(map[string]interface{})
			var args json.RawMessage
			if ok {
				argsBytes, err := json.Marshal(argsMap)
				if err == nil {
					args = json.RawMessage(argsBytes)
				}
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:        id,
				Name:      name,
				Arguments: string(args),
			})
		}
	}

	return &GenerateResponse{
		RequestID: req.RequestID, ModelID: model,
		Content:      content,
		FinishReason: doneReason,
		InputTokens:  int(promptEvalCount),
		OutputTokens: int(evalCount),
		TotalTokens:  int(promptEvalCount + evalCount),
		ToolCalls:    toolCalls,
	}, nil
}

// ---- native Ollama /api/chat request/response types ------------------------

// nativeChatRequest is the wire shape for Ollama's native /api/chat endpoint.
type nativeChatRequest struct {
	Model    string              `json:"model"`
	Messages []nativeChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Think    *bool               `json:"think,omitempty"`
	Options  map[string]any      `json:"options,omitempty"`
	Tools    []nativeChatToolDef `json:"tools,omitempty"`
	Format   string              `json:"format,omitempty"`
}

// nativeChatMessage is one message in the native chat request.
type nativeChatMessage struct {
	Role      string               `json:"role"`
	Content   string               `json:"content"`
	ToolCalls []nativeChatToolCall `json:"tool_calls,omitempty"`
}

// nativeChatToolDef is a tool definition for the native chat API.
type nativeChatToolDef struct {
	Type     string                       `json:"type"`
	Function nativeChatToolFunctionSchema `json:"function"`
}

// nativeChatToolFunctionSchema is the function schema in a native tool def.
type nativeChatToolFunctionSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// nativeChatToolCall is a tool call in a native chat message.
type nativeChatToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function nativeChatToolFunction `json:"function"`
}

// nativeChatToolFunction is the called function in a native tool call.
type nativeChatToolFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ---- transport helpers -----------------------------------------------------

func (p *OllamaProvider) getJSON(ctx context.Context, path string, out any) error {
	resp, err := p.do(ctx, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return p.decode(resp, out)
}

func (p *OllamaProvider) postJSON(ctx context.Context, path string, body []byte, out any) error {
	resp, err := p.do(ctx, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return p.decode(resp, out)
}

func (p *OllamaProvider) do(ctx context.Context, path string, body []byte) (*http.Response, error) {
	u := *p.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), reader)
	if err != nil {
		return nil, &ProviderError{Kind: "invalid_request", Provider: p.config.ID, Cause: fmt.Errorf("build request failed")}
	}
	if body == nil {
		httpReq.Method = http.MethodGet
	}
	httpReq.Header.Set("Accept", "application/json")
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for k, v := range p.config.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &ProviderError{Kind: "cancelled", Provider: p.config.ID, Cause: ctx.Err()}
		}
		if isTimeout(err) {
			return nil, &ProviderError{Kind: "timeout", Provider: p.config.ID, Cause: err}
		}
		return nil, &ProviderError{Kind: "unavailable", Provider: p.config.ID, Cause: fmt.Errorf("ollama is unavailable")}
	}
	return resp, nil
}

func (p *OllamaProvider) decode(resp *http.Response, out any) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, p.maxBytes+1))
	if err != nil {
		return &ProviderError{Kind: "unavailable", Provider: p.config.ID, Cause: fmt.Errorf("read response failed")}
	}
	if int64(len(raw)) > p.maxBytes {
		return &ProviderError{Kind: "malformed_output", Provider: p.config.ID, Cause: fmt.Errorf("response exceeds %d bytes", p.maxBytes)}
	}
	if resp.StatusCode == http.StatusNotFound {
		return &ProviderError{Kind: "model_missing", Provider: p.config.ID, Cause: fmt.Errorf("not found")}
	}
	if resp.StatusCode != http.StatusOK {
		return &ProviderError{Kind: "unavailable", Provider: p.config.ID, Cause: fmt.Errorf("http %d", resp.StatusCode)}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &ProviderError{Kind: "malformed_output", Provider: p.config.ID, Cause: fmt.Errorf("malformed response")}
	}
	return nil
}

// ErrModelMissing reports that a requested model is not installed locally.
func (e *ProviderError) ModelMissing() bool { return e != nil && e.Kind == "model_missing" }

func loopbackURL(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}
