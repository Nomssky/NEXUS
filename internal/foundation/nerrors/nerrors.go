// Package nerrors defines the NEXUS foundation error envelope.
//
// This is the M0 foundation for the LOCKED error envelope defined in
// contracts/CORE_INTERFACE_CONTRACTS.md §3 "Error Envelope". It preserves the
// canonical error *category* vocabulary exactly. It does NOT invent categories,
// and it keeps governance *outcomes* (ALLOW, DENY, REQUIRE_APPROVAL,
// ALLOW_WITH_CONSTRAINTS, ESCALATE) out of this vocabulary — those are a
// distinct vocabulary owned by a later milestone (C03 Governance).
//
// Invariants preserved here:
//   - UNKNOWN_OUTCOME is a distinct category; it is never collapsed into
//     INTERNAL_FAILURE or any generic failure.
//   - CANCELLATION is distinct from failure.
//   - Validation, configuration, dependency, timeout, authorization/policy,
//     and internal failures remain distinguishable.
//
// At M0, UNKNOWN_OUTCOME exists only as a shared semantic/type. Reconciliation
// is NOT implemented in this milestone.
package nerrors

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Category is the canonical NEXUS error category.
//
// The set below is copied verbatim from contracts/CORE_INTERFACE_CONTRACTS.md
// §3. Ordering and spelling are contract-stable.
type Category string

const (
	CategoryValidation          Category = "VALIDATION"
	CategoryAuth                Category = "AUTH"
	CategoryAuthorization       Category = "AUTHORIZATION"
	CategoryPolicyDenied        Category = "POLICY_DENIED"
	CategoryApprovalRequired    Category = "APPROVAL_REQUIRED"
	CategoryResourceUnavailable Category = "RESOURCE_UNAVAILABLE"
	CategoryTimeout             Category = "TIMEOUT"
	CategoryDependencyFailure   Category = "DEPENDENCY_FAILURE"
	CategoryRateLimit           Category = "RATE_LIMIT"
	CategoryConflict            Category = "CONFLICT"
	CategoryUnknownOutcome      Category = "UNKNOWN_OUTCOME"
	CategoryCancellation        Category = "CANCELLATION"
	CategorySecurityRejection   Category = "SECURITY_REJECTION"
	CategoryInternalFailure     Category = "INTERNAL_FAILURE"
)

// allCategories is the canonical, closed set of error categories.
var allCategories = map[Category]struct{}{
	CategoryValidation:          {},
	CategoryAuth:                {},
	CategoryAuthorization:       {},
	CategoryPolicyDenied:        {},
	CategoryApprovalRequired:    {},
	CategoryResourceUnavailable: {},
	CategoryTimeout:             {},
	CategoryDependencyFailure:   {},
	CategoryRateLimit:           {},
	CategoryConflict:            {},
	CategoryUnknownOutcome:      {},
	CategoryCancellation:        {},
	CategorySecurityRejection:   {},
	CategoryInternalFailure:     {},
}

// IsValid reports whether c is one of the canonical error categories.
func (c Category) IsValid() bool {
	_, ok := allCategories[c]
	return ok
}

// retryableByDefault captures the contract's default retryability per category
// (CORE_INTERFACE_CONTRACTS.md §3). "Maybe" categories default to true here and
// may be overridden per error instance by the producer.
var retryableByDefault = map[Category]bool{
	CategoryValidation:          false,
	CategoryAuth:                false,
	CategoryAuthorization:       false,
	CategoryPolicyDenied:        false,
	CategoryApprovalRequired:    false,
	CategoryResourceUnavailable: true,
	CategoryTimeout:             true,
	CategoryDependencyFailure:   true,
	CategoryRateLimit:           true,
	CategoryConflict:            true,
	CategoryUnknownOutcome:      false,
	CategoryCancellation:        false,
	CategorySecurityRejection:   false,
	CategoryInternalFailure:     true,
}

