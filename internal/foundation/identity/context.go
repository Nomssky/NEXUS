// Package identity — security context.
//
// SecurityContext is the minimal, validated context that flows with a request,
// session, or execution through the foundation. It binds together:
//
//   - the actor identity (WHO)
//   - authentication state (is the identity authenticated?)
//   - the active scope (WHERE) — the business/division context is explicit here,
//     never process-global
//   - permissions (grants) and capabilities (technical ability), kept SEPARATE
//   - credential *references* (never raw credentials)
//   - correlation_id (tracing only) and data classification
//
// It carries NO authority: possessing a SecurityContext never authorizes an
// action. Authorization is evaluated separately (authorize.go) and, ultimately,
// decided by Governance.
//
// INV-01: the business context is explicit; a context for Business A cannot be
// reused for Business B (TrySwitchBusiness fails closed).
package identity

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// Classification is a data sensitivity label (contracts/SCHEMA_COMMON.md §5.1).
type Classification string

const (
	ClassificationPublic       Classification = "PUBLIC"
	ClassificationInternal     Classification = "INTERNAL"
	ClassificationConfidential Classification = "CONFIDENTIAL"
	ClassificationRestricted   Classification = "RESTRICTED"
)

var validClassifications = map[Classification]struct{}{
	ClassificationPublic: {}, ClassificationInternal: {},
	ClassificationConfidential: {}, ClassificationRestricted: {},
}

// IsValid reports whether c is a canonical classification. The contract default
// is INTERNAL when absent.
func (c Classification) IsValid() bool { _, ok := validClassifications[c]; return ok }

// SecurityContext is a validated execution security context.
type SecurityContext struct {
	// Actor is the acting identity.
	Actor Identity `json:"actor"`
	// AuthState is the authentication result for the actor.
	AuthState AuthResult `json:"auth_state"`

	// BusinessID is the ACTIVE business context. It is explicit and per-context;
	// it is never read from process-global state.
	BusinessID string `json:"business_id,omitempty"`
	// DivisionID is the ACTIVE division context (may be empty).
	DivisionID string `json:"division_id,omitempty"`

	// Permissions are explicit grants. They are SEPARATE from capabilities.
	Permissions *PermissionSet `json:"-"`
	// Capabilities are technical abilities. They are SEPARATE from permissions and
	// grant nothing by themselves.
	Capabilities *CapabilitySet `json:"-"`

	// CredentialRefs are the credential *references* available in this context.
	// Raw credentials are never stored here.
	CredentialRefs []security.SecretRef `json:"credential_refs,omitempty"`

	// CorrelationID is for tracing only; it grants no authority (INV-19).
	CorrelationID string `json:"correlation_id,omitempty"`
	// Classification is the data sensitivity of this context (default INTERNAL).
	Classification Classification `json:"classification"`
}

// NewSecurityContext builds and validates a security context.
func NewSecurityContext(actor Identity, auth AuthResult, businessID, divisionID, correlationID string, classification Classification) (*SecurityContext, error) {
	if classification == "" {
		classification = ClassificationInternal
	}
	sc := &SecurityContext{
		Actor:          actor,
		AuthState:      auth,
		BusinessID:     businessID,
		DivisionID:     divisionID,
		CorrelationID:  correlationID,
		Classification: classification,
	}
	if err := sc.Validate(); err != nil {
		return nil, err
	}
	return sc, nil
}

