package agentintel

// Observation normalization tests for Operational Reliability & Tool Semantics
// v1 (contracts/OPERATIONAL_RELIABILITY_CONTRACTS.md §8, §9): the intelligence
// layer must surface the platform's terminal outcome, attempt count and
// reconciliation requirement instead of collapsing every failure into one
// undifferentiated line — and it must never turn an unknown outcome into an
// automatic re-dispatch.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	"github.com/Nomssky/NEXUS/internal/memory"
)

// outcomeStubAdapter answers with one scripted error so the platform produces a
// real terminal outcome the loop has to normalize.
type outcomeStubAdapter struct {
	err      error
	attempts int
}

func (o *outcomeStubAdapter) Operations() []string { return []string{"execute", "commit"} }

func (o *outcomeStubAdapter) Invoke(context.Context, tool.Invocation) (tool.RawResult, error) {
	o.attempts++
	if o.err != nil {
		return tool.RawResult{}, o.err
	}
	return tool.RawResult{Result: map[string]string{"output": "committed"}}, nil
}

// newReliabilityFixture wires the real capability platform in front of the
// intelligence runtime: the loop then has exactly one mediated path.
func newReliabilityFixture(t *testing.T, adapter tool.Adapter, class tool.SideEffectClass) (*Runtime, *agentexec.Registry) {
	t.Helper()
	orgs := testOrg(t)
	tools := tool.NewToolRegistry()
	memberships := identity.NewMembershipSet()
	if err := memberships.Add(identity.Membership{
		IdentityID: "owner-1", BusinessID: "biz-1", Role: identity.RoleMember, Status: identity.StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	platform := capability.New(tools, memberships,
		capability.NewScopedCredentialResolver(security.NewDevResolver()), event.NewMemBus())
	manifest := tool.ToolManifest{
		ID: "ledger", Version: "1.0.0", Name: "ledger", Description: "writes to the ledger",
		Category:           tool.ToolCategoryWrite,
		InputSchema:        tool.Schema{Fields: []tool.SchemaField{{Name: "amount", Type: tool.FieldString, MaxLength: 32}}},
		OutputSchema:       tool.Schema{Fields: []tool.SchemaField{{Name: "output", Type: tool.FieldString, MaxLength: 256}}},
		SideEffectClass:    class,
		NetworkRequirement: tool.NetworkNone,
		ScopeRequirement:   tool.ScopeBusiness,
		SecurityClass:      tool.SecuritySandboxed,
		Operations:         []string{"commit", "execute"},
	}
	if err := tools.Register(manifest, adapter); err != nil {
		t.Fatal(err)
	}
	agents := agentexec.NewRegistry("nx:test:nexus", orgs, tools)
	exec := agentexec.NewRuntime(agents, tools,
		modelrouter.NewModelRouter(modelrouter.NewModelRegistry(), modelrouter.RoutingPolicyLocalFirst), event.NewMemBus())
	exec.Platform = platform
	if _, err := agents.Create(agentexec.Definition{
		ID: "bookkeeper", Name: "Bookkeeper", BusinessID: "biz-1", Capabilities: []string{"analysis"},
		AllowedTools: []string{"ledger"},
	}, "t"); err != nil {
		t.Fatal(err)
	}
	mem, err := OpenMemory(nil, Deps{Scopes: memory.NewMembershipScopes(memberships.AllowsScope)})
	if err != nil {
		t.Fatal(err)
	}
	// Admission is mandatory for mediated tool calls (fail closed), so the
	// fixture wires the shipped permissive policy.
	exec.Platform = platform
	ctl, _, _ := allowAllController()
	exec.Controller = ctl
	return New(agents, exec, &scriptedDecider{plan: goodPlan}, nil, mem), agents
}

func ledgerRun(t *testing.T, rt *Runtime) *run {
	t.Helper()
	req := workRequest(t, Objective{Description: "post an entry"}, "")
	return &run{rt: rt, req: req, execID: "exec-1", businessID: req.BusinessID, ag: nil}
}

func TestObservationMarksCompletedToolCall(t *testing.T) {
	adapter := &outcomeStubAdapter{}
	rt, _ := newReliabilityFixture(t, adapter, tool.SideEffectRead)
	rn := ledgerRun(t, rt)
	obs, usage, state, _ := rt.perform(rn, context.Background(), Objective{Description: "post"},
		Action{Type: ActionToolCall, Tool: "ledger", Operation: "commit", Input: map[string]string{"amount": "10"}},
		"bookkeeper", nil, BudgetUsage{}, Budget{})
	if state != "" {
		t.Fatalf("a completed tool call must not end the loop, state=%q", state)
	}
	if usage.ToolCalls != 1 {
		t.Fatalf("tool call counter = %d, want 1", usage.ToolCalls)
	}
	if obs.Outcome != "completed" || obs.Attempts != 1 {
		t.Fatalf("observation must normalize a completed call, got %+v", obs)
	}
	if obs.ReconciliationRequired {
		t.Fatalf("a completed call never requires reconciliation: %+v", obs)
	}
}

func TestObservationSurfacesUnknownOutcomeWithoutRetry(t *testing.T) {
	adapter := &outcomeStubAdapter{err: fmt.Errorf("%w: response lost after dispatch", capability.ErrUnknownOutcome)}
	rt, _ := newReliabilityFixture(t, adapter, tool.SideEffectExternalMutation)
	rn := ledgerRun(t, rt)
	obs, _, state, msg := rt.perform(rn, context.Background(), Objective{Description: "post"},
		Action{Type: ActionToolCall, Tool: "ledger", Operation: "commit", Input: map[string]string{"amount": "10"}},
		"bookkeeper", nil, BudgetUsage{}, Budget{})
	if state != StateFailed {
		t.Fatalf("an indeterminate mutation must not be treated as success: %q", state)
	}
	if obs.Outcome != "unknown" {
		t.Fatalf("the observation must keep the unknown outcome distinct, got %q", obs.Outcome)
	}
	if obs.ReconciliationRequired != true {
		t.Fatalf("an unknown outcome must surface a reconciliation requirement: %+v", obs)
	}
	if obs.RetryRecommended {
		t.Fatalf("an unknown outcome must never recommend a retry: %+v", obs)
	}
	if !strings.Contains(msg, capability.ErrUnknownOutcome.Error()) {
		t.Fatalf("the loop message must name the unknown outcome, got %q", msg)
	}
	if adapter.attempts != 1 {
		t.Fatalf("the intelligence layer must never re-dispatch an unknown mutation, attempts=%d", adapter.attempts)
	}
}

func TestObservationSurfacesTimeoutDistinctly(t *testing.T) {
	adapter := &outcomeStubAdapter{err: fmt.Errorf("%w: deadline", capability.ErrTimeout)}
	rt, _ := newReliabilityFixture(t, adapter, tool.SideEffectRead)
	rn := ledgerRun(t, rt)
	obs, _, _, _ := rt.perform(rn, context.Background(), Objective{Description: "read"},
		Action{Type: ActionToolCall, Tool: "ledger", Operation: "execute", Input: map[string]string{"amount": "10"}},
		"bookkeeper", nil, BudgetUsage{}, Budget{})
	if obs.Outcome != "timed_out" {
		t.Fatalf("a deadline expiry must not be reported as a plain failure, got %q", obs.Outcome)
	}
	if obs.ReconciliationRequired {
		t.Fatalf("a timeout is not an unknown outcome: %+v", obs)
	}
}

func TestObservationCarriesRetryPostureForRead(t *testing.T) {
	adapter := &outcomeStubAdapter{err: fmt.Errorf("%w: connection reset", capability.ErrExternal)}
	rt, _ := newReliabilityFixture(t, adapter, tool.SideEffectRead)
	rn := ledgerRun(t, rt)
	obs, _, _, _ := rt.perform(rn, context.Background(), Objective{Description: "read"},
		Action{Type: ActionToolCall, Tool: "ledger", Operation: "execute", Input: map[string]string{"amount": "10"}},
		"bookkeeper", nil, BudgetUsage{}, Budget{})
	if obs.Outcome != "failed" {
		t.Fatalf("a classified external failure is failed, got %q", obs.Outcome)
	}
	if obs.Attempts != 2 {
		t.Fatalf("the observation must report the attempt count actually made, got %d", obs.Attempts)
	}
	if obs.RetryRecommended {
		t.Fatalf("a spent attempt budget must not recommend another retry: %+v", obs)
	}
}

// TestUnmediatedToolErrorsStayTerminal proves the reliability wiring did not
// weaken the existing rule: a plain (non-platform) failure still fails the
// objective without inventing an outcome for it.
func TestUnmediatedToolErrorsStayTerminal(t *testing.T) {
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan:    goodPlan,
		answers: []string{`{"type":"tool_call","tool":"calculator","input":{"a":"1","b":"0","op":"div"}}`},
	})
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "divide by zero"}, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "tool failed") {
		t.Fatalf("tool failure must fail the objective: %s / %s", out.Status, out.Error)
	}
}

