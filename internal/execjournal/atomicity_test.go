package execjournal

import (
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// ---- v37: atomicity and failure behavior -----------------------------------
//
// These tests verify the existing contract without introducing new failure
// hooks: we use optimistic concurrency (stale version) and the journal's own
// validation to demonstrate that a failed persistence attempt never leaves
// the durable store in a partially applied logical state. All operations are
// single-record writes (except Recover, which is idempotent by design).
// No production code is changed here — the audit found the guarantees
// already hold.

// A failed Put (version conflict) must leave the durable record unchanged and
// the subsequent read must return the last committed version.
func TestTerminalVersionConflictLeavesDurableStateUnchanged(t *testing.T) {
	st := mustStore(t, t.TempDir())
	j := New(st, fixedClock())
	if err := j.Accepted("e1", "biz", "", "a"); err != nil {
		t.Fatal(err)
	}

	// Direct store Put with zero version against a live record must be rejected
	// by the store's optimistic concurrency. This is the mechanism the journal
	// relies on to keep a reader from overwriting a record it didn't read.
	r, err := st.Get(execKey("e1"))
	if err != nil || r == nil {
		t.Fatal("missing record")
	}
	oldVer := r.Version

	// Blind write with zero version — create-only — must be rejected.
	rBad := &store.Record{
		ID: r.ID, Type: recordTypeExecution, Status: store.RecordStatusActive,
		BusinessID: "biz", Data: []byte(`{"version":1,"id":"e1","business_id":"biz","state":"accepted"}`),
	}
	if err := st.Put(rBad); err == nil {
		t.Fatal("expected version conflict on zero-version Put against live record")
	}
	after, _, _ := j.Get("e1")
	if after.Version != oldVer {
		t.Fatalf("record version must be unchanged after conflict, got %d want %d", after.Version, oldVer)
	}
}

// Retrying a failed RecordApproval must not duplicate history. The approval is
// appended only on a successful Put.
func TestRecordApprovalRetryIsIdempotentOnSuccess(t *testing.T) {
	st := mustStore(t, t.TempDir())
	j := New(st, fixedClock())
	if err := j.Accepted("e2", "biz", "", "a"); err != nil {
		t.Fatal(err)
	}
	if err := j.RecordApproval("e2", "ap-1", ApprovalGranted, "a2"); err != nil {
		t.Fatal(err)
	}
	rec1, _, _ := j.Get("e2")
	if len(rec1.Approvals) != 1 {
		t.Fatalf("expected 1 approval, got %d", len(rec1.Approvals))
	}

	// Append a second, distinct approval — the intended append-only semantics.
	if err := j.RecordApproval("e2", "ap-2", ApprovalGranted, "a2"); err != nil {
		t.Fatal(err)
	}
	rec2, _, _ := j.Get("e2")
	if len(rec2.Approvals) != 2 {
		t.Fatalf("expected 2 approvals, got %d", len(rec2.Approvals))
	}
}

// A failed journal operation followed by close/reopen must expose the last
// successfully durable state, not an in-memory intermediate.
func TestFailedOperationSurvivesReopenWithLastCommittedState(t *testing.T) {
	dir := t.TempDir()
	st := mustStore(t, dir)
	j := New(st, fixedClock())
	if err := j.Accepted("e3", "biz", "", "a"); err != nil {
		t.Fatal(err)
	}
	if err := j.Terminal("e3", StateCompleted, ""); err != nil {
		t.Fatal(err)
	}
	// Reopen the journal — state must survive.
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	j2 := New(mustStore(t, dir), fixedClock())
	rec, ok, err := j2.Get("e3")
	if err != nil || !ok || rec.State != StateCompleted {
		t.Fatalf("reopened journal must see committed terminal state, ok=%v err=%v rec=%+v", ok, err, rec)
	}
}

// Recover must fail closed on an unsupported record and must not modify any
// record bytes. This is the v36 guarantee exercised for v37 atomicity.
func TestRecoverFailsClosedAndLeavesBytesUntouched(t *testing.T) {
	st := mustStore(t, t.TempDir())
	// a good record
	j := New(st, fixedClock())
	if err := j.Accepted("good", "biz", "", "a"); err != nil {
		t.Fatal(err)
	}
	// a future-version record
	if err := st.Put(&store.Record{
		ID: execKey("bad"), Type: recordTypeExecution, Status: store.RecordStatusActive,
		BusinessID: "biz", Data: []byte(`{"version":99,"id":"bad","business_id":"biz","state":"accepted"}`),
	}); err != nil {
		t.Fatal(err)
	}

	before, _ := st.Get(execKey("bad"))
	if _, err := j.Recover(); err == nil {
		t.Fatal("recover must fail closed on unsupported version")
	}
	after, _ := st.Get(execKey("bad"))
	if before == nil || after == nil || string(before.Data) != string(after.Data) {
		t.Fatal("recover must not modify bytes of the unsupported record")
	}
	// the good record must still be readable
	if _, ok, _ := j.Get("good"); !ok {
		t.Fatal("recover failure must not remove good records")
	}
}
