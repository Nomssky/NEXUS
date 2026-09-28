package governance

import (
	"strings"
	"testing"
	"time"
)

// condTestPolicy builds an active global policy with the given conditions.
func condTestPolicy(id string, effect Outcome, prec int, conds ...Condition) *Policy {
	return &Policy{
		PolicyID:      id,
		PolicyVersion: "1.0",
		Status:        PolicyStatusActive,
		Subject:       Subject{SubjectType: "all"},
		Action:        Action{ActionType: "custom"},
		Resource:      Resource{ResourceType: "all"},
		Effect:        effect,
		Precedence:    prec,
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:     "admin",
		Conditions:    conds,
	}
}

func condTestRequest(actor string) Request {
	return Request{
		Actor:        actor,
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
	}
}

func assertFailSafeDeny(t *testing.T, d Decision) {
	t.Helper()
	if d.Outcome != DENY {
		t.Fatalf("want fail-safe DENY, got %v (reason %q)", d.Outcome, d.Reason)
	}
	if !strings.Contains(d.Reason, "condition evaluation failed") {
		t.Fatalf("want fail-safe reason, got %q", d.Reason)
	}
}

// TEST-GOV-COND-01: time condition gates policy applicability.
func TestConditionTimeWindow(t *testing.T) {
	// Monday 2026-09-28.
	monday := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	now := monday
	clock := func() time.Time { return now }

	// In-window: the conditional DENY applies and overrides the default
	// ALLOW; out-of-window it is excluded and the default ALLOW wins.
	engine := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-hours", DENY, 20, Condition{
			ConditionID: "c-hours", ConditionType: "time",
			Expression: "start=09:00,end=17:00",
		}),
	}, clock)
	req := condTestRequest("agent-1")

	now = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	if d := engine.Evaluate(req); d.Outcome != DENY || d.MatchedPolicyID != "pol-deny-hours" {
		t.Errorf("in-window: want DENY pol-deny-hours, got %v %q", d.Outcome, d.MatchedPolicyID)
	}
	now = time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("out-of-window: want ALLOW (deny excluded), got %v (%q)", d.Outcome, d.Reason)
	}

	// Wrap past midnight: 22:00–06:00.
	wrap := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-hours", DENY, 20, Condition{
			ConditionID: "c-hours", ConditionType: "time",
			Expression: "start=22:00,end=06:00",
		}),
	}, clock)
	now = time.Date(2026, 9, 28, 23, 30, 0, 0, time.UTC)
	if d := wrap.Evaluate(req); d.Outcome != DENY {
		t.Errorf("wrap 23:30: want DENY, got %v", d.Outcome)
	}
	now = time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)
	if d := wrap.Evaluate(req); d.Outcome != DENY {
		t.Errorf("wrap 05:00: want DENY, got %v", d.Outcome)
	}
	now = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	if d := wrap.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("wrap 12:00: want ALLOW, got %v", d.Outcome)
	}

	// days= filter: window applies only on the listed weekdays.
	days := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-hours", DENY, 20, Condition{
			ConditionID: "c-hours", ConditionType: "time",
			Expression: "start=00:00,end=23:59,days=sun",
		}),
	}, clock)
	now = monday // Monday
	if d := days.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("monday vs days=sun: want ALLOW, got %v", d.Outcome)
	}
	now = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) // Sunday
	if d := days.Evaluate(req); d.Outcome != DENY {
		t.Errorf("sunday vs days=sun: want DENY, got %v", d.Outcome)
	}
}

// TEST-GOV-COND-02: scope condition gates on request business/division.
func TestConditionScope(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	engine := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-biz", DENY, 20, Condition{
			ConditionID: "c-scope", ConditionType: "scope",
			Expression: "business=biz-1",
		}),
	}, fixedClock(now))
	req := condTestRequest("agent-1")

	req.BusinessID = "biz-1"
	if d := engine.Evaluate(req); d.Outcome != DENY || d.MatchedPolicyID != "pol-deny-biz" {
		t.Errorf("biz-1: want DENY pol-deny-biz, got %v %q", d.Outcome, d.MatchedPolicyID)
	}
	req.BusinessID = "biz-2"
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("biz-2: want ALLOW, got %v", d.Outcome)
	}
	req.BusinessID = ""
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("unset business: want ALLOW, got %v", d.Outcome)
	}
	req.BusinessID = "biz-1"
	req.DivisionID = "div-9"
	engine = NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-biz-div", DENY, 20, Condition{
			ConditionID: "c-scope", ConditionType: "scope",
			Expression: "business=biz-1,division=div-1",
		}),
	}, fixedClock(now))
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("business matches but division differs: want ALLOW, got %v", d.Outcome)
	}
	req.DivisionID = "div-1"
	if d := engine.Evaluate(req); d.Outcome != DENY {
		t.Errorf("business and division both match: want DENY, got %v", d.Outcome)
	}
}

