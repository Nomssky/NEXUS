package agentexec

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// Duplicate and transition sentinels map to HTTP semantics (G5: unknown id and
// foreign scope collapse to one 404 where the gateway enforces it).
var (
	ErrDuplicate     = errors.New("agentexec: agent already exists")
	ErrNotFound      = errors.New("agentexec: agent not found")
	ErrBadTransition = errors.New("agentexec: invalid status transition")
)

// Registry persists agent definitions (write-through to the same store the
// org registry uses, fail-closed hydration, no event storm on hydrate).
type Registry struct {
	mu      sync.RWMutex
	nexusID string
	agents  map[string]*Definition
	now     func() time.Time
	st      store.Store
	orgs    *identity.Registry
	tools   *tool.ToolRegistry
}

// NewRegistry returns an in-memory registry. nil orgs/tools force fail-closed
// create validation (business unknown → rejected).
func NewRegistry(nexusID string, orgs *identity.Registry, tools *tool.ToolRegistry) *Registry {
	return &Registry{nexusID: nexusID, agents: map[string]*Definition{}, now: nowUTC, orgs: orgs, tools: tools}
}

func nowUTC() time.Time { return time.Now().UTC() }

// OpenRegistry is NewRegistry + write-through persistence + fail-closed
// hydration. A nil store degrades to process-local state (G4: still Level 1).
func OpenRegistry(nexusID string, orgs *identity.Registry, tools *tool.ToolRegistry, st store.Store) (*Registry, error) {
	r := NewRegistry(nexusID, orgs, tools)
	if st == nil {
		return r, nil
	}
	r.st = st
	if err := r.hydrate(); err != nil {
		return nil, err
	}
	return r, nil
}

type recordEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	EntityType    string `json:"entity_type"`
	ID            string `json:"id"`
	Definition    any    `json:"definition"`
}

func (r *Registry) hydrate() error {
	records, err := r.st.List(store.Filter{Type: store.RecordTypeAgent})
	if err != nil {
		return fmt.Errorf("agents: hydrate: %w", err)
	}
	for _, rec := range records {
		var d Definition
		if err := json.Unmarshal(rec.Data, &d); err != nil {
			return fmt.Errorf("agents: corrupt record %q: %w", rec.ID, err)
		}
		if err := d.Validate(); err != nil {
			return fmt.Errorf("agents: invalid record %q: %w", rec.ID, err)
		}
		if d.ID != rec.ID {
			return fmt.Errorf("agents: record %q carries id %q", rec.ID, d.ID)
		}
		cp := d
		r.agents[rec.ID] = &cp
	}
	return nil
}

