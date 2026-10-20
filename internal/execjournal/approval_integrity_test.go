package execjournal

import (
	"testing"
	"time"
)

// ---- v35: approval-history integrity ---------------------------------------
//
// The record-level decoder already validates the execution STATE against a
// closed set and fails closed on anything else. Approval OUTCOME is the same
// kind of enumerated field and had no such check, so an out-of-vocabulary value
// decoded cleanly and reached readers.
//
// This is NOT a claim that every unknown value is corruption. SchemaVersion is
// the documented forward-compatibility mechanism: a version that extends the
// vocabulary must bump it, and a record of another version already fails closed
// (RETENTION §7). So within version 1 the vocabulary IS closed, and failing
// closed enforces the existing policy rather than adding a new one.
//
// What is deliberately NOT validated, because it is legitimate: ApprovalID may
// be empty. approval.go documents graceful degradation — when createApproval
// fails the engine surfaces APPROVAL_REQUIRED with no approval_id — so a real
// pause entry can carry one.

func approvalRecord(id string, entries ...ApprovalDecision) ExecutionRecord {
	return ExecutionRecord{
		Version: SchemaVersion, ID: id, BusinessID: "biz-1", State: StateCompleted,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
		UpdatedAt: time.Unix(1_700_000_000, 0).UTC(),
		Approvals: entries,
	}
}

func entry(id, outcome string) ApprovalDecision {
	return ApprovalDecision{
		ApprovalID: id, Outcome: outcome,
		DecidedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

// A valid history round-trips intact, in order.
func TestValidApprovalHistoryRoundTrips(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	seedRawExecution(t, st, approvalRecord("ok-1",
		entry("ap-1", ApprovalRequested),
		entry("ap-1", ApprovalGranted),
	))
	j := New(st, fixedClock())
	rec, ok, err := j.Get("ok-1")
	if err != nil || !ok {
		t.Fatalf("valid history must be readable: ok=%v err=%v", ok, err)
	}
	if len(rec.Approvals) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(rec.Approvals))
	}
	if rec.Approvals[0].Outcome != ApprovalRequested || rec.Approvals[1].Outcome != ApprovalGranted {
		t.Fatalf("order must be preserved: %+v", rec.Approvals)
	}
	// And it survives a real reopen.
	rec2, _, err := reopenJournal(t, dir).Get("ok-1")
	if err != nil || len(rec2.Approvals) != 2 || rec2.Approvals[1].Outcome != ApprovalGranted {
		t.Fatalf("history must survive reopen: %+v err=%v", rec2.Approvals, err)
	}
}

// An empty approval_id is a legitimate recorded state (graceful degradation).
func TestEmptyApprovalIDRemainsReadable(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	seedRawExecution(t, st, approvalRecord("ok-2", entry("", ApprovalRequested)))
	j := New(st, fixedClock())
	rec, ok, err := j.Get("ok-2")
	if err != nil || !ok {
		t.Fatalf("an empty approval_id must not invalidate real history: ok=%v err=%v", ok, err)
	}
	if len(rec.Approvals) != 1 || rec.Approvals[0].Outcome != ApprovalRequested {
		t.Fatalf("entry must survive: %+v", rec.Approvals)
	}
}

// An omitted decided_by (the pause entry writes "") stays valid.
func TestEmptyDecidedByRemainsReadable(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	seedRawExecution(t, st, approvalRecord("ok-3",
		ApprovalDecision{ApprovalID: "ap-3", Outcome: ApprovalRequested,
			DecidedAt: time.Unix(1_700_000_000, 0).UTC()},
	))
	j := New(st, fixedClock())
	if rec, _, err := j.Get("ok-3"); err != nil || len(rec.Approvals) != 1 {
		t.Fatalf("an omitted decider must stay valid: ok err=%v rec=%+v", err, rec)
	}
}

// A record with NO approvals is legitimate and reads as an empty history.
func TestAbsentApprovalHistoryIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	seedRawExecution(t, st, approvalRecord("ok-4"))
	j := New(st, fixedClock())
	rec, _, err := j.Get("ok-4")
	if err != nil {
		t.Fatalf("a record without history must read cleanly: %v", err)
	}
	if len(rec.Approvals) != 0 {
		t.Fatalf("expected no history, got %+v", rec.Approvals)
	}
}

// An out-of-vocabulary outcome fails the record closed, exactly as an
// out-of-vocabulary state does. It is never silently dropped, and the valid
// entries alongside it are NOT returned as a partial history.
func TestUnknownApprovalOutcomeFailsClosed(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	seedRawExecution(t, st, approvalRecord("bad-1",
		entry("ap-1", ApprovalRequested),
		entry("ap-1", "maybe-approved"), // not in the closed set
		entry("ap-1", ApprovalGranted),
	))
	j := New(st, fixedClock())
	rec, ok, err := j.Get("bad-1")
	if err == nil {
		t.Fatal("an out-of-vocabulary outcome must fail closed")
	}
	if ok {
		t.Fatal("no record may be returned alongside the error")
	}
	if rec.Approvals != nil {
		t.Fatalf("a failed read must not hand back a partial history: %+v", rec.Approvals)
	}
}

// An empty outcome is likewise invalid, and distinct from absence.
func TestEmptyApprovalOutcomeFailsClosed(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	seedRawExecution(t, st, approvalRecord("bad-2", entry("ap-1", "")))
	j := New(st, fixedClock())
	if _, ok, err := j.Get("bad-2"); err == nil || ok {
		t.Fatalf("an empty outcome must fail closed, got ok=%v err=%v", ok, err)
	}
}

// Recovery must also refuse to run over a record it cannot validate: it must
// not rewrite, compact or drop the offending entry.
func TestRecoveryRefusesAnInvalidApprovalRecord(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	seedRawExecution(t, st, approvalRecord("bad-3", entry("ap-1", "unknown-outcome")))
	j := New(st, fixedClock())
	if _, err := j.Recover(); err == nil {
		t.Fatal("recovery must fail closed over an invalid record, not repair it")
	}
	// The persisted evidence is untouched — nothing was rewritten or dropped.
	raw, err := st.Get(execKey("bad-3"))
	if err != nil || raw == nil {
		t.Fatalf("the record must still exist: %v", err)
	}
	if want := `"unknown-outcome"`; !contains(string(raw.Data), want) {
		t.Fatalf("persisted evidence must not be rewritten: %s", raw.Data)
	}
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
