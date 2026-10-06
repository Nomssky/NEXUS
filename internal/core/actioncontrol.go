package core

// Action-level governance control (AGENT_GOVERNANCE_CONTROL_CONTRACTS.md).
//
// This file is the HOST side of the admission controller: it binds an
// action-level REQUIRE_APPROVAL to the parent request so the EXISTING approval
// endpoints approve and resume it, hands escalations to the EXISTING
// governance → attention path, and publishes the controller's metadata-only
// facts on the EXISTING bus.
//
// It adds no policy evaluation, no approval rules and no authorization: the
// decision comes from governance.Engine, the approval rules from
// governance.ApprovalEngine, and the resume from core.ApproveRequest.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

// ErrActionProposalUnbound is a proposal whose parent request is not known, so
// it cannot be admitted: an approval has nothing to resume.
var ErrActionProposalUnbound = errors.New("core: action proposal has no bound request")

// actionApprovalIndex maps "requestID|fingerprint" → approval id. It is
// deliberately separate from approvalByReq (one task-level approval per
// request): one objective may need several distinct action approvals.
type actionApprovalIndex struct {
	mu sync.RWMutex
	m  map[string]string
}

func newActionApprovalIndex() *actionApprovalIndex {
	return &actionApprovalIndex{m: map[string]string{}}
}

func (i *actionApprovalIndex) key(requestID, fingerprint string) string {
	return requestID + "|" + fingerprint
}

func (i *actionApprovalIndex) put(requestID, fingerprint, approvalID string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.m[i.key(requestID, fingerprint)] = approvalID
}

func (i *actionApprovalIndex) get(requestID, fingerprint string) (string, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	id, ok := i.m[i.key(requestID, fingerprint)]
	return id, ok
}

func (i *actionApprovalIndex) drop(requestID, fingerprint string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.m, i.key(requestID, fingerprint))
}

// RequestApproval records an approval for an action-level decision and
// binds it to the parent request, so POST /api/v1/approvals/{id}/approve resumes
// the objective through the existing path. The approval record carries the
// proposal's own governance request, and the entry carries the fingerprint that
// binds the approval to exactly one action.
func (e *Engine) RequestApproval(p control.Proposal, decision governance.Decision) (string, error) {
	req := e.requestRef(proposalRequestKey(p))
	if req == nil {
		// An approval has nothing to resume without its parent request, so the
		// proposal is refused rather than recorded into the void.
		key := proposalRequestKey(p)
		if key == "" {
			key = "<unbound>"
		}
		return "", fmt.Errorf("%w: %s", ErrActionProposalUnbound, key)
	}
	// The entry is keyed by the CANONICAL request id: the resume path
	// (ApproveRequest) re-submits that request, whatever correlation the
	// proposal carried.
	requestID := req.ID

	fingerprint := p.Fingerprint()
	if id, found := e.actionApprovals.get(requestID, fingerprint); found {
		if ar := e.approvalRecord(id); ar != nil && ar.Status == governance.ApprovalStatePending {
			return id, nil
		}
	}

	config := governance.ApprovalConfig{}
	if decision.ApprovalRequired != nil {
		config = *decision.ApprovalRequired
	}
	ar, err := e.approvalEngine.RequestApproval(decision, p.GovernanceRequest(nil), config)
	if err != nil {
		return "", err
	}
	e.approvalMu.Lock()
	e.approvals[ar.DecisionID] = &approvalEntry{
		requestID:  requestID,
		businessID: p.BusinessID,
		req:        req,
		ar:         ar,
		// The fingerprint is the binding: a resume only satisfies THIS action.
		actionFingerprint: fingerprint,
	}
	e.approvalMu.Unlock()
	e.actionApprovals.put(requestID, fingerprint, ar.DecisionID)
	e.emitActionApprovalEvent(req, event.EventTypeApprovalRequested, ar, map[string]string{
		"action":      p.Action,
		"resource":    p.Resource,
		"tool_id":     p.ToolID,
		"operation":   p.Operation,
		"agent_id":    p.AgentID,
		"proposal_id": p.ProposalID,
		"gate":        "agent_action",
	})
	return ar.DecisionID, nil
}

// ApprovedState reports an approved action-level approval for exactly this
// proposal fingerprint. A missing, pending, denied, expired or differently
// bound record reports false, so governance re-evaluates the approval gate
// instead of trusting a flag (contract §5).
func (e *Engine) ApprovedState(p control.Proposal, fingerprint string) (governance.ApprovalState, bool) {
	// INV-16: a timed-out approval is not approvable, so a timed-out one is not
	// an approved one either. The sweep is the same one the decision path uses.
	e.expireApprovals()
	req := e.requestRef(proposalRequestKey(p))
	if req == nil {
		return "", false
	}
	id, ok := e.actionApprovals.get(req.ID, fingerprint)
	if !ok {
		return "", false
	}
	entry := e.approvalEntry(id)
	if entry == nil || entry.actionFingerprint != fingerprint || entry.requestID != req.ID {
		return "", false
	}
	return entry.ar.Status, true
}

// proposalRequestKey is the proposal field the parent request is looked up by:
// the correlation id of the admitted request (the execution id is the same value
// on every path the runtime establishes).
func proposalRequestKey(p control.Proposal) string {
	if p.CorrelationID != "" {
		return p.CorrelationID
	}
	return p.ExecutionID
}

// requestRef returns the parent request an action proposal belongs to.
func (e *Engine) requestRef(requestID string) *Request {
	e.requestRefsMu.RLock()
	defer e.requestRefsMu.RUnlock()
	return e.requestRefs[requestID]
}

