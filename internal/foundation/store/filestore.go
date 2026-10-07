// Package store — FileStore is a disk-backed implementation of the Store interface.
//
// Records are persisted as individual JSON files in a directory tree:
//
//	<dir>/
//	  <type>/
//	    <id>.json
//
// FileStore is safe for concurrent access (guarded by sync.RWMutex).
// It implements the Store interface exactly, so it's a drop-in replacement for MemStore.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// FileStore is a disk-backed Store implementation.
// Records are stored as JSON files organized by type.
type FileStore struct {
	dir string
	now func() time.Time
	mu  sync.RWMutex
	// index caches all records in memory for fast List/Count.
	// Writes are flushed to disk. Reads serve from cache.
	index map[string]*Record // id -> record
}

// NewFileStore creates a new FileStore rooted at the given directory.
// The directory is created if it doesn't exist.
func NewFileStore(dir string) (*FileStore, error) {
	return NewFileStoreWithClock(dir, time.Now)
}

// NewFileStoreWithClock creates a new FileStore with an injectable clock.
func NewFileStoreWithClock(dir string, now func() time.Time) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("filestore: create dir: %w", err)
	}

	fs := &FileStore{
		dir:   dir,
		now:   now,
		index: make(map[string]*Record),
	}

	// Load existing records from disk
	if err := fs.loadAll(); err != nil {
		return nil, fmt.Errorf("filestore: load: %w", err)
	}

	return fs, nil
}

// Put stores a record. If the record exists, it is updated with
// optimistic concurrency (version must match).
func (fs *FileStore) Put(record *Record) error {
	if record == nil {
		return &StoreError{Code: "INVALID_RECORD", Message: "record is nil"}
	}
	if record.ID == "" {
		return &StoreError{Code: "INVALID_RECORD", Message: "record ID required"}
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	now := fs.now()

	// Check for existing record (optimistic concurrency)
	if existing, ok := fs.index[record.ID]; ok {
		if record.Version != 0 && existing.Version != record.Version {
			return &StoreError{
				Code:    "VERSION_CONFLICT",
				Message: fmt.Sprintf("expected version %d, got %d", existing.Version, record.Version),
			}
		}
		record.Version = existing.Version + 1
		record.CreatedAt = existing.CreatedAt
	} else {
		if record.Version == 0 {
			record.Version = 1
		}
		record.CreatedAt = now
	}

	record.UpdatedAt = now

	// Write to disk
	if err := fs.writeRecord(record); err != nil {
		return err
	}

	// Update cache
	cp := *record
	fs.index[record.ID] = &cp
	return nil
}

// Get retrieves a record by ID.
func (fs *FileStore) Get(id string) (*Record, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	record, ok := fs.index[id]
	if !ok {
		return nil, nil
	}

	// Return a copy to prevent mutation
	cp := *record
	return &cp, nil
}

// Delete removes a record by ID (soft delete).
func (fs *FileStore) Delete(id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	record, ok := fs.index[id]
	if !ok {
		return &StoreError{Code: "NOT_FOUND", Message: fmt.Sprintf("record %s not found", id)}
	}

	record.Status = RecordStatusDeleted
	record.UpdatedAt = fs.now()
	record.Version++

	if err := fs.writeRecord(record); err != nil {
		return err
	}

	return nil
}

// DeleteBatch soft-deletes every id as one atomic step.
//
// The index is what every read observes, so the batch is published in two
// phases under the store lock: every updated record is first staged as a
// synced temp file next to its target (nothing is published yet, so a failure
// there leaves the store exactly as it was), and only then is each staged file
// renamed over its target. If a rename fails after some have been published,
// the already-published records are restored from their pre-batch bytes within
// this same call, so the batch still lands all-or-nothing from the store's
// point of view. There is no per-record fallback and no deferred repair work.
func (fs *FileStore) DeleteBatch(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Validate the whole batch before mutating anything.
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return &StoreError{Code: "INVALID_RECORD", Message: "record ID required"}
		}
		if _, ok := fs.index[id]; !ok {
			return &StoreError{Code: "NOT_FOUND", Message: fmt.Sprintf("record %s not found", id)}
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	now := fs.now()
	originals := make([]*Record, 0, len(unique))
	updates := make([]*Record, 0, len(unique))
	staged := make([]string, 0, len(unique))
	paths := make([]string, 0, len(unique))
	cleanup := func() {
		for _, tmp := range staged {
			_ = os.Remove(tmp)
		}
	}

	// Phase 1 — stage. Nothing is visible to any reader yet.
	for _, id := range unique {
		original := *fs.index[id]
		updated := original
		updated.Status = RecordStatusDeleted
		updated.UpdatedAt = now
		updated.Version = original.Version + 1

		tmp, path, err := fs.stageRecord(&updated)
		if err != nil {
			cleanup()
			return err
		}
		originals = append(originals, &original)
		updates = append(updates, &updated)
		staged = append(staged, tmp)
		paths = append(paths, path)
	}

	// Phase 2 — publish. The index still describes the pre-batch state, so a
	// failure here is invisible to every reader unless the rollback fails.
	for i, tmp := range staged {
		if err := os.Rename(tmp, paths[i]); err != nil {
			cleanup()
			for j := 0; j < i; j++ {
				if rerr := fs.writeRecord(originals[j]); rerr != nil {
					return &StoreError{
						Code:    "IO_ERROR",
						Message: fmt.Sprintf("delete batch rollback of %s failed: %v", originals[j].ID, rerr),
					}
				}
			}
			return &StoreError{
				Code:    "IO_ERROR",
				Message: fmt.Sprintf("delete batch publish %s: %v", updates[i].ID, err),
			}
		}
	}

	for _, updated := range updates {
		cp := *updated
		fs.index[updated.ID] = &cp
	}
	return nil
}

// List returns records matching the given filter.
//
// NOTE: FileStore uses the shared matchesFilter function (from memstore.go),
// which skips soft-deleted records (Status == "deleted") unless the filter's
// Status field is explicitly set to RecordStatusDeleted. This is inherited
// behavior from MemStore — callers must set Filter.Status to include deleted
// records.
func (fs *FileStore) List(filter Filter) ([]*Record, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var results []*Record
	for _, r := range fs.index {
		if !matchesFilter(r, filter) {
			continue
		}
		cp := *r
		results = append(results, &cp)
	}

	// Sort by CreatedAt ascending
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.Before(results[j].CreatedAt)
	})

	// Apply pagination
	if filter.Offset > 0 {
		if filter.Offset >= len(results) {
			return nil, nil
		}
		results = results[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(results) {
		results = results[:filter.Limit]
	}

	return results, nil
}

// Count returns the number of records matching the given filter.
func (fs *FileStore) Count(filter Filter) (int, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	count := 0
	for _, r := range fs.index {
		if matchesFilter(r, filter) {
			count++
		}
	}
	return count, nil
}

// CountAll returns the total number of records (including soft-deleted).
func (fs *FileStore) CountAll() int {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return len(fs.index)
}

// --- internal helpers ---

// writeRecord writes a single record as a JSON file atomically: a temp file
// in the same directory is written, fsynced, then renamed over the target.
// A crash mid-write can therefore never leave a truncated or partial record
// behind — readers see either the old bytes or the new ones.
func (fs *FileStore) writeRecord(record *Record) error {
	tmp, path, err := fs.stageRecord(record)
	if err != nil {
		return err
	}
	// Remove the temp file on any failure path; after a successful rename the
	// name no longer exists (remove of a missing path is a no-op).
	defer func() { _ = os.Remove(tmp) }()

	if err := os.Rename(tmp, path); err != nil {
		return &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("rename: %v", err)}
	}
	return nil
}

