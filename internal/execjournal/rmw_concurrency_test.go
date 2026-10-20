package execjournal

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// ---- v41: journal read-modify-write atomicity ------------------------------
//
// The journal's putExecution used to re-read the store version immediately
// before Put (journal.go), which laundered a stale snapshot's version and let
// a concurrent writer overwrite a newer record it never saw. Journal.mu only
// guarded seq, so the read-modify-write cycle was unserialized.
//
// Fix:
//   - j.mu now serializes the entire read-modify-write cycle so concurrent
//     operations through the same Journal instance are ordered and both
//     updates are preserved.
//   - putExecution/putInvocation carry the version from the initial read
//     forward instead of re-reading it, so writes through separate Journal
//     instances sharing the same store surface VERSION_CONFLICT instead of
//     silently losing an update.
//
// Contract: concurrent writers either both succeed (same instance, serialized)
// or the conflicting writer gets an explicit VERSION_CONFLICT error — never a
// silent lost update.

func TestConcurrentRecordApprovalSameJournalPreservesBoth(t *testing.T) {
	st := store.NewMemStore()
	j := New(st, fixedClock())
	if err := j.Accepted("exec-rmw-1", "biz-1", "", "actor-1"); err != nil {
		t.Fatal(err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = j.RecordApproval("exec-rmw-1",
				fmt.Sprintf("ap-%d", i), ApprovalGranted, fmt.Sprintf("approver-%d", i))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("RecordApproval[%d] should succeed (same instance serializes): %v", i, err)
		}
	}

	rec, ok, err := j.Get("exec-rmw-1")
	if err != nil || !ok {
		t.Fatalf("get: err=%v ok=%v", err, ok)
	}
	if len(rec.Approvals) != n {
		t.Fatalf("expected %d approvals, got %d: %+v", n, len(rec.Approvals), rec.Approvals)
	}
}

// TestConcurrentRecordApprovalSeparateInstances verifies that two Journal
// instances sharing the same backing store cannot silently overwrite one
// another. One write succeeds; the other gets VERSION_CONFLICT — an explicit
// conflict the caller can handle, never a silent lost update.
// TestConcurrentRecordApprovalSeparateInstances verifies that two Journal
// instances sharing the same backing store cannot silently overwrite one
// another. Either both writes succeed (serialized through their respective
// instance mutexes, both approvals preserved) or the conflicting writer gets
// VERSION_CONFLICT — an explicit conflict the caller can handle, never a
// silent lost update.
func TestConcurrentRecordApprovalSeparateInstances(t *testing.T) {
	st := store.NewMemStore()
	j1 := New(st, fixedClock())
	j2 := New(st, fixedClock())

	if err := j1.Accepted("exec-rmw-2", "biz-1", "", "actor-1"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		err1 = j1.RecordApproval("exec-rmw-2", "ap-a", ApprovalGranted, "approver-a")
	}()
	go func() {
		defer wg.Done()
		err2 = j2.RecordApproval("exec-rmw-2", "ap-b", ApprovalGranted, "approver-b")
	}()
	wg.Wait()

	// Exactly zero or one conflict is acceptable: both succeeded means both
	// reads observed the latest version and both approvals are preserved;
	// one conflict means the losing writer gets an explicit error.
	conflictCount := 0
	if err1 != nil {
		if !errors.Is(err1, store.ErrConflict) {
			t.Fatalf("unexpected error for writer 1: %v", err1)
		}
		conflictCount++
	}
	if err2 != nil {
		if !errors.Is(err2, store.ErrConflict) {
			t.Fatalf("unexpected error for writer 2: %v", err2)
		}
		conflictCount++
	}
	if conflictCount > 1 {
		t.Fatalf("expected at most one conflict, got %d", conflictCount)
	}

	successCount := 2 - conflictCount
	rec, ok, _ := j1.Get("exec-rmw-2")
	if !ok {
		t.Fatal("execution not found")
	}
	if len(rec.Approvals) != successCount {
		t.Fatalf("expected %d approvals (no silent loss), got %d: %+v", successCount, len(rec.Approvals), rec.Approvals)
	}
}

