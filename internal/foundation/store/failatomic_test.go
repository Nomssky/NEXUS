package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---- v42: FileStore failure atomicity -------------------------------------
//
// Delete previously mutated the in-memory index entry before the durable
// write succeeded. A write failure therefore left the index and the disk in
// disagreement: a subsequent read observed a deleted record that was never
// durably written. Fix: copy before mutating, write the copy, and only update
// the index after writeRecord succeeds.
//
// These tests prove the failure-atomicity contract for Delete and DeleteBatch
// using only deterministic seams (read-only directories to force write/stage
// failures) — no generic fault-injection framework.

// TestDeleteWriteFailureLeavesIndexConsistent forces writeRecord to fail
// (read-only type directory) and verifies the index and version are unchanged.
func TestDeleteWriteFailureLeavesIndexConsistent(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("open filestore: %v", err)
	}

	if err := fs.Put(&Record{
		ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
		Data: []byte(`{"x":1}`),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	before := mustGet(t, fs, "k")

	// Make the type directory read-only so stageRecord's CreateTemp fails.
	typeDir := filepath.Join(dir, string(RecordTypeMemory))
	if err := os.Chmod(typeDir, 0o550); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(typeDir, 0o750) // restore for cleanup

	if err := fs.Delete("k"); err == nil {
		t.Fatal("expected delete to fail when writeRecord cannot stage")
	}

	// Index must be unchanged: active record, same version and timestamps.
	after := mustGet(t, fs, "k")
	if after.Version != before.Version {
		t.Fatalf("version must be unchanged after failed delete, got %d want %d", after.Version, before.Version)
	}
	if after.Status != RecordStatusActive {
		t.Fatalf("status must still be active after failed delete, got %q", after.Status)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("updated_at must be unchanged after failed delete")
	}

	// Restore permissions and retry: delete must now succeed.
	if err := os.Chmod(typeDir, 0o750); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	if err := fs.Delete("k"); err != nil {
		t.Fatalf("delete after restore: %v", err)
	}
	after = mustGet(t, fs, "k")
	if after.Status != RecordStatusDeleted {
		t.Fatalf("expected deleted status after successful delete, got %q", after.Status)
	}
}

// TestDeletePersistsAcrossReopen verifies a successful delete survives
// store reopen — no resurrected active record.
func TestDeletePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("open filestore: %v", err)
	}

	if err := fs.Put(&Record{
		ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
		Data: []byte(`{"x":1}`),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := fs.Delete("k"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Reopen: the deleted record must still read as deleted.
	fs2, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("reopen filestore: %v", err)
	}
	got, err := fs2.Get("k")
	if err != nil {
		t.Fatalf("get after reopen: err=%v", err)
	}
	if got == nil || got.Status != RecordStatusDeleted {
		t.Fatalf("expected deleted after reopen, got %v", got)
	}
}

// TestDeleteBatchStagingFailureLeavesNothingChanged forces stageRecord to fail
// (read-only type directory) and verifies no record is deleted.
func TestDeleteBatchStagingFailureLeavesNothingChanged(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("open filestore: %v", err)
	}

	if err := fs.Put(&Record{
		ID: "a", Type: RecordTypeMemory, Status: RecordStatusActive,
		Data: []byte(`{"x":1}`),
	}); err != nil {
		t.Fatalf("put a: %v", err)
	}
	if err := fs.Put(&Record{
		ID: "b", Type: RecordTypeMemory, Status: RecordStatusActive,
		Data: []byte(`{"x":2}`),
	}); err != nil {
		t.Fatalf("put b: %v", err)
	}
	beforeA := mustGet(t, fs, "a")
	beforeB := mustGet(t, fs, "b")

	// Read-only type directory → stageRecord fails for every record.
	typeDir := filepath.Join(dir, string(RecordTypeMemory))
	if err := os.Chmod(typeDir, 0o550); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(typeDir, 0o750)

	if err := fs.DeleteBatch([]string{"a", "b"}); err == nil {
		t.Fatal("expected delete batch to fail when staging cannot create temp files")
	}

	// Nothing may have changed.
	afterA := mustGet(t, fs, "a")
	afterB := mustGet(t, fs, "b")
	if afterA.Status != RecordStatusActive || afterA.Version != beforeA.Version {
		t.Fatalf("record a must be unchanged after failed batch: %+v", afterA)
	}
	if afterB.Status != RecordStatusActive || afterB.Version != beforeB.Version {
		t.Fatalf("record b must be unchanged after failed batch: %+v", afterB)
	}

	// Restore permissions and retry: batch must now succeed.
	if err := os.Chmod(typeDir, 0o750); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	if err := fs.DeleteBatch([]string{"a", "b"}); err != nil {
		t.Fatalf("delete batch after restore: %v", err)
	}
	afterA = mustGet(t, fs, "a")
	afterB = mustGet(t, fs, "b")
	if afterA.Status != RecordStatusDeleted || afterB.Status != RecordStatusDeleted {
		t.Fatalf("expected both deleted after successful batch, got %q/%q", afterA.Status, afterB.Status)
	}
}

