package modelrouter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// withTestEnv gives each OpenAI test its own credential environment value.
func withTestEnv(t *testing.T, value string) {
	t.Helper()
	const name = "NEXUS_MODEL_API_KEY_TEST"
	t.Setenv(name, value)
}

func okProvider(t *testing.T, srv *httptest.Server) *OpenAIProvider {
	t.Helper()
	withTestEnv(t, "test-secret-key")
	p, err := NewOpenAIProvider(ProviderConfig{
		ID: "openai-test", Endpoint: srv.URL, APIKeyEnv: "NEXUS_MODEL_API_KEY_TEST",
		Timeout: 2 * time.Second, AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	return p
}

func TestOpenAIProviderSuccess(t *testing.T) {
	var sawAuth string
	var sawBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"type\":\"complete\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}`)
	}))
	defer srv.Close()
	p := okProvider(t, srv)
	resp, err := p.Invoke(context.Background(), &GenerateRequest{RequestID: "r1", ModelID: "gpt-test", Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: 64})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if sawAuth != "Bearer test-secret-key" {
		t.Fatalf("authorization header not sent correctly: %q", sawAuth)
	}
	if !strings.Contains(string(sawBody), `"model":"gpt-test"`) {
		t.Fatalf("request did not carry the model: %s", sawBody)
	}
	if resp.Content == "" || resp.InputTokens != 12 || resp.OutputTokens != 5 || resp.TotalTokens != 17 {
		t.Fatalf("unexpected normalized response: %+v", resp)
	}
	if strings.Contains(resp.Content, "test-secret-key") {
		t.Fatal("credential leaked into the normalized response")
	}
}

func TestOpenAIProviderHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid key"}}`)
	}))
	defer srv.Close()
	p := okProvider(t, srv)
	_, err := p.Invoke(context.Background(), &GenerateRequest{RequestID: "r", ModelID: "m"})
	if err == nil || !strings.Contains(err.Error(), "http 401") {
		t.Fatalf("expected http error, got %v", err)
	}
	if strings.Contains(err.Error(), "Bearer") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("secret leaked into error: %v", err)
	}
}

func TestOpenAIProviderMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{not valid json`)
	}))
	defer srv.Close()
	p := okProvider(t, srv)
	if _, err := p.Invoke(context.Background(), &GenerateRequest{RequestID: "r", ModelID: "m"}); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("expected malformed, got %v", err)
	}
}

func TestOpenAIProviderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	withTestEnv(t, "k")
	p, err := NewOpenAIProvider(ProviderConfig{ID: "t", Endpoint: srv.URL, APIKeyEnv: "NEXUS_MODEL_API_KEY_TEST", Timeout: 50 * time.Millisecond, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Invoke(context.Background(), &GenerateRequest{RequestID: "r", ModelID: "m"}); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestOpenAIProviderCancellationPropagates(t *testing.T) {
	started := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt32(&started, 1)
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	p := okProvider(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := p.Invoke(ctx, &GenerateRequest{RequestID: "r", ModelID: "m"})
	if err == nil {
		t.Fatal("expected cancellation")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancelled classification, got %v", err)
	}
	_ = atomic.LoadInt32(&started)
}

func TestOpenAIProviderOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 4096)))
	}))
	defer srv.Close()
	withTestEnv(t, "k")
	p, _ := NewOpenAIProvider(ProviderConfig{ID: "t", Endpoint: srv.URL, APIKeyEnv: "NEXUS_MODEL_API_KEY_TEST", Timeout: time.Second, AllowInsecureHTTP: true, MaxResponseBytes: 1024})
	if _, err := p.Invoke(context.Background(), &GenerateRequest{RequestID: "r", ModelID: "m"}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversize rejection, got %v", err)
	}
}

func TestOpenAIProviderHTTPSRequired(t *testing.T) {
	withTestEnv(t, "k")
	if _, err := NewOpenAIProvider(ProviderConfig{ID: "t", Endpoint: "https://api.example.com/v1", APIKeyEnv: "NEXUS_MODEL_API_KEY_TEST"}); err != nil {
		t.Fatalf("https should be accepted: %v", err)
	}
	if _, err := NewOpenAIProvider(ProviderConfig{ID: "t", Endpoint: "http://api.example.com/v1", APIKeyEnv: "NEXUS_MODEL_API_KEY_TEST", AllowInsecureHTTP: true}); err == nil {
		t.Fatal("plain-HTTP non-loopback must require the insecure opt-out AND still fail here")
	}
	if _, err := NewOpenAIProvider(ProviderConfig{ID: "t", Endpoint: "http://127.0.0.1:1/v1", APIKeyEnv: "NEXUS_MODEL_API_KEY_TEST"}); err == nil {
		t.Fatal("loopback HTTP without opt-in must fail")
	}
}

func TestOpenAIProviderCredentialMissing(t *testing.T) {
	t.Setenv("NEXUS_MODEL_API_KEY_TEST", "")
	if _, err := NewOpenAIProvider(ProviderConfig{ID: "t", Endpoint: "http://127.0.0.1:1", APIKeyEnv: "NEXUS_MODEL_API_KEY_TEST", AllowInsecureHTTP: true}); err == nil {
		t.Fatal("a missing credential must fail honestly")
	}
}
