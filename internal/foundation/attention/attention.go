// Package attention implements the NEXUS Attention & Priority Intelligence (C10).
//
// C10 full provides priority scoring, aggregation, dedup, suppression guardrails,
// cooldown, budget, quiet-hours, escalation, and notification routing.
//
// Key invariants:
//   - ATTENTION ≠ AUTHORITY: attention recommends, governance authorizes
//   - Suppression never hides: severity increases, scope changes, new evidence,
//     policy violations, critical security signals, direct owner messages
//   - Owner has finite attention: budget enforcement
//   - Autonomous resolution: when policy allows, agent handles without owner
//
// Flow:
//
//	Event → Relevance → Priority → Attention Decision → Ignore/Record/Monitor/Act/Escalate
package attention

import (
	"fmt"
	"sync"
	"time"
)

// AttentionLevel classifies the urgency of an attention item.
type AttentionLevel int

const (
	LevelIgnore     AttentionLevel = 0
	LevelBackground AttentionLevel = 1
	LevelNormal     AttentionLevel = 2
	LevelImportant  AttentionLevel = 3
	LevelHigh       AttentionLevel = 4
	LevelCritical   AttentionLevel = 5
	LevelEmergency  AttentionLevel = 6
)

// AttentionOutcome determines what happens to an attention item.
type AttentionOutcome string

const (
	OutcomeIgnore          AttentionOutcome = "ignore"
	OutcomeRecord          AttentionOutcome = "record"
	OutcomeMonitor         AttentionOutcome = "monitor"
	OutcomeQueue           AttentionOutcome = "queue"
	OutcomeActAutonomously AttentionOutcome = "act_autonomously"
	OutcomeEscalateAgent   AttentionOutcome = "escalate_to_agent"
	OutcomeEscalateOwner   AttentionOutcome = "escalate_to_owner"
	OutcomePause           AttentionOutcome = "pause"
	OutcomeEmergency       AttentionOutcome = "emergency"
)

// AttentionStatus tracks the lifecycle of an attention item.
type AttentionStatus string

const (
	StatusDetected     AttentionStatus = "detected"
	StatusEvaluating   AttentionStatus = "evaluating"
	StatusRanked       AttentionStatus = "ranked"
	StatusQueued       AttentionStatus = "queued"
	StatusSuppressed   AttentionStatus = "suppressed"
	StatusAcknowledged AttentionStatus = "acknowledged"
	StatusInProgress   AttentionStatus = "in_progress"
	StatusResolved     AttentionStatus = "resolved"
	StatusExpired      AttentionStatus = "expired"
)

// EscalationLevel defines how far up the chain to escalate.
type EscalationLevel int

const (
	EscalateAgent     EscalationLevel = 1
	EscalateWorkflow  EscalationLevel = 2
	EscalateCore      EscalationLevel = 3
	EscalateOwner     EscalationLevel = 4
	EscalateEmergency EscalationLevel = 5
)

// AttentionItem represents a prioritized concern.
type AttentionItem struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Level       AttentionLevel   `json:"level"`
	Outcome     AttentionOutcome `json:"outcome"`
	Status      AttentionStatus  `json:"status"`
	BusinessID  string           `json:"business_id"`
	DivisionID  string           `json:"division_id,omitempty"`
	ObjectiveID string           `json:"objective_id,omitempty"`
	TaskID      string           `json:"task_id,omitempty"`
	AgentID     string           `json:"agent_id,omitempty"`
	Source      string           `json:"source"` // "agent", "workflow", "system", "owner", "security"
	// Priority dimensions
	Urgency    int     `json:"urgency"`    // 0-10
	Importance int     `json:"importance"` // 0-10
	Risk       int     `json:"risk"`       // 0-10
	Confidence float64 `json:"confidence"` // 0.0 - 1.0
	// Computed score
	Score float64 `json:"score"`
	// Suppression guardrails
	IsSecuritySignal  bool `json:"is_security_signal"`
	IsPolicyViolation bool `json:"is_policy_violation"`
	IsOwnerMessage    bool `json:"is_owner_message"`
	SeverityIncreased bool `json:"severity_increased"`
	ScopeChanged      bool `json:"scope_changed"`
	NewEvidence       bool `json:"new_evidence"`
	// Timestamps
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// AttentionBudget defines limits on owner interruptions.
type AttentionBudget struct {
	MaxOwnerInterruptionsPerHour int `json:"max_owner_interruptions_per_hour"`
	MaxUrgentNotificationsPerDay int `json:"max_urgent_notifications_per_day"`
	MaxConcurrentHighAttention   int `json:"max_concurrent_high_attention"`
}

