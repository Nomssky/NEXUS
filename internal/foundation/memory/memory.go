// Package memory implements the NEXUS Memory & Context Intelligence (C09).
//
// C09 provides scoped memory admission/retrieval, context assembly, and
// knowledge ingestion with provenance. Memory is NOT a chat history database.
// NEXUS manages memory based on relevance, scope, time, objective, governance,
// and future action needs.
//
// Key invariants:
//   - Scoped retrieval: no "everything NEXUS knows" default
//   - Observation ≠ permanent memory
//   - Memory/knowledge can never authorize
//   - Knowledge is untrusted until validated
//   - Provenance always tracked
//
// Memory types:
//   - Working: current task context (short-lived)
//   - Episodic: event/experience records (medium-lived)
//   - Semantic: facts and relationships (long-lived)
//   - Procedural: how-to knowledge (long-lived)
package memory

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// MemoryType classifies the type of memory.
type MemoryType string

const (
	MemoryTypeWorking    MemoryType = "working"
	MemoryTypeEpisodic   MemoryType = "episodic"
	MemoryTypeSemantic   MemoryType = "semantic"
	MemoryTypeProcedural MemoryType = "procedural"
)

// MemoryStatus tracks the lifecycle of a memory.
type MemoryStatus string

const (
	StatusAdmitted  MemoryStatus = "admitted"
	StatusActive    MemoryStatus = "active"
	StatusArchived  MemoryStatus = "archived"
	StatusForgotten MemoryStatus = "forgotten"
)

// Provenance tracks where a memory came from.
type Provenance struct {
	Source     string     `json:"source"`     // "agent", "workflow", "tool", "owner", "ingestion"
	SourceID   string     `json:"source_id"`  // e.g., agent_id, workflow_id
	Confidence float64    `json:"confidence"` // 0.0 - 1.0
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
}

