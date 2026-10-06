package capability

// Behavioural tests for Operational Reliability & Tool Semantics v1
// (contracts/OPERATIONAL_RELIABILITY_CONTRACTS.md): one logical call, bounded
// attempts, an explicit terminal outcome, a real lifecycle gate, deadline-bounded
// retries, stable idempotency identity and auditable telemetry.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// loopbackPolicyFor is the explicit test-only policy that makes a loopback
// fixture reachable. Production policy never sets AllowLoopback.
func loopbackPolicyFor(url string) HTTPPolicy {
	p := DefaultHTTPPolicy()
	p.AllowInsecureHTTP = true
	p.AllowLoopback = true
	p.AllowedHosts = []string{strings.TrimPrefix(url, "http://")}
	return p
}

// ---------- fixtures ----------

// scriptedAdapter dispatches a scripted sequence of errors/results, recording
// every invocation it received so attempt-level behaviour can be asserted.
type scriptedAdapter struct {
	ops []string
	// plan is consumed one entry per attempt; the last entry repeats.
	plan []step
	mu   sync.Mutex
	seen []tool.Invocation
}

type step struct {
	result tool.RawResult
	err    error
	delay  time.Duration
}

func (s *scriptedAdapter) Operations() []string { return s.ops }

func (s *scriptedAdapter) Invoke(ctx context.Context, inv tool.Invocation) (tool.RawResult, error) {
	s.mu.Lock()
	i := len(s.seen)
	s.seen = append(s.seen, inv)
	plan := s.plan
	s.mu.Unlock()
	if i >= len(plan) {
		i = len(plan) - 1
	}
	st := plan[i]
	if st.delay > 0 {
		select {
		case <-time.After(st.delay):
		case <-ctx.Done():
			return tool.RawResult{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return tool.RawResult{}, err
	}
	return st.result, st.err
}

func (s *scriptedAdapter) attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

func (s *scriptedAdapter) invocations() []tool.Invocation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tool.Invocation(nil), s.seen...)
}

// idempotentStub advertises the §7 mutation de-duplication capability, which is
// what makes a re-issue admissible for a mutation.
type idempotentStub struct{ *scriptedAdapter }

func (i idempotentStub) SupportsIdempotency() bool { return true }

func okStep() step {
	return step{result: tool.RawResult{Result: map[string]string{"output": "ok"}}}
}

func readManifest(id string) tool.ToolManifest {
	m := baseManifest(id)
	m.SideEffectClass = tool.SideEffectRead
	return m
}

func mutationManifest(id string) tool.ToolManifest {
	m := baseManifest(id)
	m.SideEffectClass = tool.SideEffectExternalMutation
	m.Category = tool.ToolCategoryWrite
	return m
}

func eventTypes(bus *event.MemBus) []event.EventType {
	out := []event.EventType{}
	for _, e := range recorderOf(bus).all() {
		out = append(out, e.Type)
	}
	return out
}

func countEvents(bus *event.MemBus, t event.EventType) int {
	n := 0
	for _, e := range recorderOf(bus).all() {
		if e.Type == t {
			n++
		}
	}
	return n
}

// eventFields decodes the metadata-only payload of one event type.
func eventFields(bus *event.MemBus, t event.EventType) []map[string]string {
	out := []map[string]string{}
	for _, e := range recorderOf(bus).all() {
		if e.Type != t {
			continue
		}
		fields := map[string]string{}
		if len(e.Data) > 0 {
			if err := json.Unmarshal(e.Data, &fields); err != nil {
				fields["__undecodable__"] = string(e.Data)
			}
		}
		out = append(out, fields)
	}
	return out
}

// ---------- §1/§3 outcome model ----------

func TestOutcomeCompletedCarriesAttemptCount(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("out.ok"), ad)
	})
	res := p.Invoke(context.Background(), allowedRequest("out.ok"))
	if res.Status != tool.StatusSuccess || res.Outcome != "completed" {
		t.Fatalf("successful call must report outcome=completed, got %+v", res)
	}
	if res.Attempts != 1 {
		t.Fatalf("one logical call must report exactly one attempt, got %d", res.Attempts)
	}
	if res.CapabilityState != string(CapabilityEnabled) {
		t.Fatalf("default lifecycle state must be enabled, got %q", res.CapabilityState)
	}
}

func TestOutcomeFailedIsExplicit(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{err: fmt.Errorf("%w: boom", ErrExternal)}}}
	p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("out.failed"), ad)
	})
	res := p.Invoke(context.Background(), allowedRequest("out.failed"))
	if res.Outcome != "failed" || res.Attempts != 2 {
		t.Fatalf("a terminal external failure on a read is failed after the bounded retry, got %+v", res)
	}
	if res.RetryRecommended {
		t.Fatalf("a call that already consumed its retry budget must not recommend another")
	}
}

