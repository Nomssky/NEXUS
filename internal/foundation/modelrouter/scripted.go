package modelrouter

// ScriptedProvider is a **deterministic simulation provider**. It exists for
// contract tests and for exercising the agent intelligence control loop
// end-to-end through the shipped binary without a real LLM. It is NOT a
// production provider and it never pretends to be intelligent: its answers are
// a tiny, documented decision table over the prompt the runtime sends.
//
// It is seeded only when `models.seeded_provider_mode = "scripted"`
// (env `NEXUS_SEEDED_PROVIDER_MODE`), never by default.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// subjectURL extracts the first URL token from the caller's own text (the
// objective or step description) — never from the runtime envelope.
func subjectURL(lower string, i int) string {
	rest := lower[i:]
	end := strings.IndexAny(rest, " \t\n\"},")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// fixtureURL lets the deterministic simulation provider point at the
// black-box suite's local fixture server (loopback must be explicitly
// enabled via NEXUS_TOOL_HTTP_ALLOW_LOOPBACK=true for this to succeed).
func fixtureURL() string {
	if v := os.Getenv("NEXUS_SEEDED_HTTP_FIXTURE"); v != "" {
		return v
	}
	return "http://127.0.0.1:18089/fixture"
}

// NewScriptedProvider builds the deterministic simulation provider.
func NewScriptedProvider(cfg ProviderConfig) *ScriptedProvider {
	return &ScriptedProvider{config: cfg, status: initialProviderStatus(cfg.Status)}
}

// ScriptedProvider is the deterministic decision-table provider.
type ScriptedProvider struct {
	config ProviderConfig
	status ProviderStatus
	mu     sync.RWMutex
}

func (p *ScriptedProvider) Identify() string { return p.config.ID }

func (p *ScriptedProvider) HealthCheck() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.status == ProviderStatusOffline {
		return fmt.Errorf("provider %s is offline", p.config.ID)
	}
	return nil
}

func (p *ScriptedProvider) ListModels() ([]string, error) { return nil, nil }

func (p *ScriptedProvider) Invoke(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	offline := p.status == ProviderStatusOffline
	p.mu.RUnlock()
	if offline {
		return nil, fmt.Errorf("provider %s is offline", p.config.ID)
	}
	var prompt strings.Builder
	for _, m := range req.Messages {
		prompt.WriteString(m.Content)
		prompt.WriteString("\n")
	}
	content := scriptedAnswer(prompt.String())
	return &GenerateResponse{
		RequestID: req.RequestID, ModelID: req.ModelID, Content: content, FinishReason: "stop",
	}, nil
}

