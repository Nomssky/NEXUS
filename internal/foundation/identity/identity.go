// Package identity implements the M1 identity foundation (component C02 Identity
// & Trust, layer L1).
//
// Scope note: this is the *structural* identity foundation only. It models WHO
// exists and WHAT SCOPE they act within. It deliberately does NOT model
// authority, capability grants, permission grants, or trust, because those are
// resolved separately (authority by Governance / component C03; capability by
// the runtime that provides it; trust as contextual confidence).
//
// Invariants preserved here (see contracts/SCHEMA_INVARIANTS.md):
//   - INV-04: IDENTITY != AUTHORITY — an Identity carries no authority field.
//   - INV-05/06/07: capability/permission/trust are NOT stored on Identity; the
//     type has no such fields.
//   - INV-01: multi-business isolation — business/division scope is explicit and
//     an identity scoped to one business cannot silently act for another.
//
// Deferred (recorded, not implemented): durable identity storage, identity
// lifecycle transitions/persistence (C05), identity recovery, anomaly detection,
// federation, runtime agent-identity lifecycle (C12), delegation chains (C03).
package identity

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// entityTypeIdentity is the canonical entity_type for an Identity record
// (SCHEMA_IDENTITIES_ORG §2.2: always "identity").
const entityTypeIdentity = "identity"

// Type is the canonical identity type. It mirrors the minimal identity types in
// Core/NEXUS-IDENTITY-ACCESS-TRUST-SYSTEM.md §4 and the contract enum in
// contracts/SCHEMA_IDENTITIES_ORG.md §2.3, extended with the organization and
// execution identity kinds later modules reference.
//
// A Type answers "WHAT IS THIS?" — it is never an authority (INV-04).
type Type string

const (
	TypeHuman     Type = "human"
	TypeAgent     Type = "agent"
	TypeService   Type = "service"
	TypeDevice    Type = "device"
	TypeSystem    Type = "system"
	TypeBusiness  Type = "business"
	TypeDivision  Type = "division"
	TypeWorkflow  Type = "workflow"
	TypeTask      Type = "task"
	TypeProvider  Type = "provider"
	TypeModel     Type = "model"
	TypeTool      Type = "tool"
	TypeConnector Type = "connector"
)

var validTypes = map[Type]struct{}{
	TypeHuman: {}, TypeAgent: {}, TypeService: {}, TypeDevice: {}, TypeSystem: {},
	TypeBusiness: {}, TypeDivision: {}, TypeWorkflow: {}, TypeTask: {},
	TypeProvider: {}, TypeModel: {}, TypeTool: {}, TypeConnector: {},
}

// Status is the identity lifecycle status (contract: active/suspended/revoked/
// pending, plus the agent-oriented states used at later milestones).
type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusRevoked   Status = "revoked"
	StatusPending   Status = "pending"
)

var validStatuses = map[Status]struct{}{
	StatusActive: {}, StatusSuspended: {}, StatusRevoked: {}, StatusPending: {},
}

// IsValid reports whether t is a canonical identity type.
func (t Type) IsValid() bool { _, ok := validTypes[t]; return ok }

// IsValid reports whether s is a canonical identity status.
func (s Status) IsValid() bool { _, ok := validStatuses[s]; return ok }

// ScopeKind is the applicable scope of an identity or authorization.
//
// Scope answers "WHERE does this apply?". It is NOT authority: possessing a
// business scope does not grant permission within it (business scope is an
// isolation boundary, not a permission — BUSINESS_SCOPE != GLOBAL_ACCESS).
type ScopeKind string

const (
	ScopeGlobal    ScopeKind = "GLOBAL"
	ScopeBusiness  ScopeKind = "BUSINESS"
	ScopeDivision  ScopeKind = "DIVISION"
	ScopeAgent     ScopeKind = "AGENT"
	ScopeWorkflow  ScopeKind = "WORKFLOW"
	ScopeTask      ScopeKind = "TASK"
	ScopeTemporary ScopeKind = "TEMPORARY"
)

var scopeRank = map[ScopeKind]int{
	ScopeGlobal: 0, ScopeBusiness: 1, ScopeDivision: 2,
	ScopeAgent: 3, ScopeWorkflow: 4, ScopeTask: 5, ScopeTemporary: 6,
}

// IsValid reports whether s is a canonical scope kind.
func (s ScopeKind) IsValid() bool { _, ok := scopeRank[s]; return ok }

// NarrowerThan reports whether s is strictly narrower than other. Narrower =
// larger rank (GLOBAL is widest). Returning to the narrowest possible scope is
// the safe default (§6.2 "default to narrowest scope").
func (s ScopeKind) NarrowerThan(other ScopeKind) bool {
	return scopeRank[s] > scopeRank[other]
}

