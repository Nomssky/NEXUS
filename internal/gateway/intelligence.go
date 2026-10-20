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
	"strconv"

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
	// One coherent read: a resume's re-admission moves a known execution
	// between the results and in-flight registries, so composing them with
	// separate lookups could observe it as absent (404).
	view := s.engine.LookupExecution(id)
	if view.Result == nil && !view.InFlight {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
		return
	}
	if view.Result == nil {
		pending := view.Pending
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
	result := view.Result
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

// intelligenceTraceResponse is the read-only trace representation
// (EXECUTION_TRACE_CONTRACTS §10): the authoritative execution status from
// the stored record plus the bounded, redacted, process-local capture.
// Durability is stated in every response so no consumer can mistake the
// view for a durable journal (contract §12).
type intelligenceTraceResponse struct {
	ExecutionID   string             `json:"execution_id"`
	CorrelationID string             `json:"correlation_id"`
	BusinessID    string             `json:"business_id"`
	DivisionID    string             `json:"division_id,omitempty"`
	Status        string             `json:"status"`
	Durability    string             `json:"durability"`
	Capture       core.TraceCapture  `json:"capture"`
	Entries       []*core.TraceEntry `json:"entries"`
}

// handleGetIntelligenceTrace returns the process-local execution trace. It
// reuses the exact authorization sequence of the detail read (membership,
// business match, division read — 400/401/403/404 unchanged), is strictly
// read-only, bounds its result with limit/after_seq capture-order
// pagination, and represents partial, evicted or redacted data honestly
// instead of fabricating a complete trace (EXECUTION_TRACE_CONTRACTS §10).
func (s *Server) handleGetIntelligenceTrace(w http.ResponseWriter, r *http.Request) {
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
	limit := core.MaxTraceEntries
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > core.MaxTraceEntries {
			s.writeError(w, r, http.StatusBadRequest, "VALIDATION",
				fmt.Sprintf("limit must be an integer between 1 and %d", core.MaxTraceEntries))
			return
		}
		limit = n
	}
	afterSeq := uint64(0)
	if raw := r.URL.Query().Get("after_seq"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "after_seq must be a non-negative integer")
			return
		}
		afterSeq = uint64(n)
	}

	// Same 404 semantics as the detail read: unknown, foreign-business and
	// unreadable-division executions are indistinguishable (G5). One coherent
	// view so a resume transition is never observed as absent.
	var divisionID, status string
	view := s.engine.LookupExecution(id)
	if view.Result == nil && !view.InFlight {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
		return
	}
	if view.Result == nil {
		pending := view.Pending
		if pending.BusinessID != businessID {
			s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
			return
		}
		if !s.authorizeDivisionRead(res.IdentityID, businessID, pending.DivisionID) {
			s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
			return
		}
		divisionID, status = pending.DivisionID, "pending"
	} else {
		result := view.Result
		if result.BusinessID != businessID {
			s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
			return
		}
		if !s.authorizeDivisionRead(res.IdentityID, businessID, result.DivisionID) {
			s.writeError(w, r, http.StatusNotFound, "VALIDATION", "execution not found")
			return
		}
		divisionID, status = result.DivisionID, result.Status
	}

	resp := intelligenceTraceResponse{
		ExecutionID:   id,
		CorrelationID: id,
		BusinessID:    businessID,
		DivisionID:    divisionID,
		Status:        status,
		Durability:    "process_local",
		Capture: core.TraceCapture{
			Limit: core.MaxTraceEntries,
		},
		Entries: []*core.TraceEntry{},
	}
	if view, captured := s.engine.ExecutionTrace(id, afterSeq, limit); captured {
		resp.Capture = view.Capture
		if view.Entries != nil {
			resp.Entries = view.Entries
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", id)
	_ = json.NewEncoder(w).Encode(resp)
}
