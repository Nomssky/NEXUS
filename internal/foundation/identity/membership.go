// Package identity — membership model.
//
// Membership answers "which businesses/divisions does this identity belong to?".
// It explicitly does NOT grant authority: being a member of Business A does not
// make an identity an owner, admin, or operator, and it certainly does not grant
// permissions (MEMBERSHIP != AUTHORITY). Roles recorded here are descriptive
// labels, not authorities; authorization is resolved by Governance (C03).
//
// One NEXUS -> many businesses: an identity may hold memberships in several
// businesses. The *active* business is never process-global state; it is carried
// per request/session/execution context (see security.SecurityContext and the
// Context type below).
package identity

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// Role is a descriptive membership label. It is NOT authority and NOT a
// permission. Any authorization that references a role must still be decided by
// Governance.
type Role string

const (
	RoleOwner      Role = "OWNER"
	RoleAdmin      Role = "ADMIN"
	RoleOperator   Role = "OPERATOR"
	RoleReviewer   Role = "REVIEWER"
	RoleAgentMgr   Role = "AGENT_MANAGER"
	RoleMediaMgr   Role = "MEDIA_MANAGER"
	RoleResearcher Role = "RESEARCHER"
	RoleWorker     Role = "WORKER"
	RoleViewer     Role = "VIEWER"
	RoleMember     Role = "MEMBER"
)

// Membership binds an identity to a business (and optionally a single division)
// with a descriptive role.
//
// INV-01: a membership in Business A never grants access to Business B. The role
// is descriptive only and grants no authority by itself.
type Membership struct {
	IdentityID string `json:"identity_id"`
	BusinessID string `json:"business_id"`
	// DivisionID, when set, narrows membership to one division. Empty = the whole
	// business.
	DivisionID string `json:"division_id,omitempty"`
	// Role is a descriptive label, never authority.
	Role Role `json:"role"`
	// Status reflects the membership's lifecycle.
	Status Status `json:"status"`
}

// Validate checks membership consistency and rejects malformed entries.
func (m Membership) Validate() error {
	if strings.TrimSpace(m.IdentityID) == "" {
		return nerrors.Validation("identity.membership_identity_required", "membership identity_id is required")
	}
	if strings.TrimSpace(m.BusinessID) == "" {
		return nerrors.Validation("identity.membership_business_required", "membership business_id is required")
	}
	if !m.Status.IsValid() {
		return nerrors.Validation("identity.membership_status_invalid",
			fmt.Sprintf("membership status %q is not valid", m.Status))
	}
	if strings.TrimSpace(string(m.Role)) == "" {
		return nerrors.Validation("identity.membership_role_required", "membership role is required")
	}
	return nil
}

// MembershipSet is a set of memberships for identities. It supports the
// "one NEXUS -> many businesses" model and enforces cross-business isolation
// when resolving the active business context.
type MembershipSet struct {
	mu sync.RWMutex
	// byIdentity groups an identity's memberships. Guarded by mu: the gateway
	// reads it from every scoped request while the org API writes to it, and
	// Add now performs a read-modify-write of the whole group.
	byIdentity map[string][]Membership
	// st is the optional write-through store (nil = in-memory only).
	st store.Store
}

// NewMembershipSet constructs an empty, in-memory-only membership set.
func NewMembershipSet() *MembershipSet {
	return &MembershipSet{byIdentity: map[string][]Membership{}}
}

// membershipRecord is the stored form of one identity's memberships
// (SCHEMA_IDENTITIES_ORG §10). One record per identity keeps the record id
// equal to the identity id, which is already a valid store record id.
type membershipRecord struct {
	SchemaVersion string       `json:"schema_version"`
	EntityType    string       `json:"entity_type"`
	IdentityID    string       `json:"identity_id"`
	Memberships   []Membership `json:"memberships"`
}

