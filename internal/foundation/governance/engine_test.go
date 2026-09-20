package governance

import (
	"testing"
	"time"
)

// fixedClock returns a function that always returns the same time.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// TEST-M2-007: Default deny when no policies loaded
func TestEngineDefaultDenyNoPolicies(t *testing.T) {
	engine := NewEngine(nil)
	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    time.Now(),
	}

	decision := engine.Evaluate(req)
	if decision.Outcome != DENY {
		t.Errorf("expected DENY, got %v", decision.Outcome)
	}
	if decision.Reason == "" {
		t.Error("expected non-empty reason for default deny")
	}
}

// TEST-M2-008: Allow when matching policy exists
func TestEngineAllowMatchingPolicy(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-1",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        ALLOW,
			Precedence:    10,
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	decision := engine.Evaluate(req)
	if decision.Outcome != ALLOW {
		t.Errorf("expected ALLOW, got %v", decision.Outcome)
	}
	if decision.MatchedPolicyID != "pol-1" {
		t.Errorf("expected matched policy pol-1, got %v", decision.MatchedPolicyID)
	}
}

// TEST-M2-009: Deny when no policy matches scope
func TestEngineDenyScopeMismatch(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-biz1",
			PolicyVersion: "1.0",
			BusinessID:    "business-1",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        ALLOW,
			Precedence:    10,
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		BusinessID:   "business-2", // different business
		Timestamp:    now,
	}

	decision := engine.Evaluate(req)
	if decision.Outcome != DENY {
		t.Errorf("expected DENY for scope mismatch, got %v", decision.Outcome)
	}
}

// TEST-M2-010: More-restrictive-wins when policies conflict
func TestEngineMoreRestrictiveWins(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-allow",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        ALLOW,
			Precedence:    20, // higher precedence
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
		{
			PolicyID:      "pol-deny",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        DENY, // more restrictive
			Precedence:    10,   // lower precedence
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	decision := engine.Evaluate(req)
	// DENY is more restrictive than ALLOW, so DENY wins
	if decision.Outcome != DENY {
		t.Errorf("expected DENY (more-restrictive-wins), got %v", decision.Outcome)
	}
}

// TEST-M2-011: Higher precedence wins within same restrictiveness
func TestEngineHigherPrecedenceWins(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-low",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        DENY,
			Precedence:    10,
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
		{
			PolicyID:      "pol-high",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        DENY, // same effect
			Precedence:    20,   // higher precedence
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	decision := engine.Evaluate(req)
	if decision.MatchedPolicyID != "pol-high" {
		t.Errorf("expected higher precedence policy pol-high, got %v", decision.MatchedPolicyID)
	}
}

// TEST-M2-012: Expired policy is ignored
func TestEngineExpiredPolicyIgnored(t *testing.T) {
	now := time.Now()
	past := now.Add(-2 * time.Hour)
	expired := now.Add(-time.Hour)

	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:       "pol-expired",
			PolicyVersion:  "1.0",
			Status:         PolicyStatusActive,
			Subject:        Subject{SubjectType: "all"},
			Action:         Action{ActionType: "execute_tool"},
			Resource:       Resource{ResourceType: "tool"},
			Effect:         ALLOW,
			Precedence:     10,
			EffectiveFrom:  past,
			EffectiveUntil: &expired,
			CreatedAt:      past,
			CreatedBy:      "admin",
		},
	}, fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	decision := engine.Evaluate(req)
	// Policy expired, so default deny
	if decision.Outcome != DENY {
		t.Errorf("expected DENY for expired policy, got %v", decision.Outcome)
	}
}

// TEST-M2-013: Disabled policy is ignored
func TestEngineDisabledPolicyIgnored(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-disabled",
			PolicyVersion: "1.0",
			Status:        PolicyStatusDisabled,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        ALLOW,
			Precedence:    10,
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	decision := engine.Evaluate(req)
	if decision.Outcome != DENY {
		t.Errorf("expected DENY for disabled policy, got %v", decision.Outcome)
	}
}

// TEST-M2-014: REQUIRE_APPROVAL outcome with approval config
func TestEngineRequireApproval(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-approval",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        REQUIRE_APPROVAL,
			Precedence:    10,
			ApprovalConfig: &ApprovalConfig{
				ApproverType:           "human",
				TimeoutSeconds:         3600,
				AutoDenyOnTimeout:      true,
				SelfApprovalProhibited: true,
			},
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	decision := engine.Evaluate(req)
	if decision.Outcome != REQUIRE_APPROVAL {
		t.Errorf("expected REQUIRE_APPROVAL, got %v", decision.Outcome)
	}
	if decision.ApprovalRequired == nil {
		t.Error("expected approval config in decision")
	}
	if !decision.RequiresApproval() {
		t.Error("expected RequiresApproval() = true")
	}
}

