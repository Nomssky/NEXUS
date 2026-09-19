// Package identity — capability and permission model.
//
// The locked vocabulary keeps five concepts distinct:
//
//	IDENTITY   — who you are            (see Identity)
//	AUTHORITY  — what you may do        (Governance / C03, not here)
//	CAPABILITY — what you technically can do (see Capability)
//	PERMISSION — what you are granted    (see Permission)
//	TRUST      — contextual confidence   (not modeled at M1)
//
// This file models the minimal CAPABILITY and PERMISSION representations M1
// needs, and — critically — enforces that they are SEPARATE types with SEPARATE
// sets, so that having a capability can never be mistaken for having permission.
//
// INV-05: Authority != Capability. INV-06: Capability != Permission.
package identity

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// CapabilityType categorizes a capability, mirroring the contract CapabilityRef
// (contracts/SCHEMA_IDENTITIES_ORG.md §5.2).
type CapabilityType string

const (
	CapabilityTool   CapabilityType = "tool"
	CapabilityModel  CapabilityType = "model"
	CapabilityDomain CapabilityType = "domain"
	CapabilityCustom CapabilityType = "custom"
)

var validCapabilityTypes = map[CapabilityType]struct{}{
	CapabilityTool: {}, CapabilityModel: {}, CapabilityDomain: {}, CapabilityCustom: {},
}

// IsValid reports whether t is a valid capability type.
func (t CapabilityType) IsValid() bool { _, ok := validCapabilityTypes[t]; return ok }

// CapabilityRef is a *reference* to a defined capability: what an actor or
// component is technically ABLE to perform. It is NOT a grant.
//
// CRITICAL: possessing a capability does NOT mean you are authorized to use it.
type CapabilityRef struct {
	ID   string         `json:"capability_id"`
	Type CapabilityType `json:"capability_type"`
	// Scope optionally narrows the capability to a scope (e.g. a business).
	Scope string `json:"scope,omitempty"`
}

// Validate checks a capability reference.
func (c CapabilityRef) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return nerrors.Validation("identity.capability_id_required", "capability_id is required")
	}
	if !c.Type.IsValid() {
		return nerrors.Validation("identity.capability_type_invalid",
			fmt.Sprintf("capability_type %q is not valid", c.Type))
	}
	return nil
}

// CapabilitySet is an immutable-by-convention set of capabilities. It provides no
// authorization: callers may ask "is this capable?" but must never treat a true
// answer as permission.
type CapabilitySet struct {
	items map[string]CapabilityRef
}

// NewCapabilitySet builds a capability set from references, validating each.
func NewCapabilitySet(caps ...CapabilityRef) (*CapabilitySet, error) {
	s := &CapabilitySet{items: map[string]CapabilityRef{}}
	for _, c := range caps {
		if err := c.Validate(); err != nil {
			return nil, err
		}
		s.items[c.ID] = c
	}
	return s, nil
}

// Has reports whether a capability reference is present. A true result means the
// actor is technically able; it is NOT permission to act.
func (s *CapabilitySet) Has(id string) bool {
	if s == nil {
		return false
	}
	_, ok := s.items[id]
	return ok
}

// All returns the capabilities, sorted deterministically.
func (s *CapabilitySet) All() []CapabilityRef {
	if s == nil {
		return nil
	}
	out := make([]CapabilityRef, 0, len(s.items))
	for _, c := range s.items {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Permission is a specific GRANT to perform an action on a resource. It is
// resolved by Governance; M1 carries the minimal representation so the boundary
// is concrete and testable.
//
// A Permission is NOT a capability (it says "you MAY", not "you CAN") and NOT
// authority (authority is the resolved decision; a permission is an input).
type Permission struct {
	// ID identifies this grant (e.g. "perm:publish:instagram").
	ID string `json:"permission_id"`
	// Action is the action the grant permits (e.g. "execute_tool:publish").
	Action string `json:"action"`
	// Resource is the resource the action applies to (e.g. "tool:instagram").
	Resource string `json:"resource"`
	// Scope narrows the grant; empty means global (which is rare and must be
	// explicit upstream).
	Scope Scope `json:"scope"`
}

// Validate checks a permission grant.
func (p Permission) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return nerrors.Validation("identity.permission_id_required", "permission_id is required")
	}
	if strings.TrimSpace(p.Action) == "" {
		return nerrors.Validation("identity.permission_action_required", "permission action is required")
	}
	if strings.TrimSpace(p.Resource) == "" {
		return nerrors.Validation("identity.permission_resource_required", "permission resource is required")
	}
	return p.Scope.Validate()
}

// PermissionSet is a set of grants. It answers only "is this action/resource
// granted within this scope?" and never fabricates a grant from a capability.
type PermissionSet struct {
	items []Permission
}

// NewPermissionSet builds a permission set, validating each grant.
func NewPermissionSet(perms ...Permission) (*PermissionSet, error) {
	s := &PermissionSet{}
	for _, p := range perms {
		if err := p.Validate(); err != nil {
			return nil, err
		}
		s.items = append(s.items, p)
	}
	return s, nil
}

// Has reports whether a grant exists for action/resource within the given scope.
// The grant's scope must cover the requested scope (business isolation enforced).
func (s *PermissionSet) Has(action, resource string, scope Scope) bool {
	if s == nil {
		return false
	}
	for _, p := range s.items {
		if p.Action != action || p.Resource != resource {
			continue
		}
		// A permission whose scope does not cover the request is not granted.
		if p.Scope.Kind == ScopeGlobal || p.Scope.Covers(scope) {
			return true
		}
	}
	return false
}

// All returns the grants, sorted deterministically.
func (s *PermissionSet) All() []Permission {
	if s == nil {
		return nil
	}
	out := append([]Permission(nil), s.items...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
