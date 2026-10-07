package control

// Behavioural tests for the admission boundary
// (contracts/AGENT_GOVERNANCE_CONTROL_CONTRACTS.md): the five canonical
// outcomes, the runtime-established proposal boundary, enforceable constraints,
// fingerprint-bound approval and fail-closed behaviour.

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
)

// ---------- host stubs ----------

type testApprover struct {
	mu       sync.Mutex
	approved map[string]bool
	pending  map[string]string
	requests int
}

func newTestApprover() *testApprover {
	return &testApprover{approved: map[string]bool{}, pending: map[string]string{}}
}

func (a *testApprover) RequestApproval(p Proposal, _ governance.Decision) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests++
	id := "apr-" + p.ProposalID
	a.pending[p.Fingerprint()] = id
	return id, nil
}

func (a *testApprover) ApprovedState(_ Proposal, fingerprint string) (governance.ApprovalState, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approved[fingerprint] {
		return governance.ApprovalStateApproved, true
	}
	return governance.ApprovalStatePending, true
}

func (a *testApprover) approve(fp string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.approved[fp] = true
}

type testEscalator struct {
	mu   sync.Mutex
	refs []string
}

func (e *testEscalator) Escalate(p Proposal, _ string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ref := "esc-" + p.ProposalID
	e.refs = append(e.refs, ref)
	return ref, nil
}

func (e *testEscalator) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.refs)
}

type testPublisher struct {
	mu     sync.Mutex
	events map[string]map[string]string
}

func (p *testPublisher) Publish(kind, _, _ string, fields map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.events == nil {
		p.events = map[string]map[string]string{}
	}
	p.events[kind] = fields
}

func (p *testPublisher) has(kind string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.events[kind]
	return ok
}

// ---------- policy fixtures ----------

func policy(id string, effect governance.Outcome, action string, resourceIDs []string,
	constraints []governance.Constraint, approval *governance.ApprovalConfig, precedence int) *governance.Policy {
	now := time.Now().UTC()
	return &governance.Policy{
		SchemaVersion: schema.Version, EntityType: "policy", PolicyID: id, PolicyVersion: "1",
		PolicyType: governance.PolicyTypeAccessControl, Name: id, Description: id,
		Status:  governance.PolicyStatusActive,
		Subject: governance.Subject{SubjectType: "all"},
		Action:  governance.Action{ActionType: "custom", ActionIDs: []string{action}},
		Resource: governance.Resource{
			ResourceType: "tool", ResourceIDs: resourceIDs,
		},
		Effect: effect, Precedence: precedence,
		Constraints: constraints, ApprovalConfig: approval,
		EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
		Provenance: schema.ProvenanceRef{Origin: "system", Producer: "test", ProducedAt: now},
	}
}

func allowAll() *governance.Policy {
	now := time.Now().UTC()
	return &governance.Policy{
		SchemaVersion: schema.Version, EntityType: "policy",
		PolicyID: governance.DefaultAllowPolicyID, PolicyVersion: "1",
		PolicyType: governance.PolicyTypeAccessControl, Name: "allow", Description: "allow",
		Status:        governance.PolicyStatusActive,
		Subject:       governance.Subject{SubjectType: "all"},
		Action:        governance.Action{ActionType: "custom"},
		Resource:      governance.Resource{ResourceType: "all"},
		Effect:        governance.ALLOW,
		EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
		Provenance: schema.ProvenanceRef{Origin: "system", Producer: "test", ProducedAt: now},
	}
}

func proposal(id string) Proposal {
	return Proposal{
		ProposalID: id, CorrelationID: "exec-1", ExecutionID: "exec-1", ObjectiveID: "obj-1",
		StepID: "s1", ActorID: "actor-1", AgentID: "agent-1",
		BusinessID: "biz-1", DivisionID: "div-1",
		Action: ActionToolCall, Resource: "http.request", ResourceType: ResourceTypeCapability,
		ToolID: "http.request", Operation: "get",
		CreatedAt: time.Now().UTC(),
	}
}

func newController(t *testing.T, policies ...*governance.Policy) (*Controller, *testApprover, *testEscalator, *testPublisher) {
	t.Helper()
	approver, escalator, pub := newTestApprover(), &testEscalator{}, &testPublisher{}
	c := New(Options{Engine: governance.NewEngine(policies), Approver: approver,
		Escalator: escalator, Publisher: pub})
	return c, approver, escalator, pub
}

// ---------- the five outcomes ----------

