package capability

import (
	"context"
	"errors"
	"fmt"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// ErrUnknownOutcome marks a mutation whose external system cannot be proven
// to have either committed or not: the request was dispatched, no response
// arrived, and no idempotency key could confirm the final state. It is a
// first-class *outcome*, not a tool failure.
var ErrUnknownOutcome = errors.New("unknown outcome")

// ErrCapabilityDisabled is the lifecycle refusal of contract §10: a disabled
// capability is rejected before any adapter dispatch, so no effect of any kind
// can be observed from it.
var ErrCapabilityDisabled = errors.New("capability_disabled")

// DecideRetry is the one centralized verdict on the retry question. Code
// elsewhere must never invent its own retry rule (contract §6). Only
// deterministic transport transients for read-class tools may retry once;
// mutations, cancellations, timeouts, unknown outcomes and every classified
// rejection fail closed.
func DecideRetry(err error, manifest tool.ToolManifest, attemptsMade int) bool {
	if attemptsMade >= attemptBound(manifest) {
		return false // the attempt budget is spent; §2 bounds it per call
	}
	return RetryRecommended(err, manifest)
}

// RetryRecommended is the class-only half of DecideRetry: it answers "would a
// fresh attempt of this operation plausibly succeed?" without the per-call
// bound. It is the value surfaced in the observation (contract §8) and it is
// the reason an `unknown` outcome can never carry a retry recommendation
// (§3): an unknown outcome is not an error class that a retry may re-drive.
func RetryRecommended(err error, manifest tool.ToolManifest) bool {
	if err == nil {
		return false
	}
	// §6: only read-class operations retry automatically, and only for
	// deterministic transport transients. Mutations, cancellations, unknown
	// outcomes and every classified rejection fail closed.
	if manifest.SideEffectClass != tool.SideEffectRead {
		return false
	}
	switch {
	case errors.Is(err, ErrExternal), errors.Is(err, ErrTimeout),
		errors.Is(err, ErrNetworkBlocked), errors.Is(err, context.DeadlineExceeded):
		// A read that timed out on transport is a deterministic transient; the
		// deadline-fit check in runAttempts is what stops the retry loop.
		return true
	default:
		return false
	}
}

// attemptBound is the maximum number of physical attempts for one logical
// invocation: 2 for read-class (one retry), 1 for mutations.
func attemptBound(manifest tool.ToolManifest) int {
	switch manifest.SideEffectClass {
	case tool.SideEffectRead:
		return 2
	default:
		return 1
	}
}

// classifyOutcome maps the final error + caller context into the contract
// token: completed / failed / cancelled / timed_out / unknown (contract §5).
// A nil error AND a live caller context yields completed.
func classifyOutcome(err error, ctxErr error) string {
	switch {
	case err == nil && ctxErr == nil:
		return "completed"
	case errors.Is(err, context.Canceled), ctxErr == context.Canceled:
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded),
		ctxErr == context.DeadlineExceeded,
		errors.Is(err, ErrTimeout):
		return "timed_out"
	case errors.Is(err, ErrUnknownOutcome):
		return "unknown"
	default:
		return "failed"
	}
}

// errorClass returns the canonical tag carried in Result.Error / evidence
// payloads. It is the contract taxonomy of §16.
func errorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrCapabilityDisabled):
		return "capability_disabled"
	case errors.Is(err, ErrValidation):
		return "validation"
	case errors.Is(err, ErrPermission):
		return "permission denied"
	case errors.Is(err, ErrScope):
		return "scope denied"
	case errors.Is(err, ErrCredential):
		return "credential unavailable"
	case errors.Is(err, ErrNetworkBlocked):
		return "network blocked"
	case errors.Is(err, ErrTimeout):
		return "timeout"
	case errors.Is(err, ErrCancelled):
		return "cancelled"
	case errors.Is(err, ErrResourceLimit):
		return "resource limit"
	case errors.Is(err, ErrUnknownOutcome):
		return "unknown outcome"
	case errors.Is(err, ErrExternal):
		return "external error"
	default:
		return "internal adapter failure"
	}
}

// CapabilityState is the lifecycle state of one registered capability
// (OPERATIONAL_RELIABILITY_CONTRACTS §10). States map to a simple allowed
// transition graph; the Zero value used by the platform since no operator
// has interacted with it yet is CapabilityEnabled (boot semantics: the only
// tools that pass bootstrap are the finite set in which all shipped ones
// are enabled).
type CapabilityState string

const (
	CapabilityRegistered CapabilityState = "registered"
	CapabilityEnabled    CapabilityState = "enabled"
	CapabilityDisabled   CapabilityState = "disabled"
	CapabilityDeprecated CapabilityState = "deprecated"
)

// allowedTransitions encodes §10: registered -> enabled; enabled -> disabled
// or deprecated; disabled -> enabled; deprecated -> enabled or disabled.
var allowedTransitions = map[CapabilityState]map[CapabilityState]bool{
	CapabilityRegistered: {CapabilityEnabled: true, CapabilityDisabled: true},
	CapabilityEnabled:    {CapabilityDisabled: true, CapabilityDeprecated: true},
	CapabilityDisabled:   {CapabilityEnabled: true},
	CapabilityDeprecated: {CapabilityEnabled: true, CapabilityDisabled: true},
}

