package launcher

// Operational metrics for startup recovery
// (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md §3.5, §6.2, §11).
//
// Recovery counters must reflect exactly what the recovery pass classified —
// nothing more, nothing less, and never re-counting work a previous pass
// already did. Reading metrics must not mutate durable evidence.

import (
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/metrics"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// snapshotStore returns the raw serialized bytes of every record, keyed by id.
func snapshotStore(t *testing.T, st store.Store) map[string]string {
	t.Helper()
	out := map[string]string{}
	recs, err := st.List(store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		out[r.ID] = string(r.Data)
	}
	return out
}

// Obligation 8: the counters match the records recovery actually classified.
func TestRecoveryMetricsReflectClassifiedRecords(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)

	// Two already-terminal executions and two interrupted ones, one of which
	// has a dispatched (ambiguous) invocation.
	for _, id := range []string{"exec-done-1", "exec-done-2"} {
		if err := j.Accepted(id, "biz-1", "", "actor-1"); err != nil {
			t.Fatal(err)
		}
		if err := j.Terminal(id, execjournal.StateCompleted, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"exec-cut-1", "exec-cut-2"} {
		if err := j.Accepted(id, "biz-1", "", "actor-1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := j.RecordDispatchIntent("exec-cut-1", "exec-cut-1", "tool.a", "execute", "consequential"); err != nil {
		t.Fatalf("seed dispatch intent: %v", err)
	}

	before := time.Now()
	l := New(testLauncherOpts(t, st)) // New runs the recovery pass
	after := time.Now()

	if l.initErr != nil {
		t.Fatalf("launcher must build cleanly, got %v", l.initErr)
	}
	snap := l.Metrics().Snapshot()

	// Scanned = every execution record present (4).
	if snap.RecoveryExecutionsScanned != 4 {
		t.Fatalf("scanned = %d, want 4", snap.RecoveryExecutionsScanned)
	}
	// Terminal = the two durably-terminal ones, left untouched.
	if snap.RecoveryTerminal != 2 {
		t.Fatalf("terminal = %d, want 2", snap.RecoveryTerminal)
	}
	// Classified unknown = the two interrupted ones.
	if snap.RecoveryClassifiedUnknown != 2 {
		t.Fatalf("classified unknown = %d, want 2", snap.RecoveryClassifiedUnknown)
	}
	// The dispatched invocation of an interrupted execution is ambiguous.
	if snap.RecoveryUnknownInvocations != 1 {
		t.Fatalf("unknown invocations = %d, want 1", snap.RecoveryUnknownInvocations)
	}
	if snap.RecoveryFailures != 0 {
		t.Fatalf("recovery failures = %d, want 0", snap.RecoveryFailures)
	}

	// The timestamp is bounded by the launch and is a real UTC second.
	got := time.Unix(snap.LastRecoveryUnixSeconds, 0)
	if got.Before(before.Add(-2*time.Second)) || got.After(after.Add(2*time.Second)) {
		t.Fatalf("recovery timestamp %s is outside the launch window %s..%s",
			got, before, after)
	}
}

// Re-running recovery must not re-count work a previous pass already classified:
// the records are already `unknown`, so the second pass classifies nothing new.
func TestRecoveryMetricsAreNotDoubleCountedAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	if err := j.Accepted("exec-once", "biz-1", "", "actor-1"); err != nil {
		t.Fatal(err)
	}

	first := New(testLauncherOpts(t, st)).Metrics().Snapshot()
	if first.RecoveryClassifiedUnknown != 1 {
		t.Fatalf("first pass classified unknown = %d, want 1", first.RecoveryClassifiedUnknown)
	}

	second := New(testLauncherOpts(t, st)).Metrics().Snapshot()
	if second.RecoveryClassifiedUnknown != 0 {
		t.Fatalf("second pass must classify nothing new, got %d", second.RecoveryClassifiedUnknown)
	}
	// The record is still scanned (it exists) and still not terminal.
	if second.RecoveryExecutionsScanned != 1 {
		t.Fatalf("second pass scanned = %d, want 1", second.RecoveryExecutionsScanned)
	}
	if second.RecoveryTerminal != 0 {
		t.Fatalf("an unknown record is not terminal, got %d", second.RecoveryTerminal)
	}
}

// A fail-closed recovery aborts startup and counts the failure.
func TestRecoveryFailureIsCountedAndFailsClosed(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	// A corrupt execution record: recovery must refuse to serve it.
	rec := []byte(`{"version":1,"id":"exec-bad","state":`)
	if err := st.Put(&store.Record{
		ID: "nexus.execution:exec-bad", Type: "execution",
		Status: store.RecordStatusActive, BusinessID: "biz-1", Data: rec,
	}); err != nil {
		t.Fatal(err)
	}

	l := New(testLauncherOpts(t, st))
	if l.initErr == nil {
		t.Fatal("a corrupt record must fail startup closed")
	}
	snap := l.Metrics().Snapshot()
	if snap.RecoveryFailures != 1 {
		t.Fatalf("recovery failures = %d, want 1", snap.RecoveryFailures)
	}
	// A failed pass establishes no successful recovery timestamp.
	if snap.LastRecoveryUnixSeconds != 0 {
		t.Fatalf("a failed pass must not set a recovery timestamp, got %d",
			snap.LastRecoveryUnixSeconds)
	}
	// And it must not claim it classified anything.
	if snap.RecoveryClassifiedUnknown != 0 || snap.RecoveryExecutionsScanned != 0 {
		t.Fatalf("a failed pass must report no counts: %+v", snap)
	}
}

// Obligation 9: collecting metrics does not mutate journal records.
func TestMetricsCollectionDoesNotMutateJournalRecords(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	if err := j.Accepted("exec-mut", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := j.RecordDispatchIntent("exec-mut", "exec-mut", "tool.a", "execute", "consequential"); err != nil {
		t.Fatal(err)
	}
	if err := j.Terminal("exec-mut", execjournal.StateCompleted, ""); err != nil {
		t.Fatal(err)
	}

	l := New(testLauncherOpts(t, st))
	m := l.Metrics()
	before := snapshotStore(t, st)

	for i := 0; i < 25; i++ {
		_ = m.Snapshot()
	}
	// Reading through the engine's view must be equally non-mutating.
	for i := 0; i < 25; i++ {
		_ = l.engine.MetricsSnapshot()
	}

	after := snapshotStore(t, st)
	if len(before) != len(after) {
		t.Fatalf("record count changed: %d -> %d", len(before), len(after))
	}
	for id, want := range before {
		got, ok := after[id]
		if !ok {
			t.Fatalf("record %s disappeared while collecting metrics", id)
		}
		if got != want {
			t.Fatalf("record %s was mutated by a metrics read:\n before=%s\n after =%s", id, want, got)
		}
	}
}

// A launcher without a durable store still exposes a usable metrics owner.
func TestLauncherWithoutStoreExposesZeroedMetrics(t *testing.T) {
	l := New(testLauncherOpts(t, nil))
	if l.initErr != nil {
		t.Fatalf("a store-less launcher must build, got %v", l.initErr)
	}
	snap := l.Metrics().Snapshot()
	if snap.RecoveryExecutionsScanned != 0 || snap.LastRecoveryUnixSeconds != 0 {
		t.Fatalf("no durable store means no recovery evidence: %+v", snap)
	}
	if snap.RecoveryFailures != 0 {
		t.Fatalf("no store must not be reported as a recovery failure: %+v", snap)
	}
}

// A nil *Launcher is safe to read metrics from.
func TestNilLauncherMetricsIsSafe(t *testing.T) {
	var l *Launcher
	if l.Metrics() != nil {
		t.Fatal("a nil launcher must report no metrics owner")
	}
	if got := l.Metrics().Snapshot(); got != (metrics.Snapshot{}) {
		t.Fatalf("a nil owner must snapshot as all zero, got %+v", got)
	}
}
