package store

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TEST-M3-001: Store interface compliance
func TestMemStoreInterface(t *testing.T) {
	var _ Store = (*MemStore)(nil)
}

// TEST-M3-002: Put and Get
func TestMemStorePutGet(t *testing.T) {
	s := NewMemStore()
	now := time.Now()
	record := &Record{
		ID:   "rec-1",
		Type: RecordTypeWorkflow,
		Data: []byte(`{"status":"running"}`),
	}

	err := s.Put(record)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := s.Get("rec-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.ID != "rec-1" {
		t.Errorf("expected ID rec-1, got %v", got.ID)
	}
	if got.Type != RecordTypeWorkflow {
		t.Errorf("expected type workflow, got %v", got.Type)
	}
	if got.Version != 1 {
		t.Errorf("expected version 1, got %d", got.Version)
	}
	if got.CreatedAt.Before(now) {
		t.Errorf("expected CreatedAt >= now")
	}
}

// TEST-M3-003: Get non-existent record
func TestMemStoreGetNotFound(t *testing.T) {
	s := NewMemStore()
	_, err := s.Get("non-existent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TEST-M3-004: Optimistic concurrency (version conflict)
func TestMemStoreVersionConflict(t *testing.T) {
	s := NewMemStore()
	record := &Record{
		ID:   "rec-1",
		Type: RecordTypeTask,
	}

	if err := s.Put(record); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get the record (version is now 1)
	got, _ := s.Get("rec-1")

	// Simulate another writer updating the same record
	otherWriter := &Record{
		ID:      "rec-1",
		Type:    RecordTypeTask,
		Version: 1,
	}
	s.Put(otherWriter)

	// Now try to update with stale version
	got.Data = []byte(`{"stale":"update"}`)
	err := s.Put(got) // got.Version is 1, but otherWriter already bumped to 2
	// Actually this should succeed because got.Version matches existing.Version
	// Let me think... the issue is that both got and otherWriter have Version 1
	// The first Put succeeds and bumps to 2, the second Put sees version mismatch

	// Let's restructure: put with version 0 (initial), then try to put with version 0 again
	s2 := NewMemStore()
	r1 := &Record{ID: "r1", Type: RecordTypeTask}
	s2.Put(r1) // version becomes 1

	r2 := &Record{ID: "r1", Type: RecordTypeTask, Version: 0} // stale version
	err = s2.Put(r2)
	if err != ErrConflict {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

// TEST-M3-005: Successful update with correct version
func TestMemStoreUpdate(t *testing.T) {
	s := NewMemStore()
	record := &Record{
		ID:   "rec-1",
		Type: RecordTypeTask,
	}

	if err := s.Put(record); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get, modify, put back with correct version
	got, _ := s.Get("rec-1")
	got.Data = []byte(`{"status":"completed"}`)
	if err := s.Put(got); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify update
	got2, _ := s.Get("rec-1")
	if string(got2.Data) != `{"status":"completed"}` {
		t.Errorf("expected updated data, got %v", string(got2.Data))
	}
	if got2.Version != 2 {
		t.Errorf("expected version 2, got %d", got2.Version)
	}
}

// TEST-M3-006: Delete (soft delete)
func TestMemStoreDelete(t *testing.T) {
	s := NewMemStore()
	record := &Record{
		ID:   "rec-1",
		Type: RecordTypeTask,
	}

	s.Put(record)
	err := s.Delete("rec-1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// A soft delete marks the record; it does not remove it. Get therefore
	// still returns it, carrying status deleted. Visibility narrowing to live
	// records is List's job (v30 soft-delete contract). Asserting the status is
	// stricter than asserting an error: it proves the record survived as a
	// tombstone rather than silently vanishing.
	rec, err := s.Get("rec-1")
	if err != nil {
		t.Fatalf("a soft-deleted record must still be readable: %v", err)
	}
	if rec == nil {
		t.Fatal("a soft-deleted record must not vanish")
	}
	if rec.Status != RecordStatusDeleted {
		t.Errorf("expected status %q after delete, got %q", RecordStatusDeleted, rec.Status)
	}
	if rec.ID != "rec-1" {
		t.Errorf("the tombstone must keep its identity, got %q", rec.ID)
	}
}

// TEST-M3-007: Delete non-existent record
func TestMemStoreDeleteNotFound(t *testing.T) {
	s := NewMemStore()
	err := s.Delete("non-existent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TEST-M3-008: List with type filter
func TestMemStoreListFilterType(t *testing.T) {
	s := NewMemStore()
	s.Put(&Record{ID: "w1", Type: RecordTypeWorkflow})
	s.Put(&Record{ID: "t1", Type: RecordTypeTask})
	s.Put(&Record{ID: "w2", Type: RecordTypeWorkflow})

	records, err := s.List(Filter{Type: RecordTypeWorkflow})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 workflow records, got %d", len(records))
	}
}

// TEST-M3-009: List with business scope filter
func TestMemStoreListFilterScope(t *testing.T) {
	s := NewMemStore()
	s.Put(&Record{ID: "r1", Type: RecordTypeTask, BusinessID: "biz-1"})
	s.Put(&Record{ID: "r2", Type: RecordTypeTask, BusinessID: "biz-2"})
	s.Put(&Record{ID: "r3", Type: RecordTypeTask, BusinessID: "biz-1"})

	records, err := s.List(Filter{BusinessID: "biz-1"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records for biz-1, got %d", len(records))
	}
}

// TEST-M3-010: List with pagination
func TestMemStoreListPagination(t *testing.T) {
	s := NewMemStore()
	for i := 0; i < 10; i++ {
		s.Put(&Record{ID: fmt.Sprintf("r%d", i), Type: RecordTypeTask})
	}

	records, err := s.List(Filter{Type: RecordTypeTask, Limit: 3, Offset: 2})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}
}

// TEST-M3-011: Count
func TestMemStoreCount(t *testing.T) {
	s := NewMemStore()
	s.Put(&Record{ID: "r1", Type: RecordTypeWorkflow})
	s.Put(&Record{ID: "r2", Type: RecordTypeTask})
	s.Put(&Record{ID: "r3", Type: RecordTypeTask})

	count, err := s.Count(Filter{Type: RecordTypeTask})
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 tasks, got %d", count)
	}
}

// TEST-M3-012: Record expiration
func TestRecordExpiration(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)

	record := &Record{
		ID:        "rec-1",
		Type:      RecordTypeTask,
		ExpiresAt: &past,
	}

	if !record.IsExpired(now) {
		t.Error("expected record to be expired")
	}

	future := now.Add(time.Hour)
	record.ExpiresAt = &future
	if record.IsExpired(now) {
		t.Error("expected record not to be expired")
	}
}

// TEST-M3-013: Invalid record
func TestMemStoreInvalidRecord(t *testing.T) {
	s := NewMemStore()

	// nil record
	err := s.Put(nil)
	if err != ErrInvalidRecord {
		t.Errorf("expected ErrInvalidRecord for nil, got %v", err)
	}

	// empty ID
	err = s.Put(&Record{Type: RecordTypeTask})
	if err != ErrInvalidRecord {
		t.Errorf("expected ErrInvalidRecord for empty ID, got %v", err)
	}
}

// TEST-M3-014: Business isolation
func TestMemStoreBusinessIsolation(t *testing.T) {
	s := NewMemStore()
	s.Put(&Record{ID: "r1", Type: RecordTypeTask, BusinessID: "biz-1"})
	s.Put(&Record{ID: "r2", Type: RecordTypeTask, BusinessID: "biz-2"})

	// biz-1 should not see biz-2 records
	records, _ := s.List(Filter{BusinessID: "biz-1"})
	if len(records) != 1 {
		t.Errorf("expected 1 record for biz-1, got %d", len(records))
	}
	if records[0].ID != "r1" {
		t.Errorf("expected r1, got %v", records[0].ID)
	}
}

// TEST-STO-BATCH-1: DeleteBatch removes every id or none of them. A set the
// caller must remove as a unit (a governed memory delete) can never end up
// partially applied.
func TestMemStoreDeleteBatchIsAllOrNothing(t *testing.T) {
	s := NewMemStore()
	for _, id := range []string{"r1", "r2", "r3"} {
		if err := s.Put(&Record{ID: id, Type: RecordTypeMemory}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	if err := s.DeleteBatch([]string{"r1", "r2"}); err != nil {
		t.Fatalf("delete batch: %v", err)
	}
	for _, id := range []string{"r1", "r2"} {
		// "Deleted" means marked, not removed: the record is still there and
		// carries status deleted (v30 contract).
		rec, err := s.Get(id)
		if err != nil || rec == nil {
			t.Errorf("%s must survive as a tombstone: %v", id, err)
			continue
		}
		if rec.Status != RecordStatusDeleted {
			t.Errorf("%s must be marked deleted, got status %q", id, rec.Status)
		}
	}
	if rec, err := s.Get("r3"); err != nil || rec == nil {
		t.Errorf("a record outside the batch must survive: %v", err)
	}

	// An unknown id refuses the WHOLE batch: r3 stays.
	if err := s.DeleteBatch([]string{"r3", "nope"}); err == nil {
		t.Error("a batch with an unknown id must fail")
	}
	if rec, err := s.Get("r3"); err != nil || rec == nil {
		t.Errorf("a refused batch must not mutate anything: %v", err)
	}
	// Duplicates name the same record and stay a single removal.
	if err := s.DeleteBatch([]string{"r3", "r3"}); err != nil {
		t.Errorf("duplicate ids in a batch: %v", err)
	}
	if rec, err := s.Get("r3"); err != nil || rec == nil || rec.Status != RecordStatusDeleted {
		t.Errorf("r3 must be marked deleted: %v", err)
	}
	// An empty batch is a no-op, not an error.
	if err := s.DeleteBatch(nil); err != nil {
		t.Errorf("empty batch: %v", err)
	}
}

// ---- v28: documented backend semantics for Get ---------------------------
//
// The Store interface documents "Get retrieves a record by ID. Returns nil if
// not found". FileStore honors that. MemStore signals absence with ErrNotFound
// instead, and additionally hides soft-deleted records while FileStore returns
// them. These are pinned here so the difference is documented rather than
// implicit, and so it cannot drift unnoticed.
//
// v28 normalized the absence signal at the execution journal's internal
// boundary (getExecution/getInvocation), which makes every journal operation
// backend-independent without changing either store. Aligning MemStore with
// the interface is deliberately NOT done here: it would also force a decision
// on the undocumented soft-deleted case, which is a separate contract choice.

func TestBackendGetSemanticsArePinned(t *testing.T) {
	newBackends := func(t *testing.T) map[string]Store {
		t.Helper()
		fs, err := NewFileStore(t.TempDir())
		if err != nil {
			t.Fatalf("open filestore: %v", err)
		}
		return map[string]Store{"MemStore": NewMemStore(), "FileStore": fs}
	}

	for name, st := range newBackends(t) {
		t.Run(name+"/missing", func(t *testing.T) {
			rec, err := st.Get("absent")
			if rec != nil {
				t.Fatalf("a missing record must never be returned: %+v", rec)
			}
			// Absence must stay DISTINCT from a genuine failure: the only error
			// either backend may report here is the not-found signal, never an
			// I/O or corruption error.
			if err != nil && !errors.Is(err, ErrNotFound) {
				t.Fatalf("absence must not be reported as a failure: %v", err)
			}
		})
	}

	for name, st := range newBackends(t) {
		t.Run(name+"/existing", func(t *testing.T) {
			if err := st.Put(&Record{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
				t.Fatalf("put: %v", err)
			}
			rec, err := st.Get("k")
			if err != nil || rec == nil || rec.ID != "k" {
				t.Fatalf("an existing record must be returned: rec=%v err=%v", rec, err)
			}
		})
	}

	// Soft-deleted visibility is now AGREED (v30): the record still exists, so
	// Get returns it with status deleted on BOTH backends, and only List's
	// default filter narrows it away. This pins that agreement so the two
	// backends cannot drift apart again.
	t.Run("soft-deleted is returned by Get on both backends", func(t *testing.T) {
		ms := NewMemStore()
		fs, err := NewFileStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		for _, st := range []Store{ms, fs} {
			if err := st.Put(&Record{ID: "d", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
				t.Fatal(err)
			}
			if err := st.Delete("d"); err != nil {
				t.Fatal(err)
			}
			rec, err := st.Get("d")
			if err != nil || rec == nil || rec.Status != RecordStatusDeleted {
				t.Fatalf("a soft-deleted record must be returned as a tombstone: rec=%v err=%v", rec, err)
			}
			// And List is where the narrowing happens.
			live, err := st.List(Filter{Type: RecordTypeMemory})
			if err != nil {
				t.Fatal(err)
			}
			if len(live) != 0 {
				t.Fatalf("the default list must hide deleted records, got %v", ids(live))
			}
			deleted, err := st.List(Filter{Type: RecordTypeMemory, Status: RecordStatusDeleted})
			if err != nil {
				t.Fatal(err)
			}
			if len(deleted) != 1 {
				t.Fatalf("an explicit deleted filter must return it, got %v", ids(deleted))
			}
		}
	})
}
