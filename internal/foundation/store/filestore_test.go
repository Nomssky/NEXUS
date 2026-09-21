package store

import (
	"os"
	"path/filepath"
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

func TestFileStoreCorruptFileSkipped(t *testing.T) {
	dir := t.TempDir()

	// Write a corrupt file
	typeDir := filepath.Join(dir, "test")
	os.MkdirAll(typeDir, 0o755)
	os.WriteFile(filepath.Join(typeDir, "bad.json"), []byte("not json"), 0o644)

	// Write a valid file
	os.WriteFile(filepath.Join(typeDir, "good.json"), []byte(`{"id":"good","type":"test","status":"active","version":1}`), 0o644)

	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	count, _ := fs.Count(Filter{})
	if count != 1 {
		t.Errorf("expected 1 valid record, got %d", count)
	}
}
