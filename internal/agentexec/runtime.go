package agentexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// WorkflowStrategy is sequential or parallel for the unit steps of an
// execution spec.
type WorkflowStrategy string

const (
	StrategySequential WorkflowStrategy = "sequential"
	StrategyParallel   WorkflowStrategy = "parallel"
)

// ToolCallSpec is one deterministic tool invocation the agent is allowed
// to make inside this execution.
type ToolCallSpec struct {
	ToolID string `json:"tool_id"`
	// Operation selects an explicit manifest operation when the tool has
	// more than one (e.g. filesystem: read/write/list). Empty means the
	// tool's default operation. It never widens what the manifest declares.
	Operation string            `json:"operation,omitempty"`
	Input     map[string]string `json:"input,omitempty"`
}

// Node is one workflow node: an agent task + its bounded inputs.
type Node struct {
	ID           string            `json:"id"`
	Intent       string            `json:"intent"`
	AgentID      string            `json:"agent_id,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Tools        []ToolCallSpec    `json:"tools,omitempty"`
	Input        map[string]string `json:"input,omitempty"`
	DependsOn    []string          `json:"depends_on,omitempty"`
}

// WorkflowSpec composes nodes under one strategy.
type WorkflowSpec struct {
	Strategy WorkflowStrategy `json:"strategy"`
	Nodes    []Node           `json:"nodes"`
}

// DelegateSpec is a child execution: the runtime runs a nested
// agent-selection + tools + model call with inherited scope.
type DelegateSpec struct {
	ID           string         `json:"id"`
	Intent       string         `json:"intent"`
	AgentID      string         `json:"agent_id,omitempty"`
	Capabilities []string       `json:"capabilities,omitempty"`
	Tools        []ToolCallSpec `json:"tools,omitempty"`
}

// Spec is the execution-layer request (deserialized from
// executor.WorkRequest.Input["agent_execution"]).
type Spec struct {
	AgentID              string         `json:"agent_id,omitempty"`
	RequiredCapabilities []string       `json:"required_capabilities,omitempty"`
	OptionalCapabilities []string       `json:"optional_capabilities,omitempty"`
	Tools                []ToolCallSpec `json:"tools,omitempty"`
	Delegates            []DelegateSpec `json:"delegates,omitempty"`
	Workflow             *WorkflowSpec  `json:"workflow,omitempty"`
}

// Retry limits the model invocation retry loop: only provider errors
// carrying a transient marker ("timeout", "temporary", or context
// deadline) are retried, exactly once, and every retry is counted.
const (
	maxModelAttempts = 2
)

// Runtime executes agentic work as the executor's TaskHandler. It never
// bypasses the surrounding request pipeline: governance, approvals,
// events, cancellation, and scope checks stay upstream.
type Runtime struct {
	Registry *Registry
	// Platform is the Capability & Tool Platform seam
	// (contracts/CAPABILITY_TOOL_CONTRACTS.md §5). When set, InvokeTool runs
	// the full mediated path (manifest, schema, allowlist, scope, permission,
	// budget, credential, adapter, normalization, redaction). When nil the
	// pre-platform allowlist+registry path is used, which keeps unit-level
	// construction of the runtime valid without a platform.
	Platform *capability.Platform
	Tools    *tool.ToolRegistry
	ToolExec map[string]func(ctx context.Context, input map[string]string) (map[string]string, error)
	Router   *modelrouter.ModelRouter
	Bus      *event.MemBus
	// Controller is the governance admission boundary
	// (contracts/AGENT_GOVERNANCE_CONTROL_CONTRACTS.md). Every mediated tool
	// invocation passes through it BEFORE Platform.Invoke. A nil controller
	// fails closed: governance is never bypassed.
	Controller *control.Controller
	now        func() time.Time
	maxDepth   int
	maxFan     int
	mu         sync.Mutex
	// proposalSeq numbers runtime proposal identities (never model supplied).
	proposalSeq int
}

// NewRuntime builds a runtime with deterministic builtin tools registered
// when the registry does not carry them yet.
func NewRuntime(reg *Registry, tools *tool.ToolRegistry, router *modelrouter.ModelRouter, bus *event.MemBus) *Runtime {
	if tools == nil {
		tools = tool.NewToolRegistry()
	}
	registerBuiltins(tools)
	return &Runtime{
		Registry: reg, Tools: tools, ToolExec: BuiltinExecutor(),
		Router: router, Bus: bus, now: time.Now, maxDepth: 3, maxFan: 4,
	}
}

// handler is the executor.TaskHandler seam.
func (r *Runtime) Handler() func(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
	return func(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
		return r.execute(ctx, req, ag, 0)
	}
}

func (r *Runtime) emit(ev event.EventType, req *executor.WorkRequest, actor, outcome string, extra map[string]string) {
	if r.Bus == nil {
		return
	}
	data := map[string]string{"step": string(ev), "actor": actor, "outcome": outcome}
	for k, v := range extra {
		data[k] = v
	}
	b, _ := json.Marshal(data)
	_ = r.Bus.Publish(&event.Event{
		ID:            fmt.Sprintf("%s-%s-%d", req.CorrelationID, ev, r.now().UnixNano()),
		Type:          ev,
		Source:        "agentexec",
		Timestamp:     r.now(),
		BusinessID:    req.BusinessID,
		CorrelationID: req.CorrelationID,
		Priority:      event.PriorityNormal,
		Data:          b,
	})
}

func (r *Runtime) execute(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent, depth int) (*executor.Outcome, error) {
	start := r.now()
	var spec Spec
	if raw := req.Input["agent_execution"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &spec); err != nil {
			return failedOutcome(req, ag, start, "invalid agent_execution spec: "+err.Error()), nil
		}
	}
	r.emit(event.EventTypeAgentStarted, req, req.ActorID, "started", nil)

	// Workflow composition path.
	if spec.Workflow != nil {
		if depth > r.maxDepth {
			return failedOutcome(req, ag, start, "delegation depth exceeded"), nil
		}
		return r.runWorkflow(ctx, req, ag, &spec, start, depth, nil)
	}

	// Direct path: select agent → tools → model → delegates.
	if depth > r.maxDepth {
		return failedOutcome(req, ag, start, "delegation depth exceeded"), nil
	}

	sel := Select(r.Registry, &SelectionRequest{
		BusinessID: req.BusinessID, DivisionID: req.DivisionID,
		Required: spec.RequiredCapabilities, Optional: spec.OptionalCapabilities,
		AgentID: spec.AgentID, RequiredToolIDs: toolIDsOf(spec.Tools),
	}, r.toolKnown)
	if sel.Agent == nil {
		return &executor.Outcome{
			TaskID: req.TaskID, AgentID: spec.AgentID, Status: "failed",
			Error: "no eligible agent: " + sel.Reason, Duration: r.now().Sub(start),
			CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
			CreatedAt: start, CompletedAt: r.now(),
		}, nil
	}
	r.emit(event.EventTypeAgentSelected, req, req.ActorID, "selected "+sel.Agent.ID,
		map[string]string{"agent_id": sel.Agent.ID, "reason": sel.Reason,
			"matched": strings.Join(sel.MatchedCapabilities, ",")})

	outcomePriv, out := runAgentUnit(req, ctx, start, spec.Tools, sel.Agent, r)
	if out != nil {
		r.emit(event.EventTypeAgentFailed, req, req.ActorID, out.Error,
			map[string]string{"agent_id": sel.Agent.ID, "execution_kind": "unit"})
		return out, nil
	}
	unit := outcomePriv

	// Child delegates execute inside the same scope; each emits its own
	// events and counts toward child bookkeeping on the parent result.
	childResults := []string{}
	childFailures := []string{}
	for _, d := range spec.Delegates {
		if err := ctx.Err(); err != nil {
			return cancelledOutcome(req, start), nil
		}
		r.emit(event.EventTypeAgentDelegated, req, req.ActorID, "delegated "+d.ID,
			map[string]string{"delegate_id": d.ID, "agent_id": d.AgentID})
		childCtx := ctx
		childReq := &executor.WorkRequest{
			TaskID:        req.TaskID + "/delegate/" + d.ID,
			CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
			DivisionID: req.DivisionID, ActorID: req.ActorID, Intent: d.Intent,
			Input: map[string]string{"agent_execution": mustMarshalSpec(&Spec{
				AgentID: d.AgentID, RequiredCapabilities: d.Capabilities, Tools: d.Tools,
			})},
			Constraints: req.Constraints, Priority: req.Priority,
		}
		child, err := r.execute(childCtx, childReq, ag, depth+1)
		if err != nil {
			childFailures = append(childFailures, fmt.Sprintf("%s: %v", d.ID, err))
			continue
		}
		if child.Status != "completed" {
			childFailures = append(childFailures, fmt.Sprintf("%s: %s", d.ID, child.Status))
		} else {
			childResults = append(childResults, child.Output)
		}
	}
	if len(childFailures) > 0 {
		return &executor.Outcome{
			TaskID: req.TaskID, AgentID: sel.Agent.ID, Status: "failed",
			Error:    "delegated child failed: " + strings.Join(childFailures, "; "),
			Duration: r.now().Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
			CreatedAt: start, CompletedAt: r.now(),
			Evidence: unit.Evidence,
		}, nil
	}

	output := unit.Output
	if len(childResults) > 0 {
		output = output + " | children: " + strings.Join(childResults, " | ")
	}
	return &executor.Outcome{
		TaskID: req.TaskID, AgentID: sel.Agent.ID, Status: "completed", Output: output,
		Duration: r.now().Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
		CreatedAt: start, CompletedAt: r.now(),
		Evidence: append(unit.Evidence, fmt.Sprintf("children=%d", len(spec.Delegates))),
		Provider: unit.Provider, Model: unit.Model, RoutingReason: unit.RoutingReason,
		Fallback: unit.Fallback, ToolsExecuted: unit.ToolsExecuted, ChildExecutions: len(spec.Delegates), Retries: unit.Retries,
	}, nil
}

// runAgentUnit executes tools→model for one agent inside one execution.
func runAgentUnit(req *executor.WorkRequest, ctx context.Context, start time.Time, tools []ToolCallSpec, a *Definition, rt *Runtime) (*executor.Outcome, *executor.Outcome) {
	r := rt
	// Gather-phase: bounded, deterministic tools.
	toolOutputs := map[string]string{}
	toolsExecuted := 0
	for _, t := range tools {
		if err := ctx.Err(); err != nil {
			return nil, cancelledOutcome(req, start)
		}
		known, ok := r.Tools.GetTool(t.ToolID)
		if !ok {
			return nil, &executor.Outcome{TaskID: req.TaskID, AgentID: a.ID, Status: "failed",
				Error:    fmt.Sprintf("tool %q is not registered", t.ToolID),
				Duration: r.now().Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
				CreatedAt: start, CompletedAt: r.now()}
		}
		execFn, hasExec := r.ToolExec[t.ToolID]
		if !hasExec || execFn == nil {
			return nil, &executor.Outcome{TaskID: req.TaskID, AgentID: a.ID, Status: "failed",
				Error:    fmt.Sprintf("tool %q has no executor", t.ToolID),
				Duration: r.now().Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
				CreatedAt: start, CompletedAt: r.now()}
		}
		// Allowlist: the caller's tool set must be a subset of the agent's.
		if !stringIn(a.AllowedTools, t.ToolID) {
			return nil, &executor.Outcome{TaskID: req.TaskID, AgentID: a.ID, Status: "failed",
				Error:    fmt.Sprintf("tool %q is not in the agent allowlist", t.ToolID),
				Duration: r.now().Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
				CreatedAt: start, CompletedAt: r.now()}
		}
		r.emit(event.EventTypeToolRequested, req, req.ActorID, "requested",
			map[string]string{"tool_id": t.ToolID, "agent_id": a.ID})
		r.emit(event.EventTypeToolStarted, req, req.ActorID, "started",
			map[string]string{"tool_id": t.ToolID, "agent_id": a.ID})
		out, err := execFn(ctx, t.Input)
		if err != nil {
			return nil, &executor.Outcome{TaskID: req.TaskID, AgentID: a.ID, Status: "failed",
				Error:    fmt.Sprintf("tool %q failed: %v", t.ToolID, err),
				Duration: r.now().Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
				CreatedAt: start, CompletedAt: r.now()}
		}
		toolsExecuted++
		r.emit(event.EventTypeToolCompleted, req, req.ActorID, "ok",
			map[string]string{"tool_id": t.ToolID, "agent_id": a.ID, "tool_name": known.Name})
		for k, v := range out {
			toolOutputs[t.ToolID+"."+k] = v
		}
	}

	// Model phase: route deterministically, recorded decision, observable
	// events; cancellation respected; transient-only retry bounded to 2.
	caps := []modelrouter.ModelCapability{}
	if a.Model.PreferLocal {
		caps = append(caps, "local")
	}
	if a.Model.ToolCalling {
		caps = append(caps, modelrouter.CapabilityToolCalling)
	}
	if a.Model.StructuredOutput {
		caps = append(caps, modelrouter.CapabilityStructuredOutput)
	}
	r.emit(event.EventTypeModelSelected, req, req.ActorID, "routing", map[string]string{"agent_id": a.ID})
	respContents := []string{}
	var lastErr error
	attempts := 0
	for attempts < maxModelAttempts {
		attempts++
		if attempts > 1 {
			r.emit(event.EventTypeModelStarted, req, req.ActorID, "retry "+fmt.Sprint(attempts), nil)
		} else {
			r.emit(event.EventTypeModelStarted, req, req.ActorID, "started", nil)
		}
		d, err := r.invokeModel(ctx, req, a, caps, toolOutputs)
		if err == nil {

			respContents = append(respContents, d.Resp.Content)
			lm := r.now()
			return &executor.Outcome{
				TaskID: req.TaskID, AgentID: a.ID, Status: "completed",
				Output:   renderOutput(req.Intent, toolOutputs, d.Resp.Content),
				Duration: lm.Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
				CreatedAt: start, CompletedAt: lm,
				Provider: d.Decision.ProviderID, Model: d.Decision.ModelID, RoutingReason: d.Decision.Reason,
				Fallback: strings.HasPrefix(d.Decision.Reason, "failover"), Retries: attempts - 1,
				ToolsExecuted: toolsExecuted,
			}, nil
		}
		lastErr = err
		if isTransient(err) && attempts < maxModelAttempts {
			continue
		}
		break
	}
	errMsg := lastErr.Error()
	r.emit(event.EventTypeModelStarted, req, req.ActorID, "failed: "+errMsg, nil)
	return nil, &executor.Outcome{
		TaskID: req.TaskID, AgentID: a.ID, Status: "failed", Error: errMsg,
		Duration: r.now().Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
		CreatedAt: start, CompletedAt: r.now(), ToolsExecuted: toolsExecuted,
	}
}

type invokeOutcome struct {
	Resp     *modelrouter.GenerateResponse
	Decision *modelrouter.RoutingDecision
}

func (r *Runtime) invokeModel(ctx context.Context, req *executor.WorkRequest, a *Definition, caps []modelrouter.ModelCapability, toolOutputs map[string]string) (*invokeOutcome, error) {
	routeReq := &modelrouter.RoutingRequest{
		RequestID: req.CorrelationID, AgentID: a.ID, BusinessID: req.BusinessID,
		RequiredCaps:     caps,
		PreferLocal:      a.Model.PreferLocal,
		AllowedProviders: a.Model.AllowedProviders,
		FallbackEnabled:  true,
	}
	if len(a.Model.AllowedProviders) == 0 && a.Model.PreferredProvider != "" {
		routeReq.AllowedProviders = append(routeReq.AllowedProviders, a.Model.PreferredProvider)
	}
	msgs := []modelrouter.Message{
		{Role: "system", Content: fmt.Sprintf("agent=%s business=%s capabilities=%s", a.ID, req.BusinessID, strings.Join(a.Capabilities, ","))},
		{Role: "user", Content: req.Intent},
	}
	for k, v := range toolOutputs {
		msgs = append(msgs, modelrouter.Message{Role: "tool", Content: k + "=" + v})
	}
	gen := &modelrouter.GenerateRequest{RequestID: req.CorrelationID, Messages: msgs, MaxTokens: 512}
	resp, decision, err := r.Router.Invoke(ctx, routeReq, gen)
	if err != nil {
		return nil, err
	}
	r.emit(event.EventTypeModelCompleted, req, req.ActorID, "completed",
		map[string]string{"provider": decision.ProviderID, "model": decision.ModelID, "reason": decision.Reason,
			"finish_reason": resp.FinishReason})
	return &invokeOutcome{Resp: resp, Decision: decision}, nil
}

func isTransient(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "deadline") || strings.Contains(s, "timeout") && !strings.Contains(s, "offline")
}

func renderOutput(intent string, tools map[string]string, content string) string {
	var parts []string
	parts = append(parts, "intent="+intent)
	for k, v := range tools {
		parts = append(parts, k+"="+v)
	}
	if content != "" {
		parts = append(parts, "content="+content)
	}
	return strings.Join(parts, " | ")
}

func failedOutcome(req *executor.WorkRequest, ag *agent.Agent, start time.Time, msg string) *executor.Outcome {
	return &executor.Outcome{TaskID: req.TaskID, Status: "failed", Error: msg,
		Duration: time.Since(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
		CreatedAt: start, CompletedAt: time.Now()}
}

func cancelledOutcome(req *executor.WorkRequest, start time.Time) *executor.Outcome {
	return &executor.Outcome{TaskID: req.TaskID, Status: "cancelled", Error: "cancelled before completion",
		Duration: time.Since(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
		CreatedAt: start, CompletedAt: time.Now()}
}

func toolIDsOf(calls []ToolCallSpec) []string {
	out := []string{}
	for _, t := range calls {
		out = append(out, t.ToolID)
	}
	return out
}

func stringIn(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// correlationOf extracts the correlation id carried by the executor context so
// capability audit events can join the execution trace.
func correlationOf(ctx context.Context) string {
	if v, ok := ctx.Value(correlationKey{}).(string); ok {
		return v
	}
	return ""
}

// correlationKey carries the request correlation id into adapter contexts.
type correlationKey struct{}

// WithCorrelation attaches the execution correlation id to a context so the
// capability platform can stamp its audit events with it.
func WithCorrelation(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, correlationKey{}, correlationID)
}

func mustMarshalSpec(s *Spec) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (r *Runtime) toolKnown(id string) bool {
	if r.Tools == nil {
		return false
	}
	_, ok := r.Tools.GetTool(id)
	return ok
}

// InvokeTool performs one mediated tool call. With a Platform wired it is the
// capability platform's single invocation path; without one it is the
// pre-platform allowlist+registry check over the deterministic executors. Both
// paths are the *same* boundary from the caller's point of view — there is no
// second invocation mechanism.
func (r *Runtime) InvokeTool(ctx context.Context, agentID string, allowedTools []string, businessID string, call ToolCallSpec) (map[string]string, error) {
	return r.InvokeToolScoped(ctx, ToolScope{BusinessID: businessID, AgentTools: allowedTools}, agentID, call)
}

// ToolScope carries the runtime-established facts of one tool invocation.
// Every field is established by the runtime from the admitted request and the
// agent definition — never from model output
// (contracts/CAPABILITY_TOOL_CONTRACTS.md §28).
type ToolScope struct {
	ActorID       string
	BusinessID    string
	DivisionID    string
	AgentTools    []string
	CorrelationID string
}

// OutcomeError reports a failed logical invocation with its terminal
// outcome/class (OPERATIONAL_RELIABILITY_CONTRACTS §17). It unwraps to the
// underlying cause text; callers may read Outcome/Attempts directly for
// observation normalization instead of parsing error text.
type OutcomeError struct {
	Outcome                 string
	Attempts                int
	RetryRecommended        bool
	ReconciliationAvailable bool
	Cause                   error
}

func (e *OutcomeError) Error() string { return e.Cause.Error() }
func (e *OutcomeError) Unwrap() error { return e.Cause }

// InvokeToolScoped is the mediated invocation with the full runtime scope
// (identity, business, division, allowlist, correlation). It is the same
// boundary as InvokeTool; only the scope is richer.
func (r *Runtime) InvokeToolScoped(ctx context.Context, scope ToolScope, agentID string, call ToolCallSpec) (map[string]string, error) {
	allowedTools, businessID := scope.AgentTools, scope.BusinessID
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.Platform != nil {
		corr := scope.CorrelationID
		if corr == "" {
			corr = correlationOf(ctx)
		}
		// ---- governance admission, strictly BEFORE the capability platform
		// (AGENT_GOVERNANCE_CONTROL_CONTRACTS §6). A refused proposal never
		// reaches Platform.Invoke, and an allowing decision may tighten the call.
		effective, aerr := r.Admit(call, scope, corr, agentID, Limits{MaxDuration: capabilityMaxDuration})
		if aerr != nil {
			return nil, aerr
		}
		res := r.Platform.Invoke(ctx, capability.Request{
			ToolID: call.ToolID, Operation: call.Operation, Input: call.Input,
			AgentID: agentID, ActorID: scope.ActorID, BusinessID: businessID,
			DivisionID: scope.DivisionID, AgentTools: allowedTools, CorrelationID: corr,
			// The effective request carries what governance allowed: a
			// constraint may tighten this call and nothing else may widen it.
			MaxDuration: effective.MaxDuration,
		})
		if res.Status != tool.StatusSuccess {
			return nil, &OutcomeError{
				Outcome:                 res.Outcome,
				Attempts:                res.Attempts,
				RetryRecommended:        res.RetryRecommended,
				ReconciliationAvailable: res.ReconciliationAvailable,
				Cause:                   errors.New(res.Error),
			}
		}
		return res.Result, nil
	}
	if strings.TrimSpace(call.ToolID) == "" {
		return nil, fmt.Errorf("tool id is required")
	}
	if r.Tools == nil {
		return nil, fmt.Errorf("tool %q is not registered", call.ToolID)
	}
	if _, ok := r.Tools.GetTool(call.ToolID); !ok {
		return nil, fmt.Errorf("tool %q is not registered", call.ToolID)
	}
	if agentID != "" && len(allowedTools) > 0 && !stringIn(allowedTools, call.ToolID) {
		return nil, fmt.Errorf("tool %q is not in the agent allowlist", call.ToolID)
	}
	execFn, ok := r.ToolExec[call.ToolID]
	if !ok || execFn == nil {
		return nil, fmt.Errorf("tool %q has no executor", call.ToolID)
	}
	return execFn(ctx, call.Input)
}

// DelegateChild runs one child execution through the existing delegation
// primitive (AGENT_EXECUTION_CONTRACTS §10): the child inherits the parent's
// business/division/actor/correlation/cancellation context, so it can never
// widen authority. The intelligence layer uses this; it does not have a second
// delegation mechanism.
func (r *Runtime) DelegateChild(ctx context.Context, parent *executor.WorkRequest, ag *agent.Agent, child DelegateSpec, depth int) (*executor.Outcome, error) {
	childReq := &executor.WorkRequest{
		TaskID:        parent.TaskID + "/delegate/" + child.ID,
		CorrelationID: parent.CorrelationID,
		BusinessID:    parent.BusinessID,
		DivisionID:    parent.DivisionID,
		ActorID:       parent.ActorID,
		Intent:        child.Intent,
		Input: map[string]string{"agent_execution": mustMarshalSpec(&Spec{
			AgentID: child.AgentID, RequiredCapabilities: child.Capabilities, Tools: child.Tools,
		})},
		Constraints: parent.Constraints,
		Priority:    parent.Priority,
	}
	return r.execute(ctx, childReq, ag, depth+1)
}

// runWorkflow executes a WorkflowSpec sequentially or with a bounded
// parallel pool. Sequential: first-failure wins. Parallel: all nodes
// complete; workflow succeeds only if every node succeeds; node failures
// attach to the workflow result. Cancellation propagates through ctx.
func (r *Runtime) runWorkflow(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent, spec *Spec, start time.Time, depth int, cache map[string]*Spec) (*executor.Outcome, error) {
	wf := spec.Workflow
	r.emit(event.EventTypeWorkflowStarted, req, req.ActorID, string(wf.Strategy), nil)
	if err := validateWorkflow(wf); err != nil {
		return failedOutcome(req, ag, start, err.Error()), nil
	}
	results := map[string]*executor.Outcome{}
	var mu sync.Mutex
	fail := func(nodeID, err string) {
		mu.Lock()
		defer mu.Unlock()
		results[nodeID] = &executor.Outcome{TaskID: req.TaskID + "/" + nodeID, Status: "failed", Error: err,
			CorrelationID: req.CorrelationID, BusinessID: req.BusinessID, CreatedAt: start, CompletedAt: time.Now()}
	}
	if wf.Strategy == StrategyParallel {
		// Trivial dependency honoring: a node only runs after its depends_on
		// nodes finish; with just DAG declared at the API edge, dependencies
		// become ordered groups. Keep simple: resolve dependency topologically.
		order := topoSort(wf.Nodes)
		for _, grp := range order {
			sem := make(chan struct{}, r.maxFan)
			var wg sync.WaitGroup
			for _, n := range grp {
				node := n
				wg.Add(1)
				sem <- struct{}{}
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					r.runNode(ctx, req, ag, spec, node, depth+1, cache, &mu, results, fail)
				}()
			}
			wg.Wait()
		}
	} else {
		for _, n := range wf.Nodes {
			if err := ctx.Err(); err != nil {
				return cancelledOutcome(req, start), nil
			}
			r.runNode(ctx, req, ag, spec, n, depth+1, cache, &mu, results, fail)
			if results[n.ID].Status != "completed" {
				return failedOutcome(req, ag, start, fmt.Sprintf("node %s failed: %s", n.ID, results[n.ID].Error)), nil
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	failedSteps := []string{}
	completedOutputs := []string{}
	for _, n := range wf.Nodes {
		r2 := results[n.ID]
		if r2 == nil {
			failedSteps = append(failedSteps, n.ID+": did not run")
			continue
		}
		if r2.Status != "completed" {
			failedSteps = append(failedSteps, n.ID+": "+r2.Error)
		} else {
			completedOutputs = append(completedOutputs, r2.Output)
		}
	}
	if len(failedSteps) > 0 {
		r.emit(event.EventTypeWorkflowFailed, req, req.ActorID, "failed", nil)
		return &executor.Outcome{TaskID: req.TaskID, Status: "failed",
			Error: strings.Join(failedSteps, "; "), Duration: time.Since(start),
			CorrelationID: req.CorrelationID, BusinessID: req.BusinessID, CreatedAt: start, CompletedAt: time.Now()}, nil
	}
	r.emit(event.EventTypeWorkflowCompleted, req, req.ActorID, "completed", nil)
	return &executor.Outcome{TaskID: req.TaskID, Status: "completed",
		Output: strings.Join(completedOutputs, " | "), Duration: time.Since(start),
		CorrelationID: req.CorrelationID, BusinessID: req.BusinessID, CreatedAt: start, CompletedAt: time.Now()}, nil
}

func (r *Runtime) runNode(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent, spec *Spec, n Node, depth int, cache map[string]*Spec, mu *sync.Mutex, results map[string]*executor.Outcome, fail func(nodeID, err string)) {
	sub := &Spec{AgentID: n.AgentID, RequiredCapabilities: n.Capabilities, Tools: n.Tools}
	nodeReq := &executor.WorkRequest{
		TaskID: req.TaskID + "/node/" + n.ID, CorrelationID: req.CorrelationID,
		BusinessID: req.BusinessID, DivisionID: req.DivisionID, ActorID: req.ActorID, Intent: n.Intent,
		Input:       map[string]string{"agent_execution": mustMarshalSpec(sub)},
		Constraints: req.Constraints, Priority: req.Priority,
	}
	out, err := r.execute(ctx, nodeReq, ag, depth)
	mu.Lock()
	defer mu.Unlock()
	if err != nil {
		fail(n.ID, err.Error())
		return
	}
	results[n.ID] = out
}

func validateWorkflow(wf *WorkflowSpec) error {
	if len(wf.Nodes) == 0 {
		return errors.New("workflow requires at least one node")
	}
	ids := map[string]struct{}{}
	for _, n := range wf.Nodes {
		if n.ID == "" {
			return errors.New("workflow node id is required")
		}
		if _, dup := ids[n.ID]; dup {
			return fmt.Errorf("duplicate workflow node id %q", n.ID)
		}
		ids[n.ID] = struct{}{}
	}
	for _, n := range wf.Nodes {
		for _, dep := range n.DependsOn {
			if _, ok := ids[dep]; !ok {
				return fmt.Errorf("node %q depends on unknown node %q", n.ID, dep)
			}
		}
	}
	return nil
}

// topoSort groups nodes by dependency depth so parallel execution still
// honors depends_on: nodes with no unmet dependencies in one group, then
// their dependents, and so on. Cycles are rejected.
func topoSort(nodes []Node) [][]Node {
	made := map[string]bool{}
	out := [][]Node{}
	remaining := append([]Node(nil), nodes...)
	for len(remaining) > 0 {
		var group []Node
		var rest []Node
		for _, n := range remaining {
			ready := true
			for _, dep := range n.DependsOn {
				if !made[dep] {
					ready = false
					break
				}
			}
			if ready {
				group = append(group, n)
			} else {
				rest = append(rest, n)
			}
		}
		if len(group) == 0 {
			// cycle
			out = [][]Node{remaining}
			return out
		}
		out = append(out, group)
		for _, n := range group {
			made[n.ID] = true
		}
		remaining = rest
	}
	return out
}
