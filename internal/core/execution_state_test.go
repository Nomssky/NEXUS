package core

// Execution-state consistency (contracts/APPROVAL_LIFECYCLE_CONTRACTS.md §7/§8,
// DURABLE_EXECUTION_RECOVERY_CONTRACTS.md §2/§3). Each test drives an explicit
// interleaving with a barrier or direct state manipulation — no sleeps — and
// proves one invariant. All must stay green under `go test -race`.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// TestLookupExecutionClosesTheReAdmissionGap forces the exact window inside a
// resume's re-admission — the stale result dropped, the request not yet
// re-registered — and proves:
//   - a reader composing the two registries with separate locks (the v12
//     pattern) observes the request as ABSENT (both misses), which the read
//     endpoints would answer 404 for;
//   - LookupExecution, serialized on admitMu with the transition, cannot run
//     inside the window and observes a coherent pending view.
func TestLookupExecutionClosesTheReAdmissionGap(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	submitActionRequest(t, e, h, "exec-gap", "user-1")
	if res := awaitResult(t, e, "exec-gap"); res.Status != "failed" {
		t.Fatalf("expected a waiting result, got %s", res.Status)
	}
	// Known request: a stored result, no in-flight registration (the run's
	// teardown released it).
	if _, ok := e.GetResult("exec-gap"); !ok {
		t.Fatal("expected a stored result")
	}
	if _, ok := e.Pending("exec-gap"); ok {
		t.Fatal("expected no in-flight registration")
	}

	// Enter the re-admission window with a barrier: hold admitMu (as
	// ApproveRequest does across dropResult→registerInflight) and drop the
	// stale result.
	e.admitMu.Lock()
	if _, had := e.dropResult("exec-gap"); !had {
		e.admitMu.Unlock()
		t.Fatal("expected to drop the result")
	}

	// The scattered composition observes the invalid transient state.
	_, resOK := e.GetResult("exec-gap")
	_, pendOK := e.Pending("exec-gap")
	if resOK || pendOK {
		e.admitMu.Unlock()
		t.Fatalf("expected the raw composition to observe the gap, got result=%v pending=%v", resOK, pendOK)
	}

	// LookupExecution serializes on admitMu: it cannot run inside the window.
	// Queue it, finish the transition (re-register the request), then release.
	viewCh := make(chan ExecutionView, 1)
	go func() { viewCh <- e.LookupExecution("exec-gap") }()
	e.registerInflight(&Request{ID: "exec-gap", Context: NewRequestContext("exec-gap", "biz-1", "user-1")})
	e.admitMu.Unlock()

	view := <-viewCh
	if view.Result != nil || !view.InFlight {
		t.Fatalf("LookupExecution must observe a coherent pending view, got %+v", view)
	}
	if !view.Known() {
		t.Fatal("a known request must report Known")
	}
	// A coherent view never reports an unknown id as known either.
	if e.LookupExecution("exec-never").Known() {
		t.Fatal("an unknown id must not report Known")
	}
}

// TestConcurrentExecutionReadsNeverSeeAGap is the companion stress test: while
// approvals re-admit many requests, a reader sampling LookupExecution for each
// known id never observes one as unknown. Under the scattered composition it
// could catch the drop→register window.
func TestConcurrentExecutionReadsNeverSeeAGap(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))

	const n = 24
	ids := make([]string, 0, n)
	approvalIDs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		rid := fmt.Sprintf("exec-view-%d", i)
		submitActionRequest(t, e, h, rid, "user-1")
		res := awaitResult(t, e, rid)
		if res.Status != "failed" {
			t.Fatalf("expected a waiting result for %s, got %s", rid, res.Status)
		}
		ids = append(ids, rid)
		approvalIDs = append(approvalIDs, approvalIDFrom(res))
	}

	var readerWG, approverWG sync.WaitGroup
	stop := make(chan struct{})
	unknown := make(chan string, 1)
	// Reader: every known id must always resolve to a known view.
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, id := range ids {
				if !e.LookupExecution(id).Known() {
					select {
					case unknown <- id:
					default:
					}
					return
				}
			}
		}
	}()
	// Approvers: re-admit every request (each moves it through the transition).
	approverWG.Add(1)
	go func() {
		defer approverWG.Done()
		for _, aid := range approvalIDs {
			_ = e.ApproveRequest(aid, "biz-1", "approver-1", "")
		}
	}()
	approverWG.Wait()
	close(stop)
	readerWG.Wait()

	select {
	case id := <-unknown:
		t.Fatalf("LookupExecution reported known request %s as unknown during a resume", id)
	default:
	}
	// Every request must still resolve to a known view (pending or terminal) —
	// a resume still in flight is a valid known state.
	for _, id := range ids {
		if !e.LookupExecution(id).Known() {
			t.Fatalf("request %s resolved to an unknown view after the resumes", id)
		}
	}
}