func TestAdmissionOutcomes(t *testing.T) {
	approvalCfg := &governance.ApprovalConfig{ApproverType: "human",
		ApproverIDs: []string{"approver-1"}, TimeoutSeconds: 60, AutoDenyOnTimeout: true,
		SelfApprovalProhibited: true}
	cases := []struct {
		name   string
		policy *governance.Policy
		want   governance.Outcome
	}{
		{"allow", allowAll(), governance.ALLOW},
		{"deny", policy("p-deny", governance.DENY, ActionToolCall, nil, nil, nil, 1000), governance.DENY},
		{"require approval", policy("p-appr", governance.REQUIRE_APPROVAL, ActionToolCall, nil, nil, approvalCfg, 1000), governance.REQUIRE_APPROVAL},
		{"escalate", policy("p-esc", governance.ESCALATE, ActionToolCall, nil, nil, nil, 1000), governance.ESCALATE},
		{"allow with constraints", policy("p-constraints", governance.ALLOW_WITH_CONSTRAINTS, ActionToolCall, nil,
			[]governance.Constraint{{ConstraintID: "c1", ConstraintType: ConstraintToolAllowlist,
				Expression: "http.request", Severity: "mandatory"}}, nil, 1000), governance.ALLOW_WITH_CONSTRAINTS},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, approver, escalator, pub := newController(t, tc.policy)
			adm, err := c.Admit(proposal("prop-" + tc.name))
			if err != nil {
				t.Fatalf("admit: %v", err)
			}
			if adm.Decision.Outcome != tc.want {
				t.Fatalf("outcome = %s, want %s", adm.Decision.Outcome, tc.want)
			}
			if !pub.has(EventActionProposed) {
				t.Fatalf("every admission must publish the proposal fact")
			}
			switch tc.want {
			case governance.ALLOW:
				if !adm.Allowed() || adm.ApprovalID != "" {
					t.Fatalf("ALLOW must proceed without an approval: %+v", adm)
				}
			case governance.DENY:
				if adm.Allowed() || adm.Denied() != true {
					t.Fatalf("DENY must block: %+v", adm)
				}
			case governance.REQUIRE_APPROVAL:
				if adm.Allowed() || !adm.AwaitingApproval() || adm.ApprovalID == "" {
					t.Fatalf("REQUIRE_APPROVAL must stop with an approval record: %+v", adm)
				}
				if approver.requests != 1 {
					t.Fatalf("exactly one approval request expected, got %d", approver.requests)
				}
			case governance.ESCALATE:
				if adm.Allowed() || !adm.Escalated() || adm.EscalationID == "" {
					t.Fatalf("ESCALATE must block and escalate: %+v", adm)
				}
				if escalator.count() != 1 {
					t.Fatalf("exactly one escalation expected, got %d", escalator.count())
				}
			case governance.ALLOW_WITH_CONSTRAINTS:
				if !adm.Allowed() || len(adm.Effective) != 1 {
					t.Fatalf("ALLOW_WITH_CONSTRAINTS must carry enforceable constraints: %+v", adm)
				}
				if !pub.has(EventConstraintApplied) {
					t.Fatalf("applied constraints must be published")
				}
			}
		})
	}
}

func TestNoPolicyIsDefaultDeny(t *testing.T) {
	c, _, _, _ := newController(t) // no policies at all
	adm, err := c.Admit(proposal("prop-none"))
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if adm.Denied() != true {
		t.Fatalf("an unmatched action must be DENY (fail safe), got %s", adm.Decision.Outcome)
	}
}

func TestMissingEngineFailsClosed(t *testing.T) {
	c := New(Options{}) // no engine at all
	if _, err := c.Admit(proposal("prop-x")); !errors.Is(err, ErrGovernanceUnavailable) {
		t.Fatalf("a missing governance engine must fail closed, got %v", err)
	}
}

// ---------- the proposal boundary ----------

func TestProposalRequiresItsOwnAuthority(t *testing.T) {
	c, _, _, _ := newController(t, allowAll())
	for name, mutate := range map[string]func(p *Proposal){
		"no actor":         func(p *Proposal) { p.ActorID = "" },
		"no business":      func(p *Proposal) { p.BusinessID = "" },
		"no action":        func(p *Proposal) { p.Action = "" },
		"no resource type": func(p *Proposal) { p.ResourceType = "" },
	} {
		t.Run(name, func(t *testing.T) {
			p := proposal("prop-" + name)
			mutate(&p)
			if _, err := c.Admit(p); !errors.Is(err, ErrInvalidProposal) {
				t.Fatalf("a proposal without its authority must be refused, got %v", err)
			}
		})
	}
}