// Create validates and persists a new agent definition (persist → memory).
func (r *Registry) Create(d Definition, actor string) (Definition, error) {
	if r.orgs == nil {
		return Definition{}, errors.New("agents: org registry unavailable")
	}
	if d.SchemaVersion == "" {
		d.SchemaVersion = "1.0.0"
	}
	if d.EntityType == "" {
		d.EntityType = "agent"
	}
	if d.NexusID == "" {
		d.NexusID = r.nexusID
	}
	if d.Status == "" {
		d.Status = AgentActive
	}
	now := r.now()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = d.CreatedAt
	if d.Provenance.Producer == "" {
		d.Provenance = schema.ProvenanceRef{Origin: "human", Producer: "gateway:http-api", ProducedAt: now}
	}
	if d.ID == "" {
		return Definition{}, errors.New("id is required (client-drawn stable identifier)")
	}
	if err := d.Validate(); err != nil {
		return Definition{}, err
	}
	// Org references must exist and be active (SCHEMA_IDENTITIES_ORG §8 analog).
	b, ok := r.orgs.GetBusiness(d.BusinessID)
	if !ok {
		return Definition{}, fmt.Errorf("business %q does not exist", d.BusinessID)
	}
	if b.Status != "active" {
		return Definition{}, fmt.Errorf("business %q is %s (must be active)", d.BusinessID, b.Status)
	}
	if d.DivisionID != "" {
		div, ok := r.orgs.GetDivision(d.DivisionID)
		if !ok {
			return Definition{}, fmt.Errorf("division %q does not exist", d.DivisionID)
		}
		if div.BusinessID != d.BusinessID {
			return Definition{}, fmt.Errorf("division %q does not belong to %q", d.DivisionID, d.BusinessID)
		}
		if div.Status != "active" {
			return Definition{}, fmt.Errorf("division %q is %s (must be active)", d.DivisionID, div.Status)
		}
	}
	// Unknown tool ids are rejected at registration, not at invocation.
	if r.tools != nil {
		for _, id := range d.AllowedTools {
			if _, ok := r.tools.GetTool(id); !ok {
				return Definition{}, fmt.Errorf("tool %q is not registered", id)
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.agents[d.ID]; dup {
		return Definition{}, fmt.Errorf("%w: %s", ErrDuplicate, d.ID)
	}
	if r.st != nil {
		b, _ := json.Marshal(d)
		rec := &store.Record{
			ID: d.ID, Type: store.RecordTypeAgent, Status: store.RecordStatusActive,
			BusinessID: d.BusinessID, DivisionID: d.DivisionID, Data: b,
		}
		if err := r.persistRecord(rec); err != nil {
			return Definition{}, err
		}
	}
	cp := d
	r.agents[d.ID] = &cp
	return cp, nil
}

// persistRecord mirrors identity.Registry: carry over the previous version so
// both store implementations accept the write. Must be called with r.mu held.
func (r *Registry) persistRecord(rec *store.Record) error {
	if r.st == nil {
		return nil
	}
	prev, err := r.st.Get(rec.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("agents: read before persist %q: %w", rec.ID, err)
	}
	if prev != nil {
		rec.Version = prev.Version
	}
	if err := r.st.Put(rec); err != nil {
		return fmt.Errorf("agents: persist %q: %w", rec.ID, err)
	}
	return nil
}

// Get returns one definition; ok=false collapses unknown/foreign to one
// not-found (the gateway maps visibility on top of the AllowsScope rule).
func (r *Registry) Get(id string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.agents[id]
	if !ok {
		return Definition{}, false
	}
	return *d, true
}

// List returns definitions, optionally narrowed to a division filter ("" yields
// every agent of the business, mirroring business/division listing surfaces).
func (r *Registry) List(businessID, divisionID string) []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []Definition{}
	for _, d := range r.agents {
		if d.BusinessID != businessID {
			continue
		}
		if divisionID != "" && d.DivisionID != divisionID {
			continue
		}
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SetStatus applies a lifecycle transition.
func (r *Registry) SetStatus(id string, to Status, actor string) (Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.agents[id]
	if !ok {
		return Definition{}, ErrNotFound
	}
	if !canTransition(d.Status, to) {
		return Definition{}, fmt.Errorf("%w: %s -> %s", ErrBadTransition, d.Status, to)
	}
	d.Status = to
	d.UpdatedAt = r.now()
	if r.st != nil {
		b, _ := json.Marshal(*d)
		rec := &store.Record{
			ID: d.ID, Type: store.RecordTypeAgent, Status: store.RecordStatusActive,
			BusinessID: d.BusinessID, DivisionID: d.DivisionID, Data: b,
		}
		if err := r.persistRecord(rec); err != nil {
			return Definition{}, err
		}
	}
	return *d, nil
}

// Update applies a safe in-place update of the mutable projection fields.
func (r *Registry) Update(id string, mut func(*Definition) error) (Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.agents[id]
	if !ok {
		return Definition{}, ErrNotFound
	}
	cp := *d
	if err := mut(&cp); err != nil {
		return Definition{}, err
	}
	if err := cp.Validate(); err != nil {
		return Definition{}, err
	}
	cp.UpdatedAt = r.now()
	if r.st != nil {
		b, _ := json.Marshal(cp)
		rec := &store.Record{ID: cp.ID, Type: store.RecordTypeAgent, Status: store.RecordStatusActive,
			BusinessID: cp.BusinessID, DivisionID: cp.DivisionID, Data: b}
		if err := r.persistRecord(rec); err != nil {
			return Definition{}, err
		}
	}
	rd := cp
	r.agents[id] = &rd
	return cp, nil
}
