package governance

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Typed approval errors so callers (core resume path, gateway handlers)
// can classify failures without string matching. Additive: all methods keep
// returning non-nil errors in the same situations as before.
var (
	// ErrApprovalNotFound: no approval request with that decision ID.
	ErrApprovalNotFound = errors.New("approval request not found")
	// ErrApprovalNotPending: approval already resolved (approved/denied).
	ErrApprovalNotPending = errors.New("approval request not pending")
	// ErrSelfApprovalProhibited: requester tried to decide own request.
	ErrSelfApprovalProhibited = errors.New("self-approval prohibited")
	// ErrApproverUnauthorized: approver not in the policy's approver list.
	ErrApproverUnauthorized = errors.New("approver not authorized")
)

// ApprovalRequest represents a pending approval for a governance decision.
type ApprovalRequest struct {
	// DecisionID uniquely identifies this approval request.
	DecisionID string `json:"decision_id"`
	// Request is the original governance request.
	Request Request `json:"request"`
	// Decision is the governance decision that requires approval.
	Decision Decision `json:"decision"`
	// Config is the approval configuration from the matched policy.
	Config ApprovalConfig `json:"config"`
	// Status is the current approval status.
	Status ApprovalState `json:"status"`
	// Requester is the actor who made the original request.
	Requester string `json:"requester"`
	// Approver is the actor who approved/denied (empty if pending).
	Approver string `json:"approver,omitempty"`
	// RequestedAt is when the approval was requested.
	RequestedAt time.Time `json:"requested_at"`
	// ResolvedAt is when the approval was resolved (empty if pending).
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	// Reason is the approver's reason for approval/denial.
	Reason string `json:"reason,omitempty"`
}

// ApprovalEngine manages approval workflows for governance decisions.
type ApprovalEngine struct {
	mu      sync.RWMutex
	pending map[string]*ApprovalRequest // decisionID -> request
	now     func() time.Time
}

// NewApprovalEngine creates a new approval engine.
func NewApprovalEngine() *ApprovalEngine {
	return &ApprovalEngine{
		pending: make(map[string]*ApprovalRequest),
		now:     time.Now,
	}
}

// NewApprovalEngineWithClock creates a new approval engine with an injectable clock.
func NewApprovalEngineWithClock(now func() time.Time) *ApprovalEngine {
	return &ApprovalEngine{
		pending: make(map[string]*ApprovalRequest),
		now:     now,
	}
}

// RequestApproval creates an approval request for a governance decision.
// When SelfApprovalProhibited is set, a non-empty requester identity is
// required so Approve() can later enforce approver != requester (A-019:
// this stage is an identity prerequisite, not the self-approval gate —
// the actual invariant lives in Approve).
func (ae *ApprovalEngine) RequestApproval(decision Decision, req Request, config ApprovalConfig) (*ApprovalRequest, error) {
	now := ae.now()

	// Identity prerequisite for self-approval prevention (A-019): when
	// SelfApprovalProhibited is set, Approve() compares approver to this
	// requester — empty identity would make that comparison meaningless.
	if config.SelfApprovalProhibited && req.Actor == "" {
		return nil, fmt.Errorf("%w: requester identity required", ErrSelfApprovalProhibited)
	}

	ar := &ApprovalRequest{
		DecisionID:  fmt.Sprintf("apr-%s-%d", req.Actor, now.UnixNano()),
		Request:     req,
		Decision:    decision,
		Config:      config,
		Status:      ApprovalStatePending,
		Requester:   req.Actor,
		RequestedAt: now,
	}

	ae.mu.Lock()
	defer ae.mu.Unlock()
	ae.pending[ar.DecisionID] = ar
	return ar, nil
}

// Approve approves a pending approval request.
// Returns an error if the approver is the requester (self-approval prohibited).
func (ae *ApprovalEngine) Approve(decisionID, approver, reason string) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	ar, ok := ae.pending[decisionID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrApprovalNotFound, decisionID)
	}

	if ar.Status != ApprovalStatePending {
		return fmt.Errorf("%w: %s (status: %s)", ErrApprovalNotPending, decisionID, ar.Status)
	}

	// No self-approval
	if ar.Config.SelfApprovalProhibited && approver == ar.Requester {
		return fmt.Errorf("%w: approver %s is the requester", ErrSelfApprovalProhibited, approver)
	}

	// Check if approver is authorized
	if !ae.isAuthorizedApprover(ar.Config, approver) {
		return fmt.Errorf("%w: %s for approval %s", ErrApproverUnauthorized, approver, decisionID)
	}

	now := ae.now()
	ar.Status = ApprovalStateApproved
	ar.Approver = approver
	ar.Reason = reason
	ar.ResolvedAt = &now

	delete(ae.pending, decisionID)
	return nil
}

