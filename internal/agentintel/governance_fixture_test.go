package agentintel

// Test seam for the governance admission boundary
// (contracts/AGENT_GOVERNANCE_CONTROL_CONTRACTS.md). The helper builds the SAME
// permissive seeded policy the launcher ships, so a test that does not care
// about governance keeps its previous behaviour, while tests that DO care can
// drive the five canonical outcomes deterministically.

import (
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
)

// seededAllowPolicy mirrors the engine's shipped built-in (core seeds exactly
// this one so an unconfigured installation does not fall through to DENY).
func seededAllowPolicy() *governance.Policy {
	now := time.Now().UTC()
	return &governance.Policy{
		SchemaVersion: schema.Version,
		EntityType:    "policy",
		PolicyID:      governance.DefaultAllowPolicyID,
		PolicyVersion: "1",
		PolicyType:    governance.PolicyTypeAccessControl,
		Name:          "Default Allow",
		Description:   "test mirror of the seeded built-in",
		Status:        governance.PolicyStatusActive,
		Subject:       governance.Subject{SubjectType: "all"},
		Action:        governance.Action{ActionType: "custom"},
		Resource:      governance.Resource{ResourceType: "all"},
		Effect:        governance.ALLOW,
		Precedence:    0,
		EffectiveFrom: now,
		CreatedAt:     now,
		CreatedBy:     "system",
		Provenance:    schema.ProvenanceRef{Origin: "system", Producer: "test", ProducedAt: now},
	}
}

// stubApprover is an in-process approval host for tests: it records the requests
// the controller opens and can pre-approve one fingerprint so the resume path is
// exercisable without a gateway.
type stubApprover struct {
	mu       sync.Mutex
	pending  map[string]string // fingerprint → approval id
	approved map[string]bool
	byID     map[string]string // approval id → fingerprint
	next     int
	// AutoApprove approves every request as soon as it is created, simulating a
	// human who decided before the next admission.
	AutoApprove bool
}

func newStubApprover() *stubApprover {
	return &stubApprover{pending: map[string]string{}, approved: map[string]bool{}, byID: map[string]string{}}
}

func (s *stubApprover) RequestApproval(p control.Proposal, decision governance.Decision) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := "apr-test-" + p.ProposalID
	fp := p.Fingerprint()
	s.byID[id] = fp
	if s.AutoApprove {
		s.approved[fp] = true
	} else {
		s.pending[fp] = id
	}
	return id, nil
}

func (s *stubApprover) ApprovedState(p control.Proposal, fingerprint string) (governance.ApprovalState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.approved[fingerprint] {
		return governance.ApprovalStateApproved, true
	}
	return governance.ApprovalStatePending, true
}

func (s *stubApprover) Approve(fingerprint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approved[fingerprint] = true
}

// approveAll decides every pending request, simulating the approver deciding
// before the resume re-runs the objective.
func (s *stubApprover) approveAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for fp := range s.pending {
		s.approved[fp] = true
		delete(s.pending, fp)
	}
}

// pendingSnapshot reports the fingerprints still awaiting a decision.
func (s *stubApprover) pendingSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.pending))
	for fp := range s.pending {
		out = append(out, fp)
	}
	return out
}

// stubEscalator records escalations without an event bus.
type stubEscalator struct {
	mu   sync.Mutex
	refs []string
}

func (s *stubEscalator) Escalate(p control.Proposal, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref := "esc-test-" + p.ProposalID
	s.refs = append(s.refs, ref)
	return ref, nil
}

func (s *stubEscalator) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.refs)
}

// newTestController builds an admission controller over an explicit policy set.
func newTestController(policies ...*governance.Policy) (*control.Controller, *stubApprover, *stubEscalator) {
	approver := newStubApprover()
	escalator := &stubEscalator{}
	return control.New(control.Options{
		Engine:    governance.NewEngine(policies),
		Approver:  approver,
		Escalator: escalator,
	}), approver, escalator
}

// allowAllController is the permissive default used by fixtures that are not
// about governance.
func allowAllController() (*control.Controller, *stubApprover, *stubEscalator) {
	return newTestController(seededAllowPolicy())
}

