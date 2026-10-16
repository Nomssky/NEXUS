package capability

// Operational metrics at the tool boundary
// (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md §3.3, §11).
//
// The capability platform is the single authoritative logical-invocation
// boundary, so these tests drive real dispatches (including real bounded
// retries) and assert the exact separation the contract requires: one logical
// call, its physical attempts, and its outcome — where a retry never becomes a
// new logical call and an unknown outcome is never a completion.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/metrics"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// newMetricsPlatform wires a metrics owner into a fresh platform.
func newMetricsPlatform(t *testing.T, register func(reg *tool.ToolRegistry)) (*Platform, *metrics.Metrics) {
	t.Helper()
	reg := tool.NewToolRegistry()
	bus := event.NewMemBus()
	p := New(reg, testMembers(t), NewScopedCredentialResolver(security.NewDevResolver()), bus)
	m := metrics.New()
	p.Metrics = m
	if register != nil {
		register(reg)
	}
	return p, m
}

// A successful single-attempt call: one logical call, one attempt, no retry.
func TestToolMetricsForCompletedSingleAttemptCall(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	p, m := newMetricsPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("m.ok"), ad)
	})

	res := p.Invoke(context.Background(), allowedRequest("m.ok"))
	if res.Outcome != "completed" {
		t.Fatalf("expected completed, got %q", res.Outcome)
	}

	snap := m.Snapshot()
	if snap.ToolCalls != 1 {
		t.Fatalf("logical calls = %d, want 1", snap.ToolCalls)
	}
	if snap.ToolAttempts != 1 {
		t.Fatalf("physical attempts = %d, want 1", snap.ToolAttempts)
	}
	if snap.ToolRetries != 0 {
		t.Fatalf("retries = %d, want 0", snap.ToolRetries)
	}
	if snap.ToolCompleted != 1 {
		t.Fatalf("completed = %d, want 1", snap.ToolCompleted)
	}
}

// Obligation 3: a call that is retried counts as ONE logical call, several
// physical attempts, and the attempts beyond the first are retries.
func TestRetryDoesNotIncrementLogicalInvocationCount(t *testing.T) {
	// A read whose first attempt fails transiently is retried exactly once.
	ad := &scriptedAdapter{ops: []string{"execute"},
		plan: []step{{err: fmt.Errorf("%w: transient", ErrExternal)}, okStep()}}
	p, m := newMetricsPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("m.retry"), ad)
	})

	res := p.Invoke(context.Background(), allowedRequest("m.retry"))
	if res.Outcome != "completed" {
		t.Fatalf("expected the retry to succeed, got %q", res.Outcome)
	}
	if res.Attempts != 2 {
		t.Fatalf("expected 2 physical attempts, got %d", res.Attempts)
	}

	snap := m.Snapshot()
	if snap.ToolCalls != 1 {
		t.Fatalf("a retry must NOT create a new logical invocation, calls = %d", snap.ToolCalls)
	}
	if snap.ToolAttempts != 2 {
		t.Fatalf("physical attempts = %d, want 2", snap.ToolAttempts)
	}
	if snap.ToolRetries != 1 {
		t.Fatalf("retries = %d, want 1", snap.ToolRetries)
	}
	// Exactly one outcome for the logical call.
	if snap.ToolCompleted != 1 {
		t.Fatalf("completed = %d, want 1 (one outcome per logical call)", snap.ToolCompleted)
	}
	if total := snap.ToolCompleted + snap.ToolFailed + snap.ToolUnknown +
		snap.ToolTimedOut + snap.ToolCancelled; total != 1 {
		t.Fatalf("exactly one outcome must be counted, got %d", total)
	}
}

// Obligation 7: an indeterminate outcome is counted as unknown, never completed.
func TestUnknownToolOutcomeIsNeverCountedAsCompleted(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"},
		plan: []step{{err: fmt.Errorf("%w: connection lost after dispatch", ErrUnknownOutcome)}}}
	p, m := newMetricsPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(mutationManifest("m.unknown"), ad)
	})

	res := p.Invoke(context.Background(), allowedRequest("m.unknown"))
	if res.Outcome != "unknown" {
		t.Fatalf("expected unknown, got %q", res.Outcome)
	}

	snap := m.Snapshot()
	if snap.ToolUnknown != 1 {
		t.Fatalf("unknown = %d, want 1", snap.ToolUnknown)
	}
	if snap.ToolCompleted != 0 {
		t.Fatalf("an uncertain outcome must never be counted completed, got %d", snap.ToolCompleted)
	}
	if snap.ToolFailed != 0 {
		t.Fatalf("unknown must not be downgraded to failed, got %d", snap.ToolFailed)
	}
	if snap.ToolCalls != 1 || snap.ToolAttempts != 1 {
		t.Fatalf("the logical call and its attempt must still be counted: %+v", snap)
	}
}

