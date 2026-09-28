package identity

// Registry — the in-memory entity registry for the organization schemas
// (contracts/SCHEMA_IDENTITIES_ORG §2–§4): Identity, Business and Division
// records, their lifecycle transitions, and the audit trail §9 requires.
//
// Design notes (recorded decisions):
//   - In-memory only: durability is a later milestone (consistent with every
//     other runtime record — approvals, escalations, policies). The engine
//     Store() surface stays external-only (C-019).
//   - Records are created here only — nothing else materializes contract
//     entities. Cross-record rules (§8: business/division/owner references)
//     fail closed with VALIDATION errors.
//   - Audit: every create and status transition publishes an event when a
//     Publisher is wired (SCHEMA_IDENTITIES_ORG §9; ad-hoc event vocabulary
//     precedent approval.*). Without a publisher the registry stays usable
//     for tests and embedders that do not observe events.
//   - The registry stores no authority: owner_identity_id is descriptive
//     (MEMBERSHIP != AUTHORITY); authorization is resolved by Governance.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// Registry result sentinels (gateway maps them to 404/409).
var (
	ErrIdentityNotFound  = errors.New("registry: identity not found")
	ErrBusinessNotFound  = errors.New("registry: business not found")
	ErrDivisionNotFound  = errors.New("registry: division not found")
	ErrDuplicateEntity   = errors.New("registry: entity already exists")
	ErrInvalidTransition = errors.New("registry: invalid status transition")
)

// Publisher receives registry audit events. It matches *event.Bus.Publish.
type Publisher interface {
	Publish(*event.Event) error
}

// identityTransitions is the identity lifecycle matrix (§2.2 statuses). The
// contract fixes the statuses but not the edges; these edges are the
// conservative choice: revoked is terminal (fail closed), suspension is
// reversible, pending activates. Edges outside the matrix are rejected.
var identityTransitions = map[string][]string{
	"pending":   {"active", "revoked"},
	"active":    {"suspended", "revoked"},
	"suspended": {"active", "revoked"},
	"revoked":   nil,
}

// orgTransitions is the shared Business/Division lifecycle matrix (§3.1/§4.1
// statuses): archived is terminal, suspension is reversible.
var orgTransitions = map[string][]string{
	"active":    {"suspended", "archived"},
	"suspended": {"active", "archived"},
	"archived":  nil,
}

func canTransition(from, to string, matrix map[string][]string) bool {
	if from == to {
		return false
	}
	for _, n := range matrix[from] {
		if n == to {
			return true
		}
	}
	return false
}

// Registry holds the organization records for one NEXUS installation.
type Registry struct {
	mu         sync.RWMutex
	nexusID    string
	identities map[string]Identity
	businesses map[string]Business
	divisions  map[string]Division
	pub        Publisher
	now        func() time.Time
}