// denyPolicy denies one action (optionally scoped to a resource) for tests.
func denyPolicy(action, resource string) *governance.Policy {
	now := time.Now().UTC()
	p := &governance.Policy{
		SchemaVersion: schema.Version, EntityType: "policy",
		PolicyID: "test-deny-" + action, PolicyVersion: "1",
		PolicyType: governance.PolicyTypeAccessControl, Name: "test deny",
		Description: "test deny", Status: governance.PolicyStatusActive,
		Subject:  governance.Subject{SubjectType: "all"},
		Action:   governance.Action{ActionType: "custom", ActionIDs: []string{action}},
		Resource: governance.Resource{ResourceType: "all"},
		Effect:   governance.DENY, Precedence: 1000,
		EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
		Provenance: schema.ProvenanceRef{Origin: "system", Producer: "test", ProducedAt: now},
	}
	if resource != "" {
		p.Resource = governance.Resource{ResourceType: "tool", ResourceIDs: []string{resource}}
	}
	return p
}

// approvalPolicy requires an approval for one action (optionally one resource).
func approvalPolicy(action, resource string) *governance.Policy {
	now := time.Now().UTC()
	p := &governance.Policy{
		SchemaVersion: schema.Version, EntityType: "policy",
		PolicyID: "test-approval-" + action, PolicyVersion: "1",
		PolicyType: governance.PolicyTypeAccessControl, Name: "test approval",
		Description: "test approval", Status: governance.PolicyStatusActive,
		Subject:  governance.Subject{SubjectType: "all"},
		Action:   governance.Action{ActionType: "custom", ActionIDs: []string{action}},
		Resource: governance.Resource{ResourceType: "all"},
		Effect:   governance.REQUIRE_APPROVAL, Precedence: 1000,
		ApprovalConfig: &governance.ApprovalConfig{
			ApproverType: "human", ApproverIDs: []string{"approver-1"},
			TimeoutSeconds: 600, AutoDenyOnTimeout: true, SelfApprovalProhibited: true,
		},
		EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
		Provenance: schema.ProvenanceRef{Origin: "system", Producer: "test", ProducedAt: now},
	}
	if resource != "" {
		p.Resource = governance.Resource{ResourceType: "tool", ResourceIDs: []string{resource}}
	}
	return p
}

// escalatePolicy escalates one action.
func escalatePolicy(action, resource string) *governance.Policy {
	now := time.Now().UTC()
	p := &governance.Policy{
		SchemaVersion: schema.Version, EntityType: "policy",
		PolicyID: "test-escalate-" + action, PolicyVersion: "1",
		PolicyType: governance.PolicyTypeAccessControl, Name: "test escalate",
		Description: "test escalate", Status: governance.PolicyStatusActive,
		Subject:  governance.Subject{SubjectType: "all"},
		Action:   governance.Action{ActionType: "custom", ActionIDs: []string{action}},
		Resource: governance.Resource{ResourceType: "all"},
		Effect:   governance.ESCALATE, Precedence: 1000,
		EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
		Provenance: schema.ProvenanceRef{Origin: "system", Producer: "test", ProducedAt: now},
	}
	if resource != "" {
		p.Resource = governance.Resource{ResourceType: "tool", ResourceIDs: []string{resource}}
	}
	return p
}

// constraintsPolicy allows one action with mandatory constraints.
func constraintsPolicy(action string, constraints ...governance.Constraint) *governance.Policy {
	now := time.Now().UTC()
	return &governance.Policy{
		SchemaVersion: schema.Version, EntityType: "policy",
		PolicyID: "test-constraints-" + action, PolicyVersion: "1",
		PolicyType: governance.PolicyTypeAccessControl, Name: "test constraints",
		Description: "test constraints", Status: governance.PolicyStatusActive,
		Subject:  governance.Subject{SubjectType: "all"},
		Action:   governance.Action{ActionType: "custom", ActionIDs: []string{action}},
		Resource: governance.Resource{ResourceType: "all"},
		Effect:   governance.ALLOW_WITH_CONSTRAINTS, Precedence: 1000,
		Constraints:   constraints,
		EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
		Provenance: schema.ProvenanceRef{Origin: "system", Producer: "test", ProducedAt: now},
	}
}
