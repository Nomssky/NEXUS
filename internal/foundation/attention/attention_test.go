package attention

import (
	"testing"
	"time"
)

// TEST-M7-015: Submit attention item
func TestAttentionSubmit(t *testing.T) {
	ae := NewAttentionEngine()
	item, err := ae.SubmitItem("High priority issue", "Needs review", "biz-1", "agent", 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Level != LevelEscalate {
		t.Errorf("expected escalate level for priority 9, got %v", item.Level)
	}
	if item.Status != StatusNew {
		t.Errorf("expected new status, got %v", item.Status)
	}
}

// TEST-M7-016: Submit rejects empty title
func TestAttentionRejectsEmptyTitle(t *testing.T) {
	ae := NewAttentionEngine()
	_, err := ae.SubmitItem("", "desc", "biz-1", "agent", 5)
	if err == nil {
		t.Error("expected error for empty title")
	}
}

// TEST-M7-017: Submit rejects invalid priority
func TestAttentionRejectsInvalidPriority(t *testing.T) {
	ae := NewAttentionEngine()
	_, err := ae.SubmitItem("Title", "desc", "biz-1", "agent", 15)
	if err == nil {
		t.Error("expected error for priority > 10")
	}
}

// TEST-M7-018: Priority classification
func TestAttentionPriorityClassification(t *testing.T) {
	tests := []struct {
		priority int
		level    AttentionLevel
	}{
		{0, LevelIgnore},
		{1, LevelRecord},
		{4, LevelMonitor},
		{7, LevelReview},
		{9, LevelEscalate},
	}

	for _, tt := range tests {
		ae := NewAttentionEngine()
		item, _ := ae.SubmitItem("Test", "desc", "biz-1", "agent", tt.priority)
		if item.Level != tt.level {
			t.Errorf("priority %d: expected %v, got %v", tt.priority, tt.level, item.Level)
		}
	}
}

// TEST-M7-019: Escalate item
func TestAttentionEscalate(t *testing.T) {
	ae := NewAttentionEngine()
	item, _ := ae.SubmitItem("Issue", "desc", "biz-1", "agent", 5)

	err := ae.Escalate(item.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Level != LevelEscalate {
		t.Errorf("expected escalate, got %v", item.Level)
	}
	if item.Status != StatusEscalated {
		t.Errorf("expected escalated status, got %v", item.Status)
	}
}

// TEST-M7-020: Resolve item
func TestAttentionResolve(t *testing.T) {
	ae := NewAttentionEngine()
	item, _ := ae.SubmitItem("Issue", "desc", "biz-1", "agent", 5)

	err := ae.Resolve(item.ID)
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

// TEST-M7-021: Ignore item
func TestAttentionIgnore(t *testing.T) {
	ae := NewAttentionEngine()
	item, _ := ae.SubmitItem("Noise", "desc", "biz-1", "system", 0)

	err := ae.Ignore(item.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Status != StatusIgnored {
		t.Errorf("expected ignored, got %v", item.Status)
	}
}

// TEST-M7-022: Pending items excludes resolved/ignored
func TestAttentionPendingExcludesResolved(t *testing.T) {
	ae := NewAttentionEngine()
	item1, _ := ae.SubmitItem("Open", "desc", "biz-1", "agent", 5)
	item2, _ := ae.SubmitItem("Resolved", "desc", "biz-1", "agent", 5)
	item3, _ := ae.SubmitItem("Ignored", "desc", "biz-1", "agent", 0)

	ae.Resolve(item2.ID)
	ae.Ignore(item3.ID)

	pending := ae.PendingItems()
	if len(pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].ID != item1.ID {
		t.Errorf("expected item1 pending, got %v", pending[0].ID)
	}
}

// TEST-M7-023: Business isolation in attention
func TestAttentionBusinessIsolation(t *testing.T) {
	ae := NewAttentionEngine()
	item1, _ := ae.SubmitItem("Issue 1", "desc", "biz-1", "agent", 5)
	item2, _ := ae.SubmitItem("Issue 2", "desc", "biz-2", "agent", 5)

	if item1.BusinessID == item2.BusinessID {
		t.Error("expected different business IDs")
	}
}

// TEST-M7-024: Clock injection
func TestAttentionClockInjection(t *testing.T) {
	now := time.Now()
	ae := NewAttentionEngineWithClock(func() time.Time { return now })
	item, _ := ae.SubmitItem("Issue", "desc", "biz-1", "agent", 5)

	if !item.CreatedAt.Equal(now) {
		t.Errorf("expected clock-injected time")
	}
}
