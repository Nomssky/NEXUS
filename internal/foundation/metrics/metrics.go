// Package metrics is the process-local, bounded operational metrics owner
// (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md).
//
// Every series has a FIXED name and a closed label vocabulary, so cardinality
// cannot grow with executions, requests, identities, business/division ids,
// tool arguments, URLs or any other user input. There are no dynamic labels and
// no map keyed by user strings. Updates and reads are race-safe (atomics); a nil
// *Metrics is a safe no-op, so instrumentation can never block execution,
// approval, cancellation, dispatch, journal persistence or recovery.
//
// Nothing here is durable. Counters and gauges are process-local and reset at
// process start; they are not journal evidence and must never be presented as
// counts of durable records.
package metrics

import (
	"sync/atomic"
	"time"
)

// Metrics is the operational metrics owner. Construct one per process (or per
// server instance in tests) and inject it; never use a package-global value.
type Metrics struct {
	// Execution lifecycle (logical executions, not requests or tool attempts).
	executionsAccepted  atomic.Int64
	executionsInFlight  atomic.Int64
	executionsCompleted atomic.Int64
	executionsFailed    atomic.Int64
	executionsCancelled atomic.Int64

	// Approval lifecycle.
	approvalPauses  atomic.Int64
	approvalResumes atomic.Int64

	// Cancellation lifecycle.
	cancellationsRequested atomic.Int64
	cancellationsWon       atomic.Int64
	cancellationsRefused   atomic.Int64

	// Terminal outcome classification for FAILED executions (bounded codes).
	terminalApprovalReq  atomic.Int64
	terminalPolicyDenied atomic.Int64
	terminalEscalation   atomic.Int64
	terminalDeadline     atomic.Int64
	terminalOtherFailure atomic.Int64

	// Tool invocation (logical calls, physical attempts, bounded outcomes).
	toolCalls     atomic.Int64
	toolAttempts  atomic.Int64
	toolRetries   atomic.Int64
	toolCompleted atomic.Int64
	toolFailed    atomic.Int64
	toolUnknown   atomic.Int64
	toolTimedOut  atomic.Int64
	toolCancelled atomic.Int64

	// Durable journal (write-path failures only; counts are collected offline).
	journalWriteFailures atomic.Int64

	// Startup recovery outcomes (set once per successful pass).
	recoveryScanned     atomic.Int64
	recoveryTerminal    atomic.Int64
	recoveryUnknown     atomic.Int64
	recoveryUnknownInvs atomic.Int64
	recoveryFailures    atomic.Int64
	lastRecoveryUnix    atomic.Int64
}

// New returns a zeroed metrics owner. Every series starts at its documented
// initial value (all counters and gauges zero; the recovery timestamp unset) —
// see contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md §3.
func New() *Metrics { return &Metrics{} }

// --- execution lifecycle ---

// ExecutionAccepted counts a newly submitted, admitted execution and opens its
// in-flight interval. It is the durable-acceptance success path: an execution
// whose durable acceptance failed is not counted here.
func (m *Metrics) ExecutionAccepted() {
	if m == nil {
		return
	}
	m.executionsAccepted.Add(1)
	m.executionsInFlight.Add(1)
}

// ExecutionResumed counts an admitted approval-resume run and opens its
// in-flight interval (the continuation is a new in-flight run of the same
// execution).
func (m *Metrics) ExecutionResumed() {
	if m == nil {
		return
	}
	m.approvalResumes.Add(1)
	m.executionsInFlight.Add(1)
}

// ExecutionFinished closes one in-flight interval. Every admitted run (initial
// or resume) that reaches a stored terminal result calls this exactly once.
func (m *Metrics) ExecutionFinished() {
	if m == nil {
		return
	}
	m.executionsInFlight.Add(-1)
}

// TerminalOutcome records the stored terminal status of an execution. Only the
// exact, closed vocabulary is counted; an unrecognized status is ignored rather
// than folded into a misleading bucket.
func (m *Metrics) TerminalOutcome(status string) {
	if m == nil {
		return
	}
	switch status {
	case "completed":
		m.executionsCompleted.Add(1)
	case "failed":
		m.executionsFailed.Add(1)
	case "cancelled":
		m.executionsCancelled.Add(1)
	}
}

