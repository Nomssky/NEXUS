package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
)

// TEST-GW-RESUME-C2: pause → resume over a REAL HTTP connection must leave the
// engine's processing loop alive after the resume handler returns, and a
// post-resume request must reach a terminal state. Regressions for:
//   - C-2: handleControlResume passed r.Context() into Engine.Resume; net/http
//     cancels it when the handler returns, killing the loop — submits were
//     admitted (202) but never processed, cancel answered 202 forever with
//     GET 404. httptest.NewRequest never cancels, which is why the old test
//     (TEST-GW-020) stayed green.
//   - C-3: Resume did not restart the task executor, so a surviving loop
//     still failed every request with "executor not running".
func TestControlResumeOverRealHTTPProcessesAfterwards(t *testing.T) {
	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	registerSimulatedProvider(t, engine)
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })

	srv := NewServer(engine, "127.0.0.1:0", WithControlAPIKey("resume-test-key"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	do := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		var req *http.Request
		var reqErr error
		if body != "" {
			req, reqErr = http.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req, reqErr = http.NewRequest(method, path, nil)
		}
		if reqErr != nil {
			t.Fatalf("build request: %v", reqErr)
		}
		req.Header.Set("X-API-Key", "resume-test-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		payload := map[string]any{}
		_ = json.Unmarshal(raw, &payload)
		return resp.StatusCode, payload
	}

	// Pause → engine STOPPED.
	if code, _ := do(http.MethodPost, ts.URL+"/api/v1/control/pause", ""); code != http.StatusOK {
		t.Fatalf("pause: got %d", code)
	}
	if got := engine.Status(); got != "STOPPED" {
		t.Fatalf("expected STOPPED after pause, got %s", got)
	}

	// Resume over a real connection: the request context dies when this
	// handler returns — the processing loop must survive that (C-2) and the
	// executor must be running again (C-3).
	if code, _ := do(http.MethodPost, ts.URL+"/api/v1/control/resume", ""); code != http.StatusOK {
		t.Fatalf("resume: got %d", code)
	}

	code, submitted := do(http.MethodPost, ts.URL+"/api/v1/requests",
		`{"intent":"post-resume work","business_id":"biz-1","actor_id":"user-1"}`)
	if code != http.StatusAccepted {
		t.Fatalf("submit after resume: got %d (%v)", code, submitted)
	}
	reqID, _ := submitted["request_id"].(string)
	if reqID == "" {
		t.Fatalf("submit: missing request_id in %v", submitted)
	}

	// Poll for the terminal result: with a dead loop this 404s forever.
	deadline := time.Now().Add(10 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet,
			ts.URL+"/api/v1/requests/"+reqID+"?business_id=biz-1", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET result: %v", err)
		}
		var payload struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && payload.Status != "" {
			status = payload.Status
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "completed" {
		t.Fatalf("post-resume request must complete (C-2/C-3): got status=%q", status)
	}
}
