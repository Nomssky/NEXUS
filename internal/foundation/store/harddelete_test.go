package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Physical batch delete (contracts/EXECUTION_JOURNAL_RETENTION_CONTRACTS.md §3).
// The crash matrix is deterministic: each case builds the exact on-disk state at
// a failure boundary and reopens the store. No sleeps.

func putExec(t *testing.T, fs *FileStore, id, data string) {
	t.Helper()
	if err := fs.Put(&Record{ID: id, Type: "execution", Status: RecordStatusActive, Data: []byte(data)}); err != nil {
		t.Fatalf("put %s: %v", id, err)
	}
}

func execFile(dir, id string) string {
	return filepath.Join(dir, "execution", id+".json")
}

func writeBatchJournalFile(t *testing.T, dir string, recs []Record) {
	t.Helper()
	data, err := json.Marshal(&batchJournal{Version: 1, Records: recs})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, batchJournalName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func journalPresent(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, batchJournalName))
	return err == nil
}

// A committed hard batch reclaims the files (unlike a soft batch, which leaves
// the record file marked deleted) and survives a restart.
func TestFileStoreHardDeleteReclaimsFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fs, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	putExec(t, fs, "rec-a", `{"v":1}`)
	putExec(t, fs, "rec-b", `{"v":2}`)

	if err := fs.DeleteBatchHard([]string{"rec-a", "rec-b"}); err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	if _, err := os.Stat(execFile(dir, "rec-a")); !os.IsNotExist(err) {
		t.Fatal("a committed hard batch must remove the file from disk")
	}
	if _, err := os.Stat(execFile(dir, "rec-b")); !os.IsNotExist(err) {
		t.Fatal("a committed hard batch must remove the file from disk")
	}
	if journalPresent(dir) {
		t.Fatal("a committed hard batch must not leave a journal")
	}
	if r, _ := fs.Get("rec-a"); r != nil {
		t.Fatal("the index must drop a hard-deleted record")
	}

	reopened, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if all, _ := reopened.List(Filter{Type: "execution"}); len(all) != 0 {
		t.Fatalf("hard-deleted records must stay gone across restart, got %d", len(all))
	}
}

// A hard batch is all-or-nothing: an unknown id refuses the whole set.
func TestFileStoreHardDeleteIsAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	putExec(t, fs, "rec-a", `{"v":1}`)
	if err := fs.DeleteBatchHard([]string{"rec-a", "nope"}); err == nil {
		t.Fatal("an unknown id must fail the whole batch")
	}
	if _, err := os.Stat(execFile(dir, "rec-a")); err != nil {
		t.Fatalf("a refused batch must not remove any file: %v", err)
	}
	if r, _ := fs.Get("rec-a"); r == nil {
		t.Fatal("a refused batch must not drop any index entry")
	}
	if err := fs.DeleteBatchHard(nil); err != nil {
		t.Fatalf("empty batch must be a no-op: %v", err)
	}
}

// The crash matrix: at every boundary, a reopen either rolls the batch back
// (files present) or observes it committed (files absent). A corrupt journal
// aborts the open (fail closed).
func TestFileStoreHardDeleteCrashMatrix(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name         string
		setup        func(t *testing.T, dir string, journal []Record)
		wantPresent  bool // both records present after reopen
		wantOpenFail bool
	}{
		{
			name:        "before journal publication",
			setup:       func(t *testing.T, dir string, journal []Record) {}, // files present, no journal
			wantPresent: true,
		},
		{
			name: "journal published, before any unlink",
			setup: func(t *testing.T, dir string, journal []Record) {
				writeBatchJournalFile(t, dir, journal)
			},
			wantPresent: true,
		},
		{
			name: "midway through unlinks",
			setup: func(t *testing.T, dir string, journal []Record) {
				writeBatchJournalFile(t, dir, journal)
				if err := os.Remove(execFile(dir, "rec-a")); err != nil {
					t.Fatal(err)
				}
			},
			wantPresent: true,
		},
		{
			name: "all removed, before journal cleanup",
			setup: func(t *testing.T, dir string, journal []Record) {
				writeBatchJournalFile(t, dir, journal)
				_ = os.Remove(execFile(dir, "rec-a"))
				_ = os.Remove(execFile(dir, "rec-b"))
			},
			wantPresent: true,
		},
		{
			name: "committed (journal removed, files gone)",
			setup: func(t *testing.T, dir string, journal []Record) {
				_ = os.Remove(execFile(dir, "rec-a"))
				_ = os.Remove(execFile(dir, "rec-b"))
			},
			wantPresent: false,
		},
		{
			name: "corrupt journal",
			setup: func(t *testing.T, dir string, journal []Record) {
				if err := os.WriteFile(filepath.Join(dir, batchJournalName), []byte(`{"version":1,"records":[{"id":""}]}`), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantOpenFail: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fs, err := NewFileStoreWithClock(dir, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			putExec(t, fs, "rec-a", `{"v":1}`)
			putExec(t, fs, "rec-b", `{"v":2}`)
			ra, _ := fs.Get("rec-a")
			rb, _ := fs.Get("rec-b")
			journal := []Record{*ra, *rb}

			tc.setup(t, dir, journal)

			reopened, err := NewFileStoreWithClock(dir, func() time.Time { return now })
			if tc.wantOpenFail {
				if err == nil {
					t.Fatal("a corrupt journal must abort the open")
				}
				return
			}
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			got, _ := reopened.List(Filter{Type: "execution"})
			if tc.wantPresent && len(got) != 2 {
				t.Fatalf("expected the batch rolled back (2 records), got %d", len(got))
			}
			if !tc.wantPresent && len(got) != 0 {
				t.Fatalf("expected the batch committed (0 records), got %d", len(got))
			}
			if journalPresent(dir) {
				t.Fatal("recovery must clear the journal")
			}
		})
	}
}

// MemStore hard delete drops the records outright and is all-or-nothing.
func TestMemStoreHardDelete(t *testing.T) {
	s := NewMemStore()
	for _, id := range []string{"r1", "r2"} {
		if err := s.Put(&Record{ID: id, Type: "execution", Status: RecordStatusActive, Data: []byte(`{"v":1}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteBatchHard([]string{"r1", "nope"}); err == nil {
		t.Fatal("an unknown id must fail the whole batch")
	}
	if r, _ := s.Get("r1"); r == nil {
		t.Fatal("a refused batch must not remove any record")
	}
	if err := s.DeleteBatchHard([]string{"r1", "r2"}); err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	if r, _ := s.Get("r1"); r != nil {
		t.Fatal("a hard-deleted record must be gone")
	}
	if err := s.DeleteBatchHard(nil); err != nil {
		t.Fatalf("empty batch must be a no-op: %v", err)
	}
}
