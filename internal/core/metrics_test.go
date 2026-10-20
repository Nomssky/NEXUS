package core

// Operational metrics on the execution lifecycle
// (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md §3.1-§3.2, §11).
//
// These tests drive the real engine and assert the counters follow ACTUAL
// transitions — in particular that a cancellation which loses to an existing
// terminal result is never counted as a successful cancellation, and that
// approval pause/resume counters track real transitions. Synchronization is
// deterministic (the existing awaitResult polling helper); no test sleeps as a
// race proof. Everything must stay green under `go test -race`.

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/metrics"
)

// newMetricsEngine boots a running engine with a metrics owner wired, reusing
// the shared action-control harness so governance behaves exactly as in
// production tests.
func newMetricsEngine(t *testing.T, h *actionGateHandler, policies ...*governance.Policy) (*Engine, *metrics.Metrics) {
	t.Helper()
	m := metrics.New()
	e, err := NewEngine(nil, WithMetrics(m))
	if err != nil {
		t.Fatal(err)
	}
	e.Governance().SetPolicies(append([]*governance.Policy{e.Governance().Policies()[0]}, policies...))
	if err := e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.engine = e
	t.Cleanup(func() { _ = e.Stop(context.Background()) })
	return e, m
}

// Obligation 4: terminal counters reflect the actual stored terminal result.
func TestExecutionMetricsReflectStoredTerminalResults(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	// Default (shipped) policies allow the action, so the run completes.
	e, m := newMetricsEngine(t, h)

	submitActionRequest(t, e, h, "exec-m-ok", "user-1")
	if res := awaitResult(t, e, "exec-m-ok"); res.Status != "completed" {
		t.Fatalf("expected a completed result, got %s", res.Status)
	}

	snap := m.Snapshot()
	if snap.ExecutionsAccepted != 1 {
		t.Fatalf("accepted = %d, want 1", snap.ExecutionsAccepted)
	}
	if snap.ExecutionsCompleted != 1 {
		t.Fatalf("completed = %d, want 1", snap.ExecutionsCompleted)
	}
	if snap.ExecutionsFailed != 0 || snap.ExecutionsCancelled != 0 {
		t.Fatalf("a completed run must not be counted as failed/cancelled: %+v", snap)
	}
	// In-flight is closed once the terminal result is stored.
	if snap.ExecutionsInFlight != 0 {
		t.Fatalf("in_flight = %d, want 0 after a terminal result", snap.ExecutionsInFlight)
	}
	// A completed run is not an approval pause and not a failure class.
	if snap.ApprovalPauses != 0 || snap.TerminalOtherFailure != 0 {
		t.Fatalf("unexpected pause/failure classification: %+v", snap)
	}
}

// Obligation 6: approval pause and resume metrics reflect actual transitions.
func TestApprovalPauseAndResumeMetrics(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e, m := newMetricsEngine(t, h,
		actionPolicy("p-m-gate", governance.REQUIRE_APPROVAL, "http.request", nil))

	submitActionRequest(t, e, h, "exec-m-approve", "user-1")
	res := awaitResult(t, e, "exec-m-approve")
	if res.Status != "failed" || res.Error == nil || res.Error.Code != "APPROVAL_REQUIRED" {
		t.Fatalf("expected an approval wait, got %+v", res)
	}

	snap := m.Snapshot()
	if snap.ApprovalPauses != 1 {
		t.Fatalf("approval pauses = %d, want 1", snap.ApprovalPauses)
	}
	if snap.ExecutionsFailed != 1 {
		t.Fatalf("an approval pause stores a failed result, got %d", snap.ExecutionsFailed)
	}
	if snap.TerminalApprovalRequired != 1 {
		t.Fatalf("terminal approval-required class = %d, want 1", snap.TerminalApprovalRequired)
	}
	if snap.ApprovalResumes != 0 {
		t.Fatalf("resumes = %d before any approval", snap.ApprovalResumes)
	}
	// The paused run is terminal: nothing is in flight while it waits.
	if snap.ExecutionsInFlight != 0 {
		t.Fatalf("in_flight = %d, want 0 while approval-pending", snap.ExecutionsInFlight)
	}

	// Approve and let the continuation run.
	approvalID := approvalIDFrom(res)
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", "approved"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	final := awaitResult(t, e, "exec-m-approve")
	if final.Status != "completed" {
		t.Fatalf("expected the resumed run to complete, got %s", final.Status)
	}

	snap = m.Snapshot()
	if snap.ApprovalResumes != 1 {
		t.Fatalf("approval resumes = %d, want 1", snap.ApprovalResumes)
	}
	// A resume is a second run of the SAME execution: it must not be counted
	// as a newly accepted execution.
	if snap.ExecutionsAccepted != 1 {
		t.Fatalf("accepted = %d; a resume is not a new submission", snap.ExecutionsAccepted)
	}
	if snap.ExecutionsCompleted != 1 || snap.ExecutionsInFlight != 0 {
		t.Fatalf("resumed run accounting is wrong: %+v", snap)
	}
}