// TestStaleVersionIsRejected verifies that a direct store Put with a stale
// version is rejected with VERSION_CONFLICT. This is the mechanism the
// journal relies on: putExecution carries the version from its read forward,
// never re-reads it, so a stale write can never appear valid.
func TestStaleVersionIsRejected(t *testing.T) {
	st := store.NewMemStore()
	j := New(st, fixedClock())
	if err := j.Accepted("exec-rmw-3", "biz-1", "", "actor-1"); err != nil {
		t.Fatal(err)
	}

	sr, err := st.Get(execKey("exec-rmw-3"))
	if err != nil || sr == nil {
		t.Fatalf("get store record: err=%v rec=%v", err, sr)
	}
	oldVersion := sr.Version

	// Advance the durable version through the journal.
	if err := j.Terminal("exec-rmw-3", StateCompleted, ""); err != nil {
		t.Fatal(err)
	}

	// A direct store Put carrying the pre-advance version must be rejected.
	sr.Version = oldVersion // stale
	sr.Data = []byte(`{"version":1,"id":"exec-rmw-3","business_id":"biz-1","state":"accepted"}`)
	if err := st.Put(sr); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected VERSION_CONFLICT for stale version Put, got %v", err)
	}
}

// TestRecordApprovalRetryAfterConflict verifies that a writer that receives
// VERSION_CONFLICT can re-read and retry successfully, preserving both updates.
// This exercises the conflict path directly by calling the journal's internal
// read and write methods with a stale version (same package access).
func TestRecordApprovalRetryAfterConflict(t *testing.T) {
	st := store.NewMemStore()
	j := New(st, fixedClock())
	if err := j.Accepted("exec-rmw-4", "biz-1", "", "actor-1"); err != nil {
		t.Fatal(err)
	}

	// Both reads observe the same version N.
	recA, versionA, ok, err := j.getExecution("exec-rmw-4")
	if err != nil || !ok {
		t.Fatalf("getExecution: err=%v ok=%v", err, ok)
	}
	recB, versionB, ok, err := j.getExecution("exec-rmw-4")
	if err != nil || !ok {
		t.Fatalf("getExecution: err=%v ok=%v", err, ok)
	}
	if versionA != versionB {
		t.Fatalf("expected both reads to observe version %d, got %d and %d", versionA, versionA, versionB)
	}

	// Writer A appends and writes with version N — succeeds.
	recA.Approvals = append(recA.Approvals, ApprovalDecision{
		ApprovalID: "ap-a", Outcome: ApprovalGranted, DecidedBy: "approver-a", DecidedAt: j.now().UTC(),
	})
	recA.UpdatedAt = j.now().UTC()
	if err := j.putExecution(recA, versionA); err != nil {
		t.Fatalf("writer A putExecution: %v", err)
	}

	// Writer B appends (without A's approval) and writes with the stale version N — conflict.
	recB.Approvals = append(recB.Approvals, ApprovalDecision{
		ApprovalID: "ap-b", Outcome: ApprovalGranted, DecidedBy: "approver-b", DecidedAt: j.now().UTC(),
	})
	recB.UpdatedAt = j.now().UTC()
	if err := j.putExecution(recB, versionB); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected VERSION_CONFLICT for stale write, got %v", err)
	}

	// Retry: re-read (now sees A's approval at version N+1), append B's approval, write.
	recB2, versionB2, ok, err := j.getExecution("exec-rmw-4")
	if err != nil || !ok {
		t.Fatalf("retry getExecution: err=%v ok=%v", err, ok)
	}
	recB2.Approvals = append(recB2.Approvals, ApprovalDecision{
		ApprovalID: "ap-b", Outcome: ApprovalGranted, DecidedBy: "approver-b", DecidedAt: j.now().UTC(),
	})
	recB2.UpdatedAt = j.now().UTC()
	if err := j.putExecution(recB2, versionB2); err != nil {
		t.Fatalf("retry putExecution: %v", err)
	}

	rec, ok, _ := j.Get("exec-rmw-4")
	if !ok {
		t.Fatal("execution not found")
	}
	if len(rec.Approvals) != 2 {
		t.Fatalf("expected 2 approvals after retry, got %d: %+v", len(rec.Approvals), rec.Approvals)
	}
}

// MemStore backend parity for the same concurrency contract.
func TestConcurrentRecordApprovalMemStoreParity(t *testing.T) {
	st := store.NewMemStore()
	j := New(st, fixedClock())
	if err := j.Accepted("exec-rmw-5", "biz-1", "", "actor-1"); err != nil {
		t.Fatal(err)
	}

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := j.RecordApproval("exec-rmw-5",
				fmt.Sprintf("ap-%d", i), ApprovalGranted, fmt.Sprintf("approver-%d", i)); err != nil {
				t.Errorf("RecordApproval[%d] should succeed: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	rec, ok, _ := j.Get("exec-rmw-5")
	if !ok {
		t.Fatal("execution not found")
	}
	if len(rec.Approvals) != n {
		t.Fatalf("expected %d approvals, got %d", n, len(rec.Approvals))
	}
}
