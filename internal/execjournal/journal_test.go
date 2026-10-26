package execjournal

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

type evidenceWriteFailStore struct {
	store.Store
	marker string
}

func (s evidenceWriteFailStore) Put(r *store.Record) error {
	if r != nil && r.Type == recordTypeExecution && strings.Contains(string(r.Data), s.marker) {
		return errors.New("injected evidence write failure")
	}
	return s.Store.Put(r)
}

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
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
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

// ---- v25: terminal durability across admission ---------------------------
//
// Terminal() is monotonic — it refuses to overwrite a terminal record
// ("terminal is final"). Accepted() had no such guard: it wrote StateAccepted
// unconditionally. Because core's duplicate detection is process-local
// (e.results / e.inflight are in-memory maps), a process restart empties it,
// so a client-supplied execution id (gateway intelligence.go mints it from the
// X-Correlation-ID header) can be re-admitted after a restart. Re-admission
// then erased the durable evidence that the execution had already completed,
// destroying exactly the record that distinguishes completed from failed from
// unknown across the boundary.

// TestAcceptedCannotEraseATerminalRecord is the direct reproduction: once an
// execution is durably terminal, a later acceptance of the same id must be
// refused and the terminal record must survive untouched.
func TestAcceptedCannotEraseATerminalRecord(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	j := New(st, fixedClock())

	if err := j.Accepted("exec-dup", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("first acceptance: %v", err)
	}
	if err := j.Terminal("exec-dup", StateCompleted, ""); err != nil {
		t.Fatalf("terminal: %v", err)
	}

	// A second acceptance of the same id must be refused...
	if err := j.Accepted("exec-dup", "biz-1", "", "actor-1"); err == nil {
		t.Fatal("re-accepting a terminally recorded execution id must fail closed")
	}
	// ...and the durable terminal evidence must be intact.
	rec, ok, err := j.Get("exec-dup")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.State != StateCompleted {
		t.Fatalf("a terminal record must not be reset by re-acceptance, state=%q", rec.State)
	}
}

