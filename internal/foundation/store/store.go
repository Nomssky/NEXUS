// Package store defines the persistence interfaces for NEXUS (C05).
//
// C05 owns durable stores for all authoritative categories. It depends
// only on C01 (config). It is used by all L2+ components.
//
// C05 is forbidden from making decisions or knowing domain semantics
// beyond storage. It provides generic record storage, retrieval, and
// lifecycle management.
//
// All records have a common envelope with metadata for durability,
// lineage, and retention. The store is the source of truth; cache
// is never authoritative.
package store

import (
	"fmt"
	"time"
)

// RecordType classifies what domain a record belongs to.
type RecordType string

const (
	RecordTypeWorkflow  RecordType = "workflow"
	RecordTypeTask      RecordType = "task"
	RecordTypeAgent     RecordType = "agent"
	RecordTypeObjective RecordType = "objective"
	RecordTypeEvent     RecordType = "event"
	RecordTypeAudit     RecordType = "audit"
	RecordTypeMemory    RecordType = "memory"
	RecordTypeKnowledge RecordType = "knowledge"
	RecordTypeApproval  RecordType = "approval"
	RecordTypeOutcome   RecordType = "outcome"
	RecordTypeConfig    RecordType = "config"
	RecordTypeIdentity  RecordType = "identity"
	RecordTypeArtifact  RecordType = "artifact"
)

// RecordStatus tracks the lifecycle state of a stored record.
type RecordStatus string

const (
	RecordStatusActive   RecordStatus = "active"
	RecordStatusArchived RecordStatus = "archived"
	RecordStatusDeleted  RecordStatus = "deleted"
	RecordStatusPending  RecordStatus = "pending"
)

// Record is the common envelope for all stored data.
// Every authoritative category uses this envelope.
type Record struct {
	// ID is the unique identifier for this record.
	ID string `json:"id"`
	// Type classifies the domain (workflow, task, event, etc.).
	Type RecordType `json:"type"`
	// Status tracks lifecycle state.
	Status RecordStatus `json:"status"`
	// BusinessID scopes the record to a business.
	BusinessID string `json:"business_id,omitempty"`
	// DivisionID scopes the record to a division.
	DivisionID string `json:"division_id,omitempty"`
	// CorrelationID links records across the correlation chain.
	CorrelationID string `json:"correlation_id,omitempty"`
	// CausationID links to the event/action that caused this record.
	CausationID string `json:"causation_id,omitempty"`
	// Version is the optimistic concurrency version.
	Version int `json:"version"`
	// CreatedAt is when the record was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the record was last updated.
	UpdatedAt time.Time `json:"updated_at"`
	// ExpiresAt is when the record should be archived (nil = never).
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Data is the domain-specific payload (stored as opaque bytes).
	Data []byte `json:"data,omitempty"`
}

// IsExpired returns true if the record has passed its expiration time.
func (r *Record) IsExpired(now time.Time) bool {
	if r.ExpiresAt == nil {
		return false
	}
	return now.After(*r.ExpiresAt)
}

// Store is the core persistence interface.
// It provides generic CRUD operations for records.
//
// C05 is forbidden from making decisions or knowing domain semantics
// beyond storage. The store is the source of truth.
type Store interface {
	// Put stores a record. If the record exists, it is updated with
	// optimistic concurrency (version must match).
	Put(record *Record) error

	// Get retrieves a record by ID. Returns nil if not found.
	Get(id string) (*Record, error)

	// Delete removes a record by ID (soft delete sets status to deleted).
	Delete(id string) error

	// List returns records matching the given filter.
	List(filter Filter) ([]*Record, error)

	// Count returns the number of records matching the given filter.
	Count(filter Filter) (int, error)
}

// Filter defines criteria for listing records.
type Filter struct {
	// Type filters by record type (empty = all types).
	Type RecordType
	// Status filters by record status (empty = current status).
	Status RecordStatus
	// BusinessID filters by business scope.
	BusinessID string
	// DivisionID filters by division scope.
	DivisionID string
	// CorrelationID filters by correlation chain.
	CorrelationID string
	// Limit is the maximum number of records to return (0 = no limit).
	Limit int
	// Offset is the number of records to skip (for pagination).
	Offset int
	// CreatedAfter filters records created after this time.
	CreatedAfter *time.Time
	// CreatedBefore filters records created before this time.
	CreatedBefore *time.Time
}

// StoreError represents a persistence-specific error.
type StoreError struct {
	Code    string
	Message string
}

func (e *StoreError) Error() string {
	return fmt.Sprintf("store error [%s]: %s", e.Code, e.Message)
}

var (
	ErrNotFound      = &StoreError{Code: "NOT_FOUND", Message: "record not found"}
	ErrConflict      = &StoreError{Code: "CONFLICT", Message: "version conflict"}
	ErrAlreadyExists = &StoreError{Code: "ALREADY_EXISTS", Message: "record already exists"}
	ErrInvalidRecord = &StoreError{Code: "INVALID_RECORD", Message: "record validation failed"}
)