// QuietHours defines when owner notifications are suppressed.
type QuietHours struct {
	Enabled  bool           `json:"enabled"`
	Start    time.Time      `json:"start"`
	End      time.Time      `json:"end"`
	MinLevel AttentionLevel `json:"min_level"` // minimum level to break quiet hours
}

// SuppressionGuard defines what can never be suppressed.
type SuppressionGuard struct {
	NeverSuppressSecuritySignals  bool `json:"never_suppress_security_signals"`
	NeverSuppressPolicyViolations bool `json:"never_suppress_policy_violations"`
	NeverSuppressOwnerMessages    bool `json:"never_suppress_owner_messages"`
	NeverSuppressSeverityIncrease bool `json:"never_suppress_severity_increase"`
	NeverSuppressScopeChange      bool `json:"never_suppress_scope_change"`
	NeverSuppressNewEvidence      bool `json:"never_suppress_new_evidence"`
}

// FullAttentionEngine manages attention items with full priority, suppression, and budget.
type FullAttentionEngine struct {
	items  map[string]*AttentionItem
	budget AttentionBudget
	quiet  QuietHours
	guard  SuppressionGuard
	// Cooldown tracking
	cooldowns map[string]time.Time // key → last notification time
	// Budget tracking
	interruptionsThisHour int
	urgentToday           int
	hourStart             time.Time
	dayStart              time.Time
	mu                    sync.RWMutex
	now                   func() time.Time
}

// NewFullAttentionEngine creates a new full attention engine.
func NewFullAttentionEngine(budget AttentionBudget, quiet QuietHours, guard SuppressionGuard) *FullAttentionEngine {
	now := time.Now()
	return &FullAttentionEngine{
		items:     make(map[string]*AttentionItem),
		budget:    budget,
		quiet:     quiet,
		guard:     guard,
		cooldowns: make(map[string]time.Time),
		hourStart: now,
		dayStart:  now,
		now:       time.Now,
	}
}

// NewFullAttentionEngineWithClock creates a new full attention engine with an injectable clock.
func NewFullAttentionEngineWithClock(budget AttentionBudget, quiet QuietHours, guard SuppressionGuard, now func() time.Time) *FullAttentionEngine {
	current := now()
	return &FullAttentionEngine{
		items:     make(map[string]*AttentionItem),
		budget:    budget,
		quiet:     quiet,
		guard:     guard,
		cooldowns: make(map[string]time.Time),
		hourStart: current,
		dayStart:  current,
		now:       now,
	}
}

