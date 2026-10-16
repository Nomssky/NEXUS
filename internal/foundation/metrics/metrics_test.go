package metrics

// Bounded operational metrics (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md
// §3, §5, §11). These tests pin the initial values, the exact increment
// semantics that separate logical executions from physical tool attempts, the
// closed vocabularies, nil-safety and race-free concurrent access. No sleeps are
// used as race proofs; everything must stay green under `go test -race`.

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFreshMetricsStartAtDocumentedInitialValues(t *testing.T) {
	// Obligation 1: a fresh owner reports the §3.0 initial values.
	m := New()
	if m == nil {
		t.Fatal("New must return an owner")
	}
	if got := m.Snapshot(); got != (Snapshot{}) {
		t.Fatalf("a fresh snapshot must be all zero, got %+v", got)
	}
	// The recovery timestamp is explicitly "unset", not an epoch timestamp.
	if got := m.Snapshot().LastRecoveryUnixSeconds; got != 0 {
		t.Fatalf("last recovery must start unset (0), got %d", got)
	}
}

func TestExecutionLifecycleCounters(t *testing.T) {
	m := New()

	// A newly admitted execution counts as accepted and is in flight.
	m.ExecutionAccepted()
	if s := m.Snapshot(); s.ExecutionsAccepted != 1 || s.ExecutionsInFlight != 1 {
		t.Fatalf("accepted=%d in_flight=%d, want 1/1", s.ExecutionsAccepted, s.ExecutionsInFlight)
	}
	m.TerminalOutcome("completed")
	m.ExecutionFinished()
	if s := m.Snapshot(); s.ExecutionsCompleted != 1 || s.ExecutionsInFlight != 0 {
		t.Fatalf("completed=%d in_flight=%d, want 1/0", s.ExecutionsCompleted, s.ExecutionsInFlight)
	}

	// A resume is a second run of the same execution: it counts as a resume and
	// opens a new in-flight interval, but never as a new accepted execution.
	m.ExecutionResumed()
	if s := m.Snapshot(); s.ApprovalResumes != 1 || s.ExecutionsAccepted != 1 || s.ExecutionsInFlight != 1 {
		t.Fatalf("resumes=%d accepted=%d in_flight=%d, want 1/1/1",
			s.ApprovalResumes, s.ExecutionsAccepted, s.ExecutionsInFlight)
	}
	m.TerminalOutcome("completed")
	m.ExecutionFinished()
	if s := m.Snapshot(); s.ExecutionsCompleted != 2 || s.ExecutionsInFlight != 0 {
		t.Fatalf("completed=%d in_flight=%d, want 2/0", s.ExecutionsCompleted, s.ExecutionsInFlight)
	}
}

// Obligation 4: terminal counters reflect the actual terminal result.
func TestTerminalOutcomeUsesExactClosedVocabulary(t *testing.T) {
	m := New()
	for _, s := range []string{"completed", "failed", "cancelled"} {
		m.TerminalOutcome(s)
	}
	// An unrecognized status must NOT be folded into a real bucket (§6.7:
	// missing instrumentation is not a zero, and an unknown status is not
	// invented evidence).
	m.TerminalOutcome("bogus")
	m.TerminalOutcome("")
	m.TerminalOutcome("COMPLETED") // case matters: not a match

	snap := m.Snapshot()
	if snap.ExecutionsCompleted != 1 || snap.ExecutionsFailed != 1 || snap.ExecutionsCancelled != 1 {
		t.Fatalf("closed vocabulary not honored: %+v", snap)
	}
	total := snap.ExecutionsCompleted + snap.ExecutionsFailed + snap.ExecutionsCancelled
	if total != 3 {
		t.Fatalf("an unknown status was mis-bucketed: total=%d", total)
	}
}

