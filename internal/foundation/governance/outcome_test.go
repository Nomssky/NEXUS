package governance

import (
	"testing"
)

// TEST-M2-001: Outcome string representation
func TestOutcomeString(t *testing.T) {
	tests := []struct {
		outcome Outcome
		want    string
	}{
		{ALLOW, "ALLOW"},
		{DENY, "DENY"},
		{REQUIRE_APPROVAL, "REQUIRE_APPROVAL"},
		{ALLOW_WITH_CONSTRAINTS, "ALLOW_WITH_CONSTRAINTS"},
		{ESCALATE, "ESCALATE"},
		{Outcome(99), "Outcome(99)"},
	}

	for _, tt := range tests {
		if got := tt.outcome.String(); got != tt.want {
			t.Errorf("Outcome(%d).String() = %q, want %q", int(tt.outcome), got, tt.want)
		}
	}
}

// TEST-M2-002: Outcome validity check
func TestOutcomeIsValid(t *testing.T) {
	valid := []Outcome{ALLOW, DENY, REQUIRE_APPROVAL, ALLOW_WITH_CONSTRAINTS, ESCALATE}
	for _, o := range valid {
		if !o.IsValid() {
			t.Errorf("Outcome(%d).IsValid() = false, want true", int(o))
		}
	}

	invalid := []Outcome{0, -1, 6, 99}
	for _, o := range invalid {
		if o.IsValid() {
			t.Errorf("Outcome(%d).IsValid() = true, want false", int(o))
		}
	}
}

// TEST-M2-003: ParseOutcome
func TestParseOutcome(t *testing.T) {
	tests := []struct {
		input string
		want  Outcome
		err   bool
	}{
		{"ALLOW", ALLOW, false},
		{"DENY", DENY, false},
		{"REQUIRE_APPROVAL", REQUIRE_APPROVAL, false},
		{"ALLOW_WITH_CONSTRAINTS", ALLOW_WITH_CONSTRAINTS, false},
		{"ESCALATE", ESCALATE, false},
		{"allow", ALLOW, false},
		{" deny ", DENY, false},
		{"INVALID", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseOutcome(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("ParseOutcome(%q) error = %v, wantErr %v", tt.input, err, tt.err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseOutcome(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TEST-M2-004: IsAllowing
func TestOutcomeIsAllowing(t *testing.T) {
	allowing := []Outcome{ALLOW, ALLOW_WITH_CONSTRAINTS}
	for _, o := range allowing {
		if !o.IsAllowing() {
			t.Errorf("Outcome(%d).IsAllowing() = false, want true", int(o))
		}
	}

	notAllowing := []Outcome{DENY, REQUIRE_APPROVAL, ESCALATE}
	for _, o := range notAllowing {
		if o.IsAllowing() {
			t.Errorf("Outcome(%d).IsAllowing() = true, want false", int(o))
		}
	}
}

// TEST-M2-005: RequiresDecision
func TestOutcomeRequiresDecision(t *testing.T) {
	requiresDecision := []Outcome{REQUIRE_APPROVAL, ESCALATE}
	for _, o := range requiresDecision {
		if !o.RequiresDecision() {
			t.Errorf("Outcome(%d).RequiresDecision() = false, want true", int(o))
		}
	}

	noDecision := []Outcome{ALLOW, DENY, ALLOW_WITH_CONSTRAINTS}
	for _, o := range noDecision {
		if o.RequiresDecision() {
			t.Errorf("Outcome(%d).RequiresDecision() = true, want false", int(o))
		}
	}
}

// TEST-M2-006: Exactly 5 canonical outcomes exist
func TestExactlyFiveCanonicalOutcomes(t *testing.T) {
	allOutcomes := []Outcome{ALLOW, DENY, REQUIRE_APPROVAL, ALLOW_WITH_CONSTRAINTS, ESCALATE}
	if len(allOutcomes) != 5 {
		t.Errorf("expected exactly 5 canonical outcomes, got %d", len(allOutcomes))
	}

	// Verify each is valid and unique
	seen := make(map[Outcome]bool)
	for _, o := range allOutcomes {
		if !o.IsValid() {
			t.Errorf("canonical outcome %d is not valid", int(o))
		}
		if seen[o] {
			t.Errorf("duplicate canonical outcome %d", int(o))
		}
		seen[o] = true
	}
}
