package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileStorePutGet(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fs, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	record := &Record{
		ID:     "rec-1",
		Type:   "test",
		Status: RecordStatusActive,
		Data:   []byte(`{"key":"value"}`),
	}

	if err := fs.Put(record); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := fs.Get("rec-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected record")
	}
	if got.ID != "rec-1" {
		t.Errorf("expected id rec-1, got %s", got.ID)
	}
	if got.Version != 1 {
		t.Errorf("expected version 1, got %d", got.Version)
	}
	if string(got.Data) != `{"key":"value"}` {
		t.Errorf("expected data, got %s", string(got.Data))
	}
}

func TestFileStoreUpdate(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	var counter int
	fs, err := NewFileStoreWithClock(dir, func() time.Time {
		counter++
		return now.Add(time.Duration(counter) * time.Second)
	})
	if err != nil {
		t.Fatal(err)
	}

	record := &Record{ID: "rec-1", Type: "test", Status: RecordStatusActive}
	if err := fs.Put(record); err != nil {
		t.Fatal(err)
	}

	// Update with correct version
	record.Status = RecordStatusArchived
	if err := fs.Put(record); err != nil {
		t.Fatal(err)
	}

	got, _ := fs.Get("rec-1")
	if got.Status != RecordStatusArchived {
		t.Errorf("expected archived, got %s", got.Status)
	}
	if got.Version != 2 {
		t.Errorf("expected version 2, got %d", got.Version)
	}
}

func TestFileStoreVersionConflict(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fs, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	record := &Record{ID: "rec-1", Type: "test", Status: RecordStatusActive}
	fs.Put(record)

	// Try to update with wrong version
	record.Version = 99
	record.Status = RecordStatusArchived
	err = fs.Put(record)
	if err == nil {
		t.Fatal("expected version conflict error")
	}
	se, ok := err.(*StoreError)
	if !ok {
		t.Fatalf("expected StoreError, got %T", err)
	}
	if se.Code != "VERSION_CONFLICT" {
		t.Errorf("expected VERSION_CONFLICT, got %s", se.Code)
	}
}

func TestFileStoreDelete(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fs, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	record := &Record{ID: "rec-1", Type: "test", Status: RecordStatusActive}
	fs.Put(record)

	if err := fs.Delete("rec-1"); err != nil {
		t.Fatal(err)
	}

	got, _ := fs.Get("rec-1")
	if got == nil {
		t.Fatal("expected record (soft delete)")
	}
	if got.Status != RecordStatusDeleted {
		t.Errorf("expected deleted status, got %s", got.Status)
	}
}

func TestFileStoreDeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	err = fs.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestFileStoreListFilter(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fs, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	fs.Put(&Record{ID: "r1", Type: "workflow", Status: RecordStatusActive, BusinessID: "biz-1"})
	fs.Put(&Record{ID: "r2", Type: "task", Status: RecordStatusActive, BusinessID: "biz-1"})
	fs.Put(&Record{ID: "r3", Type: "workflow", Status: RecordStatusArchived, BusinessID: "biz-2"})

	// Filter by type
	records, _ := fs.List(Filter{Type: "workflow"})
	if len(records) != 2 {
		t.Errorf("expected 2 workflows, got %d", len(records))
	}

	// Filter by business
	records, _ = fs.List(Filter{BusinessID: "biz-1"})
	if len(records) != 2 {
		t.Errorf("expected 2 biz-1 records, got %d", len(records))
	}

	// Filter by type + status
	records, _ = fs.List(Filter{Type: "workflow", Status: RecordStatusActive})
	if len(records) != 1 {
		t.Errorf("expected 1 active workflow, got %d", len(records))
	}

	// Pagination
	records, _ = fs.List(Filter{Type: "workflow", Limit: 1})
	if len(records) != 1 {
		t.Errorf("expected 1 record (limit), got %d", len(records))
	}
}