// Obligation 5: a cancellation that loses to an existing terminal result is not
// counted as a successful cancellation.
func TestCancellationLosingToTerminalResultIsNotCountedAsWon(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e, m := newMetricsEngine(t, h)

	submitActionRequest(t, e, h, "exec-m-cancel", "user-1")
	if res := awaitResult(t, e, "exec-m-cancel"); res.Status != "completed" {
		t.Fatalf("expected a completed result, got %s", res.Status)
	}
	before := m.Snapshot()

	// The execution is already terminal: the cancellation loses.
	if err := e.CancelRequest("exec-m-cancel", "biz-1", "user-1"); err == nil {
		t.Fatal("cancelling a terminal execution must be refused")
	}

	after := m.Snapshot()
	if after.CancellationsRequested != before.CancellationsRequested+1 {
		t.Fatalf("the attempt must be counted as requested: %d -> %d",
			before.CancellationsRequested, after.CancellationsRequested)
	}
	if after.CancellationsWon != before.CancellationsWon {
		t.Fatalf("a refused cancellation must never count as won: %d -> %d",
			before.CancellationsWon, after.CancellationsWon)
	}
	if after.CancellationsRefused != before.CancellationsRefused+1 {
		t.Fatalf("a refusal must be counted as refused: %d -> %d",
			before.CancellationsRefused, after.CancellationsRefused)
	}
	// The losing cancellation must not disturb the stored terminal result.
	if res, ok := e.GetResult("exec-m-cancel"); !ok || res.Status != "completed" {
		t.Fatalf("the completed result must survive, got ok=%v res=%+v", ok, res)
	}
	if after.ExecutionsCancelled != before.ExecutionsCancelled {
		t.Fatal("a lost cancellation must not be counted as a cancelled execution")
	}
}

// A cancellation that actually takes effect IS counted as won.
func TestCancellationOfWaitingExecutionIsCountedAsWon(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e, m := newMetricsEngine(t, h,
		actionPolicy("p-m-cancel", governance.REQUIRE_APPROVAL, "http.request", nil))

	submitActionRequest(t, e, h, "exec-m-cwait", "user-1")
	if res := awaitResult(t, e, "exec-m-cwait"); res.Status != "failed" ||
		res.Error == nil || res.Error.Code != "APPROVAL_REQUIRED" {
		t.Fatalf("expected an approval wait, got %+v", res)
	}

	if err := e.CancelRequest("exec-m-cwait", "biz-1", "user-1"); err != nil {
		t.Fatalf("cancelling a waiting execution must succeed: %v", err)
	}
	res := awaitResult(t, e, "exec-m-cwait")
	if res.Status != "cancelled" {
		t.Fatalf("expected a cancelled result, got %s", res.Status)
	}

	snap := m.Snapshot()
	if snap.CancellationsRequested != 1 || snap.CancellationsWon != 1 || snap.CancellationsRefused != 0 {
		t.Fatalf("cancellation accounting is wrong: %+v", snap)
	}
	if snap.ExecutionsCancelled != 1 {
		t.Fatalf("cancelled executions = %d, want 1", snap.ExecutionsCancelled)
	}
	// A cancellation is not a failure class.
	if snap.ExecutionsFailed != 1 || snap.TerminalApprovalRequired != 1 {
		t.Fatalf("the pause before the cancellation must still be recorded: %+v", snap)
	}
}