// TestDeleteBatchPersistsAcrossReopen verifies a successful soft-delete batch
// survives store reopen — no resurrected active records.
func TestDeleteBatchPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("open filestore: %v", err)
	}

	if err := fs.Put(&Record{
		ID: "a", Type: RecordTypeMemory, Status: RecordStatusActive,
		Data: []byte(`{"x":1}`),
	}); err != nil {
		t.Fatalf("put a: %v", err)
	}
	if err := fs.Put(&Record{
		ID: "b", Type: RecordTypeMemory, Status: RecordStatusActive,
		Data: []byte(`{"x":2}`),
	}); err != nil {
		t.Fatalf("put b: %v", err)
	}
	if err := fs.DeleteBatch([]string{"a", "b"}); err != nil {
		t.Fatalf("delete batch: %v", err)
	}

	// Reopen: both records must read as deleted.
	fs2, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("reopen filestore: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		got, err := fs2.Get(id)
		if err != nil {
			t.Fatalf("get %s after reopen: err=%v", id, err)
		}
		if got == nil || got.Status != RecordStatusDeleted {
			t.Fatalf("expected %s deleted after reopen, got %v", id, got)
		}
	}
}

// TestDeleteFailedThenRetryKeepsVersionTrack verifies that after a delete
// fails (write failure) and permissions are restored, the retry succeeds
// with the correct monotone version.
func TestDeleteFailedThenRetryKeepsVersionTrack(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("open filestore: %v", err)
	}

	if err := fs.Put(&Record{
		ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
		Data: []byte(`{"x":1}`),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	before := mustGet(t, fs, "k")

	// Force write failure.
	typeDir := filepath.Join(dir, string(RecordTypeMemory))
	if err := os.Chmod(typeDir, 0o550); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := fs.Delete("k"); err == nil {
		t.Fatal("expected delete to fail")
	}
	if err := os.Chmod(typeDir, 0o750); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}

	// Retry: delete must succeed with incremented version.
	if err := fs.Delete("k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deleted := mustGet(t, fs, "k")
	if deleted.Status != RecordStatusDeleted {
		t.Fatalf("expected deleted, got %q", deleted.Status)
	}
	if deleted.Version != before.Version+1 {
		t.Fatalf("expected version %d after delete, got %d", before.Version+1, deleted.Version)
	}
	if !deleted.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("created_at must be preserved across delete")
	}
}

// TestStaleBatchJournalIsRolledBackOnReopen verifies that a batch journal
// left behind by a crash (renames committed but journal not removed) is
// rolled back to pre-batch state on reopen — no deleted record resurrects as
// active, and no active record is lost.
func TestStaleBatchJournalIsRolledBackOnReopen(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("open filestore: %v", err)
	}

	// Put an active record.
	if err := fs.Put(&Record{
		ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
		Data:    []byte(`{"x":1}`),
		Version: 1,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	// Simulate a crash after DeleteBatch's renames were committed but before
	// the journal was removed: write a batch journal containing the pre-batch
	// record, then write a deleted version to disk directly.
	batch := batchJournal{
		Version: 1,
		Records: []Record{
			{ID: "k", Type: RecordTypeMemory, Status: RecordStatusActive,
				Version: 1, Data: []byte(`{"x":1}`)},
		},
	}
	jdata, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal batch journal: %v", err)
	}
	jpath := filepath.Join(dir, batchJournalName)
	if err := os.WriteFile(jpath, jdata, 0o644); err != nil {
		t.Fatalf("write batch journal: %v", err)
	}

	// Overwrite the record file with a deleted version (simulating committed
	// but un-journalled rename).
	delPath := filepath.Join(dir, string(RecordTypeMemory), "k.json")
	delData, err := json.Marshal(Record{
		ID: "k", Type: RecordTypeMemory, Status: RecordStatusDeleted,
		Version: 2, Data: []byte(`{"x":1}`),
	})
	if err != nil {
		t.Fatalf("marshal deleted record: %v", err)
	}
	if err := os.WriteFile(delPath, delData, 0o644); err != nil {
		t.Fatalf("write deleted record: %v", err)
	}

	// Reopen: recovery must roll back to pre-batch state (active, version 1).
	fs2, err := NewFileStoreWithClock(dir, time.Now)
	if err != nil {
		t.Fatalf("reopen filestore: %v", err)
	}
	got, err := fs2.Get("k")
	if err != nil {
		t.Fatalf("get after recovery: err=%v", err)
	}
	if got == nil || got.Status != RecordStatusActive {
		t.Fatalf("expected active after recovery rollback, got %v", got)
	}
	if got.Version != 1 {
		t.Fatalf("expected version 1 after recovery rollback, got %d", got.Version)
	}
}
