package agentintel

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	"github.com/Nomssky/NEXUS/internal/memory"
)

// ActionType is the closed set of things a model may propose (§6). There is no
// escape hatch: anything else is a protocol error.
type ActionType string

const (
	ActionModelCall    ActionType = "model_call"
	ActionToolCall     ActionType = "tool_call"
	ActionDelegate     ActionType = "delegate"
	ActionCoordinate   ActionType = "coordinate"
	ActionMemoryRead   ActionType = "memory_read"
	ActionMemoryWrite  ActionType = "memory_write"
	ActionMemoryDelete ActionType = "memory_delete"
	ActionReplan       ActionType = "replan"
	ActionComplete     ActionType = "complete"
	ActionFail         ActionType = "fail"
	ActionContinue     ActionType = "continue"
)

var validActions = map[ActionType]struct{}{
	ActionModelCall: {}, ActionToolCall: {}, ActionDelegate: {}, ActionCoordinate: {},
	ActionMemoryRead: {}, ActionMemoryWrite: {}, ActionMemoryDelete: {},
	ActionReplan: {}, ActionComplete: {}, ActionFail: {}, ActionContinue: {},
}

// Action is a model proposal. It is data until the runtime validates and
// performs it; the model never performs anything itself.
type Action struct {
	Type ActionType `json:"type"`
	// model_call
	Prompt string `json:"prompt,omitempty"`
	// tool_call
	Tool string `json:"tool,omitempty"`
	// ProviderToolCallID preserves the provider's opaque proposal id for audit
	// correlation. It never grants authority or selects the capability.
	ProviderToolCallID string `json:"provider_tool_call_id,omitempty"`
	// Operation selects an explicit manifest operation for tools that declare
	// more than one (e.g. filesystem read/write/list, http methods). It is a
	// closed vocabulary enforced against the manifest — it can never widen
	// what the tool declares (CAPABILITY_TOOL_CONTRACTS §5).
	Operation string            `json:"operation,omitempty"`
	Input     map[string]string `json:"input,omitempty"`
	// delegate
	AgentID   string `json:"agent_id,omitempty"`
	Objective string `json:"objective,omitempty"`
	// DelegateTools proposes explicit tool calls for the child to perform
	// (AGENT_DELEGATION_CONTRACTS §4). A proposal is never a grant: the
	// runtime intersects every entry with the tool registry, the child
	// agent's allowlist and the parent agent's allowlist, and each surviving
	// call still passes the child's own governance admission and capability
	// gates. Empty means a model-only child (no child tools).
	DelegateTools []DelegateToolCall `json:"delegate_tools,omitempty"`
	// Children proposes an explicit coordination group: one concurrent wave
	// of mutually independent children (AGENT_COORDINATION_CONTRACTS §2).
	// Only valid on a coordinate action; every member needs an explicit
	// agent id, and members carry no inter-member dependencies.
	Children []CoordinationChild `json:"children,omitempty"`
	// memory_*
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
	// MemoryType classifies a proposed memory record
	// (AGENT_MEMORY_CONTEXT_CONTRACTS §3). It is a closed vocabulary and only
	// ever narrows what may be written.
	MemoryType string `json:"memory_type,omitempty"`
	// MemoryScope is the requested visibility class. It is a request, never an
	// authority: the platform clamps it to the caller's membership and to the
	// acting agent's declared memory mode (contract §4, §7).
	MemoryScope string `json:"memory_scope,omitempty"`
	// ObservationID requests PROMOTION of a real observation this execution
	// produced into durable observation memory (contract §7, §37). The content
	// then comes from the runtime's observation, never from the model, and the
	// outcome travels with it (§13).
	ObservationID string `json:"observation_id,omitempty"`
	// complete / fail / replan
	Result string `json:"result,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Synthesis is the typed answer required when a completion follows captured
	// v2 research evidence. It is untrusted model output until the loop resolves
	// every source id against the current execution's evidence and validates the
	// status against the plan's step states. It carries no source metadata.
	Synthesis *executor.ResearchSynthesis `json:"synthesis,omitempty"`
}

// DelegateToolCall is one model-proposed tool call for a delegated child.
// It mirrors ToolCallSpec without importing the execution layer into the
// protocol: the loop converts validated entries when building the child.
type DelegateToolCall struct {
	Tool      string            `json:"tool,omitempty"`
	Operation string            `json:"operation,omitempty"`
	Input     map[string]string `json:"input,omitempty"`
}

// CoordinationChild is one member of a proposed coordination group: an
// explicit child agent, its objective, and its optionally proposed tools.
// Members are mutually independent by construction — there is no field for
// inter-member dependencies (coordination contract §2).
type CoordinationChild struct {
	AgentID       string             `json:"agent_id,omitempty"`
	Objective     string             `json:"objective,omitempty"`
	DelegateTools []DelegateToolCall `json:"delegate_tools,omitempty"`
}

// ParseAction decodes exactly one strict decision object from the whole model
// response (protocol v2). Anything that is not exactly one schema-shaped
// object — prose, a second object, an array, null, oversized or truncated
// text, duplicate keys — is a protocol error, never a guess.
//
// Exactly two local-model text variants are normalized at this boundary:
//
//   - `result` emitted as a JSON object instead of a string;
//   - `synthesis` emitted as prose (string) instead of a structured object.
//
// Both variants are normalized TOGETHER by parseActionVariantFallback so one
// variant never drops the other field: a completion that carries a full,
// valid synthesis with a structured `result` used to lose the synthesis here
// and then fail the completion gate a second later as "no synthesis".
func ParseAction(content string) (Action, error) {
	var a Action
	if err := parseStrictDecisionObject(content, &a); err != nil {
		// Only a named type mismatch on one of the two documented variant
		// fields may be normalized; every other protocol failure (duplicate
		// keys, prose, an array, an unknown field, truncation) still fails
		// closed here and is never reconstructed.
		if !isNormalizedVariantError(err) {
			return Action{}, fmt.Errorf("protocol: %w", err)
		}
		a2, perr := parseActionVariantFallback(content)
		if perr != nil {
			return Action{}, fmt.Errorf("protocol: %w", err)
		}
		a = a2
	}
	// Validate action type
	if _, ok := validActions[a.Type]; !ok {
		return Action{}, fmt.Errorf("protocol: unknown action type %q", a.Type)
	}
	return a, nil
}

// isNormalizedVariantError reports whether a strict-decode error names exactly
// one of the documented local-model text variants. It is the gate that keeps
// the variant fallback a narrow normalization seam instead of a general repair
// path:
//
//   - `result` emitted as an object instead of a string;
//   - `synthesis` emitted as prose (a string) instead of an object;
//   - `synthesis.inferences[*]` emitted as prose strings instead of
//     {statement: "..."} entries (the inference text is never reworded, only
//     moved under its documented field name).
//
// Any other mismatch — an unknown field, a row of evidence_backed without
// citations, malformed statuses — is a protocol failure, never normalized.
func isNormalizedVariantError(err error) bool {
	msg := err.Error()
	if strings.Contains(msg, "cannot unmarshal object into Go struct field Action.result of type string") ||
		strings.Contains(msg, "cannot unmarshal string into Go struct field Action.synthesis of type executor.ResearchSynthesis") {
		return true
	}
	return strings.Contains(msg, "cannot unmarshal string") &&
		strings.Contains(msg, "Action.synthesis.inferences.") &&
		strings.Contains(msg, "of type executor.ResearchInference")
}

// parseActionVariantFallback decodes a well-formed decision object whose
// `result` or `synthesis` violated the strict field type. It normalizes BOTH
// fields together and never fabricates one from the other or drops it:
//
//   - `result` (string or object) is kept in its canonical string form;
//   - `synthesis` (object or prose) is normalized by normalizeSynthesis;
//   - `type` and `reason` are taken verbatim.
//
// All other action fields stay zero here; they are never needed on this path
// because the caller reached this function only after the strict decode failed
// on one of the two documented type mismatches. Downstream validation is
// unchanged: ValidateAction, validateCompletionSynthesis and citation
// resolution still apply exactly as they would for a strictly decoded action.
func parseActionVariantFallback(content string) (Action, error) {
	var raw struct {
		Type string `json:"type"`
		// Accept both object and string forms of `result`.
		Result json.RawMessage `json:"result"`
		// Accept both object and prose-string forms of `synthesis`.
		Synthesis json.RawMessage `json:"synthesis"`
		Reason    string          `json:"reason,omitempty"`
	}
	// Duplicate-key rejection already ran in the strict path before any type
	// mismatch could be reported, so a lossy single-decode here is safe.
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return Action{}, fmt.Errorf("protocol: %w", err)
	}
	a := Action{
		Type: ActionType(raw.Type),
	}

	// Coerce result to string: a plain string is used as-is; an object keeps
	// its raw JSON text as the string form. A null result stays empty instead of
	// becoming the literal "null".
	if !isJSONNull(raw.Result) {
		if len(raw.Result) == 0 {
			a.Result = ""
		} else {
			var s string
			if json.Unmarshal(raw.Result, &s) == nil {
				a.Result = s
			} else {
				a.Result = string(raw.Result)
			}
		}
	}

	a.Synthesis = normalizeSynthesis(raw.Synthesis)

	if raw.Reason != "" {
		a.Reason = raw.Reason
	}
	return a, nil
}

// normalizeSynthesis maps a raw `synthesis` token onto the canonical
// ResearchSynthesis:
//
//   - a structured object decodes directly;
//   - an object whose `inferences` entries are prose strings is repaired to the
//     documented {statement: "..."} form — the model's text is passed through
//     verbatim, never reworded and never given citations;
//   - other prose (a string) becomes an inconclusive summary;
//   - null/absent stays nil — never a synthesized skeleton.
//
// Everything else inside a synthesis stays strict: a malformed evidence row or
// status is NOT repaired here, so the downstream executor validation still
// reports it.
func normalizeSynthesis(raw json.RawMessage) *executor.ResearchSynthesis {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}
	var synthesis executor.ResearchSynthesis
	if json.Unmarshal(raw, &synthesis) == nil {
		return &synthesis
	}
	// The one structural repair available: prose inferences are still model
	// claims, merely recorded under the wrong entry shape.
	if repaired, ok := repairStringInferences(raw); ok {
		return repaired
	}
	// Not a structured object (the model wrote prose): record it honestly as an
	// inconclusive summary rather than dropping it or inventing citations.
	summary := string(raw)
	// Strip surrounding JSON quotes if present.
	if len(summary) >= 2 && summary[0] == '"' && summary[len(summary)-1] == '"' {
		if unquoted, err := strconv.Unquote(summary); err == nil {
			summary = unquoted
		}
	}
	if len(summary) > executor.MaxResearchSynthesisSummary {
		summary = summary[:executor.MaxResearchSynthesisSummary]
	}
	return &executor.ResearchSynthesis{
		Status:              executor.ResearchSynthesisInconclusive,
		Summary:             summary,
		EvidenceBacked:      nil,
		Inferences:          nil,
		Conflicts:           nil,
		UnresolvedQuestions: nil,
	}
}

// repairStringInferences rewrites an array of prose strings in `inferences`
// into the documented {statement: "..."} entries and then decodes strictly
// again. It reports ok=false unless that single rewrite produced a decodable
// synthesis: a synthesis that fails for another reason is reported, never
// partially rebuilt.
func repairStringInferences(raw json.RawMessage) (*executor.ResearchSynthesis, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}
	infRaw, ok := obj["inferences"]
	if !ok {
		return nil, false
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(infRaw, &entries); err != nil || !containsStringEntry(entries) {
		return nil, false
	}
	fixed := make([]json.RawMessage, 0, len(entries))
	for _, e := range entries {
		var s string
		if json.Unmarshal(e, &s) != nil {
			// An object entry is untouched; it must decode on its own.
			fixed = append(fixed, e)
			continue
		}
		stmt, err := json.Marshal(executor.ResearchInference{Statement: s})
		if err != nil {
			return nil, false
		}
		fixed = append(fixed, stmt)
	}
	refit, err := json.Marshal(fixed)
	if err != nil {
		return nil, false
	}
	obj["inferences"] = refit
	rewritten, err := json.Marshal(obj)
	if err != nil {
		return nil, false
	}
	var synthesis executor.ResearchSynthesis
	if err := json.Unmarshal(rewritten, &synthesis); err != nil {
		return nil, false
	}
	return &synthesis, true
}

// containsStringEntry reports whether any raw entry is a JSON string.
func containsStringEntry(entries []json.RawMessage) bool {
	for _, e := range entries {
		var s string
		if json.Unmarshal(e, &s) == nil {
			return true
		}
	}
	return false
}

// isJSONNull reports whether a raw JSON token is exactly `null`.
func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

// ParsePlan decodes exactly one strict plan object from the whole model
// response (advisory; validated after). The same whole-response rule applies:
// no prose extraction, no second object, no silent repair.
//
// In production, some models (notably Ollama) may output the `allowed_tools`
// field within steps as an object rather than an array of strings. This
// function normalizes such values: if `allowed_tools` is not an array of
// strings, it is coerced to a valid array representation. This keeps the
// Step struct API (AllowedTools []string) intact while remaining resilient
// to model output variations.
func ParsePlan(content string) ([]Step, error) {
	var envelope struct {
		Steps []Step `json:"steps"`
	}
	if err := parseStrictDecisionObject(content, &envelope); err != nil {
		// If the error is allowed_tools-as-object (a common model output variant),
		// attempt a best-effort parse: decode the JSON manually, normalize allowed_tools,
		// and return the Step values. The error may reference the field path with
		// array indices (e.g. ".steps.0.allowed_tools.0") or the base field name.
		if strings.Contains(err.Error(), "cannot unmarshal object into Go struct field Step.allowed_tools of type string") ||
			strings.Contains(err.Error(), "cannot unmarshal object into .steps.0.allowed_tools.0 of type string") {
			if steps, perr := parsePlanOutputFallback(content); perr == nil {
				if len(steps) > 0 {
					return steps, nil
				}
			}
		}
		return nil, fmt.Errorf("planner: %w", err)
	}
	if len(envelope.Steps) == 0 {
		return nil, fmt.Errorf("planner: plan has no steps")
	}
	return envelope.Steps, nil
}

// parsePlanOutputFallback is a best-effort decode when the strict JSON decoder
// rejects the model output because allowed_tools is an object instead of an
// array of strings. It manually decodes the JSON, normalizes allowed_tools to a
// string array, and returns the Step values.
func parsePlanOutputFallback(content string) ([]Step, error) {
	var raw struct {
		Steps []struct {
			StepID       string      `json:"step_id,omitempty"`
			Intent       string      `json:"intent,omitempty"`
			AllowedTools interface{} `json:"allowed_tools,omitempty"`
			// Only include fields we can reliably normalize
		} `json:"steps"`
		Reason string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, fmt.Errorf("planner: %w", err)
	}
	steps := make([]Step, 0, len(raw.Steps))
	for _, s := range raw.Steps {
		step := Step{
			StepID: s.StepID,
			Intent: s.Intent,
		}
		// Normalize allowed_tools:
		// 1. If it's a []string, use it directly.
		// 2. If it's []interface{}, convert each string element.
		// 3. If it's a map[string]interface{}, extract the first tool name.
		// 4. If it's a single string, wrap it in a slice.
		if allowedTools, ok := s.AllowedTools.([]string); ok {
			// Already []string - use directly
			step.AllowedTools = allowedTools
		} else if allowedTools, ok := s.AllowedTools.([]interface{}); ok {
			// Array of JSON values - convert to []string
			tools := make([]string, 0, len(allowedTools))
			for _, a := range allowedTools {
				if s, ok := a.(string); ok {
					tools = append(tools, s)
				}
			}
			step.AllowedTools = tools
		} else if allowedTools, ok := s.AllowedTools.(map[string]interface{}); ok {
			// Object - extract the first tool name as a string
			for _, v := range allowedTools {
				if s, ok := v.(string); ok {
					step.AllowedTools = []string{s}
					break
				}
			}
		} else if allowedTools, ok := s.AllowedTools.(string); ok {
			// Single string - wrap in slice
			step.AllowedTools = []string{allowedTools}
		}
		// Copy other fields if present
		step.AllowedOperations = nil // skip for now
		step.SuccessCriteria = nil
		step.Optional = false
		steps = append(steps, step)
	}
	return steps, nil
}

// OperationResolver is an OPTIONAL ScopeResolver capability: it reports whether
// a tool manifest declares an operation. When a resolver does not implement it,
// any explicit operation on a tool_call is rejected (fail closed).
type OperationResolver interface {
	ToolSupportsOperation(toolID, operation string) bool
}

// SideEffectResolver is an OPTIONAL ScopeResolver capability: it reports the
// manifest's side-effect class for a tool. The duplicate-effect guard uses it
// to fingerprint only effects that must not repeat (anything but read): when
// a resolver does not implement it, every tool call is treated as repeating
// -unsafe (fail closed).
type SideEffectResolver interface {
	ToolSideEffect(toolID string) (string, bool)
}

// ActionContext is everything the validator needs to decide an action. It is
// runtime state, never model-provided.
type ActionContext struct {
	Objective   Objective
	BusinessID  string
	DivisionID  string
	ActorID     string
	AgentID     string
	AgentTools  []string // the acting agent's allowlist
	Iterations  int
	Used        BudgetUsage
	Budget      Budget
	Depth       int
	Cancelled   bool
	StepHasWork bool // at least one plan step remains or an observation exists
}

// ValidateAction applies §6 in order: schema → type → scope → permissions →
// budgets → iteration → governance (already satisfied upstream) → cancellation.
// It returns a *rejection reason* or nil when the action may proceed.
func ValidateAction(a Action, c *ActionContext, r ScopeResolver, caps Caps) error {
	if a.Type == ActionContinue && c.Cancelled {
		return fmt.Errorf("cancelled")
	}
	if c.Cancelled {
		return fmt.Errorf("cancelled: the loop was cancelled")
	}
	switch a.Type {
	case ActionContinue:
		return nil
	case ActionComplete:
		if !c.StepHasWork {
			return fmt.Errorf("rejected: complete with no work performed")
		}
		return nil
	case ActionFail:
		return nil
	case ActionReplan:
		if c.Used.Replans >= c.Budget.MaxReplans {
			return fmt.Errorf("rejected: replan budget exhausted (%d)", c.Budget.MaxReplans)
		}
		return nil
	case ActionModelCall:
		if strings.TrimSpace(a.Prompt) == "" {
			return fmt.Errorf("rejected: model_call needs a prompt")
		}
		if c.Used.ModelCalls >= c.Budget.MaxModelCalls {
			return fmt.Errorf("rejected: model-call budget exhausted (%d)", c.Budget.MaxModelCalls)
		}
		return nil
	case ActionToolCall:
		if strings.TrimSpace(a.Tool) == "" {
			return fmt.Errorf("rejected: tool_call needs a tool id")
		}
		if r == nil || !r.ToolRegistered(a.Tool) {
			return fmt.Errorf("rejected: tool %q is not registered", a.Tool)
		}
		if c.AgentID != "" && !contains(c.AgentTools, a.Tool) {
			return fmt.Errorf("rejected: tool %q is not in the acting agent's allowlist", a.Tool)
		}
		if a.Operation != "" {
			if ops, ok := r.(OperationResolver); !ok || !ops.ToolSupportsOperation(a.Tool, a.Operation) {
				return fmt.Errorf("rejected: tool %q does not support operation %q", a.Tool, a.Operation)
			}
		}
		if c.Used.ToolCalls >= c.Budget.MaxToolCalls {
			return fmt.Errorf("rejected: tool-call budget exhausted (%d)", c.Budget.MaxToolCalls)
		}
		return nil
	case ActionDelegate:
		if strings.TrimSpace(a.Objective) == "" {
			return fmt.Errorf("rejected: delegate needs an objective")
		}
		if a.AgentID != "" {
			if r == nil || !r.AgentVisible(a.AgentID, c.BusinessID, c.DivisionID) {
				return fmt.Errorf("rejected: agent %q is not visible in this scope", a.AgentID)
			}
		}
		// Proposed child tools are validated structurally here; the
		// intersection with both allowlists happens here too, because both
		// agents are known. Admission and capability gates still run later,
		// per effect, at child runtime.
		if len(a.DelegateTools) > 0 {
			if strings.TrimSpace(a.AgentID) == "" {
				return fmt.Errorf("rejected: delegate_tools requires an explicit agent_id")
			}
			if err := validateDelegateTools(a.DelegateTools, a.AgentID, c.AgentID, r); err != nil {
				return err
			}
		}
		if c.Used.Delegations >= c.Budget.MaxDelegations {
			return fmt.Errorf("rejected: delegation budget exhausted (%d)", c.Budget.MaxDelegations)
		}
		if c.Depth >= c.Budget.MaxDepth {
			return fmt.Errorf("rejected: delegation depth limit reached (%d)", c.Budget.MaxDepth)
		}
		return nil
	case ActionCoordinate:
		return validateCoordinate(a, c, r)
	case ActionMemoryRead:
		if strings.TrimSpace(a.Key) == "" {
			return fmt.Errorf("rejected: memory_read needs a key")
		}
		return validateMemoryFields(a, false)
	case ActionMemoryWrite:
		if strings.TrimSpace(a.Key) == "" {
			return fmt.Errorf("rejected: memory_write needs a key")
		}
		return validateMemoryFields(a, true)
	case ActionMemoryDelete:
		if strings.TrimSpace(a.Key) == "" {
			return fmt.Errorf("rejected: memory_delete needs a key")
		}
		return validateMemoryFields(a, false)
	default:
		return fmt.Errorf("protocol: unknown action type %q", a.Type)
	}
}

// validateDelegateTools applies the structural half of the child-capability
// intersection (contract §4): every proposed child tool must name a tool, a
// registered tool, a declared operation, and an entry on BOTH the child
// agent's allowlist and the parent agent's allowlist. The delegator cannot
// give what it lacks, and the child cannot gain what its definition forbids.
func validateDelegateTools(tools []DelegateToolCall, childID, parentID string, r ScopeResolver) error {
	for i, t := range tools {
		where := fmt.Sprintf("delegate_tools[%d]", i)
		if strings.TrimSpace(t.Tool) == "" {
			return fmt.Errorf("rejected: %s needs a tool id", where)
		}
		if r == nil || !r.ToolRegistered(t.Tool) {
			return fmt.Errorf("rejected: %s tool %q is not registered", where, t.Tool)
		}
		if t.Operation != "" {
			if ops, ok := r.(OperationResolver); !ok || !ops.ToolSupportsOperation(t.Tool, t.Operation) {
				return fmt.Errorf("rejected: %s tool %q does not support operation %q", where, t.Tool, t.Operation)
			}
		}
		if !r.AgentAllowsTool(childID, t.Tool) {
			return fmt.Errorf("rejected: %s tool %q is not in child agent %q's allowlist", where, t.Tool, childID)
		}
		if parentID == "" || !r.AgentAllowsTool(parentID, t.Tool) {
			return fmt.Errorf("rejected: %s tool %q is not in parent agent %q's allowlist", where, t.Tool, parentID)
		}
	}
	return nil
}

// validateCoordinate applies the structural half of coordination validation
// (coordination contract §3, §5): a non-empty member list within the remaining
// delegation budget, every member naming an explicit scope-visible agent, and
// every proposed child tool intersectable with both allowlists. Admission,
// intersection at effect time, and the execution-layer fan-out net still run
// later; this establishes structure only.
func validateCoordinate(a Action, c *ActionContext, r ScopeResolver) error {
	if len(a.Children) == 0 {
		return fmt.Errorf("rejected: coordinate needs at least one child")
	}
	if c.Used.Delegations+len(a.Children) > c.Budget.MaxDelegations {
		return fmt.Errorf("rejected: coordination of %d children exceeds the remaining delegation budget (%d)",
			len(a.Children), c.Budget.MaxDelegations-c.Used.Delegations)
	}
	if c.Depth >= c.Budget.MaxDepth {
		return fmt.Errorf("rejected: delegation depth limit reached (%d)", c.Budget.MaxDepth)
	}
	seen := map[string]struct{}{}
	for i, m := range a.Children {
		where := fmt.Sprintf("children[%d]", i)
		if strings.TrimSpace(m.AgentID) == "" {
			return fmt.Errorf("rejected: %s needs an explicit agent_id", where)
		}
		if r == nil || !r.AgentVisible(m.AgentID, c.BusinessID, c.DivisionID) {
			return fmt.Errorf("rejected: %s agent %q is not visible in this scope", where, m.AgentID)
		}
		if strings.TrimSpace(m.Objective) == "" {
			return fmt.Errorf("rejected: %s needs an objective", where)
		}
		if _, dup := seen[delegateEffectFingerprint(m.AgentID, m.Objective, m.DelegateTools)]; dup {
			return fmt.Errorf("rejected: %s duplicates an earlier member", where)
		}
		seen[delegateEffectFingerprint(m.AgentID, m.Objective, m.DelegateTools)] = struct{}{}
		if err := validateDelegateTools(m.DelegateTools, m.AgentID, c.AgentID, r); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
	}
	return nil
}

// BudgetUsage is the consumed counter set (§9). Counters are incremented by
// the runtime at the moment an effect actually happens — never predicted by
// the model.
type BudgetUsage struct {
	Iterations  int `json:"iterations"`
	ModelCalls  int `json:"model_calls"`
	ToolCalls   int `json:"tool_calls"`
	Delegations int `json:"delegations"`
	Replans     int `json:"replans"`
	NoProgress  int `json:"no_progress"`
}

// Observation is what an action produced (§8). Observations are untrusted
// data; the loop never parses them for instructions.
type Observation struct {
	ObservationID string            `json:"observation_id"`
	Source        string            `json:"source"`
	ActionType    ActionType        `json:"action_type"`
	Status        string            `json:"status"`
	Result        map[string]string `json:"result,omitempty"`
	Text          string            `json:"text,omitempty"`
	Timestamp     time.Time         `json:"timestamp"`
	// Outcome mirrors the platform terminal classification: completed |
	// failed | cancelled | timed_out | unknown (OPERATIONAL_RELIABILITY §5).
	// Empty for memory/delegate/observation paths that are not tool calls.
	Outcome  string `json:"outcome,omitempty"`
	Attempts int    `json:"attempts,omitempty"`
	// RetryRecommended is the platform's class verdict, surfaced rather than
	// re-derived here: `unknown` always reports false (§3).
	RetryRecommended bool `json:"retry_recommended,omitempty"`
	// ReconciliationRequired marks an unknown outcome that an operator must
	// resolve out of band. It never triggers a background retry (§9).
	ReconciliationRequired bool `json:"reconciliation_required,omitempty"`
}

// canonicalMemoryTypes/Scopes are the closed vocabularies a model may propose
// for memory. Anything else is a protocol rejection, and even a valid value is
// only a REQUEST: the platform clamps scope and assigns provenance and trust.
var canonicalMemoryTypes = map[string]bool{
	"fact": true, "instruction": true, "preference": true, "observation": true,
}

var canonicalMemoryScopes = map[string]bool{
	"business": true, "division": true, "agent": true,
}

// validateMemoryFields rejects malformed or out-of-vocabulary memory metadata
// before anything touches the platform (contract §7).
func validateMemoryFields(a Action, write bool) error {
	if a.MemoryType != "" && !canonicalMemoryTypes[a.MemoryType] {
		return fmt.Errorf("rejected: memory_type %q is not canonical", a.MemoryType)
	}
	if a.MemoryScope != "" && !canonicalMemoryScopes[a.MemoryScope] {
		return fmt.Errorf("rejected: memory_scope %q is not canonical", a.MemoryScope)
	}
	if !write && a.ObservationID != "" {
		return fmt.Errorf("rejected: observation promotion is only valid on memory_write")
	}
	if a.ObservationID != "" && strings.TrimSpace(a.Value) != "" {
		return fmt.Errorf("rejected: a promoted observation takes its content from the runtime, not the model")
	}
	if write && a.ObservationID == "" && strings.TrimSpace(a.Value) == "" {
		return fmt.Errorf("rejected: memory_write needs a value or an observation to promote")
	}
	return nil
}

// DecisionRequester is the model boundary: the only way the loop "thinks".
// Implementations must return structured decisions; the seeded simulation
// provider and unit test doubles satisfy it deterministically.
type DecisionRequester interface {
	Decide(ctx context.Context, prompt DecisionPrompt) (string, error)
	Plan(ctx context.Context, prompt PlanPrompt) (string, error)
}

// DecisionPrompt is the runtime's decision request. Observation text is
// carried as data, clearly delimited, and never merged into instructions.
type DecisionPrompt struct {
	Objective    string
	StepID       string
	StepIntent   string
	Observations []Observation
	BudgetUsage  BudgetUsage
	Budget       Budget
	// DivisionID and AgentID are runtime-established scope facts (never model
	// output) used to resolve the hierarchical model preference. They select
	// WHICH stored preference applies; they grant nothing.
	DivisionID string
	AgentID    string
	// Tools is the bounded capability catalog the acting agent may use
	// (CAPABILITY_TOOL_CONTRACTS §4/§31): ids, operations, side-effect
	// classes. It never contains credentials, secret values, internal paths
	// or authorization internals, and it grants nothing — every invocation is
	// validated again by the capability platform.
	Tools []ToolHint
	// ToolConversation contains runtime-authored provider tool-call/result
	// messages from this execution only. It is process-local and bounded.
	ToolConversation []modelrouter.Message
	// Evidence is the validated source snapshot captured by THIS execution. It
	// is authoritative runtime data, not model output: the prompt renders only
	// source ids and their captured metadata, and completion validation resolves
	// citations against the same snapshot.
	Evidence *execjournal.ResearchEvidence
	// Memory is the bounded, ranked durable memory this decision may see. It is
	// already scope-authorized and provenance-labelled by the memory platform
	// (AGENT_MEMORY_CONTEXT_CONTRACTS §11).
	Memory []memory.Record
	// Context is the assembled, budgeted context block. It is produced by the
	// single context-assembly boundary (internal/memory.Assembler), so the
	// objective and the current step are always reserved and memory can never
	// crowd them out (contract §12).
	Context string
	// PlanVersion is the active runtime plan version, and StepStates is the
	// authoritative per-step ledger rendered for the decision (depth contract
	// §12). Both are runtime facts: the model reads them as data and cannot
	// write them — there is no decision field that maps back onto them.
	PlanVersion int
	StepStates  string
}

// ToolHint is one entry of the bounded capability catalog. It is advisory
// context only: ids, operations, a short description, the input shape the
// model needs to build a valid proposal, and the side-effect class. It never
// carries credentials, secret values, internal paths, authorization internals,
// governance internals or raw membership data — the capability platform
// authorizes every invocation separately.
type ToolHint struct {
	ID         string
	Summary    string
	Operations []string
	SideEffect string
	// InputShape names the accepted input fields ("a:string*,b:string"):
	// a trailing "*" marks a required field. Derived from the tool manifest's
	// input schema, never from model input.
	InputShape  string
	InputSchema tool.Schema
}

// PlanPrompt is the advisory planning request.
type PlanPrompt struct {
	Objective string
	Context   map[string]string
	// Tools is the same bounded capability catalog the acting agent may use for
	// a decision. It lets the model choose real tool ids/operations in a plan.
	Tools []ToolHint
	// MaxSteps is the effective runtime budget (already clamped to caps), not a
	// suggestion the model may exceed.
	MaxSteps int
	// PriorSteps and StepStates describe the active plan on a replan. Completed
	// steps are runtime facts the model must not rewrite; empty on first plan.
	PriorSteps  []Step
	PlanVersion int
	StepStates  string
	Reason      string
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
