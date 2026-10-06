package core

// Action-level governance control integration
// (contracts/AGENT_GOVERNANCE_CONTROL_CONTRACTS.md §5, §6, §13, §17):
// an action-level REQUIRE_APPROVAL stops the execution, the EXISTING approval
// endpoints decide it, the resume RE-EVALUATES governance, and nothing about the
// record survives a restart (G4).

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
)

// actionGateHandler admits the gated action through the engine's controller —
// exactly as agentexec does — and records that a side effect happened.
type actionGateHandler struct {
	engine  *Engine
	mu      sync.Mutex
	effects int
	// tool is the action resource the handler proposes; a test changes it to
	// model a different action on the resume.
	tool string
}

func (h *actionGateHandler) handle(ctx context.Context, req *executor.WorkRequest, _ *agent.Agent) (*executor.Outcome, error) {
	tool := h.tool
	if req.Input["action_tool"] != "" {
		tool = req.Input["action_tool"]
	}
	adm, err := h.engine.Controller().Admit(control.Proposal{
		ProposalID:    "prop-" + req.CorrelationID,
		CorrelationID: req.CorrelationID,
		ExecutionID:   req.CorrelationID,
		ObjectiveID:   req.CorrelationID,
		StepID:        req.TaskID,
		ActorID:       req.ActorID,
		AgentID:       "agent-1",
		BusinessID:    req.BusinessID,
		DivisionID:    req.DivisionID,
		Action:        control.ActionToolCall,
		Resource:      tool,
		ResourceType:  control.ResourceTypeCapability,
		ToolID:        tool,
		Operation:     "execute",
	})
	out := &executor.Outcome{TaskID: req.TaskID, CorrelationID: req.CorrelationID,
		BusinessID: req.BusinessID}
	switch {
	case err != nil:
		out.Status = "denied"
		out.Error = err.Error()
	case adm.Allowed():
		h.mu.Lock()
		h.effects++
		h.mu.Unlock()
		out.Status, out.Output = "completed", "effect applied"
	case adm.AwaitingApproval():
		out.Status = "pending_approval"
		out.ApprovalID = adm.ApprovalID
		out.Error = "governance requires approval"
	case adm.Escalated():
		out.Status = "escalated"
		out.EscalationRef = adm.EscalationID
		out.Error = "governance escalated"
	default:
		out.Status = "denied"
		out.Error = "governance denied: " + adm.Decision.Reason
	}
	return out, nil
}

func (h *actionGateHandler) effectCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.effects
}

func actionPolicy(id string, effect governance.Outcome, tool string,
	constraints []governance.Constraint) *governance.Policy {
	now := time.Now().UTC()
	p := &governance.Policy{
		SchemaVersion: schema.Version, EntityType: "policy", PolicyID: id, PolicyVersion: "1",
		PolicyType: governance.PolicyTypeAccessControl, Name: id, Description: id,
		Status:   governance.PolicyStatusActive,
		Subject:  governance.Subject{SubjectType: "all"},
		Action:   governance.Action{ActionType: "custom", ActionIDs: []string{control.ActionToolCall}},
		Resource: governance.Resource{ResourceType: control.ResourceTypeCapability, ResourceIDs: []string{tool}},
		Effect:   effect, Precedence: 1000, Constraints: constraints,
		ApprovalConfig: &governance.ApprovalConfig{ApproverType: "human",
			ApproverIDs: []string{"approver-1"}, TimeoutSeconds: 600,
			AutoDenyOnTimeout: true, SelfApprovalProhibited: true},
		EffectiveFrom: now, CreatedAt: now, CreatedBy: "test",
		Provenance: schema.ProvenanceRef{Origin: "system", Producer: "test", ProducedAt: now},
	}
	return p
}

// newActionControlEngine boots a running engine whose governance evaluates the
// given policies for tool calls (the shipped permissive policy is always kept so
// the request/task gates stay allowed).
func newActionControlEngine(t *testing.T, h *actionGateHandler, policies ...*governance.Policy) *Engine {
	t.Helper()
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	e.Governance().SetPolicies(append([]*governance.Policy{e.Governance().Policies()[0]}, policies...))
	if err := e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.engine = e
	t.Cleanup(func() { _ = e.Stop(context.Background()) })
	return e
}

