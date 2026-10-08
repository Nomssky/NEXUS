package modelrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// OpenAIProvider is a production-capable Provider that speaks the
// OpenAI-compatible Chat Completions API. It is deliberately NOT a vendor SDK:
// any endpoint that implements POST {base}/chat/completions and the normalized
// response shape can be registered behind the same Provider interface (§13).
type OpenAIProvider struct {
	config ProviderConfig
	client *http.Client
	mu     sync.RWMutex
	status ProviderStatus
}

// NewOpenAIProvider validates the configuration and returns a ready provider.
// A missing credential environment variable, an insecure plain-HTTP base URL
// without the explicit opt-in, or an unparseable base URL all fail here —
// boot must refuse a production provider that cannot be trusted.
func NewOpenAIProvider(cfg ProviderConfig) (*OpenAIProvider, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		return nil, fmt.Errorf("provider config: id is required")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("provider %q: base URL is required", cfg.ID)
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("provider %q: invalid base URL", cfg.ID)
	}
	if u.Scheme != "https" {
		if u.Scheme != "http" {
			return nil, fmt.Errorf("provider %q: base URL must be https (or http with allow_insecure_http)", cfg.ID)
		}
		if !cfg.AllowInsecureHTTP {
			return nil, fmt.Errorf("provider %q: plain HTTP requires allow_insecure_http", cfg.ID)
		}
		if !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("provider %q: plain HTTP is only permitted for loopback addresses", cfg.ID)
		}
	}
	if strings.TrimSpace(cfg.APIKeyEnv) == "" {
		return nil, fmt.Errorf("provider %q: api_key_env is required (the API key lives in the environment, never in config files)", cfg.ID)
	}
	if strings.TrimSpace(os.Getenv(cfg.APIKeyEnv)) == "" {
		return nil, fmt.Errorf("provider %q: credential %q is not set", cfg.ID, cfg.APIKeyEnv)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenAIProvider{
		config: cfg,
		client: &http.Client{Timeout: timeout},
		status: initialProviderStatus(cfg.Status),
	}, nil
}

func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (p *OpenAIProvider) Identify() string { return p.config.ID }

func (p *OpenAIProvider) HealthCheck() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.status == ProviderStatusOffline {
		return fmt.Errorf("provider %s is offline", p.config.ID)
	}
	return nil
}

func (p *OpenAIProvider) ListModels() ([]string, error) { return nil, nil }

// chatMessage/chatRequest/chatResponse mirror the OpenAI Chat Completions wire
// format. Only the fields the runtime needs are modeled; unknown response
// fields are ignored rather than rejected, so the adapter tolerates
// OpenAI-compatible implementations with extra metadata.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	ResponseFmt *chatRespFmt  `json:"response_format,omitempty"`
}

type chatRespFmt struct {
	Type       string          `json:"type"`
	JSONSchema *chatJSONSchema `json:"json_schema,omitempty"`
}

type chatJSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Strict bool            `json:"strict,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Invoke sends the request to {base}/chat/completions and normalizes the
// response. The caller's context governs cancellation and deadline.
func (p *OpenAIProvider) Invoke(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	offline := p.status == ProviderStatusOffline
	p.mu.RUnlock()
	if offline {
		return nil, fmt.Errorf("provider %s is offline", p.config.ID)
	}
	key := os.Getenv(p.config.APIKeyEnv)
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("provider %s: credential %q is not set", p.config.ID, p.config.APIKeyEnv)
	}

	msgs := make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}
	chatReq := chatRequest{
		Model:       req.ModelID,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	// The canonical structured-output request renders as the provider's
	// strict JSON-schema response format. A malformed schema declaration is
	// a configuration error, never a silent downgrade to free text.
	if req.Structured != nil {
		if strings.TrimSpace(req.Structured.Name) == "" || len(req.Structured.Schema) == 0 {
			return nil, &ProviderError{Kind: "invalid_request", Provider: p.config.ID,
				Cause: fmt.Errorf("structured output schema is incomplete")}
		}
		var schemaDoc json.RawMessage
		if err := json.Unmarshal(req.Structured.Schema, &schemaDoc); err != nil {
			return nil, &ProviderError{Kind: "invalid_request", Provider: p.config.ID,
				Cause: fmt.Errorf("structured output schema is not valid JSON: %w", err)}
		}
		chatReq.ResponseFmt = &chatRespFmt{Type: "json_schema",
			JSONSchema: &chatJSONSchema{Name: req.Structured.Name, Schema: schemaDoc, Strict: req.Structured.Strict}}
	} else if req.ResponseFormat == "json" {
		chatReq.ResponseFmt = &chatRespFmt{Type: "json_object"}
	}
	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("provider %s: serialize request: %w", p.config.ID, err)
	}
	if max := p.config.MaxRequestBytes; max > 0 && int64(len(body)) > max {
		return nil, fmt.Errorf("provider %s: request body exceeds %d bytes", p.config.ID, max)
	}

	endpoint := strings.TrimRight(p.config.Endpoint, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provider %s: build request: %w", p.config.ID, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &ProviderError{Kind: "cancelled", Provider: p.config.ID, Cause: ctx.Err()}
		}
		if isTimeout(err) {
			return nil, &ProviderError{Kind: "timeout", Provider: p.config.ID, Cause: err}
		}
		return nil, &ProviderError{Kind: "unavailable", Provider: p.config.ID, Cause: err}
	}
	defer httpResp.Body.Close()
	latency := time.Since(start).Milliseconds()

	maxResp := p.config.MaxResponseBytes
	if maxResp <= 0 {
		maxResp = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResp+1))
	if err != nil {
		return nil, fmt.Errorf("provider %s: read response: %s", p.config.ID, sanitizeError(err))
	}
	if int64(len(raw)) > maxResp {
		return nil, fmt.Errorf("provider %s: response exceeds %d bytes", p.config.ID, maxResp)
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider %s: http %d: %s", p.config.ID, httpResp.StatusCode, redactSnippet(string(raw)))
	}
	var chat chatResponse
	if err := json.Unmarshal(raw, &chat); err != nil {
		return nil, fmt.Errorf("provider %s: malformed response: %s", p.config.ID, redactSnippet(err.Error()))
	}
	if len(chat.Choices) == 0 {
		return nil, fmt.Errorf("provider %s: malformed response: no choices", p.config.ID)
	}
	return &GenerateResponse{
		RequestID:    req.RequestID,
		ModelID:      req.ModelID,
		Content:      chat.Choices[0].Message.Content,
		FinishReason: chat.Choices[0].FinishReason,
		InputTokens:  chat.Usage.PromptTokens,
		OutputTokens: chat.Usage.CompletionTokens,
		TotalTokens:  chat.Usage.TotalTokens,
		LatencyMs:    latency,
	}, nil
}

// ProviderError classifies failures into the contract's model-failure model
// (PROVIDER_CONTRACTS §7.3) without inventing new categories.
type ProviderError struct {
	Kind     string // "unavailable"|"timeout"|"cancelled"|"malformed_output"|"invalid_request"|"policy_rejection"
	Provider string
	Cause    error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("provider %s: %s: %s", e.Provider, e.Kind, e.Cause)
	}
	return fmt.Sprintf("provider %s: %s", e.Provider, e.Kind)
}

func (e *ProviderError) Unwrap() error { return e.Cause }

func isTimeout(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// secretPatterns strips credential-looking substrings from error text. The
// adapter never logs raw secrets, and an upstream error that echoes one must
// not survive normalization.
var secretPatterns = regexp.MustCompile(`(?i)(bearer\s+[A-Za-z0-9._\-]+|api[_-]?key\s*[:=]\s*[A-Za-z0-9._\-]+|sk-[A-Za-z0-9._\-]+)`)

func redactSnippet(s string) string {
	if len(s) > 256 {
		s = s[:256] + "…"
	}
	return secretPatterns.ReplaceAllString(s, "[redacted]")
}

func sanitizeError(err error) string {
	return redactSnippet(err.Error())
}