// TestAcceptedCannotEraseARecoveredUnknownRecord covers the other state that
// must never be re-armed: recovery's unknown verdict means the effect may have
// occurred, so re-running is unsafe and the verdict must stand.
func TestAcceptedCannotEraseARecoveredUnknownRecord(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	j := New(st, fixedClock())
	if err := j.Accepted("exec-unk", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	// "Restart": reopen and recover — the accepted record becomes unknown.
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	st2, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	j2 := New(st2, fixedClock())
	if _, err := j2.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if rec, _, _ := j2.Get("exec-unk"); rec.State != StateUnknown {
		t.Fatalf("recovery must classify the interrupted execution unknown, got %q", rec.State)
	}

	if err := j2.Accepted("exec-unk", "biz-1", "", "actor-1"); err == nil {
		t.Fatal("re-accepting an execution whose outcome is unknown must fail closed")
	}
	if rec, _, _ := j2.Get("exec-unk"); rec.State != StateUnknown {
		t.Fatalf("an unknown verdict must not be reset by re-acceptance, state=%q", rec.State)
	}
}

// ---- v27: durable approval history and terminal semantics ------------------
//
// Approval history is durable evidence that must outlive the execution's final
// outcome, and a denial must leave a truthful terminal record. These reopen the
// real FileStore so the facts are proven durable, never merely held in a map.

func reopenJournal(t *testing.T, dir string) *Journal {
	t.Helper()
	st, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	return New(st, fixedClock())
}

func approvalOutcomes(rec ExecutionRecord) []string {
	out := make([]string, 0, len(rec.Approvals))
	for _, a := range rec.Approvals {
		out = append(out, a.Outcome)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A granted approval stays auditable after the resumed execution completes,
// and the completion is the truthful durable outcome.
func TestApprovalHistorySurvivesCompletionAndRestart(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	j := New(st, fixedClock())
	if err := j.Accepted("exec-a", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	// The run parks on the gate.
	if err := j.Terminal("exec-a", StateFailed, ApprovalRequiredClass); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := j.RecordApproval("exec-a", "ap-1", ApprovalRequested, ""); err != nil {
		t.Fatalf("record requested: %v", err)
	}
	if err := j.RecordApproval("exec-a", "ap-1", ApprovalGranted, "approver-1"); err != nil {
		t.Fatalf("record granted: %v", err)
	}
	// The resumed run completes.
	if err := j.SupersedeApprovedPause("exec-a", StateCompleted, ""); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	j2 := reopenJournal(t, dir)
	rec, ok, err := j2.Get("exec-a")
	if err != nil || !ok {
		t.Fatalf("record must survive restart: ok=%v err=%v", ok, err)
	}
	if rec.State != StateCompleted {
		t.Fatalf("a resumed completion must be durably completed, got %q", rec.State)
	}
	if got := approvalOutcomes(rec); !equalStrings(got, []string{ApprovalRequested, ApprovalGranted}) {
		t.Fatalf("approval history must survive the terminal outcome, got %v", got)
	}
	// The history is associated with the right execution and approval.
	if rec.Approvals[1].ApprovalID != "ap-1" || rec.Approvals[1].DecidedBy != "approver-1" {
		t.Fatalf("history must carry its approval identity: %+v", rec.Approvals[1])
	}
}

// A denial is durably recorded and leaves coherent terminal semantics: the
// execution is terminally failed FOR THE DENIAL, not for the pause.
func TestDenialIsDurablyRecordedWithTruthfulTerminal(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	j := New(st, fixedClock())
	if err := j.Accepted("exec-d", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	if err := j.Terminal("exec-d", StateFailed, ApprovalRequiredClass); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := j.RecordApproval("exec-d", "ap-9", ApprovalRequested, ""); err != nil {
		t.Fatalf("record requested: %v", err)
	}
	if err := j.RecordApproval("exec-d", "ap-9", ApprovalDenied, "approver-2"); err != nil {
		t.Fatalf("record denied: %v", err)
	}
	if err := j.SupersedeApprovedPause("exec-d", StateFailed, ApprovalDeniedClass); err != nil {
		t.Fatalf("supersede on denial: %v", err)
	}

	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	j2 := reopenJournal(t, dir)
	rec, _, _ := j2.Get("exec-d")
	if rec.State != StateFailed || rec.ErrorClass != ApprovalDeniedClass {
		t.Fatalf("a denial must be durably recorded as failed/%s, got %q/%q",
			ApprovalDeniedClass, rec.State, rec.ErrorClass)
	}
	if got := approvalOutcomes(rec); !equalStrings(got, []string{ApprovalRequested, ApprovalDenied}) {
		t.Fatalf("a granted and a denied approval must be durably distinguishable, got %v", got)
	}
}

// The supersede is narrow: it refuses every state that is not the provisional
// pause, and never mutates the record when it refuses.
func TestSupersedeRefusesEveryNonPauseState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state State
		class string
	}{
		{"completed", StateCompleted, ""},
		{"cancelled", StateCancelled, ""},
		{"timed_out", StateTimedOut, ""},
		{"failed other class", StateFailed, "POLICY_DENIED"},
		{"unknown", StateUnknown, ""},
		{"still accepted", StateAccepted, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := New(store.NewMemStore(), fixedClock())
			if err := j.Accepted("exec-s", "biz-1", "", "a"); err != nil {
				t.Fatal(err)
			}
			if err := j.seedState("exec-s", tc.state, tc.class); err != nil {
				t.Fatal(err)
			}
			if err := j.SupersedeApprovedPause("exec-s", StateCompleted, ""); err == nil {
				t.Fatal("superseding a non-pause record must be refused")
			}
			rec, _, _ := j.Get("exec-s")
			if rec.State != tc.state || rec.ErrorClass != tc.class {
				t.Fatalf("a refused supersede must not mutate the record, got %q/%q", rec.State, rec.ErrorClass)
			}
		})
	}
}

// A record recovery already classified can never be resurrected by a supersede.
func TestSupersedeRefusesARecoveredRecord(t *testing.T) {
	dir := t.TempDir()
	j := New(mustStore(t, dir), fixedClock())
	if err := j.Accepted("exec-r", "biz-1", "", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if err := j.SupersedeApprovedPause("exec-r", StateCompleted, ""); err == nil {
		t.Fatal("a recovered record must never be superseded")
	}
	rec, _, _ := j.Get("exec-r")
	if rec.State != StateUnknown {
		t.Fatalf("the recovered verdict must stand, got %q", rec.State)
	}
}

// Supersede is one-shot: the first real outcome is final, exactly like any
// other terminal write.
func TestSupersedeIsOneShot(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1s", "biz-1", "", "a"); err != nil {
		t.Fatal(err)
	}
	if err := j.Terminal("exec-1s", StateFailed, ApprovalRequiredClass); err != nil {
		t.Fatal(err)
	}
	if err := j.SupersedeApprovedPause("exec-1s", StateCompleted, ""); err != nil {
		t.Fatalf("first supersede: %v", err)
	}
	if err := j.SupersedeApprovedPause("exec-1s", StateCancelled, ""); err == nil {
		t.Fatal("a second supersede must be refused")
	}
	rec, _, _ := j.Get("exec-1s")
	if rec.State != StateCompleted {
		t.Fatalf("the first supersede is final, got %q", rec.State)
	}
}

// History is never fabricated: an unknown execution and an unknown outcome are
// both refused.
func TestRecordApprovalRefusesUnknownExecutionAndOutcome(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-k", "biz-1", "", "a"); err != nil {
		t.Fatal(err)
	}
	if err := j.RecordApproval("never-accepted", "ap", ApprovalGranted, "x"); err == nil {
		t.Fatal("history must not be invented for an execution with no durable record")
	}
	if err := j.RecordApproval("exec-k", "ap", "maybe", "x"); err == nil {
		t.Fatal("an unknown approval outcome must be refused")
	}
	rec, _, _ := j.Get("exec-k")
	if len(rec.Approvals) != 0 {
		t.Fatalf("a refused history write must record nothing, got %v", rec.Approvals)
	}
}

// Compatibility: a record written before approval history existed carries no
// history. It still supersedes, and none is invented to fill the gap.
func TestLegacyApprovalPauseRecordSupersedesWithoutFabrication(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-legacy", "biz-1", "", "a"); err != nil {
		t.Fatal(err)
	}
	// Exactly what older code wrote: a terminal pause, no history.
	if err := j.Terminal("exec-legacy", StateFailed, ApprovalRequiredClass); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := j.Get("exec-legacy")
	if len(rec.Approvals) != 0 {
		t.Fatalf("a legacy pause must carry no history, got %v", rec.Approvals)
	}
	if err := j.SupersedeApprovedPause("exec-legacy", StateCompleted, ""); err != nil {
		t.Fatalf("a legacy pause must still supersede: %v", err)
	}
	rec, _, _ = j.Get("exec-legacy")
	if rec.State != StateCompleted || len(rec.Approvals) != 0 {
		t.Fatalf("supersede must not fabricate history: %q / %v", rec.State, rec.Approvals)
	}
}

