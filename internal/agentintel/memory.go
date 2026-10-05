package agentintel

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// Record is one durable, organization-scoped memory entry (§11). Memory is a
// durable *record*, never execution state: nothing here is loop state, and no
// restart can resume a control loop from it (G4).
type Record struct {
	Key        string            `json:"key"`
	Value      string            `json:"value"`
	Scope      string            `json:"scope"` // "agent" | "business"
	AgentID    string            `json:"agent_id,omitempty"`
	BusinessID string            `json:"business_id"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// recordID namespaces the entry so two scopes can hold the same key without
// shadowing each other in the store's id-keyed index.
func recordID(businessID, agentID, key string) string {
	if agentID == "" {
		agentID = "_"
	}
	return fmt.Sprintf("mem:%s:%s:%s", businessID, agentID, key)
}

// MemoryStore is the durable agent/business memory the loop reads and writes
// through explicit actions only (§11, §13). Every operation is scope-checked;
// a foreign business or a foreign agent is denied, never silently empty.
type MemoryStore struct {
	mu  sync.RWMutex
	st  store.Store
	now func() time.Time
}

// NewMemory returns a process-local (non-durable) memory store.
func NewMemory() *MemoryStore {
	return &MemoryStore{now: time.Now}
}

// OpenMemory hydrates a durable store fail-closed, like the org registry: a
// corrupt or invalid record aborts boot rather than running on partial state.
func OpenMemory(st store.Store) (*MemoryStore, error) {
	m := NewMemory()
	m.st = st
	if st == nil {
		return m, nil
	}
	if err := m.hydrate(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *MemoryStore) hydrate() error {
	recs, err := m.st.List(store.Filter{Type: store.RecordTypeMemory})
	if err != nil {
		return fmt.Errorf("memory: hydrate: %w", err)
	}
	for _, rec := range recs {
		var r Record
		if err := json.Unmarshal(rec.Data, &r); err != nil {
			return fmt.Errorf("memory: corrupt record %q: %w", rec.ID, err)
		}
		if r.Key == "" || r.BusinessID == "" {
			return fmt.Errorf("memory: invalid record %q", rec.ID)
		}
		if recordID(r.BusinessID, r.AgentID, r.Key) != rec.ID {
			return fmt.Errorf("memory: record %q carries key %q", rec.ID, r.Key)
		}
	}
	return nil
}

func (m *MemoryStore) nowTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// Write persists an agent (or business) scoped entry. Writes only ever happen
// from an explicit memory_write action (§13).
func (m *MemoryStore) Write(businessID, agentID, key, value string) error {
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(key) == "" {
		return fmt.Errorf("memory: business_id and key are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.nowTime()
	id := recordID(businessID, agentID, key)
	if m.st == nil {
		return nil
	}
	rec := Record{Key: key, Value: value, Scope: "agent", AgentID: agentID,
		BusinessID: businessID, CreatedAt: now, UpdatedAt: now}
	prev, err := m.st.Get(id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("memory: read before persist %q: %w", id, err)
	}
	version := 0
	if prev != nil {
		var old Record
		if err := json.Unmarshal(prev.Data, &old); err != nil {
			return fmt.Errorf("memory: corrupt record %q: %w", id, err)
		}
		rec.CreatedAt = old.CreatedAt
		version = prev.Version
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	sr := &store.Record{ID: id, Type: store.RecordTypeMemory, Status: store.RecordStatusActive,
		BusinessID: businessID, Data: data, Version: version, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt}
	if err := m.st.Put(sr); err != nil {
		return fmt.Errorf("memory: persist %q: %w", id, err)
	}
	return nil
}

// Read returns one entry, scope-checked: the caller must be operating within
// the same business and the same agent it writes for.
func (m *MemoryStore) Read(businessID, agentID, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.st == nil {
		return "", fmt.Errorf("memory: no entry for key %q", key)
	}
	rec, err := m.st.Get(recordID(businessID, agentID, key))
	if err != nil || rec == nil {
		return "", fmt.Errorf("memory: no entry for key %q", key)
	}
	var r Record
	if err := json.Unmarshal(rec.Data, &r); err != nil {
		return "", err
	}
	if r.BusinessID != businessID || (r.AgentID != "" && r.AgentID != agentID) {
		return "", fmt.Errorf("memory: access denied for key %q", key)
	}
	return r.Value, nil
}

// Delete removes an entry the caller could have written.
func (m *MemoryStore) Delete(businessID, agentID, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.st == nil {
		return nil
	}
	id := recordID(businessID, agentID, key)
	rec, err := m.st.Get(id)
	if err != nil || rec == nil {
		return nil
	}
	var r Record
	if err := json.Unmarshal(rec.Data, &r); err == nil {
		if r.BusinessID != businessID || (r.AgentID != "" && r.AgentID != agentID) {
			return fmt.Errorf("memory: access denied for key %q", key)
		}
	}
	return m.st.Delete(id)
}

// List returns an agent's keys (deterministic order), for inspection and tests.
func (m *MemoryStore) List(businessID, agentID string) ([]string, error) {
	if m.st == nil {
		return nil, nil
	}
	recs, err := m.st.List(store.Filter{Type: store.RecordTypeMemory})
	if err != nil {
		return nil, err
	}
	keys := []string{}
	for _, rec := range recs {
		var r Record
		if err := json.Unmarshal(rec.Data, &r); err != nil {
			continue
		}
		if r.BusinessID != businessID || r.AgentID != agentID {
			continue
		}
		keys = append(keys, r.Key)
	}
	sort.Strings(keys)
	return keys, nil
}
