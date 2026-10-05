package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// getReqResult issues GET /api/v1/requests/{id} and returns the recorder.
func getReqResult(srv *Server, id, businessID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/v1/requests/"+id+"?business_id="+businessID, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// TEST-F6: GET /api/v1/requests/{id} used to answer 404 for every request
// that had not reached a terminal state — core only stores a result when the
// chain finishes. That made an admitted, running request indistinguishable
// from a typo'd id over the exact endpoint docs/http-gateway.md tells clients
// to observe, so the whole run window was invisible to a polling client.
//
// Contract: known but not yet terminal → 202 with {request_id,
// correlation_id, status:"pending"}; unknown → 404 VALIDATION (unchanged);
// terminal → 200 with the stored result (unchanged).
func TestGetResultReportsPendingWhileInFlight(t *testing.T) {
	engine, p := cancelEngine(t)
	// Release before the engine stops if the assertions fail early — a
	// blocked provider otherwise holds Stop for its whole deadline.
	t.Cleanup(func() {
		select {
		case <-p.release:
		default:
			close(p.release)
		}
	})
	srv := NewServer(engine, ":0")

	id := submitAndWait(t, srv, engine, "pending probe", "biz-1", "user-1", false)

	// Make sure the request is genuinely executing before probing.
	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the request to reach the provider")
	}

	w := getReqResult(srv, id, "biz-1")
	if w.Code != http.StatusAccepted {
		t.Fatalf("in-flight GET: expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode pending body: %v (raw: %s)", err, w.Body.String())
	}
	if payload["status"] != "pending" {
		t.Errorf("status: want pending, got %q", payload["status"])
	}
	if payload["request_id"] != id {
		t.Errorf("request_id: want %q, got %q", id, payload["request_id"])
	}
	if payload["correlation_id"] == "" {
		t.Error("correlation_id must be echoed so the polling client can chain it")
	}

	// Scope and existence rules are unchanged while pending.
	if w := getReqResult(srv, id, "biz-2"); w.Code != http.StatusNotFound {
		t.Errorf("foreign scope while pending: expected 404, got %d", w.Code)
	}
	if w := getReqResult(srv, "req-nope", "biz-1"); w.Code != http.StatusNotFound {
		t.Errorf("unknown id: expected 404, got %d", w.Code)
	}

	// Releasing the provider must land the request on the documented 200.
	close(p.release)
	deadline := time.Now().Add(10 * time.Second)
	for {
		w := getReqResult(srv, id, "biz-1")
		if w.Code == http.StatusOK {
			var result struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatalf("decode result: %v (raw: %s)", err, w.Body.String())
			}
			if result.Status != "completed" {
				t.Errorf("terminal status: want completed, got %q", result.Status)
			}
			break
		}
		if w.Code != http.StatusAccepted {
			t.Fatalf("expected 200 or 202 while settling, got %d body=%s", w.Code, w.Body.String())
		}
		if time.Now().After(deadline) {
			t.Fatal("request never reached a terminal state")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