// SubmitItem adds a new attention item and computes its priority score.
func (fae *FullAttentionEngine) SubmitItem(
	title, description, businessID, source string,
	urgency, importance, risk int,
	confidence float64,
	guards ...bool,
) (*AttentionItem, error) {
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	now := fae.now()
	item := &AttentionItem{
		ID:          fmt.Sprintf("att-%d", now.UnixNano()),
		Title:       title,
		Description: description,
		BusinessID:  businessID,
		Source:      source,
		Status:      StatusDetected,
		Urgency:     urgency,
		Importance:  importance,
		Risk:        risk,
		Confidence:  confidence,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Apply guardrails from arguments
	if len(guards) >= 1 {
		item.IsSecuritySignal = guards[0]
	}
	if len(guards) >= 2 {
		item.IsPolicyViolation = guards[1]
	}
	if len(guards) >= 3 {
		item.IsOwnerMessage = guards[2]
	}

	// Compute score and level
	item.Score = fae.computeScore(item)
	item.Level = fae.scoreToLevel(item.Score)

	// Determine outcome
	item.Outcome = fae.determineOutcome(item)
	item.Status = StatusRanked

	fae.mu.Lock()
	defer fae.mu.Unlock()
	fae.items[item.ID] = item
	return item, nil
}

// Acknowledge marks an item as acknowledged by owner/agent.
func (fae *FullAttentionEngine) Acknowledge(itemID string) error {
	fae.mu.Lock()
	defer fae.mu.Unlock()

	item, ok := fae.items[itemID]
	if !ok {
		return fmt.Errorf("attention item %s not found", itemID)
	}

	item.Status = StatusAcknowledged
	item.UpdatedAt = fae.now()
	return nil
}

// Resolve marks an item as resolved.
func (fae *FullAttentionEngine) Resolve(itemID string) error {
	fae.mu.Lock()
	defer fae.mu.Unlock()

	item, ok := fae.items[itemID]
	if !ok {
		return fmt.Errorf("attention item %s not found", itemID)
	}

	now := fae.now()
	item.Status = StatusResolved
	item.ResolvedAt = &now
	item.UpdatedAt = now
	return nil
}

// ShouldSuppress checks if an item should be suppressed.
// Returns true if suppression is allowed, false if guardrails prevent it.
func (fae *FullAttentionEngine) ShouldSuppress(item *AttentionItem) bool {
	fae.mu.RLock()
	defer fae.mu.RUnlock()

	// Guardrails: never suppress these
	if fae.guard.NeverSuppressSecuritySignals && item.IsSecuritySignal {
		return false
	}
	if fae.guard.NeverSuppressPolicyViolations && item.IsPolicyViolation {
		return false
	}
	if fae.guard.NeverSuppressOwnerMessages && item.IsOwnerMessage {
		return false
	}
	if fae.guard.NeverSuppressSeverityIncrease && item.SeverityIncreased {
		return false
	}
	if fae.guard.NeverSuppressScopeChange && item.ScopeChanged {
		return false
	}
	if fae.guard.NeverSuppressNewEvidence && item.NewEvidence {
		return false
	}

	// Check quiet hours
	if fae.quiet.Enabled && item.Outcome == OutcomeEscalateOwner {
		if item.Level < fae.quiet.MinLevel {
			return true // suppress below min level during quiet hours
		}
	}

	// Check cooldown
	if lastNotification, ok := fae.cooldowns[item.Source]; ok {
		if fae.now().Sub(lastNotification) < 5*time.Minute {
			return true // cooldown active
		}
	}

	return false
}

// CheckBudget checks if we've exceeded interruption budget.
func (fae *FullAttentionEngine) CheckBudget() bool {
	fae.mu.Lock()
	defer fae.mu.Unlock()

	now := fae.now()

	// Reset hourly counter
	if now.Sub(fae.hourStart) > time.Hour {
		fae.interruptionsThisHour = 0
		fae.hourStart = now
	}

	// Reset daily counter
	if now.Sub(fae.dayStart) > 24*time.Hour {
		fae.urgentToday = 0
		fae.dayStart = now
	}

	if fae.budget.MaxOwnerInterruptionsPerHour > 0 && fae.interruptionsThisHour >= fae.budget.MaxOwnerInterruptionsPerHour {
		return false // budget exceeded
	}
	if fae.budget.MaxUrgentNotificationsPerDay > 0 && fae.urgentToday >= fae.budget.MaxUrgentNotificationsPerDay {
		return false // budget exceeded
	}

	return true // budget OK
}

// RecordInterruption records that an interruption was sent.
func (fae *FullAttentionEngine) RecordInterruption() {
	fae.mu.Lock()
	defer fae.mu.Unlock()
	fae.interruptionsThisHour++
	fae.urgentToday++
}

// GetItem returns an attention item by ID.
func (fae *FullAttentionEngine) GetItem(itemID string) (*AttentionItem, bool) {
	fae.mu.RLock()
	defer fae.mu.RUnlock()
	item, ok := fae.items[itemID]
	return item, ok
}

// PendingItems returns all unresolved items.
func (fae *FullAttentionEngine) PendingItems() []*AttentionItem {
	fae.mu.RLock()
	defer fae.mu.RUnlock()

	var pending []*AttentionItem
	for _, item := range fae.items {
		if item.Status != StatusResolved && item.Status != StatusExpired {
			pending = append(pending, item)
		}
	}
	return pending
}

// ItemCount returns the total number of attention items.
func (fae *FullAttentionEngine) ItemCount() int {
	fae.mu.RLock()
	defer fae.mu.RUnlock()
	return len(fae.items)
}

func (fae *FullAttentionEngine) computeScore(item *AttentionItem) float64 {
	score := float64(item.Urgency + item.Importance + item.Risk)
	// Confidence penalty (low confidence = lower score)
	score -= (1.0 - item.Confidence) * 3
	return score
}

func (fae *FullAttentionEngine) scoreToLevel(score float64) AttentionLevel {
	switch {
	case score >= 25:
		return LevelEmergency
	case score >= 20:
		return LevelCritical
	case score >= 15:
		return LevelHigh
	case score >= 10:
		return LevelImportant
	case score >= 5:
		return LevelNormal
	case score >= 2:
		return LevelBackground
	default:
		return LevelIgnore
	}
}

func (fae *FullAttentionEngine) determineOutcome(item *AttentionItem) AttentionOutcome {
	// Hard rules: security signals and policy violations always escalate
	if item.IsSecuritySignal || item.IsPolicyViolation {
		return OutcomeEmergency
	}
	if item.IsOwnerMessage {
		return OutcomeEscalateOwner
	}

	// Score-based outcome
	switch item.Level {
	case LevelEmergency:
		return OutcomeEmergency
	case LevelCritical:
		return OutcomeEscalateOwner
	case LevelHigh:
		return OutcomeEscalateAgent
	case LevelImportant:
		return OutcomeQueue
	case LevelNormal:
		return OutcomeMonitor
	case LevelBackground:
		return OutcomeRecord
	default:
		return OutcomeIgnore
	}
}
