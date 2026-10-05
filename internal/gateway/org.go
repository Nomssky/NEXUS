package gateway

// Organization entity surface (contracts/SCHEMA_IDENTITIES_ORG §2–§4):
// CRUD + lifecycle transitions for Identity, Business and Division records
// backed by identity.Registry.
//
// Scope posture:
//   - Scoped paths: identities/businesses/divisions are in isScopedAPIPath, so
//     identityMiddleware authenticates them when enforcement is on (A6).
//   - List endpoints are fail-closed on business_id (identities, divisions)
//     like every other list surface; the business list is membership-filtered
//     when enforcement is on (a non-member sees no businesses).
//   - Record creation with a business scope requires membership in it
//     (MEMBERSHIP != AUTHORITY — membership is the boundary, not a grant).
//     Business onboarding is bootstrap: authenticated (when enforced) but
//     membership-free, since the business does not exist yet.
//   - Reads/transitions of foreign-scope records answer 404 (no cross-tenant
//     existence leak), 403 only where an explicit scope query is the contract.
//   - Audit (§9): creates/status changes emit events via the registry's
//     publisher, bound to the engine bus in NewServer.

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// credentialRegistrar is the LocalAuthenticator surface needed to register a
// credential during identity creation (one-step onboarding).
type credentialRegistrar interface {
	Register(identityID string, credentialHash string, method identity.AuthMethod) error
}

// WithRegistry wires the organization entity registry. A nil registry makes
// every org endpoint fail closed (503) — records are never fabricated.
func WithRegistry(r *identity.Registry) ServerOption {
	return func(s *Server) { s.registry = r }
}

// requireRegistry fails closed when the registry is not wired.
func (s *Server) requireRegistry(w http.ResponseWriter, r *http.Request) bool {
	if s.registry == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"organization registry not configured")
		return false
	}
	return true
}

// writeRegistryError maps registry results to the HTTP error contract:
// 404 unknown record, 409 duplicate/invalid transition, 400 validation,
// 500 everything else.
func (s *Server) writeRegistryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrIdentityNotFound),
		errors.Is(err, identity.ErrBusinessNotFound),
		errors.Is(err, identity.ErrDivisionNotFound):
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", err.Error())
	case errors.Is(err, identity.ErrDuplicateEntity),
		errors.Is(err, identity.ErrInvalidTransition):
		s.writeError(w, r, http.StatusConflict, "CONFLICT", err.Error())
	default:
		if nerrors.CategoryOf(err) == nerrors.CategoryValidation {
			s.writeError(w, r, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		s.writeInternal(w, r, http.StatusInternalServerError, "INTERNAL_FAILURE",
			"registry write failed", err)
	}
}

// ---------- Division scope on submit ----------

// Division-scope sentinels for a submitted division_id. They are never
// returned to clients; writeDivisionScopeError maps them to the HTTP contract.
var (
	// errDivisionUnavailable: a division was claimed but no registry exists to
	// verify it — fail closed rather than admit an unverified scope (RT-02).
	errDivisionUnavailable = errors.New("division scope unavailable")
	// errDivisionNotFound: SCHEMA_IDENTITIES_ORG §8 — the division id does not
	// exist.
	errDivisionNotFound = errors.New("division not found")
	// errDivisionMismatch: SCHEMA_IDENTITIES_ORG §8 — the division belongs to
	// a different business than the one claimed on the request.
	errDivisionMismatch = errors.New("division does not belong to the requested business")
)

// checkDivisionScope validates a submitted division_id against §8: it must
// exist and its parent business must equal the request's business_id. It is
// input validation only — membership narrowing is the caller's step.
func (s *Server) checkDivisionScope(businessID, divisionID string) error {
	if s.registry == nil {
		return errDivisionUnavailable
	}
	div, ok := s.registry.GetDivision(divisionID)
	if !ok {
		return errDivisionNotFound
	}
	if div.BusinessID != businessID {
		return errDivisionMismatch
	}
	return nil
}

// writeDivisionScopeError maps a checkDivisionScope result to the error
// envelope: 503 when the registry is missing, 400 for an unknown division or
// one owned by a different business.
func (s *Server) writeDivisionScopeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errDivisionUnavailable):
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"organization registry not configured")
	case errors.Is(err, errDivisionMismatch):
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", errDivisionMismatch.Error())
	default:
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", errDivisionNotFound.Error())
	}
}

