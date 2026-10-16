package execjournal

import (
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

func fixedClock() func() time.Time {
	return func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
}

func invocationCount(t *testing.T, j *Journal) int {
	t.Helper()
	recs, err := j.st.List(store.Filter{Type: recordTypeInvocation})
	if err != nil {
		t.Fatalf("list invocations: %v", err)
	}
	return len(recs)
}

// A newly accepted execution is nonterminal.
func TestAcceptedIsNonterminal(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	rec, ok, err := j.Get("exec-1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.State != StateAccepted || rec.State.Terminal() {
		t.Fatalf("state = %q terminal=%v", rec.State, rec.State.Terminal())
	}
}

// A consequential dispatch writes an intent record; an interrupted one (no
// outcome) recovers as unknown and is not replayed.
func TestConsequentialDispatchIntentAndOutcome(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-c", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	invID, err := j.RecordDispatchIntent("exec-c", "call-9", "github.issue.comment", "execute", "write")
	if err != nil {
		t.Fatalf("dispatch intent: %v", err)
	}
	if invID == "" {
		t.Fatal("dispatch intent must return a record id")
	}
	if n := invocationCount(t, j); n != 1 {
		t.Fatalf("expected 1 invocation record, got %d", n)
	}
	// No outcome recorded -> interrupted -> unknown.
	rep, err := j.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if rep.UnknownInvocations != 1 {
		t.Fatalf("expected 1 unknown invocation, got %d", rep.UnknownInvocations)
	}
}

// An outcome clears the ambiguity: a completed dispatch is not counted unknown.
func TestRecordedOutcomeClearsAmbiguity(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-o", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	invID, err := j.RecordDispatchIntent("exec-o", "call-1", "http.request", "post", "mutation")
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	if err := j.RecordOutcome(invID, InvCompleted, ""); err != nil {
		t.Fatalf("outcome: %v", err)
	}
	rep, err := j.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if rep.UnknownInvocations != 0 {
		t.Fatalf("expected 0 unknown invocations, got %d", rep.UnknownInvocations)
	}
}

// An in-flight nonterminal execution is classified unknown; a terminal one is
// left terminal.
func TestRecoverClassifiesInFlightAndPreservesTerminal(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	for _, id := range []string{"exec-inf", "exec-done"} {
		if err := j.Accepted(id, "biz-1", "", "actor-1"); err != nil {
			t.Fatalf("accepted: %v", err)
		}
	}
	if err := j.Terminal("exec-done", StateCompleted, ""); err != nil {
		t.Fatalf("terminal: %v", err)
	}
	rep, err := j.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if rep.Recovered != 1 {
		t.Fatalf("expected 1 recovered (in-flight), got %d", rep.Recovered)
	}
	if rep.Terminal != 1 {
		t.Fatalf("expected 1 terminal preserved, got %d", rep.Terminal)
	}
	// The terminal record is still completed after recovery.
	rec, ok, err := j.Get("exec-done")
	if err != nil || !ok || rec.State != StateCompleted {
		t.Fatalf("terminal record changed: ok=%v err=%v state=%v", ok, err, rec.State)
	}
	// The in-flight record is now unknown with recovery metadata.
	inf, ok, err := j.Get("exec-inf")
	if err != nil || !ok {
		t.Fatalf("get in-flight: ok=%v err=%v", ok, err)
	}
	if inf.State != StateUnknown || inf.Recovery == nil || !inf.Recovery.Recovered {
		t.Fatalf("in-flight not classified: %+v", inf)
	}
}

// A corrupt/unsupported record aborts recovery (fail closed), never reset.
func TestRecoverFailsClosedOnBadRecord(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.st.Put(&store.Record{
		ID: "nexus.execution:bad", Type: recordTypeExecution, Status: store.RecordStatusActive,
		Data: []byte(`{"version":999,"id":"bad","state":"accepted"}`),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := j.Recover(); err == nil {
		t.Fatal("recovery must fail on an unsupported record version")
	}
}

// A nil journal is a safe no-op (dev/in-memory installs).
func TestNilJournalIsNoop(t *testing.T) {
	var j *Journal
	if err := j.Accepted("x", "b", "", "a"); err != nil {
		t.Fatalf("nil accepted: %v", err)
	}
	if id, err := j.RecordDispatchIntent("x", "c", "t", "op", "write"); err != nil || id != "" {
		t.Fatalf("nil intent: id=%q err=%v", id, err)
	}
	if rep, err := j.Recover(); err != nil || rep.Scanned != 0 {
		t.Fatalf("nil recover: rep=%+v err=%v", rep, err)
	}
}

// Durable records survive a restart through a real FileStore.
func TestDurableAcrossFileStoreReopen(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	j := New(st, fixedClock())
	if err := j.Accepted("exec-restart", "biz-9", "div-2", "actor-7"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	if _, err := j.RecordDispatchIntent("exec-restart", "call-1", "http.request", "post", "mutation"); err != nil {
		t.Fatalf("intent: %v", err)
	}

	// "Restart": open a fresh store over the same directory and recover.
	st2, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	j2 := New(st2, fixedClock())
	// The accepted record survived and is nonterminal before recovery.
	rec, ok, err := j2.Get("exec-restart")
	if err != nil || !ok {
		t.Fatalf("record did not survive restart: ok=%v err=%v", ok, err)
	}
	if rec.BusinessID != "biz-9" || rec.DivisionID != "div-2" || rec.ActorID != "actor-7" {
		t.Fatalf("scope not preserved: %+v", rec)
	}
	rep, err := j2.Recover()
	if err != nil {
		t.Fatalf("recover after restart: %v", err)
	}
	if rep.Recovered != 1 || rep.UnknownInvocations != 1 {
		t.Fatalf("restart classification: %+v", rep)
	}
}

// ListByBusiness returns newest-first records for one business, bounded.
func TestListByBusinessOrderingAndBound(t *testing.T) {
	tick := time.Unix(1_700_000_000, 0)
	clk := func() time.Time { tick = tick.Add(time.Second); return tick }
	j := New(store.NewMemStore(), clk)
	for _, id := range []string{"a", "b", "c"} {
		if err := j.Accepted(id, "biz-1", "", "actor"); err != nil {
			t.Fatalf("accepted %s: %v", id, err)
		}
	}
	if err := j.Accepted("z-other", "biz-other", "", "actor"); err != nil {
		t.Fatalf("accepted other: %v", err)
	}
	recs, err := j.ListByBusiness("biz-1", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("expected 3 in biz-1 (business filter), got %d", len(recs))
	}
	if recs[0].ID != "c" || recs[1].ID != "b" || recs[2].ID != "a" {
		t.Fatalf("ordering wrong (want newest first): %v", []string{recs[0].ID, recs[1].ID, recs[2].ID})
	}
	bounded, err := j.ListByBusiness("biz-1", 2)
	if err != nil || len(bounded) != 2 || bounded[0].ID != "c" {
		t.Fatalf("bound wrong: %v err=%v", bounded, err)
	}
}

// ListByBusiness fails closed on a corrupt record; it never skips it silently.
func TestListByBusinessFailsClosedOnCorrupt(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("good", "biz-1", "", "actor"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	if err := j.st.Put(&store.Record{
		ID: "nexus.execution:bad", Type: recordTypeExecution, Status: store.RecordStatusActive,
		BusinessID: "biz-1", Data: []byte(`{"version":999,"id":"bad","state":"accepted"}`),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := j.ListByBusiness("biz-1", 0); err == nil {
		t.Fatal("list must fail closed on an unsupported record version")
	}
}

// Terminal is monotonic: the first terminal state is final and a later terminal
// write never rewrites it. This is what keeps a durable record consistent with
// the FIRST committed outcome even when a later process-local resume moves the
// in-memory result on (§2 "terminal is final").
func TestTerminalIsMonotonic(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-mono", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	if err := j.Terminal("exec-mono", StateFailed, "APPROVAL_REQUIRED"); err != nil {
		t.Fatalf("first terminal: %v", err)
	}
	if err := j.Terminal("exec-mono", StateCompleted, ""); err != nil {
		t.Fatalf("second terminal must be a no-op, not an error: %v", err)
	}
	rec, ok, err := j.Get("exec-mono")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.State != StateFailed || rec.ErrorClass != "APPROVAL_REQUIRED" {
		t.Fatalf("the durable terminal was rewritten: %+v", rec)
	}
}

// A non-terminal state is refused: the journal never records a fabricated
// terminal, and an unknown state must be assigned only by recovery.
func TestTerminalRejectsNonTerminalState(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-nt", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	for _, s := range []State{StateAccepted, StateRunning, StatePending, StateUnknown} {
		if err := j.Terminal("exec-nt", s, ""); err == nil {
			t.Fatalf("state %q must be refused as a terminal transition", s)
		}
	}
	rec, ok, _ := j.Get("exec-nt")
	if !ok || rec.State != StateAccepted {
		t.Fatalf("a refused terminal write must leave the record nonterminal: %+v", rec)
	}
}

// failingStore injects a persistence failure into the terminal write.
type failingStore struct {
	store.Store
	fail bool
}

func (f *failingStore) Put(r *store.Record) error {
	if f.fail {
		return &store.StoreError{Code: "IO_ERROR", Message: "injected failure"}
	}
	return f.Store.Put(r)
}

// A terminal write whose store fails surfaces the error and NEVER fabricates a
// terminal: the record stays nonterminal, so recovery classifies it unknown
// (v11 §8 — a failed terminal write is not a completion).
func TestTerminalPersistenceFailureLeavesRecordNonterminal(t *testing.T) {
	fs := &failingStore{Store: store.NewMemStore()}
	j := New(fs, fixedClock())
	if err := j.Accepted("exec-fail", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	fs.fail = true
	if err := j.Terminal("exec-fail", StateCompleted, ""); err == nil {
		t.Fatal("a failed terminal write must surface the store error")
	}
	fs.fail = false
	rec, ok, err := j.Get("exec-fail")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.State.Terminal() {
		t.Fatalf("a failed terminal write must not leave a terminal record: %+v", rec)
	}
}