// Validate checks the security context for structural and scope consistency. It
// rejects malformed contexts (§9, §26 "malformed security context rejected").
func (s *SecurityContext) Validate() error {
	if s == nil {
		return nerrors.Validation("seccontext.nil", "security context is nil")
	}
	if err := s.Actor.Validate(); err != nil {
		return err
	}
	if s.AuthState.Authenticated && s.AuthState.IdentityID != s.Actor.ID {
		return nerrors.Validation("seccontext.auth_mismatch",
			"authenticated identity does not match the context actor")
	}
	if !s.Classification.IsValid() {
		return nerrors.Validation("seccontext.classification_invalid",
			fmt.Sprintf("classification %q is not valid", s.Classification))
	}
	// Business isolation: the context must be internally consistent. A division
	// requires a business.
	if s.DivisionID != "" && s.BusinessID == "" {
		return nerrors.Validation("seccontext.division_without_business",
			"a division context requires a business context")
	}
	// If the actor is business-scoped, the active business must match the actor's
	// own business. An identity for Business A cannot carry a Business B context.
	if s.Actor.Scope.BusinessID != "" && s.BusinessID != "" && s.Actor.Scope.BusinessID != s.BusinessID {
		return nerrors.New("seccontext.business_scope_mismatch", nerrors.CategoryAuthorization,
			"active business context does not match the actor's business scope")
	}
	if s.Actor.Scope.DivisionID != "" && s.DivisionID != "" && s.Actor.Scope.DivisionID != s.DivisionID {
		return nerrors.New("seccontext.division_scope_mismatch", nerrors.CategoryAuthorization,
			"active division context does not match the actor's division scope")
	}
	// Credential references must be scope-consistent with the context.
	for _, ref := range s.CredentialRefs {
		if !ref.Valid() {
			return nerrors.Validation("seccontext.credential_ref_invalid",
				"security context contains an invalid credential reference")
		}
		if !ref.ScopedTo(s.BusinessID, s.DivisionID) {
			return nerrors.New("seccontext.credential_scope_mismatch", nerrors.CategoryAuthorization,
				"credential reference is not scoped to this context's business/division")
		}
	}
	return nil
}

// ActiveScope returns the scope implied by the context's business/division.
func (s *SecurityContext) ActiveScope() Scope {
	switch {
	case s.BusinessID == "":
		return GlobalScope()
	case s.DivisionID == "":
		return BusinessScope(s.BusinessID)
	default:
		return DivisionScope(s.BusinessID, s.DivisionID)
	}
}

// TrySwitchBusiness returns a copy of the context re-scoped to another business,
// but ONLY if the actor is a member of that business (verified by the provided
// membership set). It fails closed otherwise, so a context can never be silently
// re-pointed at a business the actor does not belong to.
//
// This is how the "one NEXUS -> many businesses" model switches business: via an
// explicit, verified context change, never process-global mutable state.
func (s *SecurityContext) TrySwitchBusiness(members *MembershipSet, businessID, divisionID string) (*SecurityContext, error) {
	if s == nil {
		return nil, nerrors.Validation("seccontext.nil", "security context is nil")
	}
	if strings.TrimSpace(businessID) == "" {
		return nil, nerrors.Validation("seccontext.business_required", "target business is required")
	}
	if _, err := members.CurrentBusiness(s.Actor.ID, businessID, divisionID); err != nil {
		return nil, err
	}
	clone := *s
	clone.BusinessID = businessID
	clone.DivisionID = divisionID
	// Credential refs scoped to the previous business no longer apply.
	kept := clone.CredentialRefs[:0:0]
	for _, ref := range clone.CredentialRefs {
		if ref.ScopedTo(businessID, divisionID) {
			kept = append(kept, ref)
		}
	}
	clone.CredentialRefs = kept
	if err := clone.Validate(); err != nil {
		return nil, err
	}
	return &clone, nil
}

// Redacted returns a copy of the context rendered for logs: no credential refs
// are exposed as raw values (they are references only, but we still surface only
// their safe string form), and there are no raw secrets anywhere in the context.
func (s *SecurityContext) Redacted() map[string]any {
	refs := make([]string, 0, len(s.CredentialRefs))
	for _, r := range s.CredentialRefs {
		refs = append(refs, r.String())
	}
	sort.Strings(refs)
	return map[string]any{
		"actor_id":        s.Actor.ID,
		"actor_type":      string(s.Actor.Type),
		"authenticated":   s.AuthState.Authenticated,
		"business_id":     s.BusinessID,
		"division_id":     s.DivisionID,
		"correlation_id":  s.CorrelationID,
		"classification":  string(s.Classification),
		"credential_refs": refs,
	}
}

// AuthExpired reports whether the context's authentication has lapsed as of now.
func (s *SecurityContext) AuthExpired(now time.Time) bool {
	return s.AuthState.Expired(now)
}