func TestTerminalErrorClassVocabularyIsClosed(t *testing.T) {
	m := New()
	m.TerminalErrorClass("APPROVAL_REQUIRED")
	m.TerminalErrorClass("POLICY_DENIED")
	m.TerminalErrorClass("ESCALATION_REQUIRED")
	m.TerminalErrorClass("DEADLINE_EXCEEDED")
	// Anything else — including a code that does not exist yet, or a raw error
	// string — lands in the bounded "other" bucket and creates no new series.
	m.TerminalErrorClass("SOMETHING_NEW")
	m.TerminalErrorClass("tool arguments leaked here: /etc/passwd")

	snap := m.Snapshot()
	if snap.TerminalApprovalRequired != 1 || snap.TerminalPolicyDenied != 1 ||
		snap.TerminalEscalationReq != 1 || snap.TerminalDeadlineExceeded != 1 ||
		snap.TerminalOtherFailure != 2 {
		t.Fatalf("classification is wrong: %+v", snap)
	}
}

// Obligation 2 + 3: logical calls, physical attempts and retries are separate,
// and a retry never increments the logical call count.
func TestToolAttemptsAndRetriesAreDistinctFromLogicalCalls(t *testing.T) {
	m := New()

	// One logical call that took three physical attempts.
	m.ToolCall()
	m.ToolAttempts(3)
	m.ToolOutcome("completed")

	// A second logical call that succeeded on the first attempt.
	m.ToolCall()
	m.ToolAttempts(1)
	m.ToolOutcome("completed")

	snap := m.Snapshot()
	if snap.ToolCalls != 2 {
		t.Fatalf("logical calls = %d, want 2 (a retry must never add a call)", snap.ToolCalls)
	}
	if snap.ToolAttempts != 4 {
		t.Fatalf("physical attempts = %d, want 4", snap.ToolAttempts)
	}
	if snap.ToolRetries != 2 {
		t.Fatalf("retries = %d, want 2 (attempts beyond the first)", snap.ToolRetries)
	}
	if snap.ToolCompleted != 2 {
		t.Fatalf("completed = %d, want 2", snap.ToolCompleted)
	}
}

func TestToolAttemptsIgnoresNonPositive(t *testing.T) {
	m := New()
	m.ToolAttempts(0)
	m.ToolAttempts(-5)
	if s := m.Snapshot(); s.ToolAttempts != 0 || s.ToolRetries != 0 {
		t.Fatalf("non-positive attempts must be ignored: %+v", s)
	}
}

// Obligation 7: an unknown outcome is never counted as completed.
func TestUnknownToolOutcomeIsNeverCountedAsCompleted(t *testing.T) {
	m := New()
	m.ToolCall()
	m.ToolAttempts(1)
	m.ToolOutcome("unknown")

	snap := m.Snapshot()
	if snap.ToolUnknown != 1 {
		t.Fatalf("unknown = %d, want 1", snap.ToolUnknown)
	}
	if snap.ToolCompleted != 0 {
		t.Fatalf("unknown must never be counted as completed, got %d", snap.ToolCompleted)
	}
	// Nor may it be silently dropped: the call still happened.
	if snap.ToolCalls != 1 {
		t.Fatalf("the logical call must still be counted, got %d", snap.ToolCalls)
	}
}

func TestToolOutcomeUsesExactClosedVocabulary(t *testing.T) {
	m := New()
	m.ToolOutcome("completed")
	m.ToolOutcome("failed")
	m.ToolOutcome("unknown")
	m.ToolOutcome("timed_out")
	m.ToolOutcome("cancelled")
	// Not a member of the reliability contract's outcome vocabulary.
	m.ToolOutcome("weird")

	snap := m.Snapshot()
	if snap.ToolCompleted != 1 || snap.ToolFailed != 1 || snap.ToolUnknown != 1 ||
		snap.ToolTimedOut != 1 || snap.ToolCancelled != 1 {
		t.Fatalf("tool outcomes are wrong: %+v", snap)
	}
}

