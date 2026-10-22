package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// The engine durably records acceptance before the caller is told the
// execution exists, and the terminal state after the result is stored.
func TestEngineJournalAcceptAndTerminal(t *testing.T) {
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	e, err := NewEngine(nil, WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(ctx) })

	if err := e.SubmitRequest(&Request{
		ID:      "exec-core-1",
		Context: NewRequestContext("exec-core-1", "biz-1", "actor-1"),
		Intent:  "do work",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// The engine persisted acceptance synchronously during SubmitRequest.
	rec, ok, err := j.Get("exec-core-1")
	if err != nil || !ok {
		t.Fatalf("accepted record missing: ok=%v err=%v", ok, err)
	}
	if rec.State != execjournal.StateAccepted {
		t.Fatalf("expected accepted, got %q", rec.State)
	}
	if rec.BusinessID != "biz-1" || rec.ActorID != "actor-1" {
		t.Fatalf("scope not recorded: %+v", rec)
	}

	// Wait for the terminal result, then the durable terminal state.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := e.GetResult("exec-core-1"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, ok, err = j.Get("exec-core-1")
		if err == nil && ok && rec.State.Terminal() {
			return // success
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("terminal state not persisted: ok=%v err=%v state=%v", ok, err, rec.State)
}

// Recovery never re-schedules a durably-terminal execution: the engine starts
// with an empty queue, and classification does not enqueue anything.
func TestEngineRecoveryDoesNotReschedule(t *testing.T) {
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	if err := j.Accepted("exec-old", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rep, err := j.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if rep.Recovered != 1 {
		t.Fatalf("expected 1 recovered, got %d", rep.Recovered)
	}
	// A fresh engine over the same store has no queued work for the recovered id.
	e, err := NewEngine(nil, WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if _, ok := e.GetResult("exec-old"); ok {
		t.Fatal("a recovered execution must not appear as a fresh result")
	}
}

// ---- v25: a terminally recorded execution id is never re-admitted --------
//
// Core's duplicate detection is process-local: it reads the in-memory results
// and inflight maps (engine.go SubmitRequest). A restart empties both, so the
// durable record is the only thing that can still say "this execution id
// already finished". The gateway mints the execution id from the caller's
// X-Correlation-ID, so the id is caller-controlled and the guard has to be
// durable. This proves the whole path: the second process refuses the id and
// the handler — the thing that would carry a side effect — runs exactly once.

func TestEngineRefusesATerminalExecutionIDAfterRestart(t *testing.T) {
	dir := t.TempDir()
	var ran int
	handler := func(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
		ran++
		return &executor.Outcome{Status: "completed", Output: "done"}, nil
	}

	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	e, err := NewEngine(nil, WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	req := &Request{
		ID: "exec-across-restart", Context: NewRequestContext("exec-across-restart", "biz-1", "actor-1"),
		Intent: "do work once", Handler: handler,
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	awaitResult(t, e, "exec-across-restart")
	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop first engine: %v", err)
	}
	if rec, _, _ := j.Get("exec-across-restart"); rec.State != execjournal.StateCompleted {
		t.Fatalf("the execution must be durably terminal before the restart, got %q", rec.State)
	}

	// "Restart": a brand-new store, journal and engine over the same directory,
	// with the same caller-supplied execution id.
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	j2 := execjournal.New(st2, nil)
	if _, err := j2.Recover(); err != nil {
		t.Fatalf("recover after restart: %v", err)
	}
	e2, err := NewEngine(nil, WithExecutionJournal(j2))
	if err != nil {
		t.Fatalf("second engine: %v", err)
	}
	if err := e2.Start(ctx); err != nil {
		t.Fatalf("start second engine: %v", err)
	}
	t.Cleanup(func() { _ = e2.Stop(ctx) })

	if err := e2.SubmitRequest(&Request{
		ID: "exec-across-restart", Context: NewRequestContext("exec-across-restart", "biz-1", "actor-1"),
		Intent: "do work once", Handler: handler,
	}); err == nil {
		t.Fatal("an execution id that is already durably terminal must not be re-admitted after a restart")
	}

	// The work ran exactly once across both processes, and the durable
	// evidence still says completed — it was neither re-armed nor erased.
	if ran != 1 {
		t.Fatalf("the handler must run exactly once across the restart, ran %d times", ran)
	}
	if rec, _, _ := j2.Get("exec-across-restart"); rec.State != execjournal.StateCompleted {
		t.Fatalf("the terminal record must survive the refused re-admission, got %q", rec.State)
	}
}

// ---- v27: durable approval semantics -------------------------------------
//
// Repro: an approval pause is journaled terminal failed/APPROVAL_REQUIRED, and
// terminal records are immutable, so a resumed execution that COMPLETES stays
// durably "failed" forever. This asserts the approved policy using only the
// existing journal API, so it fails against the current implementation.

func newJournaledGateEngine(t *testing.T) (*Engine, *execjournal.Journal, *actionGateHandler, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	h := &actionGateHandler{tool: "http.request"}
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	e.Governance().SetPolicies(append([]*governance.Policy{e.Governance().Policies()[0]},
		actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil)))
	e.journal = j
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(context.Background()) })
	h.engine = e
	return e, j, h, dir
}

func TestResumedCompletionRecordsTheTruthfulDurableOutcome(t *testing.T) {
	e, j, h, dir := newJournaledGateEngine(t)

	submitActionRequest(t, e, h, "exec-approve", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-approve"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}
	// journalFinalize (Terminal + ApprovalRequested) runs after the result
	// is stored; wait for it so ApproveRequest doesn't race it and skip the
	// ApprovalRequested entry.
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		rec, ok, _ := j.Get("exec-approve")
		if ok && rec.ErrorClass == execjournal.ApprovalRequiredClass {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for journal approval-requested entry")
		case <-tick.C:
		}
	}
	// While paused the record is the provisional approval pause.
	if rec, _, _ := j.Get("exec-approve"); rec.State != execjournal.StateFailed || rec.ErrorClass != "APPROVAL_REQUIRED" {
		t.Fatalf("a pause must be recorded as failed/APPROVAL_REQUIRED, got %+v", rec)
	}

	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	res := awaitResult(t, e, "exec-approve")
	if res.Status != "completed" {
		t.Fatalf("the resumed execution must complete in-process: %s", res.Status)
	}

	// The durable record must agree with the real outcome.
	rec, _, _ := j.Get("exec-approve")
	if rec.State != execjournal.StateCompleted {
		t.Fatalf("a completed execution must be durably completed, got %q (%s)", rec.State, rec.ErrorClass)
	}

	// And both approval facts must still be there after the outcome, proven by
	// reopening the real store rather than reading a process-local map.
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	rec2, _, _ := execjournal.New(st2, nil).Get("exec-approve")
	if len(rec2.Approvals) != 2 ||
		rec2.Approvals[0].Outcome != execjournal.ApprovalRequested ||
		rec2.Approvals[1].Outcome != execjournal.ApprovalGranted {
		t.Fatalf("approval history must survive the terminal outcome and a restart, got %+v", rec2.Approvals)
	}
	if rec2.Approvals[1].ApprovalID != approvalID {
		t.Fatalf("history must be associated with the correct approval, got %+v", rec2.Approvals[1])
	}
}

// A denial is durable: after a restart the record says the execution was denied
// and carries that fact, rather than still claiming to await an approval.
func TestDeniedApprovalIsDurablyRecorded(t *testing.T) {
	e, j, h, dir := newJournaledGateEngine(t)

	submitActionRequest(t, e, h, "exec-denied", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-denied"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}
	// awaitResult observes the in-memory result map; journalFinalize (which
	// durably records Terminal + ApprovalRequested) runs after the result is
	// stored. Wait for it so DenyRequest doesn't race it and skip the
	// ApprovalRequested entry — the same class of test synchronization defect
	// fixed in cancel_test.go.
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		rec, ok, _ := j.Get("exec-denied")
		if ok && rec.ErrorClass == execjournal.ApprovalRequiredClass {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for journal approval-requested entry")
		case <-tick.C:
		}
	}
	if err := e.DenyRequest(approvalID, "biz-1", "approver-9", "not authorized"); err != nil {
		t.Fatalf("deny: %v", err)
	}

	// The live journal already carries the denial...
	live, _, _ := j.Get("exec-denied")
	if live.ErrorClass != execjournal.ApprovalDeniedClass {
		t.Fatalf("a denial must be recorded immediately, got %q", live.ErrorClass)
	}

	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	rec, _, _ := execjournal.New(st2, nil).Get("exec-denied")
	if rec.State != execjournal.StateFailed || rec.ErrorClass != execjournal.ApprovalDeniedClass {
		t.Fatalf("a denial must be durably failed/%s, got %q/%q",
			execjournal.ApprovalDeniedClass, rec.State, rec.ErrorClass)
	}
	if len(rec.Approvals) != 2 ||
		rec.Approvals[0].Outcome != execjournal.ApprovalRequested ||
		rec.Approvals[1].Outcome != execjournal.ApprovalDenied {
		t.Fatalf("a denied approval must be durably distinguishable, got %+v", rec.Approvals)
	}
	if rec.Approvals[1].DecidedBy != "approver-9" || rec.Approvals[1].ApprovalID != approvalID {
		t.Fatalf("the denial must record who decided and which approval: %+v", rec.Approvals[1])
	}
	// A denial resolves without resume: the execution did not run again.
	if h.effectCount() != 0 {
		t.Fatalf("a denied approval must never dispatch the gated effect, got %d", h.effectCount())
	}
}

// failOnPutStore wraps a Store and fails any execution-record Put whose data
// contains the given marker, so a test can inject a durable-write failure at one
// precise journal boundary. It decorates a real FileStore; the journal and its
// per-record atomicity are unchanged.
type failOnPutStore struct {
	store.Store
	marker string
}

func (s failOnPutStore) Put(r *store.Record) error {
	if r != nil && r.Type == "execution" && bytes.Contains(r.Data, []byte(s.marker)) {
		return errors.New("injected store failure: " + s.marker)
	}
	return s.Store.Put(r)
}

// newJournaledGateEngineFailingOn boots the same gated engine as
// newJournaledGateEngine but over a store that fails any execution Put whose
// data contains marker, so a test can drive one durable-write boundary.
func newJournaledGateEngineFailingOn(t *testing.T, marker string) (*Engine, *execjournal.Journal, *actionGateHandler) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(failOnPutStore{Store: st, marker: marker}, nil)
	h := &actionGateHandler{tool: "http.request"}
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	e.Governance().SetPolicies(append([]*governance.Policy{e.Governance().Policies()[0]},
		actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil)))
	e.journal = j
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(context.Background()) })
	h.engine = e
	return e, j, h
}

