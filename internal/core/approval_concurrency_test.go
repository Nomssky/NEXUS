package core

// Approval/resume concurrency (contracts/APPROVAL_LIFECYCLE_CONTRACTS.md §4,
// §7, §8). Each test drives an explicit interleaving with a start barrier and
// proves one invariant — no sleeps, no retries. They must stay green under
// `go test -race` and repeated runs.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

// TestApprovalDecisionReadsDoNotRaceWithDecisions forces the interleaving the
// read path must survive: the approval surface reading decision fields while
// the resume path mutates them. The engine writes Status/Approver/Reason under
// its own lock; a read under core's approvalMu (or none) is a data race, and
// ApprovalState is a string, so it can also observe a torn/stale decision.
// Run under -race this test flags the unsynchronized access; it stays green
// once every read goes through the engine-locked snapshot.
func TestApprovalDecisionReadsDoNotRaceWithDecisions(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))

	const n = 48
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		rid := fmt.Sprintf("exec-race-%d", i)
		submitActionRequest(t, e, h, rid, "user-1")
		id := approvalIDFrom(awaitResult(t, e, rid))
		if id == "" {
			t.Fatalf("no approval id for %s", rid)
		}
		ids = append(ids, id)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(3)
	// Writer: decide every approval (mutates the shared record under ae.mu).
	go func() {
		defer wg.Done()
		<-start
		for _, id := range ids {
			_ = e.ApproveRequest(id, "biz-1", "approver-1", "")
		}
	}()
	// Readers: list decisions concurrently (reads the shared record).
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < n*8; i++ {
			_ = e.ListApprovals("biz-1")
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < n*4; i++ {
			_ = e.ListApprovals("")
		}
	}()
	close(start)
	wg.Wait()
}

// selfResumingHandler admits the gated action; the FIRST time it pauses it
// approves its OWN approval before returning pending_approval. That forces the
// exact interleaving the stale-teardown race needs: the approval is granted
// AFTER this run was dequeued (its consumed-approval set was captured) but
// BEFORE it stores its terminal result and runs its teardown. The run that is
// finishing must not deregister the request, drop its reference index, or free
// the approval the continuing run depends on.
type selfResumingHandler struct {
	engine     *Engine
	approver   string
	mu         sync.Mutex
	effects    int
	approved   bool
	approveErr error
}

func (h *selfResumingHandler) handle(ctx context.Context, req *executor.WorkRequest, _ *agent.Agent) (*executor.Outcome, error) {
	adm, err := h.engine.Controller().Admit(control.Proposal{
		ProposalID:    "prop-" + req.CorrelationID,
		CorrelationID: req.CorrelationID,
		ExecutionID:   req.CorrelationID,
		ObjectiveID:   req.CorrelationID,
		StepID:        req.TaskID,
		ActorID:       req.ActorID,
		AgentID:       "agent-1",
		BusinessID:    req.BusinessID,
		DivisionID:    req.DivisionID,
		Action:        control.ActionToolCall,
		Resource:      "http.request",
		ResourceType:  control.ResourceTypeCapability,
		ToolID:        "http.request",
		Operation:     "execute",
	})
	out := &executor.Outcome{TaskID: req.TaskID, CorrelationID: req.CorrelationID, BusinessID: req.BusinessID}
	switch {
	case err != nil:
		out.Status, out.Error = "denied", err.Error()
	case adm.Allowed():
		h.mu.Lock()
		h.effects++
		h.mu.Unlock()
		out.Status, out.Output = "completed", "effect applied"
	case adm.AwaitingApproval():
		out.Status = "pending_approval"
		out.ApprovalID, out.Error = adm.ApprovalID, "governance requires approval"
		h.mu.Lock()
		if !h.approved {
			h.approved = true
			// Approve in the same breath as the pause: the continuation is
			// admitted while this run is still finishing.
			h.approveErr = h.engine.ApproveRequest(adm.ApprovalID, "biz-1", h.approver, "self-resume")
		}
		h.mu.Unlock()
	default:
		out.Status, out.Error = "denied", "governance denied"
	}
	return out, nil
}

func (h *selfResumingHandler) effectCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.effects
}

// awaitOutcome waits for the SETTLED (completed) terminal: a run that approves
// itself surfaces the pausing result before the continuation stores its own, so
// a single GetResult poll can still observe the interim APPROVAL_REQUIRED.
func awaitOutcome(t *testing.T, e *Engine, id string) *Response {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if res, ok := e.GetResult(id); ok && res.Status == "completed" {
			return res
		}
		time.Sleep(5 * time.Millisecond)
	}
	res, _ := e.GetResult(id)
	return res
}

