package launcher

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
	"github.com/Nomssky/NEXUS/internal/gateway"
)

// L-002 invariant:
//   launcher.Start() must not report successful startup until the gateway has
//   either established the required ready state (HTTP listener bound) or a
//   definitive startup failure has been observed. A gateway startup failure
//   must not be silently lost because it happened outside an arbitrary timing
//   window.

// newL002 builds a launcher with optional gateway options for L-002 tests.
func newL002(t *testing.T, addr string, gwOpts ...gateway.ServerOption) *Launcher {
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

// TestL002ImmediateGatewayStartupFailure — bind failure propagates to Start.
func TestL002ImmediateGatewayStartupFailure(t *testing.T) {
	// Port 1 requires privileges — deterministic bind failure.
	l := newL002(t, "127.0.0.1:1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := l.Start(ctx)
	if err == nil {
		t.Fatal("expected gateway startup error, got nil (false success)")
	}
	if !strings.Contains(err.Error(), "gateway") {
		t.Errorf("error should mention gateway, got: %v", err)
	}
}

// TestL002DelayedGatewayStartupFailurePropagates — deterministic delayed
// failure via channel barrier: listen blocks until released, then fails.
// Proves old ~100ms window could report success / miss the failure; the fix
// must wait for the definitive outcome (ready or error), not a timer.
func TestL002DelayedGatewayStartupFailurePropagates(t *testing.T) {
	listenStarted := make(chan struct{})
	releaseListen := make(chan struct{})

	l := newL002(t, "127.0.0.1:0", gateway.WithListenFunc(
		func(network, addr string) (net.Listener, error) {
			close(listenStarted)
			<-releaseListen // block until test releases — delayed failure
			return nil, errors.New("delayed listen failure")
		},
	))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startDone := make(chan error, 1)
	go func() { startDone <- l.Start(ctx) }()

	// Wait until gateway listen has been entered (channel barrier, not sleep).
	select {
	case <-listenStarted:
	case err := <-startDone:
		t.Fatalf("Start returned before listen began: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("listen never started")
	}

	// Old implementation returned nil after ~100ms while listen was still
	// blocked (false success). New implementation must still be waiting.
	select {
	case err := <-startDone:
		if err == nil {
			t.Fatal("L-002: Start reported success while gateway listen was still blocked (timing-window bug)")
		}
		t.Fatalf("Start returned error before listen was released: %v", err)
	case <-time.After(200 * time.Millisecond):
		// Still waiting after >100ms — no false success. Deterministic:
		// listen is blocked on releaseListen, ready not closed, gwErr empty.
	}

	// Release listen → definitive failure.
	close(releaseListen)

	select {
	case err := <-startDone:
		if err == nil {
			t.Fatal("expected delayed gateway startup failure to propagate, got nil")
		}
		if !strings.Contains(err.Error(), "delayed listen failure") &&
			!strings.Contains(err.Error(), "gateway") {
			t.Errorf("error should propagate listen failure, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after delayed listen failure")
	}
}

// TestL002SuccessfulStartupReachesReady — gateway reaches ready; Start reports
// success only after listener is bound.
func TestL002SuccessfulStartupReachesReady(t *testing.T) {
	l := newL002(t, "127.0.0.1:0")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startDone := make(chan error, 1)
	go func() { startDone <- l.Start(ctx) }()

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return")
	}

	// Ready must be closed — listener established before Start returned nil.
	select {
	case <-l.Gateway().Ready():
	default:
		t.Fatal("gateway Ready not closed after successful Start")
	}

	if err := l.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// TestL002StartupCancellationTerminatesCleanly — cancellation during startup
// returns promptly without hanging.
func TestL002StartupCancellationTerminatesCleanly(t *testing.T) {
	listenStarted := make(chan struct{})
	releaseListen := make(chan struct{})

	l := newL002(t, "127.0.0.1:0", gateway.WithListenFunc(
		func(network, addr string) (net.Listener, error) {
			close(listenStarted)
			<-releaseListen
			return net.Listen(network, addr)
		},
	))

	ctx, cancel := context.WithCancel(context.Background())

	startDone := make(chan error, 1)
	go func() { startDone <- l.Start(ctx) }()

	// Wait until listen entered.
	select {
	case <-listenStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("listen never started")
	}

	// Cancel during startup (listen still blocked).
	cancel()

	// Release listen so gateway Start can proceed/exit after cancellation.
	close(releaseListen)

	select {
	case err := <-startDone:
		// Must return promptly (error or nil) — not hang.
		_ = err
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after cancellation")
	}
}

// TestL002StartupFailureCleansUpResources — on startup failure, Stop still
// cleans owned resources (engine) correctly.
func TestL002StartupFailureCleansUpResources(t *testing.T) {
	l := newL002(t, "127.0.0.1:1") // bind failure

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := l.Start(ctx)
	if err == nil {
		t.Fatal("expected startup failure")
	}

	// Stop still works and cleans engine (idempotent shutdown path).
	if stopErr := l.Stop(ctx); stopErr != nil {
		t.Errorf("Stop after failed Start: %v", stopErr)
	}

	if st := l.Engine().Status(); st != "STOPPED" {
		t.Errorf("engine after Stop: want STOPPED, got %s", st)
	}
}