// Error is the canonical NEXUS error envelope.
//
// JSON field names match contracts/CORE_INTERFACE_CONTRACTS.md §3 exactly so
// that the M0 foundation can evolve into the locked contract without a wire
// change.
type Error struct {
	Code          string         `json:"code"`
	Category      Category       `json:"category"`
	Message       string         `json:"message"`
	Details       map[string]any `json:"details,omitempty"`
	Retryable     bool           `json:"retryable"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Timestamp     time.Time      `json:"timestamp"`

	// wrapped is the optional underlying cause. It is not serialized.
	wrapped error
}

// Error implements the standard error interface.
func (e *Error) Error() string {
	if e == nil {
		return "<nil nexus error>"
	}
	if e.Code == "" {
		return fmt.Sprintf("%s: %s", e.Category, e.Message)
	}
	return fmt.Sprintf("%s (%s): %s", e.Code, e.Category, e.Message)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.wrapped }

// MarshalJSON enforces the canonical envelope shape and refuses to serialize an
// unknown category, guarding against accidental vocabulary drift.
func (e *Error) MarshalJSON() ([]byte, error) {
	type alias Error
	if !e.Category.IsValid() {
		return nil, fmt.Errorf("nerrors: refusing to serialize invalid category %q", e.Category)
	}
	return json.Marshal((*alias)(e))
}

// New builds an Error with the contract default retryability for its category.
func New(code string, category Category, message string) *Error {
	return &Error{
		Code:      code,
		Category:  category,
		Message:   message,
		Retryable: retryableByDefault[category],
		Timestamp: time.Now().UTC(),
	}
}

// Wrap builds an Error that wraps cause, inheriting default retryability.
func Wrap(cause error, code string, category Category, message string) *Error {
	e := New(code, category, message)
	e.wrapped = cause
	return e
}

// WithRetryable overrides the category default retryability explicitly.
func (e *Error) WithRetryable(retryable bool) *Error {
	e.Retryable = retryable
	return e
}

// WithDetail attaches a single structured detail key. Never place secrets here.
func (e *Error) WithDetail(key string, value any) *Error {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

// WithCorrelation attaches a correlation_id for tracing (tracing only; it grants
// no authority — INV-19).
func (e *Error) WithCorrelation(correlationID string) *Error {
	e.CorrelationID = correlationID
	return e
}

// CategoryOf extracts the canonical category from any error, defaulting to
// INTERNAL_FAILURE for unrecognized errors. It is the single boundary where an
// arbitrary error becomes a canonical category, preventing category drift.
func CategoryOf(err error) Category {
	var e *Error
	if errors.As(err, &e) {
		return e.Category
	}
	return CategoryInternalFailure
}

// ---------- Constructors for the categories used by the M0 foundation. ----------

// Validation reports input that did not pass schema or business validation.
func Validation(code, message string) *Error {
	return New(code, CategoryValidation, message)
}

// Configuration reports invalid or missing required configuration.
//
// Configuration failures are represented as VALIDATION because the contract's
// closed category set has no separate CONFIGURATION category; the distinguishing
// signal lives in Code (e.g. "config.invalid") and in Details. This avoids
// inventing a non-canonical category.
func Configuration(code, message string) *Error {
	return New(code, CategoryValidation, message)
}

// Dependency reports an upstream dependency failure.
func Dependency(code, message string) *Error {
	return New(code, CategoryDependencyFailure, message)
}

// Timeout reports an operation that exceeded its time limit.
func Timeout(code, message string) *Error {
	return New(code, CategoryTimeout, message)
}

// Cancellation reports an operation that was cancelled (never a failure).
func Cancellation(code, message string) *Error {
	return New(code, CategoryCancellation, message)
}

// Internal reports an unexpected internal error.
func Internal(code, message string) *Error {
	return New(code, CategoryInternalFailure, message)
}

// Auth reports an authentication failure (the identity was not authenticated).
//
// AUTH is distinct from AUTHORIZATION: AUTH means "we do not know/trust who you
// are"; AUTHORIZATION means "we know who you are, but you may not do this".
func Auth(code, message string) *Error {
	return New(code, CategoryAuth, message)
}

// Authorization reports that an established identity lacks authority for an
// action. It is distinct from POLICY_DENIED (a governance policy decision) and
// from AUTH (authentication).
func Authorization(code, message string) *Error {
	return New(code, CategoryAuthorization, message)
}

// SecurityRejection reports a security boundary violation.
func SecurityRejection(code, message string) *Error {
	return New(code, CategorySecurityRejection, message)
}

// UnknownOutcome reports an operation whose outcome is uncertain.
//
// The distinction UNKNOWN_OUTCOME ≠ FAILURE and UNKNOWN_OUTCOME ≠ SUCCESS is
// preserved: this constructor never maps to INTERNAL_FAILURE. Reconciliation is
// a later-milestone concern and is intentionally not implemented at M0.
func UnknownOutcome(code, message string) *Error {
	return New(code, CategoryUnknownOutcome, message)
}

// Is reports whether target matches by canonical category.
func Is(err, target *Error) bool {
	if err == nil || target == nil {
		return false
	}
	return err.Category == target.Category
}
