package core

import (
	"github.com/Nomssky/NEXUS/internal/control"
)

// Approval waiting-state lifecycle (contracts/APPROVAL_LIFECYCLE_CONTRACTS.md).
//
// The engine owns three process-local facts about waiting work, all guarded
// by approvalMu (the existing leaf lock — never held while acquiring
// e.mu/resultsMu/inflightMu):
//
//  1. the approval index (approvals/approvalByReq/actionApprovals),
//  2. the cancelled-request set below: once cancelled, a request stays
//     cancelled, so a late approval can never revive it,
//  3. nothing durable: restart drops all three together (G4), so no
//     half-state can authorize an effect after a crash.

// maxCancelledRequests bounds the cancelled-request set. Entries are never
// removed except by FIFO eviction: request IDs are unique, so a stale entry
// can only refuse a resume that could never be valid anyway.
const maxCancelledRequests = 4096

// markCancelledReq records a terminal cancellation. Call only with approvalMu
// NOT held.
func (e *Engine) markCancelledReq(requestID string) {
	if requestID == "" {
		return
	}
	e.approvalMu.Lock()
	defer e.approvalMu.Unlock()
	if _, ok := e.cancelledReqs[requestID]; ok {
		return
	}
	e.cancelledReqs[requestID] = struct{}{}
	e.cancelledOrder = append(e.cancelledOrder, requestID)
	for len(e.cancelledOrder) > maxCancelledRequests {
		delete(e.cancelledReqs, e.cancelledOrder[0])
		e.cancelledOrder = e.cancelledOrder[1:]
	}
}

// wasCancelledReq reports a recorded terminal cancellation. Call only with
// approvalMu NOT held.
func (e *Engine) wasCancelledReq(requestID string) bool {
	e.approvalMu.Lock()
	defer e.approvalMu.Unlock()
	_, ok := e.cancelledReqs[requestID]
	return ok
}

// invalidateApprovalsForRequest drops every approval index entry bound to
// requestID and revokes the underlying engine records without recording a
// decision (revocation is not a denial by anyone). After this returns, no
// path can decide or consume an approval for the request: ApproveRequest
// finds nothing (404) and re-admission finds no applicable approval.
// Best-effort on the engine records: a concurrently decided record reports
// NotFound and is simply already gone.
func (e *Engine) invalidateApprovalsForRequest(requestID string) {
	if requestID == "" {
		return
	}
	e.approvalMu.Lock()
	var ids []string
	for id, entry := range e.approvals {
		if entry != nil && entry.requestID == requestID {
			ids = append(ids, id)
			delete(e.approvals, id)
		}
	}
	if id, ok := e.approvalByReq[requestID]; ok {
		ids = append(ids, id)
		delete(e.approvalByReq, requestID)
	}
	e.approvalMu.Unlock()
	for _, id := range ids {
		_ = e.approvalEngine.Revoke(id)
	}
	// Fingerprint index entries whose record is gone need no cleanup:
	// ApprovedState re-checks the record behind every entry and reports
	// false for a missing one, and resume cleanup drops the rest. A stale
	// entry can only make governance re-evaluate, never authorize.
}

// storeCancelledResult records the terminal cancelled outcome for a request
// whose wait is being cancelled. It is a compare-and-set against the in-memory
// results map: cancellation may replace ONLY a waiting (or absent) result.
// A committed terminal that landed after the caller observed the wait — for
// example a resume that finished committing its effect — is left untouched, so
// a late cancellation never overwrites a committed terminal
// (APPROVAL_LIFECYCLE §8: an already-dispatched effect reports honestly).
// Reports whether the cancelled result was stored.
//
// The cancelled shape mirrors chainCancelled (Status cancelled with a CANCELLED
// error) and the FIFO bookkeeping mirrors the processRequests store path; a
// move from waiting→cancelled does not add a second FIFO position for the id.
func (e *Engine) storeCancelledResult(requestID, businessID, divisionID string) bool {
	res := &Response{
		RequestID:  requestID,
		BusinessID: businessID,
		DivisionID: divisionID,
		Status:     "cancelled",
		Error: &ChainError{
			Code: "CANCELLED", Category: "CANCELLATION",
			Message:   "cancelled while waiting for approval",
			ChainStep: string(StepAgent), Retryable: false,
		},
		AuditTrail: []AuditEntry{{
			Step:      "cancel",
			Action:    "cancel waiting execution",
			Actor:     "core",
			Timestamp: e.now(),
			Outcome:   "cancelled",
		}},
	}
	e.resultsMu.Lock()
	defer e.resultsMu.Unlock()
	if cur, ok := e.results[requestID]; ok {
		if !isApprovalWaiting(cur) {
			return false // a committed terminal already won; never overwrite it
		}
	} else {
		e.resultOrder = append(e.resultOrder, requestID)
	}
	e.results[requestID] = res
	for len(e.resultOrder) > maxResults {
		delete(e.results, e.resultOrder[0])
		e.resultOrder = e.resultOrder[1:]
	}
	return true
}

// cancelWaitingExecution arbitrates a cancellation against a stored waiting
// result. Cancellation wins only while the wait is still the live terminal
// result: it stores the cancelled result, invalidates every approval bound to
// the request, and records the terminal cancellation so a later approve or
// resume never revives it (APPROVAL_LIFECYCLE §8). If a committed terminal
// landed first, the cancellation does not overwrite it and the caller
// arbitrates against the committed status instead.
func (e *Engine) cancelWaitingExecution(requestID, businessID, divisionID string) error {
	if !e.storeCancelledResult(requestID, businessID, divisionID) {
		if cur, ok := e.GetResult(requestID); ok {
			return terminalCancelError(cur.Status)
		}
		// The waiting result was removed and not yet replaced: a resume was
		// re-admitted concurrently. Report the completion race honestly rather
		// than claim a cancellation that did not take effect.
		return ErrCompletionRace
	}
	e.invalidateApprovalsForRequest(requestID)
	e.markCancelledReq(requestID)
	// Operational metrics (v15): the cancellation took effect. The cancelled
	// result is committed here and ONLY here, so this is the single site that
	// records its terminal status. No run is in flight at this point (the wait
	// ended in a previous terminal store), so the in-flight gauge is untouched.
	e.metrics.CancellationWon()
	e.metrics.TerminalOutcome("cancelled")
	return nil
}

// cancelGuardErr maps a cancelled request to the sentinel the admission and
// chain paths recognize: cancellation is reported, never mislabeled as a
// policy denial.
func cancelGuardErr() error {
	return control.ErrRequestCancelled
}