// TEST-M2-015: ALLOW_WITH_CONSTRAINTS includes constraints
func TestEngineAllowWithConstraints(t *testing.T) {
	now := time.Now()
	constraints := []Constraint{
		{ConstraintID: "c-1", ConstraintType: "time_limit", Expression: "business_hours_only", Severity: "mandatory"},
	}

	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-constrained",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        ALLOW_WITH_CONSTRAINTS,
			Constraints:   constraints,
			Precedence:    10,
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	decision := engine.Evaluate(req)
	if decision.Outcome != ALLOW_WITH_CONSTRAINTS {
		t.Errorf("expected ALLOW_WITH_CONSTRAINTS, got %v", decision.Outcome)
	}
	if len(decision.Constraints) != 1 {
		t.Errorf("expected 1 constraint, got %d", len(decision.Constraints))
	}
	if !decision.IsAllowing() {
		t.Error("expected IsAllowing() = true for ALLOW_WITH_CONSTRAINTS")
	}
}

// TEST-M2-016: Fail-safe deny when engine is nil
func TestFailSafeNilEngine(t *testing.T) {
	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    time.Now(),
	}

	decision := FailSafe(nil, req)
	if decision.Outcome != DENY {
		t.Errorf("expected DENY from fail-safe, got %v", decision.Outcome)
	}
}

// TEST-M2-017: ESCALATE outcome
func TestEngineEscalate(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-escalate",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        ESCALATE,
			Precedence:    10,
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	decision := engine.Evaluate(req)
	if decision.Outcome != ESCALATE {
		t.Errorf("expected ESCALATE, got %v", decision.Outcome)
	}
	if !decision.IsEscalation() {
		t.Error("expected IsEscalation() = true")
	}
}

// TEST-M2-018: Subject matching - all subjects
func TestEngineSubjectAll(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-all",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        ALLOW,
			Precedence:    10,
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	// Different actors should all match
	actors := []string{"agent-1", "agent-2", "human-admin", "system"}
	for _, actor := range actors {
		req := Request{
			Actor:        actor,
			Action:       "execute_tool",
			Resource:     "tool-1",
			ResourceType: "tool",
			Timestamp:    now,
		}

		decision := engine.Evaluate(req)
		if decision.Outcome != ALLOW {
			t.Errorf("expected ALLOW for actor %s, got %v", actor, decision.Outcome)
		}
	}
}

// TEST-M2-019: Subject matching - specific identity
func TestEngineSubjectIdentity(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-specific",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "identity", SubjectIDs: []string{"agent-1"}},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        ALLOW,
			Precedence:    10,
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	// Matching actor
	req1 := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}
	decision1 := engine.Evaluate(req1)
	if decision1.Outcome != ALLOW {
		t.Errorf("expected ALLOW for agent-1, got %v", decision1.Outcome)
	}

	// Non-matching actor
	req2 := Request{
		Actor:        "agent-2",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}
	decision2 := engine.Evaluate(req2)
	if decision2.Outcome != DENY {
		t.Errorf("expected DENY for agent-2, got %v", decision2.Outcome)
	}
}

// TEST-M2-020: Business scope matching
func TestEngineBusinessScope(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-biz",
			PolicyVersion: "1.0",
			BusinessID:    "business-1",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        ALLOW,
			Precedence:    10,
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	// Matching business
	req1 := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		BusinessID:   "business-1",
		Timestamp:    now,
	}
	decision1 := engine.Evaluate(req1)
	if decision1.Outcome != ALLOW {
		t.Errorf("expected ALLOW for business-1, got %v", decision1.Outcome)
	}

	// Non-matching business
	req2 := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		BusinessID:   "business-2",
		Timestamp:    now,
	}
	decision2 := engine.Evaluate(req2)
	if decision2.Outcome != DENY {
		t.Errorf("expected DENY for business-2, got %v", decision2.Outcome)
	}
}

// TEST-M2-021: Policy override relationship
func TestEnginePolicyOverride(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-base",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        ALLOW,
			Precedence:    20,
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
		{
			PolicyID:          "pol-override",
			PolicyVersion:     "1.0",
			Status:            PolicyStatusActive,
			Subject:           Subject{SubjectType: "all"},
			Action:            Action{ActionType: "execute_tool"},
			Resource:          Resource{ResourceType: "tool"},
			Effect:            DENY,
			Precedence:        10,
			OverridePolicyIDs: []string{"pol-base"},
			EffectiveFrom:     now.Add(-time.Hour),
			CreatedAt:         now.Add(-time.Hour),
			CreatedBy:         "admin",
		},
	}, fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	decision := engine.Evaluate(req)
	// pol-override overrides pol-base, even though pol-base has higher precedence
	if decision.Outcome != DENY {
		t.Errorf("expected DENY from override policy, got %v", decision.Outcome)
	}
	if decision.MatchedPolicyID != "pol-override" {
		t.Errorf("expected matched policy pol-override, got %v", decision.MatchedPolicyID)
	}
}

// TEST-M2-022: ValidateOutcome structural check
func TestValidateOutcome(t *testing.T) {
	valid := []Outcome{ALLOW, DENY, REQUIRE_APPROVAL, ALLOW_WITH_CONSTRAINTS, ESCALATE}
	for _, o := range valid {
		if err := ValidateOutcome(o); err != nil {
			t.Errorf("ValidateOutcome(%v) error = %v", o, err)
		}
	}

	invalid := []Outcome{0, -1, 6, 99}
	for _, o := range invalid {
		if err := ValidateOutcome(o); err == nil {
			t.Errorf("ValidateOutcome(%v) expected error, got nil", o)
		}
	}
}
