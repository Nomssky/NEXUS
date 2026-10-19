package core

import (
	"errors"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

// journalState maps an engine terminal Response status to the durable journal
// state. The engine writes only "completed", "failed" and "cancelled"
// (approval/escalation land on failed with their own error code); anything
// unexpected is recorded as failed rather than fabricated as success.
func journalState(result *Response) execjournal.State {
	if result == nil {
		return execjournal.StateFailed
	}
	switch result.Status {
	case "completed":
		return execjournal.StateCompleted
	case "cancelled":
		return execjournal.StateCancelled
	default:
		return execjournal.StateFailed
	}
}

// journalErrorClass returns the coarse error code for the durable record (e.g.
// "APPROVAL_REQUIRED", "POLICY_DENIED"), never the raw message.
func journalErrorClass(result *Response) string {
	if result == nil || result.Error == nil {
		return ""
	}
	return result.Error.Code
}

// approvalIDOf returns the approval a paused execution is waiting on.
func approvalIDOf(result *Response) string {
	if result == nil || result.Error == nil || result.Error.Details == nil {
		return ""
	}
	return result.Error.Details["approval_id"]
}

// journalFinalize durably records how a finished run ends.
//
// An approval pause is NOT the final business outcome. It is recorded as the
// provisional terminal failed/APPROVAL_REQUIRED (the D2 encoding) and the
// request for approval is appended to the execution's durable approval history.
//
// Any other end is the real outcome. When the execution was parked on an
// approval pause, that real outcome supersedes the pause, so a resumed
// execution that completes is durably completed instead of staying failed
// forever. Every other execution takes the ordinary first-wins Terminal path,
// whose semantics are unchanged.
func (e *Engine) journalFinalize(id string, result *Response) {
	if e.journal == nil {
		return
	}
	state, class := journalState(result), journalErrorClass(result)

	if isApprovalWaiting(result) {
		if terr := e.journal.Terminal(id, state, class); terr != nil {
			e.metrics.JournalWriteFailure()
			return
		}
		// The approval request is durable evidence in its own right, and must
		// still be readable once the execution reaches its final outcome.
		if aerr := e.journal.RecordApproval(id, approvalIDOf(result),
			execjournal.ApprovalRequested, ""); aerr != nil {
			e.metrics.JournalWriteFailure()
		}
		return
	}

	// A real outcome. ErrNotApprovalPause only means this execution was not
	// parked on a pause, which is the ordinary case.
	if serr := e.journal.SupersedeApprovedPause(id, state, class); serr == nil {
		return
	} else if !errors.Is(serr, execjournal.ErrNotApprovalPause) {
		e.metrics.JournalWriteFailure()
		return
	}
	if terr := e.journal.Terminal(id, state, class); terr != nil {
		e.metrics.JournalWriteFailure()
	}
}
