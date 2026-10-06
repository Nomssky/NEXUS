// Package capability implements the Capability & Tool Platform v1 boundary
// (contracts/CAPABILITY_TOOL_CONTRACTS.md): manifest-driven registry lookup,
// scope/permission evaluation, credential resolution, adapter dispatch, bounded
// result normalization and mandatory secret redaction.
//
// It contains exactly ONE invocation path and makes no authorization decisions
// of its own beyond combining the already-established runtime facts: the
// authenticated actor, their business/division membership, the agent's
// allowlist, the tool manifest, the upstream governance decision and the
// capability budgets. There is no RBAC, no role and no owner concept here, and
// the model cannot influence any of it.
package capability

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// Error kinds (contract §16). They are distinguishable and map onto the
// existing NEXUS error envelope at the gateway edge.
var (
	ErrValidation     = errors.New("validation")
	ErrPermission     = errors.New("permission denied")
	ErrScope          = errors.New("scope denied")
	ErrCredential     = errors.New("credential unavailable")
	ErrNetworkBlocked = errors.New("network blocked")
	ErrTimeout        = errors.New("timeout")
	ErrCancelled      = errors.New("cancelled")
	ErrExternal       = errors.New("external error")
	ErrResourceLimit  = errors.New("resource limit")
	ErrInternal       = errors.New("internal adapter failure")
)

// Caps are the runtime's hard capability budgets (contract §8). Manifests and
// callers may only ask for less.
type Caps struct {
	MaxDuration        time.Duration
	MaxOutputBytes     int
	MaxRequestBytes    int
	MaxItems           int
	MaxDepth           int
	MaxNetworkRequests int
	MaxExternalMutates int
	MaxFilesRead       int
	MaxFilesWritten    int
}

// DefaultCaps are the shipped platform defaults.
func DefaultCaps() Caps {
	return Caps{
		MaxDuration:        15 * time.Second,
		MaxOutputBytes:     32 * 1024,
		MaxRequestBytes:    64 * 1024,
		MaxItems:           256,
		MaxDepth:           12,
		MaxNetworkRequests: 64,
		MaxExternalMutates: 16,
		MaxFilesRead:       64,
		MaxFilesWritten:    32,
	}
}

// Request is one tool invocation request from the runtime. Every field is
// runtime-established; none of it can be chosen by a model.
type Request struct {
	ToolID     string
	Operation  string
	Input      map[string]string
	ActorID    string
	BusinessID string
	DivisionID string
	AgentID    string
	// AgentTools is the acting agent's allowlist (agent exec: AllowedTools).
	AgentTools []string
	// CorrelationID ties events/audit to the execution.
	CorrelationID string
}

// Auditor records capability invocations on the existing event bus. It is
// implemented by a small adapter over event.MemBus so the platform never
// grows its own bus.
type Auditor struct {
	Bus *event.MemBus
}

func (a Auditor) emit(ev event.EventType, req Request, extra map[string]string) {
	if a.Bus == nil {
		return
	}
	data := map[string]string{}
	for k, v := range extra {
		data[k] = v
	}
	payload := encodeJSON(data)
	_ = a.Bus.Publish(&event.Event{
		ID:            fmt.Sprintf("%s-%s-%d", req.CorrelationID, ev, time.Now().UnixNano()),
		Type:          ev,
		Source:        "capability",
		Timestamp:     time.Now(),
		BusinessID:    req.BusinessID,
		CorrelationID: req.CorrelationID,
		Priority:      event.PriorityNormal,
		Data:          payload,
	})
}

// CredentialResolver turns a manifest credential requirement into an opaque
// handle. It wraps the existing security.Resolver — the intelligence layer and
// the model never receive a handle, only the adapter does (contract §6).
type CredentialResolver interface {
	Resolve(ctx context.Context, req tool.CredentialRequirement, businessID, divisionID string) (tool.CredentialHandle, error)
}