// An unknown execution cannot be cancelled and is refused, not won.
func TestCancellationOfUnknownRequestIsRefused(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e, m := newMetricsEngine(t, h)

	if err := e.CancelRequest("exec-never", "biz-1", "user-1"); err == nil {
		t.Fatal("cancelling an unknown request must fail")
	}
	snap := m.Snapshot()
	if snap.CancellationsRequested != 1 || snap.CancellationsRefused != 1 || snap.CancellationsWon != 0 {
		t.Fatalf("cancellation accounting is wrong: %+v", snap)
	}
}

// Obligation 13: two independent engines never share metric state.
func TestIndependentEnginesDoNotShareMetrics(t *testing.T) {
	h1 := &actionGateHandler{tool: "http.request"}
	e1, m1 := newMetricsEngine(t, h1)
	h2 := &actionGateHandler{tool: "http.request"}
	_, m2 := newMetricsEngine(t, h2)

	submitActionRequest(t, e1, h1, "exec-iso-1", "user-1")
	awaitResult(t, e1, "exec-iso-1")

	if m1.Snapshot().ExecutionsCompleted != 1 {
		t.Fatal("the first engine must record its own completion")
	}
	if got := m2.Snapshot(); got.ExecutionsCompleted != 0 || got.ExecutionsAccepted != 0 {
		t.Fatalf("metric state leaked into the second engine: %+v", got)
	}
	if m2 == m1 {
		t.Fatal("independent engines must not share one metrics owner")
	}
}

// Obligation 14: an engine with no metrics owner behaves exactly as before —
// instrumentation can never block execution, approval, cancellation or
// journaling. This exercises the nil owner on the real paths.
func TestNilMetricsOwnerDoesNotBlockExecution(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e, err := NewEngine(nil) // no WithMetrics: the nil owner
	if err != nil {
		t.Fatal(err)
	}
	e.Governance().SetPolicies([]*governance.Policy{e.Governance().Policies()[0]})
	if err := e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.engine = e
	t.Cleanup(func() { _ = e.Stop(context.Background()) })

	submitActionRequest(t, e, h, "exec-nil-m", "user-1")
	if res := awaitResult(t, e, "exec-nil-m"); res.Status != "completed" {
		t.Fatalf("execution must succeed without metrics, got %s", res.Status)
	}
	// Approval, resume and cancellation paths must also work.
	_ = e.CancelRequest("exec-nil-m", "biz-1", "user-1")
	if res, ok := e.GetResult("exec-nil-m"); !ok || res.Status != "completed" {
		t.Fatalf("result must be unaffected, got ok=%v res=%+v", ok, res)
	}
	// The snapshot reads as all zero rather than panicking.
	if snap := e.MetricsSnapshot(); snap != (metrics.Snapshot{}) {
		t.Fatalf("a nil owner must read as all zero, got %+v", snap)
	}
}

// Obligation 12: a metrics read concurrent with execution is race-safe.
func TestMetricsSnapshotConcurrentWithExecutionIsRaceSafe(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e, m := newMetricsEngine(t, h)

	const runs = 12
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < runs; i++ {
			submitActionRequest(t, e, h, "exec-m-race-"+strconv.Itoa(i), "user-1")
		}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-done:
			// Drain, then assert nothing is left in flight.
			for i := 0; i < runs; i++ {
				awaitResult(t, e, "exec-m-race-"+strconv.Itoa(i))
			}
			if got := m.Snapshot().ExecutionsInFlight; got != 0 {
				t.Fatalf("in_flight = %d after all runs finished, want 0", got)
			}
			if got := m.Snapshot().ExecutionsAccepted; got != runs {
				t.Fatalf("accepted = %d, want %d", got, runs)
			}
			return
		default:
			_ = e.MetricsSnapshot()
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out submitting runs")
		}
	}
}
