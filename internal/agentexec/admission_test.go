package agentexec

// Capability-boundary tests for governance admission
// (contracts/AGENT_GOVERNANCE_CONTROL_CONTRACTS.md §6, §7):
// a refused proposal never reaches the adapter, an allowed one does, and a
// constrained one runs with the constrained request.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// countingAdapter records that it ran, and how long it was allowed to run.
type countingAdapter struct {
	mu        sync.Mutex
	calls     int
	durations []time.Duration
	ops       []string
}

func (c *countingAdapter) Operations() []string { return []string{"execute"} }

func (c *countingAdapter) Invoke(ctx context.Context, inv tool.Invocation) (tool.RawResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.ops = append(c.ops, inv.Operation)
	deadline, ok := ctx.Deadline()
	if ok {
		c.durations = append(c.durations, time.Until(deadline))
	}
	return tool.RawResult{Result: map[string]string{"output": "ran"}}, nil
}

func (c *countingAdapter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

type admissionHost struct {
	mu        sync.Mutex
	approved  map[string]bool
	escalated int
	pending   map[string]string
}

func (h *admissionHost) RequestApproval(p control.Proposal, _ governance.Decision) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := "apr-" + p.ProposalID
	h.pending[p.Fingerprint()] = id
	return id, nil
}

func (h *admissionHost) ApprovedState(_ control.Proposal, fp string) (governance.ApprovalState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.approved[fp] {
		return governance.ApprovalStateApproved, true
	}
	return governance.ApprovalStatePending, true
}

func (h *admissionHost) Escalate(p control.Proposal, _ string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.escalated++
	return "esc-" + p.ProposalID, nil
}

func testPolicy(id string, effect governance.Outcome, action string, resourceIDs []string,
	constraints []governance.Constraint, precedence int) *governance.Policy {
	now := time.Now().UTC()
	return &governance.Policy{
		SchemaVersion: "1.0.0", EntityType: "policy", PolicyID: id, PolicyVersion: "1",
		PolicyType: governance.PolicyTypeAccessControl, Name: id, Description: id,
		Status:   governance.PolicyStatusActive,
		Subject:  governance.Subject{SubjectType: "all"},
		Action:   governance.Action{ActionType: "custom", ActionIDs: []string{action}},
		Resource: governance.Resource{ResourceType: "capability", ResourceIDs: resourceIDs},
		Effect:   effect, Precedence: precedence, Constraints: constraints,
		ApprovalConfig: &governance.ApprovalConfig{ApproverType: "human", TimeoutSeconds: 60,
			SelfApprovalProhibited: true},
		EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
	}
}

func allowPolicy() *governance.Policy {
	now := time.Now().UTC()
	return &governance.Policy{
		SchemaVersion: "1.0.0", EntityType: "policy",
		PolicyID: governance.DefaultAllowPolicyID, PolicyVersion: "1",
		PolicyType: governance.PolicyTypeAccessControl, Name: "allow", Description: "allow",
		Status:        governance.PolicyStatusActive,
		Subject:       governance.Subject{SubjectType: "all"},
		Action:        governance.Action{ActionType: "custom"},
		Resource:      governance.Resource{ResourceType: "all"},
		Effect:        governance.ALLOW,
		EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
	}
}

type governedRuntime struct {
	rt      *Runtime
	adapter *countingAdapter
	host    *admissionHost
}

func newGovernedRuntime(t *testing.T, policies ...*governance.Policy) *governedRuntime {
	t.Helper()
	members := identity.NewMembershipSet()
	if err := members.Add(identity.Membership{IdentityID: "actor-1", BusinessID: "biz-1",
		Role: identity.RoleMember, Status: identity.StatusActive}); err != nil {
		t.Fatal(err)
	}
	tools := tool.NewToolRegistry()
	adapter := &countingAdapter{}
	platform := capability.New(tools, members,
		capability.NewScopedCredentialResolver(security.NewDevResolver()), event.NewMemBus())
	manifest := tool.ToolManifest{
		ID: "echo.tool", Version: "1.0.0", Name: "echo.tool", Description: "test",
		Category:        tool.ToolCategoryRead,
		InputSchema:     tool.Schema{Fields: []tool.SchemaField{{Name: "text", Type: tool.FieldString, MaxLength: 32}}},
		OutputSchema:    tool.Schema{Fields: []tool.SchemaField{{Name: "output", Type: tool.FieldString, MaxLength: 64}}},
		SideEffectClass: tool.SideEffectRead, NetworkRequirement: tool.NetworkNone,
		ScopeRequirement: tool.ScopeBusiness, SecurityClass: tool.SecuritySandboxed,
		Operations:     []string{"execute"},
		ResourceLimits: tool.ResourceLimits{MaxDuration: 10 * time.Second},
	}
	if err := tools.Register(manifest, adapter); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry("nx:test", testOrg(t), tools)
	if _, err := reg.Create(Definition{ID: "agent-1", Name: "a", BusinessID: "biz-1",
		Capabilities: []string{"analysis"}, AllowedTools: []string{"echo.tool"}}, "t"); err != nil {
		t.Fatal(err)
	}
	host := &admissionHost{approved: map[string]bool{}, pending: map[string]string{}}
	rt := &Runtime{
		Registry: reg, Tools: tools, ToolExec: BuiltinExecutor(),
		Platform:   platform,
		Controller: control.New(control.Options{Engine: governance.NewEngine(policies), Approver: host, Escalator: host}),
		now:        time.Now, maxDepth: 3, maxFan: 4,
	}
	return &governedRuntime{rt: rt, adapter: adapter, host: host}
}