// ApprovalPaused counts an execution that stopped at a stored APPROVAL_REQUIRED
// result (a genuine approval wait, task- or action-level).
func (m *Metrics) ApprovalPaused() {
	if m == nil {
		return
	}
	m.approvalPauses.Add(1)
}

// --- cancellation lifecycle ---

// CancellationRequested counts every CancelRequest call.
func (m *Metrics) CancellationRequested() {
	if m == nil {
		return
	}
	m.cancellationsRequested.Add(1)
}

// CancellationWon counts a cancellation that actually took effect: the wait was
// replaced by a stored cancelled result, or an executing task's cancellation
// was confirmed. A request that lost to an existing terminal result is NOT
// counted here.
func (m *Metrics) CancellationWon() {
	if m == nil {
		return
	}
	m.cancellationsWon.Add(1)
}

// CancellationRefused counts a cancellation the engine refused because the
// request was already terminal, unknown, or out of scope.
func (m *Metrics) CancellationRefused() {
	if m == nil {
		return
	}
	m.cancellationsRefused.Add(1)
}

// --- terminal error classification (FAILED executions, bounded codes) ---

// TerminalErrorClass records the coarse error class of a FAILED execution. The
// vocabulary is closed; any other code is counted as "other", so a new code can
// never explode cardinality.
func (m *Metrics) TerminalErrorClass(class string) {
	if m == nil {
		return
	}
	switch class {
	case "APPROVAL_REQUIRED":
		m.terminalApprovalReq.Add(1)
	case "POLICY_DENIED":
		m.terminalPolicyDenied.Add(1)
	case "ESCALATION_REQUIRED":
		m.terminalEscalation.Add(1)
	case "DEADLINE_EXCEEDED":
		m.terminalDeadline.Add(1)
	default:
		m.terminalOtherFailure.Add(1)
	}
}

// --- tool invocation ---

// ToolCall counts one LOGICAL tool invocation (one dispatch of a logical call),
// regardless of how many physical attempts it makes.
func (m *Metrics) ToolCall() {
	if m == nil {
		return
	}
	m.toolCalls.Add(1)
}

// ToolAttempts records the physical attempts a logical call made. Every attempt
// beyond the first is a retry; a retry never increments ToolCall.
func (m *Metrics) ToolAttempts(n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.toolAttempts.Add(n)
	if n > 1 {
		m.toolRetries.Add(n - 1)
	}
}

// ToolOutcome records the terminal outcome of a logical tool call. The closed
// vocabulary is {completed, failed, unknown, timed_out, cancelled}; anything
// else is ignored rather than mis-bucketed. `unknown` is never counted as
// completed.
func (m *Metrics) ToolOutcome(outcome string) {
	if m == nil {
		return
	}
	switch outcome {
	case "completed":
		m.toolCompleted.Add(1)
	case "failed":
		m.toolFailed.Add(1)
	case "unknown":
		m.toolUnknown.Add(1)
	case "timed_out":
		m.toolTimedOut.Add(1)
	case "cancelled":
		m.toolCancelled.Add(1)
	}
}

// --- durable journal write path ---

// JournalWriteFailure counts a durable journal write that failed. The runtime
// treats a failed terminal/dispatch-outcome write as best-effort (recovery
// classifies the record unknown); this counter makes the failure visible without
// changing execution semantics.
func (m *Metrics) JournalWriteFailure() {
	if m == nil {
		return
	}
	m.journalWriteFailures.Add(1)
}

// --- startup recovery ---

// RecoveryCompleted records one successful recovery pass and the counts it
// classified, plus the timestamp at which it completed.
func (m *Metrics) RecoveryCompleted(scanned, terminal, classifiedUnknown, unknownInvocations int64, at time.Time) {
	if m == nil {
		return
	}
	m.recoveryScanned.Store(scanned)
	m.recoveryTerminal.Store(terminal)
	m.recoveryUnknown.Store(classifiedUnknown)
	m.recoveryUnknownInvs.Store(unknownInvocations)
	m.lastRecoveryUnix.Store(at.UTC().Unix())
}

// RecoveryFailed counts a recovery pass that failed closed (startup aborts).
func (m *Metrics) RecoveryFailed() {
	if m == nil {
		return
	}
	m.recoveryFailures.Add(1)
}

