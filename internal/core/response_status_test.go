package core

import (
	"context"
	"testing"
	"time"
)

// TEST-CORE-070 (P0 regression): an executor outcome that is not "completed"
// must never become a chain "completed" response. The default engine has an
// empty model registry, so the default handler's provider invocation fails
// (E-008) → executor status=failed → WaitOutcome returns (outcome, nil) —
// the chain must surface failed + error envelope, not false success.
func TestExecutorFailureNotFalseCompleted(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:       "req-exec-fail-honest",
		Context:  NewRequestContext("corr-exec-fail", "biz-1", "user-1"),
		Intent:   "must not false-complete",
		Priority: 5,
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}

	result := waitForResult(t, e, req.ID)
	if result.Status == "completed" {
		t.Fatalf("P0: executor failure reported as completed (outcome metrics: %+v)",
			result.Outcome)
	}
	if result.Status != "failed" {
		t.Errorf("expected failed, got %q", result.Status)
	}
	if result.Error == nil {
		t.Fatal("expected a non-nil error envelope on failure")
	}
	if result.Error.Code == "" || result.Error.Category == "" || result.Error.Message == "" {
		t.Errorf("error envelope must be contract-shaped, got %+v", result.Error)
	}
}