func TestFingerprintBindsEveryAuthorityField(t *testing.T) {
	base := proposal("prop-fp")
	mutations := map[string]func(p *Proposal){
		"action":      func(p *Proposal) { p.Action = ActionDelegate },
		"resource":    func(p *Proposal) { p.Resource = "filesystem.write" },
		"tool":        func(p *Proposal) { p.ToolID = "filesystem.write" },
		"operation":   func(p *Proposal) { p.Operation = "post" },
		"actor":       func(p *Proposal) { p.ActorID = "actor-2" },
		"business":    func(p *Proposal) { p.BusinessID = "biz-2" },
		"division":    func(p *Proposal) { p.DivisionID = "div-2" },
		"agent":       func(p *Proposal) { p.AgentID = "agent-2" },
		"objective":   func(p *Proposal) { p.ObjectiveID = "obj-2" },
		"execution":   func(p *Proposal) { p.ExecutionID = "exec-2" },
		"correlation": func(p *Proposal) { p.CorrelationID = "exec-2" },
		"constraints": func(p *Proposal) {
			p.Constraints = []governance.Constraint{{ConstraintID: "c1",
				ConstraintType: ConstraintToolAllowlist, Expression: "http.request"}}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			p := base
			mutate(&p)
			if p.Fingerprint() == base.Fingerprint() {
				t.Fatalf("changing %s must change the approval fingerprint", name)
			}
		})
	}
	// The proposal id is deliberately NOT part of the binding: the resume
	// re-proposes and must still match.
	same := base
	same.ProposalID = "prop-resumed"
	if same.Fingerprint() != base.Fingerprint() {
		t.Fatalf("a re-proposal of the same action must keep its fingerprint")
	}
}

func TestProposalContextCannotOverwriteTheProposalID(t *testing.T) {
	p := proposal("prop-real")
	p.Context = map[string]string{"proposal_id": "prop-forged", "step": "s1"}
	rendered := p.GovernanceRequest(nil)
	if rendered.Context["proposal_id"] != "prop-real" {
		t.Fatalf("context must never rewrite the runtime proposal id: %+v", rendered.Context)
	}
}

// ---------- constraints ----------

func TestUnenforceableMandatoryConstraintFailsClosed(t *testing.T) {
	cases := map[string]governance.Constraint{
		"unknown type":      {ConstraintID: "c", ConstraintType: "temperature_limit", Expression: "1", Severity: "mandatory"},
		"empty allowlist":   {ConstraintID: "c", ConstraintType: ConstraintToolAllowlist, Expression: " , ", Severity: "mandatory"},
		"bad duration":      {ConstraintID: "c", ConstraintType: ConstraintMaxDuration, Expression: "soon", Severity: "mandatory"},
		"negative duration": {ConstraintID: "c", ConstraintType: ConstraintMaxDuration, Expression: "-5", Severity: "mandatory"},
		"over-wide duration": {ConstraintID: "c", ConstraintType: ConstraintMaxDuration,
			Expression: "999999", Severity: "mandatory"},
	}
	for name, constraint := range cases {
		t.Run(name, func(t *testing.T) {
			c, _, _, _ := newController(t,
				policy("p-"+name, governance.ALLOW_WITH_CONSTRAINTS, ActionToolCall, nil,
					[]governance.Constraint{constraint}, nil, 1000))
			adm, err := c.Admit(proposal("prop-" + name))
			if !errors.Is(err, ErrConstraintUnenforceable) {
				t.Fatalf("an unenforceable mandatory constraint must fail closed, got %v (%+v)", err, adm)
			}
			if adm.Allowed() {
				t.Fatalf("a failed constraint resolution must never admit the action")
			}
		})
	}
}

func TestAdvisoryConstraintIsRecordedNotEnforced(t *testing.T) {
	c, _, _, _ := newController(t,
		policy("p-advisory", governance.ALLOW_WITH_CONSTRAINTS, ActionToolCall, nil,
			[]governance.Constraint{{ConstraintID: "c", ConstraintType: "temperature_limit",
				Expression: "1", Severity: "advisory"}}, nil, 1000))
	adm, err := c.Admit(proposal("prop-advisory"))
	if err != nil {
		t.Fatalf("an advisory constraint must not block admission: %v", err)
	}
	if !adm.Allowed() || len(adm.Effective) != 0 {
		t.Fatalf("an advisory constraint is not a restriction: %+v", adm)
	}
}

