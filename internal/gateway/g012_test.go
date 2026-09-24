package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
)

// fetchComponents GETs /api/v1/control/components and returns the decoded body.
func fetchComponents(t *testing.T, srv *Server) (map[string]interface{}, []ComponentInfo) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/control/components", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw, ok := resp["components"].([]interface{})
	if !ok {
		t.Fatalf("components not an array: %T", resp["components"])
	}
	components := make([]ComponentInfo, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("component entry not an object: %T", item)
		}
		components = append(components, ComponentInfo{
			Name:   m["name"].(string),
			Status: m["status"].(string),
			Type:   m["type"].(string),
		})
	}
	return resp, components
}

func componentByName(components []ComponentInfo, name string) (ComponentInfo, bool) {
	for _, c := range components {
		if c.Name == name {
			return c, true
		}
	}
	return ComponentInfo{}, false
}

// G-012 #1: normal (running) runtime — status reflects actual expected state.
func TestG012RunningEngineReportsActualState(t *testing.T) {
	now := time.Now()
	engine, err := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))
	_, components := fetchComponents(t, srv)

	eng, ok := componentByName(components, "engine")
	if !ok {
		t.Fatal("missing engine component")
	}
	if eng.Status != string(lifecycle.StateRunning) {
		t.Errorf("engine status: want %s, got %q", lifecycle.StateRunning, eng.Status)
	}

	cb, ok := componentByName(components, "circuit_breaker")
	if !ok {
		t.Fatal("missing circuit_breaker component")
	}
	if cb.Status != "closed" {
		t.Errorf("circuit_breaker initial status: want closed, got %q", cb.Status)
	}

	exec, ok := componentByName(components, "task_executor")
	if !ok {
		t.Fatal("missing task_executor component")
	}
	if exec.Status != "running" {
		t.Errorf("task_executor while engine running: want running, got %q", exec.Status)
	}
}

// G-012 #2: stopped/unavailable component — endpoint does not report "active".
func TestG012StoppedEngineDoesNotReportActive(t *testing.T) {
	now := time.Now()
	engine, err := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := engine.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))
	_, components := fetchComponents(t, srv)

	eng, _ := componentByName(components, "engine")
	if eng.Status != string(lifecycle.StateStopped) {
		t.Errorf("engine status after stop: want %s, got %q", lifecycle.StateStopped, eng.Status)
	}

	exec, ok := componentByName(components, "task_executor")
	if !ok {
		t.Fatal("missing task_executor component")
	}
	if exec.Status == "active" {
		t.Error("task_executor must not report active after engine stop")
	}
	if exec.Status != "stopped" {
		t.Errorf("task_executor after stop: want stopped, got %q", exec.Status)
	}
}

// G-012 #3: component with no meaningful lifecycle state — deterministic, truthful.
func TestG012NoLifecycleComponentsReportConfigured(t *testing.T) {
	now := time.Now()
	engine, err := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))
	_, components := fetchComponents(t, srv)

	noLifecycle := []string{
		"backpressure",
		"recovery_manager",
		"event_bus",
		"memory_store",
		"attention_engine",
		"governance",
		"model_router",
	}
	for _, name := range noLifecycle {
		c, ok := componentByName(components, name)
		if !ok {
			t.Errorf("missing component %q", name)
			continue
		}
		if c.Status != "configured" {
			t.Errorf("%s status: want configured (no lifecycle), got %q", name, c.Status)
		}
	}

	// Deterministic: second fetch yields identical statuses.
	_, again := fetchComponents(t, srv)
	for _, name := range noLifecycle {
		a, _ := componentByName(components, name)
		b, _ := componentByName(again, name)
		if a.Status != b.Status {
			t.Errorf("%s non-deterministic: %q then %q", name, a.Status, b.Status)
		}
	}
}

// G-012 #4: no hardcoded "active" remains for the affected component set.
func TestG012NoComponentReportsActive(t *testing.T) {
	now := time.Now()
	engine, err := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))
	_, components := fetchComponents(t, srv)

	for _, c := range components {
		if c.Status == "active" {
			t.Errorf("component %q reports hardcoded active", c.Name)
		}
	}
}

// G-012 #5: existing response shape and unrelated fields remain compatible.
func TestG012ResponseShapeCompatible(t *testing.T) {
	now := time.Now()
	engine, err := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))
	resp, components := fetchComponents(t, srv)

	if count, ok := resp["count"].(float64); !ok || int(count) != len(components) {
		t.Errorf("count: want %d, got %v", len(components), resp["count"])
	}
	if len(components) == 0 {
		t.Fatal("expected non-empty components list")
	}

	want := map[string]string{
		"engine":           "core",
		"circuit_breaker":  "hardening",
		"backpressure":     "hardening",
		"recovery_manager": "hardening",
		"event_bus":        "foundation",
		"memory_store":     "foundation",
		"attention_engine": "foundation",
		"governance":       "foundation",
		"task_executor":    "execution",
		"model_router":     "intelligence",
	}
	for name, typ := range want {
		c, ok := componentByName(components, name)
		if !ok {
			t.Errorf("missing component %q", name)
			continue
		}
		if c.Type != typ {
			t.Errorf("%s type: want %q, got %q", name, typ, c.Type)
		}
		if c.Status == "" {
			t.Errorf("%s has empty status", name)
		}
	}
}
