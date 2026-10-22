package agentintel

import (
	"strings"
	"testing"
)

type planTestScope struct {
	tools   map[string][]string
	agents  map[string]map[string]bool
	visible map[string]bool
}

func (s *planTestScope) AgentVisible(agentID, _, _ string) bool { return s.visible[agentID] }

func (s *planTestScope) ToolRegistered(toolID string) bool {
	_, ok := s.tools[toolID]
	return ok
}

func (s *planTestScope) AgentAllowsTool(agentID, toolID string) bool {
	return s.agents[agentID][toolID]
}

func (s *planTestScope) ToolSupportsOperation(toolID, operation string) bool {
	for _, declared := range s.tools[toolID] {
		if declared == operation {
			return true
		}
	}
	return false
}

func operationScope() *planTestScope {
	return &planTestScope{
		tools:   map[string][]string{"web.search": {"search"}, "http.request": {"get", "post"}},
		agents:  map[string]map[string]bool{"researcher": {"web.search": true, "http.request": true}},
		visible: map[string]bool{"researcher": true},
	}
}

func TestValidatePlanAllowedOperations(t *testing.T) {
	scope := operationScope()
	caps := DefaultCaps()
	caps.MaxSteps = 4

	valid := &Plan{Steps: []Step{{
		StepID: "s1", Intent: "search", PreferredAgent: "researcher",
		AllowedTools:      []string{"web.search"},
		AllowedOperations: []ToolOperation{{Tool: "web.search", Operation: "search"}},
	}}}
	if res := ValidatePlan(valid, scope, caps); !res.Valid {
		t.Fatalf("valid operations rejected: %+v", res.Rejections)
	}

	cases := map[string]Step{
		"unknown operation": {
			StepID: "s1", Intent: "search", AllowedTools: []string{"web.search"},
			AllowedOperations: []ToolOperation{{Tool: "web.search", Operation: "delete"}},
		},
		"operation not in allowed_tools": {
			StepID: "s1", Intent: "search", AllowedTools: []string{"http.request"},
			AllowedOperations: []ToolOperation{{Tool: "web.search", Operation: "search"}},
		},
		"duplicate operation": {
			StepID: "s1", Intent: "search", AllowedTools: []string{"web.search"},
			AllowedOperations: []ToolOperation{
				{Tool: "web.search", Operation: "search"},
				{Tool: "web.search", Operation: "search"},
			},
		},
		"malformed operation": {
			StepID: "s1", Intent: "search",
			AllowedOperations: []ToolOperation{{Tool: "web.search", Operation: "search\npost"}},
		},
		"malformed step id": {
			StepID: "s 1", Intent: "search",
		},
	}
	for name, step := range cases {
		t.Run(name, func(t *testing.T) {
			res := ValidatePlan(&Plan{Steps: []Step{step}}, scope, caps)
			if res.Valid {
				t.Fatalf("%s must be rejected", name)
			}
		})
	}
}

func TestPlanPromptCarriesToolsBoundsAndRuntimeFacts(t *testing.T) {
	msgs := buildPlanEnvelope(PlanPrompt{
		Objective:   "research",
		Tools:       []ToolHint{{ID: "web.search", Operations: []string{"search"}, SideEffect: "read", Summary: "search"}},
		MaxSteps:    3,
		PriorSteps:  []Step{{StepID: "s1", Intent: "gather", AllowedTools: []string{"web.search"}}},
		PlanVersion: 1,
		StepStates:  "[s1=completed]",
		Reason:      "need another source",
	})
	if len(msgs) != 2 {
		t.Fatalf("expected two messages, got %d", len(msgs))
	}
	content := msgs[1].Content
	for _, want := range []string{"TOOLS", "web.search", "MAX_STEPS: 3", "ACTIVE_PLAN: version=1", "STEP_STATES", "s1=completed", "REPLAN_REASON", "allowed_operations"} {
		if !strings.Contains(content, want) {
			t.Fatalf("plan prompt missing %q:\n%s", want, content)
		}
	}
}