func TestOutcomeUnknownIsDistinctFromFailed(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{err: fmt.Errorf("%w: connection lost after dispatch", ErrUnknownOutcome)}}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(mutationManifest("out.unknown"), ad)
	})
	res := p.Invoke(context.Background(), allowedRequest("out.unknown"))
	if res.Outcome != "unknown" {
		t.Fatalf("indeterminate mutation must classify as unknown, got %q (error=%q)", res.Outcome, res.Error)
	}
	if res.RetryRecommended {
		t.Fatalf("an unknown outcome must never recommend a retry (§3)")
	}
	if !strings.HasPrefix(res.Error, ErrUnknownOutcome.Error()+":") {
		t.Fatalf("unknown outcome must stay visible in the error surface, got %q", res.Error)
	}
	if ad.attempts() != 1 {
		t.Fatalf("an unknown outcome must never be re-dispatched, attempts=%d", ad.attempts())
	}
	if !hasEvent(bus, event.EventTypeToolInvocationUnknownOutcome) {
		t.Fatalf("unknown outcome must emit tool.invocation.unknown, events=%v", eventTypes(bus))
	}
	if hasEvent(bus, event.EventTypeToolInvocationFailed) {
		t.Fatalf("unknown must not be downgraded to a plain failure event, events=%v", eventTypes(bus))
	}
}

func TestOutcomeCancelledIsNotRetried(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{delay: 2 * time.Second}}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("out.cancelled"), ad)
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	res := p.Invoke(ctx, allowedRequest("out.cancelled"))
	if res.Outcome != "cancelled" {
		t.Fatalf("a cancelled call must report outcome=cancelled, got %q", res.Outcome)
	}
	if res.Attempts != 1 {
		t.Fatalf("cancellation is not a retry condition, attempts=%d", res.Attempts)
	}
	if hasEvent(bus, event.EventTypeToolRetryScheduled) {
		t.Fatalf("cancellation must never schedule a retry, events=%v", eventTypes(bus))
	}
	if !hasEvent(bus, event.EventTypeToolInvocationCancelled) {
		t.Fatalf("cancelled outcome must be visible in telemetry, events=%v", eventTypes(bus))
	}
}

func TestOutcomeTimedOutFromInvocationDeadline(t *testing.T) {
	m := readManifest("out.timeout")
	m.ResourceLimits = tool.ResourceLimits{MaxDuration: 120 * time.Millisecond}
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{delay: 5 * time.Second}}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(m, ad)
	})
	res := p.Invoke(context.Background(), allowedRequest("out.timeout"))
	if res.Outcome != "timed_out" {
		t.Fatalf("deadline expiry must classify as timed_out, got %q (error=%q)", res.Outcome, res.Error)
	}
	if !hasEvent(bus, event.EventTypeToolInvocationTimedOut) {
		t.Fatalf("timed_out must emit tool.invocation.timed_out, events=%v", eventTypes(bus))
	}
}

// ---------- §2 retries ----------

func TestReadRetriesOnceOnTransient(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{
		{err: fmt.Errorf("%w: dial reset", ErrExternal)},
		okStep(),
	}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("retry.read"), ad)
	})
	res := p.Invoke(context.Background(), allowedRequest("retry.read"))
	if res.Status != tool.StatusSuccess || res.Attempts != 2 {
		t.Fatalf("a transient read failure must be retried once, got %+v", res)
	}
	if countEvents(bus, event.EventTypeToolRetryScheduled) != 1 {
		t.Fatalf("exactly one retry must be scheduled, events=%v", eventTypes(bus))
	}
	if countEvents(bus, event.EventTypeToolAttemptStarted) != 2 {
		t.Fatalf("each physical attempt must be framed, events=%v", eventTypes(bus))
	}
}

func TestReadRetryIsBoundedToOne(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{err: fmt.Errorf("%w: always down", ErrExternal)}}}
	p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("retry.bounded"), ad)
	})
	res := p.Invoke(context.Background(), allowedRequest("retry.bounded"))
	if res.Attempts != 2 {
		t.Fatalf("attempt budget must be 2 for reads, got %d", res.Attempts)
	}
	if ad.attempts() != 2 {
		t.Fatalf("adapter must be dispatched at most twice, got %d", ad.attempts())
	}
}