// orgActor resolves the audit actor for a mutation (G-009 posture: only a
// verified identity is attributed; otherwise the fixed marker is bound).
func orgActor(res identity.AuthResult) string {
	if res.IdentityID != "" {
		return res.IdentityID
	}
	return unauthenticatedActorID
}

// currentActor resolves the actor from the request context.
func currentActor(r *http.Request) string {
	if res, ok := actorFromContext(r.Context()); ok {
		return orgActor(res)
	}
	return unauthenticatedActorID
}

// gatewayProvenanceRef stamps SCHEMA_COMMON §4 provenance for a record
// created through the HTTP surface.
func gatewayProvenanceRef(now time.Time) schema.ProvenanceRef {
	return schema.ProvenanceRef{
		Origin:     "human",
		Producer:   "gateway:http-api",
		ProducedAt: now,
	}
}

// orgScopeVisible reports whether the caller may see a record of the given
// business scope. Global records (empty business) are visible whenever the
// request got this far (scoped paths authenticate when enforcement is on).
// It never writes — callers decide between 404 (hidden) and an explicit
// scope error.
func (s *Server) orgScopeVisible(r *http.Request, businessID string) bool {
	if businessID == "" || !s.identityEnforced() {
		return true
	}
	res, ok := actorFromContext(r.Context())
	if !ok {
		return false
	}
	if s.memberships == nil {
		return false
	}
	// G3: org records are business-level resources — only a business-wide
	// membership makes them visible (a division membership is narrower).
	return s.memberships.AllowsScope(res.IdentityID, businessID, "")
}

// ---------- Identity ----------

