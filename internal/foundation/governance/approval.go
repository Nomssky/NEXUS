package governance

import (
	"fmt"
	"sync"
	"time"
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
// Returns an error if self-approval is attempted.
func (ae *ApprovalEngine) RequestApproval(decision Decision, req Request, config ApprovalConfig) (*ApprovalRequest, error) {
	now := ae.now()

	// No self-approval: the requester cannot approve their own action
	if config.SelfApprovalProhibited && req.Actor == "" {
		return nil, fmt.Errorf("self-approval prohibited: requester identity required")
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
		return fmt.Errorf("approval request %s not found", decisionID)
	}

	if ar.Status != ApprovalStatePending {
		return fmt.Errorf("approval request %s is not pending (status: %s)", decisionID, ar.Status)
	}

	// No self-approval
	if ar.Config.SelfApprovalProhibited && approver == ar.Requester {
		return fmt.Errorf("self-approval prohibited: approver %s is the requester", approver)
	}

	// Check if approver is authorized
	if !ae.isAuthorizedApprover(ar.Config, approver) {
		return fmt.Errorf("approver %s is not authorized for this approval", approver)
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
		return fmt.Errorf("approval request %s not found", decisionID)
	}

	if ar.Status != ApprovalStatePending {
		return fmt.Errorf("approval request %s is not pending (status: %s)", decisionID, ar.Status)
	}

	now := ae.now()
	ar.Status = ApprovalStateDenied
	ar.Approver = approver
	ar.Reason = reason
	ar.ResolvedAt = &now

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
