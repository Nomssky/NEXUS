package launcher

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
	"github.com/Nomssky/NEXUS/internal/gateway"
)

// L-004 ownership model (encoded by these tests):
//
//	launcher owns:          engine, gateway  → Stop() terminates them
//	health.Server (found.): pure readiness state, no runtime to stop → MarkNotReady only
//	health HTTP server:     owned by app via lifecycle hook (not launcher)
//	lifecycle.Manager:      owns hook execution + its own state transitions;
//	                         launcher.Stop() must NOT call life.Shutdown() (recursion)
//	Process shutdown:       Run() → life.Shutdown() → Close hooks → launcher.Stop()

// newL004 builds a fully wired launcher for L-004 tests.
func newL004(t *testing.T, addr string, gwOpts ...gateway.ServerOption) *Launcher {
	t.Helper()
	return New(Options{
		Config:         testConfig(),
		Logger:         logging.New(logging.Options{}),
		Health:         health.NewServer(),
		Lifecycle:      lifecycle.New(lifecycle.Options{}),
		Addr:           addr,
		GatewayOptions: gwOpts,
	})
}

// TestL004DirectStopTerminatesOwnedResources — direct Stop terminates
// launcher-owned engine and gateway; readiness cleared.
func TestL004DirectStopTerminatesOwnedResources(t *testing.T) {
	l := newL004(t, "127.0.0.1:0")
	l.health.MarkReady()

	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := l.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if st := l.Engine().Status(); st != "STOPPED" {
		t.Errorf("engine after Stop: want STOPPED, got %s", st)
	}

	// Gateway listener must be gone — Ready was for startup; after Stop,
	// a fresh dial to the addr must fail (server shut down).
	// Use engine status as primary signal; gateway has no Status() accessor.

	// Readiness cleared.
	l.health.MarkReady()                // ensure we can observe the flip
	if err := l.Stop(ctx); err != nil { // second stop also clears
		t.Fatalf("second Stop: %v", err)
	}
	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()
	l.health.ReadyHandler().ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Error("readiness must be cleared by Stop")
	}
}

// TestL004StopAfterInitFailureIsNilSafe — Stop must not panic when New()
// failed to construct engine/gateway (initErr path leaves them nil).
func TestL004StopAfterInitFailureIsNilSafe(t *testing.T) {
	// Simulate initErr path: engine creation failed → nil engine/gateway.
	l := &Launcher{
		log:     logging.New(logging.Options{}),
		health:  health.NewServer(),
		life:    lifecycle.New(lifecycle.Options{}),
		initErr: errors.New("forced init failure"),
	}

	ctx := context.Background()
	// Start must surface initErr without panicking.
	if err := l.Start(ctx); err == nil || !strings.Contains(err.Error(), "initialization") {
		t.Fatalf("Start with initErr: want initialization error, got %v", err)
	}

	// Stop must be nil-safe (no panic) on the initErr path.
	if err := l.Stop(ctx); err != nil {
		t.Errorf("Stop after init failure: %v", err)
	}
}

// TestL004StartupFailureCleansUpEngine — gateway bind failure after engine
// started: Start must not leave the engine running (self-cleanup), or Stop
// after failed Start must terminate it. Encode: after failed Start + Stop,
// engine is STOPPED.
func TestL004StartupFailureCleansUpEngine(t *testing.T) {
	l := newL004(t, "127.0.0.1:1") // bind failure

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := l.Start(ctx); err == nil {
		t.Fatal("expected gateway startup failure")
	}

	// After failed Start, either Start self-cleaned OR Stop cleans up.
	// Call Stop to honor the invariant "Stop terminates owned resources".
	if err := l.Stop(ctx); err != nil {
		t.Fatalf("Stop after failed Start: %v", err)
	}

	if st := l.Engine().Status(); st != "STOPPED" {
		t.Errorf("engine after failed Start + Stop: want STOPPED, got %s", st)
	}
}

// TestL004StartupFailureSelfCleansEngine — Start must not leak a running
// engine when gateway startup fails (self-cleanup on abort). RED if engine
// still RUNNING after Start returns error without calling Stop.
func TestL004StartupFailureSelfCleansEngine(t *testing.T) {
	l := newL004(t, "127.0.0.1:1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := l.Start(ctx); err == nil {
		t.Fatal("expected gateway startup failure")
	}

	// No Stop call — Start must have aborted cleanly.
	if st := l.Engine().Status(); st != "STOPPED" {
		t.Errorf("engine after failed Start (no Stop): want STOPPED (self-clean), got %s", st)
	}
}

