package gateway

// Policy control surface (contracts/SCHEMA_GOVERNANCE_ATTENTION.md §9):
// the read/write HTTP face of the governance engine's policy set.
//
// Posture:
//   - These are control-plane paths: X-API-Key via authMiddleware, exactly
//     like every other /api/v1/control/* route. There is no identity
//     middleware, no membership check and no second authorization system —
//     the control key is the whole authority (§9.2).
//   - The record on the wire is the §2 Policy Record verbatim; the gateway
//     only defaults the server-derivable §2.2 fields and rejects everything
//     the contract marks required and does not derive (§9.3).
//   - The seeded default-allow built-in is read-only (§9.4): GET returns it,
//     PUT/DELETE answer 409 so an unconfigured installation can never be
//     flipped to default-DENY through the API.
//   - A write is observable by reading it back and by the decision reason
//     naming the matched policy_id; no new audit event is emitted, because
//     SCHEMA_GOVERNANCE §6 does not define a policy-mutation event and
//     inventing one would add vocabulary the contract does not carry.
//   - Policies are process-lifetime state — no store record type exists, so
//     nothing is written to disk (§9.6).

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
)

// controlActorID attributes a policy written through the control API when the
// caller does not name a creator. The control key authenticates the call, not
// an identity, so no identity-derived actor exists here.
const controlActorID = "control-api"

// handleListPolicies returns the full effective policy set, including the
// seeded built-in. Sorted by policy_id so the surface is stable to diff; the
// sort runs on the copy Policies() already returns, never on the engine's own
// slice (evaluation breaks full ties by input order).
func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	policies := s.engine.Governance().Policies()
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].PolicyID < policies[j].PolicyID
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"policies": policies,
	})
}

// handleGetPolicy returns one policy record.
func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	p, ok := s.findPolicy(r.PathValue("id"))
	if !ok {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "policy not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

// handlePutPolicy creates or replaces one policy (upsert on policy_id).
func (s *Server) handlePutPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body governance.Policy
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}
	if body.PolicyID != "" && body.PolicyID != id {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION",
			"policy_id in the body does not match the path")
		return
	}
	if id == governance.DefaultAllowPolicyID {
		s.writeError(w, r, http.StatusConflict, "CONFLICT",
			"built-in policy is read-only")
		return
	}
	body.PolicyID = id

	now := s.now().UTC()
	previous, exists := s.findPolicy(id)
	s.applyPolicyDefaults(&body, now, exists, previous)

	if err := body.Validate(); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}

	s.engine.Governance().PutPolicy(&body)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"policy_id":      body.PolicyID,
		"policy_version": body.PolicyVersion,
		"status":         string(body.Status),
	})
}

// handleDeletePolicy removes one policy. The seeded built-in is protected.
func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == governance.DefaultAllowPolicyID {
		s.writeError(w, r, http.StatusConflict, "CONFLICT",
			"built-in policy is read-only")
		return
	}
	if !s.engine.Governance().RemovePolicy(id) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "policy not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"policy_id": id,
		"deleted":   true,
	})
}

// findPolicy looks up a record by id in the current policy set.
func (s *Server) findPolicy(id string) (*governance.Policy, bool) {
	for _, p := range s.engine.Governance().Policies() {
		if p != nil && p.PolicyID == id {
			return p, true
		}
	}
	return nil, false
}

// applyPolicyDefaults fills the §2.2 fields the contract requires but that
// only the server can derive (§9.3). An update inherits the original
// created_at / created_by — a replace does not rewrite who created a record
// or when — and is stamped with updated_at.
func (s *Server) applyPolicyDefaults(p *governance.Policy, now time.Time, exists bool, previous *governance.Policy) {
	p.EntityType = "policy"
	if p.SchemaVersion == "" {
		p.SchemaVersion = schema.Version
	}
	if p.NexusID == "" {
		p.NexusID = s.nexusID
	}
	if p.PolicyVersion == "" {
		p.PolicyVersion = "1"
	}
	if p.EffectiveFrom.IsZero() {
		p.EffectiveFrom = now
	}
	if !p.Provenance.Valid() {
		p.Provenance = gatewayProvenanceRef(now)
	}
	if exists && previous != nil {
		p.CreatedAt = previous.CreatedAt
		p.CreatedBy = previous.CreatedBy
		updatedAt := now
		p.UpdatedAt = &updatedAt
		return
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.CreatedBy == "" {
		p.CreatedBy = controlActorID
	}
}