func submitActionRequest(t *testing.T, e *Engine, h *actionGateHandler, id, actor string) {
	t.Helper()
	if err := e.SubmitRequest(&Request{
		ID:      id,
		Context: NewRequestContext(id, "biz-1", actor),
		Intent:  "run the gated action",
		Handler: h.handle,
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
}

func awaitResult(t *testing.T, e *Engine, id string) *Response {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if res, ok := e.GetResult(id); ok {
			return res
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no result for %s", id)
	return nil
}

func approvalIDFrom(res *Response) string {
	if res.Error == nil || res.Error.Details == nil {
		return ""
	}
	return res.Error.Details["approval_id"]
}

func TestActionApprovalStopsExecutionUntilGovernanceReEvaluates(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))

	submitActionRequest(t, e, h, "exec-approve", "user-1")
	res := awaitResult(t, e, "exec-approve")
	if res.Status != "failed" || res.Error == nil || res.Error.Code != "APPROVAL_REQUIRED" {
		t.Fatalf("the action must stop as APPROVAL_REQUIRED, got %+v", res.Error)
	}
	approvalID := approvalIDFrom(res)
	if approvalID == "" {
		t.Fatalf("the response must carry the approval id: %+v", res.Error)
	}
	if h.effectCount() != 0 {
		t.Fatalf("approval-pending must not apply the effect, got %d", h.effectCount())
	}
	// It is visible on the existing approval surface for this business only.
	if records := e.ListApprovals("biz-1"); len(records) != 1 {
		t.Fatalf("exactly one pending approval expected: %+v", records)
	} else if records[0].EntityID != approvalID {
		t.Fatalf("the listed approval must be the handler's own: %+v", records[0])
	} else if records[0].RequestedAction != control.ActionToolCall {
		t.Fatalf("the record must name the gated action: %+v", records[0])
	}
	if len(e.ListApprovals("biz-2")) != 0 {
		t.Fatal("another business must not see this approval")
	}

	// An unauthorized approver and the requester itself are both refused.
	if err := e.ApproveRequest(approvalID, "biz-1", "intruder", ""); !errors.Is(err, ErrApproverUnauthorized) {
		t.Fatalf("an unauthorized approver must fail, got %v", err)
	}
	if err := e.ApproveRequest(approvalID, "biz-1", "user-1", ""); !errors.Is(err, ErrSelfApprovalProhibited) {
		t.Fatalf("self-approval must fail, got %v", err)
	}
	if err := e.ApproveRequest(approvalID, "biz-2", "approver-1", ""); !errors.Is(err, ErrScopeMismatch) {
		t.Fatalf("a foreign scope must fail, got %v", err)
	}
	if h.effectCount() != 0 {
		t.Fatalf("no refused decision may apply the effect, got %d", h.effectCount())
	}

	// The authorized approver resumes; the resume re-evaluates governance.
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", "reviewed"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	res = awaitResult(t, e, "exec-approve")
	if res.Status != "completed" {
		t.Fatalf("the resumed run must complete, got %s (%v)", res.Status, res.Error)
	}
	if h.effectCount() != 1 {
		t.Fatalf("exactly one effect may be applied, got %d", h.effectCount())
	}
	if len(e.ListApprovals("biz-1")) != 0 {
		t.Fatal("a spent approval must not stay pending")
	}
}

func TestDeniedApprovalLeavesTheActionBlocked(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	submitActionRequest(t, e, h, "exec-deny-approval", "user-1")
	res := awaitResult(t, e, "exec-deny-approval")
	approvalID := approvalIDFrom(res)
	if approvalID == "" {
		t.Fatalf("expected an approval id: %+v", res.Error)
	}
	if err := e.DenyRequest(approvalID, "biz-1", "approver-1", "not now"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if h.effectCount() != 0 {
		t.Fatalf("a denied approval must not apply the effect, got %d", h.effectCount())
	}
	// The record is resolved: it cannot be approved afterwards, and it never
	// resumed anything.
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("a denied approval must stay unapprovable, got %v", err)
	}
	if len(e.ListApprovals("biz-1")) != 0 {
		t.Fatal("a denied approval must not remain listed")
	}
}

func TestExpiredApprovalCannotAuthorizeExecution(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	submitActionRequest(t, e, h, "exec-stale", "user-1")
	res := awaitResult(t, e, "exec-stale")
	approvalID := approvalIDFrom(res)
	if approvalID == "" {
		t.Fatalf("expected an approval id: %+v", res.Error)
	}
	// The record ages past its timeout. INV-16: silence is denial.
	e.approvalMu.Lock()
	e.approvals[approvalID].ar.RequestedAt = e.now().Add(-2 * time.Hour)
	e.approvalMu.Unlock()

	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("an expired approval must not unlock execution, got %v", err)
	}
	if h.effectCount() != 0 {
		t.Fatalf("an expired approval must not apply the effect, got %d", h.effectCount())
	}
	if len(e.ListApprovals("biz-1")) != 0 {
		t.Fatal("an expired approval must be swept, not listed")
	}
}

