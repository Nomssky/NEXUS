package agentexec

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

func testOrg(t *testing.T) *identity.Registry {
	reg := identity.NewRegistry("nx:test:nexus")
	now := time.Now().UTC()
	reg.CreateIdentity(identity.Identity{ID: "owner-1", Type: identity.TypeHuman, DisplayName: "owner", CreatedAt: now, Provenance: schema.ProvenanceRef{Producer: "test", ProducedAt: now}}, "test")
	reg.CreateBusiness(identity.Business{EntityID: "biz-1", Name: "biz", OwnerIdentityID: "owner-1", CreatedAt: now}, "test")
	reg.CreateDivision(identity.Division{EntityID: "div-1", BusinessID: "biz-1", Name: "d1", OwnerIdentityID: "owner-1", CreatedAt: now}, "test")
	return reg
}

func TestRegistryCreateGetListAndTransitions(t *testing.T) {
	reg := NewRegistry("nx:test:nexus", testOrg(t), tool.NewToolRegistry())
	d, err := reg.Create(Definition{
		ID: "agent-researcher", Name: "Researcher", BusinessID: "biz-1",
		Capabilities: []string{"research", "analysis"}, Memory: MemoryConfig{Mode: "business"},
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != AgentActive {
		t.Fatalf("default status: got %q", d.Status)
	}
	if _, ok := reg.Get(d.ID); !ok {
		t.Fatal("get")
	}
	list := reg.List("biz-1", "")
	if len(list) != 1 {
		t.Fatalf("list: got %d", len(list))
	}
	if _, err := reg.Create(Definition{ID: d.ID, Name: "dupe", BusinessID: "biz-1", Capabilities: []string{"x"}}, "t"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: got %v", err)
	}
	// Suspend/activate/archive
	if _, err := reg.SetStatus(d.ID, AgentSuspended, "t"); err != nil {
		t.Fatal(err)
	}
	if got, _ := reg.Get(d.ID); got.Status != AgentSuspended {
		t.Fatalf("suspended: %q", got.Status)
	}
	if _, err := reg.SetStatus(d.ID, AgentArchived, "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetStatus(d.ID, AgentActive, "t"); err == nil {
		t.Fatal("archived must be terminal")
	}
}

func TestRegistryValidationErrors(t *testing.T) {
	reg := NewRegistry("nx:test:nexus", testOrg(t), tool.NewToolRegistry())
	if _, err := reg.Create(Definition{Name: "x", BusinessID: "biz-1", Capabilities: []string{"x"}}, "t"); err == nil {
		t.Fatal("missing id")
	}
	if _, err := reg.Create(Definition{ID: "a1", Name: "x", BusinessID: "no-biz", Capabilities: []string{"x"}}, "t"); err == nil {
		t.Fatal("unknown business")
	}
	if _, err := reg.Create(Definition{ID: "a2", Name: "x", BusinessID: "biz-1", DivisionID: "no-div", Capabilities: []string{"x"}}, "t"); err == nil {
		t.Fatal("unknown division")
	}
}

func TestRegistryDurability(t *testing.T) {
	st := store.NewMemStore()
	reg, err := OpenRegistry("nx:test:nexus", testOrg(t), tool.NewToolRegistry(), st)
	if err != nil {
		t.Fatal(err)
	}
	reg.Create(Definition{ID: "a-persist", Name: "persist", BusinessID: "biz-1", Capabilities: []string{"analysis"}}, "t")
	reg2, err := OpenRegistry("nx:test:nexus", testOrg(t), tool.NewToolRegistry(), st)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg2.Get("a-persist"); !ok {
		t.Fatal("durable definition not hydrated on reopen")
	}
}

func TestSelectorDeterministic(t *testing.T) {
	tools := tool.NewToolRegistry()
	registerBuiltins(tools)
	reg := NewRegistry("nx:test:nexus", testOrg(t), tools)
	reg.Create(Definition{ID: "a-wide", Name: "wide", BusinessID: "biz-1", Capabilities: []string{"research", "analysis"}}, "t")
	reg.Create(Definition{ID: "a-narrow", Name: "n", BusinessID: "biz-1", DivisionID: "div-1", Capabilities: []string{"research"}}, "t")
	reg.Create(Definition{ID: "a-inactive", Name: "i", BusinessID: "biz-1", Capabilities: []string{"research"}}, "t")
	reg.SetStatus("a-inactive", AgentSuspended, "t")

	// Business scope: division-scoped agent is invisible (G3).
	sel := Select(reg, &SelectionRequest{BusinessID: "biz-1", Required: []string{"research"}}, toolsAvailable)
	if sel.Agent == nil || sel.Agent.ID != "a-wide" {
		t.Fatalf("business scope: got %+v", sel)
	}
	if len(sel.RejectedCandidates) == 0 {
		t.Fatal("expect rejected candidates for explainability")
	}
	// Division scope: both business-wide and division agent apply; stable order wins.
	sel = Select(reg, &SelectionRequest{BusinessID: "biz-1", DivisionID: "div-1", Required: []string{"research"}}, toolsAvailable)
	if sel.Agent == nil {
		t.Fatal("division scope: nil agent")
	}
	// Explicit preference is validated.
	sel = Select(reg, &SelectionRequest{BusinessID: "biz-1", DivisionID: "div-1", Required: []string{}, AgentID: "a-narrow"}, toolsAvailable)
	if sel.Agent == nil || sel.Agent.ID != "a-narrow" {
		t.Fatalf("explicit preference: %+v", sel)
	}
	sel = Select(reg, &SelectionRequest{BusinessID: "biz-1", Required: []string{"research", "coding"}}, toolsAvailable)
	if sel.Agent != nil {
		t.Fatalf("uncovered capability must yield nil: %+v", sel)
	}
}

func TestSelectionDivisionNarrowing(t *testing.T) {
	reg := NewRegistry("nx:test:nexus", testOrg(t), tool.NewToolRegistry())
	reg.Create(Definition{ID: "div-agent", Name: "d", BusinessID: "biz-1", DivisionID: "div-1", Capabilities: []string{"research"}}, "t")
	// A division-scoped agent cannot serve a business-scope request.
	sel := Select(reg, &SelectionRequest{BusinessID: "biz-1", Required: []string{"research"}}, nil)
	if sel.Agent != nil {
		t.Fatalf("division agent must not serve business scope: %+v", sel)
	}
}

func toolsAvailable(id string) bool { return true }

func TestSelectorExplicitUnknown(t *testing.T) {
	reg := NewRegistry("nx:test:nexus", testOrg(t), tool.NewToolRegistry())
	sel := Select(reg, &SelectionRequest{BusinessID: "biz-1", AgentID: "nope"}, nil)
	if sel.Agent != nil {
		t.Fatal("unknown explicit agent must yield nil")
	}
}

func TestRuntimeModelFailureNoSuccess(t *testing.T) {
	// Offline provider => the runtime must report failed, never completed.
	reg := NewRegistry("nx:test:nexus", testOrg(t), tool.NewToolRegistry())
	reg.Create(Definition{ID: "ag", Name: "a", BusinessID: "biz-1", Capabilities: []string{"research"}}, "t")
	modelReg := modelrouter.NewModelRegistry()
	robots := modelrouter.NewLocalProvider(modelrouter.ProviderConfig{ID: "simulated", Status: modelrouter.ProviderStatusOffline})
	router := modelrouter.NewModelRouter(modelReg, modelrouter.RoutingPolicyLocalFirst)
	router.RegisterProvider(robots)
	_ = modelReg.RegisterModel(&modelrouter.ModelDefinition{
		ID: "simulated:default", ProviderID: "simulated",
		Capabilities: []modelrouter.ModelCapability{modelrouter.CapabilityReasoning},
		Runtime:      modelrouter.RuntimeLocal, Status: modelrouter.ModelStatusActive,
	})
	rt := NewRuntime(reg, tool.NewToolRegistry(), router, nil)
	spec := mustMarshalSpec(&Spec{RequiredCapabilities: []string{"research"}})
	req := &executor.WorkRequest{
		TaskID: "t-1", CorrelationID: "c-1", BusinessID: "biz-1", ActorID: "owner-1",
		Intent: "research", Input: map[string]string{"agent_execution": spec},
	}
	out, err := rt.Handler()(context.Background(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "failed" {
		b, _ := json.MarshalIndent(out, "", "  ")
		t.Fatalf("offline provider must not succeed; out=%s", b)
	}
	if out.Output != "" {
		t.Fatalf("failed outcome must have no agent output")
	}
}