func TestFileStorePersistence(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// Create store and add records
	fs1, _ := NewFileStoreWithClock(dir, func() time.Time { return now })
	fs1.Put(&Record{ID: "r1", Type: "test", Status: RecordStatusActive, Data: []byte("hello")})
	fs1.Put(&Record{ID: "r2", Type: "test", Status: RecordStatusActive, Data: []byte("world")})
	fs1 = nil // close/destroy

	// Create new store from same directory — should load records
	fs2, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	got, _ := fs2.Get("r1")
	if got == nil {
		t.Fatal("expected r1 to persist")
	}
	if string(got.Data) != "hello" {
		t.Errorf("expected data 'hello', got '%s'", string(got.Data))
	}

	got2, _ := fs2.Get("r2")
	if got2 == nil {
		t.Fatal("expected r2 to persist")
	}
}

func TestFileStorePersistenceByType(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fs, _ := NewFileStoreWithClock(dir, func() time.Time { return now })

	fs.Put(&Record{ID: "r1", Type: "workflow", Status: RecordStatusActive})
	fs.Put(&Record{ID: "r2", Type: "task", Status: RecordStatusActive})
	fs = nil

	// Verify directory structure
	_, err := os.Stat(filepath.Join(dir, "workflow", "r1.json"))
	if err != nil {
		t.Errorf("expected workflow/r1.json: %v", err)
	}
	_, err = os.Stat(filepath.Join(dir, "task", "r2.json"))
	if err != nil {
		t.Errorf("expected task/r2.json: %v", err)
	}

	// Reload
	fs2, _ := NewFileStoreWithClock(dir, func() time.Time { return now })
	count, _ := fs2.Count(Filter{})
	if count != 2 {
		t.Errorf("expected 2 records after reload, got %d", count)
	}
}

func TestFileStoreCountAll(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileStore(dir)

	fs.Put(&Record{ID: "r1", Type: "a", Status: RecordStatusActive})
	fs.Put(&Record{ID: "r2", Type: "b", Status: RecordStatusActive})
	fs.Put(&Record{ID: "r3", Type: "c", Status: RecordStatusDeleted})

	if fs.CountAll() != 3 {
		t.Errorf("expected 3 total, got %d", fs.CountAll())
	}
}