func TestCancellationCounters(t *testing.T) {
	m := New()
	m.CancellationRequested()
	m.CancellationRefused()
	m.CancellationRequested()
	m.CancellationWon()

	snap := m.Snapshot()
	if snap.CancellationsRequested != 2 || snap.CancellationsWon != 1 || snap.CancellationsRefused != 1 {
		t.Fatalf("cancellation counters are wrong: %+v", snap)
	}
	// A refused cancellation is never a won one.
	if snap.CancellationsWon != 1 {
		t.Fatalf("refusals must not be counted as wins, got %d", snap.CancellationsWon)
	}
}

func TestJournalWriteFailures(t *testing.T) {
	m := New()
	m.JournalWriteFailure()
	m.JournalWriteFailure()
	if got := m.Snapshot().JournalWriteFailures; got != 2 {
		t.Fatalf("write failures = %d, want 2", got)
	}
}

// Obligation 8: recovery counters reflect exactly what recovery classified.
func TestRecoveryCompletedRecordsExactlyTheClassifiedCounts(t *testing.T) {
	m := New()
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	m.RecoveryCompleted(120, 118, 2, 3, at)

	snap := m.Snapshot()
	if snap.RecoveryExecutionsScanned != 120 || snap.RecoveryTerminal != 118 ||
		snap.RecoveryClassifiedUnknown != 2 || snap.RecoveryUnknownInvocations != 3 {
		t.Fatalf("recovery counts are wrong: %+v", snap)
	}
	if want := at.Unix(); snap.LastRecoveryUnixSeconds != want {
		t.Fatalf("last recovery = %d, want %d", snap.LastRecoveryUnixSeconds, want)
	}
	// Before any pass the timestamp is unset, not an epoch value.
	fresh := New().Snapshot()
	if fresh.LastRecoveryUnixSeconds != 0 {
		t.Fatalf("unset recovery timestamp must be 0, got %d", fresh.LastRecoveryUnixSeconds)
	}
}

func TestRecoveryFailedCountsFailClosedPasses(t *testing.T) {
	m := New()
	m.RecoveryFailed()
	m.RecoveryFailed()
	if got := m.Snapshot().RecoveryFailures; got != 2 {
		t.Fatalf("recovery failures = %d, want 2", got)
	}
	// A failed pass establishes no successful recovery timestamp.
	if got := m.Snapshot().LastRecoveryUnixSeconds; got != 0 {
		t.Fatalf("a failed pass must not set the recovery timestamp, got %d", got)
	}
}

// Obligation 14: a nil metrics owner is a safe no-op — instrumentation can never
// block or fail the caller.
func TestNilMetricsIsSafeNoOp(t *testing.T) {
	var m *Metrics
	m.ExecutionAccepted()
	m.ExecutionResumed()
	m.ExecutionFinished()
	m.TerminalOutcome("completed")
	m.TerminalErrorClass("POLICY_DENIED")
	m.ApprovalPaused()
	m.CancellationRequested()
	m.CancellationWon()
	m.CancellationRefused()
	m.ToolCall()
	m.ToolAttempts(3)
	m.ToolOutcome("unknown")
	m.JournalWriteFailure()
	m.RecoveryFailed()
	m.RecoveryCompleted(1, 1, 0, 0, time.Now())

	if got := m.Snapshot(); got != (Snapshot{}) {
		t.Fatalf("a nil owner must read as all zero, got %+v", got)
	}
}

