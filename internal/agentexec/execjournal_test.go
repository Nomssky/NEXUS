package agentexec

import (
	"context"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// registerWriteTool registers a consequential (write-class) tool on the
// fixture's registry with a counting adapter.
func registerWriteTool(t *testing.T, g *governedRuntime) {
	t.Helper()
	m := tool.ToolManifest{
		ID: "write.tool", Version: "1.0.0", Name: "write.tool", Description: "consequential test tool",
		Category:        tool.ToolCategoryNetwork,
		InputSchema:     tool.Schema{Fields: []tool.SchemaField{{Name: "text", Type: tool.FieldString, MaxLength: 32}}},
		OutputSchema:    tool.Schema{Fields: []tool.SchemaField{{Name: "output", Type: tool.FieldString, MaxLength: 64}}},
		SideEffectClass: tool.SideEffectWrite, NetworkRequirement: tool.NetworkNone,
		ScopeRequirement: tool.ScopeBusiness, SecurityClass: tool.SecuritySandboxed,
		Operations: []string{"execute"},
	}
	if err := g.rt.Tools.Register(m, g.adapter); err != nil {
		t.Fatalf("register write tool: %v", err)
	}
}

// A consequential invocation writes a durable dispatch intent BEFORE the effect
// and an outcome AFTER, on the canonical platform path.
func TestConsequentialDispatchIsJournaled(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	registerWriteTool(t, g)

	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	g.rt.Journal = j

	scope := ToolScope{ActorID: "actor-1", BusinessID: "biz-1", AgentTools: []string{"write.tool"}, CorrelationID: "exec-j1"}
	out, err := g.rt.InvokeToolScoped(context.Background(), scope, "agent-1",
		ToolCallSpec{ToolID: "write.tool", Operation: "execute", Input: map[string]string{"text": "hi"}})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	_ = out
	if g.adapter.count() != 1 {
		t.Fatalf("adapter must have run once, got %d", g.adapter.count())
	}

	// The durable journal has a completed invocation for the execution.
	recs, err := st.List(store.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var invFound bool
	for _, r := range recs {
		if r.Type == "invocation" {
			invFound = true
		}
	}
	if !invFound {
		t.Fatal("expected a durable invocation record for the consequential dispatch")
	}
	// Nothing is left ambiguous: recovery finds nothing unknown.
	rep, err := j.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if rep.UnknownInvocations != 0 {
		t.Fatalf("a completed dispatch must not be unknown, got %d", rep.UnknownInvocations)
	}
}

// A read-class invocation writes no dispatch record (reads are idempotent).
func TestReadClassIsNotJournaled(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	g.rt.Journal = execjournal.New(st, nil)

	scope := ToolScope{ActorID: "actor-1", BusinessID: "biz-1", AgentTools: []string{"echo.tool"}, CorrelationID: "exec-j2"}
	if _, err := g.rt.InvokeToolScoped(context.Background(), scope, "agent-1", call()); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	recs, _ := st.List(store.Filter{})
	for _, r := range recs {
		if r.Type == "invocation" {
			t.Fatal("a read-class invocation must not create a durable dispatch record")
		}
	}
}

// An interrupted consequential dispatch (intent recorded, no outcome) recovers
// as unknown and is never replayed by recovery.
func TestInterruptedDispatchRecoversUnknownNotReplayed(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	registerWriteTool(t, g)

	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	g.rt.Journal = j

	// Simulate a crash mid-effect: an intent record with no outcome.
	if _, err := j.RecordDispatchIntent("exec-j3", "call-1", "write.tool", "execute", "write"); err != nil {
		t.Fatalf("intent: %v", err)
	}
	if err := j.Accepted("exec-j3", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}

	// "Restart": reopen the store and recover.
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	j2 := execjournal.New(st2, nil)
	rep, err := j2.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if rep.UnknownInvocations != 1 {
		t.Fatalf("expected the interrupted dispatch to be unknown, got %+v", rep)
	}
	// Recovery never touches the adapter.
	if g.adapter.count() != 0 {
		t.Fatalf("recovery must not re-dispatch the ambiguous effect (adapter ran %d)", g.adapter.count())
	}
	// The execution is classified unknown.
	rec, ok, err := j2.Get("exec-j3")
	if err != nil || !ok || rec.State != execjournal.StateUnknown {
		t.Fatalf("interrupted execution not classified unknown: ok=%v err=%v state=%v", ok, err, rec.State)
	}
}
