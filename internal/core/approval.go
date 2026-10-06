// Approval wiring (P1: ApprovalEngine → runtime).
//
// The ApprovalEngine is the validator (pending rules: authorization,
// self-approval, timeout); core owns the lifecycle around it:
//
//	REQUIRE_APPROVAL (chain gate or executor gate)
//	  → createApproval: record + index + approval.requested event
//	  → response failed/APPROVAL_REQUIRED with error.details.approval_id
//	  → POST approve → approval.approved + resume re-execution
//	  → POST deny    → approval.denied (no resume)
//	  → timeout      → approval.expired (INV-16: silence = denial)
//
// Resume semantics: the approval index maps requestID → approved record;
// approvalStateFor feeds governance Request.ApprovalState so both gates
// satisfy their REQUIRE_APPROVAL policy on the re-run (INV-10: conversion
// happens only with an explicit approved record — never on an event).
//
// Persistence is in-memory (decision P1): records do not survive restart.
// The engine Store() surface stays external-only (C-019).
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
)

// Approval sentinels (gateway maps them to HTTP: 404/409/403). Governance
// typed errors are mapped at the core boundary so callers only import core.
var (
	// ErrApprovalNotFound: no approval record with that ID (or it was
	// already resolved and cleaned up).
	ErrApprovalNotFound = errors.New("core: approval not found")
	// ErrApprovalNotPending: approval already decided (approved/denied/
	// expired) — a second decision is a state conflict.
	ErrApprovalNotPending = errors.New("core: approval not pending")
	// ErrSelfApprovalProhibited: policy forbids the requester deciding
	// their own request (SCHEMA_WORK §6.1).
	ErrSelfApprovalProhibited = errors.New("core: self-approval prohibited")
	// ErrApproverUnauthorized: approver not in the policy's approver list.
	ErrApproverUnauthorized = errors.New("core: approver not authorized")
	// ErrApprovalNotResumable: decision recorded but the resume run cannot
	// be admitted (engine not running / shutting down).
	ErrApprovalNotResumable = errors.New("core: approval cannot be resumed")
)

// approvalEntry is core's index of one approval record: the engine record
// plus the original request needed for resume. entry.ar is the SAME pointer
// the ApprovalEngine mutates in place on approve/deny/timeout, so the entry
// always reflects the current decision state.
type approvalEntry struct {
	requestID  string
	businessID string
	req        *Request
	ar         *governance.ApprovalRequest
	// actionFingerprint binds an ACTION-level approval to exactly one proposal
	// (AGENT_GOVERNANCE_CONTROL_CONTRACTS section 5). Empty for the
	// task/request-level approvals this file already handled.
	actionFingerprint string
}

// approvalEntry returns the entry for an approval id, or nil.
func (e *Engine) approvalEntry(approvalID string) *approvalEntry {
	e.approvalMu.Lock()
	defer e.approvalMu.Unlock()
	return e.approvals[approvalID]
}

// ApprovalRecord is the contract-shaped projection of an approval
// (SCHEMA_WORK §6.2 Approval Record, §6.3 state names). Fields whose value
// is not tracked in P1 (nexus_id, requester_type, escalation_ref,
// provenance, metadata) are omitted rather than invented.
type ApprovalRecord struct {
	SchemaVersion          string     `json:"schema_version"`
	EntityType             string     `json:"entity_type"`
	EntityID               string     `json:"entity_id"`
	BusinessID             string     `json:"business_id"`
	RequestedAction        string     `json:"requested_action"`
	RequesterID            string     `json:"requester_id"`
	ApproverID             string     `json:"approver_id,omitempty"`
	ApproverType           string     `json:"approver_type,omitempty"`
	AuthorityContext       string     `json:"authority_context"`
	PolicyRef              string     `json:"policy_ref,omitempty"`
	Scope                  string     `json:"scope"`
	Status                 string     `json:"status"`
	CreatedAt              time.Time  `json:"created_at"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty"`
	DecidedAt              *time.Time `json:"decided_at,omitempty"`
	DecisionRationale      string     `json:"decision_rationale,omitempty"`
	SelfApprovalProhibited bool       `json:"self_approval_prohibited"`
	DelegationAllowed      bool       `json:"delegation_allowed"`
	AuditRef               string     `json:"audit_ref"`
	CorrelationID          string     `json:"correlation_id,omitempty"`
}

