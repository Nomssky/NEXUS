package control

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

// Sentinel errors. They are distinguishable so callers classify without string
// matching, and they map onto the existing error taxonomy at the edges.
var (
	// ErrInvalidProposal is a proposal that does not name its own authority.
	ErrInvalidProposal = errors.New("governance: invalid action proposal")
	// ErrConstraintUnenforceable is a mandatory constraint the runtime cannot
	// enforce. It FAILS CLOSED (contract §4): the action does not run.
	ErrConstraintUnenforceable = errors.New("governance: constraint cannot be enforced")
	// ErrGovernanceUnavailable is a missing engine. Also fails closed.
	ErrGovernanceUnavailable = errors.New("governance: engine unavailable")
)

// EscalationRequest is what the controller needs from the host to raise an
// escalation through the EXISTING core escalation path. The host (core.Engine)
// owns event publication, the escalation queue and the attention item; this
// package never publishes governance.escalated itself.
type EscalationRequest struct {
	CorrelationID string
	ExecutionID   string
	BusinessID    string
	ActorID       string
	Reason        string
	Context       map[string]string
}

// Approver is the existing ApprovalEngine seam for action-level approvals. The
// host implements it with governance.ApprovalEngine so authorization,
// self-approval and timeout semantics are the engine's, not this package's.
type Approver interface {
	// RequestApproval records an approval request bound to the proposal's
	// fingerprint and returns the approval id.
	RequestApproval(p Proposal, decision governance.Decision) (string, error)
	// ApprovedState reports the state of the approval bound to this proposal's
	// exact fingerprint, if one exists. It is how the resume path re-evaluates
	// governance instead of trusting a flag.
	ApprovedState(p Proposal, fingerprint string) (governance.ApprovalState, bool)
}

// Escalator raises an escalation through the existing path.
type Escalator interface {
	// Escalate blocks the action and hands it to the existing
	// governance → attention path, returning the escalation reference.
	Escalate(p Proposal, reason string) (string, error)
}

// Publisher publishes metadata-only governance facts on the EXISTING bus.
type Publisher interface {
	Publish(kind, correlationID, businessID string, fields map[string]string)
}

// Options configures a Controller.
type Options struct {
	// Engine is the authoritative policy evaluator. Required: without it the
	// controller fails closed.
	Engine *governance.Engine
	// Now is injectable for deterministic tests.
	Now func() time.Time
	// Approver creates action-level approval records (host-provided, backed by
	// the existing ApprovalEngine).
	Approver Approver
	// Escalator raises escalations through the existing path.
	Escalator Escalator
	// Publisher emits governance.action_proposed / governance.constraint_applied.
	Publisher Publisher
	// MaxDuration caps an enforceable max_duration_ms constraint.
	MaxDuration time.Duration
}

// Admission is the result of one admission call.
type Admission struct {
	Decision governance.Decision
	// Effective is the constraint set that must travel with the request.
	Effective []EffectiveConstraint
	// ApprovalID is set when the decision required approval.
	ApprovalID string
	// EscalationID is set when the decision escalated.
	EscalationID string
	// Proposal is the proposal that was admitted (echoed for audit).
	Proposal Proposal
	// Fingerprint binds any approval to this exact proposal.
	Fingerprint string
	// Enforceable reports whether every mandatory constraint was enforceable.
	Enforceable bool
}

// Allowed reports whether the action may proceed to the capability platform.
func (a Admission) Allowed() bool {
	return a.Decision.IsAllowing() && a.Enforceable
}

// AwaitingApproval reports the pending-approval state.
func (a Admission) AwaitingApproval() bool { return a.Decision.Outcome == governance.REQUIRE_APPROVAL }

// Escalated reports the escalated state.
func (a Admission) Escalated() bool { return a.Decision.Outcome == governance.ESCALATE }

// Denied reports the denied state.
func (a Admission) Denied() bool { return a.Decision.Outcome == governance.DENY }

