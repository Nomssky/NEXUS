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
	mem, err := OpenMemory(nil)
	if err != nil {
		t.Fatal(err)
	}
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

var _ = executor.Outcome{}
