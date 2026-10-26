package store

import (
	"errors"
	"testing"
)

// ---- v30: soft-delete visibility parity for Get ---------------------------
//
// The Record envelope models deletion as a STATUS (RecordStatusDeleted) and
// Delete is documented as "soft delete sets status to deleted" — physical
// removal is a separate operation (DeleteBatchHard). A record that has been
// soft-deleted therefore still EXISTS, and Get must return it with its status,
// exactly as List does when asked for deleted records explicitly.
//
// These run the same operations against both backends and require identical
// answers. They fail today: MemStore hides soft-deleted records, FileStore
// returns them.

func parityBackends(t *testing.T) map[string]Store {
	t.Helper()
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("open filestore: %v", err)
	}
	return map[string]Store{"MemStore": NewMemStore(), "FileStore": fs}
}

func seedActive(t *testing.T, st Store, id string) {
	t.Helper()
	if err := st.Put(&Record{ID: id, Type: RecordTypeMemory, Status: RecordStatusActive, BusinessID: "b1"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// After a soft delete, Get must return the record carrying status deleted —
// never ErrNotFound, which would claim the record never existed.
func TestGetReturnsSoftDeletedRecordsOnBothBackends(t *testing.T) {
	for name, st := range parityBackends(t) {
		t.Run(name, func(t *testing.T) {
			seedActive(t, st, "k")
			if err := st.Delete("k"); err != nil {
				t.Fatalf("delete: %v", err)
			}
			rec, err := st.Get("k")
			if err != nil {
				t.Fatalf("a soft-deleted record must still be readable on %s, got %v", name, err)
			}
			if rec == nil {
				t.Fatalf("a soft-deleted record must not read as absent on %s", name)
			}
			if rec.Status != RecordStatusDeleted {
				t.Fatalf("the deleted status must be preserved on %s, got %q", name, rec.Status)
			}
			if rec.ID != "k" {
				t.Fatalf("the record identity must be preserved on %s, got %q", name, rec.ID)
			}
		})
	}
}

// A missing record keeps its own distinct answer on both backends and must
// never be confused with a soft-deleted one.
func TestMissingAndSoftDeletedStayDistinctOnBothBackends(t *testing.T) {
	for name, st := range parityBackends(t) {
		t.Run(name, func(t *testing.T) {
			seedActive(t, st, "k")
			if err := st.Delete("k"); err != nil {
				t.Fatal(err)
			}
			missing, missingErr := st.Get("never-existed")
			if missing != nil {
				t.Fatalf("a missing record must not be returned on %s", name)
			}
			deleted, deletedErr := st.Get("k")
			if deletedErr != nil {
				t.Fatalf("deleted read on %s: %v", name, deletedErr)
			}
			if deleted == nil {
				t.Fatalf("a soft-deleted record must be returned on %s", name)
			}
			// The one deliberate remaining difference: how absence is signalled.
			// It must never be signalled as a non-nil record.
			if missingErr != nil && !errors.Is(missingErr, ErrNotFound) {
				t.Fatalf("absence must stay a not-found signal on %s, got %v", name, missingErr)
			}
		})
	}
}

// List keeps its documented filtering: deleted records are hidden by default
// and visible when explicitly requested, identically on both backends.
func TestListDeletedFilteringParity(t *testing.T) {
	for name, st := range parityBackends(t) {
		t.Run(name, func(t *testing.T) {
			seedActive(t, st, "live")
			seedActive(t, st, "gone")
			if err := st.Delete("gone"); err != nil {
				t.Fatal(err)
			}
			def, err := st.List(Filter{Type: RecordTypeMemory})
			if err != nil {
				t.Fatalf("default list: %v", err)
			}
			if len(def) != 1 || def[0].ID != "live" {
				t.Fatalf("the default list must hide deleted records on %s, got %v", name, ids(def))
			}
			withDeleted, err := st.List(Filter{Type: RecordTypeMemory, Status: RecordStatusDeleted})
			if err != nil {
				t.Fatalf("deleted list: %v", err)
			}
			if len(withDeleted) != 1 || withDeleted[0].ID != "gone" {
				t.Fatalf("an explicit deleted filter must return it on %s, got %v", name, ids(withDeleted))
			}
		})
	}
}

// A FileStore reopened from disk must keep the same soft-delete visibility.
func TestFileStoreReopenPreservesSoftDeletedVisibility(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	seedActive(t, fs, "gone")
	if err := fs.Delete("gone"); err != nil {
		t.Fatal(err)
	}
	closeStore(t, fs)
	fs2, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	rec, err := fs2.Get("gone")
	if err != nil || rec == nil || rec.Status != RecordStatusDeleted {
		t.Fatalf("soft-delete visibility must survive a reopen: rec=%v err=%v", rec, err)
	}
	// And the default listing still hides it after the reopen.
	listed, err := fs2.List(Filter{Type: RecordTypeMemory})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("the default list must still hide deleted records after reopen, got %v", ids(listed))
	}
}

func ids(recs []*Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.ID)
	}
	return out
}
