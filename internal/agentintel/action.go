package agentintel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ActionType is the closed set of things a model may propose (§6). There is no
// escape hatch: anything else is a protocol error.
type ActionType string

const (
	ActionModelCall    ActionType = "model_call"
	ActionToolCall     ActionType = "tool_call"
	ActionDelegate     ActionType = "delegate"
	ActionMemoryRead   ActionType = "memory_read"
	ActionMemoryWrite  ActionType = "memory_write"
	ActionMemoryDelete ActionType = "memory_delete"
	ActionReplan       ActionType = "replan"
	ActionComplete     ActionType = "complete"
	ActionFail         ActionType = "fail"
	ActionContinue     ActionType = "continue"
)

var validActions = map[ActionType]struct{}{
	ActionModelCall: {}, ActionToolCall: {}, ActionDelegate: {},
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
	Tool  string            `json:"tool,omitempty"`
	Input map[string]string `json:"input,omitempty"`
	// delegate
	AgentID   string `json:"agent_id,omitempty"`
	Objective string `json:"objective,omitempty"`
	// memory_*
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
	// complete / fail / replan
	Result string `json:"result,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ParseAction extracts the single structured action from a model response.
// The response must contain a JSON object with a known "type"; anything else
// is a protocol error — the runtime never guesses an intent from prose.
func ParseAction(content string) (Action, error) {
	obj := extractJSONObject(content)
	if obj == "" {
		return Action{}, fmt.Errorf("protocol: model response contained no JSON action")
	}
	var a Action
	if err := json.Unmarshal([]byte(obj), &a); err != nil {
		return Action{}, fmt.Errorf("protocol: malformed action JSON: %w", err)
	}
	if _, ok := validActions[a.Type]; !ok {
		return Action{}, fmt.Errorf("protocol: unknown action type %q", a.Type)
	}
	return a, nil
}

// ParsePlan extracts a plan from a model response (advisory; validated after).
func ParsePlan(content string) ([]Step, error) {
	obj := extractJSONObject(content)
	if obj == "" {
		return nil, fmt.Errorf("planner: model response contained no JSON plan")
	}
	var envelope struct {
		Steps []Step `json:"steps"`
	}
	if err := json.Unmarshal([]byte(obj), &envelope); err != nil {
		return nil, fmt.Errorf("planner: malformed plan JSON: %w", err)
	}
	if len(envelope.Steps) == 0 {
		return nil, fmt.Errorf("planner: plan has no steps")
	}
	return envelope.Steps, nil
}

// extractJSONObject returns the first balanced {...} block, tolerating prose
// and fenced code around it.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
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
		if c.Used.Delegations >= c.Budget.MaxDelegations {
			return fmt.Errorf("rejected: delegation budget exhausted (%d)", c.Budget.MaxDelegations)
		}
		if c.Depth >= c.Budget.MaxDepth {
			return fmt.Errorf("rejected: delegation depth limit reached (%d)", c.Budget.MaxDepth)
		}
		return nil
	case ActionMemoryRead:
		if strings.TrimSpace(a.Key) == "" {
			return fmt.Errorf("rejected: memory_read needs a key")
		}
		return nil
	case ActionMemoryWrite:
		if strings.TrimSpace(a.Key) == "" {
			return fmt.Errorf("rejected: memory_write needs a key")
		}
		return nil
	case ActionMemoryDelete:
		if strings.TrimSpace(a.Key) == "" {
			return fmt.Errorf("rejected: memory_delete needs a key")
		}
		return nil
	default:
		return fmt.Errorf("protocol: unknown action type %q", a.Type)
	}
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
}

// WorkingMemory is per-execution, process-local scratch state (§11). It dies
// with the execution and is never persisted.
type WorkingMemory struct {
	entries map[string]string
}

func NewWorkingMemory() *WorkingMemory {
	return &WorkingMemory{entries: map[string]string{}}
}

func (m *WorkingMemory) Put(k, v string) { m.entries[k] = v }
func (m *WorkingMemory) Get(k string) (string, bool) {
	v, ok := m.entries[k]
	return v, ok
}
func (m *WorkingMemory) Delete(k string) { delete(m.entries, k) }

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
}

// PlanPrompt is the advisory planning request.
type PlanPrompt struct {
	Objective string
	Context   map[string]string
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
