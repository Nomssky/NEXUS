package memory

import (
	"fmt"
	"time"
)

// ContextAssembler builds context from memory for agents.
// It selects relevant memories and assembles them into a coherent context.
type ContextAssembler struct {
	store *MemoryStore
	now   func() time.Time
}

// NewContextAssembler creates a new context assembler.
func NewContextAssembler(store *MemoryStore) *ContextAssembler {
	return &ContextAssembler{
		store: store,
		now:   time.Now,
	}
}

// NewContextAssemblerWithClock creates a new context assembler with an injectable clock.
func NewContextAssemblerWithClock(store *MemoryStore, now func() time.Time) *ContextAssembler {
	return &ContextAssembler{
		store: store,
		now:   now,
	}
}

// ContextRequest describes what context to assemble.
type ContextRequest struct {
	BusinessID  string       `json:"business_id"`
	DivisionID  string       `json:"division_id,omitempty"`
	ObjectiveID string       `json:"objective_id,omitempty"`
	TaskID      string       `json:"task_id,omitempty"`
	MaxTokens   int          `json:"max_tokens"` // approximate token budget
	Priority    []MemoryType `json:"priority"`   // preferred memory types in order
}

// AssembledContext is the result of context assembly.
type AssembledContext struct {
	BusinessID  string         `json:"business_id"`
	ObjectiveID string         `json:"objective_id,omitempty"`
	Memories    []*MemoryEntry `json:"memories"`
	TotalTokens int            `json:"total_tokens"` // approximate
	AssembledAt time.Time      `json:"assembled_at"`
}

// Assemble builds context from memory for a given request.
func (ca *ContextAssembler) Assemble(req *ContextRequest) (*AssembledContext, error) {
	if req.BusinessID == "" {
		return nil, fmt.Errorf("business ID is required")
	}

	// Retrieve relevant memories
	query := &MemoryQuery{
		BusinessID:  req.BusinessID,
		DivisionID:  req.DivisionID,
		ObjectiveID: req.ObjectiveID,
		Types:       req.Priority,
		MaxResults:  50, // reasonable limit
	}

	memories := ca.store.Retrieve(query)

	// Estimate tokens (rough: 4 chars per token)
	totalTokens := 0
	var selected []*MemoryEntry
	for _, m := range memories {
		estimatedTokens := len(m.Content) / 4
		if req.MaxTokens > 0 && totalTokens+estimatedTokens > req.MaxTokens {
			break
		}
		selected = append(selected, m)
		totalTokens += estimatedTokens
	}

	return &AssembledContext{
		BusinessID:  req.BusinessID,
		ObjectiveID: req.ObjectiveID,
		Memories:    selected,
		TotalTokens: totalTokens,
		AssembledAt: ca.now(),
	}, nil
}
