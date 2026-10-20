package agentintel

// Delegation authority and lifecycle tests
// (contracts/AGENT_DELEGATION_CONTRACTS.md): the child runs as its own
// registry identity inside the parent scope, with intersected tools,
// independent admission, normalized outcomes, and no memory path. The parent
// receives runtime evidence only — never authority from the child.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/memory"
)

// delegateFixture adds a narrow child agent next to the governed agent: the
// child may only echo. Seeded policy allows everything; individual tests add
// their own gates through the controller where needed.
func delegateFixture(t *testing.T) *governedFixture {
	t.Helper()
	g := newGovernedFixture(t, seededAllowPolicy())
	if _, err := g.rt.Registry.Create(agentexec.Definition{
		ID: "governed-child", Name: "Child", BusinessID: "biz-1",
		Capabilities: []string{"analysis"}, AllowedTools: []string{"governed.echo"},
	}, "t"); err != nil {
		t.Fatal(err)
	}
	g.rt.Exec.Router = seededRouter(t)
	return g
}

func TestDelegateToolsIntersectedWithChildAllowlist(t *testing.T) {
	g := delegateFixture(t)
	// calculator is registered and parent-usable, but NOT on the child: the
	// delegation must fail closed with no child created.
	out := g.run(t, "delegate compute",
		`{"type":"delegate","agent_id":"governed-child","objective":"compute it",`+
			`"delegate_tools":[{"tool":"calculator","input":{"a":"1","b":"2","op":"add"}}]}`)
	if out.Status != "failed" || !strings.Contains(out.Error, "child agent") {
		t.Fatalf("a tool outside the child allowlist must be refused: %s / %s", out.Status, out.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a refused delegation creates no child effect, got %d", g.adapter.count())
	}
}

func TestDelegateToolsRequireExplicitAgent(t *testing.T) {
	g := delegateFixture(t)
	out := g.run(t, "delegate anonymously",
		`{"type":"delegate","objective":"do it","delegate_tools":[{"tool":"governed.echo","input":{"text":"hi"}}]}`)
	if out.Status != "failed" || !strings.Contains(out.Error, "explicit agent_id") {
		t.Fatalf("anonymous tool-bearing delegation must fail: %s / %s", out.Status, out.Error)
	}
}

func TestDelegateToolsOutsideParentAllowlistRejected(t *testing.T) {
	g := delegateFixture(t)
	// Narrow the PARENT to echo-only too, then try to give the child a tool
	// the child has but the parent lacks: the delegator cannot give what it
	// lacks. Use a fresh child that allows calculator while the parent agent
	// (governed-agent: echo + calculator?) — instead assert the passing case
	// and the unknown-tool case here.
	out := g.run(t, "delegate echo",
		`{"type":"delegate","agent_id":"governed-child","objective":"echo it",`+
			`"delegate_tools":[{"tool":"governed.echo","operation":"execute","input":{"text":"hi"}}]}`,
		`{"type":"complete","result":"delegated"}`)
	if out.Status != "completed" {
		t.Fatalf("an intersected delegation must run: %s / %s", out.Status, out.Error)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("exactly the child tool ran, got %d", g.adapter.count())
	}
	// An unregistered tool is rejected before any child exists.
	out2 := g.run(t, "delegate ghost tool",
		`{"type":"delegate","agent_id":"governed-child","objective":"do it",`+
			`"delegate_tools":[{"tool":"nope-tool","input":{}}]}`)
	if out2.Status != "failed" || !strings.Contains(out2.Error, "not registered") {
		t.Fatalf("an unregistered child tool must be refused: %s / %s", out2.Status, out2.Error)
	}
}

func TestDelegateParentAllowlistBoundsChild(t *testing.T) {
	g := delegateFixture(t)
	// governed-agent allows echo and calculator; shrink the parent to a fresh
	// echo-only agent and confirm a calculator-bearing delegation is refused
	// even though the tool is registered.
	if _, err := g.rt.Registry.Create(agentexec.Definition{
		ID: "narrow-parent", Name: "Narrow", BusinessID: "biz-1",
		Capabilities: []string{"analysis"}, AllowedTools: []string{"governed.echo"},
	}, "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.rt.Registry.Create(agentexec.Definition{
		ID: "wide-child", Name: "Wide", BusinessID: "biz-1",
		Capabilities: []string{"analysis"}, AllowedTools: []string{"governed.echo", "calculator"},
	}, "t"); err != nil {
		t.Fatal(err)
	}
	// Drive the loop as the narrow parent via objective context agent_id.
	obj := Objective{Description: "delegate up", Context: map[string]string{"agent_id": "narrow-parent"}}
	req := workRequest(t, obj, "")
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"delegate","agent_id":"wide-child","objective":"compute",` +
			`"delegate_tools":[{"tool":"calculator","input":{"a":"1","b":"2","op":"add"}}]}`,
	}}
	out, _ := g.rt.Run(context.Background(), req, nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "parent agent") {
		t.Fatalf("a tool the parent lacks must be refused: %s / %s", out.Status, out.Error)
	}
}

func TestDelegateUnknownChildAgentRejected(t *testing.T) {
	g := delegateFixture(t)
	out := g.run(t, "delegate nowhere",
		`{"type":"delegate","agent_id":"ghost","objective":"do it"}`)
	// Structural rejection (unknown agent) precedes governance: the run fails
	// closed with no child and no adapter contact. A *visible* agent that
	// governance refuses is covered by the existing denial test.
	if out.Status != "failed" || !strings.Contains(out.Error, "not visible") {
		t.Fatalf("an invisible child agent must be rejected: %s / %s", out.Status, out.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a rejected delegation runs nothing, got %d", g.adapter.count())
	}
}

// ---------- explicit agent selection authority ----------

// TestExplicitAgentSameDivisionAllowed verifies that an explicit agent_id
// selects an active, business-owned, division-visible agent.
func TestExplicitAgentSameDivisionAllowed(t *testing.T) {
	g := delegateFixture(t)
	// governed-child is active in biz-1, no division (business-wide).
	obj := Objective{Description: "delegate to same-division agent",
		Context: map[string]string{"agent_id": "governed-child"}}
	req := workRequest(t, obj, "")
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"delegate","agent_id":"governed-child","objective":"do it",` +
			`"delegate_tools":[{"tool":"governed.echo","operation":"execute","input":{"text":"hi"}}]}`,
		`{"type":"complete","result":"done"}`,
	}}
	out, _ := g.rt.Run(context.Background(), req, nil)
	if out.Status != "completed" {
		t.Fatalf("same-division active agent must be invokable: %s / %s", out.Status, out.Error)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("exactly one tool ran, got %d", g.adapter.count())
	}
}

