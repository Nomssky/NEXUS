package launcher

import (
	"context"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

func testLauncherOpts(t *testing.T, st store.Store) Options {
	t.Helper()
	return Options{
		Config:    config.Config{Nexus: config.NexusConfig{Environment: "development"}},
		Logger:    logging.New(logging.Options{}),
		Health:    health.NewServer(),
		Lifecycle: lifecycle.New(lifecycle.Options{}),
		Addr:      ":0",
		Store:     st,
	}
}

// Startup recovery classifies an interrupted (nonterminal) execution as unknown
// before the gateway accepts new work.
func TestLauncherRecoveryClassifiesInterrupted(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	// An interrupted execution: accepted, never terminal.
	if err := execjournal.New(st, nil).Accepted("exec-interrupted", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_ = New(testLauncherOpts(t, st)) // New runs the recovery pass

	rec, ok, err := execjournal.New(st, nil).Get("exec-interrupted")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.State != execjournal.StateUnknown {
		t.Fatalf("interrupted execution must be classified unknown, got %q", rec.State)
	}
	if rec.Recovery == nil || !rec.Recovery.Recovered {
		t.Fatalf("recovery metadata missing: %+v", rec)
	}
}

// A durably-terminal execution is left terminal by recovery.
func TestLauncherRecoveryPreservesTerminal(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	if err := j.Accepted("exec-done", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := j.Terminal("exec-done", execjournal.StateCompleted, ""); err != nil {
		t.Fatalf("terminal: %v", err)
	}

	_ = New(testLauncherOpts(t, st))

	rec, ok, err := execjournal.New(st, nil).Get("exec-done")
	if err != nil || !ok || rec.State != execjournal.StateCompleted {
		t.Fatalf("terminal execution changed: ok=%v err=%v state=%v", ok, err, rec.State)
	}
}

// A corrupt durable execution record aborts startup (fail closed).
func TestLauncherRecoveryFailsClosedOnCorrupt(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	// A record with an unsupported version (contradictory/unknown schema).
	if err := st.Put(&store.Record{
		ID: "nexus.execution:bad", Type: store.RecordType("execution"), Status: store.RecordStatusActive,
		Data: []byte(`{"version":999,"id":"bad","state":"accepted"}`),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	l := New(testLauncherOpts(t, st))
	err = l.Start(context.Background())
	if err == nil {
		t.Fatal("startup must fail when durable execution state is corrupt")
	}
	if !strings.Contains(err.Error(), "durable execution recovery failed") {
		t.Fatalf("expected a recovery error, got %q", err.Error())
	}
}
