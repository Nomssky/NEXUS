package agentexec

// Operational metrics on the agent dispatch path
// (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md §3.4, §11).
//
// The agent runtime counts durable journal write failures on its dispatch path.
// These tests prove the counter is exact, that a failed dispatch-intent write
// still refuses the consequential effect (the fail-closed guarantee is
// unchanged), and that the nil owner changes nothing.

import (
	"context"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/metrics"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// failingStore injects a persistence failure into a write. failFrom lets a test
// let the first N writes through and fail from the (N+1)-th on, so a single
// stage of a two-write sequence can be isolated.
type failingStore struct {
	store.Store
	puts     int
	fail     bool
	failFrom int
}

func (f *failingStore) Put(r *store.Record) error {
	f.puts++
	if f.fail && f.puts >= f.failFrom {
		return &store.StoreError{Code: "IO_ERROR", Message: "injected failure"}
	}
	return f.Store.Put(r)
}

// A successful consequential dispatch writes no failure.
func TestJournalWriteFailureCounterStaysZeroOnSuccess(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	registerWriteTool(t, g)

	fs := &failingStore{Store: store.NewMemStore()}
	g.rt.Journal = execjournal.New(fs, nil)
	m := metrics.New()
	g.rt.Metrics = m

	scope := ToolScope{ActorID: "actor-1", BusinessID: "biz-1",
		AgentTools: []string{"write.tool"}, CorrelationID: "exec-m-ok"}
	if _, err := g.rt.InvokeToolScoped(context.Background(), scope, "agent-1",
		ToolCallSpec{ToolID: "write.tool", Operation: "execute", Input: map[string]string{"text": "hi"}}); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if got := m.Snapshot().JournalWriteFailures; got != 0 {
		t.Fatalf("a clean dispatch must record no write failure, got %d", got)
	}
}

// A failed dispatch-intent write is counted, and the consequential effect is
// still refused: the metric observes the failure, it does not create or excuse it.
func TestFailedDispatchIntentIsCountedAndStillFailsClosed(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	registerWriteTool(t, g)

	fs := &failingStore{Store: store.NewMemStore()}
	g.rt.Journal = execjournal.New(fs, nil)
	m := metrics.New()
	g.rt.Metrics = m
	fs.fail = true

	scope := ToolScope{ActorID: "actor-1", BusinessID: "biz-1",
		AgentTools: []string{"write.tool"}, CorrelationID: "exec-m-fail"}
	_, err := g.rt.InvokeToolScoped(context.Background(), scope, "agent-1",
		ToolCallSpec{ToolID: "write.tool", Operation: "execute", Input: map[string]string{"text": "hi"}})
	if err == nil {
		t.Fatal("a failed dispatch-intent write must refuse the consequential effect")
	}
	if got := m.Snapshot().JournalWriteFailures; got != 1 {
		t.Fatalf("write failures = %d, want 1", got)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("the effect must not have been applied, adapter ran %d times", g.adapter.count())
	}
}

// A failed outcome write is best-effort: it is counted, and the dispatch itself
// still succeeds with its real result. Only the SECOND journal write (the
// outcome; the first is the dispatch intent) fails here, so this isolates the
// best-effort stage rather than re-testing the fail-closed stage above.
func TestFailedOutcomeWriteIsCountedAndDispatchStillSucceeds(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	registerWriteTool(t, g)

	fs := &failingStore{Store: store.NewMemStore()}
	g.rt.Journal = execjournal.New(fs, nil)
	m := metrics.New()
	g.rt.Metrics = m

	scope := ToolScope{ActorID: "actor-1", BusinessID: "biz-1",
		AgentTools: []string{"write.tool"}, CorrelationID: "exec-m-out"}
	// The execution is durably accepted (Put #1) and never reaches a terminal
	// state here: the engine owns that write. Only the invocation's OUTCOME
	// write fails (Put #3); the dispatch-intent write (Put #2) succeeds.
	if err := g.rt.Journal.Accepted(scope.CorrelationID, scope.BusinessID, "", scope.ActorID); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	fs.fail, fs.failFrom = true, 3

	out, err := g.rt.InvokeToolScoped(context.Background(), scope, "agent-1",
		ToolCallSpec{ToolID: "write.tool", Operation: "execute", Input: map[string]string{"text": "hi"}})
	if err != nil {
		t.Fatalf("a failed outcome write is best-effort and must not fail the call: %v", err)
	}
	if out == nil {
		t.Fatal("the call must still return its real result")
	}
	if g.adapter.count() != 1 {
		t.Fatalf("the effect must have been applied exactly once, got %d", g.adapter.count())
	}
	if got := m.Snapshot().JournalWriteFailures; got != 1 {
		t.Fatalf("write failures = %d, want exactly 1 (the outcome write)", got)
	}

	// The intent is still durable, so recovery finds an ambiguous dispatch —
	// the honest, contract-defined consequence of a lost outcome write.
	fs.fail = false
	rep, rerr := execjournal.New(fs, nil).Recover()
	if rerr != nil {
		t.Fatalf("recover: %v", rerr)
	}
	if rep.UnknownInvocations != 1 {
		t.Fatalf("a dispatch with no recorded outcome must be unknown, got %d", rep.UnknownInvocations)
	}
}

// The nil owner must not change the fail-closed behavior of the dispatch path.
func TestNilMetricsOwnerLeavesDispatchPathUnchanged(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	registerWriteTool(t, g)

	fs := &failingStore{Store: store.NewMemStore()}
	g.rt.Journal = execjournal.New(fs, nil)
	if g.rt.Metrics != nil {
		t.Fatal("this test requires no metrics owner")
	}

	fs.fail = true
	scope := ToolScope{ActorID: "actor-1", BusinessID: "biz-1",
		AgentTools: []string{"write.tool"}, CorrelationID: "exec-m-nil"}
	if _, err := g.rt.InvokeToolScoped(context.Background(), scope, "agent-1",
		ToolCallSpec{ToolID: "write.tool", Operation: "execute", Input: map[string]string{"text": "hi"}}); err == nil {
		t.Fatal("the fail-closed guarantee must hold with no metrics owner")
	}
	if g.adapter.count() != 0 {
		t.Fatalf("the effect must not have been applied, adapter ran %d times", g.adapter.count())
	}
}