// Platform is the capability platform: one registry, one permission function,
// one credential boundary, one result pipeline.
type Platform struct {
	Registry    *tool.ToolRegistry
	Memberships *identity.MembershipSet
	Credentials CredentialResolver
	Auditor     Auditor
	Caps        Caps
	Now         func() time.Time
	// secretMu guards secrets, the registered secret values used for
	// redaction. Values never leave this struct (contract §7, §11).
	secretMu sync.RWMutex
	secrets  []string
	// lifecycleMu guards the capability lifecycle map: id -> CapabilityState.
	lifecycleMu sync.RWMutex
	lifecycle   map[string]CapabilityState
}

// New wires a platform with the shipped defaults.
func New(reg *tool.ToolRegistry, members *identity.MembershipSet, creds CredentialResolver, bus *event.MemBus) *Platform {
	return &Platform{
		Registry: reg, Memberships: members, Credentials: creds,
		Auditor: Auditor{Bus: bus}, Caps: DefaultCaps(), Now: time.Now,
		lifecycle: map[string]CapabilityState{},
	}
}

func (p *Platform) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// RegisterSecret records a secret value that must be redacted from any result,
// error, event or observation. Registering the *value* is the only place a raw
// secret enters the platform, and it never leaves it.
func (p *Platform) RegisterSecret(secret string) {
	if len(secret) < 8 {
		return // too short to redact safely without mangling output
	}
	p.secretMu.Lock()
	defer p.secretMu.Unlock()
	p.secrets = append(p.secrets, secret)
}

// Manifest returns one registered manifest (contract §4). It is the
// registration lookup used by the lifecycle control surface; it grants
// nothing and reveals nothing beyond the manifest itself.
func (p *Platform) Manifest(toolID string) (tool.ToolManifest, bool) {
	return p.Registry.Manifest(toolID)
}

// Adapter returns the adapter registered for one capability, or nil when the
// tool has no implementation bound. Discovery uses it only to read the
// reliability posture an adapter advertises for itself.
func (p *Platform) Adapter(toolID string) tool.Adapter {
	a, _ := p.Registry.Adapter(toolID)
	return a
}