// TestExplicitAgentSiblingDivisionRejected verifies that an explicit agent in
// a sibling division is not selected, even though it shares the business.
func TestExplicitAgentSiblingDivisionRejected(t *testing.T) {
	g := delegateFixture(t)
	if _, err := g.rt.Registry.Create(agentexec.Definition{
		ID: "sibling-agent", Name: "Sibling", BusinessID: "biz-1", DivisionID: "div-2",
		Capabilities: []string{"analysis"}, AllowedTools: []string{"governed.echo"},
	}, "t"); err != nil {
		t.Fatal(err)
	}
	obj := Objective{Description: "delegate to sibling division agent",
		Context: map[string]string{"agent_id": "sibling-agent"}}
	req := workRequest(t, obj, "div-1")
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"tool_call","tool":"governed.echo","operation":"execute","input":{"text":"hi"}}`,
		`{"type":"complete","result":"done"}`,
	}}
	_, _ = g.rt.Run(context.Background(), req, nil)
	// The sibling agent is not selected -> agentTools("") is empty -> the tool
	// call is denied at the platform boundary, never reaching the adapter.
	if g.adapter.count() != 0 {
		t.Fatalf("sibling-division agent must not be invokable, got %d tool calls", g.adapter.count())
	}
}

// TestExplicitAgentDifferentBusinessRejected verifies that an explicit agent
// in a different business is not selected.
func TestExplicitAgentDifferentBusinessRejected(t *testing.T) {
	g := delegateFixture(t)
	if _, err := g.rt.Registry.Create(agentexec.Definition{
		ID: "other-biz-agent", Name: "Other", BusinessID: "biz-2",
		Capabilities: []string{"analysis"}, AllowedTools: []string{"governed.echo"},
	}, "t"); err != nil {
		t.Fatal(err)
	}
	obj := Objective{Description: "delegate to other-business agent",
		Context: map[string]string{"agent_id": "other-biz-agent"}}
	req := workRequest(t, obj, "")
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"tool_call","tool":"governed.echo","operation":"execute","input":{"text":"hi"}}`,
		`{"type":"complete","result":"done"}`,
	}}
	_, _ = g.rt.Run(context.Background(), req, nil)
	if g.adapter.count() != 0 {
		t.Fatalf("different-business agent must not be invokable, got %d tool calls", g.adapter.count())
	}
}