// MemoryEntry represents a single memory record.
type MemoryEntry struct {
	ID           string       `json:"id"`
	Type         MemoryType   `json:"type"`
	Status       MemoryStatus `json:"status"`
	BusinessID   string       `json:"business_id"`
	DivisionID   string       `json:"division_id,omitempty"`
	ObjectiveID  string       `json:"objective_id,omitempty"`
	AgentID      string       `json:"agent_id,omitempty"`
	WorkflowID   string       `json:"workflow_id,omitempty"`
	Content      string       `json:"content"`
	Summary      string       `json:"summary,omitempty"`
	Tags         []string     `json:"tags,omitempty"`
	Entities     []string     `json:"entities,omitempty"`
	Provenance   Provenance   `json:"provenance"`
	Relevance    float64      `json:"relevance"` // computed relevance score
	AccessCount  int          `json:"access_count"`
	LastAccessed *time.Time   `json:"last_accessed,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	ExpiresAt    *time.Time   `json:"expires_at,omitempty"`
}

// MemoryQuery represents a query for memory retrieval.
type MemoryQuery struct {
	BusinessID    string       `json:"business_id"`
	DivisionID    string       `json:"division_id,omitempty"`
	ObjectiveID   string       `json:"objective_id,omitempty"`
	Types         []MemoryType `json:"types,omitempty"`
	Tags          []string     `json:"tags,omitempty"`
	Entities      []string     `json:"entities,omitempty"`
	Keywords      string       `json:"keywords,omitempty"`
	MaxResults    int          `json:"max_results"`
	MinConfidence float64      `json:"min_confidence"`
}

// MemoryStore manages memory entries with scoped retrieval.
type MemoryStore struct {
	entries map[string]*MemoryEntry
	mu      sync.RWMutex
	now     func() time.Time
}

// NewMemoryStore creates a new memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries: make(map[string]*MemoryEntry),
		now:     time.Now,
	}
}

// NewMemoryStoreWithClock creates a new memory store with an injectable clock.
func NewMemoryStoreWithClock(now func() time.Time) *MemoryStore {
	return &MemoryStore{
		entries: make(map[string]*MemoryEntry),
		now:     now,
	}
}

// Admit adds a memory entry to the store.
// Admission is gated by scope and provenance.
func (ms *MemoryStore) Admit(entry *MemoryEntry) error {
	if entry.Content == "" {
		return fmt.Errorf("content is required")
	}
	if entry.BusinessID == "" {
		return fmt.Errorf("business ID is required for scoped retrieval")
	}

	now := ms.now()
	entry.ID = fmt.Sprintf("mem-%d", now.UnixNano())
	entry.Status = StatusAdmitted
	entry.CreatedAt = now
	entry.UpdatedAt = now

	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.entries[entry.ID] = entry
	return nil
}

// Retrieve queries memory entries with scope enforcement.
// Scoped retrieval: only returns entries matching the query's business/division scope.
func (ms *MemoryStore) Retrieve(query *MemoryQuery) []*MemoryEntry {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	var results []*MemoryEntry
	for _, entry := range ms.entries {
		if entry.Status == StatusForgotten || entry.Status == StatusArchived {
			continue
		}
		// Scope enforcement: business must match
		if entry.BusinessID != query.BusinessID {
			continue
		}
		// Division scope (if specified)
		if query.DivisionID != "" && entry.DivisionID != "" && entry.DivisionID != query.DivisionID {
			continue
		}
		// Type filter
		if len(query.Types) > 0 {
			found := false
			for _, t := range query.Types {
				if entry.Type == t {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		// Confidence filter
		if entry.Provenance.Confidence < query.MinConfidence {
			continue
		}
		// Tag filter
		if len(query.Tags) > 0 && !hasAnyTag(entry.Tags, query.Tags) {
			continue
		}

		// Keyword filter: if keywords are specified, entry content must contain at least one keyword (case-insensitive).
		if query.Keywords != "" {
			if !contentMatchesKeywords(entry.Content, query.Keywords) {
				continue
			}
		}

		// Update access tracking
		now := ms.now()
		entry.AccessCount++
		entry.LastAccessed = &now

		results = append(results, entry)
		if query.MaxResults > 0 && len(results) >= query.MaxResults {
			break
		}
	}
	return results
}

// Archive marks a memory as archived (no longer actively retrieved).
func (ms *MemoryStore) Archive(entryID string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	entry, ok := ms.entries[entryID]
	if !ok {
		return fmt.Errorf("memory %s not found", entryID)
	}

	entry.Status = StatusArchived
	entry.UpdatedAt = ms.now()
	return nil
}

// Forget marks a memory as forgotten (permanently excluded from retrieval).
func (ms *MemoryStore) Forget(entryID string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	entry, ok := ms.entries[entryID]
	if !ok {
		return fmt.Errorf("memory %s not found", entryID)
	}

	entry.Status = StatusForgotten
	entry.UpdatedAt = ms.now()
	return nil
}

// Get returns a memory entry by ID.
func (ms *MemoryStore) Get(entryID string) (*MemoryEntry, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	entry, ok := ms.entries[entryID]
	return entry, ok
}

// Count returns the total number of active memory entries.
func (ms *MemoryStore) Count() int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	count := 0
	for _, entry := range ms.entries {
		if entry.Status != StatusForgotten {
			count++
		}
	}
	return count
}

func hasAnyTag(entryTags, queryTags []string) bool {
	tagSet := make(map[string]bool, len(entryTags))
	for _, t := range entryTags {
		tagSet[t] = true
	}
	for _, t := range queryTags {
		if tagSet[t] {
			return true
		}
	}
	return false
}

// contentMatchesKeywords returns true if content contains at least one of the
// space-separated keywords (case-insensitive substring match).
func contentMatchesKeywords(content, keywords string) bool {
	contentLower := strings.ToLower(content)
	for _, kw := range strings.Fields(keywords) {
		if strings.Contains(contentLower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}
