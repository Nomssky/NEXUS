package store

import (
	"errors"
	"testing"
	"time"
)

// ---- v32: mutation contract (Put version semantics + repeated delete) -----
//
// Version 0 and repeated delete were the two remaining backend differences.
// Both are settled here against real backends:
//
//	Version 0 — no caller ever constructs a Record with an explicit Version;
//	the only assignment is "prev.Version" when a prior record exists. So 0
//	means "create; I have no prior version", and a zero-version Put against an
//	EXISTING id must be refused rather than silently overwriting it.
//
//	Repeated delete — deleting an already-deleted record is a no-op: it is not
//	a new update, so it must not advance Version or updated_at. A retried
//	delete therefore cannot invalidate a client that read the version the first
//	delete produced. (core/cancel.go already treats a repeat cancel the same
//	way: "idempotent repeat cancel".)

func mutationStores(t *testing.T) map[string]Store {
	t.Helper()
	tick := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := func() time.Time {
		tick = tick.Add(time.Hour)
		return tick
	}
	fs, err := NewFileStoreWithClock(t.TempDir(), clk)
	if err != nil {
		t.Fatalf("open filestore: %v", err)
	}
	return map[string]Store{"MemStore": NewMemStoreWithClock(clk), "FileStore": fs}
}

func active(id string) *Record {
	return &Record{ID: id, Type: RecordTypeMemory, Status: RecordStatusActive}
}

// snap captures the observable mutation fields as scalars.
//
// It must not hold on to a *Record: MemStore.Get returns its INTERNAL pointer
// (FileStore.Get returns a copy), so a retained pointer would be mutated in
// place by the very operation under test and the comparison would pass for the
// wrong reason.
type snap struct {
	version   int
	status    RecordStatus
	createdAt time.Time
	updatedAt time.Time
	data      string
}

func snapshot(t *testing.T, st Store, id string) snap {
	t.Helper()
	r := mustGet(t, st, id)
	return snap{
		version: r.Version, status: r.Status,
		createdAt: r.CreatedAt, updatedAt: r.UpdatedAt, data: string(r.Data),
	}
}

// A zero-version Put is a CREATE. It must succeed on an absent id and be
// refused on an id that already exists, live or deleted.
func TestZeroVersionIsCreateOnlyOnBothBackends(t *testing.T) {
	t.Run("absent id creates", func(t *testing.T) {
		for name, st := range mutationStores(t) {
			if err := st.Put(active("new")); err != nil {
				t.Fatalf("a zero-version Put must create on %s, got %v", name, err)
			}
			if v := mustGet(t, st, "new").Version; v != 1 {
				t.Fatalf("a created record starts at version 1 on %s, got %d", name, v)
			}
		}
	})
	t.Run("live id is refused", func(t *testing.T) {
		for name, st := range mutationStores(t) {
			if err := st.Put(active("live")); err != nil {
				t.Fatal(err)
			}
			before := mustGet(t, st, "live")
			err := st.Put(&Record{ID: "live", Type: RecordTypeMemory,
				Status: RecordStatusActive, Data: []byte("blind")})
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("a zero-version Put must not overwrite a live record on %s, got %v", name, err)
			}
			after := mustGet(t, st, "live")
			if after.Version != before.Version || len(after.Data) != 0 {
				t.Fatalf("the refused blind write must mutate nothing on %s: %+v", name, after)
			}
		}
	})
	t.Run("deleted id is refused", func(t *testing.T) {
		for name, st := range mutationStores(t) {
			if err := st.Put(active("gone")); err != nil {
				t.Fatal(err)
			}
			if err := st.Delete("gone"); err != nil {
				t.Fatal(err)
			}
			tomb := mustGet(t, st, "gone")
			err := st.Put(&Record{ID: "gone", Type: RecordTypeMemory, Status: RecordStatusActive})
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("a zero-version Put must not resurrect a tombstone on %s, got %v", name, err)
			}
			after := mustGet(t, st, "gone")
			if after.Version != tomb.Version || after.Status != RecordStatusDeleted {
				t.Fatalf("the refused resurrection must mutate nothing on %s: %+v", name, after)
			}
		}
	})
}