func TestNonRetryableClassesNeverRetry(t *testing.T) {
	cases := map[string]error{
		"validation":     fmt.Errorf("%w: bad input", ErrValidation),
		"permission":     fmt.Errorf("%w: no", ErrPermission),
		"resource limit": fmt.Errorf("%w: too big", ErrResourceLimit),
		"unknown":        fmt.Errorf("%w: indeterminate", ErrUnknownOutcome),
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{err: err}}}
			p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
				_ = reg.Register(readManifest("retry."+name), ad)
			})
			_ = p.Invoke(context.Background(), allowedRequest("retry."+name))
			if ad.attempts() != 1 {
				t.Fatalf("%s must never be retried, attempts=%d", name, ad.attempts())
			}
			if hasEvent(bus, event.EventTypeToolRetryScheduled) {
				t.Fatalf("%s must not schedule a retry, events=%v", name, eventTypes(bus))
			}
		})
	}
}

func TestIndeterminateMutationIsNeverRetried(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{err: fmt.Errorf("%w: connection reset", ErrExternal)}}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(mutationManifest("retry.mutation"), ad)
	})
	res := p.Invoke(context.Background(), allowedRequest("retry.mutation"))
	if ad.attempts() != 1 || res.Attempts != 1 {
		t.Fatalf("an indeterminate mutation must never be re-dispatched, attempts=%d adapter=%d", res.Attempts, ad.attempts())
	}
	if res.RetryRecommended {
		t.Fatalf("an indeterminate mutation must never recommend an automatic retry")
	}
	if hasEvent(bus, event.EventTypeToolRetryScheduled) {
		t.Fatalf("mutation retry must be suppressed, events=%v", eventTypes(bus))
	}
}

func TestMutationRetriesOnlyWhenProvenNotSentAndIdempotent(t *testing.T) {
	t.Run("not sent and idempotent re-issues once", func(t *testing.T) {
		base := &scriptedAdapter{ops: []string{"execute"}, plan: []step{
			{err: fmt.Errorf("%w: dial refused", ErrNotSent)},
			okStep(),
		}}
		ad := idempotentStub{base}
		p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
			_ = reg.Register(mutationManifest("retry.notsent.idem"), ad)
		})
		res := p.Invoke(context.Background(), allowedRequest("retry.notsent.idem"))
		if res.Status != tool.StatusSuccess || res.Attempts != 2 {
			t.Fatalf("a provably unsent mutation may be re-issued once, got %+v", res)
		}
		if countEvents(bus, event.EventTypeToolRetryScheduled) != 1 {
			t.Fatalf("the re-issue must be framed, events=%v", eventTypes(bus))
		}
		invs := ad.invocations()
		if invs[0].IdempotencyKey != invs[1].IdempotencyKey {
			t.Fatalf("the re-issue must carry the same idempotency key")
		}
	})
	t.Run("not sent but not idempotent stays single-shot", func(t *testing.T) {
		ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{
			{err: fmt.Errorf("%w: dial refused", ErrNotSent)},
			okStep(),
		}}
		p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
			_ = reg.Register(mutationManifest("retry.notsent.plain"), ad)
		})
		res := p.Invoke(context.Background(), allowedRequest("retry.notsent.plain"))
		if res.Attempts != 1 || ad.attempts() != 1 {
			t.Fatalf("a capability without idempotency support must not re-issue, attempts=%d", res.Attempts)
		}
	})
	t.Run("idempotent but unknown stays single-shot", func(t *testing.T) {
		base := &scriptedAdapter{ops: []string{"execute"}, plan: []step{
			{err: fmt.Errorf("%w: response lost", ErrUnknownOutcome)},
			okStep(),
		}}
		ad := idempotentStub{base}
		p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
			_ = reg.Register(mutationManifest("retry.unknown.idem"), ad)
		})
		res := p.Invoke(context.Background(), allowedRequest("retry.unknown.idem"))
		if res.Attempts != 1 || res.Outcome != "unknown" {
			t.Fatalf("an unknown outcome must never be re-dispatched, got %+v", res)
		}
	})
}

// ---------- §5 deadline hierarchy ----------

func TestRetryConsumesTheSharedCallDeadline(t *testing.T) {
	m := readManifest("deadline.shared")
	m.ResourceLimits = tool.ResourceLimits{MaxDuration: 300 * time.Millisecond}
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{delay: 5 * time.Second}}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(m, ad)
	})
	start := time.Now()
	res := p.Invoke(context.Background(), allowedRequest("deadline.shared"))
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("retries must share one call deadline, whole call took %s", elapsed)
	}
	if res.Outcome != "timed_out" {
		t.Fatalf("deadline exhaustion must classify as timed_out, got %q", res.Outcome)
	}
	if res.Attempts > 2 {
		t.Fatalf("the attempt bound is 2 for reads, got %d", res.Attempts)
	}
	if countEvents(bus, event.EventTypeToolRetryScheduled) > 1 {
		t.Fatalf("at most one retry may be scheduled inside one call deadline, events=%v", eventTypes(bus))
	}
}

