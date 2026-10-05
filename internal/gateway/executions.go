package gateway

// Execution surface (Agent Execution Layer v1). An execution is a core
// request instrumented with the agentexec runtime handler: governance,
// approvals, escalation, events, cancel semantics, G2/G3/G5/G4 are all
// inherited from the existing request path. This file is the admission +
// retrieval edges with no new authorization model.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/core"
)

type executionBody struct {
	Intent       string                   `json:"intent"`
	BusinessID   string                   `json:"business_id"`
	DivisionID   string                   `json:"division_id,omitempty"`
	ActorID      string                   `json:"actor_id"`
	Priority     int                      `json:"priority"`
	Constraints  []string                 `json:"constraints,omitempty"`
	AgentID      string                   `json:"agent_id,omitempty"`
	RequiredCaps []string                 `json:"required_capabilities,omitempty"`
	OptionalCaps []string                 `json:"optional_capabilities,omitempty"`
	Tools        []agentexec.ToolCallSpec `json:"tools,omitempty"`
	Delegates    []agentexec.DelegateSpec `json:"delegates,omitempty"`
	Workflow     *agentexec.WorkflowSpec  `json:"workflow,omitempty"`
}

// handleSubmitExecution admits an execution request. It also answers with the
// same envelope the request submit path uses.
func (s *Server) handleSubmitExecution(w http.ResponseWriter, r *http.Request) {
	if s.agentRunner == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"agent runtime not configured")
		return
	}
	var body executionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "invalid JSON body")
		return
	}
	if body.Intent == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "intent required")
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
	// G2: admission requires an active business (and an active division
	// when named).
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
	// G2 second layer for entities the runner manages: an integration reads
	// a frozen agent registry elsewhere; this endpoint does not evaluate
	// agent status at admission — selection rejects inactive agents at
	// execution time instead (documented contract).
	spec := agentexec.Spec{
		AgentID: body.AgentID, RequiredCapabilities: body.RequiredCaps,
		OptionalCapabilities: body.OptionalCaps, Tools: body.Tools,
		Delegates: body.Delegates, Workflow: body.Workflow,
	}
	raw, _ := json.Marshal(spec)
	ctx := core.NewRequestContext("api-"+fmt.Sprint(s.now().UnixNano())+"-e", body.BusinessID, s.submitActorID(body.ActorID))
	if body.DivisionID != "" {
		ctx = ctx.WithDivision(body.DivisionID)
	}
	corrID := r.Header.Get("X-Correlation-ID")
	if corrID == "" {
		corrID = ctx.CorrelationID
	} else {
		ctx.CorrelationID = corrID
	}
	req := &core.Request{
		ID: corrID, Context: ctx, Intent: body.Intent, Priority: body.Priority,
		Constraints: body.Constraints,
		Input:       map[string]string{"agent_execution": string(raw)},
		Handler:     s.agentRunner.Handler(),
	}
	if err := s.engine.SubmitRequest(req); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "RESOURCE_UNAVAILABLE", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", corrID)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"execution_id": corrID, "correlation_id": corrID, "status": "accepted", "actor_id": s.submitActorID(body.ActorID),
	})
}

// handleGetExecution reports an execution result (200 terminal / 202 pending /
// 404 unknown|foreign, G5), reusing the engine's process-local result store
// with the same authorization shape as a request read.
func (s *Server) handleGetExecution(w http.ResponseWriter, r *http.Request) {
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

// handleCancelExecution follows E-005/cancel semantics through the engine's
// CancelRequest; idempotent repeat cancels answer 202, foreign scope 404.
func (s *Server) handleCancelExecution(w http.ResponseWriter, r *http.Request) {
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
