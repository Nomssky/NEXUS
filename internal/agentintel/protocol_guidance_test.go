package agentintel

import (
	"strings"
	"testing"
)

// TestPlanEnvelopeGuidesPreferredAgentOmission pins the planner-boundary
// information asymmetry found during the live local-model acceptance: the plan
// contract validates `preferred_agent` against the agents actually visible in
// scope (and rejects an invented id), while the planner prompt advertised the
// field without ever listing any agent id. A local model then invented
// `preferred_agent:"Nexus"` and the whole real run failed.
//
// The runtime must tell the model to omit a field it cannot be informed about,
// and must keep every validation rule intact: an invented id is still rejected
// (see the "unknown agent" case in agentintel_test.go).
func TestPlanEnvelopeGuidesPreferredAgentOmission(t *testing.T) {
	msgs := buildPlanEnvelope(PlanPrompt{
		Objective: "search the web for a topic",
		MaxSteps:  3,
		Tools:     []ToolHint{{ID: "web.search", Summary: "search"}},
	})
	if len(msgs) == 0 {
		t.Fatal("plan envelope produced no messages")
	}
	user := msgs[len(msgs)-1].Content

	for _, want := range []string{
		"preferred_agent",
		"omit preferred_agent",
		"invented id rejects the whole plan",
	} {
		if !strings.Contains(user, want) {
			t.Fatalf("plan envelope must guide preferred_agent (%q):\n%s", want, user)
		}
	}
	// The pre-existing tool guidance must not be lost by the addition.
	if !strings.Contains(user, "Only use tool ids listed in TOOLS") {
		t.Fatalf("existing TOOLS guidance disappeared:\n%s", user)
	}
	// The prompt must not fabricate a catalog it does not have.
	if strings.Contains(user, "AGENTS:") {
		t.Fatalf("no agent catalog is rendered, so no AGENTS line may be promised:\n%s", user)
	}
}

// TestDecisionEnvelopeStatesToolCallRequiredPairing pins the decision-boundary
// analogy of the same asymmetry found by the live local-model acceptance: the
// contract rejects a `tool_call` action whose `tool` is empty (ValidateAction
// "tool_call needs a tool id"), while the envelope only listed the fields and
// never stated the requirement. Local models intermittently produced a
// text-form tool_call without a tool id and the real run failed closed.
//
// The fix is information-only: the pairing requirement is stated. Validation
// itself is untouched — an empty tool is still rejected, never guessed from a
// prompt or operation field.
func TestDecisionEnvelopeStatesToolCallRequiredPairing(t *testing.T) {
	msgs := buildDecisionEnvelope(DecisionPrompt{
		Objective: "search the web",
		Tools:     []ToolHint{{ID: "web.search", Summary: "search"}},
	})
	if len(msgs) == 0 {
		t.Fatal("decision envelope produced no messages")
	}
	user := msgs[len(msgs)-1].Content

	if !strings.Contains(user, "tool_call action must set tool") {
		t.Fatalf("decision envelope must state the tool_call/tool pairing:\n%s", user)
	}
	if !strings.Contains(user, "empty or absent is rejected") {
		t.Fatalf("decision envelope must state that an empty tool id is rejected:\n%s", user)
	}
	// And the constructive form of the same class of fix: the field name the
	// model kept inventing (`action`) is explicitly named as not existing.
	if !strings.Contains(user, "there is no `action` field") {
		t.Fatalf("decision envelope must state that `action` is not a field:\n%s", user)
	}
	// Pre-existing guidance must survive: unknown-field rejection and catalog.
	if !strings.Contains(user, "Any field not listed here") {
		t.Fatalf("unknown-field guidance disappeared:\n%s", user)
	}
	// The catalog itself must still be rendered for the pairing to be usable.
	if !strings.Contains(user, "web.search") {
		t.Fatalf("TOOLS catalog missing:\n%s", user)
	}
	// The catalog prints the MANIFEST type (int), so the envelope must state
	// the string wire contract — the model otherwise emits JSON numbers and the
	// strict decoder rejects them (observed live: "cannot unmarshal number into
	// Action.input.limit").
	if !strings.Contains(user, "input values are string-encoded") {
		t.Fatalf("decision envelope must state the string input contract:\n%s", user)
	}
	// Structured claims live only inside synthesis.
	if !strings.Contains(user, "exists only inside `synthesis`") {
		t.Fatalf("decision envelope must state that evidence_backed is synthesis-only:\n%s", user)
	}
}

// TestPlanEnvelopeStatesStringEncodedInputs is the plan-side half of the same
// fix: the plan catalog also renders manifest types and must state the wire
// contract before a planner proposes numeric literals.
func TestPlanEnvelopeStatesStringEncodedInputs(t *testing.T) {
	msgs := buildPlanEnvelope(PlanPrompt{
		Objective: "search the web",
		MaxSteps:  3,
		Tools: []ToolHint{{
			ID: "web.search", Summary: "search", Operations: []string{"search"},
			InputShape: "query:string*,limit:int",
		}},
	})
	if len(msgs) == 0 {
		t.Fatal("plan envelope produced no messages")
	}
	user := msgs[len(msgs)-1].Content
	if !strings.Contains(user, "input values are string-encoded") {
		t.Fatalf("plan envelope must state the string input contract:\n%s", user)
	}
	if !strings.Contains(user, "limit:int") {
		t.Fatalf("the manifest type must still be rendered:\n%s", user)
	}
}

// TestNumericDecisionInputStillRejected keeps the guard that the prompt fix
// only corrects the INFORMATION asymmetry: a JSON number for a string-encoded
// field is still a protocol rejection, never silently coerced.
func TestNumericDecisionInputStillRejected(t *testing.T) {
	if _, err := ParseAction(`{"type":"tool_call","tool":"web.search","input":{"limit":5}}`); err == nil {
		t.Fatal("a JSON number for a string-encoded input must still be rejected")
	} else if !strings.Contains(err.Error(), "cannot unmarshal number") {
		t.Fatalf("expected a type error, got: %v", err)
	}
}