// A denial whose approval-history write fails must not be reported as a clean
// success, and must not durably become failed/POLICY_DENIED without the "denied"
// history entry the recovery contract requires for a denial to be "durable and
// distinguishable from a still-pending pause". The record must stay the honest
// provisional pause and the caller must be told the denial did not persist.
func TestDenyRequestFailsHonestlyWhenApprovalWriteFails(t *testing.T) {
	e, j, h := newJournaledGateEngineFailingOn(t, `"outcome":"denied"`)

	submitActionRequest(t, e, h, "exec-deny-partial", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-deny-partial"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}

	err := e.DenyRequest(approvalID, "biz-1", "approver-1", "not now")
	if err == nil {
		t.Fatal("DenyRequest must return an error when the approval decision fails to persist")
	}

	rec, _, _ := j.Get("exec-deny-partial")
	if rec.State != execjournal.StateFailed || rec.ErrorClass != execjournal.ApprovalRequiredClass {
		t.Fatalf("record must remain the honest provisional pause, got %q/%q", rec.State, rec.ErrorClass)
	}
	for _, a := range rec.Approvals {
		if a.Outcome == execjournal.ApprovalDenied {
			t.Fatalf("no denied entry may be recorded: %+v", rec.Approvals)
		}
	}
}

// A denial whose terminal-supersede write fails (after the approval history was
// recorded) must also not be reported as a clean success: the state did not
// become the denial the caller was promised.
func TestDenyRequestFailsHonestlyWhenSupersedeFails(t *testing.T) {
	e, j, h := newJournaledGateEngineFailingOn(t, `"error_class":"POLICY_DENIED"`)

	submitActionRequest(t, e, h, "exec-deny-partial-2", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-deny-partial-2"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}

	err := e.DenyRequest(approvalID, "biz-1", "approver-1", "not now")
	if err == nil {
		t.Fatal("DenyRequest must return an error when the terminal supersede fails to persist")
	}

	// The approval decision was recorded, but the execution must not claim the
	// denial's terminal class.
	rec, _, _ := j.Get("exec-deny-partial-2")
	if rec.ErrorClass == execjournal.ApprovalDeniedClass {
		t.Fatalf("record must not carry the denial class when the supersede failed, got %q", rec.ErrorClass)
	}
}

// A pause whose approval-history write fails is a real partial-commit state: the
// record is durably terminal failed/APPROVAL_REQUIRED but the approval request is
// not in the durable history. It is not auto-recoverable (terminal), so this
// characterizes the established partial-commit semantics rather than asserting a
// fix; the safe resolution is a product/architecture decision.
func TestPauseWithoutApprovalHistoryIsTerminal(t *testing.T) {
	e, j, h := newJournaledGateEngineFailingOn(t, `"outcome":"requested"`)

	submitActionRequest(t, e, h, "exec-pause-partial", "user-1")
	if approvalID := approvalIDFrom(awaitResult(t, e, "exec-pause-partial")); approvalID == "" {
		t.Fatal("expected an approval id")
	}

	rec, _, _ := j.Get("exec-pause-partial")
	if rec.State != execjournal.StateFailed || rec.ErrorClass != execjournal.ApprovalRequiredClass {
		t.Fatalf("a pause must be recorded as failed/APPROVAL_REQUIRED, got %q/%q", rec.State, rec.ErrorClass)
	}
	for _, a := range rec.Approvals {
		if a.Outcome == execjournal.ApprovalRequested {
			t.Fatalf("expected no requested entry when the history write fails, got %+v", rec.Approvals)
		}
	}
}

// Restart while an approval is pending must not imply the execution can resume:
// the approval state is process-local, so the pause is final and unresumable.
func TestRestartWhileApprovalPendingDoesNotClaimResumability(t *testing.T) {
	e, _, h, dir := newJournaledGateEngine(t)

	submitActionRequest(t, e, h, "exec-pending", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-pending"))
	if err := e.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// "Restart": a brand-new store, journal and engine over the same directory.
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	j2 := execjournal.New(st2, nil)
	if _, err := j2.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	e2, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("second engine: %v", err)
	}
	e2.journal = j2
	if err := e2.Start(context.Background()); err != nil {
		t.Fatalf("start second engine: %v", err)
	}
	t.Cleanup(func() { _ = e2.Stop(context.Background()) })

	// Recovery must not have rewritten the terminal pause.
	rec, _, _ := j2.Get("exec-pending")
	if rec.State != execjournal.StateFailed || rec.ErrorClass != execjournal.ApprovalRequiredClass {
		t.Fatalf("recovery must leave the terminal pause untouched, got %q/%q", rec.State, rec.ErrorClass)
	}
	// The approval itself is gone, so nothing can resume it.
	if err := e2.ApproveRequest(approvalID, "biz-1", "approver-1", ""); err == nil {
		t.Fatal("an approval must not be resumable after a restart")
	}
	// And the recorded history still shows only the request — no grant exists.
	if len(rec.Approvals) != 1 || rec.Approvals[0].Outcome != execjournal.ApprovalRequested {
		t.Fatalf("history must not invent a decision after a restart: %+v", rec.Approvals)
	}
}

// A grant whose durable history write fails is a partial-commit state: the
// approval is already committed in-memory and the resume proceeds, but the
// granted entry is missing from the durable history. The caller is NOT told.
// A grant whose durable write fails must prevent the execution from resuming.
// The approval is committed in-memory but the execution must not run without
// durable authorization evidence. The caller receives an error.
func TestApproveRequestGrantWriteFailurePreventsResume(t *testing.T) {
	e, j, h := newJournaledGateEngineFailingOn(t, `"outcome":"granted"`)

	submitActionRequest(t, e, h, "exec-grant-partial", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-grant-partial"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}

	// The grant write fails: ApproveRequest must return an error.
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); err == nil {
		t.Fatal("ApproveRequest must return an error when the grant write fails")
	}

	// The execution must not have run.
	if h.effectCount() != 0 {
		t.Fatalf("the resumed execution must not run when the grant write fails, ran %d times", h.effectCount())
	}

	// The durable record must retain its truthful pre-resume state.
	rec, _, _ := j.Get("exec-grant-partial")
	if rec.State != execjournal.StateFailed || rec.ErrorClass != execjournal.ApprovalRequiredClass {
		t.Fatalf("record must stay failed/APPROVAL_REQUIRED, got %q/%q", rec.State, rec.ErrorClass)
	}
	for _, a := range rec.Approvals {
		if a.Outcome == execjournal.ApprovalGranted {
			t.Fatalf("no granted entry may be recorded when the write fails: %+v", rec.Approvals)
		}
	}
}

// After a failed grant write, the approval is APPROVED in-memory but the
// execution did not run. A retry returns ErrApprovalNotPending (the engine
// already decided). This is a documented terminal error — no silent success.
func TestApproveRequestGrantWriteFailureRetryFails(t *testing.T) {
	e, j, h := newJournaledGateEngineFailingOn(t, `"outcome":"granted"`)

	submitActionRequest(t, e, h, "exec-grant-retry", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-grant-retry"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}

	// First attempt: grant write fails, ApproveRequest returns error.
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); err == nil {
		t.Fatal("first ApproveRequest must return an error when the grant write fails")
	}

	// Retry: the approval is already APPROVED in-memory, so the engine
	// returns ErrApprovalNotPending. This is a documented terminal error.
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); err == nil {
		t.Fatal("retry must not silently succeed")
	}

	// The execution must never have run.
	if h.effectCount() != 0 {
		t.Fatalf("execution must never run, ran %d times", h.effectCount())
	}

	// The durable record must remain unchanged.
	rec, _, _ := j.Get("exec-grant-retry")
	if rec.State != execjournal.StateFailed || rec.ErrorClass != execjournal.ApprovalRequiredClass {
		t.Fatalf("record must stay failed/APPROVAL_REQUIRED, got %q/%q", rec.State, rec.ErrorClass)
	}
}

// A successful grant write enables the resume. The execution runs, completes,
// and the durable record shows the granted entry.
func TestApproveRequestGrantSuccessEnablesResume(t *testing.T) {
	e, j, h, _ := newJournaledGateEngine(t)

	submitActionRequest(t, e, h, "exec-grant-success", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-grant-success"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}
	// journalFinalize (Terminal + ApprovalRequested) runs after the result
	// is stored; wait for it so ApproveRequest doesn't race it and skip the
	// ApprovalRequested entry.
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		rec, ok, _ := j.Get("exec-grant-success")
		if ok && rec.ErrorClass == execjournal.ApprovalRequiredClass {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for journal approval-requested entry")
		case <-tick.C:
		}
	}
	// The grant write succeeds: ApproveRequest returns nil and the execution resumes.
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); err != nil {
		t.Fatalf("ApproveRequest must succeed when the grant write succeeds: %v", err)
	}

	// Wait for the resumed execution to complete.
	res := awaitResult(t, e, "exec-grant-success")
	if res.Status != "completed" {
		t.Fatalf("the resumed execution must complete: %s", res.Status)
	}

	// The execution must have run.
	if h.effectCount() != 1 {
		t.Fatalf("the resumed execution must run, ran %d times", h.effectCount())
	}

	// The durable record must show the completed outcome with the granted entry.
	rec, _, _ := j.Get("exec-grant-success")
	if rec.State != execjournal.StateCompleted {
		t.Fatalf("record must be completed, got %q (%s)", rec.State, rec.ErrorClass)
	}
	if len(rec.Approvals) != 2 ||
		rec.Approvals[0].Outcome != execjournal.ApprovalRequested ||
		rec.Approvals[1].Outcome != execjournal.ApprovalGranted {
		t.Fatalf("approval history must contain requested + granted: %+v", rec.Approvals)
	}
}