// TestApprovalGrantedDuringRunSurvivesThatRun proves the stale-teardown fix: an
// approval granted while the pausing run is still finishing is not consumed by
// that run, so its resume re-admits and completes instead of re-gating. Before
// the fix the finishing run's cleanup (cleanupResumeApproval, keyed off "is the
// approval approved NOW") deleted the just-granted approval, deregistering the
// request and dropping its reference too, so the resume failed.
func TestApprovalGrantedDuringRunSurvivesThatRun(t *testing.T) {
	h := &selfResumingHandler{approver: "approver-1"}
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	e.Governance().SetPolicies(append([]*governance.Policy{e.Governance().Policies()[0]},
		actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil)))
	if err := e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Stop(context.Background()) })
	h.engine = e

	const rid = "exec-self-resume"
	if err := e.SubmitRequest(&Request{
		ID:      rid,
		Context: NewRequestContext(rid, "biz-1", "user-1"),
		Intent:  "run the gated action",
		Handler: h.handle,
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	res := awaitOutcome(t, e, rid)
	if h.approveErr != nil {
		t.Fatalf("the mid-run approval must be accepted: %v", h.approveErr)
	}
	if res.Status != "completed" {
		t.Fatalf("the approval granted during the run must drive its resume to completion, got %s (%+v)", res.Status, res.Error)
	}
	if n := h.effectCount(); n != 1 {
		t.Fatalf("the continuation must be applied exactly once, got %d effects", n)
	}
	// The run's own teardown must have released the registration and the
	// reference for a finished request (no leak), and the spent approval is gone.
	if _, pending := e.Pending(rid); pending {
		t.Fatal("a finished request must not stay registered as pending")
	}
	if len(e.ListApprovals("biz-1")) != 0 {
		t.Fatal("a consumed approval must not stay listed")
	}
	// idempotent sanity: awaiting again does not re-run the effect.
	if res2 := awaitResult(t, e, rid); res2.Status != "completed" || h.effectCount() != 1 {
		t.Fatalf("a terminal result must be stable: %s effects=%d", res2.Status, h.effectCount())
	}
}

// TestConcurrentResumeAttemptsConsumeTheApprovalOnce proves the approval stays
// single-use when two resume requests race: the engine lock admits exactly one,
// the other is a state conflict, and the continuation is applied once.
func TestConcurrentResumeAttemptsConsumeTheApprovalOnce(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	submitActionRequest(t, e, h, "exec-double-resume", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-double-resume"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = e.ApproveRequest(approvalID, "biz-1", "approver-1", "")
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrApprovalNotPending) && !errors.Is(err, ErrApprovalNotFound) {
			t.Fatalf("a losing concurrent resume must be a state conflict, got %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("exactly one concurrent resume may be admitted, got %d", successes)
	}
	if res := awaitResult(t, e, "exec-double-resume"); res.Status != "completed" {
		t.Fatalf("the admitted resume must complete: %+v", res.Error)
	}
	if n := h.effectCount(); n != 1 {
		t.Fatalf("the continuation must be consumed exactly once, got %d effects", n)
	}
}

// TestResumeRacingCancellationNeverDoubleConsumes proves cancellation and
// resume linearize: when a resume is admitted concurrently with a cancellation,
// the terminal state is never a failure that overwrites the cancellation
// (APPROVAL_LIFECYCLE §8: cancellation wins), the effect is applied at most
// once, and a cancelled execution never had its effect applied behind the
// cancellation.
//
// Before the fix this test failed ~40% of runs under -race: the resume reached
// the executor, its re-admission refused the invalidated approval, and the
// executor DENY was recorded as failed/POLICY_DENIED, overwriting the cancelled
// terminal even though the cancellation had succeeded.
func TestResumeRacingCancellationNeverDoubleConsumes(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	submitActionRequest(t, e, h, "exec-race-cancel", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-race-cancel"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	var approveErr, cancelErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		approveErr = e.ApproveRequest(approvalID, "biz-1", "approver-1", "")
	}()
	go func() {
		defer wg.Done()
		<-start
		cancelErr = e.CancelRequest("exec-race-cancel", "biz-1", "user-1")
	}()
	close(start)
	wg.Wait()

	res := awaitResult(t, e, "exec-race-cancel")
	// A resume must never downgrade a cancellation to a failure.
	if res.Status != "completed" && res.Status != "cancelled" {
		t.Fatalf("terminal must be completed or cancelled, got %s (%+v)", res.Status, res.Error)
	}
	if n := h.effectCount(); n > 1 {
		t.Fatalf("the effect must never be applied twice, got %d", n)
	}
	if res.Status == "completed" && h.effectCount() != 1 {
		t.Fatalf("a completed terminal must have applied the effect, got %d", h.effectCount())
	}
	if res.Status == "cancelled" && h.effectCount() != 0 {
		t.Fatalf("a cancelled execution must not have applied the effect, got %d", h.effectCount())
	}
	// Cancellation wins: a successful cancel leaves a cancelled terminal,
	// whichever side of the race the approve reached.
	if cancelErr == nil && res.Status != "cancelled" {
		t.Fatalf("a successful cancellation must leave a cancelled terminal, got %s (approveErr=%v)", res.Status, approveErr)
	}
	// Whatever the interleaving, the approval leaves the decidable set.
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("a spent or cancelled approval must not be decidable, got %v", err)
	}
}

// TestTerminalResultStaysTerminalUnderConcurrentDecisions proves a completed
// terminal is immutable: late concurrent approve/deny/list operations neither
// unset it nor replay the effect.
func TestTerminalResultStaysTerminalUnderConcurrentDecisions(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	submitActionRequest(t, e, h, "exec-terminal", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-terminal"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if res := awaitResult(t, e, "exec-terminal"); res.Status != "completed" {
		t.Fatalf("the resumed run must complete: %+v", res.Error)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		_ = e.ApproveRequest(approvalID, "biz-1", "approver-1", "")
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = e.DenyRequest(approvalID, "biz-1", "approver-1", "")
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 64; i++ {
			_ = e.ListApprovals("biz-1")
		}
	}()
	close(start)
	wg.Wait()

	if res, ok := e.GetResult("exec-terminal"); !ok || res.Status != "completed" {
		t.Fatalf("a completed terminal must stay completed, got %+v ok=%v", res, ok)
	}
	if n := h.effectCount(); n != 1 {
		t.Fatalf("no late decision may re-run the effect, got %d", n)
	}
}