func TestOutcomeErrorUnwrapsToCause(t *testing.T) {
	cause := errors.New("boom")
	err := &agentexec.OutcomeError{Outcome: "unknown", Attempts: 1, Cause: cause}
	if !errors.Is(err, cause) {
		t.Fatalf("OutcomeError must unwrap to its cause")
	}
	if err.Error() != "boom" {
		t.Fatalf("OutcomeError text must be the cause text, got %q", err.Error())
	}
	var typed *agentexec.OutcomeError
	if !errors.As(error(err), &typed) || typed.Outcome != "unknown" {
		t.Fatalf("OutcomeError must be recoverable with errors.As")
	}
}

// ---- pre-send failure vs post-send uncertainty (contract §2, §3) ----------
//
// The two are the sharpest distinction the reliability model makes, and they
// differ in exactly the way recovery depends on:
//
//   - NOT SENT   — provably nothing left this process. The operation did not
//                  take effect, so re-issuing it cannot duplicate anything.
//   - UNKNOWN    — the request may have reached the remote system and the
//                  response was lost. Re-issuing may duplicate a real effect.
//
// At the capability layer only `ErrNotSent` admits a mutation retry
// (`RetryAdmitted`), and only when the adapter supports idempotency. These
// tests prove the loop-level consequence, which is what an agent's evidence
// actually says.