// Approval history written before an ordinary terminal survives it: the history
// is independent of the execution's own outcome.
func TestApprovalHistorySurvivesAnOrdinaryTerminal(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-o", "biz-1", "", "a"); err != nil {
		t.Fatal(err)
	}
	if err := j.RecordApproval("exec-o", "ap", ApprovalRequested, ""); err != nil {
		t.Fatal(err)
	}
	if err := j.Terminal("exec-o", StateFailed, ApprovalRequiredClass); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := j.Get("exec-o")
	if got := approvalOutcomes(rec); !equalStrings(got, []string{ApprovalRequested}) {
		t.Fatalf("an ordinary terminal must not erase approval history, got %v", got)
	}
}

// An abandoned approval is a terminal record, so it is retention-eligible on
// the ordinary age basis — the documented, testable cleanup for approvals that
// were never decided.
func TestAbandonedApprovalIsRetentionEligible(t *testing.T) {
	tick := time.Unix(1_700_000_000, 0)
	clk := func() time.Time { tick = tick.Add(time.Second); return tick }
	j := New(store.NewMemStore(), clk)
	if err := j.Accepted("exec-ab", "biz-1", "", "a"); err != nil {
		t.Fatal(err)
	}
	if err := j.Terminal("exec-ab", StateFailed, ApprovalRequiredClass); err != nil {
		t.Fatal(err)
	}
	if err := j.RecordApproval("exec-ab", "ap", ApprovalRequested, ""); err != nil {
		t.Fatal(err)
	}
	// Young: still retained.
	if _, err := j.ApplyRetention(RetentionPolicy{Enabled: true, MinAge: 7 * 24 * time.Hour}); err != nil {
		t.Fatalf("apply young: %v", err)
	}
	if _, ok, _ := j.Get("exec-ab"); !ok {
		t.Fatal("a young abandoned approval must be retained")
	}
	// Old enough: reclaimable.
	tick = tick.Add(30 * 24 * time.Hour)
	rep, err := j.ApplyRetention(RetentionPolicy{Enabled: true, MinAge: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("apply old: %v", err)
	}
	if rep.RemovedRecords != 1 {
		t.Fatalf("an abandoned approval must become retention-eligible, report %+v", rep)
	}
	if _, ok, _ := j.Get("exec-ab"); ok {
		t.Fatal("the abandoned approval record must be gone")
	}
}

// mustStore opens a real FileStore over dir for a test that later reopens it.
// It returns the concrete type so callers can release ownership before the
// next open of the same directory.
func mustStore(t *testing.T, dir string) *store.FileStore {
	t.Helper()
	st, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return st
}

// seedState forces a durable state without going through the public transition
// API, so a test can construct records that no ordinary path produces (a
// recovered `unknown`, for example) and prove the supersede refuses them.
func (j *Journal) seedState(id string, state State, class string) error {
	rec, version, ok, err := j.getExecution(id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	rec.State = state
	rec.ErrorClass = class
	rec.UpdatedAt = j.now().UTC()
	return j.putExecution(rec, version)
}

// ---- v28: backend parity for a missing durable record ----------------------
//
// Store.Get's contract (internal/foundation/store/store.go) is "returns nil if
// not found", and FileStore honors it. MemStore signals absence with
// ErrNotFound instead. Every journal operation must therefore behave the same
// whichever backend holds the records — these run one table against both.

func backendJournals(t *testing.T) map[string]*Journal {
	t.Helper()
	dir := t.TempDir()
	fs, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatalf("open filestore: %v", err)
	}
	return map[string]*Journal{
		"MemStore":  New(store.NewMemStore(), fixedClock()),
		"FileStore": New(fs, fixedClock()),
	}
}

// A terminal write for an execution that was never accepted still records the
// fact. This is the documented intent, and it must not depend on the backend.
func TestTerminalParityForAnUnknownExecution(t *testing.T) {
	for name, j := range backendJournals(t) {
		t.Run(name, func(t *testing.T) {
			if err := j.Terminal("never-accepted", StateFailed, "SOME_CLASS"); err != nil {
				t.Fatalf("terminal for an unknown execution must be recorded, got %v", err)
			}
			rec, ok, err := j.Get("never-accepted")
			if err != nil || !ok {
				t.Fatalf("the terminal fact must be recorded: ok=%v err=%v", ok, err)
			}
			if rec.State != StateFailed || rec.ErrorClass != "SOME_CLASS" {
				t.Fatalf("unexpected recorded state %q/%q", rec.State, rec.ErrorClass)
			}
		})
	}
}

// An outcome for an invocation whose intent record is gone is a no-op on both
// backends: there is nothing to update, and that is not a write failure.
func TestRecordOutcomeParityForAnUnknownInvocation(t *testing.T) {
	for name, j := range backendJournals(t) {
		t.Run(name, func(t *testing.T) {
			if err := j.RecordOutcome("inv-does-not-exist", InvCompleted, ""); err != nil {
				t.Fatalf("a missing intent record must be a no-op, not an error, got %v", err)
			}
		})
	}
}

// A supersede against an execution that does not exist reports the SAME thing
// on both backends: there is no approval pause to supersede.
func TestSupersedeParityForAnUnknownExecution(t *testing.T) {
	for name, j := range backendJournals(t) {
		t.Run(name, func(t *testing.T) {
			err := j.SupersedeApprovedPause("no-such-exec", StateCompleted, "")
			if !errors.Is(err, ErrNotApprovalPause) {
				t.Fatalf("expected ErrNotApprovalPause, got %v", err)
			}
		})
	}
}

// Approval history never fabricates a record, and reports it identically on
// both backends (v27 behaviour preserved by the normalization).
func TestRecordApprovalParityForAnUnknownExecution(t *testing.T) {
	for name, j := range backendJournals(t) {
		t.Run(name, func(t *testing.T) {
			if err := j.RecordApproval("no-such-exec", "ap", ApprovalGranted, "x"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
		})
	}
}

// The v25 duplicate-submission guard must be backend-independent: re-accepting
// an already-terminal or recovered-unknown execution id is refused either way.
func TestAcceptedRefusalParityForTerminalRecords(t *testing.T) {
	for name, j := range backendJournals(t) {
		t.Run(name, func(t *testing.T) {
			if err := j.Accepted("dup", "biz-1", "", "a"); err != nil {
				t.Fatal(err)
			}
			if err := j.Terminal("dup", StateCompleted, ""); err != nil {
				t.Fatal(err)
			}
			if err := j.Accepted("dup", "biz-1", "", "a"); !errors.Is(err, ErrAlreadyRecorded) {
				t.Fatalf("a terminal id must not be re-accepted, got %v", err)
			}
		})
	}
}

// The complete v25-v27 lifecycle must produce an identical durable record on
// both backends: acceptance, pause, approval history, one-shot supersession,
// duplicate protection, and recovery of an interrupted execution.
func TestFullLifecycleParityAcrossBackends(t *testing.T) {
	for name, j := range backendJournals(t) {
		t.Run(name, func(t *testing.T) {
			// Accepted, paused, approval requested.
			if err := j.Accepted("life", "biz-1", "div-1", "actor-1"); err != nil {
				t.Fatalf("accepted: %v", err)
			}
			if err := j.Terminal("life", StateFailed, ApprovalRequiredClass); err != nil {
				t.Fatalf("pause: %v", err)
			}
			if err := j.RecordApproval("life", "ap-1", ApprovalRequested, ""); err != nil {
				t.Fatalf("requested: %v", err)
			}
			// A terminal id must not be re-armed (v25).
			if err := j.Accepted("life", "biz-1", "div-1", "actor-1"); !errors.Is(err, ErrAlreadyRecorded) {
				t.Fatalf("v25 duplicate protection must hold on %s, got %v", name, err)
			}
			// Granted, then a truthful supersession (v27).
			if err := j.RecordApproval("life", "ap-1", ApprovalGranted, "approver-1"); err != nil {
				t.Fatalf("granted: %v", err)
			}
			if err := j.SupersedeApprovedPause("life", StateCompleted, ""); err != nil {
				t.Fatalf("supersede: %v", err)
			}
			// Supersession is one-shot.
			if err := j.SupersedeApprovedPause("life", StateCancelled, ""); !errors.Is(err, ErrNotApprovalPause) {
				t.Fatalf("a second supersede must be refused on %s, got %v", name, err)
			}

			rec, _, err := j.Get("life")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if rec.State != StateCompleted || rec.ErrorClass != "" {
				t.Fatalf("final state must be completed with no error class, got %q/%q", rec.State, rec.ErrorClass)
			}
			if got := approvalOutcomes(rec); !equalStrings(got, []string{ApprovalRequested, ApprovalGranted}) {
				t.Fatalf("history must be the ordered request+grant, got %v", got)
			}
			if rec.BusinessID != "biz-1" || rec.DivisionID != "div-1" || rec.ActorID != "actor-1" {
				t.Fatalf("scope must be preserved identically on %s: %+v", name, rec)
			}
		})
	}
}

// Recovery must classify an interrupted execution identically on both backends,
// and must leave terminal records alone.
func TestRecoveryParityAcrossBackends(t *testing.T) {
	for name, j := range backendJournals(t) {
		t.Run(name, func(t *testing.T) {
			if err := j.Accepted("open-1", "biz-1", "", "a"); err != nil {
				t.Fatal(err)
			}
			if err := j.Accepted("done-1", "biz-1", "", "a"); err != nil {
				t.Fatal(err)
			}
			if err := j.Terminal("done-1", StateCompleted, ""); err != nil {
				t.Fatal(err)
			}
			rep, err := j.Recover()
			if err != nil {
				t.Fatalf("recover: %v", err)
			}
			if rep.Scanned != 2 || rep.Terminal != 1 || rep.Recovered != 1 {
				t.Fatalf("recovery counts must match on %s, got %+v", name, rep)
			}
			if rec, _, _ := j.Get("open-1"); rec.State != StateUnknown {
				t.Fatalf("an interrupted execution must be unknown on %s, got %q", name, rec.State)
			}
			if rec, _, _ := j.Get("done-1"); rec.State != StateCompleted {
				t.Fatalf("a terminal record must never be rewritten on %s, got %q", name, rec.State)
			}
		})
	}
}

// A corrupt durable record must stay distinguishable from absence on BOTH
// backends: it fails closed rather than reading as "no record".
func TestCorruptRecordIsNotMistakenForAbsence(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	j := New(st, fixedClock())
	if err := j.Accepted("good", "biz-1", "", "a"); err != nil {
		t.Fatal(err)
	}
	// Corrupt one record's payload directly in the store.
	corrupt := execKey("good")
	r, err := st.Get(corrupt)
	if err != nil || r == nil {
		t.Fatalf("setup: %v", err)
	}
	r.Data = []byte("{not json")
	if err := st.Put(r); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if _, _, err := j.Get("good"); err == nil {
		t.Fatal("a corrupt record must fail closed, not read as absent")
	}
	// Recovery must refuse to run over corrupt durable state.
	if _, err := j.Recover(); err == nil {
		t.Fatal("recovery must fail closed over a corrupt record")
	}
	// An unrelated missing record is still plain absence, never a failure.
	if _, _, err := j.Get("never-existed"); err != nil {
		t.Fatalf("absence must not be reported as an error, got %v", err)
	}
}

func sampleEvidence(title string) *ResearchEvidence {
	return &ResearchEvidence{
		Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
		Operation: ResearchEvidenceOperation, Count: 1,
		Sources:     []ResearchSource{{ID: "src-1", Title: title, URL: "https://example.test/source", Source: "example.test", Snippet: "bounded snippet"}},
		Attribution: ResearchAttributionUnavailable,
	}
}

// Evidence is written atomically with the terminal state, carries the real
// terminal timestamp, and survives reopening the durable store.
func TestTerminalWithEvidenceSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	j := New(st, fixedClock())
	if err := j.Accepted("exec-evidence", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatal(err)
	}
	if err := j.TerminalWithEvidence("exec-evidence", StateCompleted, "", sampleEvidence("Nexus")); err != nil {
		t.Fatalf("terminal with evidence: %v", err)
	}
	rec, ok, err := j.Get("exec-evidence")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.State != StateCompleted || rec.CompletedAt == nil {
		t.Fatalf("terminal state/timestamp missing: %+v", rec)
	}
	if rec.ResearchEvidence == nil || rec.ResearchEvidence.Count != 1 ||
		rec.ResearchEvidence.Sources[0].Title != "Nexus" {
		t.Fatalf("evidence not recorded: %+v", rec.ResearchEvidence)
	}
	if rec.CompletedAt.Before(rec.CreatedAt) {
		t.Fatalf("completed_at must not precede created_at: %v < %v", rec.CompletedAt, rec.CreatedAt)
	}

	// Restart: reopen the same durable directory with a fresh journal.
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	j2 := New(st2, nil)
	rec2, ok, err := j2.Get("exec-evidence")
	if err != nil || !ok {
		t.Fatalf("reopen get: ok=%v err=%v", ok, err)
	}
	if rec2.CompletedAt == nil || !rec2.CompletedAt.Equal(*rec.CompletedAt) {
		t.Fatalf("completed_at changed across restart: %v -> %v", rec.CompletedAt, rec2.CompletedAt)
	}
	if rec2.ResearchEvidence == nil || rec2.ResearchEvidence.Sources[0].URL != "https://example.test/source" {
		t.Fatalf("evidence did not survive restart: %+v", rec2.ResearchEvidence)
	}
}

// A v2 plan/step/operation snapshot is written atomically with the terminal
// transition and survives reopening the durable store. The association is
// execution-scoped: a plan id from another execution never becomes durable.
func TestTerminalWithResearchProvenanceSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStoreWithClock(dir, fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	j := New(st, fixedClock())
	if err := j.Accepted("exec-prov", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatal(err)
	}
	if err := j.TerminalWithEvidence("exec-prov", StateCompleted, "", provenanceEvidence("exec-prov")); err != nil {
		t.Fatalf("terminal with provenance: %v", err)
	}
	rec, ok, err := j.Get("exec-prov")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.ResearchEvidence == nil || rec.ResearchEvidence.Plan == nil ||
		rec.ResearchEvidence.Plan.PlanID != "exec-prov" ||
		len(rec.ResearchEvidence.Steps) != 2 || len(rec.ResearchEvidence.Operations) != 2 {
		t.Fatalf("provenance not recorded: %+v", rec.ResearchEvidence)
	}

	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec2, ok, err := New(st2, nil).Get("exec-prov")
	if err != nil || !ok {
		t.Fatalf("reopen get: ok=%v err=%v", ok, err)
	}
	if rec2.ResearchEvidence == nil ||
		rec2.ResearchEvidence.Operations[0].SourceIDs[0] != "src-1" ||
		rec2.ResearchEvidence.Steps[1].Status != "completed" {
		t.Fatalf("provenance did not survive restart: %+v", rec2.ResearchEvidence)
	}

	// Cross-execution plan identity is refused before the terminal write.
	if err := j.TerminalWithEvidence("exec-other", StateCompleted, "", provenanceEvidence("exec-prov")); err == nil {
		t.Fatal("a plan from another execution must not become durable")
	}
}

// Legacy v52/v53 records carry evidence schema v1, which has no source ids,
// plan, steps or operations. It must remain readable exactly as written.
func TestLegacyResearchEvidenceDecodesWithoutInventedProvenance(t *testing.T) {
	st := store.NewMemStore()
	raw := []byte(`{"version":1,"id":"legacy-evidence","business_id":"biz-1","state":"completed",` +
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:01:00Z",` +
		`"research_evidence":{"version":1,"capability":"web.search","operation":"search","count":1,` +
		`"sources":[{"title":"Legacy","url":"https://legacy.example","source":"legacy.example","snippet":"old"}]}}`)
	if err := st.Put(&store.Record{ID: execKey("legacy-evidence"), Type: recordTypeExecution, Data: raw}); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := New(st, fixedClock()).Get("legacy-evidence")
	if err != nil || !ok {
		t.Fatalf("legacy evidence get: ok=%v err=%v", ok, err)
	}
	if rec.ResearchEvidence == nil || rec.ResearchEvidence.Version != ResearchEvidenceLegacyVersion {
		t.Fatalf("legacy evidence missing: %+v", rec.ResearchEvidence)
	}
	if rec.ResearchEvidence.Plan != nil || len(rec.ResearchEvidence.Steps) != 0 ||
		len(rec.ResearchEvidence.Operations) != 0 {
		t.Fatalf("legacy evidence must not invent provenance: %+v", rec.ResearchEvidence)
	}
	if rec.ResearchEvidence.Sources[0].ID != "" {
		t.Fatalf("legacy evidence must not invent source ids: %+v", rec.ResearchEvidence.Sources[0])
	}
}

func TestSupersedeApprovedPauseUpdatesTimestampsAndEvidence(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	clock := func() time.Time { return now }
	j := New(store.NewMemStore(), clock)
	if err := j.Accepted("exec-pause", "biz-1", "", "actor-1"); err != nil {
		t.Fatal(err)
	}
	if err := j.TerminalWithEvidence("exec-pause", StateFailed, ApprovalRequiredClass, sampleEvidence("Before pause")); err != nil {
		t.Fatal(err)
	}
	paused, _, _ := j.Get("exec-pause")
	if paused.CompletedAt == nil {
		t.Fatal("the provisional pause must carry its transition time")
	}
	pauseTime := *paused.CompletedAt

	now = now.Add(5 * time.Minute)
	if err := j.SupersedeApprovedPauseWithEvidence("exec-pause", StateCompleted, "", sampleEvidence("After resume")); err != nil {
		t.Fatal(err)
	}
	final, _, _ := j.Get("exec-pause")
	if final.State != StateCompleted || final.CompletedAt == nil {
		t.Fatalf("final state missing: %+v", final)
	}
	if !final.CompletedAt.After(pauseTime) {
		t.Fatalf("final completed_at must replace the pause time: %v -> %v", pauseTime, final.CompletedAt)
	}
	if final.ResearchEvidence == nil || final.ResearchEvidence.Sources[0].Title != "After resume" {
		t.Fatalf("final evidence was not replaced: %+v", final.ResearchEvidence)
	}
}

// A resumed run that captured no new evidence must not erase evidence captured
// before the pause.
func TestSupersedeApprovedPausePreservesPauseEvidenceWhenFinalIsNil(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-pause-keep", "biz-1", "", "actor-1"); err != nil {
		t.Fatal(err)
	}
	if err := j.TerminalWithEvidence("exec-pause-keep", StateFailed, ApprovalRequiredClass, sampleEvidence("Kept")); err != nil {
		t.Fatal(err)
	}
	if err := j.SupersedeApprovedPauseWithEvidence("exec-pause-keep", StateCompleted, "", nil); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := j.Get("exec-pause-keep")
	if rec.ResearchEvidence == nil || rec.ResearchEvidence.Sources[0].Title != "Kept" {
		t.Fatalf("pause evidence must survive a nil final capture: %+v", rec.ResearchEvidence)
	}
}

