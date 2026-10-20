package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// Physical batch delete (contracts/EXECUTION_JOURNAL_RETENTION_CONTRACTS.md §5):
// a backup taken over a store carrying a pending hard-delete transaction is
// internally consistent, and restore recovers the transaction safely before
// serving — it never resurrects a committed delete and never drops an
// interrupted one.

func seedHardDeleteStore(t *testing.T, dir string) {
	t.Helper()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for id, data := range map[string]string{"exec-a": `{"v":1}`, "exec-b": `{"v":2}`} {
		if err := st.Put(&store.Record{ID: id, Type: "execution", Status: store.RecordStatusActive, Data: []byte(data)}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
}

func TestRestoreRecoversInterruptedHardDeleteBatch(t *testing.T) {
	src := t.TempDir()
	seedHardDeleteStore(t, src)
	st, err := store.NewFileStore(src)
	if err != nil {
		t.Fatal(err)
	}
	ra, _ := st.Get("exec-a")
	rb, _ := st.Get("exec-b")

	// Simulate an interrupted hard batch: the journal holds both pre-batch
	// records and the first file is already removed.
	data, err := json.Marshal(map[string]any{"version": 1, "records": []store.Record{*ra, *rb}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".batch-journal.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(src, "execution", "exec-a.json")); err != nil {
		t.Fatal(err)
	}

	art := filepath.Join(t.TempDir(), "b.tar")
	if _, err := Create(src, art); err != nil {
		t.Fatalf("create: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(art, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	// Recovery on reopen rolled the interrupted batch back: both records exist.
	if !FileExists(filepath.Join(dest, "execution", "exec-a.json")) ||
		!FileExists(filepath.Join(dest, "execution", "exec-b.json")) {
		t.Fatal("restore must roll back the interrupted hard-delete batch")
	}
	if FileExists(filepath.Join(dest, ".batch-journal.json")) {
		t.Fatal("restore must consume the batch journal")
	}
}

func TestRestoreKeepsCommittedHardDelete(t *testing.T) {
	src := t.TempDir()
	seedHardDeleteStore(t, src)
	st, err := store.NewFileStore(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteBatchHard([]string{"exec-b"}); err != nil {
		t.Fatalf("hard delete: %v", err)
	}

	art := filepath.Join(t.TempDir(), "b.tar")
	if _, err := Create(src, art); err != nil {
		t.Fatalf("create: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "restored")
	if _, err := Restore(art, dest); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if FileExists(filepath.Join(dest, "execution", "exec-b.json")) {
		t.Fatal("a committed hard delete must not be resurrected by backup/restore")
	}
	if !FileExists(filepath.Join(dest, "execution", "exec-a.json")) {
		t.Fatal("a surviving record must remain after restore")
	}
}