func scope() ToolScope {
	return ToolScope{ActorID: "actor-1", BusinessID: "biz-1", AgentTools: []string{"echo.tool"},
		CorrelationID: "exec-1"}
}

func call() ToolCallSpec {
	return ToolCallSpec{ToolID: "echo.tool", Operation: "execute", Input: map[string]string{"text": "hi"}}
}

func TestAllowedActionReachesTheAdapter(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	out, err := g.rt.InvokeToolScoped(context.Background(), scope(), "agent-1", call())
	if err != nil {
		t.Fatalf("an allowed action must execute: %v", err)
	}
	if out["output"] != "ran" || g.adapter.count() != 1 {
		t.Fatalf("the adapter must have run exactly once: %+v", out)
	}
}

func TestDeniedActionNeverReachesTheAdapter(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy(),
		testPolicy("p-deny", governance.DENY, control.ActionToolCall, nil, nil, 1000))
	_, err := g.rt.InvokeToolScoped(context.Background(), scope(), "agent-1", call())
	if err == nil {
		t.Fatal("a denied action must not execute")
	}
	var ae *AdmissionError
	if !errors.As(err, &ae) || ae.Outcome != governance.DENY {
		t.Fatalf("a denial must be distinguishable: %v", err)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a denied action must never reach the adapter, calls=%d", g.adapter.count())
	}
}

func TestPendingApprovalNeverReachesTheAdapter(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy(),
		testPolicy("p-appr", governance.REQUIRE_APPROVAL, control.ActionToolCall, nil, nil, 1000))
	_, err := g.rt.InvokeToolScoped(context.Background(), scope(), "agent-1", call())
	var ae *AdmissionError
	if !errors.As(err, &ae) || ae.Outcome != governance.REQUIRE_APPROVAL || ae.ApprovalID == "" {
		t.Fatalf("the action must wait for approval with an id: %v", err)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("approval-pending must not invoke the adapter, calls=%d", g.adapter.count())
	}
}

func TestEscalationNeverReachesTheAdapter(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy(),
		testPolicy("p-esc", governance.ESCALATE, control.ActionToolCall, nil, nil, 1000))
	_, err := g.rt.InvokeToolScoped(context.Background(), scope(), "agent-1", call())
	var ae *AdmissionError
	if !errors.As(err, &ae) || ae.Outcome != governance.ESCALATE || ae.EscalationID == "" {
		t.Fatalf("the action must escalate: %v", err)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("an escalated action must not invoke the adapter, calls=%d", g.adapter.count())
	}
	if g.host.escalated != 1 {
		t.Fatalf("exactly one escalation expected, got %d", g.host.escalated)
	}
}

func TestAllowedWithConstraintsRunsTheConstrainedRequest(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy(),
		testPolicy("p-constraints", governance.ALLOW_WITH_CONSTRAINTS, control.ActionToolCall, nil,
			[]governance.Constraint{{ConstraintID: "c-duration", ConstraintType: control.ConstraintMaxDuration,
				Expression: "1500", Severity: "mandatory"}}, 1000))
	if _, err := g.rt.InvokeToolScoped(context.Background(), scope(), "agent-1", call()); err != nil {
		t.Fatalf("an allowed-with-constraints action must execute: %v", err)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("the adapter must run once, got %d", g.adapter.count())
	}
	// The adapter's context must reflect the tightened bound, not the manifest or
	// platform cap. (A read divides its call budget across its two attempts, so
	// the per-attempt context is half of the 1.5s call budget.)
	g.adapter.mu.Lock()
	got := g.adapter.durations[0]
	g.adapter.mu.Unlock()
	if got > 1500*time.Millisecond {
		t.Fatalf("the constrained call must not outrun its restriction, got %v", got)
	}
	if got < 500*time.Millisecond {
		t.Fatalf("the constrained budget should still be usable, got %v", got)
	}
}

