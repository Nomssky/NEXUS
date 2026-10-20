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
	// Organization records (SCHEMA_IDENTITIES_ORG §3/§4).
	RecordTypeBusiness RecordType = "business"
	RecordTypeDivision RecordType = "division"
	// Authentication and membership records (SCHEMA_IDENTITIES_ORG §10).
	// The credential record carries a verification hash, never a raw secret.
	RecordTypeCredential RecordType = "credential"
	RecordTypeMembership RecordType = "membership"
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

// clone returns a fully independent copy of a record.
//
// A shallow struct copy is NOT enough: Record carries Data []byte and
// ExpiresAt *time.Time, so a copied record would still share the payload's
// backing array and the expiry pointer with stored state. Either could then be
// mutated by a caller without ever passing through the store's validated
// mutation paths — bypassing optimistic concurrency, version rules and
// persistence entirely. Deep-copying both reference fields makes a record
// crossing the store boundary a true value (v33).
func (r *Record) clone() *Record {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Data != nil {
		cp.Data = make([]byte, len(r.Data))
		copy(cp.Data, r.Data)
	}
	if r.ExpiresAt != nil {
		exp := *r.ExpiresAt
		cp.ExpiresAt = &exp
	}
	return &cp
}

// Store is the core persistence interface.
// It provides generic CRUD operations for records.
//
// C05 is forbidden from making decisions or knowing domain semantics
// beyond storage. The store is the source of truth.
type Store interface {
	// Put stores a record.
	//
	// If the record exists it is updated, and the caller's version MUST equal
	// the stored version (optimistic concurrency); anything else is
	// ErrConflict and nothing is written. A zero Version therefore means "I
	// have no prior record" — a CREATE — and must never overwrite an existing
	// record, live or deleted. Callers that update an existing record read it
	// first and carry its version forward (v32).
	//
	// Creation always yields Version 1, whatever version the caller supplied
	// (SCHEMA_COMMON §3.9). An update preserves the stored CreatedAt and
	// refreshes UpdatedAt.
	Put(record *Record) error

	// Get retrieves a record by ID.
	//
	// Absence and soft deletion are DIFFERENT states, and a backend must not
	// conflate them:
	//
	//   - absent: there is no such record. A backend signals this as a nil
	//     record; whether it also returns ErrNotFound is backend-specific and
	//     callers must tolerate both. A genuine read failure is never absence
	//     and is always returned as a distinct error.
	//   - soft-deleted: the record still EXISTS with Status == deleted. Get
	//     returns it with that status. Hiding it would claim it never existed,
	//     and would lose the version and timestamps a caller needs to reason
	//     about recreation.
	//
	// List is where visibility is narrowed: matchesFilter skips soft-deleted
	// records unless the filter explicitly asks for RecordStatusDeleted. A
	// caller that needs live records therefore checks Status after Get, and a
	// caller that needs deleted ones may use either Get or an explicit
	// List filter.
	Get(id string) (*Record, error)

	// Delete removes a record by ID (soft delete sets status to deleted).
	//
	// Deleting an ALREADY-deleted record is a no-op: it returns success and
	// advances neither Version nor UpdatedAt, so a retried delete is safe and
	// cannot invalidate the version a client read from the first one (v32).
	// This mirrors core's "idempotent repeat cancel". An unknown id is still an
	// error.
	Delete(id string) error

	// DeleteBatch soft-deletes every id as ONE atomic step: either every
	// record ends up deleted, or none of them does and the call returns an
	// error. It fails when any id is unknown, so a caller that holds a set of
	// records it must remove together never ends up with a partially applied
	// removal — a governed deletion is exactly this shape.
	//
	// A correct implementation gives this durable contract:
	//   - normal call failure: the store still reads as pre-batch, synchronously;
	//   - concurrent access: the store lock serializes batches; readers observe
	//     the whole batch or none of it;
	//   - process crash: a batch journal records the pre-batch state durably
	//     before any record file is published, so startup recovery rolls the
	//     interrupted batch back, and an interrupted batch is NEVER exposed
	//     half-applied — the store fails closed on a corrupt or ambiguous
	//     journal rather than guessing;
	//   - after a successful return: the batch survives restart, journal
	//     already removed;
	//   - filesystem I/O failure: the call fails and, after the batch's own
	//     rollback, no record is half-changed.
	//
	// The implementation must never degrade the batch into per-record deletes:
	// a fallback reintroduces the partial mutation this method exists to
	// prevent.
	DeleteBatch(ids []string) error

	// DeleteBatchHard physically removes every id as ONE atomic step and
	// reclaims the space DeleteBatch only marks. It carries the same atomicity
	// contract as DeleteBatch — all-or-nothing, one store-wide batch journal,
	// interrupted batches rolled back on the next open, corrupt/ambiguous
	// journals fail closed — except that a committed batch leaves no file
	// behind (a soft batch rewrites the record; a hard batch unlinks it).
	//
	// It exists for retention: it is destructive and irreversible, so callers
	// must have proven the records are disposable against an explicit,
	// governed policy (contracts/EXECUTION_JOURNAL_RETENTION_CONTRACTS.md). It
	// must never be used as a general-purpose delete.
	DeleteBatchHard(ids []string) error

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

// Is lets callers match store sentinels uniformly across backends. VERSION_CONFLICT
// is FileStore's descriptive form of the CONFLICT sentinel (it keeps the
// "expected N, got M" detail in its message), so errors.Is(err, ErrConflict)
// must recognise both spellings; otherwise a version conflict raised by the
// durable backend would silently miss a caller checking the exported sentinel.
func (e *StoreError) Is(target error) bool {
	t, ok := target.(*StoreError)
	if !ok || t == nil {
		return false
	}
	if e.Code == t.Code {
		return true
	}
	return (e.Code == "VERSION_CONFLICT" && t.Code == "CONFLICT") ||
		(e.Code == "CONFLICT" && t.Code == "VERSION_CONFLICT")
}

var (
	ErrNotFound      = &StoreError{Code: "NOT_FOUND", Message: "record not found"}
	ErrConflict      = &StoreError{Code: "CONFLICT", Message: "version conflict"}
	ErrAlreadyExists = &StoreError{Code: "ALREADY_EXISTS", Message: "record already exists"}
	ErrInvalidRecord = &StoreError{Code: "INVALID_RECORD", Message: "record validation failed"}
)
