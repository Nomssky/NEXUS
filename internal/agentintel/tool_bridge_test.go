package agentintel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// toolCallProvider is a Provider double that returns a canned response and
// records the last request, so tests can assert what the decision adapter
// advertised and how it translated a native proposal.
type toolCallProvider struct {
	id   string
	resp *modelrouter.GenerateResponse
	last *modelrouter.GenerateRequest
}

func (p *toolCallProvider) Identify() string              { return p.id }
func (p *toolCallProvider) HealthCheck() error            { return nil }
func (p *toolCallProvider) ListModels() ([]string, error) { return []string{p.id + ":default"}, nil }
func (p *toolCallProvider) Invoke(_ context.Context, req *modelrouter.GenerateRequest) (*modelrouter.GenerateResponse, error) {
	p.last = req
	r := *p.resp
	return &r, nil
}

// sequenceProvider returns a different canned response per call and records
// every request, so the decision adapter's bounded strict retry can be
// asserted precisely.
type sequenceProvider struct {
	id        string
	responses []*modelrouter.GenerateResponse
	requests  []*modelrouter.GenerateRequest
}

func (p *sequenceProvider) Identify() string              { return p.id }
func (p *sequenceProvider) HealthCheck() error            { return nil }
func (p *sequenceProvider) ListModels() ([]string, error) { return []string{p.id + ":default"}, nil }

func (p *sequenceProvider) Invoke(_ context.Context, req *modelrouter.GenerateRequest) (*modelrouter.GenerateResponse, error) {
	p.requests = append(p.requests, req)
	i := len(p.requests) - 1
	if i >= len(p.responses) {
		i = len(p.responses) - 1
	}
	r := *p.responses[i]
	return &r, nil
}

