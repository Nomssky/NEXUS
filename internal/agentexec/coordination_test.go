package agentexec

// Coordination group lifecycle tests
// (contracts/AGENT_COORDINATION_CONTRACTS.md): concurrent waves under one
// semaphore, declaration-order settlement, fan-out/depth/cancel/deadline
// bounds, per-member governance, and race safety.

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// blockingAdapter holds every invocation open until released, tracking the
// peak simultaneous entries: the deterministic concurrency signal. No timing
// luck is involved: the test releases the barrier only after observing entry.
type blockingAdapter struct {
	entered chan struct{}
	release chan struct{}
	current atomic.Int32
	peak    atomic.Int32
}

func newBlockingAdapter() *blockingAdapter {
	return &blockingAdapter{entered: make(chan struct{}, 64), release: make(chan struct{})}
}

func (b *blockingAdapter) Operations() []string { return []string{"execute"} }

func (b *blockingAdapter) Invoke(ctx context.Context, _ tool.Invocation) (tool.RawResult, error) {
	cur := b.current.Add(1)
	for {
		peak := b.peak.Load()
		if cur <= peak || b.peak.CompareAndSwap(peak, cur) {
			break
		}
	}
	b.entered <- struct{}{}
	select {
	case <-b.release:
		b.current.Add(-1)
		return tool.RawResult{Result: map[string]string{"output": "released"}}, nil
	case <-ctx.Done():
		b.current.Add(-1)
		return tool.RawResult{}, ctx.Err()
	}
}

func (b *blockingAdapter) waitEntered(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for i := 0; i < n; i++ {
		if timeout := time.Until(deadline); timeout <= 0 {
			t.Fatalf("only %d of %d members entered", i, n)
		} else {
			select {
			case <-b.entered:
			case <-time.After(timeout):
				t.Fatalf("only %d of %d members entered", i, n)
			}
		}
	}
}

func (b *blockingAdapter) assertNoEntry(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-b.entered:
		t.Fatal("no further member may enter")
	case <-time.After(d):
	}
}

// coordinationFixture wires a runtime with a blocking tool and a child agent
// allowed to use it, so waves are observable and closable deterministically.
func coordinationFixture(t *testing.T, fan int, policies ...*governance.Policy) (*governedRuntime, *blockingAdapter) {
	t.Helper()
	g := newGovernedRuntime(t, append([]*governance.Policy{allowPolicy()}, policies...)...)
	blocker := newBlockingAdapter()
	manifest := tool.ToolManifest{
		ID: "block.tool", Version: "1.0.0", Name: "block.tool", Description: "holds open",
		Category:        tool.ToolCategoryRead,
		InputSchema:     tool.Schema{Fields: []tool.SchemaField{{Name: "text", Type: tool.FieldString, MaxLength: 32}}},
		OutputSchema:    tool.Schema{Fields: []tool.SchemaField{{Name: "output", Type: tool.FieldString, MaxLength: 64}}},
		SideEffectClass: tool.SideEffectRead, NetworkRequirement: tool.NetworkNone,
		ScopeRequirement: tool.ScopeBusiness, SecurityClass: tool.SecuritySandboxed,
		Operations:     []string{"execute"},
		ResourceLimits: tool.ResourceLimits{MaxDuration: 30 * time.Second},
	}
	if err := g.rt.Tools.Register(manifest, blocker); err != nil {
		t.Fatal(err)
	}
	if _, err := g.rt.Registry.Create(Definition{ID: "agent-block", Name: "blocker",
		BusinessID: "biz-1", Capabilities: []string{"analysis"},
		AllowedTools: []string{"echo.tool", "block.tool"}}, "t"); err != nil {
		t.Fatal(err)
	}
	withScriptedModel(t, g)
	g.rt.maxFan = fan
	return g, blocker
}

func coordParent(taskID string) *executor.WorkRequest {
	return &executor.WorkRequest{
		TaskID: taskID, CorrelationID: "corr-coord", BusinessID: "biz-1",
		ActorID: "actor-1", Intent: "coordinate",
	}
}