func TestParentDeadlineBoundsChildCall(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{delay: 5 * time.Second}}}
	p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("deadline.parent"), ad)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	res := p.Invoke(ctx, allowedRequest("deadline.parent"))
	if time.Since(start) > time.Second {
		t.Fatalf("the parent deadline must bound the call, took %s", time.Since(start))
	}
	if res.Outcome != "timed_out" {
		t.Fatalf("a parent deadline expiry must classify as timed_out, got %q", res.Outcome)
	}
}

func TestRetryCannotExtendTheCallDeadline(t *testing.T) {
	m := readManifest("deadline.nofit")
	m.ResourceLimits = tool.ResourceLimits{MaxDuration: 300 * time.Millisecond}
	// The first attempt burns most of the shared deadline before failing. The
	// retry is admissible by class, but it only has the time that is left: it
	// is cut short and cannot succeed, because a retry never receives a fresh
	// deadline (§5).
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{
		{delay: 260 * time.Millisecond, err: fmt.Errorf("%w: late transient", ErrExternal)},
		{delay: 2 * time.Second},
	}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(m, ad)
	})
	start := time.Now()
	res := p.Invoke(context.Background(), allowedRequest("deadline.nofit"))
	elapsed := time.Since(start)
	if elapsed > 700*time.Millisecond {
		t.Fatalf("a retry must not extend the call deadline: whole call took %s", elapsed)
	}
	if res.Status == tool.StatusSuccess || res.Outcome != "timed_out" {
		t.Fatalf("the cut-short retry must end the call as timed_out, got %+v", res)
	}
	if res.Attempts != 2 {
		t.Fatalf("both attempts must be counted, got %d", res.Attempts)
	}
	if !hasEvent(bus, event.EventTypeToolRetryScheduled) {
		t.Fatalf("the admitted retry must be announced, events=%v", eventTypes(bus))
	}
}

func TestDeadlineExhaustedBeforeAnyAttempt(t *testing.T) {
	m := readManifest("deadline.exhausted")
	m.ResourceLimits = tool.ResourceLimits{MaxDuration: 300 * time.Millisecond}
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{delay: 2 * time.Second}}}
	p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(m, ad)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	res := p.Invoke(ctx, allowedRequest("deadline.exhausted"))
	if res.Outcome != "timed_out" {
		t.Fatalf("an exhausted parent deadline must classify as timed_out, got %+v", res)
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("attempts must not outlive the parent deadline, took %s", elapsed)
	}
	// Both attempts were cut short by the same deadline, and the attempt count
	// is reported honestly.
	if res.Attempts != 2 || ad.attempts() != 2 {
		t.Fatalf("both physical attempts must be counted, result=%d adapter=%d", res.Attempts, ad.attempts())
	}
}

// ---------- §7 idempotency identity ----------

func TestIdempotencyIdentityIsStableAcrossAttempts(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{
		{err: fmt.Errorf("%w: reset", ErrExternal)},
		okStep(),
	}}
	p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("idem.read"), ad)
	})
	req := allowedRequest("idem.read")
	req.CorrelationID = "call-abc"
	_ = p.Invoke(context.Background(), req)
	invs := ad.invocations()
	if len(invs) != 2 {
		t.Fatalf("expected two attempts, got %d", len(invs))
	}
	if invs[0].CallID != "call-abc" || invs[1].CallID != "call-abc" {
		t.Fatalf("the logical call id must be stable across attempts: %q then %q", invs[0].CallID, invs[1].CallID)
	}
	if invs[0].IdempotencyKey != invs[1].IdempotencyKey || invs[0].IdempotencyKey == "" {
		t.Fatalf("the idempotency key must be stable across attempts: %q then %q",
			invs[0].IdempotencyKey, invs[1].IdempotencyKey)
	}
	if invs[0].AttemptIndex != 0 || invs[1].AttemptIndex != 1 {
		t.Fatalf("attempt index must be 0-based and monotonic: %d then %d", invs[0].AttemptIndex, invs[1].AttemptIndex)
	}
}