// NewRegistry constructs an empty registry stamped with the installation ID.
func NewRegistry(nexusID string) *Registry {
	return &Registry{
		nexusID:    nexusID,
		identities: map[string]Identity{},
		businesses: map[string]Business{},
		divisions:  map[string]Division{},
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// SetPublisher wires the audit-event sink (nil disables publishing).
func (r *Registry) SetPublisher(p Publisher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pub = p
}

// SetClock injects a clock for deterministic tests.
func (r *Registry) SetClock(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
}

// publishLocked enqueues one audit event. Call with r.mu held; delivery is
// fire-and-forget (audit must never block or fail the mutation — the bus
// drops on error, matching every other publisher in the system).
func (r *Registry) publishLocked(entityID string, typ event.EventType, businessID string, payload map[string]string) {
	if r.pub == nil {
		return
	}
	payload["entity_id"] = entityID
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = r.pub.Publish(&event.Event{
		ID:         fmt.Sprintf("%s-%s", entityID, typ),
		Type:       typ,
		Source:     "identity.registry",
		Timestamp:  r.now(),
		BusinessID: businessID,
		Priority:   event.PriorityNormal,
		Data:       data,
	})
}

// ---------- Identity ----------

// CreateIdentity validates and stores an identity record, stamping contract
// defaults (envelope fields, nexus_id, id, created_at, provenance) where the
// caller left them empty, enforcing the §8 reference rules, and emitting
// identity.created. actor labels the creator for the audit trail.
func (r *Registry) CreateIdentity(ident Identity, actor string) (Identity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	if ident.SchemaVersion == "" {
		ident.SchemaVersion = "1.0.0"
	}
	if ident.EntityType == "" {
		ident.EntityType = entityTypeIdentity
	}
	if ident.NexusID == "" {
		ident.NexusID = r.nexusID
	}
	if ident.ID == "" {
		id, err := security.NewID(string(ident.Type))
		if err != nil {
			return Identity{}, err
		}
		ident.ID = id
	}
	if ident.Status == "" {
		ident.Status = StatusActive
	}
	if ident.CreatedAt.IsZero() {
		ident.CreatedAt = now
	}
	if !ident.Provenance.Valid() {
		ident.Provenance.Origin = "system"
		ident.Provenance.Producer = "registry.CreateIdentity"
		ident.Provenance.ProducedAt = now
	}
	if err := ident.Validate(); err != nil {
		return Identity{}, err
	}
	if _, dup := r.identities[ident.ID]; dup {
		return Identity{}, fmt.Errorf("%w: identity %q", ErrDuplicateEntity, ident.ID)
	}
	// §8: a business-scoped identity must reference an existing, active
	// business; a division scope must reference a division of that business.
	if ident.BusinessID != "" {
		b, ok := r.businesses[ident.BusinessID]
		if !ok {
			return Identity{}, nerrors.Validation("registry.business_not_found",
				fmt.Sprintf("business %q does not exist", ident.BusinessID))
		}
		if b.Status != BusinessActive {
			return Identity{}, nerrors.Validation("registry.business_not_active",
				fmt.Sprintf("business %q is %s (must be active)", ident.BusinessID, b.Status))
		}
	}
	if ident.DivisionID != "" {
		d, ok := r.divisions[ident.DivisionID]
		if !ok {
			return Identity{}, nerrors.Validation("registry.division_not_found",
				fmt.Sprintf("division %q does not exist", ident.DivisionID))
		}
		if d.BusinessID != ident.BusinessID {
			return Identity{}, nerrors.Validation("registry.division_business_mismatch",
				"division_id must belong to the identity's business (§8)")
		}
		if d.Status != DivisionActive {
			return Identity{}, nerrors.Validation("registry.division_not_active",
				fmt.Sprintf("division %q is %s (must be active)", ident.DivisionID, d.Status))
		}
	}

	r.identities[ident.ID] = ident
	r.publishLocked(ident.ID, event.EventTypeIdentityCreated, ident.BusinessID, map[string]string{
		"identity_type": string(ident.Type),
		"business_id":   ident.BusinessID,
		"division_id":   ident.DivisionID,
		"status":        string(ident.Status),
		"actor":         actor,
	})
	return ident, nil
}

// GetIdentity returns one identity record.
func (r *Registry) GetIdentity(id string) (Identity, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ident, ok := r.identities[id]
	return ident, ok
}

// ListIdentities returns identities sorted by ID. An empty businessID lists
// every identity (callers that scope should pass the business).
func (r *Registry) ListIdentities(businessID string) []Identity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Identity, 0, len(r.identities))
	for _, i := range r.identities {
		if businessID == "" || i.BusinessID == businessID {
			out = append(out, i)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// SetIdentityStatus applies a lifecycle transition (matrix above) and emits
// identity.status_changed. An unknown id is ErrIdentityNotFound; an edge
// outside the matrix — including any change to the terminal revoked state —
// is ErrInvalidTransition.
func (r *Registry) SetIdentityStatus(id string, to Status, actor string) (Identity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ident, ok := r.identities[id]
	if !ok {
		return Identity{}, fmt.Errorf("%w: identity %q", ErrIdentityNotFound, id)
	}
	if !to.IsValid() {
		return Identity{}, nerrors.Validation("registry.status_invalid",
			fmt.Sprintf("status %q is not valid", to))
	}
	if !canTransition(string(ident.Status), string(to), identityTransitions) {
		return Identity{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, ident.Status, to)
	}
	from := ident.Status
	ident.Status = to
	now := r.now()
	ident.UpdatedAt = &now
	r.identities[id] = ident
	r.publishLocked(id, event.EventTypeIdentityStatusChanged, ident.BusinessID, map[string]string{
		"from":   string(from),
		"to":     string(to),
		"actor":  actor,
		"status": string(to),
	})
	return ident, nil
}

// RemoveIdentity deletes a record. It exists solely to roll back a failed
// credential registration during create; the public surface has no delete
// (the contract defines no identity delete — status revoked is the lifecycle
// end). Removing an unknown id is a no-op error-free success (idempotent).
func (r *Registry) RemoveIdentity(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.identities, id)
}

// ---------- Business ----------

// CreateBusiness validates and stores a business record (§3.1), enforcing the
// owner rules (existing, human, active identity) and emitting
// business.onboarded.
func (r *Registry) CreateBusiness(b Business, actor string) (Business, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	if b.SchemaVersion == "" {
		b.SchemaVersion = "1.0.0"
	}
	if b.EntityType == "" {
		b.EntityType = "business"
	}
	if b.NexusID == "" {
		b.NexusID = r.nexusID
	}
	if b.EntityID == "" {
		id, err := security.NewID("business")
		if err != nil {
			return Business{}, err
		}
		b.EntityID = id
		b.BusinessID = id
	}
	if b.BusinessID == "" {
		b.BusinessID = b.EntityID
	}
	if b.Status == "" {
		b.Status = BusinessActive
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	if !b.Provenance.Valid() {
		b.Provenance.Origin = "system"
		b.Provenance.Producer = "registry.CreateBusiness"
		b.Provenance.ProducedAt = now
	}
	if err := b.Validate(); err != nil {
		return Business{}, err
	}
	if _, dup := r.businesses[b.EntityID]; dup {
		return Business{}, fmt.Errorf("%w: business %q", ErrDuplicateEntity, b.EntityID)
	}
	owner, ok := r.identities[b.OwnerIdentityID]
	if !ok {
		return Business{}, nerrors.Validation("registry.owner_not_found",
			fmt.Sprintf("owner_identity_id %q does not reference an existing identity", b.OwnerIdentityID))
	}
	if owner.Type != TypeHuman {
		return Business{}, nerrors.Validation("registry.owner_not_human",
			"business owner_identity_id must reference a human identity (§3.1)")
	}
	if owner.Status != StatusActive {
		return Business{}, nerrors.Validation("registry.owner_not_active",
			fmt.Sprintf("owner identity %q is %s (must be active)", owner.ID, owner.Status))
	}

	r.businesses[b.EntityID] = b
	r.publishLocked(b.EntityID, event.EventTypeBusinessOnboarded, b.BusinessID, map[string]string{
		"name":              b.Name,
		"owner_identity_id": b.OwnerIdentityID,
		"actor":             actor,
	})
	return b, nil
}

// GetBusiness returns one business record.
func (r *Registry) GetBusiness(id string) (Business, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.businesses[id]
	return b, ok
}

// ListBusinesses returns all businesses sorted by entity_id.
func (r *Registry) ListBusinesses() []Business {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Business, 0, len(r.businesses))
	for _, b := range r.businesses {
		out = append(out, b)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].EntityID < out[b].EntityID })
	return out
}