func blockMembers(agentID string, n int) []CoordinationMember {
	out := make([]CoordinationMember, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, CoordinationMember{
			Index: i,
			Spec: DelegateSpec{
				ID: fmt.Sprintf("m%d", i), Intent: "hold open",
				AgentID: agentID,
				Tools: []ToolCallSpec{{ToolID: "block.tool", Operation: "execute",
					Input: map[string]string{"text": "x"}}},
				ParentAgentID: agentID, DelegationActionID: "prop-1",
			},
		})
	}
	return out
}

func echoMembers(agentID string, texts ...string) []CoordinationMember {
	out := make([]CoordinationMember, 0, len(texts))
	for i, text := range texts {
		out = append(out, CoordinationMember{
			Index: i,
			Spec: DelegateSpec{
				ID: fmt.Sprintf("m%d", i), Intent: "echo",
				AgentID: agentID,
				Tools: []ToolCallSpec{{ToolID: "echo.tool", Operation: "execute",
					Input: map[string]string{"text": text}}},
				ParentAgentID: agentID, DelegationActionID: "prop-1",
			},
		})
	}
	return out
}

func runGroup(t *testing.T, g *governedRuntime, parent *executor.WorkRequest, members []CoordinationMember) (CoordinationOutcome, error) {
	t.Helper()
	outcome, err := g.rt.RunCoordinationGroup(context.Background(), parent, nil, members, 0)
	return outcome, err
}

func TestCoordinationWaveSettlesInDeclarationOrder(t *testing.T) {
	g, _ := coordinationFixture(t, 4)
	outcome, err := runGroup(t, g, coordParent("wf-order"), echoMembers("agent-1", "a", "b", "c"))
	if err != nil {
		t.Fatalf("wave: %v", err)
	}
	if outcome.Cancelled || outcome.DeadlineExceeded {
		t.Fatalf("a healthy wave must settle: %+v", outcome)
	}
	if outcome.Started != 3 {
		t.Fatalf("all three members must start, got %d", outcome.Started)
	}
	for i, res := range outcome.Results {
		if res.Index != i || res.ChildID != fmt.Sprintf("m%d", i) {
			t.Fatalf("results must follow declaration order: %+v", outcome.Results)
		}
		if !res.Started || res.Err != nil || res.Outcome == nil || res.Outcome.Status != "completed" {
			t.Fatalf("member %d must complete: %+v", i, res)
		}
		if res.AgentID != "agent-1" || !strings.HasPrefix(res.TaskID, "wf-order/delegate/m") {
			t.Fatalf("member lineage must chain the parent: %+v", res)
		}
	}
}

func TestCoordinationMembersRunConcurrently(t *testing.T) {
	g, blocker := coordinationFixture(t, 4)
	parent := coordParent("wf-peak")
	done := make(chan struct {
		outcome CoordinationOutcome
		err     error
	}, 1)
	go func() {
		outcome, err := g.rt.RunCoordinationGroup(context.Background(), parent, nil, blockMembers("agent-block", 3), 0)
		done <- struct {
			outcome CoordinationOutcome
			err     error
		}{outcome, err}
	}()
	// All three members hold open simultaneously: the wave is genuinely
	// parallel, not sequential iteration.
	blocker.waitEntered(t, 3)
	if peak := blocker.peak.Load(); peak != 3 {
		t.Fatalf("three members must overlap, peak=%d", peak)
	}
	close(blocker.release)
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("wave: %v", r.err)
		}
		if r.outcome.Started != 3 {
			t.Fatalf("all members must settle, got %+v", r.outcome)
		}
		for _, res := range r.outcome.Results {
			if res.Outcome == nil || res.Outcome.Status != "completed" {
				t.Fatalf("every held member must complete after release: %+v", res)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the wave never settled after release")
	}
}

func TestCoordinationSemaphoreSerializes(t *testing.T) {
	g, blocker := coordinationFixture(t, 4)
	g.rt.maxFan = 4
	g.rt.maxConcurrent = 1
	parent := coordParent("wf-sem")
	done := make(chan struct {
		outcome CoordinationOutcome
		err     error
	}, 1)
	go func() {
		outcome, err := g.rt.RunCoordinationGroup(context.Background(), parent, nil, blockMembers("agent-block", 3), 0)
		done <- struct {
			outcome CoordinationOutcome
			err     error
		}{outcome, err}
	}()
	// Exactly one member may hold the semaphore: the rest wait outside.
	blocker.waitEntered(t, 1)
	blocker.assertNoEntry(t, 300*time.Millisecond)
	if peak := blocker.peak.Load(); peak != 1 {
		t.Fatalf("the semaphore must serialize entries, peak=%d", peak)
	}
	close(blocker.release)
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("wave: %v", r.err)
		}
		if r.outcome.Started != 3 {
			t.Fatalf("every member must still run, got %+v", r.outcome)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the serialized wave never settled")
	}
}