// scriptedAnswer is the documented deterministic decision table.
//
// Planning prompt  -> a two-step plan derived from the objective's keywords.
// Decision prompt  -> the first applicable rule:
//  1. "infinite"/"never complete" -> continue (exercises the budget rules)
//  2. "malformed"                  -> prose without JSON (protocol error)
//  3. "delegate", not yet observed -> delegate action
//  4. "memory"                     -> memory_write, then memory_read
//  6. named tool, not yet observed -> tool_call for that tool
//     (calculator, echo, http.request, web.search, filesystem.*, git, data);
//     "post via http" / "delete via http" select a mutating HTTP method
//  7. "promote" with a tool observation in the block -> memory_write of THAT
//     observation (content, outcome and provenance come from the runtime), then
//     complete
//  8. "replan" with replans == 0    -> replan action
//  9. otherwise                    -> complete
//
// "already observed" is read from the observation block the runtime echoes
// into the prompt, and the replan counter is read from the budget line, so the
// policy stays deterministic and progress-aware without any hidden state.
func scriptedAnswer(prompt string) string {
	lower := strings.ToLower(prompt)
	objective := extractAfter(lower, "objective: ")
	stepLine := extractAfter(lower, "step: ")
	budgetLine := extractAfter(lower, "budget: ")
	obsBlock := extractObservations(lower)
	// Keyword matching only ever looks at caller data (objective + step),
	// never at the runtime's own instruction envelope — otherwise the
	// envelope's own grammar words would trigger rules.
	subject := objective + " " + stepLine

	if strings.HasPrefix(strings.TrimSpace(lower), "system: you are the planner") {
		return scriptedPlan(objective)
	}
	observedTool := func(name string) bool { return strings.Contains(obsBlock, "source=tool:"+name) }
	observed := func(kind string) bool { return strings.Contains(obsBlock, " "+kind+" ok]") }

	switch {
	case strings.Contains(subject, "infinite") || strings.Contains(subject, "never complete"):
		return `{"type":"continue"}`
	case strings.Contains(subject, "malformed"):
		return "I believe the best course is to simply do the thing." // no JSON: protocol error
	case strings.Contains(subject, "delegate") && !observed("delegate"):
		return fmt.Sprintf(`{"type":"delegate","agent_id":%q,"objective":"gather supporting detail"}`, scriptedAgent(lower))
	case strings.Contains(subject, "memory"):
		switch {
		case observed("memory_read"):
			return `{"type":"complete","result":"memory consulted"}`
		case observed("memory_write"):
			return `{"type":"memory_read","key":"notes"}`
		default:
			return `{"type":"memory_write","key":"notes","value":"noted during the objective"}`
		}
	case strings.Contains(subject, "calculator") && !observedTool("calculator"):
		return `{"type":"tool_call","tool":"calculator","input":{"a":"6","b":"7","op":"mul"}}`
	case strings.Contains(subject, "echo") && !observedTool("echo"):
		return fmt.Sprintf(`{"type":"tool_call","tool":"echo","input":{"text":%q}}`, objective)
	case (strings.Contains(subject, "http request") || strings.Contains(subject, "http.request") || strings.Contains(subject, "fetch")) && !observedTool("http.request"):
		url := fixtureURL()
		// An explicit URL inside the objective is caller data and wins; the
		// fixture env URL is the fallback.
		if i := strings.Index(lower, "http://"); i >= 0 {
			url = subjectURL(lower, i)
		} else if i := strings.Index(lower, "https://"); i >= 0 {
			url = subjectURL(lower, i)
		}
		// Mutation methods are opt-in per objective so the deterministic table
		// can exercise idempotency keys and unknown outcomes without ever
		// mutating anything the objective did not explicitly ask to mutate.
		method, body := "get", ""
		if strings.Contains(subject, "post via http") {
			method, body = "post", `{"entry":"nexus-e2e-mutation"}`
		}
		if strings.Contains(subject, "delete via http") {
			method = "delete"
		}
		return fmt.Sprintf(
			`{"type":"tool_call","tool":"http.request","operation":%q,"input":{"url":%q,"body":%q,"content_type":"application/json"}}`,
			method, url, body)
	case (strings.Contains(subject, "search the web") || strings.Contains(subject, "web research") || strings.Contains(subject, "web.search")) && !observedTool("web.search"):
		return `{"type":"tool_call","tool":"web.search","input":{"query":"nexus"}}`
	case strings.Contains(subject, "filesystem") && strings.Contains(subject, "..") && !observedTool("filesystem"):
		// Traversal attempt: the sandbox must reject it (black-box probe).
		return `{"type":"tool_call","tool":"filesystem.read","operation":"read","input":{"path":"../../../etc/passwd"}}`
	case strings.Contains(subject, "filesystem read") && !observedTool("filesystem.read"):
		return `{"type":"tool_call","tool":"filesystem.read","operation":"read","input":{"path":"notes/intel.txt"}}`
	case strings.Contains(subject, "filesystem") && !observedTool("filesystem.write"):
		return `{"type":"tool_call","tool":"filesystem.write","operation":"write","input":{"path":"notes/intel.txt","content":"written during objective"}}`
	case strings.Contains(subject, "git") && strings.Contains(subject, "escape") && !observedTool("git"):
		return `{"type":"tool_call","tool":"git","operation":"status","input":{"repo":"../../"}}`
	case strings.Contains(subject, "git status") && !observedTool("git"):
		return `{"type":"tool_call","tool":"git","operation":"status","input":{"repo":"."}}`
	case strings.Contains(subject, "github") && !observedTool("github"):
		return `{"type":"tool_call","tool":"github.issue.list","operation":"execute","input":{"repo":"owner/repo"}}`
	case (strings.Contains(subject, "json") || strings.Contains(subject, "structured data") || strings.Contains(subject, "data.json")) && !observedTool("data"):
		return `{"type":"tool_call","tool":"data","operation":"json.parse","input":{"payload":"{\"a\":1}"}}`
	case strings.Contains(subject, "promote") && observed("memory_write"):
		// The promotion already happened; finish deterministically.
		return `{"type":"complete","result":"observation promoted"}`
	case strings.Contains(subject, "promote") && observationID(obsBlock) != "":
		// Promotion names an observation the RUNTIME produced in this execution.
		// The model supplies no content: the platform takes it from the
		// observation, together with its outcome and provenance.
		return fmt.Sprintf(`{"type":"memory_write","key":"promoted-observation","observation_id":%q}`,
			observationID(obsBlock))
	case strings.Contains(subject, "replan") && budgetUsed(budgetLine, "replans") == 0:
		return `{"type":"replan","reason":"the first approach was not sufficient"}`
	default:
		return `{"type":"complete","result":"objective pursued deterministically"}`
	}
}

