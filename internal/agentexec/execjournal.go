package agentexec

import "github.com/Nomssky/NEXUS/internal/execjournal"

// invocationStatus maps a platform outcome to the durable invocation status.
// An unrecognized outcome is recorded as failed, never fabricated as completed;
// "unknown" is preserved as unknown.
func invocationStatus(outcome string) string {
	switch outcome {
	case "completed":
		return execjournal.InvCompleted
	case "unknown":
		return execjournal.InvUnknown
	default:
		return execjournal.InvFailed
	}
}