// Restart after a failed grant write does not invent a grant. The durable
// record remains the honest provisional pause.
func TestRestartAfterFailedGrantDoesNotInventGrant(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(failOnPutStore{Store: st, marker: `"outcome":"granted"`}, nil)
	h := &actionGateHandler{tool: "http.request"}
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	e.Governance().SetPolicies(append([]*governance.Policy{e.Governance().Policies()[0]},
		actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil)))
	e.journal = j
	h.engine = e
	if err := e.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	submitActionRequest(t, e, h, "exec-grant-restart", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-grant-restart"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); err == nil {
		t.Fatal("ApproveRequest must return an error when the grant write fails")
	}
	if err := e.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// "Restart": a brand-new store, journal and engine over the same directory.
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	j2 := execjournal.New(st2, nil)
	if _, err := j2.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}

	// The durable record must not contain a granted entry.
	rec, _, _ := j2.Get("exec-grant-restart")
	for _, a := range rec.Approvals {
		if a.Outcome == execjournal.ApprovalGranted {
			t.Fatalf("recovery must not invent a grant: %+v", rec.Approvals)
		}
	}
	if rec.State != execjournal.StateFailed || rec.ErrorClass != execjournal.ApprovalRequiredClass {
		t.Fatalf("record must stay failed/APPROVAL_REQUIRED, got %q/%q", rec.State, rec.ErrorClass)
	}
}

