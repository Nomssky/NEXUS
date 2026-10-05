package gateway

// Agent intelligence layer v1 (AGENT_INTELLIGENCE_CONTRACTS §17): the
// objective surface. An intelligence execution is an ordinary admitted request
// whose handler is the bounded control loop, so identity, scope, governance,
// approval, cancellation, visibility and durability semantics are the same
// ones the rest of the gateway enforces — this file adds no authorization.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Nomssky/NEXUS/internal/agentintel"
	"github.com/Nomssky/NEXUS/internal/core"
)

// WithIntelligence wires the intelligence runtime. A nil runtime makes the
// objective endpoints fail closed (503) instead of fabricating runs.
func WithIntelligence(rt *agentintel.Runtime) ServerOption {
	return func(s *Server) { s.intel = rt }
}

type intelligenceBody struct {
	BusinessID string               `json:"business_id"`
	DivisionID string               `json:"division_id,omitempty"`
	ActorID    string               `json:"actor_id"`
	Objective  agentintel.Objective `json:"objective"`
}

// handleSubmitIntelligence admits an objective execution. Admission mirrors
// POST /api/v1/requests and /api/v1/executions exactly (G2/G3/G5).
func (s *Server) handleSubmitIntelligence(w http.ResponseWriter, r *http.Request) {
	if s.intel == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"agent intelligence runtime not configured")
		return
	}
	var body intelligenceBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}
	if body.BusinessID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}
	if body.ActorID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "actor_id required")
		return
	}
	if err := body.Objective.Validate(); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if s.identityEnforced() {
		res, ok := actorFromContext(r.Context())
		if !ok {
			s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
			return
		}
		if body.ActorID != res.IdentityID {
			s.writeError(w, r, http.StatusForbidden, "AUTHORIZATION",
				"access denied: actor_id does not match authenticated identity")
			return
		}
		if _, stopped := s.requireAgentActorMembership(w, r, body.BusinessID, body.DivisionID); stopped {
			return
		}
	}
	// G2: admission is lifecycle-aware, exactly like every other work surface.
	if s.registry != nil {
		if b, ok := s.registry.GetBusiness(body.BusinessID); ok && b.Status != "active" {
			s.writeError(w, r, http.StatusConflict, "CONFLICT",
				fmt.Sprintf("business %q is %s: no new work is admitted", body.BusinessID, b.Status))
			return
		}
		if body.DivisionID != "" {
			if d, ok := s.registry.GetDivision(body.DivisionID); ok && d.Status != "active" {
				s.writeError(w, r, http.StatusConflict, "CONFLICT",
					fmt.Sprintf("division %q is %s: no new work is admitted", body.DivisionID, d.Status))
				return
			}
		}
	}

	corrID := r.Header.Get("X-Correlation-ID")
	if corrID == "" {
		corrID = fmt.Sprintf("api-%d-int", s.now().UnixNano())
	}
	actorID := s.submitActorID(body.ActorID)
	obj := body.Objective
	obj.ObjectiveID = corrID
	obj.RequestID = corrID
	ctx := core.NewRequestContext(corrID, body.BusinessID, actorID)
	if body.DivisionID != "" {
		ctx = ctx.WithDivision(body.DivisionID)
	}
	req := &core.Request{
		ID: corrID, Context: ctx, Intent: obj.Description,
		Input:   map[string]string{"agent_objective": agentintel.EncodeObjective(obj)},
		Handler: s.intel.Handler(),
	}
	if err := s.engine.SubmitRequest(req); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "RESOURCE_UNAVAILABLE", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", corrID)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"execution_id": corrID, "correlation_id": corrID, "status": "accepted", "actor_id": actorID,
	})
}

// handleGetIntelligence reports the objective execution result: 200 terminal
// (the terminal state rides in outcome.summary as state=<state>), 202 pending,
// 404 unknown-or-out-of-scope (G5).
func (s *Server) handleGetIntelligence(w http.ResponseWriter, r *http.Request) {
	businessID := r.URL.Query().Get("business_id")
	if businessID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}
	res, stopped := s.requireActorMembership(w, r, businessID)
	if stopped {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "execution ID required")
		return
	}
	result, ok := s.engine.GetResult(id)
	if !ok {
		if pending, inFlight := s.engine.Pending(id); inFlight {
			if pending.BusinessID != businessID {
				s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
				return
			}
			if !s.authorizeDivisionRead(res.IdentityID, businessID, pending.DivisionID) {
				s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Correlation-ID", pending.CorrelationID)
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"execution_id": id, "correlation_id": pending.CorrelationID, "status": "pending",
			})
			return
		}
		if result, ok = s.engine.GetResult(id); !ok {
			s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
			return
		}
	}
	if result.BusinessID != businessID {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
		return
	}
	if !s.authorizeDivisionRead(res.IdentityID, businessID, result.DivisionID) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// handleCancelIntelligence uses the same E-005 cancellation path as requests
// and executions.
func (s *Server) handleCancelIntelligence(w http.ResponseWriter, r *http.Request) {
	businessID := r.URL.Query().Get("business_id")
	if businessID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}
	res, stopped := s.requireActorMembership(w, r, businessID)
	if stopped {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "execution ID required")
		return
	}
	actorID := unauthenticatedActorID
	if res.IdentityID != "" {
		actorID = res.IdentityID
	}
	err := s.engine.CancelRequest(id, businessID, actorID)
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Correlation-ID", id)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"execution_id": id, "correlation_id": id, "status": "cancelling"})
	case errors.Is(err, core.ErrRequestNotFound), errors.Is(err, core.ErrScopeMismatch), errors.Is(err, core.ErrDivisionScopeMismatch):
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
	case errors.Is(err, core.ErrAlreadyCompleted):
		msg := "execution already in a non-cancellable terminal state"
		var terminal *core.TerminalStateError
		if errors.As(err, &terminal) {
			msg = fmt.Sprintf("execution already in terminal state: %s", terminal.Status)
		}
		s.writeError(w, r, http.StatusConflict, "CONFLICT", msg)
	default:
		s.writeInternal(w, r, http.StatusInternalServerError, "INTERNAL_FAILURE", "cancellation failed", err)
	}
}