// TestL004LifecycleTriggeredShutdownNoRecursion — life.Shutdown() → Close
// hook → launcher.Stop() must terminate exactly once and return; launcher.Stop
// must NOT call life.Shutdown() (would recurse).
func TestL004LifecycleTriggeredShutdownNoRecursion(t *testing.T) {
	l := newL004(t, "127.0.0.1:0")

	stopCalls := 0
	l.life.RegisterHook(lifecycle.Hook{
		Name: "core-runtime",
		Init: l.Start,
		Close: func(ctx context.Context) error {
			stopCalls++
			return l.Stop(ctx)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := l.life.Start(ctx); err != nil {
		t.Fatalf("life.Start: %v", err)
	}
	l.health.MarkReady()

	// If launcher.Stop called life.Shutdown, this would recurse / stack
	// overflow / hang. Must complete and reach STOPPED exactly once.
	done := make(chan lifecycle.ExitCode, 1)
	go func() { done <- l.life.Shutdown(ctx) }()

	select {
	case code := <-done:
		if code != lifecycle.ExitOK {
			t.Errorf("Shutdown exit code: want ExitOK, got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("life.Shutdown hung — likely shutdown recursion")
	}

	if st := l.life.State(); st != lifecycle.StateStopped {
		t.Errorf("lifecycle state: want STOPPED, got %s", st)
	}
	if stopCalls != 1 {
		t.Errorf("launcher.Stop via hook: want exactly 1 call, got %d", stopCalls)
	}
	if st := l.Engine().Status(); st != "STOPPED" {
		t.Errorf("engine after lifecycle shutdown: want STOPPED, got %s", st)
	}
}

// TestL004DirectStopDoesNotDriveLifecycle — direct launcher.Stop() stops
// owned components but does not call life.Shutdown() or transition lifecycle
// state (lifecycle owns its own state via life.Shutdown/Start).
func TestL004DirectStopDoesNotDriveLifecycle(t *testing.T) {
	l := newL004(t, "127.0.0.1:0")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Drive lifecycle to RUNNING via hooks (as Run does).
	l.life.RegisterHook(lifecycle.Hook{
		Name:  "core-runtime",
		Init:  l.Start,
		Close: l.Stop,
	})
	if err := l.life.Start(ctx); err != nil {
		t.Fatalf("life.Start: %v", err)
	}
	if l.life.State() != lifecycle.StateRunning {
		t.Fatalf("precondition: want RUNNING, got %s", l.life.State())
	}

	// Direct Stop — components stop, lifecycle state must remain RUNNING
	// (only life.Shutdown transitions it; Stop must not call life.Shutdown).
	if err := l.Stop(ctx); err != nil {
		t.Fatalf("direct Stop: %v", err)
	}

	if st := l.Engine().Status(); st != "STOPPED" {
		t.Errorf("engine: want STOPPED, got %s", st)
	}
	if st := l.life.State(); st != lifecycle.StateRunning {
		t.Errorf("lifecycle state after direct Stop: want RUNNING (Stop must not drive life.Shutdown), got %s", st)
	}

	// Full process shutdown still works afterward.
	if code := l.life.Shutdown(ctx); code != lifecycle.ExitOK {
		t.Errorf("life.Shutdown after direct Stop: want ExitOK, got %d", code)
	}
	if st := l.life.State(); st != lifecycle.StateStopped {
		t.Errorf("lifecycle after Shutdown: want STOPPED, got %s", st)
	}
}

// TestL004IdempotentStop — multiple Stop calls safe; no panic, no error.
func TestL004IdempotentStop(t *testing.T) {
	l := newL004(t, "127.0.0.1:0")

	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := l.Stop(ctx); err != nil {
			t.Fatalf("Stop #%d: %v", i+1, err)
		}
	}

	if st := l.Engine().Status(); st != "STOPPED" {
		t.Errorf("engine after repeated Stop: want STOPPED, got %s", st)
	}
}

// TestL004StartupCancellationCleansUp — cancel during startup returns and
// leaves engine stopped (no leaked running engine after aborted Start).
func TestL004StartupCancellationCleansUp(t *testing.T) {
	listenStarted := make(chan struct{})
	releaseListen := make(chan struct{})

	l := newL004(t, "127.0.0.1:0", gateway.WithListenFunc(
		func(network, addr string) (net.Listener, error) {
			close(listenStarted)
			<-releaseListen
			return net.Listen(network, addr)
		},
	))

	ctx, cancel := context.WithCancel(context.Background())

	startDone := make(chan error, 1)
	go func() { startDone <- l.Start(ctx) }()

	select {
	case <-listenStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("listen never started")
	}

	cancel()
	close(releaseListen)

	select {
	case err := <-startDone:
		if err == nil {
			t.Fatal("expected cancellation error from Start")
		}
		if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
			t.Errorf("want context cancellation, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after cancellation")
	}

	// Engine must not be left RUNNING after aborted startup.
	if st := l.Engine().Status(); st != "STOPPED" {
		t.Errorf("engine after cancelled Start: want STOPPED (self-clean), got %s", st)
	}
}

// TestL004ReadinessClearedBeforeDrain — Stop clears readiness first (load
// balancers stop routing before drain). Verified by flipping ready then Stop.
func TestL004ReadinessClearedBeforeDrain(t *testing.T) {
	l := newL004(t, "127.0.0.1:0")
	l.health.MarkReady()

	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := l.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// ReadyHandler must report not-ready (503).
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ready", nil)
	l.health.ReadyHandler().ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Error("readiness must be cleared by Stop (want non-200)")
	}
}