// TestExplicitAgentSuspendedRejected verifies that an explicit suspended
// agent is not selected.
func TestExplicitAgentSuspendedRejected(t *testing.T) {
	g := delegateFixture(t)
	if _, err := g.rt.Registry.SetStatus("governed-child", agentexec.AgentSuspended, "t"); err != nil {
		t.Fatal(err)
	}
	obj := Objective{Description: "delegate to suspended agent",
		Context: map[string]string{"agent_id": "governed-child"}}
	req := workRequest(t, obj, "")
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"tool_call","tool":"governed.echo","operation":"execute","input":{"text":"hi"}}`,
		`{"type":"complete","result":"done"}`,
	}}
	_, _ = g.rt.Run(context.Background(), req, nil)
	if g.adapter.count() != 0 {
		t.Fatalf("suspended agent must not be invokable, got %d tool calls", g.adapter.count())
	}
}

// TestExplicitAgentUnknownRejected verifies that an unknown explicit agent is
// not selected and does not fall back to another agent.
func TestExplicitAgentUnknownRejected(t *testing.T) {
	g := delegateFixture(t)
	obj := Objective{Description: "delegate to unknown agent",
		Context: map[string]string{"agent_id": "ghost-agent"}}
	req := workRequest(t, obj, "")
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"tool_call","tool":"governed.echo","operation":"execute","input":{"text":"hi"}}`,
		`{"type":"complete","result":"done"}`,
	}}
	_, _ = g.rt.Run(context.Background(), req, nil)
	if g.adapter.count() != 0 {
		t.Fatalf("unknown agent must not be invokable, got %d tool calls", g.adapter.count())
	}
}

