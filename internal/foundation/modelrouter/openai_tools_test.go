package modelrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAIProviderSerializesToolDefinitions pins the wire shape of a
// tool-calling request: tools are advertised as OpenAI function definitions,
// and assistant/tool messages carry tool_calls and tool_call_id respectively.
// A tool-calling request is still a structured-output request, so both must
// coexist on the wire.
func TestOpenAIProviderSerializesToolDefinitions(t *testing.T) {
	var sawBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"type\":\"complete\"}"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer srv.Close()
	p := okProvider(t, srv)

	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"a": map[string]interface{}{"type": "string", "maxLength": 8},
		},
		"required":             []string{"a"},
		"additionalProperties": false,
	}
	req := &GenerateRequest{
		RequestID: "r1", ModelID: "gpt-test",
		Messages: []Message{
			{Role: "user", Content: "decide"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "calc", Arguments: `{"input":{"a":"1"}}`}}},
			{Role: "tool", ToolCallID: "call_1", Content: `{"result":"1"}`},
		},
		Tools:      []ToolDefinition{{Name: "calc", Description: "arithmetic", Parameters: params}},
		Structured: &StructuredOutputSchema{Name: "agent_decision_v1", Schema: []byte(`{"type":"object"}`), Strict: true},
	}
	if _, err := p.Invoke(context.Background(), req); err != nil {
		t.Fatalf("invoke: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(sawBody, &body); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	tools, ok := body["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools not serialized: %s", sawBody)
	}
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" {
		t.Fatalf("tool type must be function: %s", sawBody)
	}
	fn := tool["function"].(map[string]interface{})
	if fn["name"] != "calc" || fn["description"] != "arithmetic" {
		t.Fatalf("tool function not serialized: %s", sawBody)
	}
	if _, ok := fn["parameters"].(map[string]interface{}); !ok {
		t.Fatalf("tool parameters not serialized: %s", sawBody)
	}
	if _, ok := body["response_format"]; !ok {
		t.Fatalf("structured output must coexist with tools: %s", sawBody)
	}

	msgs := body["messages"].([]interface{})
	assistant := msgs[1].(map[string]interface{})
	calls, ok := assistant["tool_calls"].([]interface{})
	if !ok || len(calls) != 1 {
		t.Fatalf("assistant tool_calls not serialized: %s", sawBody)
	}
	call := calls[0].(map[string]interface{})
	if call["id"] != "call_1" || call["type"] != "function" {
		t.Fatalf("assistant tool call shape wrong: %s", sawBody)
	}
	if call["function"].(map[string]interface{})["name"] != "calc" {
		t.Fatalf("assistant tool call name wrong: %s", sawBody)
	}
	toolMsg := msgs[2].(map[string]interface{})
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" {
		t.Fatalf("tool result message shape wrong: %s", sawBody)
	}
}

// TestOpenAIProviderNormalizesToolCalls verifies a provider tool call is
// surfaced as untrusted data (id/name/arguments), never executed here.
func TestOpenAIProviderNormalizesToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_9","type":"function","function":{"name":"calc","arguments":"{\"input\":{\"a\":\"2\"}}"}}]},"finish_reason":"tool_calls"}],"usage":{}}`)
	}))
	defer srv.Close()
	p := okProvider(t, srv)
	resp, err := p.Invoke(context.Background(), &GenerateRequest{
		RequestID: "r1", ModelID: "gpt-test",
		Messages: []Message{{Role: "user", Content: "decide"}},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %+v", resp.ToolCalls)
	}
	got := resp.ToolCalls[0]
	if got.ID != "call_9" || got.Name != "calc" || got.Arguments != `{"input":{"a":"2"}}` {
		t.Fatalf("tool call not normalized: %+v", got)
	}
}

// TestOpenAIProviderRejectsIncompleteToolCall fails closed when the provider
// returns a truncated tool call: a missing id, name or arguments is a
// malformed output, never a silently-ignored proposal.
func TestOpenAIProviderRejectsIncompleteToolCall(t *testing.T) {
	cases := map[string]string{
		"missing id":        `{"choices":[{"message":{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"calc","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		"missing name":      `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		"missing arguments": `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"calc"}}]},"finish_reason":"tool_calls"}]}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, payload)
			}))
			defer srv.Close()
			p := okProvider(t, srv)
			if _, err := p.Invoke(context.Background(), &GenerateRequest{RequestID: "r1", ModelID: "m", Messages: []Message{{Role: "user", Content: "x"}}}); err == nil {
				t.Fatal("an incomplete tool call must fail the invocation")
			}
		})
	}
}

// TestOpenAIProviderRejectsInvalidToolDefinition refuses to send a tool with
// no name or an unserializable schema, before any network call.
func TestOpenAIProviderRejectsInvalidToolDefinition(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"}}]}`)
	}))
	defer srv.Close()
	p := okProvider(t, srv)

	if _, err := p.Invoke(context.Background(), &GenerateRequest{
		RequestID: "r1", ModelID: "m", Messages: []Message{{Role: "user", Content: "x"}},
		Tools: []ToolDefinition{{Name: "   ", Parameters: map[string]interface{}{"type": "object"}}},
	}); err == nil {
		t.Fatal("an unnamed tool must be rejected")
	}
	if calls != 0 {
		t.Fatalf("invalid tool definition must not reach the network, calls=%d", calls)
	}
}