// notSentThenOKAdapter fails its FIRST dispatch as provably not sent, then
// succeeds. It advertises idempotency support, which is the second half of the
// mutation-retry gate.
type notSentThenOKAdapter struct {
	attempts int
	invs     []tool.Invocation
}

func (a *notSentThenOKAdapter) Operations() []string { return []string{"execute", "commit"} }

func (a *notSentThenOKAdapter) SupportsIdempotency() bool { return true }

func (a *notSentThenOKAdapter) Invoke(_ context.Context, inv tool.Invocation) (tool.RawResult, error) {
	a.attempts++
	a.invs = append(a.invs, inv)
	if a.attempts == 1 {
		return tool.RawResult{}, fmt.Errorf("%w: dial tcp: connect: connection refused", capability.ErrNotSent)
	}
	return tool.RawResult{Result: map[string]string{"output": "committed"}}, nil
}

// TestNotSentMutationIsRetriedAndUnknownIsNot is the pre-send / post-send
// contrast, driven through the real loop against a real capability platform.
//
// Both cases use the SAME objective and the SAME write-class tool; only the
// adapter's first-dispatch failure differs. The loop's verdict must differ with
// it.
func TestNotSentMutationIsRetriedAndUnknownIsNot(t *testing.T) {
	notSent := &notSentThenOKAdapter{}
	rt, _ := newReliabilityFixture(t, notSent, tool.SideEffectExternalMutation)
	rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"tool_call","tool":"ledger","operation":"commit","input":{"amount":"10"}}`,
		`{"type":"complete","result":"committed"}`,
	}}
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{
		Description: "commit once",
		// The retry really happened, so a completed effect now exists.
		SuccessCriteria: []string{"effects_completed>=1"},
	}, ""), nil)

	// Provably-not-sent: nothing was committed remotely, so re-issuing is safe.
	if notSent.attempts != 2 {
		t.Fatalf("a provably not-sent mutation must be re-dispatched once, got %d attempts", notSent.attempts)
	}
	if out.Status != "completed" {
		t.Fatalf("the retried mutation must let the objective complete: %s (%s)", out.Status, out.Error)
	}
	// Both physical attempts belonged to ONE logical call, so they presented
	// one idempotency token — the retry cannot duplicate the first attempt.
	if len(notSent.invs) != 2 {
		t.Fatalf("expected two physical attempts, got %d", len(notSent.invs))
	}
	if notSent.invs[0].IdempotencyKey != notSent.invs[1].IdempotencyKey ||
		notSent.invs[0].IdempotencyKey == "" {
		t.Fatalf("a retry must reuse the logical call's idempotency key: %q then %q",
			notSent.invs[0].IdempotencyKey, notSent.invs[1].IdempotencyKey)
	}

	// The same tool and objective, but the first dispatch MIGHT have taken
	// effect: the loop must not re-issue it.
	unknown := &outcomeStubAdapter{err: fmt.Errorf("%w: response lost after dispatch", capability.ErrUnknownOutcome)}
	rt2, _ := newReliabilityFixture(t, unknown, tool.SideEffectExternalMutation)
	rt2.Decider = &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"tool_call","tool":"ledger","operation":"commit","input":{"amount":"10"}}`,
	}}
	out2, _ := rt2.Run(context.Background(), workRequest(t, Objective{Description: "commit once"}, ""), nil)

	if unknown.attempts != 1 {
		t.Fatalf("an unknown outcome must never be re-dispatched, got %d attempts", unknown.attempts)
	}
	if out2.Status != "failed" || !strings.Contains(out2.Error, "unknown") {
		t.Fatalf("an unknown outcome must stay unknown and terminal: %s (%s)", out2.Status, out2.Error)
	}
}