// SetBusinessStatus applies a lifecycle transition (active/suspended/archived;
// archived is terminal) and emits business.status_changed.
func (r *Registry) SetBusinessStatus(id string, to BusinessStatus, actor string) (Business, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	b, ok := r.businesses[id]
	if !ok {
		return Business{}, fmt.Errorf("%w: business %q", ErrBusinessNotFound, id)
	}
	if !to.IsValid() {
		return Business{}, nerrors.Validation("registry.status_invalid",
			fmt.Sprintf("status %q is not valid", to))
	}
	if !canTransition(string(b.Status), string(to), orgTransitions) {
		return Business{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, b.Status, to)
	}
	from := b.Status
	b.Status = to
	now := r.now()
	b.UpdatedAt = &now
	r.businesses[id] = b
	r.publishLocked(id, event.EventTypeBusinessStatusChanged, b.BusinessID, map[string]string{
		"from":   string(from),
		"to":     string(to),
		"actor":  actor,
		"status": string(to),
	})
	return b, nil
}

// ---------- Division ----------

// CreateDivision validates and stores a division record (§4.1), enforcing the
// §8 reference rules (existing active business, scope-compatible owner) and
// emitting division.created. The new division is appended to its business's
// divisions list (§3.1).
func (r *Registry) CreateDivision(d Division, actor string) (Division, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	if d.SchemaVersion == "" {
		d.SchemaVersion = "1.0.0"
	}
	if d.EntityType == "" {
		d.EntityType = "division"
	}
	if d.NexusID == "" {
		d.NexusID = r.nexusID
	}
	if d.EntityID == "" {
		id, err := security.NewID("division")
		if err != nil {
			return Division{}, err
		}
		d.EntityID = id
	}
	if d.ParentBusinessID == "" {
		d.ParentBusinessID = d.BusinessID
	}
	if d.Status == "" {
		d.Status = DivisionActive
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	if !d.Provenance.Valid() {
		d.Provenance.Origin = "system"
		d.Provenance.Producer = "registry.CreateDivision"
		d.Provenance.ProducedAt = now
	}
	if err := d.Validate(); err != nil {
		return Division{}, err
	}
	if _, dup := r.divisions[d.EntityID]; dup {
		return Division{}, fmt.Errorf("%w: division %q", ErrDuplicateEntity, d.EntityID)
	}
	b, ok := r.businesses[d.BusinessID]
	if !ok {
		return Division{}, nerrors.Validation("registry.business_not_found",
			fmt.Sprintf("business %q does not exist", d.BusinessID))
	}
	if b.Status != BusinessActive {
		return Division{}, nerrors.Validation("registry.business_not_active",
			fmt.Sprintf("business %q is %s (must be active)", d.BusinessID, b.Status))
	}
	owner, ok := r.identities[d.OwnerIdentityID]
	if !ok {
		return Division{}, nerrors.Validation("registry.owner_not_found",
			fmt.Sprintf("owner_identity_id %q does not reference an existing identity", d.OwnerIdentityID))
	}
	if owner.Status != StatusActive {
		return Division{}, nerrors.Validation("registry.owner_not_active",
			fmt.Sprintf("owner identity %q is %s (must be active)", owner.ID, owner.Status))
	}
	// §6.1/INV-01: the owner must sit inside the division's business (or be
	// global) — never in a different business.
	if owner.BusinessID != "" && owner.BusinessID != d.BusinessID {
		return Division{}, nerrors.Validation("registry.owner_scope_mismatch",
			"division owner belongs to a different business")
	}

	r.divisions[d.EntityID] = d
	b.Divisions = append(b.Divisions, d.EntityID)
	b.UpdatedAt = &now
	r.businesses[b.EntityID] = b
	r.publishLocked(d.EntityID, event.EventTypeDivisionCreated, d.BusinessID, map[string]string{
		"business_id": d.BusinessID,
		"name":        d.Name,
		"actor":       actor,
	})
	return d, nil
}

// GetDivision returns one division record.
func (r *Registry) GetDivision(id string) (Division, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.divisions[id]
	return d, ok
}

// ListDivisions returns the divisions of a business sorted by entity_id.
// An empty businessID lists every division.
func (r *Registry) ListDivisions(businessID string) []Division {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Division, 0, len(r.divisions))
	for _, d := range r.divisions {
		if businessID == "" || d.BusinessID == businessID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].EntityID < out[b].EntityID })
	return out
}

// SetDivisionStatus applies a lifecycle transition (active/suspended/archived;
// archived is terminal) and emits division.status_changed.
func (r *Registry) SetDivisionStatus(id string, to DivisionStatus, actor string) (Division, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.divisions[id]
	if !ok {
		return Division{}, fmt.Errorf("%w: division %q", ErrDivisionNotFound, id)
	}
	if !to.IsValid() {
		return Division{}, nerrors.Validation("registry.status_invalid",
			fmt.Sprintf("status %q is not valid", to))
	}
	if !canTransition(string(d.Status), string(to), orgTransitions) {
		return Division{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, d.Status, to)
	}
	from := d.Status
	d.Status = to
	now := r.now()
	d.UpdatedAt = &now
	r.divisions[id] = d
	r.publishLocked(id, event.EventTypeDivisionStatusChanged, d.BusinessID, map[string]string{
		"business_id": d.BusinessID,
		"from":        string(from),
		"to":          string(to),
		"actor":       actor,
		"status":      string(to),
	})
	return d, nil
}
