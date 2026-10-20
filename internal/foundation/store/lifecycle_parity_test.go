package store

import (
	"errors"
	"testing"
	"time"
)

// ---- v31: Create -> Delete -> Recreate lifecycle parity --------------------
//
// Two independent rules govern this lifecycle, both stated by contract:
//
//	version   — SCHEMA_COMMON 3.9: "Starts at 1, increments on each update."
//	            A soft delete mutates status and updated_at, so it is an update
//	            and must increment. The version is therefore monotonic across a
//	            tombstone and recreation continues it, never restarting it.
//	createdAt — AGENT_MEMORY_CONTEXT 9: an update "preserv[es] created_at".
//	            Creation time is set once, by the store, and is immutable for
//	            the record's identity — including across a tombstone.
//
// Both backends must agree on both. These run the identical sequence against
// each and compare.

// lifecycleStores returns both backends over one advancing clock, so timestamps
// are deterministic and any divergence is a policy difference, not timing.
func lifecycleStores(t *testing.T) map[string]Store {
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

func mustGet(t *testing.T, st Store, id string) *Record {
	t.Helper()
	rec, err := st.Get(id)
	if err != nil || rec == nil {
		t.Fatalf("get %q: rec=%v err=%v", id, rec, err)
	}
	return rec
}

// A successful soft delete is an update and must increment the version.
func TestDeleteIncrementsVersionOnBothBackends(t *testing.T) {
	for name, st := range lifecycleStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := st.Put(&Record{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
				t.Fatal(err)
			}
			if v := mustGet(t, st, "k").Version; v != 1 {
				t.Fatalf("a new record starts at version 1, got %d", v)
			}
			if err := st.Delete("k"); err != nil {
				t.Fatal(err)
			}
			got := mustGet(t, st, "k")
			if got.Version != 2 {
				t.Fatalf("a soft delete must increment the version on %s, got %d", name, got.Version)
			}
			if got.Status != RecordStatusDeleted {
				t.Fatalf("status must be deleted on %s, got %q", name, got.Status)
			}
		})
	}
}

// A batch delete is the same kind of update and must increment too.
func TestDeleteBatchIncrementsVersionOnBothBackends(t *testing.T) {
	for name, st := range lifecycleStores(t) {
		t.Run(name, func(t *testing.T) {
			for _, id := range []string{"a", "b"} {
				if err := st.Put(&Record{ID: id, Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.DeleteBatch([]string{"a", "b"}); err != nil {
				t.Fatalf("delete batch on %s: %v", name, err)
			}
			for _, id := range []string{"a", "b"} {
				rec := mustGet(t, st, id)
				if rec.Version != 2 || rec.Status != RecordStatusDeleted {
					t.Fatalf("%s: batch delete must mark deleted at version 2, got %q at %d", id, rec.Status, rec.Version)
				}
			}
		})
	}
}

// Recreation continues the record's identity: the version keeps climbing
// (never restarts) and created_at is preserved from the original creation.
func TestRecreateContinuesVersionAndPreservesCreatedAt(t *testing.T) {
	for name, st := range lifecycleStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := st.Put(&Record{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
				t.Fatal(err)
			}
			original := mustGet(t, st, "k")
			if err := st.Delete("k"); err != nil {
				t.Fatal(err)
			}
			tomb := mustGet(t, st, "k")
			// Recreate the way every real caller does: read the current version
			// and carry it, so the write is a checked update (not a blind write —
			// see the Version 0 note above).
			if err := st.Put(&Record{
				ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
				Version: tomb.Version, Data: []byte("second"),
			}); err != nil {
				t.Fatalf("recreate on %s: %v", name, err)
			}
			again := mustGet(t, st, "k")
			if again.Version != tomb.Version+1 {
				t.Fatalf("recreation must continue the version on %s: tombstone %d -> %d",
					name, tomb.Version, again.Version)
			}
			if !again.CreatedAt.Equal(original.CreatedAt) {
				t.Fatalf("created_at must be preserved across recreation on %s: %v -> %v",
					name, original.CreatedAt, again.CreatedAt)
			}
			if string(again.Data) != "second" {
				t.Fatalf("content must be fresh on %s, got %q", name, again.Data)
			}
			if again.Status != RecordStatusActive {
				t.Fatalf("a recreated record must be live on %s, got %q", name, again.Status)
			}
		})
	}
}

// An update of a live record preserves created_at and refreshes updated_at.
func TestUpdatePreservesCreatedAtOnBothBackends(t *testing.T) {
	for name, st := range lifecycleStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := st.Put(&Record{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
				t.Fatal(err)
			}
			first := mustGet(t, st, "k")
			if err := st.Put(&Record{
				ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
				Version: first.Version, Data: []byte("updated"),
			}); err != nil {
				t.Fatalf("update on %s: %v", name, err)
			}
			after := mustGet(t, st, "k")
			if after.Version != first.Version+1 {
				t.Fatalf("an update must increment the version on %s: %d -> %d", name, first.Version, after.Version)
			}
			if !after.CreatedAt.Equal(first.CreatedAt) {
				t.Fatalf("an update must preserve created_at on %s: %v -> %v", name, first.CreatedAt, after.CreatedAt)
			}
			if !after.UpdatedAt.After(first.UpdatedAt) {
				t.Fatalf("an update must refresh updated_at on %s: %v -> %v", name, first.UpdatedAt, after.UpdatedAt)
			}
		})
	}
}