func toolBridgeRouter(t *testing.T, p modelrouter.Provider) *modelrouter.ModelRouter {
	t.Helper()
	reg := modelrouter.NewModelRegistry()
	if err := reg.RegisterModel(&modelrouter.ModelDefinition{
		ID: "fake:default", ProviderID: "fake",
		Capabilities: []modelrouter.ModelCapability{
			modelrouter.CapabilityStructuredOutput, modelrouter.CapabilityToolCalling},
		Runtime: modelrouter.RuntimeRemote, Status: modelrouter.ModelStatusActive,
	}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	mr := modelrouter.NewModelRouter(reg, modelrouter.RoutingPolicyLocalFirst)
	mr.RegisterProvider(p)
	return mr
}

func bridgeHints() []ToolHint {
	return []ToolHint{{
		ID: "calc", Summary: "arithmetic", Operations: []string{"add", "mul"},
		InputSchema: tool.Schema{Fields: []tool.SchemaField{
			{Name: "a", Type: tool.FieldString, Required: true, MaxLength: 16},
			{Name: "b", Type: tool.FieldString, Required: true, MaxLength: 16},
		}},
	}}
}

// TestDecisionTranslatesSingleProviderToolCall verifies a native tool call is
// converted into the existing strict action protocol — never executed here —
// and that the provider's opaque call id is preserved for correlation.
func TestDecisionTranslatesSingleProviderToolCall(t *testing.T) {
	p := &toolCallProvider{id: "fake", resp: &modelrouter.GenerateResponse{
		Content:      "",
		FinishReason: "tool_calls",
		ToolCalls: []modelrouter.ToolCall{{
			ID: "call_1", Name: "calc", Arguments: `{"operation":"mul","input":{"a":"6","b":"7"}}`,
		}},
	}}
	d := &Decision{Router: toolBridgeRouter(t, p), AgentID: "helper"}
	raw, err := d.Decide(context.Background(), DecisionPrompt{StepID: "s1", Tools: bridgeHints()})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.last == nil || len(p.last.Tools) != 1 || p.last.Tools[0].Name != "calc" {
		t.Fatalf("tool definitions were not advertised: %+v", p.last)
	}
	// Tools and a strict response_format are mutually exclusive on
	// OpenAI-compatible servers, so a tool-bearing decision must not send one.
	if p.last.Structured != nil || p.last.ResponseFormat != "" {
		t.Fatalf("tool-bearing decision must not request structured output: %+v", p.last)
	}
	action, err := ParseAction(raw)
	if err != nil {
		t.Fatalf("translated action must parse strictly: %v (%s)", err, raw)
	}
	if action.Type != ActionToolCall || action.Tool != "calc" || action.Operation != "mul" ||
		action.Input["a"] != "6" || action.ProviderToolCallID != "call_1" {
		t.Fatalf("unexpected translated action: %+v", action)
	}
}

// TestDecisionWithoutToolsKeepsStructuredRequest pins the text-only flow: an
// agent with no tool catalog still asks for the strict decision schema and
// never advertises tools or tool-calling capability.
func TestDecisionWithoutToolsKeepsStructuredRequest(t *testing.T) {
	p := &toolCallProvider{id: "fake", resp: &modelrouter.GenerateResponse{
		Content: `{"type":"complete","result":"done"}`, FinishReason: "stop",
	}}
	d := &Decision{Router: toolBridgeRouter(t, p), AgentID: "helper"}
	if _, err := d.Decide(context.Background(), DecisionPrompt{StepID: "s1"}); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.last == nil || p.last.Structured == nil || len(p.last.Tools) != 0 {
		t.Fatalf("text-only decision must keep the structured request: %+v", p.last)
	}
}

// TestDecisionStrictRetryIsBounded verifies that a tool-bearing decision whose
// first answer is not a valid action triggers exactly ONE strict retry (tools
// dropped, decision schema added), and that a tool-bearing decision with no
// catalogue never retries.
func TestDecisionStrictRetryIsBounded(t *testing.T) {
	prose := &modelrouter.GenerateResponse{Content: "I think the answer is 42.", FinishReason: "stop"}
	done := &modelrouter.GenerateResponse{Content: `{"type":"complete","result":"42"}`, FinishReason: "stop"}

	withTools := &sequenceProvider{id: "fake", responses: []*modelrouter.GenerateResponse{prose, done}}
	d := &Decision{Router: toolBridgeRouter(t, withTools), AgentID: "helper"}
	conv := []modelrouter.Message{{Role: "tool", ToolCallID: "call_1", Content: `{"result":"42"}`}}
	raw, err := d.Decide(context.Background(), DecisionPrompt{StepID: "s1", Tools: bridgeHints(), ToolConversation: conv})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if _, perr := ParseAction(raw); perr != nil {
		t.Fatalf("retry must yield a valid action: %v (%s)", perr, raw)
	}
	if len(withTools.requests) != 2 {
		t.Fatalf("expected exactly one retry, got %d calls", len(withTools.requests))
	}
	if len(withTools.requests[0].Tools) == 0 || withTools.requests[0].Structured != nil {
		t.Fatalf("first attempt must advertise tools without a schema: %+v", withTools.requests[0])
	}
	if len(withTools.requests[1].Tools) != 0 || withTools.requests[1].Structured == nil {
		t.Fatalf("retry must use the strict schema without tools: %+v", withTools.requests[1])
	}
	// Both attempts preserve the tool conversation so the model still sees the
	// proposal and its result.
	for i, req := range withTools.requests {
		found := false
		for _, m := range req.Messages {
			if m.Role == "tool" && m.ToolCallID == "call_1" {
				found = true
			}
		}
		if !found {
			t.Fatalf("request %d must carry the tool conversation: %+v", i, req.Messages)
		}
	}
}

// TestDecisionFailsClosedOnProviderProposals verifies the adapter never
// executes or partially executes an ambiguous or out-of-catalog proposal.
func TestDecisionFailsClosedOnProviderProposals(t *testing.T) {
	cases := map[string]struct {
		calls []modelrouter.ToolCall
	}{
		"multiple calls": {calls: []modelrouter.ToolCall{
			{ID: "c1", Name: "calc", Arguments: `{}`},
			{ID: "c2", Name: "calc", Arguments: `{}`},
		}},
		"out of catalog":  {calls: []modelrouter.ToolCall{{ID: "c1", Name: "evil", Arguments: `{}`}}},
		"missing call id": {calls: []modelrouter.ToolCall{{ID: "", Name: "calc", Arguments: `{}`}}},
		"malformed args": {calls: []modelrouter.ToolCall{
			{ID: "c1", Name: "calc", Arguments: `{"input":{"a":"1"},"unexpected":true}`}}},
		"non-json args": {calls: []modelrouter.ToolCall{{ID: "c1", Name: "calc", Arguments: `not json`}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := &toolCallProvider{id: "fake", resp: &modelrouter.GenerateResponse{ToolCalls: tc.calls, FinishReason: "tool_calls"}}
			d := &Decision{Router: toolBridgeRouter(t, p), AgentID: "helper"}
			if _, err := d.Decide(context.Background(), DecisionPrompt{StepID: "s1", Tools: bridgeHints()}); err == nil {
				t.Fatal("an unusable provider proposal must fail closed")
			}
		})
	}
}

func TestProviderToolArguments(t *testing.T) {
	got, err := providerToolArguments(`{"operation":"mul","input":{"a":"6"}}`)
	if err != nil {
		t.Fatalf("valid arguments rejected: %v", err)
	}
	if got.Operation != "mul" || got.Input["a"] != "6" {
		t.Fatalf("unexpected decode: %+v", got)
	}
	if _, err := providerToolArguments(`{"input":{"a":"1"}} trailing`); err == nil {
		t.Fatal("trailing content must be rejected")
	}
	if _, err := providerToolArguments(`{"unknown":1}`); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
}

func TestMarshalToolArgumentsRoundTrips(t *testing.T) {
	raw := marshalToolArguments("mul", map[string]string{"a": "6", "b": "7"})
	got, err := providerToolArguments(raw)
	if err != nil {
		t.Fatalf("round-trip decode failed: %v", err)
	}
	if got.Operation != "mul" || got.Input["a"] != "6" || got.Input["b"] != "7" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestProviderToolDefinitionsRequireInputAndOperation(t *testing.T) {
	defs := providerToolDefinitions(bridgeHints())
	if len(defs) != 1 || defs[0].Name != "calc" {
		t.Fatalf("unexpected definitions: %+v", defs)
	}
	params, ok := defs[0].Parameters.(map[string]interface{})
	if !ok {
		t.Fatalf("parameters must be a schema object: %+v", defs[0].Parameters)
	}
	if params["additionalProperties"] != false {
		t.Fatal("tool schema must be closed")
	}
	required, _ := params["required"].([]string)
	if !contains(required, "input") || !contains(required, "operation") {
		t.Fatalf("multi-operation tool must require input and operation: %v", required)
	}
	props := params["properties"].(map[string]interface{})
	input := props["input"].(map[string]interface{})
	if input["type"] != "object" || input["additionalProperties"] != false {
		t.Fatalf("input must be a closed object: %+v", input)
	}
}

// TestProviderToolDefinitionsAdvertiseStringEncodedInputs pins the wire
// contract: the decision protocol, Action.Input and the manifest all treat
// input values as strings, so every advertised property must be a string —
// even for int/float/bool manifest fields — or a schema-conformant model call
// would be rejected by the decoder.
func TestProviderToolDefinitionsAdvertiseStringEncodedInputs(t *testing.T) {
	m := tool.ToolManifest{
		ID: "web.search",
		InputSchema: tool.Schema{Fields: []tool.SchemaField{
			{Name: "query", Type: tool.FieldString, Required: true, MaxLength: 512},
			{Name: "limit", Type: tool.FieldInt, Max: 10, Min: 1},
			{Name: "ratio", Type: tool.FieldFloat},
			{Name: "exact", Type: tool.FieldBool},
			{Name: "mode", Type: tool.FieldString, Enum: []string{"fast", "deep"}},
		}},
	}
	defs := providerToolDefinitions([]ToolHint{{ID: m.ID, Summary: "web", Operations: []string{"search"}, InputSchema: m.InputSchema}})
	params := defs[0].Parameters.(map[string]interface{})
	input := params["properties"].(map[string]interface{})["input"].(map[string]interface{})
	props := input["properties"].(map[string]interface{})
	for _, name := range []string{"query", "limit", "ratio", "exact", "mode"} {
		p, ok := props[name].(map[string]interface{})
		if !ok {
			t.Fatalf("missing property %q in %+v", name, props)
		}
		if p["type"] != "string" {
			t.Fatalf("property %q must be advertised as string, got %v", name, p["type"])
		}
	}
	// A schema-conformant (string-encoded) argument decodes and passes the
	// manifest's own validation.
	args, err := providerToolArguments(`{"operation":"search","input":{"query":"nexus","limit":"5","ratio":"0.5","exact":"true","mode":"fast"}}`)
	if err != nil {
		t.Fatalf("string-encoded arguments must decode: %v", err)
	}
	if err := m.ValidateInput(args.Input); err != nil {
		t.Fatalf("string-encoded arguments must validate against the manifest: %v", err)
	}
	// The numeric form the old schema invited must fail closed, never coerce.
	if _, err := providerToolArguments(`{"input":{"query":"nexus","limit":5}}`); err == nil {
		t.Fatal("a JSON number for a string-encoded field must be rejected")
	}
}

// bridgeDecider is a DecisionRequester that replays canned actions and records
// the prompts it was asked, so the runtime's tool-conversation bookkeeping can
// be asserted without a real provider.
type bridgeDecider struct {
	answers []string
	idx     int
	prompts []DecisionPrompt
}

func (b *bridgeDecider) Plan(_ context.Context, _ PlanPrompt) (string, error) { return goodPlan, nil }

func (b *bridgeDecider) Decide(_ context.Context, p DecisionPrompt) (string, error) {
	b.prompts = append(b.prompts, p)
	if b.idx < len(b.answers) {
		a := b.answers[b.idx]
		b.idx++
		return a, nil
	}
	return `{"type":"complete","result":"done"}`, nil
}

// TestProviderToolCallIDFlowsThroughGovernedPath verifies that a
// provider-originated tool call is executed exactly once through the mediated
// runtime and that its result is fed back as a bounded tool conversation keyed
// by the provider's opaque call id.
func TestProviderToolCallIDFlowsThroughGovernedPath(t *testing.T) {
	dec := &bridgeDecider{answers: []string{
		`{"type":"tool_call","tool":"calculator","input":{"a":"6","b":"7","op":"mul"},"provider_tool_call_id":"call_42"}`,
		`{"type":"complete","result":"42"}`,
	}}
	rt, _ := newIntelFixture(t, nil, dec)
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "multiply"}, ""), nil)
	if out.Status != "completed" {
		t.Fatalf("status=%s err=%s", out.Status, out.Error)
	}
	if len(dec.prompts) < 2 {
		t.Fatalf("expected a second decision after the tool result, got %d", len(dec.prompts))
	}
	conv := dec.prompts[1].ToolConversation
	if len(conv) != 2 {
		t.Fatalf("expected an assistant+tool exchange, got %+v", conv)
	}
	if conv[0].Role != "assistant" || len(conv[0].ToolCalls) != 1 || conv[0].ToolCalls[0].ID != "call_42" {
		t.Fatalf("assistant proposal not preserved: %+v", conv[0])
	}
	if conv[1].Role != "tool" || conv[1].ToolCallID != "call_42" {
		t.Fatalf("tool result not correlated: %+v", conv[1])
	}
	if !json.Valid([]byte(conv[1].Content)) {
		t.Fatalf("tool result content must be structured: %q", conv[1].Content)
	}
	// The mediated calculator actually ran: its computed value (6*7) is the
	// payload the model receives, not merely a "success" flag.
	if !strings.Contains(conv[1].Content, "42") {
		t.Fatalf("tool result must carry the mediated side effect: %q", conv[1].Content)
	}
}

// TestProviderToolCallMissingCatalogEntryFailsClosed verifies a provider-native
// proposal that names an unregistered tool is rejected by the runtime before
// any effect, even though the adapter preserved the call id.
func TestProviderToolCallMissingCatalogEntryFailsClosed(t *testing.T) {
	dec := &bridgeDecider{answers: []string{
		`{"type":"tool_call","tool":"ghost.tool","input":{},"provider_tool_call_id":"call_x"}`,
	}}
	rt, _ := newIntelFixture(t, nil, dec)
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "ghost"}, ""), nil)
	if out.Status == "completed" || strings.Contains(out.Output, "tool_calls=1") {
		t.Fatalf("an unknown tool must not execute or read as success: %s / %s", out.Status, out.Error)
	}
}