// createApproval creates an approval record for a REQUIRE_APPROVAL decision,
// indexes it for the resume path, and emits approval.requested. One record
// per request: a repeat create returns the existing record (the chain gate
// and the executor gate each produce REQUIRE_APPROVAL, but only one runs
// per request; dedup is defense in depth).
//
// Returns an error only when the engine refuses the record (e.g. self-approval
// prohibited with an empty requester identity) — callers then surface the
// APPROVAL_REQUIRED failure without an approval_id (graceful degradation:
// D2 behavior preserved, no resume possible).
func (e *Engine) createApproval(req *Request, decision governance.Decision, govReq governance.Request) (*governance.ApprovalRequest, error) {
	if req == nil || req.Context == nil {
		return nil, fmt.Errorf("core: cannot create approval without request context")
	}

	e.approvalMu.Lock()
	if id, ok := e.approvalByReq[req.ID]; ok {
		if entry := e.approvals[id]; entry != nil {
			e.approvalMu.Unlock()
			return entry.ar, nil
		}
	}
	e.approvalMu.Unlock()

	// Policy without an approval config → zero config (no timeout, no
	// self-approval prohibition): never invent policy requirements.
	config := governance.ApprovalConfig{}
	if decision.ApprovalRequired != nil {
		config = *decision.ApprovalRequired
	}

	ar, err := e.approvalEngine.RequestApproval(decision, govReq, config)
	if err != nil {
		return nil, err
	}

	e.approvalMu.Lock()
	e.approvals[ar.DecisionID] = &approvalEntry{
		requestID:  req.ID,
		businessID: req.Context.BusinessID,
		req:        req,
		ar:         ar,
	}
	e.approvalByReq[req.ID] = ar.DecisionID
	e.approvalMu.Unlock()

	e.emitApprovalEvent(req, event.EventTypeApprovalRequested, ar, nil)
	return ar, nil
}

// approvalStateFor returns a non-nil APPROVED state when an approved record
// exists for requestID (resume re-run), nil otherwise. Callers pass it into
// governance Request.ApprovalState.
func (e *Engine) approvalStateFor(requestID string) *governance.ApprovalState {
	e.approvalMu.Lock()
	defer e.approvalMu.Unlock()
	id, ok := e.approvalByReq[requestID]
	if !ok {
		return nil
	}
	entry := e.approvals[id]
	if entry == nil || entry.ar.Status != governance.ApprovalStateApproved {
		return nil
	}
	state := governance.ApprovalStateApproved
	return &state
}

// expireApprovals sweeps timed-out approvals (INV-16: silence ≠ approval —
// expiry defaults to denial when the policy configures auto-deny), removes
// their index entries, and emits approval.expired for each. Lazy trigger:
// called from every approval read/decision entry point.
func (e *Engine) expireApprovals() {
	for _, ar := range e.approvalEngine.CheckTimeouts() {
		e.approvalMu.Lock()
		entry := e.approvals[ar.DecisionID]
		var req *Request
		if entry != nil {
			req = entry.req
			delete(e.approvals, ar.DecisionID)
			delete(e.approvalByReq, entry.requestID)
		}
		e.approvalMu.Unlock()
		if req != nil {
			e.emitApprovalEvent(req, event.EventTypeApprovalExpired, ar,
				map[string]string{"status": "EXPIRED"})
		}
	}
}

// ListApprovals returns contract-shaped records for pending approvals,
// optionally scoped to businessID (empty = all — the gateway never passes
// empty; it fails closed on a missing business_id first). Resolved records
// are not listed: they are removed from the index at resolution or at
// resume completion.
func (e *Engine) ListApprovals(businessID string) []ApprovalRecord {
	e.expireApprovals()

	e.approvalMu.Lock()
	records := make([]ApprovalRecord, 0, len(e.approvals))
	for _, entry := range e.approvals {
		if entry.ar.Status != governance.ApprovalStatePending {
			continue // decided, awaiting resume cleanup — not actionable
		}
		if businessID != "" && entry.businessID != businessID {
			continue
		}
		records = append(records, projectApproval(entry))
	}
	e.approvalMu.Unlock()

	// Stable ordering for API consumers (map iteration is random).
	sort.Slice(records, func(i, j int) bool {
		if !records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].CreatedAt.Before(records[j].CreatedAt)
		}
		return records[i].EntityID < records[j].EntityID
	})
	return records
}