// Controller is the single admission point for agent actions.
type Controller struct {
	engine   *governance.Engine
	approver Approver
	escalatr Escalator
	pub      Publisher
	now      func() time.Time
	maxDur   time.Duration

	mu            sync.Mutex
	consumed      map[string]struct{} // executionID|fingerprint: one admission per approval
	consumedOrder []string            // FIFO of consumed keys, bounding the ledger
}

// New builds a controller. A nil engine is allowed to be constructed but every
// admission then fails closed (governance is never bypassed).
func New(opts Options) *Controller {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	maxDur := opts.MaxDuration
	if maxDur <= 0 {
		maxDur = 30 * time.Second
	}
	return &Controller{engine: opts.Engine, approver: opts.Approver, escalatr: opts.Escalator,
		pub: opts.Publisher, now: now, maxDur: maxDur, consumed: map[string]struct{}{}}
}

// Admit evaluates one proposal through the authoritative engine and performs the
// admission side effects (approval record, escalation handoff, constraint
// resolution). It never executes anything: the caller does, and only when
// Admission.Allowed() is true.
func (c *Controller) Admit(p Proposal) (Admission, error) {
	if err := p.Validate(); err != nil {
		return Admission{}, err
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = c.now()
	}
	if c.pub != nil {
		c.pub.Publish(EventActionProposed, p.CorrelationID, p.BusinessID, map[string]string{
			"proposal_id": p.ProposalID,
			"action":      p.Action,
			"resource":    p.Resource,
			"tool_id":     p.ToolID,
			"operation":   p.Operation,
			"agent_id":    p.AgentID,
			"step_id":     p.StepID,
		})
	}
	// Governance unavailable → fail closed, never "assume allowed".
	if c.engine == nil {
		return Admission{}, fmt.Errorf("%w: action admission refused", ErrGovernanceUnavailable)
	}
	fp := p.Fingerprint()
	decision := c.engine.Evaluate(p.GovernanceRequest(c.approvalFor(p, fp)))

	adm := Admission{Decision: decision, Proposal: p, Fingerprint: fp, Enforceable: true}
	// The decision itself is a fact on the existing event vocabulary: one
	// governance.decided per admission, correlated to the proposal that caused it.
	c.publishDecision(p, decision)

	switch decision.Outcome {
	case governance.ALLOW, governance.ALLOW_WITH_CONSTRAINTS:
		effective, err := c.resolveConstraints(p, decision)
		if err != nil {
			// Fail closed: an unenforceable mandatory constraint never executes.
			adm.Enforceable = false
			return adm, err
		}
		adm.Effective = effective
		if len(effective) > 0 && c.pub != nil {
			c.pub.Publish(EventConstraintApplied, p.CorrelationID, p.BusinessID, map[string]string{
				"proposal_id": p.ProposalID,
				"constraints": fmt.Sprint(len(effective)),
				"kinds":       constraintKinds(effective),
			})
		}
		return adm, nil
	case governance.REQUIRE_APPROVAL:
		if c.approver == nil {
			return adm, fmt.Errorf("%w: no approver configured for REQUIRE_APPROVAL", ErrGovernanceUnavailable)
		}
		id, err := c.approver.RequestApproval(p, decision)
		if err != nil {
			return adm, err
		}
		adm.ApprovalID = id
		return adm, nil
	case governance.ESCALATE:
		if c.escalatr == nil {
			return adm, fmt.Errorf("%w: no escalation path configured for ESCALATE", ErrGovernanceUnavailable)
		}
		ref, err := c.escalatr.Escalate(p, decision.Reason)
		if err != nil {
			return adm, err
		}
		adm.EscalationID = ref
		return adm, nil
	default: // DENY
		return adm, nil
	}
}