// observationID returns the id of the newest observation in the block, so a
// deterministic provider can ask the runtime to promote a record it actually
// produced. It reads the runtime's own observation framing only.
func observationID(obsBlock string) string {
	const marker = "[obs-"
	i := strings.LastIndex(obsBlock, marker)
	if i < 0 {
		return ""
	}
	rest := obsBlock[i+len(marker):]
	end := strings.IndexAny(rest, " \n\t]")
	if end < 0 {
		return ""
	}
	return "obs-" + rest[:end]
}

// extractObservations returns only the observation block of the prompt (the
// data the runtime echoed), so progress tracking never matches the envelope.
func extractObservations(lower string) string {
	i := strings.Index(lower, "observations (data")
	if i < 0 {
		return ""
	}
	rest := lower[i:]
	if j := strings.Index(rest, "budget:"); j > 0 {
		return rest[:j]
	}
	return rest
}

// budgetUsed reads "<name>=used/total" from the prompt's budget line.
func budgetUsed(lower, name string) int {
	i := strings.Index(lower, name+"=")
	if i < 0 {
		return 0
	}
	rest := lower[i+len(name)+1:]
	end := strings.Index(rest, "/")
	if end < 0 {
		return 0
	}
	n := 0
	for _, c := range rest[:end] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func scriptedPlan(objective string) string {
	step1 := `{"step_id":"s1","intent":"gather the required inputs","required_capabilities":[]}`
	step2 := `{"step_id":"s2","intent":"produce the requested result","required_capabilities":[],"dependencies":["s1"]}`
	if strings.Contains(objective, "calculator") {
		step1 = `{"step_id":"s1","intent":"compute the requested value","required_capabilities":[],"allowed_tools":["calculator"]}`
	}
	if strings.Contains(objective, "delegate") {
		step2 = `{"step_id":"s2","intent":"delegate the research subtask and summarize","required_capabilities":[],"dependencies":["s1"]}`
	}
	return fmt.Sprintf(`{"steps":[%s,%s]}`, step1, step2)
}

func extractAfter(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := s[i+len(marker):]
	if j := strings.Index(rest, "\n"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// scriptedAgent picks the first agent id mentioned in the prompt context, when
// the objective asked for a specific one; empty means "let the selector
// decide", which is exactly the delegation contract (§10: the model proposes,
// the selector resolves).
func scriptedAgent(lower string) string {
	const marker = "agent_id="
	i := strings.Index(lower, marker)
	if i < 0 {
		return ""
	}
	rest := lower[i+len(marker):]
	end := strings.IndexAny(rest, " \n\"}")
	if end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}
