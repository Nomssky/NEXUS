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
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
)

// TEST-GW-001: Health endpoint returns ok
func TestHealthEndpoint(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/api/v1/requests/nonexistent?business_id=biz-1", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TEST-GW-010: Get result — found
func TestGetResultFound(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	resultReq := httptest.NewRequest("GET", "/api/v1/requests/"+submitResp["request_id"]+"?business_id=biz-1", nil)
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
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
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	srv := NewServer(engine, ":8080", WithClock(func() time.Time { return now }))

	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.Mux() == nil {
		t.Fatal("expected non-nil mux")
	}
}

// TEST-GW-016: Control status endpoint returns component health
func TestControlStatusEndpoint(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/api/v1/control/status", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "RUNNING" {
		t.Errorf("expected status running, got %v", resp["status"])
	}
	if resp["components"] == nil {
		t.Error("expected components map")
	}
}

// TEST-GW-017: Control metrics endpoint returns executor stats
func TestControlMetricsEndpoint(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/api/v1/control/metrics", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["executor"] == nil {
		t.Error("expected executor metrics")
	}
	if resp["backpressure"] == nil {
		t.Error("expected backpressure metrics")
	}
	if resp["circuit_breaker"] == nil {
		t.Error("expected circuit_breaker metrics")
	}
	if resp["recovery"] == nil {
		t.Error("expected recovery metrics")
	}
}

// TEST-GW-018: Control components endpoint lists all components
func TestControlComponentsEndpoint(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/api/v1/control/components", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	components, ok := resp["components"].([]interface{})
	if !ok || len(components) == 0 {
		t.Error("expected non-empty components list")
	}

	count, ok := resp["count"].(float64)
	if !ok || count == 0 {
		t.Error("expected positive count")
	}
}

// TEST-GW-019: Control pause endpoint stops the engine
func TestControlPauseEndpoint(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("POST", "/api/v1/control/pause", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "paused" {
		t.Errorf("expected status paused, got %v", resp["status"])
	}

	// Engine should now reject requests
	if engine.Status() != lifecycle.StateStopped {
		t.Errorf("expected engine stopped, got %s", engine.Status())
	}
}

// TEST-GW-020: Control resume endpoint returns error when engine can't restart
func TestControlResumeEndpoint(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	// Pause first
	pauseReq := httptest.NewRequest("POST", "/api/v1/control/pause", nil)
	pauseW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(pauseW, pauseReq)

	if engine.Status() != lifecycle.StateStopped {
		t.Fatal("expected engine stopped after pause")
	}

	// Resume — engine should restart successfully
	resumeReq := httptest.NewRequest("POST", "/api/v1/control/resume", nil)
	resumeW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(resumeW, resumeReq)

	if resumeW.Code != http.StatusOK {
		t.Fatalf("expected 200 on resume, got %d", resumeW.Code)
	}

	if engine.Status() != lifecycle.StateRunning {
		t.Fatal("expected engine running after resume")
	}
}

// =====================================================================
// P0 SECURITY REGRESSION TESTS
// =====================================================================

// TEST-SEC-001: Auth middleware blocks control endpoints without correct key
func TestAuthMiddlewareBlocksNoKey(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0",
		WithClock(func() time.Time { return now }),
		WithControlAPIKey("secret-key-123"),
	)

	// Use authMiddleware-wrapped handler (not raw Mux)
	handler := srv.authMiddleware(srv.Mux())

	// Control endpoint without API key → 401
	req := httptest.NewRequest("GET", "/api/v1/control/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without key, got %d", w.Code)
	}

	var resp map[string]map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if errResp, ok := resp["error"]; ok {
		if errResp["category"] != "UNAUTHORIZED" {
			t.Errorf("expected UNAUTHORIZED category, got %v", errResp["category"])
		}
	}
}

// TEST-SEC-002: Auth middleware blocks control endpoints with wrong key
func TestAuthMiddlewareBlocksWrongKey(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0",
		WithClock(func() time.Time { return now }),
		WithControlAPIKey("secret-key-123"),
	)

	handler := srv.authMiddleware(srv.Mux())

	req := httptest.NewRequest("GET", "/api/v1/control/status", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong key, got %d", w.Code)
	}
}

// TEST-SEC-003: Auth middleware allows control endpoints with correct key
func TestAuthMiddlewareAllowsCorrectKey(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0",
		WithClock(func() time.Time { return now }),
		WithControlAPIKey("secret-key-123"),
	)

	handler := srv.authMiddleware(srv.Mux())

	req := httptest.NewRequest("GET", "/api/v1/control/status", nil)
	req.Header.Set("X-API-Key", "secret-key-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with correct key, got %d", w.Code)
	}
}

// TEST-SEC-004: Auth middleware does NOT protect non-control endpoints
func TestAuthMiddlewareSkipsPublicEndpoints(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))

	srv := NewServer(engine, ":0",
		WithClock(func() time.Time { return now }),
		WithControlAPIKey("secret-key-123"),
	)

	handler := srv.authMiddleware(srv.Mux())

	// Health endpoint should work without API key even when auth is configured
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /health without key, got %d", w.Code)
	}
}