// createIdentityBody is the POST /api/v1/identities input. An optional
// credential registers the authentication material in the same step
// (one-step onboarding); it is never echoed back.
type createIdentityBody struct {
	EntityID         string            `json:"entity_id,omitempty"`
	IdentityType     string            `json:"identity_type"`
	DisplayName      string            `json:"display_name"`
	Status           string            `json:"status,omitempty"`
	BusinessID       string            `json:"business_id,omitempty"`
	DivisionID       string            `json:"division_id,omitempty"`
	ParentID         string            `json:"parent_id,omitempty"`
	ExpiresAt        *time.Time        `json:"expires_at,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Role             string            `json:"role,omitempty"`
	Credential       string            `json:"credential,omitempty"`
	CredentialMethod string            `json:"credential_method,omitempty"`
}

// handleCreateIdentity creates an identity record (§2.2), optionally
// registering a credential and an active membership for its business.
func (s *Server) handleCreateIdentity(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w, r) {
		return
	}
	var body createIdentityBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}

	// Bootstrap restriction: system identities are created by the runtime,
	// not through the public surface.
	if body.IdentityType == string(identity.TypeSystem) {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION",
			"system identities are created by the runtime, not via the API")
		return
	}
	status := identity.StatusActive
	switch body.Status {
	case "":
	case string(identity.StatusActive), string(identity.StatusPending):
		status = identity.Status(body.Status)
	default:
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION",
			"status must be active or pending at creation")
		return
	}

	// Membership boundary (A6): creating an identity inside a business
	// requires being a member of that business.
	if body.BusinessID != "" {
		if _, stopped := s.requireBusinessWideMembership(w, r, body.BusinessID); stopped {
			return
		}
	}
	actor := currentActor(r)

	// Pre-generate the ID so membership/credential registration bind to the
	// same identifier the registry will store.
	id := body.EntityID
	if id == "" {
		generated, err := security.NewID(body.IdentityType)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid identity_type")
			return
		}
		id = generated
	}

	now := s.now().UTC()
	scope := identity.GlobalScope()
	if body.BusinessID != "" {
		if body.DivisionID != "" {
			scope = identity.DivisionScope(body.BusinessID, body.DivisionID)
		} else {
			scope = identity.BusinessScope(body.BusinessID)
		}
	}
	ident := identity.Identity{
		ID:          id,
		Type:        identity.Type(body.IdentityType),
		DisplayName: body.DisplayName,
		Status:      status,
		CreatedAt:   now,
		ExpiresAt:   body.ExpiresAt,
		BusinessID:  body.BusinessID,
		DivisionID:  body.DivisionID,
		Scope:       scope,
		ParentID:    body.ParentID,
		Metadata:    body.Metadata,
		Provenance:  gatewayProvenanceRef(now),
	}
	if ident.Type == "" || !ident.Type.IsValid() {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION",
			"identity_type is required and must be a canonical type")
		return
	}

	// Pre-validate the membership so no record exists if the role is bad.
	role := identity.RoleMember
	if body.Role != "" {
		role = identity.Role(body.Role)
	}
	member := identity.Membership{
		IdentityID: id,
		BusinessID: body.BusinessID,
		DivisionID: body.DivisionID,
		Role:       role,
		Status:     identity.StatusActive,
	}
	if body.BusinessID != "" {
		if err := member.Validate(); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
	}

	created, err := s.registry.CreateIdentity(ident, actor)
	if err != nil {
		s.writeRegistryError(w, r, err)
		return
	}

	// Optional credential registration; roll the record back if it fails so
	// no identity exists without the credential the caller asked to set.
	if body.Credential != "" {
		reg, ok := s.authenticator.(credentialRegistrar)
		if !ok || s.authenticator == nil {
			s.registry.RemoveIdentity(created.ID)
			s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
				"authenticator does not support credential registration")
			return
		}
		method := identity.AuthMethodToken
		if body.CredentialMethod != "" {
			switch body.CredentialMethod {
			case string(identity.AuthMethodToken),
				string(identity.AuthMethodPassword),
				string(identity.AuthMethodService),
				string(identity.AuthMethodDevice):
				method = identity.AuthMethod(body.CredentialMethod)
			default:
				s.registry.RemoveIdentity(created.ID)
				s.writeError(w, r, http.StatusBadRequest, "VALIDATION",
					"credential_method must be token, password, service or device")
				return
			}
		}
		if err := reg.Register(created.ID, security.HashCredential([]byte(body.Credential)), method); err != nil {
			s.registry.RemoveIdentity(created.ID)
			s.writeRegistryError(w, r, err)
			return
		}
	}

	// Convenience membership: the created identity joins its business with a
	// descriptive role (MEMBERSHIP != AUTHORITY — no permission is granted).
	if body.BusinessID != "" && s.memberships != nil {
		if err := s.memberships.Add(member); err != nil {
			s.writeInternal(w, r, http.StatusInternalServerError, "INTERNAL_FAILURE",
				"membership add failed", err)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(created)
}

// handleListIdentities lists a business's identities. business_id is
// mandatory (fail closed — no unscoped listing).
func (s *Server) handleListIdentities(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w, r) {
		return
	}
	businessID := r.URL.Query().Get("business_id")
	if businessID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}
	if _, stopped := s.requireBusinessWideMembership(w, r, businessID); stopped {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"identities": s.registry.ListIdentities(businessID),
	})
}

// handleGetIdentity returns one identity. A foreign business scope answers
// the same 404 as an unknown id (no cross-tenant existence leak).
func (s *Server) handleGetIdentity(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w, r) {
		return
	}
	id := r.PathValue("id")
	ident, ok := s.registry.GetIdentity(id)
	if !ok || !s.orgScopeVisible(r, ident.BusinessID) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "identity not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ident)
}

// handleIdentityTransition applies suspend/revoke/activate (audit §9:
// identity suspended/revoked).
func (s *Server) handleIdentityTransition(w http.ResponseWriter, r *http.Request, to identity.Status) {
	if !s.requireRegistry(w, r) {
		return
	}
	id := r.PathValue("id")
	ident, ok := s.registry.GetIdentity(id)
	if !ok || !s.orgScopeVisible(r, ident.BusinessID) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "identity not found")
		return
	}
	updated, err := s.registry.SetIdentityStatus(id, to, currentActor(r))
	if err != nil {
		s.writeRegistryError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"identity_id": updated.ID,
		"status":      string(updated.Status),
	})
}

// ---------- Business ----------

// createBusinessBody is the POST /api/v1/businesses input (§3.1).
type createBusinessBody struct {
	EntityID        string                     `json:"entity_id,omitempty"`
	Name            string                     `json:"name"`
	OwnerIdentityID string                     `json:"owner_identity_id"`
	Settings        *identity.BusinessSettings `json:"settings,omitempty"`
	Metadata        map[string]string          `json:"metadata,omitempty"`
}

// handleCreateBusiness onboards a business (§9: business onboarded audit).
// Bootstrap: authenticated when enforcement is on, membership-free (the
// business does not exist yet).
func (s *Server) handleCreateBusiness(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w, r) {
		return
	}
	var body createBusinessBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}

	b := identity.Business{
		EntityID:        body.EntityID,
		Name:            body.Name,
		OwnerIdentityID: body.OwnerIdentityID,
		Settings:        body.Settings,
		Metadata:        body.Metadata,
		Provenance:      gatewayProvenanceRef(s.now().UTC()),
	}
	created, err := s.registry.CreateBusiness(b, currentActor(r))
	if err != nil {
		s.writeRegistryError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(created)
}

// handleListBusinesses lists businesses. With enforcement on the result is
// filtered to the actor's memberships — a non-member sees no businesses
// (G-002: no cross-tenant business directory).
func (s *Server) handleListBusinesses(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w, r) {
		return
	}
	all := s.registry.ListBusinesses()
	if !s.identityEnforced() {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"businesses": all})
		return
	}
	res, ok := actorFromContext(r.Context())
	if !ok {
		s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	visible := make([]identity.Business, 0, len(all))
	for _, b := range all {
		if s.memberships != nil && s.memberships.AllowsScope(res.IdentityID, b.EntityID, "") {
			visible = append(visible, b)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"businesses": visible})
}

// handleGetBusiness returns one business to a member of it (or anyone when
// enforcement is off). Foreign scope answers 404 (hidden).
func (s *Server) handleGetBusiness(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w, r) {
		return
	}
	id := r.PathValue("id")
	b, ok := s.registry.GetBusiness(id)
	if !ok || !s.orgScopeVisible(r, b.EntityID) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "business not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(b)
}

// handleBusinessTransition applies suspend/archive/activate to a business.
func (s *Server) handleBusinessTransition(w http.ResponseWriter, r *http.Request, to identity.BusinessStatus) {
	if !s.requireRegistry(w, r) {
		return
	}
	id := r.PathValue("id")
	b, ok := s.registry.GetBusiness(id)
	if !ok || !s.orgScopeVisible(r, b.EntityID) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "business not found")
		return
	}
	updated, err := s.registry.SetBusinessStatus(id, to, currentActor(r))
	if err != nil {
		s.writeRegistryError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"business_id": updated.BusinessID,
		"status":      string(updated.Status),
	})
}

// ---------- Division ----------

// createDivisionBody is the POST /api/v1/divisions input (§4.1).
type createDivisionBody struct {
	EntityID         string                     `json:"entity_id,omitempty"`
	BusinessID       string                     `json:"business_id"`
	Name             string                     `json:"name"`
	OwnerIdentityID  string                     `json:"owner_identity_id"`
	ParentBusinessID string                     `json:"parent_business_id,omitempty"`
	Settings         *identity.DivisionSettings `json:"settings,omitempty"`
	Metadata         map[string]string          `json:"metadata,omitempty"`
}

// handleCreateDivision creates a division of the caller's business (§4).
func (s *Server) handleCreateDivision(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w, r) {
		return
	}
	var body createDivisionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}
	if body.BusinessID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}
	if _, stopped := s.requireBusinessWideMembership(w, r, body.BusinessID); stopped {
		return
	}

	d := identity.Division{
		EntityID:         body.EntityID,
		BusinessID:       body.BusinessID,
		Name:             body.Name,
		OwnerIdentityID:  body.OwnerIdentityID,
		ParentBusinessID: body.ParentBusinessID,
		Settings:         body.Settings,
		Metadata:         body.Metadata,
		Provenance:       gatewayProvenanceRef(s.now().UTC()),
	}
	created, err := s.registry.CreateDivision(d, currentActor(r))
	if err != nil {
		s.writeRegistryError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(created)
}

// handleListDivisions lists a business's divisions (fail-closed business_id).
func (s *Server) handleListDivisions(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w, r) {
		return
	}
	businessID := r.URL.Query().Get("business_id")
	if businessID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}
	if _, stopped := s.requireBusinessWideMembership(w, r, businessID); stopped {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"divisions": s.registry.ListDivisions(businessID),
	})
}

// handleGetDivision returns one division to a member of its business.
// Foreign scope answers 404 (hidden).
func (s *Server) handleGetDivision(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w, r) {
		return
	}
	id := r.PathValue("id")
	d, ok := s.registry.GetDivision(id)
	if !ok || !s.orgScopeVisible(r, d.BusinessID) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "division not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(d)
}

// handleDivisionTransition applies suspend/archive/activate to a division.
func (s *Server) handleDivisionTransition(w http.ResponseWriter, r *http.Request, to identity.DivisionStatus) {
	if !s.requireRegistry(w, r) {
		return
	}
	id := r.PathValue("id")
	d, ok := s.registry.GetDivision(id)
	if !ok || !s.orgScopeVisible(r, d.BusinessID) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "division not found")
		return
	}
	updated, err := s.registry.SetDivisionStatus(id, to, currentActor(r))
	if err != nil {
		s.writeRegistryError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"division_id": updated.EntityID,
		"status":      string(updated.Status),
	})
}