// Optimistic concurrency: a genuinely stale version is refused on both
// backends and mutates nothing.
//
// NOTE on Version 0: FileStore treats a zero version as "no check" while
// MemStore requires a strict match, so a zero-version write to an EXISTING
// record is accepted on one backend and refused on the other. That asymmetry is
// pre-existing, already documented and deliberately accommodated at the call
// site (execjournal putExecution: "MemStore (strict equality) and FileStore
// (0 = no check)"), so it is deliberately NOT asserted here — it is a separate
// open contract question, not part of this lifecycle.
func TestStaleVersionWriteIsRefusedOnBothBackends(t *testing.T) {
	for name, st := range lifecycleStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := st.Put(&Record{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
				t.Fatal(err)
			}
			first := mustGet(t, st, "k")
			// Move the record forward so version 1 becomes genuinely stale.
			if err := st.Put(&Record{
				ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
				Version: first.Version, Data: []byte("v2"),
			}); err != nil {
				t.Fatalf("advance on %s: %v", name, err)
			}
			before := mustGet(t, st, "k")

			err := st.Put(&Record{
				ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
				Version: first.Version, Data: []byte("stale"),
			})
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("a stale write must be refused on %s, got %v", name, err)
			}
			after := mustGet(t, st, "k")
			if after.Version != before.Version || !after.CreatedAt.Equal(before.CreatedAt) ||
				!after.UpdatedAt.Equal(before.UpdatedAt) || string(after.Data) != "v2" {
				t.Fatalf("a refused write must mutate nothing on %s: %+v", name, after)
			}
		})
	}
}

// A failed operation must leave version and timestamps untouched, on either
// backend, for a delete of an unknown id.
func TestFailedDeleteMutatesNothing(t *testing.T) {
	for name, st := range lifecycleStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := st.Put(&Record{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
				t.Fatal(err)
			}
			before := mustGet(t, st, "k")
			if err := st.Delete("does-not-exist"); err == nil {
				t.Fatalf("deleting an unknown id must fail on %s", name)
			}
			after := mustGet(t, st, "k")
			if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) ||
				after.Status != before.Status {
				t.Fatalf("a failed delete must mutate nothing on %s: %+v", name, after)
			}
		})
	}
}

// Repeated cycles keep both backends on the same monotone version track.
func TestRepeatedDeleteRecreateCyclesAgree(t *testing.T) {
	for name, st := range lifecycleStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := st.Put(&Record{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
				t.Fatal(err)
			}
			created := mustGet(t, st, "k").CreatedAt
			want := 1
			for cycle := 1; cycle <= 3; cycle++ {
				if err := st.Delete("k"); err != nil {
					t.Fatalf("cycle %d delete on %s: %v", cycle, name, err)
				}
				if got := mustGet(t, st, "k").Version; got != want+1 {
					t.Fatalf("cycle %d: delete must reach version %d on %s, got %d", cycle, want+1, name, got)
				}
				want++
				cur := mustGet(t, st, "k")
				if err := st.Put(&Record{
					ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
					Version: cur.Version, Data: []byte("gen"),
				}); err != nil {
					t.Fatalf("cycle %d recreate on %s: %v", cycle, name, err)
				}
				rec := mustGet(t, st, "k")
				if rec.Version != want+1 {
					t.Fatalf("cycle %d: recreate must reach version %d on %s, got %d", cycle, want+1, name, rec.Version)
				}
				if !rec.CreatedAt.Equal(created) {
					t.Fatalf("cycle %d: created_at must stay preserved on %s", cycle, name)
				}
				want++
			}
		})
	}
}

// The lifecycle survives a FileStore reopen: the tombstone and its version are
// durable, so a restart cannot resurrect a stale generation.
func TestFileStoreReopenPreservesLifecycle(t *testing.T) {
	dir := t.TempDir()
	tick := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := func() time.Time { tick = tick.Add(time.Hour); return tick }
	fs, err := NewFileStoreWithClock(dir, clk)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Put(&Record{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := fs.Delete("k"); err != nil {
		t.Fatal(err)
	}
	before := mustGet(t, fs, "k")

	fs2, err := NewFileStoreWithClock(dir, clk)
	if err != nil {
		t.Fatal(err)
	}
	after := mustGet(t, fs2, "k")
	if after.Version != before.Version || after.Status != RecordStatusDeleted {
		t.Fatalf("the tombstone must survive a reopen intact: %+v", after)
	}
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("created_at must survive a reopen: %v -> %v", before.CreatedAt, after.CreatedAt)
	}
	// Recreation after a restart continues the same version track. The caller
	// carries the version it read, so this is a checked update: under the v32
	// contract a zero version is a CREATE and may not overwrite a tombstone.
	if err := fs2.Put(&Record{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
		Version: after.Version}); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, fs2, "k").Version; got != before.Version+1 {
		t.Fatalf("post-restart recreation must continue the version, got %d", got)
	}
}