// Restart after a partial denial (history recorded, supersede failed) leaves
// the record failed/APPROVAL_REQUIRED with the denied entry still in history.
// Recovery must not invent a grant or rewrite the terminal state.
func TestRestartAfterPartialDenialPreservesHistory(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j2 := execjournal.New(failOnPutStore{Store: st, marker: `"error_class":"POLICY_DENIED"`}, nil)
	e2, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	e2.Governance().SetPolicies(append([]*governance.Policy{e2.Governance().Policies()[0]},
		actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil)))
	e2.journal = j2
	if err := e2.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	h := &actionGateHandler{tool: "http.request", engine: e2}

	submitActionRequest(t, e2, h, "exec-deny-restart", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e2, "exec-deny-restart"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}
	// journalFinalize (Terminal + ApprovalRequested) runs after the result
	// is stored; wait for it so DenyRequest doesn't race it and skip the
	// ApprovalRequested entry.
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		rec, ok, _ := j2.Get("exec-deny-restart")
		if ok && rec.ErrorClass == execjournal.ApprovalRequiredClass {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for journal approval-requested entry")
		case <-tick.C:
		}
	}
	if err := e2.DenyRequest(approvalID, "biz-1", "approver-1", ""); err == nil {
		t.Fatal("DenyRequest must return an error when the supersede fails")
	}
	if err := e2.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	st3, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	j3 := execjournal.New(st3, nil)
	if _, err := j3.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	rec, _, _ := j3.Get("exec-deny-restart")
	if rec.State != execjournal.StateFailed || rec.ErrorClass != execjournal.ApprovalRequiredClass {
		t.Fatalf("record must stay failed/APPROVAL_REQUIRED, got %q/%q", rec.State, rec.ErrorClass)
	}
	if len(rec.Approvals) != 2 ||
		rec.Approvals[0].Outcome != execjournal.ApprovalRequested ||
		rec.Approvals[1].Outcome != execjournal.ApprovalDenied {
		t.Fatalf("denial must stay in history after restart: %+v", rec.Approvals)
	}
}