func TestDistinctLogicalCallsGetDistinctKeys(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(mutationManifest("idem.two"), ad)
	})
	first := allowedRequest("idem.two")
	first.CorrelationID = "call-one"
	second := allowedRequest("idem.two")
	second.CorrelationID = "call-two"
	_ = p.Invoke(context.Background(), first)
	_ = p.Invoke(context.Background(), second)
	invs := ad.invocations()
	if invs[0].IdempotencyKey == invs[1].IdempotencyKey {
		t.Fatalf("two logical calls must never share an idempotency key")
	}
	if invs[0].IdempotencyKey != "call-one" || invs[1].IdempotencyKey != "call-two" {
		t.Fatalf("the idempotency key must derive from the call id: %+v", invs)
	}
}

func TestHTTPAdapterSendsIdempotencyKey(t *testing.T) {
	var seen string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()
	policy := loopbackPolicyFor(ts.URL)
	h := NewHTTPTool(policy)
	if !h.SupportsIdempotency() {
		t.Fatalf("the HTTP capability must advertise idempotency support")
	}
	_, err := h.Invoke(context.Background(), tool.Invocation{
		ToolID: "http.request", Operation: "post", Input: map[string]string{"url": ts.URL},
		IdempotencyKey: "call-xyz",
	})
	if err != nil {
		t.Fatalf("mutation invoke: %v", err)
	}
	if seen != "call-xyz" {
		t.Fatalf("the stable idempotency key must reach the remote system, got %q", seen)
	}
}

func TestHTTPReadDoesNotCarryIdempotencyKey(t *testing.T) {
	var seen string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()
	h := NewHTTPTool(loopbackPolicyFor(ts.URL))
	_, err := h.Invoke(context.Background(), tool.Invocation{
		ToolID: "http.request", Operation: "get", Input: map[string]string{"url": ts.URL},
		IdempotencyKey: "call-abc",
	})
	if err != nil {
		t.Fatalf("read invoke: %v", err)
	}
	if seen != "" {
		t.Fatalf("a read must not claim mutation idempotency, got %q", seen)
	}
}

// ---------- §10 lifecycle ----------

func TestDisabledCapabilityBlocksInvocation(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("life.disable"), ad)
	})
	if err := p.SetCapabilityState("life.disable", CapabilityDisabled); err != nil {
		t.Fatalf("disable transition: %v", err)
	}
	res := p.Invoke(context.Background(), allowedRequest("life.disable"))
	if ad.attempts() != 0 {
		t.Fatalf("a disabled capability must never reach its adapter, attempts=%d", ad.attempts())
	}
	if res.Status != tool.StatusDenied {
		t.Fatalf("a disabled capability must be denied, got %+v", res)
	}
	if !strings.Contains(res.Error, ErrCapabilityDisabled.Error()) {
		t.Fatalf("the refusal must carry the capability_disabled class, got %q", res.Error)
	}
	if !hasEvent(bus, event.EventTypeToolCapabilityDisabled) {
		t.Fatalf("a disabled refusal must be audited, events=%v", eventTypes(bus))
	}
	if hasEvent(bus, event.EventTypeToolInvocationStarted) {
		t.Fatalf("a disabled capability must short-circuit before dispatch, events=%v", eventTypes(bus))
	}
}

func TestDeprecatedCapabilityStillExecutes(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("life.deprecated"), ad)
	})
	if err := p.SetCapabilityState("life.deprecated", CapabilityDeprecated); err != nil {
		t.Fatalf("deprecate transition: %v", err)
	}
	res := p.Invoke(context.Background(), allowedRequest("life.deprecated"))
	if res.Status != tool.StatusSuccess || res.CapabilityState != string(CapabilityDeprecated) {
		t.Fatalf("a deprecated capability must still execute and report its state, got %+v", res)
	}
	if !hasEvent(bus, event.EventTypeToolCapabilityDeprecated) {
		t.Fatalf("deprecation must be visible in telemetry, events=%v", eventTypes(bus))
	}
}

func TestLifecycleTransitionsAreBounded(t *testing.T) {
	p, _, _ := newPlatform(t, nil)
	if err := p.SetCapabilityState("life.t", CapabilityDeprecated); err != nil {
		t.Fatalf("enabled -> deprecated must be allowed: %v", err)
	}
	if err := p.SetCapabilityState("life.t", CapabilityDeprecated); err == nil {
		t.Fatalf("deprecated -> deprecated must be rejected")
	}
	if err := p.SetCapabilityState("life.t", CapabilityDisabled); err != nil {
		t.Fatalf("deprecated -> disabled must be allowed: %v", err)
	}
	if err := p.SetCapabilityState("life.t", CapabilityDeprecated); err == nil {
		t.Fatalf("disabled -> deprecated must be rejected")
	}
	if err := p.SetCapabilityState("life.t", CapabilityEnabled); err != nil {
		t.Fatalf("disabled -> enabled must be allowed: %v", err)
	}
	if got := p.CapabilityState("life.t"); got != CapabilityEnabled {
		t.Fatalf("state after the cycle = %q, want enabled", got)
	}
	if got := p.CapabilityState("life.untouched"); got != CapabilityEnabled {
		t.Fatalf("an untouched capability must default to enabled, got %q", got)
	}
}

