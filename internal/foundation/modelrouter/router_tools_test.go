package modelrouter

import (
	"context"
	"testing"
)

// TestRouterRefusesToolRequestWithoutToolCallingModel fails closed: a request
// that advertises tool definitions is never sent to a model that does not
// declare tool calling, and the provider is not invoked at all.
func TestRouterRefusesToolRequestWithoutToolCallingModel(t *testing.T) {
	reg := NewModelRegistry()
	if err := reg.RegisterModel(&ModelDefinition{
		ID: "a", ProviderID: "spy-a",
		Capabilities: []ModelCapability{CapabilityReasoning, CapabilityStructuredOutput},
		Runtime:      RuntimeRemote, Status: ModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	router := NewModelRouter(reg, RoutingPolicyNearest)
	spy := &spyProvider{id: "spy-a"}
	router.RegisterProvider(spy)

	_, _, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID: "req-tools", RequiredCaps: []ModelCapability{CapabilityStructuredOutput},
	}, &GenerateRequest{
		RequestID: "req-tools",
		Tools:     []ToolDefinition{{Name: "calc", Parameters: map[string]interface{}{"type": "object"}}},
	})
	if err == nil {
		t.Fatal("a tool request must not reach a model without tool-calling capability")
	}
	if spy.lastModel != "" {
		t.Fatalf("provider must not be invoked, got %q", spy.lastModel)
	}
}

// TestRouterReselectsToolCallingModel verifies the router reselects a model
// that advertises CapabilityToolCalling when tools are present, even if the
// caller only required structured output.
func TestRouterReselectsToolCallingModel(t *testing.T) {
	reg := NewModelRegistry()
	for _, d := range []*ModelDefinition{
		{ID: "a", ProviderID: "spy-a", Capabilities: []ModelCapability{CapabilityReasoning, CapabilityStructuredOutput},
			Runtime: RuntimeRemote, Status: ModelStatusActive},
		{ID: "b", ProviderID: "spy-b", Capabilities: []ModelCapability{CapabilityReasoning, CapabilityStructuredOutput, CapabilityToolCalling},
			Runtime: RuntimeRemote, Status: ModelStatusActive},
	} {
		if err := reg.RegisterModel(d); err != nil {
			t.Fatal(err)
		}
	}
	router := NewModelRouter(reg, RoutingPolicyNearest)
	spyA := &spyProvider{id: "spy-a"}
	spyB := &spyProvider{id: "spy-b"}
	router.RegisterProvider(spyA)
	router.RegisterProvider(spyB)

	_, decision, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID: "req-tools", RequiredCaps: []ModelCapability{CapabilityReasoning},
		AllowedModels: []string{"a", "b"},
	}, &GenerateRequest{
		RequestID: "req-tools",
		Tools:     []ToolDefinition{{Name: "calc", Parameters: map[string]interface{}{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if decision.ModelID != "b" {
		t.Fatalf("a tool request must reach the tool-calling model, got %q", decision.ModelID)
	}
	if spyB.lastModel != "b" {
		t.Fatalf("tool-calling provider was not invoked: %q", spyB.lastModel)
	}
}
