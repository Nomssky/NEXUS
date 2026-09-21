// Package core implements the NEXUS Core Runtime — the orchestrator that wires
// foundation packages into a cohesive, running system.
//
// The Core Runtime provides:
//   - Bootstrap sequence (dependency injection, component wiring)
//   - Canonical execution chain (request → objective → decision → plan → workflow → task → outcome)
//   - Request context propagation (correlation_id, business_id, objective_id, why_chain)
//   - Graceful shutdown and recovery
//
// Key invariants:
//   - WHY preserved through entire chain
//   - Correlation ID propagated everywhere
//   - Business isolation enforced at every boundary
//   - Governance checked before execution
//   - No autonomous action without owner intent
package core

import (
	"fmt"
	"time"
)

// RequestContext carries context through the entire execution chain.
// Every contract must propagate this context.
type RequestContext struct {
	// CorrelationID is a globally unique identifier for tracing.
	// Generated at entry point, propagated through all calls.
	CorrelationID string `json:"correlation_id"`

	// BusinessID identifies the business scope.
	BusinessID string `json:"business_id"`

	// DivisionID is an optional sub-scope within the business.
	DivisionID string `json:"division_id,omitempty"`

	// ObjectiveID tracks which objective this request serves.
	ObjectiveID string `json:"objective_id,omitempty"`

	// WhyChain preserves the chain of reasoning from mission to current task.
	// WHY is never discarded without explicit Governance approval.
	WhyChain []string `json:"why_chain,omitempty"`

	// ActorID identifies who initiated this request.
	ActorID string `json:"actor_id"`

	// Timeout is the caller-owned deadline for this request.
	Timeout time.Duration `json:"timeout,omitempty"`

	// CreatedAt records when this context was created.
	CreatedAt time.Time `json:"created_at"`
}

// NewRequestContext creates a new request context with required fields.
func NewRequestContext(correlationID, businessID, actorID string) *RequestContext {
	return &RequestContext{
		CorrelationID: correlationID,
		BusinessID:    businessID,
		ActorID:       actorID,
		CreatedAt:     time.Now(),
	}
}

// WithObjective adds an objective to the context and extends the why-chain.
func (rc *RequestContext) WithObjective(objectiveID, why string) *RequestContext {
	rc.ObjectiveID = objectiveID
	rc.WhyChain = append(rc.WhyChain, why)
	return rc
}

// WithDivision adds a division scope.
func (rc *RequestContext) WithDivision(divisionID string) *RequestContext {
	rc.DivisionID = divisionID
	return rc
}

// WithTimeout sets the caller-owned timeout.
func (rc *RequestContext) WithTimeout(timeout time.Duration) *RequestContext {
	rc.Timeout = timeout
	return rc
}

// Clone creates a deep copy of the context.
func (rc *RequestContext) Clone() *RequestContext {
	clone := &RequestContext{
		CorrelationID: rc.CorrelationID,
		BusinessID:    rc.BusinessID,
		DivisionID:    rc.DivisionID,
		ObjectiveID:   rc.ObjectiveID,
		ActorID:       rc.ActorID,
		Timeout:       rc.Timeout,
		CreatedAt:     rc.CreatedAt,
	}
	if len(rc.WhyChain) > 0 {
		clone.WhyChain = make([]string, len(rc.WhyChain))
		copy(clone.WhyChain, rc.WhyChain)
	}
	return clone
}

// String returns a summary of the context for logging.
func (rc *RequestContext) String() string {
	return fmt.Sprintf("corr=%s biz=%s obj=%s actor=%s", rc.CorrelationID, rc.BusinessID, rc.ObjectiveID, rc.ActorID)
}

// Request represents an incoming owner request to the NEXUS system.
type Request struct {
	// ID is the unique request identifier.
	ID string `json:"id"`

	// Context carries the request context.
	Context *RequestContext `json:"context"`

	// Intent is the owner's stated intent (natural language).
	Intent string `json:"intent"`

	// Priority is the request priority (0-10, 10 highest).
	Priority int `json:"priority"`

	// Constraints are any constraints on how this request should be fulfilled.
	Constraints []string `json:"constraints,omitempty"`

	// Deadline is when this request must be completed by.
	Deadline *time.Time `json:"deadline,omitempty"`
}

// Response represents the outcome of processing a request through the chain.
type Response struct {
	// RequestID links back to the original request.
	RequestID string `json:"request_id"`

	// BusinessID identifies the business scope of the request.
	// Used for authorization checks on result retrieval.
	BusinessID string `json:"business_id"`

	// Status is the final status: completed, failed, cancelled, escalated.
	Status string `json:"status"`

	// Outcome is the structured outcome of the request.
	Outcome *Outcome `json:"outcome,omitempty"`

	// Error provides failure details if status is failed.
	Error *ChainError `json:"error,omitempty"`

	// AuditTrail captures the full execution path.
	AuditTrail []AuditEntry `json:"audit_trace"`

	// Duration is total processing time.
	Duration time.Duration `json:"duration"`
}

// Outcome represents the structured result of a completed request.
type Outcome struct {
	// Summary is a human-readable summary of what was accomplished.
	Summary string `json:"summary"`

	// Artifacts are any artifacts produced (files, data, etc).
	Artifacts []string `json:"artifacts,omitempty"`

	// Metrics captures performance metrics.
	Metrics map[string]interface{} `json:"metrics,omitempty"`
}

// ChainError represents an error that occurred in the execution chain.
type ChainError struct {
	// Code is the machine-readable error code.
	Code string `json:"code"`

	// Category is the error category (from contracts).
	Category string `json:"category"`

	// Message is the human-readable description.
	Message string `json:"message"`

	// Retryable indicates if the caller should retry.
	Retryable bool `json:"retryable"`

	// ChainStep identifies where in the chain the error occurred.
	ChainStep string `json:"chain_step"`

	// CorrelationID links the error to a specific request for tracing.
	CorrelationID string `json:"correlation_id,omitempty"`

	// Timestamp records when this error occurred.
	Timestamp time.Time `json:"timestamp"`
}

// Error implements the error interface.
func (e *ChainError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Category, e.ChainStep, e.Message)
}

// AuditEntry captures a single step in the execution chain.
type AuditEntry struct {
	// Step is the chain step name.
	Step string `json:"step"`

	// Action is what was done.
	Action string `json:"action"`

	// Actor is who/what performed the action.
	Actor string `json:"actor"`

	// Timestamp is when this entry was recorded.
	Timestamp time.Time `json:"timestamp"`

	// Duration is how long this step took.
	Duration time.Duration `json:"duration"`

	// Outcome is the result of this step.
	Outcome string `json:"outcome"`
}
