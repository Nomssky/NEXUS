package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
)

// TEST-GW-001: Health endpoint returns ok
func TestHealthEndpoint(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("expected ok, got %s", resp["status"])
	}
}

// TEST-GW-002: Ready endpoint returns ready when engine running
func TestReadyEndpointRunning(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ready" {
		t.Errorf("expected ready, got %s", resp["status"])
	}
}

// TEST-GW-003: Ready endpoint returns not ready when engine stopped
func TestReadyEndpointNotReady(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

// TEST-GW-004: Status endpoint returns engine status
func TestStatusEndpoint(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "CREATED" {
		t.Errorf("expected CREATED, got %s", resp["status"])
	}
}

// TEST-GW-005: Submit request — missing intent
func TestSubmitRequestMissingIntent(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	body := `{"business_id": "biz-1", "actor_id": "user-1"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TEST-GW-006: Submit request — missing business_id
func TestSubmitRequestMissingBusiness(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	body := `{"intent": "test", "actor_id": "user-1"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TEST-GW-007: Submit request — successful
func TestSubmitRequestSuccess(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	body := `{"intent": "test request", "business_id": "biz-1", "actor_id": "user-1"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "accepted" {
		t.Errorf("expected accepted, got %s", resp["status"])
	}
	if resp["request_id"] == "" {
		t.Error("expected request_id")
	}
}

// TEST-GW-008: Submit request with custom correlation ID
func TestSubmitRequestCustomCorrelation(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	body := `{"intent": "test", "business_id": "biz-1", "actor_id": "user-1"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", "my-custom-id")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}

	corrID := w.Header().Get("X-Correlation-ID")
	if corrID != "my-custom-id" {
		t.Errorf("expected my-custom-id, got %s", corrID)
	}
}

// TEST-GW-009: Get result — not found
func TestGetResultNotFound(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/api/v1/requests/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TEST-GW-010: Get result — found
func TestGetResultFound(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	// Submit a request
	body := `{"intent": "test", "business_id": "biz-1", "actor_id": "user-1"}`
	submitReq := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(submitW, submitReq)

	var submitResp map[string]string
	json.NewDecoder(submitW.Body).Decode(&submitResp)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Get result
	resultReq := httptest.NewRequest("GET", "/api/v1/requests/"+submitResp["request_id"], nil)
	resultW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(resultW, resultReq)

	if resultW.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", resultW.Code)
	}

	var result core.Response
	json.NewDecoder(resultW.Body).Decode(&result)
	if result.Status != "completed" {
		t.Errorf("expected completed, got %s", result.Status)
	}
}

// TEST-GW-011: Invalid JSON body
func TestSubmitRequestInvalidJSON(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TEST-GW-012: Submit to stopped engine returns 503
func TestSubmitToStoppedEngine(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	body := `{"intent": "test", "business_id": "biz-1", "actor_id": "user-1"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

// TEST-GW-013: Health endpoint — consistent format
func TestHealthEndpointFormat(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)

	if _, ok := resp["time"]; !ok {
		t.Error("expected time field in health response")
	}
}

// TEST-GW-014: Submit request — priority field
func TestSubmitRequestWithPriority(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	body := `{"intent": "urgent", "business_id": "biz-1", "actor_id": "user-1", "priority": 10}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}
}

// TEST-GW-015: Server creation
func TestServerCreation(t *testing.T) {
	now := time.Now()
	engine := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	srv := NewServer(engine, ":8080", WithClock(func() time.Time { return now }))

	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.Mux() == nil {
		t.Fatal("expected non-nil mux")
	}
}