// A creation starts at version 1 regardless of any version the caller sent, on
// both backends.
func TestCreationAlwaysStartsAtVersionOne(t *testing.T) {
	for name, st := range mutationStores(t) {
		if err := st.Put(&Record{ID: "k", Type: RecordTypeMemory,
			Status: RecordStatusActive, Version: 5}); err != nil {
			t.Fatalf("create on %s: %v", name, err)
		}
		if v := mustGet(t, st, "k").Version; v != 1 {
			t.Fatalf("a created record must start at version 1 on %s, got %d", name, v)
		}
	}
}

// A repeated soft delete is a no-op: no version increment, no updated_at
// refresh, no status change. This is what makes a retried delete safe.
func TestRepeatedDeleteIsANoOpOnBothBackends(t *testing.T) {
	for name, st := range mutationStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := st.Put(active("k")); err != nil {
				t.Fatal(err)
			}
			if err := st.Delete("k"); err != nil {
				t.Fatal(err)
			}
			first := snapshot(t, st, "k")

			if err := st.Delete("k"); err != nil {
				t.Fatalf("repeating a delete must succeed on %s, got %v", name, err)
			}
			second := snapshot(t, st, "k")
			if second.version != first.version {
				t.Fatalf("a repeated delete must not increment the version on %s: %d -> %d",
					name, first.version, second.version)
			}
			if !second.updatedAt.Equal(first.updatedAt) {
				t.Fatalf("a repeated delete must not refresh updated_at on %s: %v -> %v",
					name, first.updatedAt, second.updatedAt)
			}
			if second.status != RecordStatusDeleted || !second.createdAt.Equal(first.createdAt) {
				t.Fatalf("a repeated delete must not change the record on %s: %+v", name, second)
			}
		})
	}
}

// The batch form is the same operation and must be equally idempotent.
func TestRepeatedDeleteBatchIsANoOpOnBothBackends(t *testing.T) {
	for name, st := range mutationStores(t) {
		t.Run(name, func(t *testing.T) {
			for _, id := range []string{"a", "b"} {
				if err := st.Put(active(id)); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.DeleteBatch([]string{"a", "b"}); err != nil {
				t.Fatal(err)
			}
			firstA := snapshot(t, st, "a")
			if err := st.DeleteBatch([]string{"a", "b"}); err != nil {
				t.Fatalf("repeating a batch delete must succeed on %s, got %v", name, err)
			}
			secondA := snapshot(t, st, "a")
			if secondA.version != firstA.version || !secondA.updatedAt.Equal(firstA.updatedAt) {
				t.Fatalf("a repeated batch delete must be a no-op on %s: %+v -> %+v",
					name, firstA, secondA)
			}
		})
	}
}

// A client's version, read right after the first delete, stays valid across a
// retried delete. This is the retry-safety property the contract depends on.
func TestRetriedDeleteDoesNotInvalidateTheReadersVersion(t *testing.T) {
	for name, st := range mutationStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := st.Put(active("k")); err != nil {
				t.Fatal(err)
			}
			if err := st.Delete("k"); err != nil {
				t.Fatal(err)
			}
			client := snapshot(t, st, "k").version
			if err := st.Delete("k"); err != nil { // the retry
				t.Fatal(err)
			}
			if got := mustGet(t, st, "k").Version; got != client {
				t.Fatalf("a retried delete must not move the version on %s: client holds %d, store is %d",
					name, client, got)
			}
			// And a checked resurrection at that version still works.
			if err := st.Put(&Record{ID: "k", Type: RecordTypeMemory,
				Status: RecordStatusActive, Version: client}); err != nil {
				t.Fatalf("a checked recreation at the client's version must succeed on %s, got %v", name, err)
			}
		})
	}
}

// Idempotence must survive a restart: reopening must not resurrect the
// distinction, and a repeat delete after reopen is still a no-op.
func TestRepeatedDeleteStaysNoOpAfterFileStoreReopen(t *testing.T) {
	dir := t.TempDir()
	tick := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := func() time.Time { tick = tick.Add(time.Hour); return tick }
	fs, err := NewFileStoreWithClock(dir, clk)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Put(active("k")); err != nil {
		t.Fatal(err)
	}
	if err := fs.Delete("k"); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, fs, "k")

	fs2, err := NewFileStoreWithClock(dir, clk)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs2.Delete("k"); err != nil {
		t.Fatalf("a repeat delete after reopen must succeed: %v", err)
	}
	after := snapshot(t, fs2, "k")
	if after.version != before.version || !after.updatedAt.Equal(before.updatedAt) {
		t.Fatalf("a repeat delete after reopen must be a no-op: %+v -> %+v", before, after)
	}
}
