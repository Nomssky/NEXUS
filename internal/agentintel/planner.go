package agentintel

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Planner turns an objective into an advisory plan (§3). The runtime validates
// every plan before execution — planner output is never trusted.
type Planner interface {
	Plan(ctx context.Context, prompt PlanPrompt) ([]Step, error)
}

// ModelPlanner asks the routed model for a structured plan. The model
// proposes; the validator decides.
type ModelPlanner struct {
	Decider DecisionRequester
}

// Plan implements Planner.
func (p *ModelPlanner) Plan(ctx context.Context, prompt PlanPrompt) ([]Step, error) {
	if p == nil || p.Decider == nil {
		return nil, fmt.Errorf("planner: no decision model available")
	}
	raw, err := p.Decider.Plan(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("planner: %w", err)
	}
	steps, err := ParsePlan(raw)
	if err != nil {
		return nil, err
	}
	return steps, nil
}

// ScriptedPlanner is the deterministic planner used by contract tests and the
// seeded simulation provider: it derives steps from the objective's declared
// capabilities, never from prose semantics. It exists so planning is testable
// without a real model; it is not "intelligence".
type ScriptedPlanner struct {
	Steps []Step
}

// Plan implements Planner.
func (p *ScriptedPlanner) Plan(_ context.Context, prompt PlanPrompt) ([]Step, error) {
	if p == nil || len(p.Steps) == 0 {
		return nil, fmt.Errorf("planner: scripted planner has no steps")
	}
	out := make([]Step, len(p.Steps))
	copy(out, p.Steps)
	return out, nil
}

// buildDecisionMessages renders the decision prompt in the legacy line shape
// for the scripted simulation provider and contract tests. Production code
// uses buildDecisionEnvelope (protocol v2); the content contract is identical,
// only the message framing differs.
func buildDecisionMessages(p DecisionPrompt) []string {
	msgs := buildDecisionEnvelope(p)
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, strings.ToUpper(string(m.Role))+": "+m.Content)
	}
	return out
}

func kvString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// buildPlanMessages renders the planning prompt in the legacy line shape for
// the scripted simulation provider. Production code uses buildPlanEnvelope
// (protocol v2); the content contract is identical.
func buildPlanMessages(p PlanPrompt) []string {
	msgs := buildPlanEnvelope(p)
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, strings.ToUpper(string(m.Role))+": "+m.Content)
	}
	return out
}
