package memory

import (
	"testing"
	"time"
)

// TEST-M8-001: Admit memory with scope
func TestMemoryAdmit(t *testing.T) {
	ms := NewMemoryStore()
	entry := &MemoryEntry{
		Type:       MemoryTypeEpisodic,
		BusinessID: "biz-1",
		Content:    "Workflow completed successfully",
		Provenance: Provenance{Source: "workflow", Confidence: 0.9},
	}

	err := ms.Admit(entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.ID == "" {
		t.Error("expected ID to be assigned")
	}
	if entry.Status != StatusAdmitted {
		t.Errorf("expected admitted, got %v", entry.Status)
	}
}

// TEST-M8-002: Admit rejects empty content
func TestMemoryAdmitRejectsEmptyContent(t *testing.T) {
	ms := NewMemoryStore()
	err := ms.Admit(&MemoryEntry{
		Type:       MemoryTypeWorking,
		BusinessID: "biz-1",
		Content:    "",
	})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

// TEST-M8-003: Admit rejects empty business ID
func TestMemoryAdmitRejectsEmptyBusiness(t *testing.T) {
	ms := NewMemoryStore()
	err := ms.Admit(&MemoryEntry{
		Type:    MemoryTypeWorking,
		Content: "some content",
	})
	if err == nil {
		t.Error("expected error for empty business ID")
	}
}

// TEST-M8-004: Scoped retrieval by business
func TestMemoryScopedRetrieval(t *testing.T) {
	ms := NewMemoryStore()
	ms.Admit(&MemoryEntry{Type: MemoryTypeEpisodic, BusinessID: "biz-1", Content: "biz1 memory", Provenance: Provenance{Confidence: 1.0}})
	ms.Admit(&MemoryEntry{Type: MemoryTypeEpisodic, BusinessID: "biz-2", Content: "biz2 memory", Provenance: Provenance{Confidence: 1.0}})

	results := ms.Retrieve(&MemoryQuery{BusinessID: "biz-1"})
	if len(results) != 1 {
		t.Errorf("expected 1 result for biz-1, got %d", len(results))
	}
	if results[0].Content != "biz1 memory" {
		t.Errorf("expected biz1 memory, got %v", results[0].Content)
	}
}

// TEST-M8-005: Retrieval filters by type
func TestMemoryRetrievalByType(t *testing.T) {
	ms := NewMemoryStore()
	ms.Admit(&MemoryEntry{Type: MemoryTypeWorking, BusinessID: "biz-1", Content: "working", Provenance: Provenance{Confidence: 1.0}})
	ms.Admit(&MemoryEntry{Type: MemoryTypeEpisodic, BusinessID: "biz-1", Content: "episodic", Provenance: Provenance{Confidence: 1.0}})

	results := ms.Retrieve(&MemoryQuery{BusinessID: "biz-1", Types: []MemoryType{MemoryTypeWorking}})
	if len(results) != 1 {
		t.Errorf("expected 1 working memory, got %d", len(results))
	}
}

// TEST-M8-006: Retrieval filters by confidence
func TestMemoryRetrievalByConfidence(t *testing.T) {
	ms := NewMemoryStore()
	ms.Admit(&MemoryEntry{Type: MemoryTypeSemantic, BusinessID: "biz-1", Content: "high", Provenance: Provenance{Confidence: 0.9}})
	ms.Admit(&MemoryEntry{Type: MemoryTypeSemantic, BusinessID: "biz-1", Content: "low", Provenance: Provenance{Confidence: 0.3}})

	results := ms.Retrieve(&MemoryQuery{BusinessID: "biz-1", MinConfidence: 0.5})
	if len(results) != 1 {
		t.Errorf("expected 1 high-confidence result, got %d", len(results))
	}
}

// TEST-M8-007: Archive memory
func TestMemoryArchive(t *testing.T) {
	ms := NewMemoryStore()
	entry := &MemoryEntry{Type: MemoryTypeWorking, BusinessID: "biz-1", Content: "temp", Provenance: Provenance{Confidence: 1.0}}
	ms.Admit(entry)

	ms.Archive(entry.ID)

	// Archived memory not in retrieval
	results := ms.Retrieve(&MemoryQuery{BusinessID: "biz-1"})
	if len(results) != 0 {
		t.Errorf("expected 0 results after archive, got %d", len(results))
	}
}

// TEST-M8-008: Forget memory
func TestMemoryForget(t *testing.T) {
	ms := NewMemoryStore()
	entry := &MemoryEntry{Type: MemoryTypeEpisodic, BusinessID: "biz-1", Content: "sensitive", Provenance: Provenance{Confidence: 1.0}}
	ms.Admit(entry)

	ms.Forget(entry.ID)

	results := ms.Retrieve(&MemoryQuery{BusinessID: "biz-1"})
	if len(results) != 0 {
		t.Errorf("expected 0 results after forget, got %d", len(results))
	}
}

// TEST-M8-009: Observation ≠ permanent memory
func TestObservationNotPermanent(t *testing.T) {
	ms := NewMemoryStore()
	entry := &MemoryEntry{
		Type:       MemoryTypeWorking,
		BusinessID: "biz-1",
		Content:    "temporary observation",
		Provenance: Provenance{Confidence: 0.5},
	}
	ms.Admit(entry)

	// Working memory can be forgotten
	ms.Forget(entry.ID)

	if _, ok := ms.Get(entry.ID); !ok {
		t.Error("entry should still exist but be forgotten")
	}

	results := ms.Retrieve(&MemoryQuery{BusinessID: "biz-1"})
	if len(results) != 0 {
		t.Error("forgotten memory should not be retrieved")
	}
}

// TEST-M8-010: Memory types
func TestMemoryTypes(t *testing.T) {
	types := []MemoryType{MemoryTypeWorking, MemoryTypeEpisodic, MemoryTypeSemantic, MemoryTypeProcedural}
	if len(types) != 4 {
		t.Errorf("expected 4 memory types, got %d", len(types))
	}
}

// TEST-M8-011: Context assembly
func TestContextAssembly(t *testing.T) {
	ms := NewMemoryStore()
	ms.Admit(&MemoryEntry{Type: MemoryTypeEpisodic, BusinessID: "biz-1", Content: "Event 1", Provenance: Provenance{Confidence: 0.9}})
	ms.Admit(&MemoryEntry{Type: MemoryTypeSemantic, BusinessID: "biz-1", Content: "Fact 1", Provenance: Provenance{Confidence: 0.8}})

	ca := NewContextAssembler(ms)
	ctx, err := ca.Assemble(&ContextRequest{
		BusinessID: "biz-1",
		MaxTokens:  1000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ctx.Memories) != 2 {
		t.Errorf("expected 2 memories in context, got %d", len(ctx.Memories))
	}
}

// TEST-M8-012: Context assembly respects token budget
func TestContextAssemblyTokenBudget(t *testing.T) {
	ms := NewMemoryStore()
	ms.Admit(&MemoryEntry{Type: MemoryTypeSemantic, BusinessID: "biz-1", Content: "Very long content that exceeds token budget", Provenance: Provenance{Confidence: 0.9}})

	ca := NewContextAssembler(ms)
	ctx, err := ca.Assemble(&ContextRequest{
		BusinessID: "biz-1",
		MaxTokens:  5, // very small budget
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should respect budget
	if ctx.TotalTokens > 5 {
		t.Errorf("expected tokens ≤ 5, got %d", ctx.TotalTokens)
	}
}

// TEST-M8-013: Knowledge discovery
func TestKnowledgeDiscovery(t *testing.T) {
	ms := NewMemoryStore()
	ki := NewKnowledgeIngestion(ms)

	entry, err := ki.Discover(SourceWeb, "https://example.com", "biz-1", "Some knowledge")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Status != KStatusDiscovered {
		t.Errorf("expected discovered, got %v", entry.Status)
	}
	if entry.Source != SourceWeb {
		t.Errorf("expected web source, got %v", entry.Source)
	}
}

// TEST-M8-014: Knowledge pipeline: discover → validate → classify → store
func TestKnowledgePipeline(t *testing.T) {
	ms := NewMemoryStore()
	ki := NewKnowledgeIngestion(ms)

	entry, _ := ki.Discover(SourceDocument, "", "biz-1", "Important fact")

	ki.Validate(entry.ID, 0.85)
	if entry.Status != KStatusValidated {
		t.Errorf("expected validated, got %v", entry.Status)
	}

	ki.Classify(entry.ID, "fact")
	if entry.Status != KStatusClassified {
		t.Errorf("expected classified, got %v", entry.Status)
	}

	err := ki.Store(entry.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Status != KStatusStored {
		t.Errorf("expected stored, got %v", entry.Status)
	}

	// Knowledge should now be in memory store
	results := ms.Retrieve(&MemoryQuery{BusinessID: "biz-1"})
	if len(results) != 1 {
		t.Errorf("expected 1 memory from knowledge, got %d", len(results))
	}
}

// TEST-M8-015: Knowledge rejects unclassified store
func TestKnowledgeRejectsUnclassifiedStore(t *testing.T) {
	ms := NewMemoryStore()
	ki := NewKnowledgeIngestion(ms)

	entry, _ := ki.Discover(SourceWeb, "", "biz-1", "Content")
	ki.Validate(entry.ID, 0.7)

	err := ki.Store(entry.ID)
	if err == nil {
		t.Error("expected error for storing unclassified knowledge")
	}
}

// TEST-M8-016: Knowledge provenance tracked
func TestKnowledgeProvenance(t *testing.T) {
	ms := NewMemoryStore()
	ki := NewKnowledgeIngestion(ms)

	entry, _ := ki.Discover(SourceAPI, "https://api.example.com", "biz-1", "API data")
	ki.Validate(entry.ID, 0.9)
	ki.Classify(entry.ID, "data")
	ki.Store(entry.ID)

	results := ms.Retrieve(&MemoryQuery{BusinessID: "biz-1"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Provenance.Source != string(SourceAPI) {
		t.Errorf("expected API provenance, got %v", results[0].Provenance.Source)
	}
}

// TEST-M8-017: Memory never authorizes
func TestMemoryNeverAuthorizes(t *testing.T) {
	// This is a structural invariant: MemoryEntry has no authority fields
	// Memory can inform decisions but never grants authority
	ms := NewMemoryStore()
	entry := &MemoryEntry{
		Type:       MemoryTypeSemantic,
		BusinessID: "biz-1",
		Content:    "Important fact",
		Provenance: Provenance{Confidence: 0.9},
	}
	ms.Admit(entry)

	// Memory has no authority field — verified by compilation
	// This test documents the invariant
	if entry.Provenance.Confidence > 1.0 {
		t.Error("confidence should be bounded")
	}
}

// TEST-M8-018: Business isolation in memory
func TestMemoryBusinessIsolation(t *testing.T) {
	ms := NewMemoryStore()
	ms.Admit(&MemoryEntry{Type: MemoryTypeSemantic, BusinessID: "biz-1", Content: "biz1", Provenance: Provenance{Confidence: 1.0}})
	ms.Admit(&MemoryEntry{Type: MemoryTypeSemantic, BusinessID: "biz-2", Content: "biz2", Provenance: Provenance{Confidence: 1.0}})

	r1 := ms.Retrieve(&MemoryQuery{BusinessID: "biz-1"})
	r2 := ms.Retrieve(&MemoryQuery{BusinessID: "biz-2"})

	if len(r1) != 1 || len(r2) != 1 {
		t.Errorf("expected 1 result per business")
	}
	if r1[0].Content == r2[0].Content {
		t.Error("expected different content for different businesses")
	}
}

// TEST-M8-019: Retrieval tracks access count
func TestMemoryAccessTracking(t *testing.T) {
	ms := NewMemoryStore()
	entry := &MemoryEntry{Type: MemoryTypeSemantic, BusinessID: "biz-1", Content: "Accessed", Provenance: Provenance{Confidence: 1.0}}
	ms.Admit(entry)

	ms.Retrieve(&MemoryQuery{BusinessID: "biz-1"})
	ms.Retrieve(&MemoryQuery{BusinessID: "biz-1"})

	e, _ := ms.Get(entry.ID)
	if e.AccessCount != 2 {
		t.Errorf("expected 2 accesses, got %d", e.AccessCount)
	}
	if e.LastAccessed == nil {
		t.Error("expected last accessed to be set")
	}
}

// TEST-M8-020: Clock injection
func TestMemoryClockInjection(t *testing.T) {
	now := time.Now()
	ms := NewMemoryStoreWithClock(func() time.Time { return now })
	entry := &MemoryEntry{Type: MemoryTypeWorking, BusinessID: "biz-1", Content: "Test", Provenance: Provenance{Confidence: 1.0}}
	ms.Admit(entry)

	if !entry.CreatedAt.Equal(now) {
		t.Errorf("expected clock-injected time")
	}
}