// TEST-GOV-COND-03: attribute condition, including negate and fail-safe on
// unknown attribute keys.
func TestConditionAttribute(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

	engine := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-alice", DENY, 20, Condition{
			ConditionID: "c-actor", ConditionType: "attribute",
			Expression: "actor=alice",
		}),
	}, fixedClock(now))
	if d := engine.Evaluate(condTestRequest("alice")); d.Outcome != DENY {
		t.Errorf("actor=alice: want DENY, got %v", d.Outcome)
	}
	if d := engine.Evaluate(condTestRequest("bob")); d.Outcome != ALLOW {
		t.Errorf("actor=bob: want ALLOW, got %v", d.Outcome)
	}

	// negate inverts the raw result: deny everyone EXCEPT alice.
	engine = NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-not-alice", DENY, 20, Condition{
			ConditionID: "c-actor", ConditionType: "attribute",
			Expression: "actor=alice", Negate: true,
		}),
	}, fixedClock(now))
	if d := engine.Evaluate(condTestRequest("alice")); d.Outcome != ALLOW {
		t.Errorf("negate alice: want ALLOW (condition false), got %v", d.Outcome)
	}
	if d := engine.Evaluate(condTestRequest("bob")); d.Outcome != DENY {
		t.Errorf("negate bob: want DENY (condition negates true), got %v", d.Outcome)
	}

	// risk comparison is case-insensitive.
	engine = NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-high", DENY, 20, Condition{
			ConditionID: "c-risk", ConditionType: "attribute",
			Expression: "risk=high",
		}),
	}, fixedClock(now))
	req := condTestRequest("agent-1")
	req.RiskLevel = RiskLevel("HIGH")
	if d := engine.Evaluate(req); d.Outcome != DENY {
		t.Errorf("risk HIGH vs =high: want DENY, got %v", d.Outcome)
	}

	// Unknown attribute key is an evaluation error → fail-safe DENY, not a
	// silently-excluded policy (which would have left the default ALLOW).
	engine = NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-allow-bad-key", ALLOW, 20, Condition{
			ConditionID: "c-bad", ConditionType: "attribute",
			Expression: "frobnicate=x",
		}),
	}, fixedClock(now))
	assertFailSafeDeny(t, engine.Evaluate(condTestRequest("agent-1")))
}

// TEST-GOV-COND-04: malformed expressions fail safe to DENY.
func TestConditionMalformedFailSafe(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		cond Condition
	}{
		{"bad time", Condition{ConditionID: "c1", ConditionType: "time", Expression: "start=9am,end=17:00"}},
		{"time missing end", Condition{ConditionID: "c1", ConditionType: "time", Expression: "start=09:00"}},
		{"scope empty", Condition{ConditionID: "c1", ConditionType: "scope", Expression: "nonsense=1"}},
		{"empty expression", Condition{ConditionID: "c1", ConditionType: "time", Expression: ""}},
		{"unknown type", Condition{ConditionID: "c1", ConditionType: "moon-phase", Expression: "x=1"}},
		{"count missing max", Condition{ConditionID: "c1", ConditionType: "count", Expression: "action=execute_tool"}},
		{"bad day", Condition{ConditionID: "c1", ConditionType: "time", Expression: "start=00:00,end=23:59,days=caturday"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The extra ALLOW policy would win precedence if the bad
			// condition were silently skipped; fail-safe must DENY instead.
			engine := NewEngineWithClock([]*Policy{
				condTestPolicy("pol-default-allow", ALLOW, 10),
				condTestPolicy("pol-allow-conditional", ALLOW, 20, tc.cond),
			}, fixedClock(now))
			assertFailSafeDeny(t, engine.Evaluate(condTestRequest("agent-1")))
		})
	}
}

// TEST-GOV-COND-05: count condition triggers once the windowed evaluation
// count reaches max, and slides with the window.
func TestConditionCount(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	engine := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-after-2", DENY, 20, Condition{
			ConditionID: "c-count", ConditionType: "count",
			Expression: "action=execute_tool,max=2",
		}),
	}, clock)
	req := condTestRequest("agent-1")

	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("eval 1: want ALLOW (count=1 < 2), got %v", d.Outcome)
	}
	if d := engine.Evaluate(req); d.Outcome != DENY {
		t.Errorf("eval 2: want DENY (count=2 >= 2), got %v", d.Outcome)
	}
	if d := engine.Evaluate(req); d.Outcome != DENY {
		t.Errorf("eval 3: want DENY (count=3 >= 2), got %v", d.Outcome)
	}

	// Sliding window: history older than window_seconds drops out.
	now = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	engine = NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-rate", DENY, 20, Condition{
			ConditionID: "c-rate", ConditionType: "count",
			Expression: "action=execute_tool,max=2,window_seconds=60",
		}),
	}, clock)
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("rate eval 1: want ALLOW, got %v", d.Outcome)
	}
	now = now.Add(10 * time.Second)
	if d := engine.Evaluate(req); d.Outcome != DENY {
		t.Errorf("rate eval 2 (+10s): want DENY (2 in window), got %v", d.Outcome)
	}
	now = now.Add(2 * time.Minute)
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("rate eval 3 (+120s): want ALLOW (window slid), got %v", d.Outcome)
	}
}