func TestUnenforceableConstraintNeverReachesTheAdapter(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy(),
		testPolicy("p-bad", governance.ALLOW_WITH_CONSTRAINTS, control.ActionToolCall, nil,
			[]governance.Constraint{{ConstraintID: "c-bad", ConstraintType: "unknown_constraint",
				Expression: "x", Severity: "mandatory"}}, 1000))
	_, err := g.rt.InvokeToolScoped(context.Background(), scope(), "agent-1", call())
	if !errors.Is(err, control.ErrConstraintUnenforceable) {
		t.Fatalf("an unenforceable constraint must fail closed, got %v", err)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a failed constraint must not invoke the adapter, calls=%d", g.adapter.count())
	}
}

func TestToolAllowlistConstraintBlocksOtherTools(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy(),
		testPolicy("p-allowlist", governance.ALLOW_WITH_CONSTRAINTS, control.ActionToolCall, nil,
			[]governance.Constraint{{ConstraintID: "c-tools", ConstraintType: control.ConstraintToolAllowlist,
				Expression: "something.else", Severity: "mandatory"}}, 1000))
	_, err := g.rt.InvokeToolScoped(context.Background(), scope(), "agent-1", call())
	if !errors.Is(err, control.ErrConstraintUnenforceable) {
		t.Fatalf("a tool outside the constrained set must fail closed, got %v", err)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("no adapter invocation may happen, calls=%d", g.adapter.count())
	}
}

func TestWithoutControllerToolCallsFailClosed(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	g.rt.Controller = nil // governance unavailable
	_, err := g.rt.InvokeToolScoped(context.Background(), scope(), "agent-1", call())
	var ae *AdmissionError
	if !errors.As(err, &ae) || ae.Outcome != governance.DENY {
		t.Fatalf("a missing controller must fail closed, got %v", err)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("governance unavailable must not invoke the adapter, calls=%d", g.adapter.count())
	}
}

func TestAllowlistStillAppliesAfterAdmission(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	badScope := scope()
	badScope.AgentTools = []string{"some.other.tool"}
	if _, err := g.rt.InvokeToolScoped(context.Background(), badScope, "agent-1", call()); err == nil {
		t.Fatal("the agent allowlist must still reject after an ALLOW decision")
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a rejected invocation must not reach the adapter, calls=%d", g.adapter.count())
	}
}

func TestAdmissionErrorMessageCarriesNoSecrets(t *testing.T) {
	g := newGovernedRuntime(t, testPolicy("p-deny", governance.DENY, control.ActionToolCall, nil, nil, 1000))
	_, err := g.rt.InvokeToolScoped(context.Background(), scope(), "agent-1", call())
	if err == nil {
		t.Fatal("expected a denial")
	}
	msg := err.Error()
	for _, forbidden := range []string{"Bearer ", "sk-", "password"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("the admission error leaked %q: %s", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "echo.tool") {
		t.Fatalf("the denial must identify the refused resource: %s", msg)
	}
}

func TestApprovalResumeAdmitsTheIdenticalActionOnce(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy(),
		testPolicy("p-appr", governance.REQUIRE_APPROVAL, control.ActionToolCall, nil, nil, 1000))
	s := scope()
	c := call()

	if _, err := g.rt.InvokeToolScoped(context.Background(), s, "agent-1", c); err == nil {
		t.Fatal("the first attempt must wait for approval")
	}
	// Approve exactly this proposal.
	proposal := control.Proposal{
		ProposalID: "any", CorrelationID: s.CorrelationID, ExecutionID: s.CorrelationID,
		ActorID: s.ActorID, AgentID: "agent-1", BusinessID: s.BusinessID, DivisionID: s.DivisionID,
		Action: control.ActionToolCall, Resource: c.ToolID, ResourceType: control.ResourceTypeCapability,
		ToolID: c.ToolID, Operation: c.Operation,
	}
	g.host.mu.Lock()
	g.host.approved[proposal.Fingerprint()] = true
	g.host.mu.Unlock()

	if _, err := g.rt.InvokeToolScoped(context.Background(), s, "agent-1", c); err != nil {
		t.Fatalf("the approved action must execute: %v", err)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("the approved action must have run once, got %d", g.adapter.count())
	}
}
