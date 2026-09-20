package memory

import (
	"fmt"
	"sync"
	"time"
)

// KnowledgeSource represents where knowledge came from.
type KnowledgeSource string

const (
	SourceWeb       KnowledgeSource = "web"
	SourceDocument  KnowledgeSource = "document"
	SourceAPI       KnowledgeSource = "api"
	SourceAgent     KnowledgeSource = "agent"
	SourceWorkflow  KnowledgeSource = "workflow"
	SourceOwner     KnowledgeSource = "owner"
	SourceIngestion KnowledgeSource = "ingestion"
)

// KnowledgeStatus tracks the ingestion pipeline status.
type KnowledgeStatus string

const (
	KStatusDiscovered KnowledgeStatus = "discovered"
	KStatusAcquired   KnowledgeStatus = "acquired"
	KStatusValidated  KnowledgeStatus = "validated"
	KStatusClassified KnowledgeStatus = "classified"
	KStatusStored     KnowledgeStatus = "stored"
	KStatusRejected   KnowledgeStatus = "rejected"
)

// KnowledgeEntry represents a piece of knowledge being ingested.
type KnowledgeEntry struct {
	ID             string          `json:"id"`
	Status         KnowledgeStatus `json:"status"`
	Source         KnowledgeSource `json:"source"`
	SourceURL      string          `json:"source_url,omitempty"`
	SourceID       string          `json:"source_id,omitempty"`
	BusinessID     string          `json:"business_id"`
	Content        string          `json:"content"`
	ExtractedAt    *time.Time      `json:"extracted_at,omitempty"`
	Classification string          `json:"classification,omitempty"` // "fact", "opinion", "data", "procedure"
	Confidence     float64         `json:"confidence"`               // 0.0 - 1.0
	Provenance     Provenance      `json:"provenance"`
	Tags           []string        `json:"tags,omitempty"`
	Entities       []string        `json:"entities,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// KnowledgeIngestion manages the knowledge ingestion pipeline.
// Source → Discovery → Acquisition → Validation → Classification → Store.
type KnowledgeIngestion struct {
	store   *MemoryStore
	entries map[string]*KnowledgeEntry
	mu      sync.RWMutex
	now     func() time.Time
}

// NewKnowledgeIngestion creates a new knowledge ingestion pipeline.
func NewKnowledgeIngestion(store *MemoryStore) *KnowledgeIngestion {
	return &KnowledgeIngestion{
		store:   store,
		entries: make(map[string]*KnowledgeEntry),
		now:     time.Now,
	}
}

// NewKnowledgeIngestionWithClock creates a new knowledge ingestion with an injectable clock.
func NewKnowledgeIngestionWithClock(store *MemoryStore, now func() time.Time) *KnowledgeIngestion {
	return &KnowledgeIngestion{
		store:   store,
		entries: make(map[string]*KnowledgeEntry),
		now:     now,
	}
}

// Discover records a new knowledge source for ingestion.
func (ki *KnowledgeIngestion) Discover(
	source KnowledgeSource, sourceURL, businessID, content string,
) (*KnowledgeEntry, error) {
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if businessID == "" {
		return nil, fmt.Errorf("business ID is required")
	}

	now := ki.now()
	entry := &KnowledgeEntry{
		ID:         fmt.Sprintf("know-%d", now.UnixNano()),
		Status:     KStatusDiscovered,
		Source:     source,
		SourceURL:  sourceURL,
		BusinessID: businessID,
		Content:    content,
		Confidence: 0.5, // default low confidence until validated
		Provenance: Provenance{
			Source:     string(source),
			Confidence: 0.5,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	ki.entries[entry.ID] = entry
	return entry, nil
}

// Validate marks knowledge as validated (content verified).
func (ki *KnowledgeIngestion) Validate(entryID string, confidence float64) error {
	ki.mu.Lock()
	defer ki.mu.Unlock()

	entry, ok := ki.entries[entryID]
	if !ok {
		return fmt.Errorf("knowledge entry %s not found", entryID)
	}

	entry.Status = KStatusValidated
	entry.Confidence = confidence
	entry.Provenance.Confidence = confidence
	now := ki.now()
	entry.Provenance.VerifiedAt = &now
	entry.UpdatedAt = now
	return nil
}

// Classify assigns a classification to knowledge.
func (ki *KnowledgeIngestion) Classify(entryID, classification string) error {
	ki.mu.Lock()
	defer ki.mu.Unlock()

	entry, ok := ki.entries[entryID]
	if !ok {
		return fmt.Errorf("knowledge entry %s not found", entryID)
	}

	entry.Status = KStatusClassified
	entry.Classification = classification
	entry.UpdatedAt = ki.now()
	return nil
}

// Store moves validated knowledge into the memory store.
func (ki *KnowledgeIngestion) Store(entryID string) error {
	ki.mu.Lock()
	defer ki.mu.Unlock()

	entry, ok := ki.entries[entryID]
	if !ok {
		return fmt.Errorf("knowledge entry %s not found", entryID)
	}

	if entry.Status != KStatusClassified {
		return fmt.Errorf("knowledge must be classified before storing (current: %s)", entry.Status)
	}

	// Create memory entry from knowledge
	memEntry := &MemoryEntry{
		Type:       MemoryTypeSemantic,
		BusinessID: entry.BusinessID,
		Content:    entry.Content,
		Summary:    entry.Classification,
		Tags:       entry.Tags,
		Entities:   entry.Entities,
		Provenance: entry.Provenance,
	}

	if err := ki.store.Admit(memEntry); err != nil {
		return fmt.Errorf("failed to admit knowledge to memory: %w", err)
	}

	entry.Status = KStatusStored
	entry.UpdatedAt = ki.now()
	return nil
}

// GetEntry returns a knowledge entry by ID.
func (ki *KnowledgeIngestion) GetEntry(entryID string) (*KnowledgeEntry, bool) {
	entry, ok := ki.entries[entryID]
	return entry, ok
}

// EntryCount returns the total number of knowledge entries.
func (ki *KnowledgeIngestion) EntryCount() int {
	return len(ki.entries)
}

// mu is needed for the methods above
// Actually, KnowledgeIngestion needs a mutex