// OpenMembershipSet constructs a write-through membership set: every Add is
// persisted to st and the stored memberships are hydrated before
// OpenMembershipSet returns. Hydration fails closed — a record that cannot be
// decoded or validated aborts the call so boot never continues on a partially
// restored membership set. A nil st degrades to NewMembershipSet.
func OpenMembershipSet(st store.Store) (*MembershipSet, error) {
	s := NewMembershipSet()
	if st == nil {
		return s, nil
	}
	s.st = st
	if err := s.hydrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// hydrate loads every stored membership record into memory. Soft-deleted
// records are already excluded by the store's List. Call with a fresh set.
func (s *MembershipSet) hydrate() error {
	records, err := s.st.List(store.Filter{Type: store.RecordTypeMembership})
	if err != nil {
		return fmt.Errorf("memberships: hydrate: %w", err)
	}
	for _, rec := range records {
		var mr membershipRecord
		if err := json.Unmarshal(rec.Data, &mr); err != nil {
			return fmt.Errorf("memberships: corrupt record %q: %w", rec.ID, err)
		}
		if membershipRecordID(mr.IdentityID) != rec.ID {
			return fmt.Errorf("memberships: record %q carries identity_id %q", rec.ID, mr.IdentityID)
		}
		for _, m := range mr.Memberships {
			if m.IdentityID != mr.IdentityID {
				return fmt.Errorf("memberships: record %q holds a membership for %q", rec.ID, m.IdentityID)
			}
			if err := m.Validate(); err != nil {
				return fmt.Errorf("memberships: invalid record %q: %w", rec.ID, err)
			}
		}
		s.byIdentity[mr.IdentityID] = append(s.byIdentity[mr.IdentityID], mr.Memberships...)
	}
	return nil
}

// Add records a membership after validating it. Duplicate (identity, business,
// division) entries are ignored idempotently.
//
// Write-through order matches the registry: persist → memory, so a store
// failure leaves the set untouched (no membership that exists only in memory).
func (s *MembershipSet) Add(m Membership) error {
	if err := m.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.byIdentity[m.IdentityID]
	for _, e := range existing {
		if e.BusinessID == m.BusinessID && e.DivisionID == m.DivisionID {
			return nil
		}
	}
	next := append(append([]Membership(nil), existing...), m)
	if s.st != nil {
		rec, err := marshalRecord(store.RecordTypeMembership, membershipRecordID(m.IdentityID), "", "", membershipRecord{
			SchemaVersion: membershipSchemaVersion,
			EntityType:    entityTypeMembership,
			IdentityID:    m.IdentityID,
			Memberships:   next,
		})
		if err != nil {
			return err
		}
		if err := persistRecord(s.st, rec); err != nil {
			return err
		}
	}
	s.byIdentity[m.IdentityID] = next
	return nil
}

// For returns the memberships of an identity, sorted deterministically.
func (s *MembershipSet) For(identityID string) []Membership {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]Membership(nil), s.byIdentity[identityID]...)
	sort.Slice(out, func(a, b int) bool {
		if out[a].BusinessID != out[b].BusinessID {
			return out[a].BusinessID < out[b].BusinessID
		}
		return out[a].DivisionID < out[b].DivisionID
	})
	return out
}

// IsMember reports whether an identity has an active membership in a business
// (and, if divisionID is non-empty, a membership covering that division).
//
// This is a membership *observable*, not an authorization decision. It answers
// "are you inside this boundary?" not "may you perform this action?".
func (s *MembershipSet) IsMember(identityID, businessID, divisionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.byIdentity[identityID] {
		if m.Status != StatusActive || m.BusinessID != businessID {
			continue
		}
		if divisionID == "" || m.DivisionID == "" || m.DivisionID == divisionID {
			return true
		}
	}
	return false
}

// CurrentBusiness resolves the active business context for an identity and
// verifies the identity is a member of it. It fails closed when the identity is
// not a member — an identity cannot silently act for a business it does not
// belong to.
//
// The requested business is passed in explicitly (from the request/session/
// execution context); it is NEVER read from process-global state.
func (s *MembershipSet) CurrentBusiness(identityID, businessID, divisionID string) (Membership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if strings.TrimSpace(businessID) == "" {
		return Membership{}, nerrors.Validation("identity.business_context_required",
			"an explicit business context is required")
	}
	for _, m := range s.byIdentity[identityID] {
		if m.Status != StatusActive || m.BusinessID != businessID {
			continue
		}
		if m.DivisionID != "" && divisionID != "" && m.DivisionID != divisionID {
			continue
		}
		return m, nil
	}
	return Membership{}, nerrors.New("identity.not_a_member", nerrors.CategoryAuthorization,
		"identity is not an active member of the requested business/division")
}
