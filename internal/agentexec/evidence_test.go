package agentexec

import (
	"context"
	"strings"
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

// v52 research evidence: the runtime must hand evidence-aware callers the
// canonical tool result, because a source envelope is only captureable when the
// platform says it was NOT truncated. The map-valued wrapper keeps its exact
// contract and must delegate to the envelope path, never fork it.
func TestInvokeToolScopedResultCarriesTheCanonicalEnvelope(t *testing.T) {
	g := newGovernedRuntime(t, allowPolicy())
	before := g.adapter.count()

	res, err := g.rt.InvokeToolScopedResult(context.Background(), scope(), "agent-1", call())
	if err != nil {
		t.Fatalf("an allowed action must execute: %v", err)
	}
	if res.Status != tool.StatusSuccess || res.Outcome != "completed" {
		t.Fatalf("unexpected envelope: %+v", res)
	}
	if res.ToolID != "echo.tool" || res.Operation != "execute" {
		t.Fatalf("identity missing from envelope: %+v", res)
	}
	if res.Result["output"] != "ran" || res.Truncated {
		t.Fatalf("untruncated result mangled: %+v", res)
	}
	if res.Attempts < 1 {
		t.Fatalf("the envelope must count its attempts: %+v", res)
	}
	if g.adapter.count() != before+1 {
		t.Fatalf("the envelope path must run the adapter exactly once, got %d", g.adapter.count())
	}

	// The map wrapper keeps returning the same map through the same mediated
	// path; it is a convenience surface, not a second unmediated boundary.
	got, err := g.rt.InvokeToolScoped(context.Background(), scope(), "agent-1", call())
	if err != nil || got["output"] != "ran" {
		t.Fatalf("wrapper must stay equivalent: %+v / %v", got, err)
	}
	if g.adapter.count() != before+2 {
		t.Fatalf("the wrapper must delegate to the envelope path, got %d adapter calls", g.adapter.count())
	}
}

// oversizedAdapter emits a value larger than the manifest's output budget so the
// platform truncates and marks the result while still reporting success.
type oversizedAdapter struct{ value string }

func (a oversizedAdapter) Operations() []string { return []string{"execute"} }

func (a oversizedAdapter) Invoke(context.Context, tool.Invocation) (tool.RawResult, error) {
	return tool.RawResult{Result: map[string]string{"output": a.value}}, nil
}

func newYieldingRuntime(t *testing.T, maxOutput int) *Runtime {
	t.Helper()
	members := identity.NewMembershipSet()
	if err := members.Add(identity.Membership{IdentityID: "actor-1", BusinessID: "biz-1",
		Role: identity.RoleMember, Status: identity.StatusActive}); err != nil {
		t.Fatal(err)
	}
	tools := tool.NewToolRegistry()
	platform := capability.New(tools, members,
		capability.NewScopedCredentialResolver(security.NewDevResolver()), event.NewMemBus())
	toolID := "yield.tool"
	manifest := tool.ToolManifest{
		ID: toolID, Version: "1.0.0", Name: "yield.tool", Description: "test",
		Category:        tool.ToolCategoryRead,
		InputSchema:     tool.Schema{Fields: []tool.SchemaField{{Name: "text", Type: tool.FieldString, MaxLength: 32}}},
		OutputSchema:    tool.Schema{Fields: []tool.SchemaField{{Name: "output", Type: tool.FieldString, MaxLength: 4096}}},
		SideEffectClass: tool.SideEffectRead, NetworkRequirement: tool.NetworkNone,
		ScopeRequirement: tool.ScopeBusiness, SecurityClass: tool.SecuritySandboxed,
		Operations:     []string{"execute"},
		ResourceLimits: tool.ResourceLimits{MaxDuration: time.Second, MaxOutputByte: maxOutput},
	}
	if err := tools.Register(manifest, oversizedAdapter{value: strings.Repeat("x", 512)}); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry("nx:test-yield", testOrg(t), tools)
	if _, err := reg.Create(Definition{ID: "agent-1", Name: "a", BusinessID: "biz-1",
		Capabilities: []string{"analysis"}, AllowedTools: []string{toolID}}, "t"); err != nil {
		t.Fatal(err)
	}
	return &Runtime{
		Registry: reg, Tools: tools, ToolExec: BuiltinExecutor(),
		Platform: platform,
		Controller: control.New(control.Options{Engine: governance.NewEngine([]*governance.Policy{allowPolicy()}),
			Approver:  &admissionHost{approved: map[string]bool{}, pending: map[string]string{}},
			Escalator: &admissionHost{approved: map[string]bool{}, pending: map[string]string{}}}),
		now: time.Now, maxDepth: 3, maxFan: 4, maxConcurrent: 4,
	}
}

func TestInvokeToolScopedResultSurfacesTruncation(t *testing.T) {
	rt := newYieldingRuntime(t, 8)
	res, err := rt.InvokeToolScopedResult(context.Background(), ToolScope{
		ActorID: "actor-1", BusinessID: "biz-1", AgentTools: []string{"yield.tool"},
		CorrelationID: "exec-1",
	}, "agent-1", ToolCallSpec{ToolID: "yield.tool", Operation: "execute", Input: map[string]string{"text": "hi"}})
	if err != nil {
		t.Fatalf("a truncated-but-successful result is not an error: %v", err)
	}
	if res.Status != tool.StatusSuccess {
		t.Fatalf("status=%s", res.Status)
	}
	if !res.Truncated {
		t.Fatalf("the truncated envelope must say so: %+v", res)
	}
}