func TestConstrainAppliesAndFailsClosed(t *testing.T) {
	adm := Admission{Effective: []EffectiveConstraint{
		{Kind: ConstraintToolAllowlist, ID: "c1", Allowed: []string{"http.request", "filesystem.write"}},
		{Kind: ConstraintOperationAllowlist, ID: "c2", Allowed: []string{"get"}},
		{Kind: ConstraintMaxDuration, ID: "c3", MaxDuration: 2 * time.Second},
	}}
	cr, err := Constrain("http.request", "get", Limits{MaxDuration: 10 * time.Second}, adm)
	if err != nil {
		t.Fatalf("an allowed call must constrain cleanly: %v", err)
	}
	if cr.MaxDuration != 2*time.Second {
		t.Fatalf("the constraint must tighten the call: %v", cr.MaxDuration)
	}
	if _, err := Constrain("filesystem.write", "write", Limits{MaxDuration: 10 * time.Second}, adm); !errors.Is(err, ErrConstraintUnenforceable) {
		t.Fatalf("a tool outside the allowlist must fail closed, got %v", err)
	}
	if _, err := Constrain("http.request", "post", Limits{MaxDuration: 10 * time.Second}, adm); !errors.Is(err, ErrConstraintUnenforceable) {
		t.Fatalf("an operation outside the allowlist must fail closed, got %v", err)
	}
	// A constraint can only tighten: a wider bound never extends the call.
	wide := Admission{Effective: []EffectiveConstraint{
		{Kind: ConstraintMaxDuration, ID: "c3", MaxDuration: 10 * time.Minute}}}
	cr, err = Constrain("http.request", "get", Limits{MaxDuration: 3 * time.Second}, wide)
	if err != nil || cr.MaxDuration != 3*time.Second {
		t.Fatalf("a constraint must never widen the call: %v %v", cr.MaxDuration, err)
	}
}

// ---------- approval resume ----------

func TestApprovedProposalIsAdmittedOnceOnly(t *testing.T) {
	approvalCfg := &governance.ApprovalConfig{ApproverType: "human", TimeoutSeconds: 60,
		SelfApprovalProhibited: true}
	c, approver, _, _ := newController(t,
		policy("p-appr", governance.REQUIRE_APPROVAL, ActionToolCall, nil, nil, approvalCfg, 1000))
	p := proposal("prop-resume")

	// Before approval the action waits.
	adm, err := c.Admit(p)
	if err != nil || !adm.AwaitingApproval() {
		t.Fatalf("first admission must wait: %+v %v", adm, err)
	}
	// The approver decides.
	approver.approve(p.Fingerprint())
	adm, err = c.Admit(p)
	if err != nil || !adm.Allowed() {
		t.Fatalf("the resumed admission must be allowed: %+v %v", adm, err)
	}
	// A repeated identical proposal needs fresh approval: one approval is not
	// permission forever.
	adm, err = c.Admit(p)
	if err != nil || !adm.AwaitingApproval() {
		t.Fatalf("a repeated identical proposal must require fresh approval: %+v %v", adm, err)
	}
}

func TestApprovalOfAnotherActionDoesNotAdmitThisOne(t *testing.T) {
	approvalCfg := &governance.ApprovalConfig{ApproverType: "human", TimeoutSeconds: 60,
		SelfApprovalProhibited: true}
	c, approver, _, _ := newController(t,
		policy("p-appr", governance.REQUIRE_APPROVAL, ActionToolCall, nil, nil, approvalCfg, 1000))
	approved := proposal("prop-a")
	other := proposal("prop-b")
	other.Operation = "post" // a different action
	approver.approve(approved.Fingerprint())

	adm, err := c.Admit(other)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if adm.Allowed() {
		t.Fatalf("an approval for another action must not admit this one: %+v", adm)
	}
	if !adm.AwaitingApproval() {
		t.Fatalf("the unmatched action must wait for its own approval: %+v", adm)
	}
}

func TestApprovalNeverOverridesDeny(t *testing.T) {
	approvalCfg := &governance.ApprovalConfig{ApproverType: "human", TimeoutSeconds: 60,
		SelfApprovalProhibited: true}
	c, approver, _, _ := newController(t,
		policy("p-deny", governance.DENY, ActionToolCall, nil, nil, nil, 1000),
		policy("p-appr", governance.REQUIRE_APPROVAL, ActionToolCall, nil, nil, approvalCfg, 500))
	p := proposal("prop-deny")
	approver.approve(p.Fingerprint())
	adm, err := c.Admit(p)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if adm.Allowed() {
		t.Fatalf("an approval must never override an explicit DENY: %+v", adm)
	}
}

