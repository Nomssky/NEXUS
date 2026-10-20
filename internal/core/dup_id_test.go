package core

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
)

// TestDuplicateRequestIDIsRejected reproduces a correctness gap: submitting
// two requests with the same ID causes both to be admitted and executed,
// producing duplicate side effects. The Engine should reject a duplicate
// in-flight/completed request ID instead of running it twice.
func TestDuplicateRequestIDIsRejected(t *testing.T) {
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(ctx)

	var execCount atomic.Int32
	handler := func(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
		execCount.Add(1)
		return &executor.Outcome{TaskID: req.TaskID, Status: "completed", Output: "ok"}, nil
	}

	reqID := "dup-req-1"
	reqCtx := NewRequestContext(reqID, "biz-1", "actor-1")

	req1 := &Request{ID: reqID, Context: reqCtx, Intent: "test", Handler: handler}
	req2 := &Request{ID: reqID, Context: reqCtx, Intent: "test", Handler: handler}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = e.SubmitRequest(req1)
	}()
	go func() {
		defer wg.Done()
		_ = e.SubmitRequest(req2)
	}()
	wg.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := e.GetResult(reqID); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	count := execCount.Load()
	t.Logf("executions for duplicate ID: %d", count)
	if count != 1 {
		t.Fatalf("duplicate request ID executed %d times, want 1", count)
	}
}

// TestDuplicateCompletedRequestIDIsRejected verifies that a request ID that
// already has a terminal result cannot be re-admitted for a second execution.
func TestDuplicateCompletedRequestIDIsRejected(t *testing.T) {
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(ctx)

	var execCount atomic.Int32
	handler := func(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
		execCount.Add(1)
		return &executor.Outcome{TaskID: req.TaskID, Status: "completed", Output: "ok"}, nil
	}

	reqID := "dup-req-completed"
	reqCtx := NewRequestContext(reqID, "biz-1", "actor-1")

	req1 := &Request{ID: reqID, Context: reqCtx, Intent: "test", Handler: handler}
	if err := e.SubmitRequest(req1); err != nil {
		t.Fatalf("first SubmitRequest: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := e.GetResult(reqID); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := e.GetResult(reqID); !ok {
		t.Fatal("first request never reached a terminal result")
	}

	// Re-submitting the same ID must be rejected, not executed again.
	req2 := &Request{ID: reqID, Context: reqCtx, Intent: "test", Handler: handler}
	err = e.SubmitRequest(req2)
	if err == nil {
		t.Fatal("expected duplicate completed request to be rejected")
	}
	if err.Error() != "duplicate request: \"dup-req-completed\" already completed" {
		t.Fatalf("unexpected duplicate error: %v", err)
	}

	count := execCount.Load()
	t.Logf("executions for already-completed duplicate ID: %d", count)
	if count != 1 {
		t.Fatalf("already-completed duplicate ID executed %d times, want 1", count)
	}
}

// TestDuplicateDetectionAtomicUnderAdmitMu deterministically exercises both
// duplicate-detection code paths inside the admitMu critical section:
//  1. an in-flight registration blocks re-admission ("already in flight"),
//  2. after the result is stored and inflight deregistered, re-admission is
//     rejected as "already completed".
//
// This establishes that the terminal-result check and the inflight check are
// both evaluated under admitMu, so a concurrent completion cannot create the
// gap where a request passes both checks and registers a duplicate ID.
func TestDuplicateDetectionAtomicUnderAdmitMu(t *testing.T) {
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(ctx)

	reqID := "atomic-dup-1"
	reqCtx := NewRequestContext(reqID, "biz-1", "actor-1")

	// 1. Manually register an in-flight request (simulating an admitted run
	//    that has not yet stored a terminal result).
	e.admitMu.Lock()
	e.inflight[reqID] = &inflightRequest{
		requestID:  reqID,
		businessID: "biz-1",
		state:      inflightExecuting,
	}
	e.admitMu.Unlock()

	// 2. Submitting the same ID while in-flight must be rejected.
	dup := &Request{ID: reqID, Context: reqCtx, Intent: "test"}
	err = e.SubmitRequest(dup)
	if err == nil {
		t.Fatal("expected duplicate in-flight request to be rejected")
	}
	if err.Error() != `duplicate request: "atomic-dup-1" already in flight` {
		t.Fatalf("unexpected in-flight error: %v", err)
	}

	// 3. Store a terminal result and deregister inflight (simulating a
	//    concurrent completion: result stored under resultsMu, then inflight
	//    deregistered under admitMu).
	e.resultsMu.Lock()
	e.results[reqID] = &Response{RequestID: reqID, BusinessID: "biz-1", Status: "completed"}
	e.resultOrder = append(e.resultOrder, reqID)
	e.resultsMu.Unlock()

	e.admitMu.Lock()
	delete(e.inflight, reqID)
	e.admitMu.Unlock()

	// 4. Submitting the same ID after completion must be rejected as completed.
	dup2 := &Request{ID: reqID, Context: reqCtx, Intent: "test"}
	err = e.SubmitRequest(dup2)
	if err == nil {
		t.Fatal("expected duplicate completed request to be rejected")
	}
	if err.Error() != `duplicate request: "atomic-dup-1" already completed` {
		t.Fatalf("unexpected completed error: %v", err)
	}

	// 5. The original result remains unchanged — no re-execution occurred.
	if res, ok := e.GetResult(reqID); !ok || res.Status != "completed" {
		t.Fatalf("original result was modified or lost: %+v", res)
	}
}
