package gateway

// Shared test helpers for live/isolated acceptance tests.

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

// liveOllama returns the local model endpoint/model used by the acceptance tests.
func liveOllama() (endpoint, model string) {
	endpoint = os.Getenv("NEXUS_LIVE_OLLAMA_URL")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434"
	}
	model = os.Getenv("NEXUS_LIVE_OLLAMA_MODEL")
	if model == "" {
		model = "bonsai-2:9b"
	}
	return
}

// writeTestConfig writes a test-owned config that declares exactly one model
// provider (the local Ollama runtime) so any successful run is attributable to
// it. It is created inside the test's temp dir and contains no secret.
func writeTestConfig(t *testing.T, healthPort int, endpoint, model string) string {
	t.Helper()
	cfg := map[string]any{
		"health": map[string]any{"enabled": true, "host": "127.0.0.1", "port": healthPort},
		"models": map[string]any{
			"preferred_model": model,
			"providers": []map[string]any{{
				"kind":                "ollama",
				"id":                  "ollama",
				"name":                "Local Ollama (test-owned config)",
				"endpoint":            endpoint,
				"model_id":            model,
				"timeout_ms":          180000,
				"allow_insecure_http": true,
				"max_response_bytes":  2097152,
				"disable_thinking":    true,
			}},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	path := filepath.Join(t.TempDir(), "acceptance-nexus-config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func livePost(t *testing.T, url, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build POST: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func liveGet(t *testing.T, url string, hdr map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build GET: %v", err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

const (
	ResearchEvidenceProvenanceVersion = execjournal.ResearchEvidenceProvenanceVersion
	ResearchEvidenceCapability        = execjournal.ResearchEvidenceCapability
	ResearchProvenanceAuthoritative   = execjournal.ResearchProvenanceAuthoritative
	ResearchSynthesisVersion          = execjournal.ResearchSynthesisVersion
	ResearchSynthesisMethod           = execjournal.ResearchSynthesisMethod
	ResearchSynthesisStatusComplete   = execjournal.ResearchSynthesisStatusComplete
	ResearchSynthesisStatusPartial    = execjournal.ResearchSynthesisStatusPartial
	ResearchEvaluationVersion         = execjournal.ResearchEvaluationVersion
)
