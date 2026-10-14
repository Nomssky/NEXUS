package agentexec

import (
	"context"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// newPrePlatformRuntime builds a governed runtime without a capability
// platform so InvokeToolScoped takes the pre-platform (internal) path.
func newPrePlatformRuntime(t *testing.T, bus *event.MemBus, rec *eventRecorder) *Runtime {
	t.Helper()
	tools := tool.NewToolRegistry()
	registerBuiltins(tools)
	members := identity.NewMembershipSet()
	if err := members.Add(identity.Membership{IdentityID: "actor-1", BusinessID: "biz-1",
		Role: identity.RoleMember, Status: identity.StatusActive}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	policies := []*governance.Policy{{
		SchemaVersion: "1.0.0", EntityType: "policy", PolicyID: governance.DefaultAllowPolicyID,
		PolicyVersion: "1", PolicyType: governance.PolicyTypeAccessControl,
		Name: "allow", Status: governance.PolicyStatusActive,
		Subject:  governance.Subject{SubjectType: "all"},
		Action:   governance.Action{ActionType: "custom"},
		Resource: governance.Resource{ResourceType: "all"},
		Effect:   governance.ALLOW, EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
	}}
	host := &admissionHost{approved: map[string]bool{}, pending: map[string]string{}}
	rt := &Runtime{
		Registry:   nil,
		Tools:      tools,
		ToolExec:   BuiltinExecutor(),
		Controller: control.New(control.Options{Engine: governance.NewEngine(policies), Approver: host, Escalator: host}),
		now:        time.Now, maxDepth: 3, maxFan: 4, maxConcurrent: 4,
		Bus: bus,
	}
	return rt
}

func subscribeEvidence(t *testing.T, rec *eventRecorder, bus *event.MemBus) {
	t.Helper()
	if _, err := bus.Subscribe(rec,
		event.EventTypeToolInvocationStarted, event.EventTypeToolInvocationCompleted,
		event.EventTypeToolInvocationFailed, event.EventTypeToolAttemptStarted,
		event.EventTypeToolAttemptCompleted, event.EventTypeToolRetryScheduled); err != nil {
		t.Fatal(err)
	}
}

func TestPrePlatformBuiltinSuccessEmitsDispatchEvidence(t *testing.T) {
	bus := event.NewMemBus()
	rec := &eventRecorder{}
	subscribeEvidence(t, rec, bus)
	rt := newPrePlatformRuntime(t, bus, rec)
	scope := ToolScope{ActorID: "actor-1", BusinessID: "biz-1", AgentTools: []string{"calculator"}, CorrelationID: "corr-success"}
	out, err := rt.InvokeToolScoped(context.Background(), scope, "agent-1",
		ToolCallSpec{ToolID: "calculator", Operation: "execute", Input: map[string]string{
			"a": "6", "b": "7", "op": "mul",
		}})
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if out["result"] != "42" {
		t.Fatalf("expected 42, got %v", out)
	}
	_, _ = bus.Dispatch()
	started := rec.fields(event.EventTypeToolInvocationStarted)
	completed := rec.fields(event.EventTypeToolInvocationCompleted)
	if len(started) != 1 || len(completed) != 1 {
		t.Fatalf("expected one started and one completed, got started=%d completed=%d", len(started), len(completed))
	}
	if started[0]["source"] == "" {
		// source is on the envelope, not in data; check via recorder events below
	}
	// Verify source from raw recorder events.
	foundSource := false
	for _, e := range rec.events {
		if e.Type == event.EventTypeToolInvocationStarted {
			if e.Source == "agentexec" {
				foundSource = true
			}
		}
	}
	if !foundSource {
		t.Fatalf("invocation.started source should be agentexec")
	}
	if started[0]["side_effect_class"] != "read" {
		t.Fatalf("side_effect_class should be read, got %q", started[0]["side_effect_class"])
	}
	if started[0]["call_id"] != "corr-success" || completed[0]["call_id"] != "corr-success" {
		t.Fatalf("call_id mismatch: started=%v completed=%v", started[0], completed[0])
	}
	for _, typ := range []event.EventType{event.EventTypeToolAttemptStarted, event.EventTypeToolAttemptCompleted, event.EventTypeToolRetryScheduled} {
		if n := len(rec.fields(typ)); n != 0 {
			t.Fatalf("must not fabricate %s (got %d)", typ, n)
		}
	}
}

func TestPrePlatformBuiltinFailureEmitsFailedEvidence(t *testing.T) {
	bus := event.NewMemBus()
	rec := &eventRecorder{}
	subscribeEvidence(t, rec, bus)
	rt := newPrePlatformRuntime(t, bus, rec)
	scope := ToolScope{ActorID: "actor-1", BusinessID: "biz-1", AgentTools: []string{"calculator"}, CorrelationID: "corr-fail"}
	_, err := rt.InvokeToolScoped(context.Background(), scope, "agent-1",
		ToolCallSpec{ToolID: "calculator", Operation: "execute", Input: map[string]string{
			"a": "not-a-number", "b": "7", "op": "mul",
		}})
	if err == nil {
		t.Fatalf("expected failure")
	}
	_, _ = bus.Dispatch()
	started := rec.fields(event.EventTypeToolInvocationStarted)
	failed := rec.fields(event.EventTypeToolInvocationFailed)
	if len(started) != 1 || len(failed) != 1 {
		t.Fatalf("expected one started and one failed, got started=%d failed=%d", len(started), len(failed))
	}
	if failed[0]["error_kind"] != "executor failure" {
		t.Fatalf("error_kind should be executor failure, got %q", failed[0]["error_kind"])
	}
	if failed[0]["outcome"] != "failed" {
		t.Fatalf("outcome should be failed, got %q", failed[0]["outcome"])
	}
	for _, typ := range []event.EventType{event.EventTypeToolAttemptStarted, event.EventTypeToolAttemptCompleted, event.EventTypeToolRetryScheduled} {
		if n := len(rec.fields(typ)); n != 0 {
			t.Fatalf("must not fabricate %s (got %d)", typ, n)
		}
	}
}

func TestPrePlatformBuiltinGovernanceDenialEmitsNoDispatch(t *testing.T) {
	bus := event.NewMemBus()
	rec := &eventRecorder{}
	subscribeEvidence(t, rec, bus)
	tools := tool.NewToolRegistry()
	registerBuiltins(tools)
	now := time.Now().UTC()
	policies := []*governance.Policy{{
		SchemaVersion: "1.0.0", EntityType: "policy", PolicyID: "default-deny",
		PolicyVersion: "1", PolicyType: governance.PolicyTypeAccessControl,
		Name: "deny", Status: governance.PolicyStatusActive,
		Subject:  governance.Subject{SubjectType: "all"},
		Action:   governance.Action{ActionType: "custom"},
		Resource: governance.Resource{ResourceType: "all"},
		Effect:   governance.DENY, EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
	}}
	host := &admissionHost{approved: map[string]bool{}, pending: map[string]string{}}
	rt := &Runtime{
		Registry:   nil,
		Tools:      tools,
		ToolExec:   BuiltinExecutor(),
		Controller: control.New(control.Options{Engine: governance.NewEngine(policies), Approver: host, Escalator: host}),
		now:        time.Now, maxDepth: 3, maxFan: 4, maxConcurrent: 4,
		Bus: bus,
	}
	scope := ToolScope{ActorID: "actor-1", BusinessID: "biz-1", AgentTools: []string{"calculator"}, CorrelationID: "corr-deny"}
	_, err := rt.InvokeToolScoped(context.Background(), scope, "agent-1",
		ToolCallSpec{ToolID: "calculator", Operation: "execute", Input: map[string]string{
			"a": "6", "b": "7", "op": "mul",
		}})
	if err == nil {
		t.Fatalf("expected governance denial")
	}
	_, _ = bus.Dispatch()
	for _, typ := range []event.EventType{event.EventTypeToolInvocationStarted, event.EventTypeToolInvocationCompleted, event.EventTypeToolInvocationFailed} {
		if n := len(rec.fields(typ)); n != 0 {
			t.Fatalf("governance refusal must not produce %s (got %d)", typ, n)
		}
	}
}
