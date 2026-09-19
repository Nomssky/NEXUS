package nerrors

import (
	"encoding/json"
	"errors"
	"testing"
)

// The canonical category set is exactly the 14 categories from
// CORE_INTERFACE_CONTRACTS.md §3.
func TestCanonicalCategories(t *testing.T) {
	want := []Category{
		CategoryValidation, CategoryAuth, CategoryAuthorization,
		CategoryPolicyDenied, CategoryApprovalRequired, CategoryResourceUnavailable,
		CategoryTimeout, CategoryDependencyFailure, CategoryRateLimit,
		CategoryConflict, CategoryUnknownOutcome, CategoryCancellation,
		CategorySecurityRejection, CategoryInternalFailure,
	}
	if len(allCategories) != 14 {
		t.Fatalf("expected exactly 14 canonical categories, got %d", len(allCategories))
	}
	for _, c := range want {
		if !c.IsValid() {
			t.Fatalf("category %s should be valid", c)
		}
	}
	if Category("DENY").IsValid() {
		t.Fatal("governance outcome DENY must not be a valid error category")
	}
}

// UNKNOWN_OUTCOME is distinct from INTERNAL_FAILURE (never collapsed).
func TestUnknownOutcomeDistinct(t *testing.T) {
	if CategoryUnknownOutcome == CategoryInternalFailure {
		t.Fatal("UNKNOWN_OUTCOME must not equal INTERNAL_FAILURE")
	}
	e := UnknownOutcome("tool.unknown", "outcome uncertain")
	if e.Category != CategoryUnknownOutcome {
		t.Fatalf("expected UNKNOWN_OUTCOME, got %s", e.Category)
	}
}

// CANCELLATION is distinct from failure.
func TestCancellationDistinct(t *testing.T) {
	e := Cancellation("op.cancelled", "cancelled by owner")
	if e.Category != CategoryCancellation {
		t.Fatalf("expected CANCELLATION, got %s", e.Category)
	}
	if e.Retryable {
		t.Fatal("cancellation must not be retryable")
	}
}

// Validation/config failures are not retryable; dependency failures are.
func TestRetryabilityDefaults(t *testing.T) {
	if Validation("v", "bad").Retryable {
		t.Fatal("validation must not be retryable")
	}
	if !Dependency("d", "down").Retryable {
		t.Fatal("dependency failure should be retryable by default")
	}
}

// The envelope serializes with contract field names.
func TestEnvelopeJSONShape(t *testing.T) {
	e := Validation("config.invalid", "bad value").WithCorrelation("corr-1")
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"code", "category", "message", "retryable", "correlation_id", "timestamp"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("expected envelope field %q, got %s", k, string(data))
		}
	}
}

// Serializing an invalid category is refused (guards vocabulary drift).
func TestMarshalRejectsInvalidCategory(t *testing.T) {
	e := New("x", Category("NOT_A_CATEGORY"), "bad")
	if _, err := json.Marshal(e); err == nil {
		t.Fatal("expected marshal error for invalid category")
	}
}

// Wrapping preserves the cause chain.
func TestWrapUnwrap(t *testing.T) {
	cause := errors.New("root")
	e := Wrap(cause, "dep", CategoryDependencyFailure, "wrap")
	if !errors.Is(e, cause) {
		t.Fatal("expected wrapped cause to be discoverable")
	}
	if CategoryOf(e) != CategoryDependencyFailure {
		t.Fatalf("expected DEPENDENCY_FAILURE, got %s", CategoryOf(e))
	}
}

// CategoryOf defaults unknown errors to INTERNAL_FAILURE.
func TestCategoryOfUnknown(t *testing.T) {
	if CategoryOf(errors.New("plain")) != CategoryInternalFailure {
		t.Fatal("expected INTERNAL_FAILURE for a plain error")
	}
}
