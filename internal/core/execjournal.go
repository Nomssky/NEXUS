package core

import "github.com/Nomssky/NEXUS/internal/execjournal"

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