// TEST-SEC-005: Auth middleware path prefix — "/api/v1/control" (no trailing slash) should NOT match
func TestAuthMiddlewareExactPrefix(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))

	srv := NewServer(engine, ":0",
		WithClock(func() time.Time { return now }),
		WithControlAPIKey("secret-key-123"),
	)

	handler := srv.authMiddleware(srv.Mux())

	// This path starts with "/api/v1/control" but NOT "/api/v1/control/"
	// It should NOT be protected by auth middleware
	req := httptest.NewRequest("GET", "/api/v1/controlstatus", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should get 404 (no such route) not 401 (auth failure)
	if w.Code == http.StatusUnauthorized {
		t.Error("auth middleware incorrectly matched /api/v1/controlstatus as a control endpoint")
	}
}

// TEST-SEC-006: Cross-tenant result access denied
func TestGetResultCrossTenantDenied(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	// Submit request as biz-1
	body := `{"intent": "test", "business_id": "biz-1", "actor_id": "user-1"}`
	submitReq := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(submitW, submitReq)

	var submitResp map[string]string
	json.NewDecoder(submitW.Body).Decode(&submitResp)
	reqID := submitResp["request_id"]

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Try to get result as biz-2 (different business) → should be denied
	resultReq := httptest.NewRequest("GET", "/api/v1/requests/"+reqID+"?business_id=biz-2", nil)
	resultW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(resultW, resultReq)

	if resultW.Code != http.StatusForbidden {
		t.Errorf("expected 403 for cross-tenant access, got %d", resultW.Code)
	}

	var errResp map[string]map[string]interface{}
	json.NewDecoder(resultW.Body).Decode(&errResp)
	if errResp["error"]["category"] != "AUTHORIZATION" {
		t.Errorf("expected AUTHORIZATION category, got %v", errResp["error"]["category"])
	}
}

// TEST-SEC-007: Same-tenant result access allowed
func TestGetResultSameTenantAllowed(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	// Submit request as biz-1
	body := `{"intent": "test", "business_id": "biz-1", "actor_id": "user-1"}`
	submitReq := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(submitW, submitReq)

	var submitResp map[string]string
	json.NewDecoder(submitW.Body).Decode(&submitResp)
	reqID := submitResp["request_id"]

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Get result as biz-1 (same business) → should be allowed
	resultReq := httptest.NewRequest("GET", "/api/v1/requests/"+reqID+"?business_id=biz-1", nil)
	resultW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(resultW, resultReq)

	if resultW.Code != http.StatusOK {
		t.Errorf("expected 200 for same-tenant access, got %d", resultW.Code)
	}

	var result core.Response
	json.NewDecoder(resultW.Body).Decode(&result)
	if result.BusinessID != "biz-1" {
		t.Errorf("expected business_id biz-1, got %s", result.BusinessID)
	}
}

// TEST-SEC-008: Result access without business_id is REJECTED (fail-closed)
func TestGetResultNoBusinessIDRejected(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	// Submit request
	body := `{"intent": "test", "business_id": "biz-1", "actor_id": "user-1"}`
	submitReq := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(submitW, submitReq)

	var submitResp map[string]string
	json.NewDecoder(submitW.Body).Decode(&submitResp)
	reqID := submitResp["request_id"]

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Get result WITHOUT business_id → 400 (fail-closed: scoping cannot be
	// bypassed by omitting the parameter)
	resultReq := httptest.NewRequest("GET", "/api/v1/requests/"+reqID, nil)
	resultW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(resultW, resultReq)

	if resultW.Code != http.StatusBadRequest {
		t.Errorf("expected 400 without business_id (fail-closed), got %d", resultW.Code)
	}
}

// TEST-SEC-009: Response includes BusinessID field
func TestResponseIncludesBusinessID(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	// Submit request
	body := `{"intent": "test", "business_id": "biz-1", "actor_id": "user-1"}`
	submitReq := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	submitReq.Header.Set("Content-Type", "application/json")
	submitW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(submitW, submitReq)

	var submitResp map[string]string
	json.NewDecoder(submitW.Body).Decode(&submitResp)
	reqID := submitResp["request_id"]

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Get result (business_id required — fail-closed)
	resultReq := httptest.NewRequest("GET", "/api/v1/requests/"+reqID+"?business_id=biz-1", nil)
	resultW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(resultW, resultReq)

	if resultW.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", resultW.Code)
	}

	var result core.Response
	json.NewDecoder(resultW.Body).Decode(&result)

	if result.BusinessID != "biz-1" {
		t.Errorf("expected business_id biz-1 in response, got %q", result.BusinessID)
	}
}

// TEST-SEC-011: Empty control API key disables control endpoints (fail-closed)
func TestHandlerEmptyControlKeyDisabled(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	// No WithControlAPIKey — controlAPIKey is ""
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	handler := srv.Handler()

	// Control endpoint with no key configured → 403 (disabled, not open)
	req := httptest.NewRequest("GET", "/api/v1/control/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when control key not configured, got %d", w.Code)
	}

	// Even supplying a key must not open disabled endpoints
	req2 := httptest.NewRequest("GET", "/api/v1/control/status", nil)
	req2.Header.Set("X-API-Key", "anything")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Errorf("expected 403 when control key not configured (with header), got %d", w2.Code)
	}

	// Public endpoints still work
	req3 := httptest.NewRequest("GET", "/health", nil)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 for /health, got %d", w3.Code)
	}
}

// TEST-SEC-010: Gateway HTTP server has MaxHeaderBytes limit (G-016 fix)
func TestMaxHeaderBytesConfigured(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	if srv.server.MaxHeaderBytes == 0 {
		t.Error("expected MaxHeaderBytes to be configured (0 = unlimited, DoS risk)")
	}
}
