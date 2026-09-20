// Package attention implements the NEXUS basic Attention & Priority Intelligence (C10).
//
// M7 implements basic review routing: attention items, priority scoring,
// and escalation. Full attention (aggregation, dedup, suppression guardrails)
// is M9 scope.
//
// Attention manages awareness, not execution authority. Attention can
// recommend routing to specialist or Executive for review without
// interrupting the owner. Attention does NOT run tasks, create/redefine
// objectives, or replace Executive/Decision Engine.
//
// Flow:
//
//	Event → Relevance → Priority → Attention Decision → Ignore/Record/Monitor/Escalate
package attention

import (
	"fmt"
	"sync"
	"time"
)

// AttentionLevel classifies the urgency of an attention item.
type AttentionLevel string

const (
	LevelIgnore   AttentionLevel = "ignore"
	LevelRecord   AttentionLevel = "record"
	LevelMonitor  AttentionLevel = "monitor"
	LevelReview   AttentionLevel = "review"
	LevelEscalate AttentionLevel = "escalate"
)

// AttentionStatus tracks the lifecycle of an attention item.
type AttentionStatus string

const (
	StatusNew       AttentionStatus = "new"
	StatusOpen      AttentionStatus = "open"
	StatusReviewing AttentionStatus = "reviewing"
	StatusEscalated AttentionStatus = "escalated"
	StatusResolved  AttentionStatus = "resolved"
	StatusIgnored   AttentionStatus = "ignored"
)

// AttentionItem represents a concern that needs prioritization.
type AttentionItem struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Level       AttentionLevel  `json:"level"`
	Status      AttentionStatus `json:"status"`
	BusinessID  string          `json:"business_id"`
	ObjectiveID string          `json:"objective_id,omitempty"`
	TaskID      string          `json:"task_id,omitempty"`
	AgentID     string          `json:"agent_id,omitempty"`
	Priority    int             `json:"priority"` // 0-10, higher = more urgent
	Source      string          `json:"source"`   // "agent", "workflow", "system", "owner"
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	ResolvedAt  *time.Time      `json:"resolved_at,omitempty"`
}

// AttentionEngine manages attention items and review routing.
type AttentionEngine struct {
	items map[string]*AttentionItem
	mu    sync.RWMutex
	now   func() time.Time
}

// NewAttentionEngine creates a new attention engine.
func NewAttentionEngine() *AttentionEngine {
	return &AttentionEngine{
		items: make(map[string]*AttentionItem),
		now:   time.Now,
	}
}

// NewAttentionEngineWithClock creates a new attention engine with an injectable clock.
func NewAttentionEngineWithClock(now func() time.Time) *AttentionEngine {
	return &AttentionEngine{
		items: make(map[string]*AttentionItem),
		now:   now,
	}
}

// SubmitItem adds a new attention item.
func (ae *AttentionEngine) SubmitItem(
	title, description, businessID, source string,
	priority int,
) (*AttentionItem, error) {
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if priority < 0 || priority > 10 {
		return nil, fmt.Errorf("priority must be 0-10")
	}

	now := ae.now()
	item := &AttentionItem{
		ID:          fmt.Sprintf("att-%d", now.UnixNano()),
		Title:       title,
		Description: description,
		BusinessID:  businessID,
		Source:      source,
		Priority:    priority,
		Status:      StatusNew,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Auto-classify level based on priority
	item.Level = classifyLevel(priority)

	ae.mu.Lock()
	defer ae.mu.Unlock()
	ae.items[item.ID] = item
	return item, nil
}

// Escalate escalates an attention item to higher review.
func (ae *AttentionEngine) Escalate(itemID string) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	item, ok := ae.items[itemID]
	if !ok {
		return fmt.Errorf("attention item %s not found", itemID)
	}

	item.Level = LevelEscalate
	item.Status = StatusEscalated
	item.UpdatedAt = ae.now()
	return nil
}

// Resolve marks an attention item as resolved.
func (ae *AttentionEngine) Resolve(itemID string) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	item, ok := ae.items[itemID]
	if !ok {
		return fmt.Errorf("attention item %s not found", itemID)
	}

	now := ae.now()
	item.Status = StatusResolved
	item.ResolvedAt = &now
	item.UpdatedAt = now
	return nil
}

// Ignore marks an attention item as ignored.
func (ae *AttentionEngine) Ignore(itemID string) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	item, ok := ae.items[itemID]
	if !ok {
		return fmt.Errorf("attention item %s not found", itemID)
	}

	item.Status = StatusIgnored
	item.UpdatedAt = ae.now()
	return nil
}

// GetItem returns an attention item by ID.
func (ae *AttentionEngine) GetItem(itemID string) (*AttentionItem, bool) {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	item, ok := ae.items[itemID]
	return item, ok
}

// PendingItems returns all unresolved items sorted by priority.
func (ae *AttentionEngine) PendingItems() []*AttentionItem {
	ae.mu.RLock()
	defer ae.mu.RUnlock()

	var pending []*AttentionItem
	for _, item := range ae.items {
		if item.Status != StatusResolved && item.Status != StatusIgnored {
			pending = append(pending, item)
		}
	}
	return pending
}

// ItemCount returns the total number of attention items.
func (ae *AttentionEngine) ItemCount() int {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	return len(ae.items)
}

func classifyLevel(priority int) AttentionLevel {
	switch {
	case priority >= 9:
		return LevelEscalate
	case priority >= 7:
		return LevelReview
	case priority >= 4:
		return LevelMonitor
	case priority >= 1:
		return LevelRecord
	default:
		return LevelIgnore
	}
}