func TestChangedActionAfterApprovalRequiresFreshGovernance(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	// Only the first tool is gated: a resume that proposes a DIFFERENT action
	// falls into the permissive policy and is evaluated on its own merits.
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	req := &Request{
		ID:      "exec-changed",
		Context: NewRequestContext("exec-changed", "biz-1", "user-1"),
		Intent:  "run the gated action",
		// The resumed run proposes a different tool than the approved one.
		Input:   map[string]string{"action_tool": "filesystem.write"},
		Handler: h.handle,
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatal(err)
	}
	// filesystem.write is not gated, so this run is allowed immediately — proof
	// that the gate is per-action and not a blanket flag.
	res := awaitResult(t, e, "exec-changed")
	if res.Status != "completed" {
		t.Fatalf("an ungated action must run, got %s (%v)", res.Status, res.Error)
	}
	if h.effectCount() != 1 {
		t.Fatalf("the ungated action must have applied its effect, got %d", h.effectCount())
	}
}

func TestChangedGatedActionAfterApprovalNeedsItsOwnApproval(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	// Both tools are gated, but with DIFFERENT policies: approving one must not
	// admit the other.
	e := newActionControlEngine(t, h,
		actionPolicy("p-gate-1", governance.REQUIRE_APPROVAL, "http.request", nil),
		actionPolicy("p-gate-2", governance.REQUIRE_APPROVAL, "filesystem.write", nil))
	if err := e.SubmitRequest(&Request{
		ID: "exec-mismatch", Context: NewRequestContext("exec-mismatch", "biz-1", "user-1"),
		Intent: "gate A", Input: map[string]string{"action_tool": "http.request"}, Handler: h.handle,
	}); err != nil {
		t.Fatal(err)
	}
	res := awaitResult(t, e, "exec-mismatch")
	approvalID := approvalIDFrom(res)
	if approvalID == "" {
		t.Fatalf("expected an approval id: %+v", res.Error)
	}
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// The resumed run proposes the OTHER gated action. Its fingerprint differs, so
	// the approval cannot authorize it: it stops again with a NEW approval.
	e.approvalMu.Lock()
	e.approvals[approvalID].req.Input["action_tool"] = "filesystem.write"
	e.approvalMu.Unlock()
	res = awaitResult(t, e, "exec-mismatch")
	if res.Status != "failed" || res.Error == nil || res.Error.Code != "APPROVAL_REQUIRED" {
		t.Fatalf("the changed action must need fresh approval, got %+v", res.Error)
	}
	if newID := approvalIDFrom(res); newID == "" || newID == approvalID {
		t.Fatalf("a fresh approval record must be opened, got %q", newID)
	}
	if h.effectCount() != 0 {
		t.Fatalf("no effect may be applied while a change awaits approval, got %d", h.effectCount())
	}
}

func TestApprovalIsSingleUseForItsProposal(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	if err := e.SubmitRequest(&Request{
		ID: "exec-twice", Context: NewRequestContext("exec-twice", "biz-1", "user-1"),
		Intent: "gate", Handler: h.handle,
	}); err != nil {
		t.Fatal(err)
	}
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-twice"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if res := awaitResult(t, e, "exec-twice"); res.Status != "completed" {
		t.Fatalf("the resumed run must complete: %+v", res.Error)
	}
	// A later admission of the same proposal in the same execution finds a spent
	// record: the controller treats it as no approval at all.
	p := control.Proposal{CorrelationID: "exec-twice", ExecutionID: "exec-twice",
		ActorID: "user-1", AgentID: "agent-1", BusinessID: "biz-1",
		Action: control.ActionToolCall, Resource: "http.request",
		ResourceType: control.ResourceTypeCapability, ToolID: "http.request", Operation: "execute"}
	if state, ok := e.ApprovedState(p, p.Fingerprint()); ok {
		t.Fatalf("a spent approval must not be reported as usable, got %s", state)
	}
}

func TestExplicitDenyIsNeverOverriddenByAnApproval(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h,
		actionPolicy("p-approve", governance.REQUIRE_APPROVAL, "http.request", nil),
		actionPolicy("p-deny", governance.DENY, "http.request", nil))
	if err := e.SubmitRequest(&Request{
		ID: "exec-deny", Context: NewRequestContext("exec-deny", "biz-1", "user-1"),
		Intent: "denied action", Handler: h.handle,
	}); err != nil {
		t.Fatal(err)
	}
	res := awaitResult(t, e, "exec-deny")
	if res.Status != "failed" || res.Error == nil || res.Error.Code != "POLICY_DENIED" {
		t.Fatalf("the denial must surface as POLICY_DENIED, got %+v", res.Error)
	}
	if approvalIDFrom(res) != "" {
		t.Fatal("a denied action must not open an approval")
	}
	if h.effectCount() != 0 {
		t.Fatalf("a denied action must not apply the effect, got %d", h.effectCount())
	}
}