// Catalog returns the bounded capability catalog for one scope (contract §4).
// It never contains credentials, secret values, filesystem roots or internal
// network topology.
func (p *Platform) Catalog(businessID, divisionID string) []tool.ToolManifest {
	out := []tool.ToolManifest{}
	for _, m := range p.Registry.ListManifests() {
		if !p.agentMaySee(m, businessID, divisionID) {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// agentMaySee reports whether the scope may see a manifest at all. It is the
// business/division visibility rule of G3 applied to tool metadata.
func (p *Platform) agentMaySee(m tool.ToolManifest, businessID, divisionID string) bool {
	return businessID != ""
}

// Invoke is the single mediated invocation path (contract §5). Every gate runs
// here; adapters never authorize anything themselves.
func (p *Platform) Invoke(ctx context.Context, req Request) tool.Result {
	start := p.now()
	res := tool.Result{ToolID: req.ToolID, Operation: req.Operation, Status: tool.StatusError}

	// 1–3: manifest lookup, schema validation, operation resolution.
	manifest, ok := p.Registry.Manifest(req.ToolID)
	if !ok {
		return p.deny(req, res, start, ErrValidation, fmt.Sprintf("tool %q is not registered", req.ToolID), "tool.invocation.rejected")
	}
	res.Operation = req.Operation
	if req.Operation == "" {
		req.Operation = defaultOperation(manifest)
		res.Operation = req.Operation
	}
	if !manifest.Supports(req.Operation) {
		return p.deny(req, res, start, ErrValidation,
			fmt.Sprintf("tool %q does not support operation %q", req.ToolID, req.Operation), "tool.invocation.rejected")
	}
	input := req.Input
	if input == nil {
		input = map[string]string{}
	}
	if err := manifest.ValidateInput(input); err != nil {
		return p.deny(req, res, start, ErrValidation, err.Error(), "tool.invocation.rejected")
	}

	// 4: agent allowlist.
	if !contains(req.AgentTools, req.ToolID) {
		p.Auditor.emit(event.EventTypeToolPermissionDenied, req, map[string]string{"tool_id": req.ToolID, "reason": "not in agent allowlist"})
		return p.deny(req, res, start, ErrPermission,
			fmt.Sprintf("tool %q is not in the agent allowlist", req.ToolID), "tool.invocation.rejected")
	}

	// 5–6: scope + permission (identity, membership, manifest, governance state).
	if err := p.CanInvoke(req, manifest); err != nil {
		p.Auditor.emit(event.EventTypeToolPermissionDenied, req, map[string]string{"tool_id": req.ToolID, "reason": err.Error()})
		kind := ErrPermission
		if errors.Is(err, ErrScope) {
			kind = ErrScope
		}
		return p.deny(req, res, start, kind, err.Error(), "tool.invocation.rejected")
	}

	// 7: budgets — the manifest may request less than the runtime cap; the
	// runtime cap always wins.
	limits := clampLimits(manifest.ResourceLimits, p.Caps)

	// 8: credential resolution (fail closed; never reached for tools without a
	// credential requirement).
	handle := tool.CredentialHandle{}
	if manifest.CredentialRequirement.Required {
		if p.Credentials == nil {
			p.Auditor.emit(event.EventTypeToolCredentialDenied, req, map[string]string{"tool_id": req.ToolID, "reference": manifest.CredentialRequirement.Reference})
			return p.deny(req, res, start, ErrCredential,
				fmt.Sprintf("tool %q requires credential %q", req.ToolID, manifest.CredentialRequirement.Reference), "tool.invocation.rejected")
		}
		h, err := p.Credentials.Resolve(ctx, manifest.CredentialRequirement, req.BusinessID, req.DivisionID)
		if err != nil {
			p.Auditor.emit(event.EventTypeToolCredentialDenied, req, map[string]string{"tool_id": req.ToolID, "reference": manifest.CredentialRequirement.Reference, "reason": err.Error()})
			return p.deny(req, res, start, ErrCredential, err.Error(), "tool.invocation.rejected")
		}
		handle = h
	}

	adapter, ok := p.Registry.Adapter(req.ToolID)
	if !ok {
		return p.deny(req, res, start, ErrInternal, fmt.Sprintf("tool %q has no adapter", req.ToolID), "tool.invocation.failed")
	}

	// Capability lifecycle (OPERATIONAL_RELIABILITY_CONTRACTS §10). A disabled
	// capability is refused before any adapter dispatch; a deprecated one still
	// executes but is surfaced in telemetry. A tool an operator has never
	// touched is enabled.
	state := p.CapabilityState(req.ToolID)
	if state == CapabilityDisabled {
		p.Auditor.emit(event.EventTypeToolCapabilityDisabled, req, map[string]string{
			"tool_id": req.ToolID, "operation": req.Operation, "agent_id": req.AgentID,
		})
		return p.deny(req, res, start, ErrCapabilityDisabled,
			fmt.Sprintf("capability %q is disabled", req.ToolID), "tool.invocation.rejected")
	}
	if state == CapabilityDeprecated {
		p.Auditor.emit(event.EventTypeToolCapabilityDeprecated, req, map[string]string{
			"tool_id": req.ToolID, "operation": req.Operation, "agent_id": req.AgentID,
		})
	}

	// 9: adapter dispatch under ONE call deadline shared by every physical
	// attempt of this logical call (contract §1, §5, §7).
	callID := req.CorrelationID
	if strings.TrimSpace(callID) == "" {
		callID = fmt.Sprintf("%s-%d", req.ToolID, p.now().UnixNano())
	}
	callCtx, cancel := context.WithTimeout(ctx, limits.MaxDuration)
	defer cancel()
	p.Auditor.emit(event.EventTypeToolInvocationStarted, req, map[string]string{
		"tool_id": req.ToolID, "operation": req.Operation, "agent_id": req.AgentID,
		"side_effect_class": string(manifest.SideEffectClass), "call_id": callID,
		"capability_state": string(state),
	})
	raw, attempts, err := p.runAttempts(callCtx, adapter, manifest, limits, tool.Invocation{
		ToolID: req.ToolID, Operation: req.Operation, Input: input,
		BusinessID: req.BusinessID, DivisionID: req.DivisionID, ActorID: req.ActorID,
		AgentID: req.AgentID, Credential: handle, Limits: limits,
		CallID: callID, IdempotencyKey: callID,
	}, req, callID)
	res.Attempts = attempts
	res.CapabilityState = string(state)
	res.Outcome = classifyOutcome(err, ctx.Err())
	// The surfaced verdict combines the class decision with the remaining
	// budget: a spent attempt budget never recommends another attempt, and an
	// unknown outcome never does either (§3).
	res.RetryRecommended = RetryRecommended(err, manifest) && attempts < attemptBound(manifest)
	if res.Outcome == "unknown" {
		// An unknown outcome is never re-driven automatically (§9). It only
		// surfaces a reconciliation requirement when the capability advertises
		// a read-back path; NEXUS runs no background reconcilers (G4 Level 1).
		res.ReconciliationAvailable = adapterSupportsReconciliation(adapter)
	}
	elapsed := p.now().Sub(start)
	if err != nil {
		kind := adapterErrorKind(ctx, err)
		msg := p.redactString(err.Error())
		if kind != ErrInternal {
			res.Error = kind.Error() + ": " + msg
		} else {
			res.Error = msg
		}
		res.Duration = elapsed
		res.Status = tool.StatusError
		p.Auditor.emit(invocationEventForOutcome(res.Outcome), req, map[string]string{
			"tool_id": req.ToolID, "operation": req.Operation, "error_kind": errorClass(kind),
			"outcome": res.Outcome, "attempts": fmt.Sprint(res.Attempts),
			"duration_ms": fmt.Sprint(elapsed.Milliseconds()), "call_id": callID,
			"capability_state":  string(state),
			"retry_recommended": fmt.Sprint(res.RetryRecommended),
		})
		if res.ReconciliationAvailable {
			p.Auditor.emit(event.EventTypeToolReconciliationAvailable, req, map[string]string{
				"tool_id": req.ToolID, "operation": req.Operation, "call_id": callID,
				"outcome": res.Outcome,
			})
		}
		return res
	}

	// 10–12: normalize → bound → redact → observation-ready result.
	normalized, truncated := p.normalize(manifest, raw, limits)
	normalized.ToolID = req.ToolID
	normalized.Operation = req.Operation
	normalized.Status = tool.StatusSuccess
	normalized.Duration = elapsed
	normalized.Outcome = "completed"
	normalized.Attempts = attempts
	normalized.CapabilityState = string(state)
	if truncated {
		normalized.Truncated = true
		p.Auditor.emit(event.EventTypeToolResultTruncated, req, map[string]string{
			"tool_id": req.ToolID, "operation": req.Operation, "bytes": fmt.Sprint(normalized.Bytes),
		})
	}
	auditFields := map[string]string{
		"tool_id": req.ToolID, "operation": req.Operation, "agent_id": req.AgentID,
		"status": normalized.Status, "duration_ms": fmt.Sprint(elapsed.Milliseconds()),
		"result_bytes": fmt.Sprint(normalized.Bytes), "truncated": fmt.Sprint(normalized.Truncated),
		"side_effect_class": string(manifest.SideEffectClass), "outcome": "completed",
		"attempts": fmt.Sprint(attempts), "capability_state": string(state),
		"call_id": callID,
	}
	if manifest.CredentialRequirement.Required {
		// The audit records the *reference*, never the value.
		auditFields["credential_ref"] = manifest.CredentialRequirement.Reference
	}
	p.Auditor.emit(event.EventTypeToolInvocationCompleted, req, auditFields)
	return normalized
}

// runAttempts dispatches one logical call as bounded physical attempts that all
// share the single call deadline (contract §1, §2, §5). It is the only place
// that may retry anything: the verdict comes from DecideRetry, the bound from
// the side-effect class, and a retry is only started when a whole attempt
// still fits inside the remaining deadline.
func (p *Platform) runAttempts(callCtx context.Context, adapter tool.Adapter, manifest tool.ToolManifest,
	limits tool.ResourceLimits, inv tool.Invocation, req Request, callID string) (tool.RawResult, int, error) {
	deadline, hasDeadline := callCtx.Deadline()
	bound := attemptBound(manifest)
	var raw tool.RawResult
	var err error
	attempts := 0
	for attempts < bound {
		if hasDeadline {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				if err == nil {
					// The deadline was already gone before the first attempt:
					// no dispatch happened, the call is timed out (§5).
					err = context.DeadlineExceeded
				}
				return raw, attempts, err
			}
		}
		slice := limits.MaxDuration
		if bound > 1 {
			// A retryable call divides its one deadline across the attempts it
			// may make: nobody gets a fresh deadline (§5).
			slice = time.Until(deadline) / time.Duration(bound)
		}
		attempts++
		attemptCtx, cancelAttempt := context.WithTimeout(callCtx, slice)
		attemptStart := p.now()
		p.Auditor.emit(event.EventTypeToolAttemptStarted, req, map[string]string{
			"tool_id": req.ToolID, "operation": req.Operation, "call_id": callID,
			"attempt": fmt.Sprint(attempts), "max_attempts": fmt.Sprint(bound),
		})
		inv.AttemptIndex = attempts - 1
		raw, err = adapter.Invoke(attemptCtx, inv)
		// The attempt context's own error must be read before it is released:
		// cancel() would otherwise make every attempt look cancelled.
		attemptCtxErr := attemptCtx.Err()
		cancelAttempt()
		attemptOutcome := classifyOutcome(err, attemptCtxErr)
		fields := map[string]string{
			"tool_id": req.ToolID, "operation": req.Operation, "call_id": callID,
			"attempt": fmt.Sprint(attempts), "outcome": attemptOutcome,
			"duration_ms": fmt.Sprint(p.now().Sub(attemptStart).Milliseconds()),
		}
		if err != nil {
			fields["error_class"] = errorClass(adapterErrorKind(callCtx, err))
		}
		p.Auditor.emit(event.EventTypeToolAttemptCompleted, req, fields)
		if err == nil {
			return raw, attempts, nil
		}
		if !DecideRetry(err, manifest, attempts) {
			return raw, attempts, err
		}
		// §5: a retry may only be scheduled when a whole attempt still fits in
		// the remaining call deadline; otherwise the first error stands.
		if hasDeadline && time.Until(deadline) < slice {
			return raw, attempts, err
		}
		p.Auditor.emit(event.EventTypeToolRetryScheduled, req, map[string]string{
			"tool_id": req.ToolID, "operation": req.Operation, "call_id": callID,
			"attempt":     fmt.Sprint(attempts),
			"error_class": errorClass(adapterErrorKind(callCtx, err)),
		})
	}
	return raw, attempts, err
}

// CanInvoke evaluates permission for one invocation (contract §28/§5 step 6).
// It combines only established facts: the actor's membership, the agent
// allowlist, manifest requirements and credential scope. The model has no role.
func (p *Platform) CanInvoke(req Request, manifest tool.ToolManifest) error {
	if req.ActorID == "" || req.BusinessID == "" {
		return fmt.Errorf("%w: actor and business scope are required", ErrScope)
	}
	if p.Memberships == nil {
		return fmt.Errorf("%w: membership store unavailable", ErrScope)
	}
	// G3: division scope narrows; a business-scope invocation needs a
	// business-wide membership; a division invocation is covered by a
	// business-wide membership or one of that exact division.
	if !p.Memberships.AllowsScope(req.ActorID, req.BusinessID, req.DivisionID) {
		return fmt.Errorf("%w: actor is not authorized for business=%s division=%s", ErrScope, req.BusinessID, req.DivisionID)
	}
	if manifest.ScopeRequirement == tool.ScopeDivision && req.DivisionID == "" {
		return fmt.Errorf("%w: tool requires a division scope", ErrScope)
	}
	if manifest.ScopeRequirement == tool.ScopeWorkspace && req.DivisionID == "" && req.BusinessID == "" {
		return fmt.Errorf("%w: tool requires a workspace scope", ErrScope)
	}
	if manifest.CredentialRequirement.Required {
		if req.BusinessID == "" {
			return fmt.Errorf("%w: credentialed tool requires a business scope", ErrScope)
		}
	}
	return nil
}

// normalize bounds the adapter result and redacts it (contract §7).
func (p *Platform) normalize(manifest tool.ToolManifest, raw tool.RawResult, limits tool.ResourceLimits) (tool.Result, bool) {
	res := tool.Result{Result: map[string]string{}, Metadata: map[string]string{}}
	truncated := false
	keys := sortedKeys(raw.Result)
	metaBytes := 0
	for _, k := range keys {
		v := raw.Result[k]
		if f, ok := manifest.OutputSchema.Field(k); ok && f.MaxLength > 0 && len(v) > f.MaxLength {
			v = v[:f.MaxLength]
			truncated = true
		}
		if len(v) > limits.MaxOutputByte {
			v = v[:limits.MaxOutputByte]
			truncated = true
		}
		clean := p.redactString(v)
		res.Result[k] = clean
		res.Bytes += len(k) + len(clean)
		if res.Bytes > limits.MaxOutputByte {
			// hard output bound: drop the rest and mark truncated
			res.Result[k] = clean[:max(0, limits.MaxOutputByte-res.Bytes+len(clean))]
			res.Bytes = limits.MaxOutputByte
			truncated = true
			break
		}
	}
	for _, k := range sortedKeys(raw.Headers) {
		if metaBytes > 2048 {
			truncated = true
			break
		}
		if isSensitiveHeader(k) {
			res.Metadata["header."+strings.ToLower(k)] = "[redacted]"
			continue
		}
		res.Metadata["header."+strings.ToLower(k)] = p.redactString(raw.Headers[k])
		metaBytes += len(k)
	}
	for _, k := range sortedKeys(raw.Metadata) {
		if metaBytes > 4096 {
			truncated = true
			break
		}
		res.Metadata[k] = p.redactString(raw.Metadata[k])
		metaBytes += len(k)
	}
	return res, truncated
}

func (p *Platform) deny(req Request, res tool.Result, start time.Time, kind error, msg, evName string) tool.Result {
	res.Status = tool.StatusDenied
	res.Error = fmt.Sprintf("%s: %s", kind.Error(), p.redactString(msg))
	res.Duration = p.now().Sub(start)
	ev := event.EventType(evName)
	p.Auditor.emit(ev, req, map[string]string{"tool_id": req.ToolID, "error_kind": kind.Error(), "reason": res.Error})
	return res
}

func adapterErrorKind(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, context.Canceled) || ctx.Err() != nil:
		return ErrCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrTimeout
	case errors.Is(err, ErrNetworkBlocked):
		return ErrNetworkBlocked
	case errors.Is(err, ErrPermission):
		return ErrPermission
	case errors.Is(err, ErrScope):
		return ErrScope
	case errors.Is(err, ErrCredential):
		return ErrCredential
	case errors.Is(err, ErrResourceLimit):
		return ErrResourceLimit
	case errors.Is(err, ErrExternal):
		return ErrExternal
	case errors.Is(err, ErrValidation):
		return ErrValidation
	case errors.Is(err, ErrUnknownOutcome):
		return ErrUnknownOutcome
	default:
		return ErrInternal
	}
}