// Snapshot is an immutable, bounded view of the metrics. Field set and names are
// fixed; there are no dynamic labels.
type Snapshot struct {
	ExecutionsAccepted  int64 `json:"executions_accepted"`
	ExecutionsInFlight  int64 `json:"executions_in_flight"`
	ExecutionsCompleted int64 `json:"executions_completed"`
	ExecutionsFailed    int64 `json:"executions_failed"`
	ExecutionsCancelled int64 `json:"executions_cancelled"`

	ApprovalPauses  int64 `json:"approval_pauses"`
	ApprovalResumes int64 `json:"approval_resumes"`

	CancellationsRequested int64 `json:"cancellations_requested"`
	CancellationsWon       int64 `json:"cancellations_won"`
	CancellationsRefused   int64 `json:"cancellations_refused"`

	TerminalApprovalRequired int64 `json:"terminal_approval_required"`
	TerminalPolicyDenied     int64 `json:"terminal_policy_denied"`
	TerminalEscalationReq    int64 `json:"terminal_escalation_required"`
	TerminalDeadlineExceeded int64 `json:"terminal_deadline_exceeded"`
	TerminalOtherFailure     int64 `json:"terminal_other_failure"`

	ToolCalls     int64 `json:"tool_calls"`
	ToolAttempts  int64 `json:"tool_attempts"`
	ToolRetries   int64 `json:"tool_retries"`
	ToolCompleted int64 `json:"tool_completed"`
	ToolFailed    int64 `json:"tool_failed"`
	ToolUnknown   int64 `json:"tool_unknown"`
	ToolTimedOut  int64 `json:"tool_timed_out"`
	ToolCancelled int64 `json:"tool_cancelled"`

	JournalWriteFailures int64 `json:"journal_write_failures"`

	RecoveryExecutionsScanned  int64 `json:"recovery_executions_scanned"`
	RecoveryTerminal           int64 `json:"recovery_terminal"`
	RecoveryClassifiedUnknown  int64 `json:"recovery_classified_unknown"`
	RecoveryUnknownInvocations int64 `json:"recovery_unknown_invocations"`
	RecoveryFailures           int64 `json:"recovery_failures"`
	LastRecoveryUnixSeconds    int64 `json:"last_recovery_unix_seconds"`
}

// Snapshot returns a race-safe, bounded point-in-time view. It performs no I/O
// and never touches the store, so it cannot recursively depend on the subsystem
// it is diagnosing.
func (m *Metrics) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	return Snapshot{
		ExecutionsAccepted:  m.executionsAccepted.Load(),
		ExecutionsInFlight:  m.executionsInFlight.Load(),
		ExecutionsCompleted: m.executionsCompleted.Load(),
		ExecutionsFailed:    m.executionsFailed.Load(),
		ExecutionsCancelled: m.executionsCancelled.Load(),

		ApprovalPauses:  m.approvalPauses.Load(),
		ApprovalResumes: m.approvalResumes.Load(),

		CancellationsRequested: m.cancellationsRequested.Load(),
		CancellationsWon:       m.cancellationsWon.Load(),
		CancellationsRefused:   m.cancellationsRefused.Load(),

		TerminalApprovalRequired: m.terminalApprovalReq.Load(),
		TerminalPolicyDenied:     m.terminalPolicyDenied.Load(),
		TerminalEscalationReq:    m.terminalEscalation.Load(),
		TerminalDeadlineExceeded: m.terminalDeadline.Load(),
		TerminalOtherFailure:     m.terminalOtherFailure.Load(),

		ToolCalls:     m.toolCalls.Load(),
		ToolAttempts:  m.toolAttempts.Load(),
		ToolRetries:   m.toolRetries.Load(),
		ToolCompleted: m.toolCompleted.Load(),
		ToolFailed:    m.toolFailed.Load(),
		ToolUnknown:   m.toolUnknown.Load(),
		ToolTimedOut:  m.toolTimedOut.Load(),
		ToolCancelled: m.toolCancelled.Load(),

		JournalWriteFailures: m.journalWriteFailures.Load(),

		RecoveryExecutionsScanned:  m.recoveryScanned.Load(),
		RecoveryTerminal:           m.recoveryTerminal.Load(),
		RecoveryClassifiedUnknown:  m.recoveryUnknown.Load(),
		RecoveryUnknownInvocations: m.recoveryUnknownInvs.Load(),
		RecoveryFailures:           m.recoveryFailures.Load(),
		LastRecoveryUnixSeconds:    m.lastRecoveryUnix.Load(),
	}
}