// Obligation 11: high-cardinality input cannot create high-cardinality series.
// The structural guarantee is that Snapshot has a fixed field set and no API
// accepts a label; this test asserts both the constant field count and that a
// flood of distinct user values leaves the shape unchanged.
func TestHighCardinalityInputCannotGrowTheSeriesSet(t *testing.T) {
	m := New()
	for i := 0; i < 5000; i++ {
		id := string(rune('a'+i%26)) + strings.Repeat("x", i%97) + "-" + itoa(i)
		m.ExecutionAccepted()
		m.ToolCall()
		m.ToolAttempts(int64(i%3) + 1)
		m.ToolOutcome([]string{"completed", "failed", "unknown"}[i%3])
		m.TerminalOutcome([]string{"completed", "failed", "cancelled"}[i%3])
		m.TerminalErrorClass([]string{"POLICY_DENIED", "NEW_CODE_" + id}[i%2])
		m.ExecutionFinished()
	}

	// The serialized shape is constant: a fixed set of flat numeric fields.
	raw, err := json.Marshal(m.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("the snapshot must be a flat JSON object: %v", err)
	}
	// 15 execution + 8 tools + 7 journal.
	const wantFields = 30
	if len(decoded) != wantFields {
		t.Fatalf("series count changed: got %d fields, want %d", len(decoded), wantFields)
	}
	for k, v := range decoded {
		if _, ok := v.(float64); !ok {
			t.Fatalf("field %q must be a number (no labels, no strings), got %T", k, v)
		}
	}
	// No user input leaked into any field name.
	if strings.Contains(string(raw), "NEW_CODE") {
		t.Fatal("user-controlled text leaked into the metric shape")
	}
}

// Obligation 12: concurrent updates and reads are race-safe. This is a race
// detector assertion, not a timing assertion.
func TestConcurrentUpdatesAndReadsAreRaceSafe(t *testing.T) {
	m := New()
	const goroutines = 16
	const iterations = 200

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m.ExecutionAccepted()
				m.TerminalOutcome([]string{"completed", "failed", "cancelled"}[(g+i)%3])
				m.TerminalErrorClass([]string{"POLICY_DENIED", "APPROVAL_REQUIRED", "OTHER"}[(g+i)%3])
				m.ExecutionFinished()
				m.ToolCall()
				m.ToolAttempts(int64((g+i)%3) + 1)
				m.ToolOutcome([]string{"completed", "failed", "unknown"}[(g+i)%3])
				m.JournalWriteFailure()
				m.CancellationRequested()
			}
		}(g)
	}
	// Readers run concurrently with the writers and are released by closing
	// stop once the writers have finished. Synchronization is deterministic:
	// no sleep is used as a race proof.
	var readers sync.WaitGroup
	for g := 0; g < 4; g++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = m.Snapshot()
			}
		}()
	}
	wg.Wait()
	close(stop)
	readers.Wait()

	snap := m.Snapshot()
	want := int64(goroutines * iterations)
	if snap.ExecutionsAccepted != want {
		t.Fatalf("accepted = %d, want %d", snap.ExecutionsAccepted, want)
	}
	if snap.ToolCalls != want {
		t.Fatalf("tool calls = %d, want %d", snap.ToolCalls, want)
	}
	// In-flight is balanced: every accepted run was finished.
	if snap.ExecutionsInFlight != 0 {
		t.Fatalf("in_flight = %d, want 0", snap.ExecutionsInFlight)
	}
	if snap.ExecutionsCompleted+snap.ExecutionsFailed+snap.ExecutionsCancelled != want {
		t.Fatalf("terminal outcomes must total %d, got %d",
			want, snap.ExecutionsCompleted+snap.ExecutionsFailed+snap.ExecutionsCancelled)
	}
}

// Obligation 13: two independent owners never share state.
func TestIndependentOwnersDoNotShareValues(t *testing.T) {
	a, b := New(), New()
	a.ExecutionAccepted()
	a.ToolCall()
	a.ToolOutcome("unknown")

	if got := b.Snapshot(); got != (Snapshot{}) {
		t.Fatalf("a second owner must start empty, got %+v", got)
	}
	if a.Snapshot().ExecutionsAccepted != 1 || b.Snapshot().ExecutionsAccepted != 0 {
		t.Fatal("metric state leaked between owners")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