func TestScopeScopedPolicyMatchesOnlyItsScope(t *testing.T) {
	// A policy pinned to one resource must not deny a different one.
	now := time.Now().UTC()
	scoped := policy("p-scoped", governance.DENY, ActionToolCall, []string{"filesystem.write"}, nil, nil, 1000)
	scoped.BusinessID = "biz-1"
	c, _, _, _ := newController(t, scoped, allowAll())
	adm, err := c.Admit(proposal("prop-scoped"))
	if err != nil || !adm.Allowed() {
		t.Fatalf("a policy for another resource must not apply: %+v %v", adm, err)
	}
	// The same policy denies its own resource.
	p := proposal("prop-scoped")
	p.ToolID, p.Resource = "filesystem.write", "filesystem.write"
	adm, err = c.Admit(p)
	if err != nil || adm.Allowed() {
		t.Fatalf("the scoped deny must apply to its resource: %+v %v", adm, err)
	}
	_ = now
}

func TestRiskForSideEffectClasses(t *testing.T) {
	if RiskFor("external_mutation") != governance.RiskLevelHigh {
		t.Fatalf("an external mutation is high risk")
	}
	if RiskFor("write") != governance.RiskLevelMedium {
		t.Fatalf("a write is medium risk")
	}
	if RiskFor("read") != governance.RiskLevelLow {
		t.Fatalf("a read is low risk")
	}
}

func TestMemoryResourceRendering(t *testing.T) {
	// The resource is the runtime-established RECORD id, never a
	// model-requested scope: the memory platform may clamp a request to a
	// narrower scope, and governance must decide about the effect.
	recordID := "mem:biz-1:governed-agent:notes"
	if got := MemoryResource(recordID); got != "memory:"+recordID {
		t.Fatalf("memory resource id = %q", got)
	}
	if !strings.HasPrefix(MemoryResource("mem:x"), "memory:mem:") {
		t.Fatalf("a memory resource must be namespaced")
	}
	if got := DelegateTarget("agent-7"); got != "agent:agent-7" {
		t.Fatalf("delegate resource id = %q", got)
	}
}

// --- non-tool constraint enforcement (contract §4) ---------------------------

func TestConstrainEffectEnforcesAgainstTheRealTarget(t *testing.T) {
	adm := Admission{Effective: []EffectiveConstraint{
		{Kind: ConstraintResourceRestrict, ID: "c1", Allowed: []string{"memory:mem:biz:a:notes"}},
	}}
	effect, err := ConstrainEffect(ActionMemoryWrite, "memory:mem:biz:a:notes", adm)
	if err != nil {
		t.Fatalf("a satisfied constraint must admit the effect: %v", err)
	}
	if len(effect.Applied) != 1 {
		t.Fatalf("the satisfied constraint must be recorded: %+v", effect)
	}

	if _, err := ConstrainEffect(ActionMemoryWrite, "memory:mem:biz:a:other", adm); !errors.Is(err, ErrConstraintUnenforceable) {
		t.Fatalf("a target outside the constraint must fail closed, got %v", err)
	}
}

func TestConstrainEffectNeverTrustsAnUnidentifiedTarget(t *testing.T) {
	adm := Admission{Effective: []EffectiveConstraint{
		{Kind: ConstraintResourceRestrict, ID: "c1", Allowed: []string{"a"}},
	}}
	if _, err := ConstrainEffect(ActionDelegate, "", adm); !errors.Is(err, ErrConstraintUnenforceable) {
		t.Fatalf("an empty target must fail closed, got %v", err)
	}
}

func TestConstrainEffectRejectsADurationConstraintOnANonToolEffect(t *testing.T) {
	adm := Admission{Effective: []EffectiveConstraint{
		{Kind: ConstraintMaxDuration, ID: "c-time", MaxDuration: time.Second},
	}}
	if _, err := ConstrainEffect(ActionMemoryDelete, "memory:mem:x", adm); !errors.Is(err, ErrConstraintUnenforceable) {
		t.Fatalf("a duration bound cannot be verified for a non-tool effect: %v", err)
	}
}

func TestConstrainEffectRejectsAnUnknownType(t *testing.T) {
	adm := Admission{Effective: []EffectiveConstraint{
		{Kind: "temperature_limit", ID: "c-x"},
	}}
	if _, err := ConstrainEffect(ActionDelegate, "agent:helper", adm); !errors.Is(err, ErrConstraintUnenforceable) {
		t.Fatalf("an unknown constraint type must fail closed, got %v", err)
	}
}