// TestLateCancellationDoesNotOverwriteACommittedTerminal forces the exact
// interleaving the reported risk describes: a resume commits its completed
// terminal AFTER the cancellation observed the wait but BEFORE it stored the
// cancelled result. The compare-and-set guard must refuse the overwrite.
func TestLateCancellationDoesNotOverwriteACommittedTerminal(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	submitActionRequest(t, e, h, "exec-late-cancel", "user-1")
	if res := awaitResult(t, e, "exec-late-cancel"); res.Status != "failed" {
		t.Fatalf("expected a waiting result, got %s", res.Status)
	}

	// A committed terminal landed after the caller observed the wait.
	completed := &Response{RequestID: "exec-late-cancel", BusinessID: "biz-1", Status: "completed"}
	e.resultsMu.Lock()
	e.results["exec-late-cancel"] = completed
	e.resultsMu.Unlock()

	if stored := e.storeCancelledResult("exec-late-cancel", "biz-1", ""); stored {
		t.Fatal("storeCancelledResult must refuse to overwrite a committed terminal")
	}
	got, ok := e.GetResult("exec-late-cancel")
	if !ok || got != completed || got.Status != "completed" {
		t.Fatalf("the completed terminal was overwritten: %+v", got)
	}

	// The cancellation helper must arbitrate the same way.
	if err := e.cancelWaitingExecution("exec-late-cancel", "biz-1", ""); !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("a cancellation that lost the race must surface the committed terminal, got %v", err)
	}
	if got, _ := e.GetResult("exec-late-cancel"); got.Status != "completed" {
		t.Fatalf("the completed terminal changed after arbitration: %+v", got)
	}
}

// TestCancellationStillWinsOverAWaitingResult proves the guard did not weaken
// the contract direction: a live wait is still replaced by cancelled.
func TestCancellationStillWinsOverAWaitingResult(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	submitActionRequest(t, e, h, "exec-still-cancel", "user-1")
	if res := awaitResult(t, e, "exec-still-cancel"); res.Status != "failed" {
		t.Fatalf("expected a waiting result, got %s", res.Status)
	}
	if err := e.CancelRequest("exec-still-cancel", "biz-1", "user-1"); err != nil {
		t.Fatalf("cancel of a waiting execution: %v", err)
	}
	stored, ok := e.GetResult("exec-still-cancel")
	if !ok || stored.Status != "cancelled" || stored.Error == nil || stored.Error.Code != "CANCELLED" {
		t.Fatalf("waiting cancel must store a cancelled result, got %+v", stored)
	}
	if len(e.ListApprovals("biz-1")) != 0 {
		t.Fatal("the waiting cancel must invalidate the bound approval")
	}
}

// awaitJournalState polls until the durable record reaches a terminal state.
func awaitJournalState(t *testing.T, j *execjournal.Journal, id string) execjournal.ExecutionRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rec, ok, err := j.Get(id)
		if err == nil && ok && rec.State.Terminal() {
			return rec
		}
		time.Sleep(2 * time.Millisecond)
	}
	rec, _, _ := j.Get(id)
	t.Fatalf("durable record %s never reached a terminal state: %+v", id, rec)
	return rec
}

// TestWaitingPauseDurableTerminalIsFinalAcrossProcessLocalCancel establishes
// the journal-versus-memory relationship from the existing contracts: the pause
// records a durable terminal failed (APPROVAL_REQUIRED) (v10 §3 window 7), and a
// later process-local cancellation changes the in-memory result but never
// rewrites the durable record (terminal is final, §2). The durable record is
// recovery evidence, not a live result mirror.
func TestWaitingPauseDurableTerminalIsFinalAcrossProcessLocalCancel(t *testing.T) {
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	h := &actionGateHandler{tool: "http.request"}
	e, err := NewEngine(nil, WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	e.Governance().SetPolicies(append([]*governance.Policy{e.Governance().Policies()[0]},
		actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil)))
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(context.Background()) })
	h.engine = e

	const rid = "exec-journal-wait"
	submitActionRequest(t, e, h, rid, "user-1")
	if res := awaitResult(t, e, rid); res.Status != "failed" {
		t.Fatalf("expected a waiting result, got %s", res.Status)
	}

	rec := awaitJournalState(t, j, rid)
	if rec.State != execjournal.StateFailed || rec.ErrorClass != "APPROVAL_REQUIRED" {
		t.Fatalf("pause must record durable failed/APPROVAL_REQUIRED, got %+v", rec)
	}

	// A process-local cancellation changes the in-memory result...
	if err := e.CancelRequest(rid, "biz-1", "user-1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if res, _ := e.GetResult(rid); res == nil || res.Status != "cancelled" {
		t.Fatalf("in-memory result must become cancelled, got %+v", res)
	}

	// ...but never rewrites the durable terminal record.
	rec2, ok, err := j.Get(rid)
	if err != nil || !ok {
		t.Fatalf("journal: ok=%v err=%v", ok, err)
	}
	if rec2.State != execjournal.StateFailed || rec2.ErrorClass != "APPROVAL_REQUIRED" {
		t.Fatalf("the durable terminal must stay final and unrewritten, got %+v", rec2)
	}
}
