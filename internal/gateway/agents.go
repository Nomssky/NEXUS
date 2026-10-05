package gateway

// Agent definition surface (Agent Execution Layer v1). Register, read, list,
// update, suspend/archive/activate. Scope posture mirrors the org surface:
// business-wide membership required for creation/mutation/listings; record
// reads use the G3 visibility rule (404 for non-visible records).

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
)

// WithAgentExecution wires the agent registry and the execution runtime the
// execution endpoints use. A nil registry makes the agent endpoints answer
// 503 instead of fabricating records.
func WithAgentExecution(reg *agentexec.Registry, rt *agentexec.Runtime) ServerOption {
	return func(s *Server) {
		s.agents = reg
		s.agentRunner = rt
	}
}

func (s *Server) requireAgentRegistry(w http.ResponseWriter, r *http.Request) bool {
	if s.agents == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"agent registry not configured")
		return false
	}
	return true
}

type createAgentBody struct {
	EntityID     string                      `json:"entity_id"`
	Name         string                      `json:"name"`
	Description  string                      `json:"description"`
	BusinessID   string                      `json:"business_id"`
	DivisionID   string                      `json:"division_id,omitempty"`
	Capabilities []string                    `json:"capabilities"`
	AllowedTools []string                    `json:"allowed_tools,omitempty"`
	Model        agentexec.ModelRequirements `json:"model,omitempty"`
	Memory       agentexec.MemoryConfig      `json:"memory,omitempty"`
	Parameters   map[string]string           `json:"parameters,omitempty"`
}

// agentVisible reports visibility of one definition: business-wide for
// divisionless agents; divisible by the same rule for division-carried
// agents. Foreign businesses and unknown ids both answer not-found.
func (s *Server) agentVisible(r *http.Request, d agentexec.Definition) bool {
	if !s.identityEnforced() {
		return true
	}
	res, ok := actorFromContext(r.Context())
	if !ok {
		return false
	}
	if s.memberships == nil {
		return false
	}
	return s.memberships.AllowsScope(res.IdentityID, d.BusinessID, d.DivisionID)
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgentRegistry(w, r) {
		return
	}
	var body createAgentBody
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
	d := agentexec.Definition{
		ID: body.EntityID, Name: body.Name, Description: body.Description,
		BusinessID: body.BusinessID, DivisionID: body.DivisionID,
		Capabilities: body.Capabilities, AllowedTools: body.AllowedTools,
		Model: body.Model, Memory: body.Memory, Parameters: body.Parameters,
		Status: agentexec.AgentActive,
	}
	created, err := s.agents.Create(d, currentActor(r))
	if err != nil {
		s.writeAgentError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(created)
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgentRegistry(w, r) {
		return
	}
	id := r.PathValue("id")
	ex, ok := s.agents.Get(id)
	if !ok || !s.agentVisible(r, ex) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "agent not found")
		return
	}
	if _, stopped := s.requireBusinessWideMembership(w, r, ex.BusinessID); stopped {
		return
	}
	var body createAgentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}
	updated, err := s.agents.Update(id, func(d *agentexec.Definition) error {
		if body.Name != "" {
			d.Name = body.Name
		}
		if body.Description != "" {
			d.Description = body.Description
		}
		if len(body.Capabilities) > 0 {
			d.Capabilities = body.Capabilities
		}
		if len(body.AllowedTools) > 0 {
			d.AllowedTools = body.AllowedTools
		}
		if body.Memory.Mode != "" {
			d.Memory = body.Memory
		}
		return nil
	})
	if err != nil {
		s.writeAgentError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgentRegistry(w, r) {
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
	divisionID := r.URL.Query().Get("division_id")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"agents": s.agents.List(businessID, divisionID)})
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgentRegistry(w, r) {
		return
	}
	id := r.PathValue("id")
	d, ok := s.agents.Get(id)
	if !ok || !s.agentVisible(r, d) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "agent not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(d)
}

func (s *Server) handleAgentTransitionSuspend(w http.ResponseWriter, r *http.Request) {
	s.handleAgentTransition(w, r, agentexec.AgentSuspended)
}

func (s *Server) handleAgentTransitionArchive(w http.ResponseWriter, r *http.Request) {
	s.handleAgentTransition(w, r, agentexec.AgentArchived)
}

func (s *Server) handleAgentTransitionActivate(w http.ResponseWriter, r *http.Request) {
	s.handleAgentTransition(w, r, agentexec.AgentActive)
}

func (s *Server) handleAgentTransition(w http.ResponseWriter, r *http.Request, to agentexec.Status) {
	if !s.requireAgentRegistry(w, r) {
		return
	}
	id := r.PathValue("id")
	d, ok := s.agents.Get(id)
	if !ok || !s.agentVisible(r, d) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "agent not found")
		return
	}
	if _, stopped := s.requireBusinessWideMembership(w, r, d.BusinessID); stopped {
		return
	}
	updated, err := s.agents.SetStatus(id, to, currentActor(r))
	if err != nil {
		s.writeAgentError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"agent_id": updated.ID, "status": string(updated.Status)})
}

func (s *Server) writeAgentError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, agentexec.ErrNotFound):
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "agent not found")
	case errors.Is(err, agentexec.ErrDuplicate):
		s.writeError(w, r, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, agentexec.ErrBadTransition) || strings.HasPrefix(err.Error(), "business ") || strings.HasPrefix(err.Error(), "division ") || strings.Contains(err.Error(), "must be active"):
		s.writeError(w, r, http.StatusConflict, "CONFLICT", err.Error())
	default:
		s.writeAgentValidationError(w, r, err)
	}
}

func (s *Server) writeAgentValidationError(w http.ResponseWriter, r *http.Request, err error) {
	s.writeError(w, r, http.StatusBadRequest, "VALIDATION", err.Error())
}

// requireAgentActorMembership is the execution-path analog of request submit:
// the actor must be inside the scope the execution declares (division_id
// narrows to it; business-scope requires a business-wide membership).
func (s *Server) requireAgentActorMembership(w http.ResponseWriter, r *http.Request, businessID, divisionID string) (identity.AuthResult, bool) {
	if !s.identityEnforced() {
		return identity.AuthResult{}, false
	}
	res, stopped := s.requireActorMembership(w, r, businessID)
	if stopped {
		return res, true
	}
	if res.IdentityID != "" && s.memberships != nil && !s.memberships.AllowsScope(res.IdentityID, businessID, divisionID) {
		if divisionID == "" {
			s.writeError(w, r, http.StatusForbidden, "AUTHORIZATION",
				"access denied: business-scope execution requires a business-wide membership")
			return res, true
		}
		s.writeError(w, r, http.StatusForbidden, "AUTHORIZATION",
			"access denied: actor is not a member of the requested division")
		return res, true
	}
	return res, false
}