// TestNotSentAndUnknownCarryDifferentRecoveryPosture pins the evidence the
// observation itself carries, which is what any later recovery decision reads.
// A provably-not-sent failure is a definite non-effect and is surfaced as such;
// an unknown outcome is never a retry recommendation and instead demands
// reconciliation.
func TestNotSentAndUnknownCarryDifferentRecoveryPosture(t *testing.T) {
	notSent := &notSentThenOKAdapter{}
	rt, _ := newReliabilityFixture(t, notSent, tool.SideEffectRead)
	rn := ledgerRun(t, rt)
	obs, _, state, _ := rt.perform(rn, context.Background(), Objective{Description: "read"},
		Action{Type: ActionToolCall, Tool: "ledger", Operation: "execute", Input: map[string]string{"amount": "10"}},
		"bookkeeper", nil, BudgetUsage{}, Budget{})
	if state != "" && state != StateFailed {
		t.Fatalf("unexpected terminal state %s", state)
	}
	// The read retried and succeeded, so it is a completed outcome and never a
	// reconciliation case.
	if obs.Outcome != "completed" {
		t.Fatalf("a retried not-sent read must complete, got outcome %q", obs.Outcome)
	}
	if obs.ReconciliationRequired {
		t.Fatal("a completed outcome must never demand reconciliation")
	}

	unknown := &outcomeStubAdapter{err: fmt.Errorf("%w: response lost after dispatch", capability.ErrUnknownOutcome)}
	rt2, _ := newReliabilityFixture(t, unknown, tool.SideEffectExternalMutation)
	rn2 := ledgerRun(t, rt2)
	obs2, _, state2, _ := rt2.perform(rn2, context.Background(), Objective{Description: "commit"},
		Action{Type: ActionToolCall, Tool: "ledger", Operation: "commit", Input: map[string]string{"amount": "10"}},
		"bookkeeper", nil, BudgetUsage{}, Budget{})
	if state2 != StateFailed {
		t.Fatalf("an unknown outcome must be terminal, got %s", state2)
	}
	if obs2.Outcome != "unknown" {
		t.Fatalf("the outcome must stay unknown, got %q", obs2.Outcome)
	}
	if obs2.RetryRecommended {
		t.Fatal("an unknown outcome must never carry a retry recommendation")
	}
	if !obs2.ReconciliationRequired {
		t.Fatal("an unknown outcome must surface its reconciliation requirement")
	}
}

var _ = executor.Outcome{}
