package modelrouter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewOllamaProviderValidation(t *testing.T) {
	if _, err := NewOllamaProvider(OllamaConfig{BaseURL: "http://example.com:11434"}); err == nil {
		t.Fatal("plain http to a non-loopback host must be refused without AllowInsecureHTTP")
	}
	if _, err := NewOllamaProvider(OllamaConfig{BaseURL: "http://example.com:11434", AllowInsecureHTTP: true}); err != nil {
		t.Fatalf("explicit insecure opt-in must be honored: %v", err)
	}
	if _, err := NewOllamaProvider(OllamaConfig{BaseURL: "not a url"}); err == nil {
		t.Fatal("invalid url must be rejected")
	}
	p, err := NewOllamaProvider(OllamaConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Identify() != "ollama" {
		t.Fatalf("identity=%s", p.Identify())
	}
}

func TestOllamaHealthCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"version":"0.40.11"}`))
	}))
	defer srv.Close()
	p, _ := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL})
	if err := p.HealthCheck(); err != nil {
		t.Fatalf("health: %v", err)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":""}`))
	}))
	defer empty.Close()
	pe, _ := NewOllamaProvider(OllamaConfig{BaseURL: empty.URL})
	if err := pe.HealthCheck(); err == nil {
		t.Fatal("empty version must fail closed")
	}
}

func TestOllamaDiscoverAndShow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5-coder:7b","size":4700000000,"digest":"abc","modified_at":"2026-01-01T00:00:00Z","details":{"family":"qwen2","parameter_size":"7.6B","quantization_level":"Q4_K_M","format":"gguf"}}]}`))
		case "/api/show":
			_, _ = w.Write([]byte(`{"details":{"family":"qwen2","parameter_size":"7.6B","quantization_level":"Q4_K_M","format":"gguf"},"model_info":{"qwen2.context_length":32768,"qwen2.embedding_length":3584}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p, _ := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL})

	models, err := p.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Name != "qwen2.5-coder:7b" || !models[0].Installed ||
		models[0].ParameterSize != "7.6B" || models[0].Quantization != "Q4_K_M" {
		t.Fatalf("unexpected discovery: %+v", models)
	}
	list, err := p.ListModels()
	if err != nil || len(list) != 1 || list[0] != "qwen2.5-coder:7b" {
		t.Fatalf("list models: %v %v", list, err)
	}
	md, err := p.ShowModel(context.Background(), "qwen2.5-coder:7b")
	if err != nil {
		t.Fatal(err)
	}
	if md.ContextLength != 32768 || md.EmbeddingLength != 3584 {
		t.Fatalf("unexpected metadata: %+v", md)
	}
}

func TestOllamaInvokeSuccessAndToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":"hello"},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":1}`))
	}))
	defer srv.Close()
	p, _ := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL})
	resp, err := p.Invoke(context.Background(), &GenerateRequest{RequestID: "r", ModelID: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello" || resp.TotalTokens != 4 {
		t.Fatalf("unexpected response: %+v", resp)
	}

	toolSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"calc","arguments":{"a":"1"}}}]},"done":true,"done_reason":"tool_calls","prompt_eval_count":3,"eval_count":1}`))
	}))
	defer toolSrv.Close()
	pt, _ := NewOllamaProvider(OllamaConfig{BaseURL: toolSrv.URL})
	resp, err = pt.Invoke(context.Background(), &GenerateRequest{RequestID: "r", ModelID: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "calc" || resp.ToolCalls[0].Arguments != `{"a":"1"}` {
		t.Fatalf("tool call not normalized: %+v", resp.ToolCalls)
	}
}

func TestOllamaInvokeThinkDisabled(t *testing.T) {
	var sentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		sentBody = string(b)
		_, _ = w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":"hello"},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":1}`))
	}))
	defer srv.Close()
	p, _ := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL, DisableThinking: true})
	resp, err := p.Invoke(context.Background(), &GenerateRequest{RequestID: "r", ModelID: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if !strings.Contains(sentBody, `"think":false`) {
		t.Fatalf("expected think=false in request, got: %s", sentBody)
	}
	if !strings.Contains(sentBody, `"num_predict"`) {
		t.Fatalf("expected options.num_predict in request, got: %s", sentBody)
	}
}

func TestOllamaInvokeThinkEnabledByDefault(t *testing.T) {
	var sentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		sentBody = string(b)
		_, _ = w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":"hello"},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":1}`))
	}))
	defer srv.Close()
	p, _ := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL}) // DisableThinking defaults to false
	if _, err := p.Invoke(context.Background(), &GenerateRequest{RequestID: "r", ModelID: "m", Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	// num_predict is always set (Ollama honors options.num_predict on native endpoint)
	if !strings.Contains(sentBody, `"num_predict"`) {
		t.Fatalf("expected options.num_predict in request, got: %s", sentBody)
	}
	if strings.Contains(sentBody, `"think":false`) {
		t.Fatalf("expected think=true (not sent) when thinking is not disabled, got: %s", sentBody)
	}
}

func TestOllamaInvokeEmptyContentPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		// Simulates a thinking model that exhausts its budget on reasoning
		// and emits no visible content. The provider passes it through;
		// the protocol boundary (agentintel/protocol.go) rejects it.
		_, _ = w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":""},"done":true,"done_reason":"length","prompt_eval_count":10,"eval_count":4086}`))
	}))
	defer srv.Close()
	p, _ := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL})
	resp, err := p.Invoke(context.Background(), &GenerateRequest{RequestID: "r", ModelID: "m", Messages: []Message{{Role: "user", Content: "think hard"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "" {
		t.Fatalf("expected empty content, got %q", resp.Content)
	}
}

func TestOllamaInvokeErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		kind   string
	}{
		"model missing":   {http.StatusNotFound, `{"error":"model not found"}`, "model_missing"},
		"unavailable":     {http.StatusInternalServerError, `{"error":"boom"}`, "unavailable"},
		"malformed json":  {http.StatusOK, `not json`, "malformed_output"},
		"no message":      {http.StatusOK, `{"model":"m","done":true}`, "malformed_output"},
		"incomplete call": {http.StatusOK, `{"model":"m","message":{"role":"assistant","tool_calls":[{"id":"c","type":"function","function":{"name":"calc"}}]}},"done":true,"done_reason":"tool_calls"}`, "malformed_output"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			p, _ := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL})
			_, err := p.Invoke(context.Background(), &GenerateRequest{ModelID: "m", Messages: []Message{{Role: "user", Content: "x"}}})
			var pe *ProviderError
			if !errors.As(err, &pe) || pe.Kind != c.kind {
				t.Fatalf("want kind %s, got %v", c.kind, err)
			}
		})
	}
}

func TestOllamaInvokeCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"model":"m","message":{"content":"late"},"done":true,"done_reason":"stop"}`))
	}))
	defer srv.Close()
	p, _ := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL, Timeout: 5 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := p.Invoke(ctx, &GenerateRequest{ModelID: "m", Messages: []Message{{Role: "user", Content: "x"}}})
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.Kind != "cancelled" {
		t.Fatalf("expected cancelled, got %v", err)
	}
	if strings.Contains(err.Error(), "x") && !strings.Contains(err.Error(), "cancel") {
		t.Fatalf("error must not leak prompt content: %v", err)
	}
}

// TestOllamaInvokeStructuredOutputFormat verifies that when a structured
// output schema is provided, the native /api/chat request includes
// format="json". The installed local model (bonsai-2:9b, qwen35 family) does
// not support complex JSON Schema constraints via the format parameter;
// sending the full schema causes HTTP 500. The provider requests "json" mode
// to ensure valid JSON output, which the strict boundary parser then
// validates against the full canonical schema.
func TestOllamaInvokeStructuredOutputFormat(t *testing.T) {
	var sentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		sentBody = string(b)
		_, _ = w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":"{\"type\":\"complete\",\"result\":\"ok\"}"},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":1}`))
	}))
	defer srv.Close()
	p, _ := NewOllamaProvider(OllamaConfig{BaseURL: srv.URL})
	schema := []byte(`{"type":"object","properties":{"type":{"type":"string","enum":["complete"]},"result":{"type":"string"}},"required":["type","result"]}`)
	_, err := p.Invoke(context.Background(), &GenerateRequest{
		RequestID: "r", ModelID: "m", Messages: []Message{{Role: "user", Content: "hi"}},
		Structured: &StructuredOutputSchema{Name: "agent_decision_v1", Schema: schema, Strict: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sentBody, `"format":"json"`) {
		t.Fatalf("expected format=json in request, got: %s", sentBody)
	}
	// The full schema must NOT be sent (it causes HTTP 500 on this model)
	if strings.Contains(sentBody, "agent_decision_v1") {
		t.Fatalf("full schema name must not be sent in format field, got: %s", sentBody)
	}
	if strings.Contains(sentBody, "additionalProperties") {
		t.Fatalf("full schema must not be sent in format field, got: %s", sentBody)
	}
}