// publishDecision records the admission decision as a metadata-only fact on the
// existing governance.decided event. It carries ids, scope, action, resource,
// the outcome and the matched policy provenance — never prompts, tool output or
// credential material, and never chain-of-thought.
func (c *Controller) publishDecision(p Proposal, d governance.Decision) {
	if c.pub == nil {
		return
	}
	fields := map[string]string{
		"proposal_id":   p.ProposalID,
		"action":        p.Action,
		"resource":      p.Resource,
		"resource_type": p.ResourceType,
		"decision":      d.Outcome.String(),
		"tool_id":       p.ToolID,
		"operation":     p.Operation,
		"agent_id":      p.AgentID,
		"objective_id":  p.ObjectiveID,
		"execution_id":  p.ExecutionID,
		"actor_id":      p.ActorID,
		"division_id":   p.DivisionID,
		"risk_level":    string(p.RiskLevel),
	}
	if d.MatchedPolicyID != "" {
		fields["policy_id"] = d.MatchedPolicyID
	}
	if d.MatchedPolicyVersion != "" {
		fields["policy_version"] = d.MatchedPolicyVersion
	}
	if len(d.Constraints) > 0 {
		fields["constraints"] = fmt.Sprint(len(d.Constraints))
	}
	c.pub.Publish(EventGovernanceDecided, p.CorrelationID, p.BusinessID, fields)
}

// maxConsumedApprovals bounds the single-use ledger of spent approvals. It is a
// cache of "this exact proposal was already admitted under this approval": a
// dropped key can only cost one extra admission, which still requires a fresh
// approval, so eviction never widens authority.
const maxConsumedApprovals = 4096

// approvalFor returns the approval state for this proposal when — and only when
// — an approved record exists for the EXACT same fingerprint and has not already
// been consumed by this execution. Everything else leaves approval nil, so the
// engine re-evaluates the approval gate (contract §5).
func (c *Controller) approvalFor(p Proposal, fingerprint string) *governance.ApprovalState {
	if c.approver == nil {
		return nil
	}
	state, ok := c.approver.ApprovedState(p, fingerprint)
	if !ok || state != governance.ApprovalStateApproved {
		return nil
	}
	key := p.ExecutionID + "|" + fingerprint
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, spent := c.consumed[key]; spent {
		// One approval authorizes one admission after the resume. A repeated
		// identical proposal must earn fresh approval.
		return nil
	}
	c.consumed[key] = struct{}{}
	c.consumedOrder = append(c.consumedOrder, key)
	for len(c.consumedOrder) > maxConsumedApprovals {
		delete(c.consumed, c.consumedOrder[0])
		c.consumedOrder = c.consumedOrder[1:]
	}
	approved := governance.ApprovalStateApproved
	return &approved
}

// MarkConsumed is exported for hosts that resolve approvals outside this
// package's Admit path (an approval-driven resume that never re-proposes).
func (c *Controller) MarkConsumed(executionID, fingerprint string) {
	key := executionID + "|" + fingerprint
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, spent := c.consumed[key]; spent {
		return
	}
	c.consumed[key] = struct{}{}
	c.consumedOrder = append(c.consumedOrder, key)
	for len(c.consumedOrder) > maxConsumedApprovals {
		delete(c.consumed, c.consumedOrder[0])
		c.consumedOrder = c.consumedOrder[1:]
	}
}

func constraintKinds(cs []EffectiveConstraint) string {
	kinds := make([]string, 0, len(cs))
	for _, c := range cs {
		kinds = append(kinds, c.Kind)
	}
	return strings.Join(kinds, ",")
}

// Event names published by the controller (the rest are reused from the
// existing vocabulary).
const (
	EventActionProposed = "governance.action_proposed"
	// EventGovernanceDecided is the EXISTING vocabulary type
	// (SCHEMA_EVENTS_TRIGGERS) that until now was declared but never
	// published. Admission is its producer.
	EventGovernanceDecided = "governance.decided"
	EventConstraintApplied = "governance.constraint_applied"
)