// Scope is an explicit scope with at most one value per level. Empty strings mean
// "not at this level". Scope carries NO permission.
type Scope struct {
	Kind       ScopeKind `json:"kind"`
	BusinessID string    `json:"business_id,omitempty"`
	DivisionID string    `json:"division_id,omitempty"`
	AgentID    string    `json:"agent_id,omitempty"`
	WorkflowID string    `json:"workflow_id,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
}

// GlobalScope returns a scope with no business/division/execution binding.
func GlobalScope() Scope { return Scope{Kind: ScopeGlobal} }

// BusinessScope returns a business-scoped scope.
func BusinessScope(businessID string) Scope {
	return Scope{Kind: ScopeBusiness, BusinessID: businessID}
}

// DivisionScope returns a division-scoped scope (must be within a business).
func DivisionScope(businessID, divisionID string) Scope {
	return Scope{Kind: ScopeDivision, BusinessID: businessID, DivisionID: divisionID}
}

// Validate checks internal consistency of the scope. It fails closed on
// contradictory scopes (e.g. a division without a business).
func (s Scope) Validate() error {
	if !s.Kind.IsValid() {
		return nerrors.Validation("identity.scope_invalid",
			fmt.Sprintf("scope kind %q is not valid", s.Kind))
	}
	if s.Kind != ScopeGlobal {
		if s.DivisionID != "" && s.BusinessID == "" {
			return nerrors.Validation("identity.scope_invalid",
				"division scope requires a business scope")
		}
	}
	switch s.Kind {
	case ScopeBusiness:
		if s.BusinessID == "" {
			return nerrors.Validation("identity.scope_invalid", "business scope requires business_id")
		}
	case ScopeDivision:
		if s.BusinessID == "" || s.DivisionID == "" {
			return nerrors.Validation("identity.scope_invalid",
				"division scope requires business_id and division_id")
		}
	case ScopeAgent:
		if s.AgentID == "" {
			return nerrors.Validation("identity.scope_invalid", "agent scope requires agent_id")
		}
	case ScopeWorkflow:
		if s.WorkflowID == "" {
			return nerrors.Validation("identity.scope_invalid", "workflow scope requires workflow_id")
		}
	case ScopeTask:
		if s.TaskID == "" {
			return nerrors.Validation("identity.scope_invalid", "task scope requires task_id")
		}
	}
	return nil
}

// Covers reports whether this scope permits visibility of the target scope,
// following the locked scope hierarchy rules (SCHEMA_COMMON.md §6.2):
//
//   - a parent scope can see its children (downward visibility);
//   - siblings are mutually isolated (cross-business/cross-division denied);
//   - cross-scope access is denied by default and requires an explicit policy
//     decision by Governance (NOT made here).
//
// This method answers a *visibility* question only. It is NOT an authorization
// decision and must never be used as one.
func (s Scope) Covers(target Scope) bool {
	if err := s.Validate(); err != nil {
		return false
	}
	if err := target.Validate(); err != nil {
		return false
	}
	// A global scope sees everything within the installation.
	if s.Kind == ScopeGlobal {
		return true
	}
	// Business isolation: two non-empty, different business IDs never see each other.
	if s.BusinessID != "" && target.BusinessID != "" && s.BusinessID != target.BusinessID {
		return false
	}
	// If the target is business-scoped, the source must be that business or its ancestor.
	if target.BusinessID != "" && s.BusinessID != target.BusinessID {
		return false
	}
	// A source narrowed to a division must not cover a whole-business target
	// (a division is narrower than the business).
	if s.DivisionID != "" && target.DivisionID == "" {
		return false
	}
	// Division isolation: siblings never see each other.
	if s.DivisionID != "" && target.DivisionID != "" && s.DivisionID != target.DivisionID {
		return false
	}
	if target.DivisionID != "" && s.DivisionID != "" && s.DivisionID != target.DivisionID {
		return false
	}
	// Same business+division (or source is broader at this level) -> visible.
	return true
}

// String renders the scope compactly for logs (never secret-bearing).
func (s Scope) String() string {
	parts := []string{string(s.Kind)}
	if s.BusinessID != "" {
		parts = append(parts, "biz="+s.BusinessID)
	}
	if s.DivisionID != "" {
		parts = append(parts, "div="+s.DivisionID)
	}
	return strings.Join(parts, ":")
}

// Identity is the structural record of WHO something is.
//
// It intentionally has NO authority, capability, permission, or trust fields
// (INV-04/05/06/07). Trust, authority, capabilities, and permissions are
// resolved elsewhere and are referenced, not embedded.
type Identity struct {
	// SchemaVersion is the common envelope schema version (SCHEMA_COMMON §3.1).
	SchemaVersion string `json:"schema_version"`
	// EntityType is always "identity" (contract §2.2).
	EntityType string `json:"entity_type"`
	// ID is the unique identifier, e.g. "nx:agent:<hex>".
	ID string `json:"entity_id"`
	// NexusID is the NEXUS installation identifier.
	NexusID string `json:"nexus_id"`
	// Type is the identity type (who/what).
	Type Type `json:"identity_type"`
	// DisplayName is a human-readable name.
	DisplayName string `json:"display_name"`
	// Status is the lifecycle status.
	Status Status `json:"status"`

	// CreatedAt is when the identity was created (contract §2.2, required).
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the identity was last modified (contract §2.2).
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// ExpiresAt bounds the identity's validity (contract §2.2: e.g. temporary
	// agents). Authentication is refused once this passes (enforced by
	// LocalAuthenticator when a registry is wired).
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// BusinessID is the flat contract scope field (§2.2: conditionally
	// required — present for business-scoped identities, absent for
	// global/system identities). It mirrors Scope.BusinessID; Validate keeps
	// the two representations consistent.
	BusinessID string `json:"business_id,omitempty"`
	// DivisionID is the flat contract division scope field (§2.2), mirroring
	// Scope.DivisionID.
	DivisionID string `json:"division_id,omitempty"`

	// Scope is the primary scoping of this identity (business/division/...).
	Scope Scope `json:"scope"`

	// ParentID optionally references a parent identity (e.g. spawner agent or
	// managing human). It must never cross businesses (INV-01).
	ParentID string `json:"parent_id,omitempty"`
	// IdentityRef optionally references the Identity record for execution
	// identities (e.g. an agent's stable identity, per SCHEMA_IDENTITIES_ORG §5).
	IdentityRef string `json:"identity_ref,omitempty"`

	// Provenance is the required origin record (contract §2.2, SCHEMA_COMMON §4).
	Provenance schema.ProvenanceRef `json:"provenance"`

	// Metadata carries namespaced extension data. It must never hold secrets.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Validate checks structural correctness. It returns a canonical VALIDATION
// error on the first problem (deterministic). A malformed identity is rejected.
//
// Validate has a pointer receiver because it first reconciles the flat
// contract scope fields (business_id/division_id) with the nested Scope
// representation — exactly one of the two may drive, never a contradiction.
func (i *Identity) Validate() error {
	if strings.TrimSpace(i.ID) == "" {
		return nerrors.Validation("identity.id_required", "identity id is required")
	}
	if strings.TrimSpace(i.NexusID) == "" {
		return nerrors.Validation("identity.nexus_id_required", "identity nexus_id is required")
	}
	if !i.Type.IsValid() {
		return nerrors.Validation("identity.type_invalid",
			fmt.Sprintf("identity type %q is not valid", i.Type))
	}
	if !i.Status.IsValid() {
		return nerrors.Validation("identity.status_invalid",
			fmt.Sprintf("identity status %q is not valid", i.Status))
	}
	if strings.TrimSpace(i.DisplayName) == "" {
		return nerrors.Validation("identity.display_name_required", "identity display_name is required")
	}
	if err := i.reconcileScope(); err != nil {
		return err
	}
	if i.SchemaVersion == "" {
		return nerrors.Validation("identity.schema_version_required", "identity schema_version is required")
	}
	if i.EntityType != entityTypeIdentity {
		return nerrors.Validation("identity.entity_type_invalid",
			fmt.Sprintf("entity_type must be %q, got %q", entityTypeIdentity, i.EntityType))
	}
	if i.CreatedAt.IsZero() {
		return nerrors.Validation("identity.created_at_required", "identity created_at is required")
	}
	if !i.Provenance.Valid() {
		return nerrors.Validation("identity.provenance_required",
			"identity provenance must carry origin, producer and produced_at")
	}
	if i.ExpiresAt != nil && !i.ExpiresAt.After(i.CreatedAt) {
		return nerrors.Validation("identity.expires_at_invalid",
			"identity expires_at must be after created_at")
	}
	if err := i.Scope.Validate(); err != nil {
		return err
	}
	// Global identities (business/division/system) must not carry a business
	// scope; business-scoped identities must carry one.
	if i.isGlobalType() && i.Scope.BusinessID != "" {
		return nerrors.Validation("identity.scope_invalid",
			"global identity types must not be business-scoped")
	}
	if i.isBusinessScopedType() && i.Scope.BusinessID == "" {
		return nerrors.Validation("identity.scope_invalid",
			fmt.Sprintf("%s identity requires a business scope", i.Type))
	}
	if err := i.validateMetadata(); err != nil {
		return err
	}
	return nil
}

// reconcileScope keeps the flat contract fields (business_id/division_id) and
// the nested Scope in lockstep: whichever side is populated fills the other;
// a contradiction between the two fails closed.
func (i *Identity) reconcileScope() error {
	flatBusiness, flatDivision := i.BusinessID, i.DivisionID
	scopeBusiness, scopeDivision := i.Scope.BusinessID, i.Scope.DivisionID

	switch {
	case flatBusiness == "" && flatDivision == "":
		// Scope drives: hydrate the flat contract fields from it. A record with
		// no scope information at all is global (§2.2: business_id absent for
		// global identities).
		i.BusinessID = scopeBusiness
		i.DivisionID = scopeDivision
		if i.Scope.Kind == "" {
			i.Scope.Kind = ScopeGlobal
		}
		return nil
	case scopeBusiness == "" && scopeDivision == "":
		// Flat fields drive: hydrate the scope from them.
		i.Scope.BusinessID = flatBusiness
		i.Scope.DivisionID = flatDivision
		if i.Scope.Kind == "" {
			if flatDivision != "" {
				i.Scope.Kind = ScopeDivision
			} else if flatBusiness != "" {
				i.Scope.Kind = ScopeBusiness
			}
		}
		return nil
	}
	// Both sides populated: they must agree exactly.
	if flatBusiness != scopeBusiness || flatDivision != scopeDivision {
		return nerrors.Validation("identity.scope_conflict",
			"business_id/division_id contradict the scope object")
	}
	return nil
}

func (i Identity) isGlobalType() bool {
	switch i.Type {
	case TypeSystem, TypeBusiness, TypeService, TypeProvider, TypeModel, TypeConnector:
		// service/provider/model/connector may be global OR business-scoped;
		// only system/business are strictly global. Keep the strict set small.
		return i.Type == TypeSystem || i.Type == TypeBusiness
	default:
		return false
	}
}

func (i Identity) isBusinessScopedType() bool {
	switch i.Type {
	case TypeDivision, TypeAgent, TypeWorkflow, TypeTask, TypeDevice:
		return true
	default:
		return false
	}
}

// validateMetadata enforces the metadata extension rules (SCHEMA_COMMON §11)
// via the shared map validator.
func (i Identity) validateMetadata() error {
	return validateMetadataMap(i.Metadata)
}

// validateMetadataMap enforces the metadata extension rules (SCHEMA_COMMON
// §11) on any entity record: bounded entries, bounded key/value sizes, and
// rejection of secret-bearing keys so metadata can never smuggle a secret.
func validateMetadataMap(m map[string]string) error {
	if len(m) > 50 {
		return nerrors.Validation("identity.metadata_too_many", "metadata exceeds 50 entries")
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if len(k) > 128 {
			return nerrors.Validation("identity.metadata_key_too_long", "metadata key exceeds 128 characters")
		}
		if security.IsSecretKey(k) {
			return nerrors.Validation("identity.metadata_secret_key",
				"metadata must not carry secret-bearing keys")
		}
		if len(m[k]) > 1024 {
			return nerrors.Validation("identity.metadata_value_too_long", "metadata value exceeds 1024 characters")
		}
	}
	return nil
}

// SameBusinessAs reports whether two identities share a business scope. Two
// global identities are considered to share the (empty) global business. This is
// a scope observable, NOT an authorization decision.
func (i Identity) SameBusinessAs(other Identity) bool {
	return i.Scope.BusinessID == other.Scope.BusinessID
}

// String renders a safe summary; never includes secrets.
func (i Identity) String() string {
	return fmt.Sprintf("identity{id=%s type=%s status=%s scope=%s}", i.ID, i.Type, i.Status, i.Scope)
}

// New creates a validated identity with a generated ID. It is the safe default
// constructor: it never accepts authority fields because none exist. It stamps
// the common envelope (schema_version, entity_type, created_at, provenance)
// required by SCHEMA_IDENTITIES_ORG §2.2.
func New(nexusID string, t Type, displayName string, scope Scope) (Identity, error) {
	id, err := security.NewID(string(t))
	if err != nil {
		return Identity{}, err
	}
	now := time.Now().UTC()
	ident := Identity{
		SchemaVersion: schema.Version,
		EntityType:    entityTypeIdentity,
		ID:            id,
		NexusID:       nexusID,
		Type:          t,
		DisplayName:   displayName,
		Status:        StatusActive,
		CreatedAt:     now,
		Scope:         scope,
		Provenance: schema.ProvenanceRef{
			Origin:     "system",
			Producer:   "identity.New",
			ProducedAt: now,
		},
	}
	if err := ident.Validate(); err != nil {
		return Identity{}, err
	}
	return ident, nil
}
