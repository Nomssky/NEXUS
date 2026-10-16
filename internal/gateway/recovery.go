package gateway

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

// Durable execution read API (v11,
// contracts/DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md). It exposes
// a read-only, authorization-safe view of the v10 durable journal, on a NEW
// route so the process-local execution-result semantics (and the frozen
// post-restart 404) are untouched. No mutation endpoint exists.

const (
	recoverySource       = "durable_journal"
	recoveryDefaultLimit = 50
	recoveryMaxLimit     = 200
)

// durableExecutionView is the allow-listed projection of a durable execution
// record. It never includes actor_id, the internal recovery reason, store keys,
// or raw durable bytes.
type durableExecutionView struct {
	Source      string               `json:"source"`
	ExecutionID string               `json:"execution_id"`
	BusinessID  string               `json:"business_id"`
	DivisionID  string               `json:"division_id,omitempty"`
	State       string               `json:"state"`
	ErrorClass  string               `json:"error_class,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
	Recovery    *durableRecoveryView `json:"recovery,omitempty"`
}

type durableRecoveryView struct {
	Recovered   bool      `json:"recovered"`
	PriorState  string    `json:"prior_state,omitempty"`
	RecoveredAt time.Time `json:"recovered_at,omitempty"`
}

func durableView(er execjournal.ExecutionRecord) durableExecutionView {
	v := durableExecutionView{
		Source:      recoverySource,
		ExecutionID: er.ID,
		BusinessID:  er.BusinessID,
		DivisionID:  er.DivisionID,
		State:       string(er.State),
		ErrorClass:  er.ErrorClass,
		CreatedAt:   er.CreatedAt,
		UpdatedAt:   er.UpdatedAt,
	}
	if er.Recovery != nil && er.Recovery.Recovered {
		v.Recovery = &durableRecoveryView{
			Recovered:   true,
			PriorState:  string(er.Recovery.PriorState),
			RecoveredAt: er.Recovery.RecoveredAt,
		}
	}
	return v
}

// journalOrUnavailable returns the journal, or writes a 503 and reports true.
func (s *Server) journalOrUnavailable(w http.ResponseWriter, r *http.Request) (*execjournal.Journal, bool) {
	var j *execjournal.Journal
	if s.engine != nil {
		j = s.engine.ExecutionJournal()
	}
	if j == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "RESOURCE_UNAVAILABLE",
			"durable execution journal is not available")
		return nil, true
	}
	return j, false
}

// handleGetRecoveryExecution returns the durable lifecycle evidence for one
// execution. Authorization mirrors the execution-result read sequence.
func (s *Server) handleGetRecoveryExecution(w http.ResponseWriter, r *http.Request) {
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
	j, unavailable := s.journalOrUnavailable(w, r)
	if unavailable {
		return
	}
	rec, ok, err := j.Get(id)
	if err != nil {
		// Fail closed on a corrupt/unsupported record; never reinterpret it.
		s.writeError(w, r, http.StatusServiceUnavailable, "RESOURCE_UNAVAILABLE",
			"durable execution record is unavailable")
		return
	}
	// Unknown, foreign and unreadable are indistinguishable (404, G5).
	if !ok || rec.BusinessID != businessID {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "durable execution record not found")
		return
	}
	if !s.authorizeDivisionRead(res.IdentityID, businessID, rec.DivisionID) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "durable execution record not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(durableView(rec))
}

// handleListRecoveryExecutions returns a bounded, newest-first page of durable
// execution records the caller is authorized to see.
func (s *Server) handleListRecoveryExecutions(w http.ResponseWriter, r *http.Request) {
	businessID := r.URL.Query().Get("business_id")
	if businessID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}
	res, stopped := s.requireActorMembership(w, r, businessID)
	if stopped {
		return
	}
	divisionID := r.URL.Query().Get("division_id")
	if divisionID != "" {
		if err := s.authorizeMembershipScope(res.IdentityID, businessID, divisionID); err != nil {
			s.writeError(w, r, http.StatusForbidden, "AUTHORIZATION",
				"access denied: actor is not authorized for the requested division")
			return
		}
	}
	limit, ok := parseBoundedInt(r.URL.Query().Get("limit"), recoveryDefaultLimit, 1, recoveryMaxLimit)
	if !ok {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION",
			"limit must be an integer between 1 and 200")
		return
	}
	offset, ok := parseBoundedInt(r.URL.Query().Get("offset"), 0, 0, execjournal.MaxListScan)
	if !ok {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION",
			"offset must be an integer >= 0")
		return
	}
	j, unavailable := s.journalOrUnavailable(w, r)
	if unavailable {
		return
	}
	recs, err := j.ListByBusiness(businessID, execjournal.MaxListScan)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "RESOURCE_UNAVAILABLE",
			"durable execution records are unavailable")
		return
	}

	// Scope filter: an explicit division must match; otherwise the caller's
	// scope decides (business-wide sees all, division-scoped sees its own).
	authorized := make([]execjournal.ExecutionRecord, 0, len(recs))
	for _, rec := range recs {
		if divisionID != "" {
			if rec.DivisionID != divisionID {
				continue
			}
		} else if !s.authorizeDivisionRead(res.IdentityID, businessID, rec.DivisionID) {
			continue
		}
		authorized = append(authorized, rec)
	}

	total := len(authorized)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := authorized[offset:end]
	views := make([]durableExecutionView, 0, len(page))
	for _, rec := range page {
		views = append(views, durableView(rec))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"source":     recoverySource,
		"executions": views,
		"count":      len(views),
		"limit":      limit,
		"offset":     offset,
		"has_more":   end < total,
	})
}

// parseBoundedInt parses an optional integer query parameter, defaulting when
// empty, and rejects values outside [min,max].
func parseBoundedInt(raw string, def, min, max int) (int, bool) {
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return 0, false
	}
	return n, true
}