// stageRecord writes the record's JSON into a synced temp file next to its
// target and returns that temp path together with the target path. The caller
// publishes it with os.Rename, which is what makes the publish atomic; a
// batch stages every record before publishing any of them.
func (fs *FileStore) stageRecord(record *Record) (tmpName string, path string, err error) {
	typeDir := filepath.Join(fs.dir, string(record.Type))
	if err := os.MkdirAll(typeDir, 0o755); err != nil {
		return "", "", &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("create dir: %v", err)}
	}

	path = filepath.Join(typeDir, record.ID+".json")
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", "", &StoreError{Code: "SERIALIZATION_ERROR", Message: fmt.Sprintf("marshal: %v", err)}
	}

	tmp, err := os.CreateTemp(typeDir, "."+record.ID+"-*.tmp")
	if err != nil {
		return "", "", &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("create temp: %v", err)}
	}
	tmpName = tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", "", &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("write: %v", err)}
	}
	// Durability: flush file contents before the rename publishes them.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", "", &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("sync: %v", err)}
	}
	if err := tmp.Close(); err != nil {
		return "", "", &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("close: %v", err)}
	}
	// Match the historical permissions of direct writes (os.WriteFile 0644).
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return "", "", &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("chmod: %v", err)}
	}
	return tmpName, path, nil
}

// loadAll loads all records from disk into the index.
//
// Fail closed (F4): an unreadable directory, an unreadable file, an
// unparseable file, or a file that carries no record id aborts the open.
// Silently skipping such a file dropped it from the index, so the caller's
// documented fail-closed hydration (OpenRegistry on corrupt records) never
// saw the corruption and booted with silently missing data.
func (fs *FileStore) loadAll() error {
	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // empty store
		}
		return &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("read dir: %v", err)}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		typeDir := filepath.Join(fs.dir, entry.Name())
		files, err := os.ReadDir(typeDir)
		if err != nil {
			return &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("read dir %s: %v", typeDir, err)}
		}

		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
				continue
			}
			path := filepath.Join(typeDir, f.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("read %s: %v", path, err)}
			}

			var record Record
			if err := json.Unmarshal(data, &record); err != nil {
				return &StoreError{Code: "CORRUPT_RECORD", Message: fmt.Sprintf("parse %s: %v", path, err)}
			}
			if record.ID == "" {
				return &StoreError{Code: "CORRUPT_RECORD", Message: fmt.Sprintf("parse %s: record id is empty", path)}
			}

			fs.index[record.ID] = &record
		}
	}

	return nil
}
