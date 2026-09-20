package launcher

import (
	"context"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
)

func testConfig() config.Config {
	return config.Config{
		Nexus: config.NexusConfig{
			ID:          "test-nexus",
			Environment: "test",
		},
		Health: config.HealthConfig{
			Enabled: true,
			Host:    "127.0.0.1",
			Port:    0,
		},
		Lifecycle: config.LifecycleConfig{
			ShutdownTimeoutSeconds: 5,
		},
	}
}

// TEST-LAUNCH-001: Launcher creation
func TestLauncherCreation(t *testing.T) {
	cfg := testConfig()
	log := logging.New(logging.Options{})
	healthSrv := health.NewServer()
	life := lifecycle.New(lifecycle.Options{})

	l := New(Options{
		Config:    cfg,
		Logger:    log,
		Health:    healthSrv,
		Lifecycle: life,
		Addr:      ":0",
	})

	if l == nil {
		t.Fatal("expected non-nil launcher")
	}
	if l.Engine() == nil {
		t.Fatal("expected non-nil engine")
	}
	if l.Gateway() == nil {
		t.Fatal("expected non-nil gateway")
	}
}

// TEST-LAUNCH-002: Engine accessible
func TestLauncherEngine(t *testing.T) {
	cfg := testConfig()
	log := logging.New(logging.Options{})
	healthSrv := health.NewServer()
	life := lifecycle.New(lifecycle.Options{})

	l := New(Options{
		Config:    cfg,
		Logger:    log,
		Health:    healthSrv,
		Lifecycle: life,
		Addr:      ":0",
	})

	if l.Engine().Status() != "CREATED" {
		t.Errorf("expected CREATED, got %s", l.Engine().Status())
	}
}

// TEST-LAUNCH-003: Start and stop
func TestLauncherStartStop(t *testing.T) {
	cfg := testConfig()
	log := logging.New(logging.Options{})
	healthSrv := health.NewServer()
	life := lifecycle.New(lifecycle.Options{})

	l := New(Options{
		Config:    cfg,
		Logger:    log,
		Health:    healthSrv,
		Lifecycle: life,
		Addr:      ":0",
	})

	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if l.Engine().Status() != "RUNNING" {
		t.Errorf("expected RUNNING, got %s", l.Engine().Status())
	}

	if err := l.Stop(ctx); err != nil {
		t.Fatalf("failed to stop: %v", err)
	}

	if l.Engine().Status() != "STOPPED" {
		t.Errorf("expected STOPPED, got %s", l.Engine().Status())
	}
}

// TEST-LAUNCH-004: Submit request through launcher
func TestLauncherSubmitRequest(t *testing.T) {
	now := time.Now()
	cfg := testConfig()
	log := logging.New(logging.Options{})
	healthSrv := health.NewServer()
	life := lifecycle.New(lifecycle.Options{})

	l := New(Options{
		Config:    cfg,
		Logger:    log,
		Health:    healthSrv,
		Lifecycle: life,
		Addr:      ":0",
	})

	// Inject clock for deterministic testing
	_ = core.NewEngine(&cfg, core.WithClock(func() time.Time { return now }))

	ctx := context.Background()
	l.Start(ctx)
	defer l.Stop(ctx)

	req := &core.Request{
		ID:      "test-req",
		Context: core.NewRequestContext("corr-1", "biz-1", "user-1"),
		Intent:  "test intent",
	}

	if err := l.Engine().SubmitRequest(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	result, ok := l.Engine().GetResult("test-req")
	if !ok {
		t.Fatal("expected result")
	}
	if result.Status != "completed" {
		t.Errorf("expected completed, got %s", result.Status)
	}
}

// TEST-LAUNCH-005: Double start prevented
func TestLauncherDoubleStart(t *testing.T) {
	cfg := testConfig()
	log := logging.New(logging.Options{})
	healthSrv := health.NewServer()
	life := lifecycle.New(lifecycle.Options{})

	l := New(Options{
		Config:    cfg,
		Logger:    log,
		Health:    healthSrv,
		Lifecycle: life,
		Addr:      ":0",
	})

	ctx := context.Background()
	l.Start(ctx)
	defer l.Stop(ctx)

	// Second start should fail (engine already running)
	if err := l.Start(ctx); err == nil {
		t.Error("expected error on double start")
	}
}

// TEST-LAUNCH-006: Stop is idempotent
func TestLauncherStopIdempotent(t *testing.T) {
	cfg := testConfig()
	log := logging.New(logging.Options{})
	healthSrv := health.NewServer()
	life := lifecycle.New(lifecycle.Options{})

	l := New(Options{
		Config:    cfg,
		Logger:    log,
		Health:    healthSrv,
		Lifecycle: life,
		Addr:      ":0",
	})

	ctx := context.Background()
	l.Start(ctx)

	if err := l.Stop(ctx); err != nil {
		t.Fatalf("first stop failed: %v", err)
	}
	if err := l.Stop(ctx); err != nil {
		t.Fatalf("second stop failed: %v", err)
	}
}
