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

// List returns records matching the given filter.
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

// writeRecord writes a single record as a JSON file.
func (fs *FileStore) writeRecord(record *Record) error {
	typeDir := filepath.Join(fs.dir, string(record.Type))
	if err := os.MkdirAll(typeDir, 0o755); err != nil {
		return &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("create dir: %v", err)}
	}

	path := filepath.Join(typeDir, record.ID+".json")
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return &StoreError{Code: "SERIALIZATION_ERROR", Message: fmt.Sprintf("marshal: %v", err)}
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return &StoreError{Code: "IO_ERROR", Message: fmt.Sprintf("write: %v", err)}
	}

	return nil
}

// loadAll loads all records from disk into the index.
func (fs *FileStore) loadAll() error {
	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // empty store
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		typeDir := filepath.Join(fs.dir, entry.Name())
		files, err := os.ReadDir(typeDir)
		if err != nil {
			continue // skip unreadable dirs
		}

		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
				continue
			}
			path := filepath.Join(typeDir, f.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue // skip unreadable files
			}

			var record Record
			if err := json.Unmarshal(data, &record); err != nil {
				continue // skip corrupt files
			}

			fs.index[record.ID] = &record
		}
	}

	return nil
}
