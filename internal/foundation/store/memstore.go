package store

import (
	"sync"
	"time"
)

// MemStore is an in-memory implementation of the Store interface.
// It is suitable for testing and development. Production deployments
// should use a durable store implementation.
//
// MemStore is NOT the source of truth for production — it exists only
// for testing and as a reference implementation of the Store interface.
type MemStore struct {
	mu      sync.RWMutex
	records map[string]*Record
	now     func() time.Time
}

// NewMemStore creates a new in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{
		records: make(map[string]*Record),
		now:     time.Now,
	}
}

// NewMemStoreWithClock creates a new in-memory store with an injectable clock.
func NewMemStoreWithClock(now func() time.Time) *MemStore {
	return &MemStore{
		records: make(map[string]*Record),
		now:     now,
	}
}

// Put stores a record. If the record already exists, the version must match.
func (s *MemStore) Put(record *Record) error {
	if record == nil {
		return ErrInvalidRecord
	}
	if record.ID == "" {
		return ErrInvalidRecord
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	existing, exists := s.records[record.ID]

	if exists {
		// Optimistic concurrency: version must match
		if existing.Version != record.Version {
			return ErrConflict
		}
		record.Version++
		// created_at is set once, by the store, at creation; an update may never
		// rewrite it (AGENT_MEMORY_CONTEXT §9, "preserving created_at"). This
		// also covers recreation over a tombstone: the id keeps one identity, so
		// it keeps one creation time. Matching FileStore here is what makes both
		// backends agree on the Create -> Delete -> Recreate lifecycle.
		record.CreatedAt = existing.CreatedAt
		record.UpdatedAt = now
		s.records[record.ID] = record.clone()
		return nil
	}

	// New record
	record.Version = 1
	record.CreatedAt = now
	record.UpdatedAt = now
	if record.Status == "" {
		record.Status = RecordStatusActive
	}
	s.records[record.ID] = record.clone()
	return nil
}

// Get retrieves a record by ID.
//
// A soft-deleted record still exists and is returned with its status, matching
// FileStore and the Store contract: absence means "no such record", not
// "deleted". Narrowing visibility to live records is List's job, via the
// deleted-status filter.
func (s *MemStore) Get(id string) (*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, exists := s.records[id]
	if !exists {
		return nil, ErrNotFound
	}
	return record.clone(), nil
}

// Delete removes a record by ID (soft delete).
//
// A soft delete mutates the record, so it is an update and increments the
// version, matching FileStore and SCHEMA_COMMON §3.9 ("increments on each
// update"). Without that increment a writer holding the pre-delete version
// could still match it and silently resurrect the record.
//
// Deleting an ALREADY-deleted record is a no-op: it is not a further update, so
// it advances neither Version nor updated_at. That makes a retried delete safe
// and matches FileStore (v32).
func (s *MemStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, exists := s.records[id]
	if !exists {
		return ErrNotFound
	}
	if record.Status == RecordStatusDeleted {
		return nil // already deleted: repeating changes nothing
	}

	record.Status = RecordStatusDeleted
	record.UpdatedAt = s.now()
	record.Version++
	return nil
}

// DeleteBatch removes every record by id as one atomic step: an unknown id
// leaves the whole set untouched.
func (s *MemStore) DeleteBatch(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate the whole batch first: nothing has been mutated yet, so a
	// refusal cannot leave a partially deleted set behind.
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return ErrInvalidRecord
		}
		if _, ok := s.records[id]; !ok {
			return ErrNotFound
		}
		seen[id] = struct{}{}
	}

	// Ids that are already deleted are already in the target state: repeating
	// the batch over them changes nothing, so they are skipped rather than
	// counted as a further update (v32, matching Delete).
	targets := make([]string, 0, len(seen))
	for id := range seen {
		if rec := s.records[id]; rec == nil || rec.Status == RecordStatusDeleted {
			continue
		}
		targets = append(targets, id)
	}
	if len(targets) == 0 {
		return nil
	}

	now := s.now()
	for _, id := range targets {
		record := s.records[id]
		record.Status = RecordStatusDeleted
		record.UpdatedAt = now
		// Same rule as Delete: a batch soft delete is an update, so it
		// increments the version exactly as FileStore's DeleteBatch does.
		record.Version++
	}
	return nil
}

// DeleteBatchHard removes every record by id as one atomic step and reclaims
// it (MemStore holds no files, so the record is dropped outright). An unknown
// id leaves the whole set untouched. See Store.DeleteBatchHard for the
// destructive contract.
func (s *MemStore) DeleteBatchHard(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate the whole batch first: nothing has been mutated yet, so a
	// refusal cannot leave a partially deleted set behind.
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return ErrInvalidRecord
		}
		if _, ok := s.records[id]; !ok {
			return ErrNotFound
		}
		seen[id] = struct{}{}
	}

	for id := range seen {
		delete(s.records, id)
	}
	return nil
}

// List returns records matching the given filter.
func (s *MemStore) List(filter Filter) ([]*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Record
	for _, record := range s.records {
		if !matchesFilter(record, filter) {
			continue
		}
		result = append(result, record.clone())
	}

	// Apply pagination
	if filter.Offset > 0 && filter.Offset < len(result) {
		result = result[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(result) {
		result = result[:filter.Limit]
	}

	return result, nil
}

// Count returns the number of records matching the given filter.
func (s *MemStore) Count(filter Filter) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, record := range s.records {
		if matchesFilter(record, filter) {
			count++
		}
	}
	return count, nil
}

// matchesFilter checks if a record matches the given filter criteria.
func matchesFilter(record *Record, filter Filter) bool {
	// Skip deleted records unless explicitly requested
	if record.Status == RecordStatusDeleted && filter.Status != RecordStatusDeleted {
		return false
	}

	if filter.Type != "" && record.Type != filter.Type {
		return false
	}
	if filter.Status != "" && record.Status != filter.Status {
		return false
	}
	if filter.BusinessID != "" && record.BusinessID != filter.BusinessID {
		return false
	}
	if filter.DivisionID != "" && record.DivisionID != filter.DivisionID {
		return false
	}
	if filter.CorrelationID != "" && record.CorrelationID != filter.CorrelationID {
		return false
	}
	if filter.CreatedAfter != nil && record.CreatedAt.Before(*filter.CreatedAfter) {
		return false
	}
	if filter.CreatedBefore != nil && record.CreatedAt.After(*filter.CreatedBefore) {
		return false
	}

	return true
}