func TestDisableRacingDispatchNeverWidensAuthority(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{delay: 60 * time.Millisecond}}}
	p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("life.race"), ad)
	})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = p.Invoke(context.Background(), allowedRequest("life.race"))
	}()
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		_ = p.SetCapabilityState("life.race", CapabilityDisabled)
	}()
	wg.Wait()
	// Whichever side won, the outcome is one of the two legal states: either
	// the in-flight call completed, or it was refused before dispatch.
	if got := p.CapabilityState("life.race"); got != CapabilityDisabled {
		t.Fatalf("the disable must stick, state=%q", got)
	}
	res := p.Invoke(context.Background(), allowedRequest("life.race"))
	if res.Status != tool.StatusDenied || ad.attempts() != 1 {
		t.Fatalf("after the race only new invocations may be refused, got %+v attempts=%d", res, ad.attempts())
	}
}

// ---------- §8 discovery posture ----------

func TestReliabilityDiscoveryIsDerivedFromManifestAndAdapter(t *testing.T) {
	read := readManifest("rel.read")
	mutation := mutationManifest("rel.write")
	idempotent := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	plain := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(read, idempotent)
		_ = reg.Register(mutation, plain)
	})
	readRel := ReliabilityFor(read, p.CapabilityState(read.ID), p.Adapter(read.ID))
	if readRel.RetryPolicy != RetryPolicyBoundedRead || readRel.MaxAttempts != 2 {
		t.Fatalf("a read must advertise the bounded read retry policy, got %+v", readRel)
	}
	mutRel := ReliabilityFor(mutation, p.CapabilityState(mutation.ID), p.Adapter(mutation.ID))
	if mutRel.RetryPolicy != RetryPolicyNone || mutRel.MaxAttempts != 2 {
		t.Fatalf("a mutation without idempotency support must advertise no retry at all, got %+v", mutRel)
	}
	if mutRel.SupportsIdempotency {
		t.Fatalf("an adapter that does not advertise idempotency must not claim it")
	}
	if readRel.SupportsReconciliation || readRel.ReconciliationAvailable {
		t.Fatalf("no capability may advertise reconciliation without implementing it, got %+v", readRel)
	}
}

// ---------- §10 telemetry ----------

func TestAttemptTelemetryIsStructuredAndSecretFree(t *testing.T) {
	const secret = "s3cr3t-token-value-abcdefgh"
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{
		{err: fmt.Errorf("%w: dial failed with %s", ErrExternal, secret)},
		okStep(),
	}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("obs.attempt"), ad)
	})
	p.RegisterSecret(secret)
	res := p.Invoke(context.Background(), allowedRequest("obs.attempt"))
	if res.Status != tool.StatusSuccess {
		t.Fatalf("call should succeed on the retry: %+v", res)
	}
	// The bus is a priority heap, so frames are asserted as a multiset: the
	// contract fixes which frames exist and what each carries, not their order.
	starts := eventFields(bus, event.EventTypeToolAttemptStarted)
	if len(starts) != 2 {
		t.Fatalf("one attempt.started frame per physical attempt, got %d", len(starts))
	}
	seenAttempts := map[string]bool{}
	for _, f := range starts {
		if f["call_id"] != "corr-1" || f["max_attempts"] != "2" {
			t.Fatalf("attempt frame must carry the call id and budget, got %+v", f)
		}
		seenAttempts[f["attempt"]] = true
	}
	if !seenAttempts["1"] || !seenAttempts["2"] {
		t.Fatalf("both attempt indices must be framed, got %+v", starts)
	}
	completes := eventFields(bus, event.EventTypeToolAttemptCompleted)
	if len(completes) != 2 {
		t.Fatalf("one attempt.completed frame per physical attempt, got %d", len(completes))
	}
	outcomes := map[string]bool{}
	for _, f := range completes {
		outcomes[f["outcome"]] = true
		if f["outcome"] == "failed" && f["error_class"] != "external error" {
			t.Fatalf("a failed attempt must carry its error class, got %+v", f)
		}
	}
	if !outcomes["failed"] || !outcomes["completed"] {
		t.Fatalf("both per-attempt outcomes must be framed, got %+v", completes)
	}
	retry := eventFields(bus, event.EventTypeToolRetryScheduled)
	if len(retry) != 1 || retry[0]["error_class"] != "external error" || retry[0]["call_id"] != "corr-1" {
		t.Fatalf("the retry frame must be structured, got %+v", retry)
	}
	done := eventFields(bus, event.EventTypeToolInvocationCompleted)
	if len(done) != 1 || done[0]["outcome"] != "completed" || done[0]["attempts"] != "2" {
		t.Fatalf("the terminal frame must carry outcome and attempt count, got %+v", done)
	}
	for _, e := range recorderOf(bus).all() {
		if strings.Contains(string(e.Data), secret) {
			t.Fatalf("event %s leaked a secret: %s", e.Type, string(e.Data))
		}
	}
}