func TestFailedAndTimedOutOutcomesAreCountedSeparately(t *testing.T) {
	failAd := &scriptedAdapter{ops: []string{"execute"},
		plan: []step{{err: fmt.Errorf("%w: boom", ErrExternal)}, {err: fmt.Errorf("%w: boom", ErrExternal)}}}
	p, m := newMetricsPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("m.failed"), failAd)
	})
	if res := p.Invoke(context.Background(), allowedRequest("m.failed")); res.Outcome != "failed" {
		t.Fatalf("expected failed, got %q", res.Outcome)
	}

	slowAd := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{delay: 2 * time.Second}}}
	p2, m2 := newMetricsPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("m.slow"), slowAd)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	res := p2.Invoke(ctx, allowedRequest("m.slow"))
	if res.Outcome != "timed_out" && res.Outcome != "cancelled" {
		t.Fatalf("expected a bounded deadline outcome, got %q", res.Outcome)
	}

	if got := m.Snapshot().ToolFailed; got != 1 {
		t.Fatalf("failed = %d, want 1", got)
	}
	snap2 := m2.Snapshot()
	if snap2.ToolTimedOut+snap2.ToolCancelled != 1 {
		t.Fatalf("the deadline outcome must be counted as timed_out or cancelled, got %+v", snap2)
	}
	if snap2.ToolCompleted != 0 {
		t.Fatalf("a deadline outcome must not be counted completed, got %d", snap2.ToolCompleted)
	}
}

// Obligation 3.4: a refusal decided BEFORE dispatch is not a logical call.
func TestPreDispatchRefusalIsNotCountedAsALogicalCall(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	p, m := newMetricsPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("m.denied"), ad)
	})

	// Unknown tool: rejected before any dispatch.
	res := p.Invoke(context.Background(), allowedRequest("m.no-such-tool"))
	if res.Status == tool.StatusSuccess {
		t.Fatalf("an unknown tool must not succeed, got %+v", res)
	}
	if got := m.Snapshot(); got.ToolCalls != 0 || got.ToolAttempts != 0 {
		t.Fatalf("a pre-dispatch refusal must not count as an invocation: %+v", got)
	}
	if ad.attempts() != 0 {
		t.Fatalf("a pre-dispatch refusal must not reach the adapter, attempts=%d", ad.attempts())
	}
}

// A disabled capability is refused before dispatch and is not a logical call.
func TestDisabledCapabilityIsNotCountedAsALogicalCall(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	p, m := newMetricsPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("m.disabled"), ad)
	})
	p.SetCapabilityState("m.disabled", CapabilityDisabled)

	if res := p.Invoke(context.Background(), allowedRequest("m.disabled")); res.Status == tool.StatusSuccess {
		t.Fatalf("a disabled capability must not succeed, got %+v", res)
	}
	if got := m.Snapshot(); got.ToolCalls != 0 {
		t.Fatalf("a disabled capability must not count as an invocation: %+v", got)
	}
}

// Obligation 14: instrumentation never blocks or changes a dispatch. With no
// metrics owner wired, the platform must behave exactly as shipped.
func TestNilMetricsOwnerDoesNotChangeDispatch(t *testing.T) {
	reg := tool.NewToolRegistry()
	bus := event.NewMemBus()
	p := New(reg, testMembers(t), NewScopedCredentialResolver(security.NewDevResolver()), bus)
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	_ = reg.Register(readManifest("m.nil"), ad)

	// p.Metrics is nil here on purpose.
	if p.Metrics != nil {
		t.Fatal("this test requires no metrics owner")
	}
	if res := p.Invoke(context.Background(), allowedRequest("m.nil")); res.Outcome != "completed" {
		t.Fatalf("dispatch must succeed without metrics, got %q", res.Outcome)
	}
	if ad.attempts() != 1 {
		t.Fatalf("exactly one attempt expected, got %d", ad.attempts())
	}
}

// Several logical calls aggregate independently and stay separated from
// physical attempts.
func TestMultipleLogicalCallsAggregateIndependently(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"},
		plan: []step{okStep(), okStep(), okStep()}}
	p, m := newMetricsPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("m.multi"), ad)
	})

	for i := 0; i < 3; i++ {
		req := allowedRequest("m.multi")
		req.CorrelationID = fmt.Sprintf("corr-%d", i)
		if res := p.Invoke(context.Background(), req); res.Outcome != "completed" {
			t.Fatalf("call %d expected completed, got %q", i, res.Outcome)
		}
	}

	snap := m.Snapshot()
	if snap.ToolCalls != 3 || snap.ToolAttempts != 3 || snap.ToolRetries != 0 || snap.ToolCompleted != 3 {
		t.Fatalf("three clean calls must aggregate exactly: %+v", snap)
	}
}
