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
		record.UpdatedAt = now
		s.records[record.ID] = record
		return nil
	}

	// New record
	record.Version = 1
	record.CreatedAt = now
	record.UpdatedAt = now
	if record.Status == "" {
		record.Status = RecordStatusActive
	}
	s.records[record.ID] = record
	return nil
}

// Get retrieves a record by ID.
func (s *MemStore) Get(id string) (*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, exists := s.records[id]
	if !exists || record.Status == RecordStatusDeleted {
		return nil, ErrNotFound
	}
	return record, nil
}

// Delete removes a record by ID (soft delete).
func (s *MemStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, exists := s.records[id]
	if !exists {
		return ErrNotFound
	}

	record.Status = RecordStatusDeleted
	record.UpdatedAt = s.now()
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
		result = append(result, record)
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