// ApproveRequest decides a pending approval with APPROVED and resumes the
// original request. Order matters: 404 → 403 → running → engine decision →
// resume. The stored failed/APPROVAL_REQUIRED result is dropped before
// re-admission so consumers never see it as the resume's terminal result.
func (e *Engine) ApproveRequest(approvalID, businessID, approver, reason string) error {
	// INV-16: a timed-out approval must not be approvable — sweep first.
	e.expireApprovals()

	e.approvalMu.Lock()
	entry := e.approvals[approvalID]
	e.approvalMu.Unlock()
	if entry == nil {
		return ErrApprovalNotFound
	}
	if entry.businessID != businessID {
		return ErrScopeMismatch
	}

	// Resume needs a live processing loop — check before mutating the
	// approval state, so a stopped engine never leaves a decided record
	// without a run (read shutdownCh under lock: Resume() rewrites it).
	e.mu.RLock()
	running := e.status == lifecycle.StateRunning
	shutdownCh := e.shutdownCh
	e.mu.RUnlock()
	if !running {
		return ErrApprovalNotResumable
	}

	// Validate + resolve via the ApprovalEngine (pending check, approver
	// authorization, self-approval rules). The engine mutates entry.ar in
	// place before dropping it from its pending map, so this entry now
	// reads APPROVED.
	if err := e.approvalEngine.Approve(approvalID, approver, reason); err != nil {
		return mapApprovalErr(err)
	}
	e.emitApprovalEvent(entry.req, event.EventTypeApprovalApproved, entry.ar,
		map[string]string{
			"status":             "APPROVED",
			"approver_id":        approver,
			"decision_rationale": reason,
		})

	// Resume: drop the stale APPROVAL_REQUIRED result, re-admit the
	// original request. On the re-run approvalStateFor(req.ID) returns
	// approved, so both governance gates pass and the handler executes.
	old, hadResult := e.dropResult(entry.requestID)

	// C-018: the dequeue path Releases a slot for every item it pops
	// (engine.go:453) and SubmitRequest pairs that with an Accept. Resume
	// re-admits through that same queue, so it must take an Accept slot
	// too — skipping it left a Release with no matching Accept, permanently
	// under-counting QueueSize() until the counter was pinned at 0 and the
	// gate stopped rejecting.
	//
	// The approval is already APPROVED here (one decision per record), so a
	// saturated queue waits for a slot rather than failing: returning an
	// error would strand an approved record with no run and no way to
	// re-decide it. The send below already blocks on the buffered channel,
	// so this adds no new blocking behaviour — only an honest count.
	if !e.acceptResumeSlot(shutdownCh) {
		if hadResult {
			e.restoreResult(entry.requestID, old)
		}
		return fmt.Errorf("%w: engine shutting down during resume", ErrApprovalNotResumable)
	}
	e.registerInflight(entry.req)
	// The resumed run re-proposes the gated ACTION, so the reference index must
	// be live for the admission that consumes this approval
	// (AGENT_GOVERNANCE_CONTROL_CONTRACTS §5). It was dropped when the first run
	// stored its terminal result.
	e.trackRequestRef(entry.req)

	select {
	case e.requests <- entry.req:
		return nil
	case <-shutdownCh:
		// Engine went down between the running check and the send: give the
		// slot back (exactly one Release per Accept), release the
		// registration, keep the previously stored result. The approval
		// stays APPROVED (in-memory state dies with the process anyway) —
		// surfaced so the client can resubmit.
		e.backpressure.Release()
		e.deregisterInflight(entry.requestID)
		if hadResult {
			e.restoreResult(entry.requestID, old)
		}
		return fmt.Errorf("%w: engine shutting down during resume", ErrApprovalNotResumable)
	}
}

// resumeAcceptRetryInterval is how long acceptResumeSlot waits before
// re-trying a saturated queue. It only applies when the engine is already at
// its queue limit; the normal path takes a slot on the first attempt.
const resumeAcceptRetryInterval = 5 * time.Millisecond

