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
	"fmt"
	"sort"
	"strings"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
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
	byIdentity map[string][]Membership
}

// NewMembershipSet constructs an empty membership set.
func NewMembershipSet() *MembershipSet {
	return &MembershipSet{byIdentity: map[string][]Membership{}}
}

// Add records a membership after validating it. Duplicate (identity, business,
// division) entries are ignored idempotently.
func (s *MembershipSet) Add(m Membership) error {
	if err := m.Validate(); err != nil {
		return err
	}
	existing := s.byIdentity[m.IdentityID]
	for _, e := range existing {
		if e.BusinessID == m.BusinessID && e.DivisionID == m.DivisionID {
			return nil
		}
	}
	s.byIdentity[m.IdentityID] = append(existing, m)
	return nil
}

// For returns the memberships of an identity, sorted deterministically.
func (s *MembershipSet) For(identityID string) []Membership {
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