// Older records have neither field. They decode as unavailable, never as a
// zero timestamp or an empty-but-present source list.
func TestLegacyExecutionRecordHasNoFabricatedTimestampsOrEvidence(t *testing.T) {
	st := store.NewMemStore()
	raw := []byte(`{"version":1,"id":"legacy","business_id":"biz-1","state":"completed",` +
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:01:00Z"}`)
	if err := st.Put(&store.Record{ID: execKey("legacy"), Type: recordTypeExecution, Data: raw}); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := New(st, fixedClock()).Get("legacy")
	if err != nil || !ok {
		t.Fatalf("legacy get: ok=%v err=%v", ok, err)
	}
	if rec.CompletedAt != nil {
		t.Fatalf("legacy completed_at must be unavailable, got %v", rec.CompletedAt)
	}
	if rec.ResearchEvidence != nil {
		t.Fatalf("legacy evidence must be absent, got %+v", rec.ResearchEvidence)
	}
}

// If the atomic terminal write fails, the record remains nonterminal and no
// evidence is exposed. A process-local result cannot become durable evidence
// through a later partial write.
func TestEvidenceWriteFailureLeavesRecordNonterminalWithoutEvidence(t *testing.T) {
	base := store.NewMemStore()
	failing := evidenceWriteFailStore{Store: base, marker: `"research_evidence"`}
	j := New(failing, fixedClock())
	if err := j.Accepted("exec-fail", "biz-1", "", "actor-1"); err != nil {
		t.Fatal(err)
	}
	if err := j.TerminalWithEvidence("exec-fail", StateCompleted, "", sampleEvidence("Never durable")); err == nil {
		t.Fatal("the injected terminal write must fail")
	}
	rec, ok, err := j.Get("exec-fail")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.State.Terminal() || rec.CompletedAt != nil || rec.ResearchEvidence != nil {
		t.Fatalf("failed write must leave no terminal/evidence claim: %+v", rec)
	}
}

// Corrupt or unsupported evidence fails closed at decode rather than serving a
// possibly partial snapshot.
func TestUnsupportedEvidenceVersionFailsClosed(t *testing.T) {
	st := store.NewMemStore()
	raw := []byte(`{"version":1,"id":"bad-evidence","business_id":"biz-1","state":"completed",` +
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:01:00Z",` +
		`"research_evidence":{"version":99,"capability":"web.search","operation":"search","count":0,"sources":[]}}`)
	if err := st.Put(&store.Record{ID: execKey("bad-evidence"), Type: recordTypeExecution, Data: raw}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := New(st, fixedClock()).Get("bad-evidence"); err == nil {
		t.Fatal("unsupported evidence version must fail closed")
	}
}

// A plan snapshot belongs to exactly one execution. A record whose persisted
// plan id names another execution must not decode as valid provenance.
func TestForeignResearchPlanFailsClosedAtDecode(t *testing.T) {
	st := store.NewMemStore()
	raw := []byte(`{"version":1,"id":"exec-mismatch","business_id":"biz-1","state":"completed",` +
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:01:00Z",` +
		`"research_evidence":{"version":2,"capability":"web.search","operation":"search","count":0,"sources":[],` +
		`"plan":{"plan_id":"exec-other","version":1},` +
		`"steps":[{"step_id":"s1","status":"completed"}]}}`)
	if err := st.Put(&store.Record{ID: execKey("exec-mismatch"), Type: recordTypeExecution, Data: raw}); err != nil {
		t.Fatal(err)
	}
	_, _, err := New(st, fixedClock()).Get("exec-mismatch")
	if err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("a foreign research plan id must fail closed at decode, got %v", err)
	}
}