func TestUnknownOutcomeTelemetryStaysDistinguishable(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{{err: fmt.Errorf("%w: lost response", ErrUnknownOutcome)}}}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(mutationManifest("obs.unknown"), ad)
	})
	_ = p.Invoke(context.Background(), allowedRequest("obs.unknown"))
	frames := eventFields(bus, event.EventTypeToolInvocationUnknownOutcome)
	if len(frames) != 1 {
		t.Fatalf("one unknown terminal frame expected, got %d", len(frames))
	}
	if frames[0]["outcome"] != "unknown" || frames[0]["error_kind"] != "unknown outcome" {
		t.Fatalf("the unknown frame must stay distinguishable, got %+v", frames[0])
	}
	if frames[0]["retry_recommended"] != "false" {
		t.Fatalf("the unknown frame must state that no retry is recommended, got %+v", frames[0])
	}
}

// ---------- security regression matrix ----------

func TestReliabilityPathsCannotWidenAuthority(t *testing.T) {
	ad := &scriptedAdapter{ops: []string{"execute"}, plan: []step{okStep()}}
	p, _, _ := newPlatform(t, func(reg *tool.ToolRegistry) {
		_ = reg.Register(readManifest("sec.tool"), ad)
		_ = reg.Register(mutationManifest("sec.write"), ad)
	})
	// A disabled capability stays disabled for an unauthorized actor too: the
	// lifecycle gate is not an authorization surface.
	_ = p.SetCapabilityState("sec.tool", CapabilityDisabled)
	foreign := allowedRequest("sec.tool")
	foreign.ActorID = "actor-foreign"
	foreign.BusinessID = "biz-2"
	if res := p.Invoke(context.Background(), foreign); res.Status == tool.StatusSuccess {
		t.Fatalf("a foreign actor must still be denied, got %+v", res)
	}
	// An allowlisted agent cannot invoke a tool that is not on its allowlist,
	// regardless of retry posture.
	notAllowed := allowedRequest("sec.write")
	notAllowed.AgentTools = []string{"other.tool"}
	if res := p.Invoke(context.Background(), notAllowed); res.Status == tool.StatusSuccess {
		t.Fatalf("agent allowlist must still apply, got %+v", res)
	}
	// Lifecycle state never conjures a registration.
	if _, ok := p.Manifest("sec.unknown"); ok {
		t.Fatalf("an unknown capability must stay unregistered")
	}
	if got := p.CapabilityState("sec.unknown"); got != CapabilityEnabled {
		t.Fatalf("an unknown capability reports the default state, got %q", got)
	}
}

func TestClassifyAndRecommendAreConsistent(t *testing.T) {
	cases := []struct {
		err     error
		ctxErr  error
		outcome string
		retryOK bool
		class   tool.SideEffectClass
	}{
		{nil, nil, "completed", false, tool.SideEffectRead},
		{fmt.Errorf("%w: x", ErrExternal), nil, "failed", true, tool.SideEffectRead},
		{fmt.Errorf("%w: x", ErrExternal), nil, "failed", false, tool.SideEffectExternalMutation},
		{context.Canceled, nil, "cancelled", false, tool.SideEffectRead},
		{context.DeadlineExceeded, nil, "timed_out", true, tool.SideEffectRead},
		{fmt.Errorf("%w: x", ErrUnknownOutcome), nil, "unknown", false, tool.SideEffectRead},
		{fmt.Errorf("%w: x", ErrNotSent), nil, "failed", true, tool.SideEffectExternalMutation},
		{nil, context.Canceled, "cancelled", false, tool.SideEffectRead},
		{nil, context.DeadlineExceeded, "timed_out", false, tool.SideEffectRead},
	}
	for _, c := range cases {
		if got := classifyOutcome(c.err, c.ctxErr); got != c.outcome {
			t.Fatalf("classifyOutcome(%v,%v) = %q, want %q", c.err, c.ctxErr, got, c.outcome)
		}
		if got := RetryRecommended(c.err, c.class); got != c.retryOK {
			t.Fatalf("RetryRecommended(%v, %v) = %v, want %v", c.err, c.class, got, c.retryOK)
		}
	}
	if got := attemptBound(tool.SideEffectRead); got != 2 {
		t.Fatalf("read attempt bound = %d, want 2", got)
	}
	if got := attemptBound(tool.SideEffectExternalMutation); got != 2 {
		t.Fatalf("attempt bound is one retry for every class, got %d", got)
	}
}