// CapabilityState returns the current state for a tool, or CapabilityEnabled
// when the operator has never interacted with it (existing behaviour).
func (p *Platform) CapabilityState(toolID string) CapabilityState {
	p.lifecycleMu.RLock()
	defer p.lifecycleMu.RUnlock()
	if v, ok := p.lifecycle[toolID]; ok {
		return v
	}
	return CapabilityEnabled
}

// SetCapabilityState applies an allowed transition. It fails closed when the
// transition is not in the graph; no state change applies on an error.
func (p *Platform) SetCapabilityState(toolID string, next CapabilityState) error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	cur := p.lifecycle[toolID]
	if cur == "" {
		cur = CapabilityEnabled
	}
	if transition, ok := allowedTransitions[cur]; !ok || transition[next] == false {
		return fmt.Errorf("capability %s: invalid transition %s -> %s", toolID, cur, next)
	}
	p.lifecycle[toolID] = next
	return nil
}

// IdempotencyCapable is implemented by adapters that accept a stable
// idempotency key for their mutations (contract §7). Discovery reports it so a
// caller can reason about duplicate suppression without guessing.
type IdempotencyCapable interface {
	SupportsIdempotency() bool
}

// ReconciliationCapable is implemented by adapters that can later resolve an
// unknown outcome (e.g. by reading back the created object). NEXUS never runs
// background reconciliation workers: advertising it only surfaces the
// reconciliation requirement in the observation and telemetry (contract §9).
type ReconciliationCapable interface {
	SupportsReconciliation() bool
}

// RetryPolicy is the discovery-visible retry posture of one capability.
const (
	RetryPolicyNone        = "none"
	RetryPolicyBoundedRead = "bounded_read_retry"
)

// Reliability is the bounded, non-secret reliability metadata for one
// capability (contract §8 discovery). It describes posture only: it grants
// nothing and reveals no topology, credentials or roots.
type Reliability struct {
	ToolID                  string          `json:"tool_id"`
	State                   CapabilityState `json:"capability_state"`
	SupportsIdempotency     bool            `json:"supports_idempotency"`
	SupportsReconciliation  bool            `json:"supports_reconciliation"`
	RetryPolicy             string          `json:"retry_policy"`
	MaxAttempts             int             `json:"max_attempts"`
	SideEffectClass         string          `json:"side_effect_class"`
	MaxDurationMS           int64           `json:"max_duration_ms"`
	MaxOutputBytes          int             `json:"max_output_bytes"`
	ReconciliationAvailable bool            `json:"reconciliation_available"`
}

// ReliabilityFor returns the discovery metadata for one registered manifest.
// Everything is derived from the manifest, the lifecycle map and what the
// adapter itself advertises; nothing is inferred from a tool's name.
func ReliabilityFor(manifest tool.ToolManifest, state CapabilityState, adapter tool.Adapter) Reliability {
	r := Reliability{
		ToolID:          manifest.ID,
		State:           state,
		SideEffectClass: string(manifest.SideEffectClass),
		MaxAttempts:     attemptBound(manifest),
		RetryPolicy:     RetryPolicyNone,
	}
	if r.MaxAttempts > 1 {
		r.RetryPolicy = RetryPolicyBoundedRead
	}
	if a, ok := adapter.(IdempotencyCapable); ok && a.SupportsIdempotency() {
		r.SupportsIdempotency = true
	}
	if a, ok := adapter.(ReconciliationCapable); ok && a.SupportsReconciliation() {
		r.SupportsReconciliation = true
		r.ReconciliationAvailable = true
	}
	if manifest.ResourceLimits.MaxDuration > 0 {
		r.MaxDurationMS = manifest.ResourceLimits.MaxDuration.Milliseconds()
	}
	r.MaxOutputBytes = manifest.ResourceLimits.MaxOutputByte
	return r
}

// adapterSupportsReconciliation reports whether an adapter can resolve an
// unknown outcome on demand (contract §9).
func adapterSupportsReconciliation(adapter tool.Adapter) bool {
	a, ok := adapter.(ReconciliationCapable)
	return ok && a.SupportsReconciliation()
}

// invocationEventForOutcome maps a terminal outcome onto the event emitted at
// its completion tag (contract §10). Completed uses the historical
// invocation_completed frame (no churn for consumers); every distinguished
// failure outcome gets its own emission class.
func invocationEventForOutcome(outcome string) event.EventType {
	switch outcome {
	case "cancelled":
		return event.EventTypeToolInvocationCancelled
	case "timed_out":
		return event.EventTypeToolInvocationTimedOut
	case "unknown":
		return event.EventTypeToolInvocationUnknownOutcome
	default:
		return event.EventTypeToolInvocationFailed
	}
}