// TEST-GOV-COND-06: composite combines sibling conditions (all/any), and
// referenced operands are not additionally required by the policy AND.
func TestConditionComposite(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

	// any(a,b): true when either operand holds.
	engine := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-any", DENY, 20,
			Condition{ConditionID: "c-a", ConditionType: "attribute", Expression: "actor=alice"},
			Condition{ConditionID: "c-b", ConditionType: "attribute", Expression: "resource=tool-1"},
			Condition{ConditionID: "c-any", ConditionType: "composite", Expression: "any:c-a,c-b"},
		),
	}, fixedClock(now))
	// actor=bob (c-a false) but resource=tool-1 (c-b true) → any holds.
	// If the operand were also required by the policy AND, c-a would kill
	// the policy and this would wrongly ALLOW.
	if d := engine.Evaluate(condTestRequest("bob")); d.Outcome != DENY {
		t.Errorf("any(c-a=false,c-b=true): want DENY, got %v", d.Outcome)
	}
	req := condTestRequest("bob")
	req.Resource = "tool-9"
	if d := engine.Evaluate(req); d.Outcome != ALLOW {
		t.Errorf("any(both false): want ALLOW, got %v", d.Outcome)
	}

	// all(a,b): requires both operands.
	engine = NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-all", DENY, 20,
			Condition{ConditionID: "c-a", ConditionType: "attribute", Expression: "actor=alice"},
			Condition{ConditionID: "c-b", ConditionType: "attribute", Expression: "resource=tool-1"},
			Condition{ConditionID: "c-all", ConditionType: "composite", Expression: "all:c-a,c-b"},
		),
	}, fixedClock(now))
	if d := engine.Evaluate(condTestRequest("bob")); d.Outcome != ALLOW {
		t.Errorf("all(a=false): want ALLOW, got %v", d.Outcome)
	}
	if d := engine.Evaluate(condTestRequest("alice")); d.Outcome != DENY {
		t.Errorf("all(both true): want DENY, got %v", d.Outcome)
	}

	// negate applies to the composite result.
	engine = NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-deny-negated", DENY, 20,
			Condition{ConditionID: "c-a", ConditionType: "attribute", Expression: "actor=alice"},
			Condition{ConditionID: "c-not", ConditionType: "composite", Expression: "any:c-a", Negate: true},
		),
	}, fixedClock(now))
	if d := engine.Evaluate(condTestRequest("alice")); d.Outcome != ALLOW {
		t.Errorf("negated composite true→false: want ALLOW, got %v", d.Outcome)
	}
	if d := engine.Evaluate(condTestRequest("bob")); d.Outcome != DENY {
		t.Errorf("negated composite false→true: want DENY, got %v", d.Outcome)
	}
}

// TEST-GOV-COND-07: composite cycles, unknown references and duplicate ids
// are evaluation errors → fail-safe DENY (a cycle must never let the policy
// slip through un-evaluated).
func TestConditionCompositeGraphFailSafe(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		cond Condition
	}{
		{"self cycle", Condition{ConditionID: "c1", ConditionType: "composite", Expression: "all:c1"}},
		{"unknown reference", Condition{ConditionID: "c1", ConditionType: "composite", Expression: "all:missing"}},
		{"bad composite kind", Condition{ConditionID: "c1", ConditionType: "composite", Expression: "both:c-a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := NewEngineWithClock([]*Policy{
				condTestPolicy("pol-default-allow", ALLOW, 10),
				condTestPolicy("pol-allow-conditional", ALLOW, 20, tc.cond),
			}, fixedClock(now))
			assertFailSafeDeny(t, engine.Evaluate(condTestRequest("agent-1")))
		})
	}

	// Mutually-referencing composites: both are operands of each other, so
	// without a static cycle check neither would ever be evaluated.
	engine := NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-allow-conditional", ALLOW, 20,
			Condition{ConditionID: "c1", ConditionType: "composite", Expression: "all:c2"},
			Condition{ConditionID: "c2", ConditionType: "composite", Expression: "all:c1"},
		),
	}, fixedClock(now))
	assertFailSafeDeny(t, engine.Evaluate(condTestRequest("agent-1")))

	// Duplicate condition_id within one policy.
	engine = NewEngineWithClock([]*Policy{
		condTestPolicy("pol-default-allow", ALLOW, 10),
		condTestPolicy("pol-allow-conditional", ALLOW, 20,
			Condition{ConditionID: "c1", ConditionType: "attribute", Expression: "actor=alice"},
			Condition{ConditionID: "c1", ConditionType: "attribute", Expression: "actor=bob"},
		),
	}, fixedClock(now))
	assertFailSafeDeny(t, engine.Evaluate(condTestRequest("agent-1")))
}