// acceptResumeSlot takes a backpressure slot for an approval resume,
// blocking until one frees up or the engine shuts down. Reports false only
// on shutdown (the caller then unwinds the resume).
func (e *Engine) acceptResumeSlot(shutdownCh <-chan struct{}) bool {
	for {
		if e.backpressure.Accept() {
			return true
		}
		timer := time.NewTimer(resumeAcceptRetryInterval)
		select {
		case <-shutdownCh:
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

// DenyRequest decides a pending approval with DENIED. No resume: the stored
// response stays failed/APPROVAL_REQUIRED (D2 pin) — the approval record
// carries the decision. Denial relies on gateway membership scoping; the
// ApprovalEngine applies no approver-authorization check on Deny (existing
// engine semantics — denying is the safe direction).
func (e *Engine) DenyRequest(approvalID, businessID, approver, reason string) error {
	e.expireApprovals()

	e.approvalMu.Lock()
	entry := e.approvals[approvalID]
	e.approvalMu.Unlock()
	if entry == nil {
		return ErrApprovalNotFound
	}
	if entry.businessID != businessID {
		return ErrScopeMismatch
	}

	if err := e.approvalEngine.Deny(approvalID, approver, reason); err != nil {
		return mapApprovalErr(err)
	}
	e.emitApprovalEvent(entry.req, event.EventTypeApprovalDenied, entry.ar,
		map[string]string{
			"status":             "DENIED",
			"approver_id":        approver,
			"decision_rationale": reason,
		})

	// Resolved without resume — drop the index entry.
	e.dropApproval(approvalID)
	return nil
}

// cleanupResumeApproval drops the index entry once an approved approval's
// resume run stored its terminal result — the record has served its purpose
// (both gates consulted it during the run). Pending/denied entries are
// untouched: pending waits for a decision, denied was already dropped.
//
// Action-level approvals (AGENT_GOVERNANCE_CONTROL_CONTRACTS §5) are cleaned up
// the same way, and their fingerprint index entry is dropped with them, so a
// spent approval cannot authorize anything later.
func (e *Engine) cleanupResumeApproval(requestID string) {
	var fingerprints []string
	e.approvalMu.Lock()
	id, ok := e.approvalByReq[requestID]
	entry := e.approvals[id]
	if ok && entry != nil && entry.ar.Status == governance.ApprovalStateApproved {
		delete(e.approvals, id)
		delete(e.approvalByReq, requestID)
	}
	for approvalID, aEntry := range e.approvals {
		if aEntry.requestID != requestID || aEntry.actionFingerprint == "" {
			continue
		}
		if aEntry.ar.Status == governance.ApprovalStateApproved {
			delete(e.approvals, approvalID)
			fingerprints = append(fingerprints, aEntry.actionFingerprint)
		}
	}
	e.approvalMu.Unlock()
	for _, fp := range fingerprints {
		e.actionApprovals.drop(requestID, fp)
	}
}

// dropApproval removes an index entry (both maps).
func (e *Engine) dropApproval(approvalID string) {
	e.approvalMu.Lock()
	defer e.approvalMu.Unlock()
	entry := e.approvals[approvalID]
	if entry == nil {
		return
	}
	delete(e.approvals, approvalID)
	delete(e.approvalByReq, entry.requestID)
}

// dropResult removes a stored result (and its FIFO position) and returns it
// so a failed resume admission can restore it.
func (e *Engine) dropResult(requestID string) (*Response, bool) {
	e.resultsMu.Lock()
	defer e.resultsMu.Unlock()
	res, ok := e.results[requestID]
	if !ok {
		return nil, false
	}
	delete(e.results, requestID)
	for i, id := range e.resultOrder {
		if id == requestID {
			e.resultOrder = append(e.resultOrder[:i], e.resultOrder[i+1:]...)
			break
		}
	}
	return res, true
}

// restoreResult re-inserts a previously dropped result (shutdown-race path).
func (e *Engine) restoreResult(requestID string, res *Response) {
	if res == nil {
		return
	}
	e.resultsMu.Lock()
	defer e.resultsMu.Unlock()
	if _, exists := e.results[requestID]; exists {
		return
	}
	e.results[requestID] = res
	e.resultOrder = append(e.resultOrder, requestID)
}

// emitApprovalEvent publishes an approval-domain event (SCHEMA_EVENTS
// approval domain; RUNTIME §16.2 payload: approval_id, requester_id).
func (e *Engine) emitApprovalEvent(req *Request, typ event.EventType, ar *governance.ApprovalRequest, extra map[string]string) {
	if req == nil || req.Context == nil || ar == nil {
		return
	}
	payload := map[string]string{
		"approval_id":  ar.DecisionID,
		"requester_id": ar.Requester,
	}
	for k, v := range extra {
		payload[k] = v
	}
	data, _ := json.Marshal(payload)
	_ = e.eventBus.Publish(&event.Event{
		ID:            fmt.Sprintf("%s-%s", ar.DecisionID, typ),
		Type:          typ,
		Source:        "core",
		Timestamp:     e.now(),
		BusinessID:    req.Context.BusinessID,
		CorrelationID: req.Context.CorrelationID,
		Priority:      event.PriorityNormal,
		Data:          data,
	})
}

// projectApproval maps an indexed entry to the contract record shape
// (SCHEMA_WORK §6.2/§6.3). Called with approvalMu held.
func projectApproval(entry *approvalEntry) ApprovalRecord {
	ar := entry.ar
	rec := ApprovalRecord{
		SchemaVersion:          "1.0.0",
		EntityType:             "approval",
		EntityID:               ar.DecisionID,
		BusinessID:             entry.businessID,
		RequestedAction:        ar.Request.Action,
		RequesterID:            ar.Requester,
		ApproverType:           ar.Config.ApproverType,
		AuthorityContext:       ar.Decision.Reason,
		PolicyRef:              ar.Decision.MatchedPolicyID,
		Scope:                  ar.Request.Resource,
		Status:                 "PENDING",
		CreatedAt:              ar.RequestedAt,
		SelfApprovalProhibited: ar.Config.SelfApprovalProhibited,
		DelegationAllowed:      ar.Config.DelegationAllowed,
		AuditRef:               entry.requestID,
	}
	switch ar.Status {
	case governance.ApprovalStateApproved:
		rec.Status = "APPROVED"
	case governance.ApprovalStateDenied:
		rec.Status = "DENIED"
	}
	if ar.Config.TimeoutSeconds > 0 {
		// INV-16: silence until expires_at, then denial (auto-deny config).
		exp := ar.RequestedAt.Add(time.Duration(ar.Config.TimeoutSeconds) * time.Second)
		rec.ExpiresAt = &exp
	}
	if ar.ResolvedAt != nil {
		rec.DecidedAt = ar.ResolvedAt
		rec.DecisionRationale = ar.Reason
		rec.ApproverID = ar.Approver
	}
	if entry.req.Context != nil {
		rec.CorrelationID = entry.req.Context.CorrelationID
	}
	return rec
}

// mapApprovalErr converts ApprovalEngine typed errors to core sentinels.
func mapApprovalErr(err error) error {
	switch {
	case errors.Is(err, governance.ErrApprovalNotFound):
		return ErrApprovalNotFound
	case errors.Is(err, governance.ErrApprovalNotPending):
		return ErrApprovalNotPending
	case errors.Is(err, governance.ErrSelfApprovalProhibited):
		return ErrSelfApprovalProhibited
	case errors.Is(err, governance.ErrApproverUnauthorized):
		return ErrApproverUnauthorized
	default:
		return err
	}
}

// emitEscalation publishes governance.escalated — the D3 async handoff of an
// ESCALATE outcome to a higher authority, which doubles as the CTR-ATT-001
// human notification (alert_id=escalation_ref, summary=reason, context via
// requester/gate/request ids, options, deadline). CTR-GOV-002 input shape:
// escalation_id, reason, context, urgency, deadline; ack is the event bus
// accept (the escalation intake consumer then queues the escalation_id —
// see escalation.go).
func (e *Engine) emitEscalation(req *Request, escalationRef, reason, gate string) {
	if req == nil || req.Context == nil {
		return
	}
	urgency := req.Priority
	if urgency <= 0 {
		// Schema §4.4: governance escalations are "high"; 0 = unset.
		urgency = defaultEscalationUrgency
	}
	deadline := e.now().Add(defaultEscalationTTL)
	if req.Deadline != nil {
		deadline = *req.Deadline
	}
	data, _ := json.Marshal(escalationPayload{
		EscalationRef: escalationRef,
		RequestID:     req.ID,
		RequesterID:   req.Context.ActorID,
		Reason:        reason,
		Gate:          gate,
		Urgency:       strconv.Itoa(urgency),
		Deadline:      deadline.UTC().Format(time.RFC3339Nano),
		Options:       escalationAlertOptions,
	})
	_ = e.eventBus.Publish(&event.Event{
		ID:            fmt.Sprintf("%s-%s", escalationRef, event.EventTypeGovernanceEscalated),
		Type:          event.EventTypeGovernanceEscalated,
		Source:        "core",
		Timestamp:     e.now(),
		BusinessID:    req.Context.BusinessID,
		CorrelationID: req.Context.CorrelationID,
		Priority:      event.PriorityNormal,
		Data:          data,
	})
}
