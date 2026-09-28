package governance

import (
	"testing"
	"time"
)

// scopeTestPolicy builds an active global-subject policy with D1 narrow
// scope ids set on the policy.
func scopeTestPolicy(id string, effect Outcome, prec int, set func(*Policy)) *Policy {
	p := condTestPolicy(id, effect, prec)
	if set != nil {
		set(p)
	}
	return p
}

// TEST-GOV-SCOPE-01 (D1): an agent-scoped policy applies only to requests
// carrying that agent id; unset or different request ids are excluded.
func TestScopeLevelAgentMatching(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	engine := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		scopeTestPolicy("pol-deny-agent", DENY, 20, func(p *Policy) {
			p.AgentID = "agent-X"
		}),
	}, fixedClock(now))

	req := condTestRequest("agent-1")
	req.AgentID = "agent-X"
	if d := engine.Evaluate(req); d.Outcome != DENY || d.MatchedPolicyID != "pol-deny-agent" {
		t.Errorf("agent-X: want DENY pol-deny-agent, got %v %q", d.Outcome, d.MatchedPolicyID)
	}
	req.AgentID = "agent-Y"
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("agent-Y: want ALLOW (agent policy excluded), got %v", d.Outcome)
	}
	req.AgentID = ""
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("unset agent: want ALLOW, got %v", d.Outcome)
	}
}

// TEST-GOV-SCOPE-02 (D1): workflow/task scoping plus ScopeLevelFor
// reporting on the decision (TASK > WORKFLOW > AGENT > ... > GLOBAL).
func TestScopeLevelWorkflowTaskMatching(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	engine := NewEngineWithClock([]*Policy{
		scopeTestPolicy("pol-allow-workflow", ALLOW, 10, func(p *Policy) {
			p.WorkflowID = "wf-1"
		}),
	}, fixedClock(now))

	req := condTestRequest("agent-1")
	req.WorkflowID = "wf-1"
	d := engine.Evaluate(req)
	if d.Outcome != ALLOW || d.MatchedPolicyID != "pol-allow-workflow" {
		t.Fatalf("wf-1: want ALLOW pol-allow-workflow, got %v %q", d.Outcome, d.MatchedPolicyID)
	}
	if d.ScopeLevel != ScopeLevelWorkflow {
		t.Errorf("scope level: want WORKFLOW, got %s", d.ScopeLevel)
	}
	req.WorkflowID = "wf-2"
	if d := engine.Evaluate(req); d.Outcome != DENY {
		t.Errorf("wf-2: want DENY (default, no policies), got %v", d.Outcome)
	}

	// A task pin outranks a workflow pin on the same policy (most specific
	// identifier set wins).
	p := scopeTestPolicy("pol-both", ALLOW, 10, func(p *Policy) {
		p.WorkflowID = "wf-1"
		p.TaskID = "task-9"
	})
	if p.ScopeLevelFor() != ScopeLevelTask {
		t.Errorf("task+workflow pin: want TASK, got %s", p.ScopeLevelFor())
	}
}

// TEST-GOV-SCOPE-03 (D1): when outcome and precedence tie, the narrower
// scope wins (SCHEMA_COMMON: default to the narrowest possible scope).
func TestScopeLevelNarrowestWinsTieBreak(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	engine := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-global", ALLOW, 10),
		scopeTestPolicy("pol-task", ALLOW, 10, func(p *Policy) {
			p.TaskID = "task-1"
		}),
	}, fixedClock(now))

	req := condTestRequest("agent-1")
	req.TaskID = "task-1"
	d := engine.Evaluate(req)
	if d.MatchedPolicyID != "pol-task" {
		t.Errorf("tie with task in scope: want narrower pol-task to win, got %q", d.MatchedPolicyID)
	}
	if d.ScopeLevel != ScopeLevelTask {
		t.Errorf("scope level: want TASK, got %s", d.ScopeLevel)
	}

	// Task policy does not apply → global one is the only candidate.
	req.TaskID = "task-2"
	d = engine.Evaluate(req)
	if d.MatchedPolicyID != "pol-global" {
		t.Errorf("task out of scope: want pol-global, got %q", d.MatchedPolicyID)
	}

	// Division narrows business at equal outcome/precedence.
	engine = NewEngineWithClock([]*Policy{
		scopeTestPolicy("pol-biz", ALLOW, 10, func(p *Policy) {
			p.BusinessID = "biz-1"
		}),
		scopeTestPolicy("pol-div", ALLOW, 10, func(p *Policy) {
			p.BusinessID = "biz-1"
			p.DivisionID = "div-1"
		}),
	}, fixedClock(now))
	req.BusinessID = "biz-1"
	req.DivisionID = "div-1"
	if d := engine.Evaluate(req); d.MatchedPolicyID != "pol-div" {
		t.Errorf("biz vs div tie: want narrower pol-div, got %q", d.MatchedPolicyID)
	}
}

// TEST-GOV-SCOPE-04 (D1): the condition grammar gains the narrow scope
// keys — scope= and attribute= conditions can pin agent/workflow/task.
func TestScopeLevelConditionKeys(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	engine := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-scope-cond", DENY, 20, Condition{
			ConditionID: "c-scope", ConditionType: "scope",
			Expression: "agent=agent-X,task=task-7",
		}),
	}, fixedClock(now))

	req := condTestRequest("agent-1")
	req.AgentID = "agent-X"
	req.TaskID = "task-7"
	if d := engine.Evaluate(req); d.Outcome != DENY {
		t.Errorf("scope condition match: want DENY, got %v", d.Outcome)
	}
	req.TaskID = "task-8"
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("scope condition task differs: want ALLOW, got %v", d.Outcome)
	}

	engine = NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-attr-cond", DENY, 20, Condition{
			ConditionID: "c-attr", ConditionType: "attribute",
			Expression: "workflow=wf-3",
		}),
	}, fixedClock(now))
	req.WorkflowID = "wf-3"
	if d := engine.Evaluate(req); d.Outcome != DENY {
		t.Errorf("attribute workflow match: want DENY, got %v", d.Outcome)
	}
	req.WorkflowID = "wf-4"
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("attribute workflow differs: want ALLOW, got %v", d.Outcome)
	}
}
