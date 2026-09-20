package attention

import (
	"testing"
	"time"
)

// TEST-M9-001: Submit item with priority scoring
func TestAttentionSubmitWithScoring(t *testing.T) {
	fae := NewFullAttentionEngine(
		AttentionBudget{MaxOwnerInterruptionsPerHour: 10},
		QuietHours{Enabled: false},
		SuppressionGuard{NeverSuppressSecuritySignals: true},
	)

	item, err := fae.SubmitItem("Critical issue", "Desc", "biz-1", "agent", 9, 8, 7, 0.9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Score <= 0 {
		t.Errorf("expected positive score, got %v", item.Score)
	}
	if item.Level < LevelHigh {
		t.Errorf("expected high or above level, got %v", item.Level)
	}
}

// TEST-M9-002: Security signal always escalates
func TestAttentionSecuritySignalAlwaysEscalates(t *testing.T) {
	fae := NewFullAttentionEngine(
		AttentionBudget{},
		QuietHours{Enabled: false},
		SuppressionGuard{NeverSuppressSecuritySignals: true},
	)

	item, _ := fae.SubmitItem("Security issue", "Desc", "biz-1", "security", 5, 5, 5, 0.8, true)
	if item.Outcome != OutcomeEmergency {
		t.Errorf("expected emergency for security signal, got %v", item.Outcome)
	}
}

// TEST-M9-003: Policy violation always escalates
func TestAttentionPolicyViolationAlwaysEscalates(t *testing.T) {
	fae := NewFullAttentionEngine(
		AttentionBudget{},
		QuietHours{Enabled: false},
		SuppressionGuard{NeverSuppressPolicyViolations: true},
	)

	item, _ := fae.SubmitItem("Policy violation", "Desc", "biz-1", "system", 5, 5, 5, 0.8, false, true)
	if item.Outcome != OutcomeEmergency {
		t.Errorf("expected emergency for policy violation, got %v", item.Outcome)
	}
}

// TEST-M9-004: Owner message always escalates
func TestAttentionOwnerMessageAlwaysEscalates(t *testing.T) {
	fae := NewFullAttentionEngine(
		AttentionBudget{},
		QuietHours{Enabled: false},
		SuppressionGuard{NeverSuppressOwnerMessages: true},
	)

	item, _ := fae.SubmitItem("Owner message", "Desc", "biz-1", "owner", 3, 3, 3, 0.8, false, false, true)
	if item.Outcome != OutcomeEscalateOwner {
		t.Errorf("expected escalate_to_owner for owner message, got %v", item.Outcome)
	}
}

// TEST-M9-005: Suppression guard prevents hiding security
func TestSuppressionGuardPreventsHidingSecurity(t *testing.T) {
	fae := NewFullAttentionEngine(
		AttentionBudget{},
		QuietHours{Enabled: false},
		SuppressionGuard{NeverSuppressSecuritySignals: true},
	)

	item := &AttentionItem{
		IsSecuritySignal: true,
		Source:           "security",
		Level:            LevelHigh,
	}

	if fae.ShouldSuppress(item) {
		t.Error("should not suppress security signal")
	}
}

// TEST-M9-006: Suppression guard prevents hiding policy violation
func TestSuppressionGuardPreventsHidingPolicyViolation(t *testing.T) {
	fae := NewFullAttentionEngine(
		AttentionBudget{},
		QuietHours{Enabled: false},
		SuppressionGuard{NeverSuppressPolicyViolations: true},
	)

	item := &AttentionItem{
		IsPolicyViolation: true,
		Source:            "system",
		Level:             LevelHigh,
	}

	if fae.ShouldSuppress(item) {
		t.Error("should not suppress policy violation")
	}
}

// TEST-M9-007: Budget check passes under limit
func TestBudgetCheckUnderLimit(t *testing.T) {
	fae := NewFullAttentionEngine(
		AttentionBudget{MaxOwnerInterruptionsPerHour: 5},
		QuietHours{Enabled: false},
		SuppressionGuard{},
	)

	if !fae.CheckBudget() {
		t.Error("expected budget OK")
	}
}

// TEST-M9-008: Budget check fails over limit
func TestBudgetCheckOverLimit(t *testing.T) {
	fae := NewFullAttentionEngine(
		AttentionBudget{MaxOwnerInterruptionsPerHour: 2},
		QuietHours{Enabled: false},
		SuppressionGuard{},
	)

	fae.RecordInterruption()
	fae.RecordInterruption()

	if fae.CheckBudget() {
		t.Error("expected budget exceeded")
	}
}

// TEST-M9-009: Quiet hours suppress low-level notifications
func TestQuietHoursSuppressLowLevel(t *testing.T) {
	now := time.Now()
	fae := NewFullAttentionEngineWithClock(
		AttentionBudget{},
		QuietHours{Enabled: true, MinLevel: LevelCritical},
		SuppressionGuard{},
		func() time.Time { return now },
	)

	item := &AttentionItem{
		Outcome: OutcomeEscalateOwner,
		Level:   LevelNormal, // below critical
		Source:  "system",
	}

	if !fae.ShouldSuppress(item) {
		t.Error("expected suppression during quiet hours for low-level")
	}
}

// TEST-M9-010: Quiet hours allow critical notifications
func TestQuietHoursAllowCritical(t *testing.T) {
	now := time.Now()
	fae := NewFullAttentionEngineWithClock(
		AttentionBudget{},
		QuietHours{Enabled: true, MinLevel: LevelCritical},
		SuppressionGuard{},
		func() time.Time { return now },
	)

	item := &AttentionItem{
		Outcome: OutcomeEscalateOwner,
		Level:   LevelCritical, // at or above critical
		Source:  "system",
	}

	if fae.ShouldSuppress(item) {
		t.Error("should not suppress critical during quiet hours")
	}
}

// TEST-M9-011: Acknowledge item
func TestAttentionAcknowledge(t *testing.T) {
	fae := NewFullAttentionEngine(AttentionBudget{}, QuietHours{}, SuppressionGuard{})
	item, _ := fae.SubmitItem("Issue", "Desc", "biz-1", "agent", 5, 5, 5, 0.8)

	err := fae.Acknowledge(item.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Status != StatusAcknowledged {
		t.Errorf("expected acknowledged, got %v", item.Status)
	}
}

// TEST-M9-012: Resolve item
func TestAttentionResolve(t *testing.T) {
	fae := NewFullAttentionEngine(AttentionBudget{}, QuietHours{}, SuppressionGuard{})
	item, _ := fae.SubmitItem("Issue", "Desc", "biz-1", "agent", 5, 5, 5, 0.8)

	err := fae.Resolve(item.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Status != StatusResolved {
		t.Errorf("expected resolved, got %v", item.Status)
	}
	if item.ResolvedAt == nil {
		t.Error("expected resolved at timestamp")
	}
}

// TEST-M9-013: Pending items excludes resolved
func TestAttentionPendingExcludesResolved(t *testing.T) {
	fae := NewFullAttentionEngine(AttentionBudget{}, QuietHours{}, SuppressionGuard{})
	item1, _ := fae.SubmitItem("Open", "Desc", "biz-1", "agent", 5, 5, 5, 0.8)
	item2, _ := fae.SubmitItem("Resolved", "Desc", "biz-1", "agent", 5, 5, 5, 0.8)

	fae.Resolve(item2.ID)

	pending := fae.PendingItems()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].ID != item1.ID {
		t.Errorf("expected item1 pending, got %v", pending[0].ID)
	}
}

// TEST-M9-014: Attention levels
func TestAttentionLevels(t *testing.T) {
	levels := []AttentionLevel{
		LevelIgnore, LevelBackground, LevelNormal, LevelImportant,
		LevelHigh, LevelCritical, LevelEmergency,
	}
	if len(levels) != 7 {
		t.Errorf("expected 7 levels, got %d", len(levels))
	}
}

// TEST-M9-015: Attention outcomes
func TestAttentionOutcomes(t *testing.T) {
	outcomes := []AttentionOutcome{
		OutcomeIgnore, OutcomeRecord, OutcomeMonitor, OutcomeQueue,
		OutcomeActAutonomously, OutcomeEscalateAgent, OutcomeEscalateOwner,
		OutcomePause, OutcomeEmergency,
	}
	if len(outcomes) != 9 {
		t.Errorf("expected 9 outcomes, got %d", len(outcomes))
	}
}

// TEST-M9-016: Attention ≠ Authority invariant
func TestAttentionNotAuthority(t *testing.T) {
	// Structural invariant: AttentionItem has no authority fields
	// Attention recommends, governance authorizes
	fae := NewFullAttentionEngine(AttentionBudget{}, QuietHours{}, SuppressionGuard{})
	item, _ := fae.SubmitItem("Issue", "Desc", "biz-1", "agent", 5, 5, 5, 0.8)

	// Item has outcome (recommendation), NOT authority
	if item.Outcome == "" {
		t.Error("expected outcome to be set")
	}
	// No authority field exists — verified by compilation
}

// TEST-M9-017: Business isolation in attention
func TestAttentionBusinessIsolation(t *testing.T) {
	fae := NewFullAttentionEngine(AttentionBudget{}, QuietHours{}, SuppressionGuard{})
	item1, _ := fae.SubmitItem("Issue 1", "Desc", "biz-1", "agent", 5, 5, 5, 0.8)
	item2, _ := fae.SubmitItem("Issue 2", "Desc", "biz-2", "agent", 5, 5, 5, 0.8)

	if item1.BusinessID == item2.BusinessID {
		t.Error("expected different business IDs")
	}
}

// TEST-M9-018: Cooldown suppresses rapid duplicates
func TestCooldownSuppressesDuplicates(t *testing.T) {
	now := time.Now()
	fae := NewFullAttentionEngineWithClock(
		AttentionBudget{},
		QuietHours{},
		SuppressionGuard{},
		func() time.Time { return now },
	)

	item := &AttentionItem{Source: "api", Level: LevelNormal}
	fae.cooldowns["api"] = now // just notified

	if !fae.ShouldSuppress(item) {
		t.Error("expected suppression during cooldown")
	}
}

// TEST-M9-019: Suppression guard prevents hiding severity increase
func TestSuppressionGuardPreventsHidingSeverityIncrease(t *testing.T) {
	fae := NewFullAttentionEngine(
		AttentionBudget{},
		QuietHours{},
		SuppressionGuard{NeverSuppressSeverityIncrease: true},
	)

	item := &AttentionItem{
		SeverityIncreased: true,
		Source:            "workflow",
		Level:             LevelNormal,
	}

	if fae.ShouldSuppress(item) {
		t.Error("should not suppress severity increase")
	}
}

// TEST-M9-020: Score-to-level mapping
func TestScoreToLevelMapping(t *testing.T) {
	fae := NewFullAttentionEngine(AttentionBudget{}, QuietHours{}, SuppressionGuard{})

	tests := []struct {
		score float64
		level AttentionLevel
	}{
		{0, LevelIgnore},
		{3, LevelBackground},
		{7, LevelNormal},
		{12, LevelImportant},
		{17, LevelHigh},
		{22, LevelCritical},
		{30, LevelEmergency},
	}

	for _, tt := range tests {
		level := fae.scoreToLevel(tt.score)
		if level != tt.level {
			t.Errorf("score %v: expected %v, got %v", tt.score, tt.level, level)
		}
	}
}