func clampLimits(requested tool.ResourceLimits, caps Caps) tool.ResourceLimits {
	out := tool.ResourceLimits{
		MaxDuration:   caps.MaxDuration,
		MaxOutputByte: caps.MaxOutputBytes,
		MaxRequestByt: caps.MaxRequestBytes,
		MaxItems:      caps.MaxItems,
		MaxDepth:      caps.MaxDepth,
	}
	if requested.MaxDuration > 0 && requested.MaxDuration < out.MaxDuration {
		out.MaxDuration = requested.MaxDuration
	}
	if requested.MaxOutputByte > 0 && requested.MaxOutputByte < out.MaxOutputByte {
		out.MaxOutputByte = requested.MaxOutputByte
	}
	if requested.MaxRequestByt > 0 && requested.MaxRequestByt < out.MaxRequestByt {
		out.MaxRequestByt = requested.MaxRequestByt
	}
	if requested.MaxItems > 0 && requested.MaxItems < out.MaxItems {
		out.MaxItems = requested.MaxItems
	}
	if requested.MaxDepth > 0 && requested.MaxDepth < out.MaxDepth {
		out.MaxDepth = requested.MaxDepth
	}
	return out
}

func defaultOperation(m tool.ToolManifest) string {
	if len(m.Operations) == 0 {
		return ""
	}
	return m.Operations[0]
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