func TestErrorClassTaxonomyIsStable(t *testing.T) {
	cases := map[error]string{
		ErrValidation:           "validation",
		ErrPermission:           "permission denied",
		ErrScope:                "scope denied",
		ErrCredential:           "credential unavailable",
		ErrCapabilityDisabled:   "capability_disabled",
		ErrNetworkBlocked:       "network blocked",
		ErrTimeout:              "timeout",
		ErrCancelled:            "cancelled",
		ErrResourceLimit:        "resource limit",
		ErrUnknownOutcome:       "unknown outcome",
		ErrExternal:             "external error",
		errors.New("who knows"): "internal adapter failure",
	}
	for err, want := range cases {
		if got := errorClass(err); got != want {
			t.Fatalf("errorClass(%v) = %q, want %q", err, got, want)
		}
	}
}

// operationClassAdapter narrows its tool-level class for one operation, the way
// http.request narrows `write` to `read` for GET/HEAD.
type operationClassAdapter struct {
	*scriptedAdapter
	narrowed tool.SideEffectClass
	op       string
}

func (o operationClassAdapter) OperationSideEffect(op string) (tool.SideEffectClass, bool) {
	if op == o.op {
		return o.narrowed, true
	}
	return "", false
}

func TestEffectiveClassNarrowsNeverWidens(t *testing.T) {
	m := mutationManifest("class.narrow")
	ad := operationClassAdapter{narrowed: tool.SideEffectRead, op: "read"}
	if got := EffectiveClass(m, ad, "read"); got != tool.SideEffectRead {
		t.Fatalf("a declared operation class must narrow, got %q", got)
	}
	if got := EffectiveClass(m, ad, "write"); got != m.SideEffectClass {
		t.Fatalf("an undeclared operation must keep the manifest class, got %q", got)
	}
	// An adapter may not widen the manifest class.
	widen := operationClassAdapter{narrowed: tool.SideEffectExternalMutation, op: "read"}
	readManifest := readManifest("class.widen")
	if got := EffectiveClass(readManifest, widen, "read"); got != tool.SideEffectRead {
		t.Fatalf("an adapter must never widen the manifest class, got %q", got)
	}
	// A capability that declares nothing keeps the manifest class.
	plain := &scriptedAdapter{ops: []string{"execute"}}
	if got := EffectiveClass(m, plain, "write"); got != m.SideEffectClass {
		t.Fatalf("the manifest class must remain the default, got %q", got)
	}
}

func TestReadOperationOfAWriteCapabilityRetries(t *testing.T) {
	m := mutationManifest("class.retry")
	m.Operations = []string{"read", "write"}
	base := &scriptedAdapter{ops: []string{"read", "write"}, plan: []step{
		{err: fmt.Errorf("%w: connection reset", ErrExternal)},
		okStep(),
	}}
	ad := operationClassAdapter{scriptedAdapter: base, narrowed: tool.SideEffectRead, op: "read"}
	p, _, bus := newPlatform(t, func(reg *tool.ToolRegistry) { _ = reg.Register(m, ad) })
	req := allowedRequest("class.retry")
	req.Operation = "read"
	res := p.Invoke(context.Background(), req)
	if res.Status != tool.StatusSuccess || res.Attempts != 2 {
		t.Fatalf("a declared read operation must take the read retry policy, got %+v", res)
	}
	if !hasEvent(bus, event.EventTypeToolRetryScheduled) {
		t.Fatalf("the retry must be framed, events=%v", eventTypes(bus))
	}
	// The same capability's mutating operation stays single-shot.
	mutating := allowedRequest("class.retry")
	mutating.Operation = "write"
	res = p.Invoke(context.Background(), mutating)
	if res.Attempts != 1 {
		t.Fatalf("a mutating operation must stay single-shot, got %+v", res)
	}
}
