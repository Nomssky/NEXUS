package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TEST-M0-001 (lifecycle part): start transitions CREATED -> RUNNING with init
// hooks run.
func TestStartRunsInitHooks(t *testing.T) {
	m := New(Options{ShutdownTimeout: time.Second})
	initRan := false
	m.RegisterHook(Hook{Name: "a", Init: func(context.Context) error {
		initRan = true
		return nil
	}})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !initRan {
		t.Fatal("init hook did not run")
	}
	if m.State() != StateRunning {
		t.Fatalf("expected RUNNING, got %s", m.State())
	}
}

// Init hook failure moves to FAILED.
func TestStartFailure(t *testing.T) {
	m := New(Options{})
	m.RegisterHook(Hook{Name: "bad", Init: func(context.Context) error {
		return errors.New("boom")
	}})
	if err := m.Start(context.Background()); err == nil {
		t.Fatal("expected start error")
	}
	if m.State() != StateFailed {
		t.Fatalf("expected FAILED, got %s", m.State())
	}
}

// TEST-M0-008: graceful shutdown runs close hooks in reverse order and reaches
// STOPPED.
func TestGracefulShutdownReverseOrder(t *testing.T) {
	m := New(Options{ShutdownTimeout: time.Second})
	var order []string
	m.RegisterHook(Hook{Name: "first", Close: func(context.Context) error {
		order = append(order, "first")
		return nil
	}})
	m.RegisterHook(Hook{Name: "second", Close: func(context.Context) error {
		order = append(order, "second")
		return nil
	}})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if code := m.Shutdown(context.Background()); code != ExitOK {
		t.Fatalf("expected ExitOK, got %d", code)
	}
	if m.State() != StateStopped {
		t.Fatalf("expected STOPPED, got %s", m.State())
	}
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("close hooks must run in reverse order, got %v", order)
	}
}

// A failing close hook yields a failure exit code but still reaches STOPPED.
func TestShutdownHookFailureExitCode(t *testing.T) {
	m := New(Options{ShutdownTimeout: time.Second})
	m.RegisterHook(Hook{Name: "bad", Close: func(context.Context) error {
		return errors.New("close failed")
	}})
	_ = m.Start(context.Background())
	if code := m.Shutdown(context.Background()); code != ExitFailure {
		t.Fatalf("expected ExitFailure, got %d", code)
	}
	if m.State() != StateStopped {
		t.Fatalf("expected STOPPED, got %s", m.State())
	}
}

// TEST-M0-010: shutdown is idempotent and clean.
func TestShutdownIdempotent(t *testing.T) {
	m := New(Options{ShutdownTimeout: time.Second})
	_ = m.Start(context.Background())
	if code := m.Shutdown(context.Background()); code != ExitOK {
		t.Fatalf("first shutdown should be ExitOK, got %d", code)
	}
	if code := m.Shutdown(context.Background()); code != ExitOK {
		t.Fatalf("second shutdown should be ExitOK, got %d", code)
	}
}

// TEST-M0-009: the shutdown signal path drives a graceful stop.
func TestWaitForShutdownContextCancel(t *testing.T) {
	m := New(Options{ShutdownTimeout: time.Second})
	_ = m.Start(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if code := m.WaitForShutdownSignal(ctx); code != ExitOK {
		t.Fatalf("expected ExitOK via context cancellation, got %d", code)
	}
	if m.State() != StateStopped {
		t.Fatalf("expected STOPPED, got %s", m.State())
	}
}

// State observers are notified on transitions.
func TestStateObservers(t *testing.T) {
	m := New(Options{})
	var seen []State
	m.OnStateChange(func(_, to State) { seen = append(seen, to) })
	_ = m.Start(context.Background())
	_ = m.Shutdown(context.Background())
	if len(seen) == 0 {
		t.Fatal("expected state observer notifications")
	}
}
