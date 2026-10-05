package agentexec

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// flakyProvider fails the first call with a transient-looking error, the
// second succeeds. provider.simulated errors fail honestly otherwise.
type flakyProvider struct {
	calls *int32
}

func (f *flakyProvider) Identify() string              { return "flaky" }
func (f *flakyProvider) HealthCheck() error            { return nil }
func (f *flakyProvider) ListModels() ([]string, error) { return []string{"flaky:default"}, nil }
func (f *flakyProvider) Invoke(_ context.Context, _ *modelrouter.GenerateRequest) (*modelrouter.GenerateResponse, error) {
	n := atomic.AddInt32(f.calls, 1)
	if n == 1 {
		return nil, errors.New("provider flaky: response timeout")
	}
	return &modelrouter.GenerateResponse{RequestID: "r1", ModelID: "flaky:default", Content: "ok", FinishReason: "stop"}, nil
}

func TestRuntimeRetriesTransientOnceAndCounts(t *testing.T) {
	reg := NewRegistry("nx:test:nexus", testOrg(t), tool.NewToolRegistry())
	reg.Create(Definition{ID: "ag", Name: "a", BusinessID: "biz-1", Capabilities: []string{"research"}}, "t")
	modelReg := modelrouter.NewModelRegistry()
	var calls int32
	router := modelrouter.NewModelRouter(modelReg, modelrouter.RoutingPolicyLocalFirst)
	router.RegisterProvider(&flakyProvider{calls: &calls})
	_ = modelReg.RegisterModel(&modelrouter.ModelDefinition{
		ID: "flaky:default", ProviderID: "flaky",
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
	if out.Status != "completed" {
		t.Fatalf("transient retry should recover: %q %s", out.Status, out.Error)
	}
	t.Logf("provider=%s model=%s retries=%d calls=%d reason=%q", out.Provider, out.Model, out.Retries, atomic.LoadInt32(&calls), out.RoutingReason)
	t.Logf("output=%q", out.Output)
	if out.Retries != 1 {
		t.Fatalf("retries: got %d want 1", out.Retries)
	}
}

func TestRuntimeDoesNotRetryOfflineFailure(t *testing.T) {
	reg := NewRegistry("nx:test:nexus", testOrg(t), tool.NewToolRegistry())
	reg.Create(Definition{ID: "ag", Name: "a", BusinessID: "biz-1", Capabilities: []string{"research"}}, "t")
	modelReg := modelrouter.NewModelRegistry()
	router := modelrouter.NewModelRouter(modelReg, modelrouter.RoutingPolicyLocalFirst)
	router.RegisterProvider(modelrouter.NewLocalProvider(modelrouter.ProviderConfig{ID: "simulated", Status: modelrouter.ProviderStatusOffline}))
	_ = modelReg.RegisterModel(&modelrouter.ModelDefinition{
		ID: "simulated:default", ProviderID: "simulated",
		Capabilities: []modelrouter.ModelCapability{modelrouter.CapabilityReasoning},
		Runtime:      modelrouter.RuntimeLocal, Status: modelrouter.ModelStatusActive,
	})
	rt := NewRuntime(reg, tool.NewToolRegistry(), router, nil)
	spec := mustMarshalSpec(&Spec{RequiredCapabilities: []string{"research"}})
	req := &executor.WorkRequest{
		TaskID: "t-1", CorrelationID: "c-1", BusinessID: "biz-1", ActorID: "owner-1",
		Input: map[string]string{"agent_execution": spec},
	}
	out, err := rt.Handler()(context.Background(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "failed" || out.Retries != 0 {
		t.Fatalf("offline must fail without retries: %+v", out)
	}
}