// A second DenyRequest for the same approval fails because the approval was
// dropped from memory after the first denial. No duplicate history entry.
func TestSecondDenyRequestFailsWithApprovalNotFound(t *testing.T) {
	e, j, h, _ := newJournaledGateEngine(t)

	submitActionRequest(t, e, h, "exec-deny-twice", "user-1")
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-deny-twice"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}
	// journalFinalize (Terminal + ApprovalRequested) runs after the result
	// is stored; wait for it so DenyRequest doesn't race it and skip the
	// ApprovalRequested entry.
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		rec, ok, _ := j.Get("exec-deny-twice")
		if ok && rec.ErrorClass == execjournal.ApprovalRequiredClass {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for journal approval-requested entry")
		case <-tick.C:
		}
	}
	if err := e.DenyRequest(approvalID, "biz-1", "approver-1", ""); err != nil {
		t.Fatalf("deny: %v", err)
	}

	err := e.DenyRequest(approvalID, "biz-1", "approver-1", "")
	if !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("second DenyRequest must fail with ErrApprovalNotFound, got %v", err)
	}
	rec, _, _ := j.Get("exec-deny-twice")
	if len(rec.Approvals) != 2 {
		t.Fatalf("history must not duplicate: %+v", rec.Approvals)
	}
}

// The v25 guard survives: a completed or denied execution id cannot be
// re-admitted, and its durable evidence is not erased.
func TestCompletedAndDeniedIDsCannotBeReAdmitted(t *testing.T) {
	e, j, h, _ := newJournaledGateEngine(t)

	// Denied execution.
	submitActionRequest(t, e, h, "exec-guard-d", "user-1")
	id := approvalIDFrom(awaitResult(t, e, "exec-guard-d"))
	if err := e.DenyRequest(id, "biz-1", "approver-1", "no"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if err := e.SubmitRequest(&Request{
		ID: "exec-guard-d", Context: NewRequestContext("exec-guard-d", "biz-1", "user-1"),
		Intent: "again", Handler: h.handle,
	}); err == nil {
		t.Fatal("a denied execution id must not be re-admitted")
	}

	// Completed execution.
	submitActionRequest(t, e, h, "exec-guard-c", "user-1")
	id2 := approvalIDFrom(awaitResult(t, e, "exec-guard-c"))
	if err := e.ApproveRequest(id2, "biz-1", "approver-1", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	awaitResult(t, e, "exec-guard-c")
	if err := e.SubmitRequest(&Request{
		ID: "exec-guard-c", Context: NewRequestContext("exec-guard-c", "biz-1", "user-1"),
		Intent: "again", Handler: h.handle,
	}); err == nil {
		t.Fatal("a completed execution id must not be re-admitted")
	}

	// Both durable records survived the refused re-admission intact.
	for _, tc := range []struct{ id, state string }{
		{"exec-guard-d", execjournal.ApprovalDeniedClass},
		{"exec-guard-c", ""},
	} {
		rec, _, _ := j.Get(tc.id)
		if !rec.State.Terminal() {
			t.Fatalf("%s must stay terminal, got %q", tc.id, rec.State)
		}
		if rec.ErrorClass != tc.state {
			t.Fatalf("%s evidence must not be erased, got %q", tc.id, rec.ErrorClass)
		}
	}
}

// ---- v52: research evidence is atomic with the terminal record -----------

func coreSampleEvidence(title string) *execjournal.ResearchEvidence {
	return &execjournal.ResearchEvidence{
		Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
		Operation: execjournal.ResearchEvidenceOperation, Count: 1,
		Sources: []execjournal.ResearchSource{{
			ID: "src-1", Title: title, URL: "https://docs.example/nexus", Source: "docs.example",
			Snippet: "The runtime decides; the model proposes.",
		}},
	}
}

func waitForJournalState(t *testing.T, j *execjournal.Journal, id string, want execjournal.State) execjournal.ExecutionRecord {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec, ok, err := j.Get(id)
		if err != nil {
			t.Fatalf("journal get: %v", err)
		}
		if ok && rec.State == want {
			return rec
		}
		time.Sleep(2 * time.Millisecond)
	}
	rec, _, _ := j.Get(id)
	t.Fatalf("timed out waiting for journal state %q, got %+v", want, rec)
	return execjournal.ExecutionRecord{}
}

// The engine writes the agent's validated source snapshot in the same durable
// update as the terminal state, and the terminal timestamp is a real lifecycle
// fact that survives a restart.
func TestEnginePersistsResearchEvidenceWithTerminalState(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	e, err := NewEngine(nil, WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(ctx) })

	evidence := coreSampleEvidence("Nexus capability boundary")
	if err := e.SubmitRequest(&Request{
		ID: "exec-core-evidence", Context: NewRequestContext("exec-core-evidence", "biz-1", "actor-1"),
		Intent: "research", Handler: func(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
			return &executor.Outcome{Status: "completed", Output: "done", ResearchEvidence: evidence}, nil
		},
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res := awaitResult(t, e, "exec-core-evidence"); res.Status != "completed" {
		t.Fatalf("process-local result: %+v", res)
	}
	rec := waitForJournalState(t, j, "exec-core-evidence", execjournal.StateCompleted)
	if rec.CompletedAt == nil {
		t.Fatal("a terminal record must carry completed_at")
	}
	if rec.ResearchEvidence == nil || rec.ResearchEvidence.Count != 1 ||
		rec.ResearchEvidence.Sources[0].Title != "Nexus capability boundary" {
		t.Fatalf("terminal evidence missing: %+v", rec.ResearchEvidence)
	}

	// A fresh process over the same durable directory must expose exactly the
	// same evidence and terminal timestamp.
	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	j2 := execjournal.New(st2, nil)
	if _, err := j2.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	rec2, ok, err := j2.Get("exec-core-evidence")
	if err != nil || !ok {
		t.Fatalf("reopened get: ok=%v err=%v", ok, err)
	}
	if rec2.CompletedAt == nil || !rec2.CompletedAt.Equal(*rec.CompletedAt) {
		t.Fatalf("completed_at changed across restart: %v -> %v", rec.CompletedAt, rec2.CompletedAt)
	}
	if rec2.ResearchEvidence == nil || rec2.ResearchEvidence.Sources[0].URL != "https://docs.example/nexus" {
		t.Fatalf("evidence did not survive restart: %+v", rec2.ResearchEvidence)
	}
}

// Synthesis is now a durable artifact: the live terminal response carries the
// validated typed object, and the durable record persists it atomically with
// the evidence and evaluation it describes. A restart can resurrect the
// synthesis because it is bound to the exact evidence snapshot.
func TestEnginePersistsResearchSynthesisDurably(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	e, err := NewEngine(nil, WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(ctx) })

	evidence := coreSampleEvidence("Nexus capability boundary")
	synthesis := &executor.ResearchSynthesis{
		Status:  executor.ResearchSynthesisComplete,
		Summary: "Synthesized from the captured source.",
		EvidenceBacked: []executor.ResearchEvidenceStatement{{
			Statement: "The runtime decides and the model proposes.", SourceIDs: []string{"src-1"},
		}},
		Inferences:          []executor.ResearchInference{},
		Conflicts:           []executor.ResearchEvidenceStatement{},
		UnresolvedQuestions: []string{},
	}
	if err := e.SubmitRequest(&Request{
		ID: "exec-core-synthesis", Context: NewRequestContext("exec-core-synthesis", "biz-1", "actor-1"),
		Intent: "research", Handler: func(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
			return &executor.Outcome{
				Status: "completed", Output: "done",
				ResearchEvidence: evidence, ResearchSynthesis: synthesis,
			}, nil
		},
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	res := awaitResult(t, e, "exec-core-synthesis")
	if res.Outcome == nil || res.Outcome.ResearchSynthesis == nil ||
		res.Outcome.ResearchSynthesis.EvidenceBacked[0].SourceIDs[0] != "src-1" {
		t.Fatalf("process-local synthesis missing from response: %+v", res.Outcome)
	}
	rec := waitForJournalState(t, j, "exec-core-synthesis", execjournal.StateCompleted)
	if rec.ResearchSynthesis == nil {
		t.Fatalf("durable synthesis missing: %+v", rec.ResearchSynthesis)
	}
	if rec.ResearchSynthesis.Status != executor.ResearchSynthesisComplete {
		t.Fatalf("durable synthesis status = %q", rec.ResearchSynthesis.Status)
	}
	if len(rec.ResearchSynthesis.EvidenceBacked) != 1 ||
		rec.ResearchSynthesis.EvidenceBacked[0].Statement != "The runtime decides and the model proposes." ||
		len(rec.ResearchSynthesis.EvidenceBacked[0].SourceIDs) != 1 ||
		rec.ResearchSynthesis.EvidenceBacked[0].SourceIDs[0] != "src-1" {
		t.Fatalf("durable synthesis statements/citations mismatch: %+v", rec.ResearchSynthesis.EvidenceBacked)
	}
	if rec.ResearchSynthesis.EvidenceFingerprint != execjournal.ResearchEvidenceFingerprint(rec.ResearchEvidence) {
		t.Fatal("durable synthesis must stay bound to the persisted evidence")
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("research_synthesis")) {
		t.Fatalf("durable record must carry research_synthesis: %s", raw)
	}
	if rec.ResearchEvidence == nil || rec.ResearchEvidence.Count != 1 {
		t.Fatalf("durable evidence missing: %+v", rec.ResearchEvidence)
	}

	// A fresh process over the same durable directory must expose exactly the
	// same synthesis, evidence and terminal timestamp.
	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	j2 := execjournal.New(st2, nil)
	if _, err := j2.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	rec2, ok, err := j2.Get("exec-core-synthesis")
	if err != nil || !ok {
		t.Fatalf("reopened get: ok=%v err=%v", ok, err)
	}
	if rec2.ResearchSynthesis == nil {
		t.Fatal("durable synthesis did not survive restart")
	}
	if rec2.ResearchSynthesis.Status != executor.ResearchSynthesisComplete ||
		len(rec2.ResearchSynthesis.EvidenceBacked) != 1 ||
		rec2.ResearchSynthesis.EvidenceBacked[0].SourceIDs[0] != "src-1" {
		t.Fatalf("durable synthesis changed across restart: %+v", rec2.ResearchSynthesis)
	}
	if rec2.ResearchSynthesis.EvidenceFingerprint != execjournal.ResearchEvidenceFingerprint(rec2.ResearchEvidence) {
		t.Fatal("durable synthesis evidence binding changed across restart")
	}
	if !rec2.CompletedAt.Equal(*rec.CompletedAt) {
		t.Fatalf("completed_at changed across restart: %v -> %v", rec.CompletedAt, rec2.CompletedAt)
	}
}

// If the atomic terminal-with-evidence write fails, the execution is NOT
// durably completed and exposes no evidence. Recovery classifies the interrupted
// record unknown and does not re-run the handler. A process-local result cannot
// be promoted to durable evidence by a later write.
func TestEngineEvidenceWriteFailureNeverClaimsDurableEvidenceOrReplays(t *testing.T) {
	dir := t.TempDir()
	base, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	failing := failOnPutStore{Store: base, marker: `"research_evidence"`}
	j := execjournal.New(failing, nil)
	e, err := NewEngine(nil, WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	var ran int
	if err := e.SubmitRequest(&Request{
		ID: "exec-core-evidence-fail", Context: NewRequestContext("exec-core-evidence-fail", "biz-1", "actor-1"),
		Intent: "research", Handler: func(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
			ran++
			return &executor.Outcome{Status: "completed", Output: "done",
				ResearchEvidence: coreSampleEvidence("Not durable")}, nil
		},
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	res := awaitResult(t, e, "exec-core-evidence-fail")
	if res.Status != "completed" {
		t.Fatalf("process-local result: %+v", res)
	}
	// Give journalFinalize time to fail and leave the record untouched.
	time.Sleep(20 * time.Millisecond)
	rec, ok, err := j.Get("exec-core-evidence-fail")
	if err != nil || !ok {
		t.Fatalf("journal get: ok=%v err=%v", ok, err)
	}
	if rec.State.Terminal() || rec.CompletedAt != nil || rec.ResearchEvidence != nil {
		t.Fatalf("failed atomic write must not claim completion/evidence: %+v", rec)
	}
	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// Restart: recovery must classify the interrupted record unknown, not
	// completed, and it must not silently re-run the handler.
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	j2 := execjournal.New(st2, nil)
	if _, err := j2.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	rec2, ok, err := j2.Get("exec-core-evidence-fail")
	if err != nil || !ok {
		t.Fatalf("reopened get: ok=%v err=%v", ok, err)
	}
	if rec2.State != execjournal.StateUnknown {
		t.Fatalf("interrupted record must be unknown, got %q", rec2.State)
	}
	if rec2.CompletedAt != nil || rec2.ResearchEvidence != nil {
		t.Fatalf("recovered record must not fabricate completion/evidence: %+v", rec2)
	}
	if ran != 1 {
		t.Fatalf("the handler must not be replayed, ran %d times", ran)
	}
}