// trackRequestRef indexes a request while it is admitted or in flight, so an
// action-level approval raised inside it has something to resume. It is a plain
// reference index: no lifecycle, no authorization, no durability (G4 — a
// restart loses it exactly like the approval records).
//
// The request is indexed under its own id AND its correlation id, because a
// proposal carries the correlation the runtime established while the resume
// path re-submits the canonical request id.
func (e *Engine) trackRequestRef(req *Request) {
	if req == nil {
		return
	}
	e.requestRefsMu.Lock()
	defer e.requestRefsMu.Unlock()
	e.requestRefs[req.ID] = req
	if req.Context != nil && req.Context.CorrelationID != "" && req.Context.CorrelationID != req.ID {
		e.requestRefs[req.Context.CorrelationID] = req
	}
}

// untrackRequestRef drops both index keys of a finished request.
func (e *Engine) untrackRequestRef(req *Request) {
	if req == nil {
		return
	}
	e.requestRefsMu.Lock()
	defer e.requestRefsMu.Unlock()
	delete(e.requestRefs, req.ID)
	if req.Context != nil && req.Context.CorrelationID != "" {
		delete(e.requestRefs, req.Context.CorrelationID)
	}
}

// approvalRecord returns the engine record for an approval id.
func (e *Engine) approvalRecord(approvalID string) *governance.ApprovalRequest {
	e.approvalMu.Lock()
	defer e.approvalMu.Unlock()
	entry := e.approvals[approvalID]
	if entry == nil {
		return nil
	}
	return entry.ar
}

// Escalate hands an ESCALATE decision to the existing escalation →
// attention path. The action stays blocked: this publishes a fact and queues a
// human-facing item, nothing more.
func (e *Engine) Escalate(p control.Proposal, reason string) (string, error) {
	ref := fmt.Sprintf("esc-action-%s-%d", p.ProposalID, e.now().UnixNano())

	ctx := map[string]string{
		"gate":        "agent_action",
		"action":      p.Action,
		"resource":    p.Resource,
		"proposal_id": p.ProposalID,
		"agent_id":    p.AgentID,
	}
	for k, v := range p.Context {
		if _, taken := ctx[k]; !taken {
			ctx[k] = v
		}
	}
	e.emitActionEscalation(p, ref, reason, ctx)
	return ref, nil
}

// Publish publishes a metadata-only governance fact on the existing
// bus. It is the controller's Publisher seam; no event here grants authority.
func (e *Engine) Publish(kind, correlationID, businessID string, fields map[string]string) {
	data, err := json.Marshal(fields)
	if err != nil {
		return
	}
	_ = e.eventBus.Publish(&event.Event{
		ID:            fmt.Sprintf("%s-%s-%d", correlationID, kind, e.now().UnixNano()),
		Type:          event.EventType(kind),
		Source:        "agent-governance",
		Timestamp:     e.now(),
		BusinessID:    businessID,
		CorrelationID: correlationID,
		Priority:      event.PriorityNormal,
		Data:          data,
	})
}

// emitActionEscalation publishes governance.escalated with the CTR-GOV-002
// payload shape the existing intake already consumes.
func (e *Engine) emitActionEscalation(p control.Proposal, ref, reason string, ctx map[string]string) {
	data, _ := json.Marshal(escalationPayload{
		EscalationRef: ref,
		RequestID:     p.CorrelationID,
		RequesterID:   p.ActorID,
		Reason:        reason,
		Gate:          "agent_action",
		Urgency:       strconv.Itoa(defaultEscalationUrgency),
		Deadline:      e.now().Add(defaultEscalationTTL).UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		Options:       escalationAlertOptions,
	})
	_ = e.eventBus.Publish(&event.Event{
		ID:            fmt.Sprintf("%s-%s", ref, event.EventTypeGovernanceEscalated),
		Type:          event.EventTypeGovernanceEscalated,
		Source:        "agent-governance",
		Timestamp:     e.now(),
		BusinessID:    p.BusinessID,
		CorrelationID: p.CorrelationID,
		Priority:      event.PriorityNormal,
		Data:          data,
	})
}

// emitActionApprovalEvent publishes an approval event for an action-level
// record, using the same envelope the task-level path emits.
func (e *Engine) emitActionApprovalEvent(req *Request, typ event.EventType,
	ar *governance.ApprovalRequest, extra map[string]string) {
	if req == nil || req.Context == nil {
		return
	}
	fields := map[string]string{
		"approval_id": ar.DecisionID,
		"requester":   ar.Requester,
		"status":      string(ar.Status),
	}
	for k, v := range extra {
		fields[k] = v
	}
	data, _ := json.Marshal(fields)
	_ = e.eventBus.Publish(&event.Event{
		ID:            fmt.Sprintf("%s-%s", ar.DecisionID, typ),
		Type:          typ,
		Source:        "agent-governance",
		Timestamp:     e.now(),
		BusinessID:    req.Context.BusinessID,
		CorrelationID: req.Context.CorrelationID,
		Priority:      event.PriorityNormal,
		Data:          data,
	})
}

// Controller builds the admission controller wired to THIS engine: the
// authoritative policy engine, this engine's approval index, this engine's
// escalation path and this engine's bus.
func (e *Engine) Controller() *control.Controller {
	return control.New(control.Options{
		Engine: e.govEngine, Now: e.now,
		Approver: e, Escalator: e, Publisher: e,
	})
}
