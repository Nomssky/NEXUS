package agentexec

// Delegation boundary tests (contracts/AGENT_DELEGATION_CONTRACTS.md):
// explicit parent linkage, inherited-not-widened scope, bounded fan-out and
// depth, cancellation, and per-effect child governance through the same
// admission point as every other tool call.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
)

// withScriptedModel gives the runtime a deterministic model so delegated
// children complete their model unit without an external provider.
func withScriptedModel(t *testing.T, g *governedRuntime) {
	t.Helper()
	reg := modelrouter.NewModelRegistry()
	mr := modelrouter.NewModelRouter(reg, modelrouter.RoutingPolicyLocalFirst)
	mr.RegisterProvider(modelrouter.NewScriptedProvider(modelrouter.ProviderConfig{ID: "scripted"}))
	if err := reg.RegisterModel(&modelrouter.ModelDefinition{
		ID: "scripted:default", ProviderID: "scripted",
		Capabilities: []modelrouter.ModelCapability{modelrouter.CapabilityReasoning},
		Runtime:      modelrouter.RuntimeLocal, Status: modelrouter.ModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	g.rt.Router = mr
}

func delegateReq(taskID string, delegates ...DelegateSpec) *executor.WorkRequest {
	return &executor.WorkRequest{
		TaskID: taskID, CorrelationID: "corr-1", BusinessID: "biz-1",
		ActorID: "actor-1", Intent: "parent work",
		Input: map[string]string{"agent_execution": mustMarshalSpec(&Spec{
			AgentID: "agent-1", Delegates: delegates,
		})},
	}
}

// eventRecorder captures bus events for linkage assertions.
type eventRecorder struct {
	mu     sync.Mutex
	events []*event.Event
}

func (r *eventRecorder) Handle(e *event.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *eventRecorder) fields(want event.EventType) []map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]string
	for _, e := range r.events {
		if e.Type != want {
			continue
		}
		var m map[string]string
		if err := json.Unmarshal(e.Data, &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

func TestDelegateChildStampsExplicitLinkage(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	withScriptedModel(t, g)
	rec := &eventRecorder{}
	bus := event.NewMemBus()
	if _, err := bus.Subscribe(rec, event.EventTypeAgentStarted); err != nil {
		t.Fatal(err)
	}
	g.rt.Bus = bus
	parent := &executor.WorkRequest{
		TaskID: "wf-9", CorrelationID: "corr-9", BusinessID: "biz-1",
		ActorID: "actor-1", Intent: "parent work",
	}
	out, err := g.rt.DelegateChild(context.Background(), parent, nil, DelegateSpec{
		ID: "c1", Intent: "child work", AgentID: "agent-1",
		ParentAgentID: "agent-1", DelegationActionID: "prop-7",
	}, 0)
	if err != nil {
		t.Fatalf("delegation: %v", err)
	}
	if out.Status != "completed" {
		t.Fatalf("child must complete: %+v", out)
	}
	// Lineage is observable, not conventional: the task path extends the
	// parent, scope is inherited unwidened, and the child runs as its own
	// registry identity.
	if !strings.HasPrefix(out.TaskID, "wf-9/delegate/c1") {
		t.Fatalf("child task must extend the parent path: %q", out.TaskID)
	}
	if out.BusinessID != "biz-1" || out.CorrelationID != "corr-9" {
		t.Fatalf("child scope must equal parent scope: %+v", out)
	}
	if out.AgentID != "agent-1" {
		t.Fatalf("child runs as its own registry identity: %+v", out)
	}
	// The stamped linkage rides the child's start event as metadata.
	if _, err := bus.Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	started := rec.fields(event.EventTypeAgentStarted)
	found := false
	for _, m := range started {
		if m["parent_task_id"] == "wf-9" && m["parent_agent_id"] == "agent-1" &&
			m["delegation_action_id"] == "prop-7" && m["delegation_depth"] == "1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the child start must carry the stamped linkage: %v", started)
	}
	// A root execution carries no linkage fields at all.
	if got := delegationFields(&executor.WorkRequest{TaskID: "wf-root"}); got != nil {
		t.Fatalf("a root execution must not carry linkage: %v", got)
	}
	if got := delegationFields(nil); got != nil {
		t.Fatalf("nil request must not carry linkage: %v", got)
	}
}

func TestSpecFanOutBounded(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	delegates := make([]DelegateSpec, 0, 5)
	for i := 0; i < 5; i++ {
		delegates = append(delegates, DelegateSpec{ID: string(rune('a' + i)), Intent: "x", AgentID: "agent-1"})
	}
	out, err := g.rt.execute(context.Background(), delegateReq("wf-fan", delegates...), nil, 0)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.Status != "failed" || !strings.Contains(out.Error, "fan-out exceeded") {
		t.Fatalf("fan-out beyond maxFan must fail closed: %+v", out)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a rejected fan-out runs nothing, got %d", g.adapter.count())
	}
}

func TestNestedDepthBounded(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	withScriptedModel(t, g)
	// Depth == maxDepth still runs; anything deeper fails closed.
	out, err := g.rt.execute(context.Background(), delegateReq("wf-ok"), nil, 3)
	if err != nil || out.Status != "completed" {
		t.Fatalf("depth == maxDepth still runs: %+v %v", out, err)
	}
	out, err = g.rt.execute(context.Background(), delegateReq("wf-too-deep"), nil, 4)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.Status != "failed" || !strings.Contains(out.Error, "delegation depth exceeded") {
		t.Fatalf("depth beyond maxDepth must fail: %+v", out)
	}
}

func TestGrandchildStaysBounded(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	withScriptedModel(t, g)
	rec := &eventRecorder{}
	bus := event.NewMemBus()
	if _, err := bus.Subscribe(rec, event.EventTypeAgentStarted); err != nil {
		t.Fatal(err)
	}
	g.rt.Bus = bus
	// A linked child execution that delegates once more: the grandchild runs
	// at depth 2 with the root scope intact and its own parent link stamped.
	grandchild := &executor.WorkRequest{
		TaskID: "wf-root/delegate/c1", CorrelationID: "corr-1", BusinessID: "biz-1",
		ActorID: "actor-1", Intent: "child work",
		Input: map[string]string{"agent_execution": mustMarshalSpec(&Spec{
			AgentID:   "agent-1",
			Delegates: []DelegateSpec{{ID: "g1", Intent: "grandchild work", AgentID: "agent-1"}},
		})},
		Delegation: &executor.DelegationLink{
			ParentTaskID: "wf-root", ParentAgentID: "agent-1",
			ParentCorrelationID: "corr-1", ActionID: "prop-1", Depth: 1,
		},
	}
	out, err := g.rt.execute(context.Background(), grandchild, nil, 1)
	if err != nil || out.Status != "completed" {
		t.Fatalf("a bounded grandchild must complete: %+v %v", out, err)
	}
	if out.BusinessID != "biz-1" || out.CorrelationID != "corr-1" {
		t.Fatalf("nested scope must stay root scope: %+v", out)
	}
	if _, err := bus.Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// The grandchild's own start event links it to the child execution at
	// depth 2 — lineage chains through both levels.
	found := false
	for _, m := range rec.fields(event.EventTypeAgentStarted) {
		if m["parent_task_id"] == "wf-root/delegate/c1" && m["delegation_depth"] == "2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the grandchild start must link to its parent: %v", rec.fields(event.EventTypeAgentStarted))
	}
}

func TestDelegateChildCancelledContextCancels(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	withScriptedModel(t, g)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	parent := &executor.WorkRequest{
		TaskID: "wf-cancel", CorrelationID: "corr-cancel", BusinessID: "biz-1",
		ActorID: "actor-1", Intent: "parent work",
	}
	out, err := g.rt.DelegateChild(ctx, parent, nil, DelegateSpec{
		ID: "c1", Intent: "child work", AgentID: "agent-1",
		Tools:         []ToolCallSpec{{ToolID: "echo.tool", Operation: "execute", Input: map[string]string{"text": "hi"}}},
		ParentAgentID: "agent-1", DelegationActionID: "prop-x",
	}, 0)
	if err != nil {
		t.Fatalf("delegation: %v", err)
	}
	if out.Status != "cancelled" {
		t.Fatalf("a cancelled parent context must cancel the child: %+v", out)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a cancelled child runs no tool, got %d", g.adapter.count())
	}
}

func TestChildScopeCannotWiden(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	withScriptedModel(t, g)
	// DelegateChild copies the parent scope fields unconditionally: there is
	// no parameter through which a child could claim another business.
	parent := &executor.WorkRequest{
		TaskID: "wf-scope", CorrelationID: "corr-scope", BusinessID: "biz-1",
		DivisionID: "div-1", ActorID: "actor-1", Intent: "parent work",
	}
	out, err := g.rt.DelegateChild(context.Background(), parent, nil, DelegateSpec{
		ID: "c1", Intent: "child work", AgentID: "agent-1",
		ParentAgentID: "agent-1", DelegationActionID: "prop-x",
	}, 0)
	if err != nil || out.Status != "completed" {
		t.Fatalf("child must complete: %+v %v", out, err)
	}
	if out.BusinessID != "biz-1" {
		t.Fatalf("child business must equal parent business: %+v", out)
	}
}