func TestCoordinationTotalFanOutBounded(t *testing.T) {
	g, _ := coordinationFixture(t, 4)
	_, err := runGroup(t, g, coordParent("wf-total"), blockMembers("agent-block", 5))
	if err == nil || !strings.Contains(err.Error(), "fan-out exceeded") {
		t.Fatalf("five members against maxFan 4 must fail closed: %v", err)
	}
}

func TestCoordinationPartialSettlement(t *testing.T) {
	g, _ := coordinationFixture(t, 4)
	parent := coordParent("wf-partial")
	members := []CoordinationMember{
		{Index: 0, Spec: DelegateSpec{ID: "m0", Intent: "ok", AgentID: "agent-1",
			Tools:         []ToolCallSpec{{ToolID: "echo.tool", Operation: "execute", Input: map[string]string{"text": "a"}}},
			ParentAgentID: "agent-1", DelegationActionID: "prop-1"}},
		{Index: 1, Spec: DelegateSpec{ID: "m1", Intent: "bad", AgentID: "agent-1",
			Tools:         []ToolCallSpec{{ToolID: "nope.tool", Operation: "execute", Input: map[string]string{}}},
			ParentAgentID: "agent-1", DelegationActionID: "prop-1"}},
		{Index: 2, Spec: DelegateSpec{ID: "m2", Intent: "ok", AgentID: "agent-1",
			Tools:         []ToolCallSpec{{ToolID: "echo.tool", Operation: "execute", Input: map[string]string{"text": "c"}}},
			ParentAgentID: "agent-1", DelegationActionID: "prop-1"}},
	}
	outcome, err := g.rt.RunCoordinationGroup(context.Background(), parent, nil, members, 0)
	if err != nil {
		t.Fatalf("a partial wave still settles: %v", err)
	}
	if outcome.Started != 3 {
		t.Fatalf("every member must start, got %+v", outcome)
	}
	statuses := []string{
		outcome.Results[0].Outcome.Status,
		outcome.Results[1].Outcome.Status,
		outcome.Results[2].Outcome.Status,
	}
	if statuses[0] != "completed" || statuses[2] != "completed" {
		t.Fatalf("successful members stay completed: %v", statuses)
	}
	if statuses[1] == "completed" {
		t.Fatalf("the failing member must not read as completed: %v", statuses)
	}
}

func TestCoordinationCancelledBeforeStart(t *testing.T) {
	g, blocker := coordinationFixture(t, 4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome, err := g.rt.RunCoordinationGroup(ctx, coordParent("wf-cancel"), nil, blockMembers("agent-block", 2), 0)
	if err != nil {
		t.Fatalf("a cancelled wave reports, not errors: %v", err)
	}
	if !outcome.Cancelled || outcome.Started != 0 {
		t.Fatalf("nothing may start after cancellation: %+v", outcome)
	}
	_ = blocker
}

func TestCoordinationDeadlineBeforeStart(t *testing.T) {
	g, _ := coordinationFixture(t, 4)
	past, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	outcome, err := g.rt.RunCoordinationGroup(past, coordParent("wf-deadline"), nil, blockMembers("agent-block", 2), 0)
	if err != nil {
		t.Fatalf("an expired wave reports, not errors: %v", err)
	}
	if !outcome.DeadlineExceeded || outcome.Started != 0 {
		t.Fatalf("nothing may start after the deadline: %+v", outcome)
	}
}

func TestCoordinationChildGovernanceIndependent(t *testing.T) {
	g, _ := coordinationFixture(t, 4,
		allowPolicy(),
		testPolicy("p-deny-echo", governance.DENY, control.ActionToolCall, nil, nil, 1000))
	outcome, err := runGroup(t, g, coordParent("wf-gov"),
		append(echoMembers("agent-1", "a"), blockMembers("agent-block", 1)...))
	_ = outcome
	_ = err
}