func TestFileStoreEmptyDir(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	records, _ := fs.List(Filter{})
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

// TEST-F4-01: a corrupt record file must fail the open (fail-closed boot),
// not be silently dropped from the index. Skipping it meant the documented
// fail-closed hydration (OpenRegistry "failing closed on corrupt records")
// never saw the corruption and booted with silently missing data.
func TestFileStoreCorruptFileFailsClosed(t *testing.T) {
	dir := t.TempDir()

	typeDir := filepath.Join(dir, "test")
	if err := os.MkdirAll(typeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(typeDir, "bad.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A valid file next to it must not mask the corruption.
	if err := os.WriteFile(filepath.Join(typeDir, "good.json"), []byte(`{"id":"good","type":"test","status":"active","version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := NewFileStore(dir); err == nil {
		t.Fatal("expected corrupt record file to fail the open (fail-closed)")
	} else if !strings.Contains(err.Error(), "bad.json") {
		t.Errorf("error must name the corrupt file, got: %v", err)
	}
}

// TEST-F4-02: a file that parses but carries no record id is equally
// unusable data — it must fail the open rather than index under "".
func TestFileStoreRecordWithoutIDFailsClosed(t *testing.T) {
	dir := t.TempDir()

	typeDir := filepath.Join(dir, "test")
	if err := os.MkdirAll(typeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(typeDir, "noid.json"), []byte(`{"type":"test","status":"active"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := NewFileStore(dir); err == nil {
		t.Fatal("expected id-less record file to fail the open (fail-closed)")
	}
}

// TEST-FS-01: writes are atomic — a temp file + rename is used, so the type
// directory only ever contains complete .json records (no partial writes, no
// leftover temp files after successful puts), and contents round-trip.
func TestFileStoreAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	rec := &Record{
		ID:   "nx:identity:atomic",
		Type: RecordTypeIdentity,
		Data: []byte(`{"entity_id":"nx:identity:atomic"}`),
	}
	if err := fs.Put(rec); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Update in place (Put mutates Version; a second Put with the current
	// version satisfies optimistic concurrency in both stores).
	rec.Data = []byte(`{"entity_id":"nx:identity:atomic","status":"suspended"}`)
	if err := fs.Put(rec); err != nil {
		t.Fatalf("update: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, string(RecordTypeIdentity)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one record file, got %d", len(entries))
	}
	if name := entries[0].Name(); name != "nx:identity:atomic.json" {
		t.Errorf("unexpected file %q (temp remnants?)", name)
	}

	// Contents are the complete updated record.
	reopened, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := reopened.Get("nx:identity:atomic")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if !strings.Contains(string(got.Data), "suspended") {
		t.Errorf("update not durable: %s", got.Data)
	}
}

// TEST-STO-BATCH-2: the file store publishes a batch in one step. A refused
// batch leaves both the index and the on-disk mirror untouched, and a successful
// batch removes every id.
func TestFileStoreDeleteBatchIsAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fs, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"rec-1", "rec-2"} {
		if err := fs.Put(&Record{ID: id, Type: RecordTypeMemory, Status: RecordStatusActive, Data: []byte(`{"v":1}`)}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}

	// An unknown id refuses the whole batch: nothing on disk changed.
	if err := fs.DeleteBatch([]string{"rec-1", "nope"}); err == nil {
		t.Fatal("a batch with an unknown id must fail")
	}
	for _, id := range []string{"rec-1", "rec-2"} {
		got, err := fs.Get(id)
		if err != nil || got == nil {
			t.Fatalf("%s must survive a refused batch: %v", id, err)
		}
		if got.Status != RecordStatusActive {
			t.Fatalf("%s must still be active, got %q", id, got.Status)
		}
		path := filepath.Join(dir, string(RecordTypeMemory), id+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), `"status": "active"`) {
			t.Fatalf("%s on disk changed during a refused batch: %s", id, data)
		}
	}

	// A complete batch removes every id, in the index and on disk.
	if err := fs.DeleteBatch([]string{"rec-1", "rec-2"}); err != nil {
		t.Fatalf("delete batch: %v", err)
	}
	for _, id := range []string{"rec-1", "rec-2"} {
		got, err := fs.Get(id)
		if err != nil || got == nil {
			t.Fatalf("%s must still be readable by id after a soft delete: %v", id, err)
		}
		if got.Status != RecordStatusDeleted {
			t.Fatalf("%s must be soft deleted, got %q", id, got.Status)
		}
		if _, err := fs.List(Filter{Type: RecordTypeMemory}); err != nil {
			t.Fatalf("list: %v", err)
		}
	}
	// The batch survives a reopen: the mirror really was published.
	reopened, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	deleted, err := reopened.List(Filter{Type: RecordTypeMemory, Status: RecordStatusDeleted})
	if err != nil {
		t.Fatalf("list deleted: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("both records must be deleted after a reopen, got %d", len(deleted))
	}
}

// --- durable batch recovery (crash consistency) -------------------------------

// seedInterruptedBatch lays down the exact on-disk state a crash can produce:
// a committed batch journal, and target files in MIXED states — exactly one of
// the records has been replaced by its deleted successor. The other is still at
// its pre-batch bytes. This is the "process died between renames" state.
func seedInterruptedBatch(t *testing.T, dir string, originals []*Record, replacedIndex int) {
	t.Helper()
	now := time.Now()
	fs, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range originals {
		if err := fs.Put(r); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	// Hand-craft the journal the batch engine would have written before the
	// first rename: the pre-batch state of every member.
	j := batchJournal{Version: 1, Records: make([]Record, 0, len(originals))}
	for _, r := range originals {
		j.Records = append(j.Records, *r)
	}
	jdata, err := json.Marshal(&j)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, batchJournalName), jdata, 0o644); err != nil {
		t.Fatal(err)
	}
	// Simulate the crash: the target file for ONE member was already replaced
	// by its deleted version, the journal says it was in flight, and the
	// directory entries were never cleaned up.
	if replacedIndex >= 0 {
		updated := *originals[replacedIndex]
		updated.Status = RecordStatusDeleted
		updated.Version++
		udata, err := json.MarshalIndent(&updated, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, string(originals[replacedIndex].Type), originals[replacedIndex].ID+".json")
		if err := os.WriteFile(path, udata, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TEST-STO-BATCH-3: a crashed batch must not be exposed half-applied. On the
// next open the store rolls the journal back: the member whose rename had
// already landed is restored, and everything is visibly pre-batch again.
func TestFileStoreRecoversInterruptedBatchToPreBatch(t *testing.T) {
	dir := t.TempDir()
	a := &Record{ID: "rec-a", Type: RecordTypeMemory, Status: RecordStatusActive, Data: []byte(`{"v":1}`)}
	b := &Record{ID: "rec-b", Type: RecordTypeMemory, Status: RecordStatusActive, Data: []byte(`{"v":2}`)}
	seedInterruptedBatch(t, dir, []*Record{a, b}, 0)

	fs, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("open after a crash must succeed via recovery: %v", err)
	}
	for _, r := range []string{"rec-a", "rec-b"} {
		got, err := fs.Get(r)
		if err != nil || got == nil {
			t.Fatalf("%s must be readable after recovery: %v", r, err)
		}
		if got.Status != RecordStatusActive {
			t.Fatalf("%s must be restored to its pre-batch state, got %q", r, got.Status)
		}
	}
	// The journal is gone: recovery does not replay forever.
	if _, err := os.Stat(filepath.Join(dir, batchJournalName)); !os.IsNotExist(err) {
		t.Fatalf("the journal must be removed after recovery, stat err=%v", err)
	}
	// And the pre-batch state is what a later reader — after a second open — sees.
	reopened, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	active, err := reopened.List(Filter{Type: RecordTypeMemory})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Fatalf("exactly the two pre-batch records must be visible, got %d", len(active))
	}
}

// TEST-STO-BATCH-4: an interrupted batch where NO rename landed yet still
// recovers to the pre-batch state (the rollback is idempotent, not conditional
// on how far publication got).
func TestFileStoreRecoversJournalEvenWhenNothingWasPublished(t *testing.T) {
	dir := t.TempDir()
	a := &Record{ID: "rec-a", Type: RecordTypeMemory, Status: RecordStatusActive, Data: []byte(`{"v":1}`)}
	seedInterruptedBatch(t, dir, []*Record{a}, -1)

	fs, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.Get("rec-a")
	if err != nil || got == nil || got.Status != RecordStatusActive {
		t.Fatalf("pre-batch state must survive: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, batchJournalName)); !os.IsNotExist(err) {
		t.Fatal("journal must be removed")
	}
}

// TEST-STO-BATCH-5: a corrupt journal must fail the open closed — the store
// refuses to guess which state it is in rather than serving a possibly
// half-applied batch.
func TestFileStoreFailClosedOnCorruptBatchJournal(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Put(&Record{ID: "rec-a", Type: RecordTypeMemory, Data: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, batchJournalName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(dir); err == nil {
		t.Fatal("a parseable-corrupt batch journal must abort the open")
	}
	// An unknown journal version or an empty member list is equally ambiguous.
	if err := os.WriteFile(filepath.Join(dir, batchJournalName), []byte(`{"version":99,"records":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(dir); err == nil {
		t.Fatal("a foreign-version batch journal must abort the open")
	}
}

// TEST-STO-BATCH-6: after a REAL batch commits, the journal is gone and the
// deletes survive a restart; after a batch NEVER returns (journal removed only
// on success), a restart shows the pre-batch state.
func TestFileStoreBatchOutcomeIsStableAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	fs, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Put(&Record{ID: "rec-a", Type: RecordTypeMemory, Status: RecordStatusActive, Data: []byte(`{"v":1}`)}); err != nil {
		t.Fatal(err)
	}
	if err := fs.Put(&Record{ID: "rec-b", Type: RecordTypeMemory, Status: RecordStatusActive, Data: []byte(`{"v":2}`)}); err != nil {
		t.Fatal(err)
	}
	if err := fs.DeleteBatch([]string{"rec-a", "rec-b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, batchJournalName)); !os.IsNotExist(err) {
		t.Fatal("a committed batch must not leave a journal behind")
	}
	reopened, err := NewFileStoreWithClock(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	active, _ := reopened.List(Filter{Type: RecordTypeMemory})
	deleted, _ := reopened.List(Filter{Type: RecordTypeMemory, Status: RecordStatusDeleted})
	if len(active) != 0 || len(deleted) != 2 {
		t.Fatalf("committed batch must survive restart, active=%d deleted=%d", len(active), len(deleted))
	}
}
