package gateway

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/researchcompare"
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
	// recoveryComparisonMaxID bounds a query-supplied execution id before any
	// journal read. It is deliberately a little wider than generated ids.
	recoveryComparisonMaxID = 512
)

// durableExecutionView is the allow-listed projection of a durable execution
// record. It never includes actor_id, the internal recovery reason, store keys,
// or raw durable bytes.
type durableExecutionView struct {
	Source      string    `json:"source"`
	ExecutionID string    `json:"execution_id"`
	BusinessID  string    `json:"business_id"`
	DivisionID  string    `json:"division_id,omitempty"`
	State       string    `json:"state"`
	ErrorClass  string    `json:"error_class,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// CompletedAt is the real terminal transition time. It is absent for an
	// older record or an execution that never reached a terminal transition
	// (e.g. recovery classified it unknown).
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// ResearchEvidence is returned only by the single-execution detail read.
	// The list stays bounded and does not repeat source snapshots for every
	// history row.
	ResearchEvidence *execjournal.ResearchEvidence `json:"research_evidence,omitempty"`
	// ResearchEvaluation is the persisted deterministic quality verdict,
	// returned only by the single-execution detail read alongside its evidence.
	ResearchEvaluation *execjournal.ResearchEvaluation `json:"research_evaluation,omitempty"`
	// ResearchSynthesis is the persisted validated typed synthesis, returned
	// only by the single-execution detail read alongside the evidence and
	// evaluation it describes.
	ResearchSynthesis *execjournal.ResearchSynthesis `json:"research_synthesis,omitempty"`
	Recovery          *durableRecoveryView           `json:"recovery,omitempty"`
	// Approvals is the durable approval history for THIS execution, in the
	// order it was recorded. It is omitted entirely when the execution never
	// went through an approval gate, so an unapproved execution's response is
	// exactly what it was before history was exposed.
	Approvals []durableApprovalView `json:"approvals,omitempty"`
}

// durableApprovalView is the allow-listed projection of one approval decision.
//
// The approver identity is deliberately NOT projected: the durable read
// contract excludes identity references, and the live approval surface gates
// approver identity behind business-wide membership — a strictly higher bar
// than this division-scoped read. Exposing it here would weaken that, so
// whether the decider is ever exposed is an open contract decision (v34).
type durableApprovalView struct {
	ApprovalID string    `json:"approval_id"`
	Outcome    string    `json:"outcome"`
	DecidedAt  time.Time `json:"decided_at"`
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
		CompletedAt: er.CompletedAt,
	}
	if er.Recovery != nil && er.Recovery.Recovered {
		v.Recovery = &durableRecoveryView{
			Recovered:   true,
			PriorState:  string(er.Recovery.PriorState),
			RecoveredAt: er.Recovery.RecoveredAt,
		}
	}
	// Approval history is projected only as recorded: append order is preserved,
	// and nothing is synthesized. A superseded pause still appears as the
	// recorded `requested` entry; the execution's own `state` is what carries
	// the outcome, so a reader can never mistake a historical decision for a
	// currently effective one.
	if len(er.Approvals) > 0 {
		v.Approvals = make([]durableApprovalView, 0, len(er.Approvals))
		for _, a := range er.Approvals {
			v.Approvals = append(v.Approvals, durableApprovalView{
				ApprovalID: a.ApprovalID,
				Outcome:    a.Outcome,
				DecidedAt:  a.DecidedAt,
			})
		}
	}
	return v
}

// durableDetailView adds the fields that are useful only when reading one
// execution. The source snapshot is not repeated in every list row.
func durableDetailView(er execjournal.ExecutionRecord) durableExecutionView {
	v := durableView(er)
	v.ResearchEvidence = er.ResearchEvidence
	v.ResearchEvaluation = er.ResearchEvaluation
	v.ResearchSynthesis = er.ResearchSynthesis
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
	_ = json.NewEncoder(w).Encode(durableDetailView(rec))
}

// durableComparisonView is the allow-listed comparison envelope. The
// comparison itself contains only bounded summaries and recorded differences;
// it never includes raw evidence, synthesis prose, prompts, actor ids or
// recovery reasons.
type durableComparisonView struct {
	Source string `json:"source"`
	researchcompare.Comparison
}

// handleCompareRecoveryExecutions projects a deterministic comparison of two
// eligible durable research snapshots. Both executions are authorized
// independently inside the requested business. Missing, foreign and
// out-of-scope records are indistinguishable (404), and eligibility is checked
// only after both records are authorized.
func (s *Server) handleCompareRecoveryExecutions(w http.ResponseWriter, r *http.Request) {
	businessID := r.URL.Query().Get("business_id")
	if businessID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}
	res, stopped := s.requireActorMembership(w, r, businessID)
	if stopped {
		return
	}
	leftID := r.URL.Query().Get("left_execution_id")
	rightID := r.URL.Query().Get("right_execution_id")
	if !validComparisonExecutionID(leftID) || !validComparisonExecutionID(rightID) {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION",
			"left_execution_id and right_execution_id are required and must be valid")
		return
	}
	if leftID == rightID {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION",
			"left_execution_id and right_execution_id must differ")
		return
	}
	j, unavailable := s.journalOrUnavailable(w, r)
	if unavailable {
		return
	}
	left, right, leftOK, rightOK, err := j.GetPair(leftID, rightID)
	if err != nil {
		// A corrupt or unsupported record fails closed and is never partially
		// interpreted as a comparison input.
		s.writeError(w, r, http.StatusServiceUnavailable, "RESOURCE_UNAVAILABLE",
			"durable execution records are unavailable")
		return
	}
	// Unknown, foreign, unreadable and out-of-scope are indistinguishable (404).
	if !leftOK || !rightOK || left.BusinessID != businessID || right.BusinessID != businessID {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "durable execution record not found")
		return
	}
	if !s.authorizeDivisionRead(res.IdentityID, businessID, left.DivisionID) ||
		!s.authorizeDivisionRead(res.IdentityID, businessID, right.DivisionID) {
		s.writeError(w, r, http.StatusNotFound, "VALIDATION", "durable execution record not found")
		return
	}
	if !eligibleForResearchComparison(left) || !eligibleForResearchComparison(right) {
		s.writeError(w, r, http.StatusConflict, "CONFLICT",
			"both executions must be settled and eligible for comparison")
		return
	}
	comparison, err := researchcompare.Compare(left, right)
	if err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "RESOURCE_UNAVAILABLE",
			"durable comparison is unavailable")
		return
	}
	if err := researchcompare.Validate(comparison); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "RESOURCE_UNAVAILABLE",
			"durable comparison is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(durableComparisonView{
		Source: recoverySource, Comparison: comparison,
	})
}

func validComparisonExecutionID(id string) bool {
	return id != "" && len(id) <= recoveryComparisonMaxID &&
		!strings.ContainsAny(id, "\x00\n\r\t")
}

// eligibleForResearchComparison admits settled records only. A provisional
// approval pause is a failed/APPROVAL_REQUIRED record that can still be
// superseded by the narrow approval-resume path, so it is not a stable
// comparison snapshot. StateUnknown is recovery-settled and therefore eligible.
func eligibleForResearchComparison(rec execjournal.ExecutionRecord) bool {
	if rec.State == execjournal.StateUnknown {
		return true
	}
	if !rec.State.Terminal() {
		return false
	}
	if rec.State == execjournal.StateFailed && rec.ErrorClass == execjournal.ApprovalRequiredClass {
		return false
	}
	return true
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