func TestEscalationBlocksTheActionAndReachesAttention(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-esc", governance.ESCALATE, "http.request", nil))
	if err := e.SubmitRequest(&Request{
		ID: "exec-esc", Context: NewRequestContext("exec-esc", "biz-1", "user-1"),
		Intent: "escalated action", Handler: h.handle,
	}); err != nil {
		t.Fatal(err)
	}
	res := awaitResult(t, e, "exec-esc")
	if res.Status != "failed" || res.Error == nil || res.Error.Code != "ESCALATION_REQUIRED" {
		t.Fatalf("escalation must surface as ESCALATION_REQUIRED, got %+v", res.Error)
	}
	escalationRef := res.Error.Details["escalation_ref"]
	if escalationRef == "" {
		t.Fatalf("the response must carry the escalation ref: %+v", res.Error.Details)
	}
	if h.effectCount() != 0 {
		t.Fatalf("an escalated action must not apply the effect, got %d", h.effectCount())
	}
	// The existing escalation queue received it exactly once (the handler's own
	// escalation is reused, not duplicated by the chain). Delivery is the bus's
	// dispatch step, which the gateway's dispatch loop drives in production; the
	// test drives it directly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, _ = e.EventBus().Dispatch()
		if _, ok := e.escalations.get(escalationRef); ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the escalation never reached the existing escalation queue")
}

func TestUnboundActionApprovalIsRefused(t *testing.T) {
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	// No parent request exists for this correlation, so the approval has nothing
	// to resume: the proposal is refused rather than recorded into the void.
	p := control.Proposal{CorrelationID: "no-such-request", ActorID: "user-1", BusinessID: "biz-1",
		Action: control.ActionToolCall, Resource: "http.request",
		ResourceType: control.ResourceTypeCapability, ToolID: "http.request", Operation: "execute"}
	if _, err := e.RequestApproval(p, governance.Decision{Outcome: governance.REQUIRE_APPROVAL}); !errors.Is(err, ErrActionProposalUnbound) {
		t.Fatalf("an unbound approval must be refused, got %v", err)
	}
}

func TestActionApprovalIsProcessLocalAcrossRestart(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	if err := e.SubmitRequest(&Request{
		ID: "exec-restart", Context: NewRequestContext("exec-restart", "biz-1", "user-1"),
		Intent: "gate", Handler: h.handle,
	}); err != nil {
		t.Fatal(err)
	}
	approvalID := approvalIDFrom(awaitResult(t, e, "exec-restart"))
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}
	// G4: approvals are process-local. A restarted installation has neither the
	// approval nor the execution, and says so instead of inventing continuity.
	restarted, err := NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Stop(context.Background()) }()
	if err := restarted.ApproveRequest(approvalID, "biz-1", "approver-1", ""); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("a restarted process must not know the old approval, got %v", err)
	}
	if _, ok := restarted.GetResult("exec-restart"); ok {
		t.Fatal("a restarted process must not resurrect the old execution")
	}
	if _, ok := restarted.Pending("exec-restart"); ok {
		t.Fatal("a restarted process must not resurrect the old execution as pending")
	}
	if len(restarted.ListApprovals("biz-1")) != 0 {
		t.Fatal("a restarted process must not list the old approval")
	}
}

func TestActionApprovalEventsAreMetadataOnly(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h, actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))

	// Capture the governance/approval facts on the EXISTING bus.
	var mu sync.Mutex
	var captured []*event.Event
	if _, err := e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		mu.Lock()
		captured = append(captured, ev)
		mu.Unlock()
		return nil
	}), event.EventTypeApprovalRequested, event.EventTypeGovernanceDecided,
		event.EventType("governance.action_proposed"), event.EventType("governance.constraint_applied"),
	); err != nil {
		t.Fatal(err)
	}

	if err := e.SubmitRequest(&Request{
		ID: "exec-events", Context: NewRequestContext("exec-events", "biz-1", "user-1"),
		Intent: "gate", Handler: h.handle,
	}); err != nil {
		t.Fatal(err)
	}
	awaitResult(t, e, "exec-events")
	// The bus delivers on dispatch (the gateway drives it in production).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, _ = e.EventBus().Dispatch()
		mu.Lock()
		n := len(captured)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	kinds := map[string]bool{}
	for _, ev := range captured {
		kinds[string(ev.Type)] = true
		for _, forbidden := range []string{"bearer ", "sk-", "password", "prompt", "chain-of-thought"} {
			if strings.Contains(strings.ToLower(string(ev.Data)), forbidden) {
				t.Fatalf("event %s leaked %q: %s", ev.Type, forbidden, ev.Data)
			}
		}
	}
	for _, want := range []string{"governance.action_proposed", "approval.requested"} {
		if !kinds[want] {
			t.Fatalf("expected a %s fact, got %v", want, kinds)
		}
	}
}