// Deny denies a pending approval request.
func (ae *ApprovalEngine) Deny(decisionID, approver, reason string) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	ar, ok := ae.pending[decisionID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrApprovalNotFound, decisionID)
	}

	if ar.Status != ApprovalStatePending {
		return fmt.Errorf("%w: %s (status: %s)", ErrApprovalNotPending, decisionID, ar.Status)
	}

	now := ae.now()
	ar.Status = ApprovalStateDenied
	ar.Approver = approver
	ar.Reason = reason
	ar.ResolvedAt = &now

	delete(ae.pending, decisionID)
	return nil
}

// Revoke drops a pending approval without recording a decision. It exists
// for cancellation: revoking is not a denial by anyone, so it records no
// approver, no reason, and emits nothing — the execution's cancelled outcome
// is the audit trail. Revoking a non-pending record is a no-op mismatch
// (ErrApprovalNotFound), never a state change.
func (ae *ApprovalEngine) Revoke(decisionID string) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	ar, ok := ae.pending[decisionID]
	if !ok || ar.Status != ApprovalStatePending {
		return fmt.Errorf("%w: %s", ErrApprovalNotFound, decisionID)
	}
	delete(ae.pending, decisionID)
	return nil
}

// GetApproval returns the current state of an approval request.
func (ae *ApprovalEngine) GetApproval(decisionID string) (*ApprovalRequest, bool) {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	ar, ok := ae.pending[decisionID]
	return ar, ok
}

// ApprovalDecision is a value snapshot of an approval record's DECISION
// fields. The engine mutates those fields (Status/Approver/Reason/ResolvedAt)
// in place while holding ae.mu; every other reader must take this snapshot
// instead of reading the shared *ApprovalRequest directly. ApprovalState is a
// string, so an unsynchronized read is a data race that can also observe a
// torn or stale decision — e.g. a resume re-admission reading a non-approved
// status after an approve and re-gating a request that was legitimately
// approved.
//
// A record leaves ae.pending on Approve/Deny/CheckTimeouts, but the fields are
// still only written under ae.mu; DecisionOf therefore synchronizes on the
// pointer identity, so callers that already hold the record (core keeps the
// same *ApprovalRequest the engine created) can read it safely afterwards.
type ApprovalDecision struct {
	Status     ApprovalState
	Approver   string
	Reason     string
	ResolvedAt *time.Time
}

// DecisionOf snapshots ar's decision fields under the engine's lock. It is the
// only safe way for a holder of the record pointer to read the mutable fields.
func (ae *ApprovalEngine) DecisionOf(ar *ApprovalRequest) ApprovalDecision {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	return decisionFrom(ar)
}

// Decision snapshots the decision fields of decisionID. ok is false when the
// engine holds no such record (never created, or already revoked).
func (ae *ApprovalEngine) Decision(decisionID string) (ApprovalDecision, bool) {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	ar, ok := ae.pending[decisionID]
	if !ok {
		return ApprovalDecision{}, false
	}
	return decisionFrom(ar), true
}

// decisionFrom copies a record's mutable decision fields. Call only with ae.mu
// held (read or write).
func decisionFrom(ar *ApprovalRequest) ApprovalDecision {
	if ar == nil {
		return ApprovalDecision{}
	}
	d := ApprovalDecision{Status: ar.Status, Approver: ar.Approver, Reason: ar.Reason}
	if ar.ResolvedAt != nil {
		t := *ar.ResolvedAt
		d.ResolvedAt = &t
	}
	return d
}

// PendingApprovals returns all pending approval requests.
func (ae *ApprovalEngine) PendingApprovals() []*ApprovalRequest {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	var out []*ApprovalRequest
	for _, ar := range ae.pending {
		out = append(out, ar)
	}
	return out
}

// CheckTimeouts checks for timed-out approval requests and auto-denes them
// if configured.
func (ae *ApprovalEngine) CheckTimeouts() []*ApprovalRequest {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	now := ae.now()
	var timedOut []*ApprovalRequest

	for id, ar := range ae.pending {
		if ar.Status != ApprovalStatePending {
			continue
		}

		elapsed := now.Sub(ar.RequestedAt).Seconds()
		if elapsed > float64(ar.Config.TimeoutSeconds) {
			if ar.Config.AutoDenyOnTimeout {
				ar.Status = ApprovalStateDenied
				ar.Reason = "auto-denied: approval timeout"
				ar.ResolvedAt = &now
				timedOut = append(timedOut, ar)
				delete(ae.pending, id)
			}
		}
	}

	return timedOut
}

// isAuthorizedApprover checks if an approver is authorized for a given config.
func (ae *ApprovalEngine) isAuthorizedApprover(config ApprovalConfig, approver string) bool {
	// If no specific approvers listed, any authorized actor can approve
	if len(config.ApproverIDs) == 0 {
		return true
	}

	// Check if approver is in the authorized list
	for _, id := range config.ApproverIDs {
		if id == approver {
			return true
		}
	}

	return false
}