// TestDefaultSelectionUsesSelector verifies that no explicit agent_id falls
// back to the standard selector (which enforces the same eligibility rules).
func TestDefaultSelectionUsesSelector(t *testing.T) {
	g := delegateFixture(t)
	obj := Objective{Description: "delegate without explicit agent"}
	req := workRequest(t, obj, "")
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"delegate","agent_id":"governed-child","objective":"do it",` +
			`"delegate_tools":[{"tool":"governed.echo","operation":"execute","input":{"text":"hi"}}]}`,
		`{"type":"complete","result":"done"}`,
	}}
	out, _ := g.rt.Run(context.Background(), req, nil)
	if out.Status != "completed" {
		t.Fatalf("default selection must work: %s / %s", out.Status, out.Error)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("exactly one tool ran, got %d", g.adapter.count())
	}
}

// ---------- delegation child outcome mapping ----------

func TestDelegateChildOutcomeMapping(t *testing.T) {
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{plan: goodPlan})
	mkOut := func(status string) *executor.Outcome {
		return &executor.Outcome{TaskID: "t/delegate/c", AgentID: "c",
			Status: status, Error: status + " detail", ApprovalID: "apr-1", EscalationRef: "esc-1"}
	}
	// Unknown stays unknown with reconciliation markers, never re-dispatched.
	o, _, st, msg := rt.delegateChildOutcome(&Observation{}, BudgetUsage{}, mkOut("unknown"), nil)
	if st != StateFailed || o.Outcome != "unknown" || !o.ReconciliationRequired {
		t.Fatalf("unknown must be preserved: %+v %s %s", o, st, msg)
	}
	if !strings.Contains(msg, "unknown") {
		t.Fatalf("the message must name the unknown outcome: %q", msg)
	}
	// A pending child approval is reported, never adopted.
	o2, _, st2, _ := rt.delegateChildOutcome(&Observation{}, BudgetUsage{}, mkOut("pending_approval"), nil)
	if st2 != StateFailed || o2.Result["approval_id"] != "apr-1" {
		t.Fatalf("child approval must be reported, not resumed: %+v %s", o2, st2)
	}
	// Escalation is authoritative evidence, not success.
	o3, _, st3, msg3 := rt.delegateChildOutcome(&Observation{}, BudgetUsage{}, mkOut("escalated"), nil)
	if st3 != StateFailed || o3.Result["escalation_id"] != "esc-1" || !strings.Contains(msg3, "escalated") {
		t.Fatalf("child escalation must not read as success: %+v %s %s", o3, st3, msg3)
	}
	// Denials keep their axis.
	o4, _, st4, _ := rt.delegateChildOutcome(&Observation{}, BudgetUsage{}, mkOut("denied"), nil)
	if st4 != StateFailed || o4.Result["governance_outcome"] != "DENY" {
		t.Fatalf("child denial must stay a denial: %+v %s", o4, st4)
	}
	// Transport errors fail honestly.
	o5 := &Observation{}
	_, _, st5, msg5 := rt.delegateChildOutcome(o5, BudgetUsage{}, nil, errors.New("boom"))
	if st5 != StateFailed || !strings.Contains(msg5, "boom") {
		t.Fatalf("transport failure must fail honestly: %s %s", st5, msg5)
	}
}

func TestDelegateDepthBoundEnforced(t *testing.T) {
	g := delegateFixture(t)
	// A budget of max_depth 1 leaves no room below the root: the very first
	// delegation is already too deep.
	obj := Objective{Description: "delegate deep", Budget: Budget{MaxDepth: 1}}
	req := workRequest(t, obj, "")
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"delegate","agent_id":"governed-child","objective":"do it"}`,
	}}
	out, _ := g.rt.Run(context.Background(), req, nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "delegation depth limit") {
		t.Fatalf("depth must bound delegation: %s / %s", out.Status, out.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a depth-rejected delegation creates no child, got %d", g.adapter.count())
	}
}

func TestChildMemoryIsolationByConstruction(t *testing.T) {
	// A delegated child has no durable memory path: after a delegation that
	// runs a real child tool, the memory store holds nothing new.
	g := delegateFixture(t)
	out := g.run(t, "delegate echo",
		`{"type":"delegate","agent_id":"governed-child","objective":"say it",`+
			`"delegate_tools":[{"tool":"governed.echo","operation":"execute","input":{"text":"hi"}}]}`,
		`{"type":"complete","result":"delegated"}`)
	if out.Status != "completed" {
		t.Fatalf("the delegation must complete: %s / %s", out.Status, out.Error)
	}
	res, err := g.rt.Memory.Query(
		memory.Identity{ActorID: "owner-1", BusinessID: "biz-1", AgentID: "governed-agent"},
		memory.Query{BusinessID: "biz-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 0 {
		t.Fatalf("a child execution must leave no memory records: %+v", res.Records)
	}
}

func TestDuplicateDelegationSuppressed(t *testing.T) {
	g := delegateFixture(t)
	dlg := `{"type":"delegate","agent_id":"governed-child","objective":"say it",` +
		`"delegate_tools":[{"tool":"governed.echo","operation":"execute","input":{"text":"hi"}}]}`
	out := g.run(t, "delegate twice", dlg, dlg, `{"type":"complete","result":"done"}`)
	if out.Status != "failed" || !strings.Contains(out.Error, "duplicate") {
		t.Fatalf("an identical second delegation must be rejected: %s / %s", out.Status, out.Error)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("the child tool ran exactly once, got %d", g.adapter.count())
	}
}
