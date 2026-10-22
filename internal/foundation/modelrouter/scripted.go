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
	"encoding/json"
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
	// Protocol v2 carries the structured request name alongside the prompt:
	// a plan request is answered with a plan even though the v2 instruction
	// envelope no longer starts with the legacy planner prefix. The decision
	// table itself is unchanged — only the framing moved.
	if req.Structured != nil && req.Structured.Name == "agent_plan_v1" {
		content := scriptedPlan(extractAfter(strings.ToLower(prompt.String()), "objective: "))
		return &GenerateResponse{
			RequestID: req.RequestID, ModelID: req.ModelID, Content: content, FinishReason: "stop",
		}, nil
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
//  5. "forget"/"purge" + "memory"   -> memory_delete of key "notes", then complete
//  6. named tool, not yet successfully executed -> tool_call for that tool
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
// A tool observation counts as progress only when its status is ok: a
// governance refusal (pending_approval, denied, escalated) is not an
// execution, so after an approval resume the table re-proposes the waiting
// action — exactly as a real model would.
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
	observedTool := func(name string) bool { return toolExecuted(obsBlock, name) }
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
			return scriptedCompletion(lower, "memory consulted")
		case observed("memory_write"):
			return `{"type":"memory_read","key":"notes"}`
		case observed("memory_delete"):
			return scriptedCompletion(lower, "memory forgotten")
		case forgetMemory(subject):
			return `{"type":"memory_delete","key":"notes"}`
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
	case strings.Contains(subject, "research thoroughly") && webSearchCount(obsBlock) < 2:
		// Deterministic multi-step research: two web.search steps collect
		// distinct evidence (query-keyed by the scripted provider) before the
		// loop completes. This is simulation behavior only — live providers
		// are not represented by this table.
		query := "nexus"
		if webSearchCount(obsBlock) == 1 {
			query = "nexus capabilities"
		}
		return `{"type":"tool_call","tool":"web.search","input":{"query":"` + query + `"}}`
	case strings.Contains(subject, "invalid citation"):
		if ids, ok := scriptedEvidenceIDs(lower); ok && len(ids) > 0 {
			// Deliberately cite a source that is not in this execution so the
			// runtime must reject the whole synthesis.
			return scriptedSynthesisAction([]string{"src-999"}, "complete")
		}
		return `{"type":"complete","result":"objective pursued deterministically"}`
	case strings.Contains(subject, "missing synthesis"):
		if _, ok := scriptedEvidenceIDs(lower); ok {
			return `{"type":"complete","result":"completed without the required synthesis"}`
		}
		return `{"type":"complete","result":"objective pursued deterministically"}`
	case strings.Contains(subject, "partial failure") && observedTool("web.search"):
		return `{"type":"fail","reason":"the downstream analysis step failed after evidence was captured"}`
	case strings.Contains(subject, "duplicate citation"):
		if ids, ok := scriptedEvidenceIDs(lower); ok && len(ids) > 0 {
			return scriptedSynthesisAction([]string{ids[0], ids[0]}, "complete")
		}
		return `{"type":"complete","result":"objective pursued deterministically"}`
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
		return scriptedCompletion(lower, "observation promoted")
	case strings.Contains(subject, "promote") && observationID(obsBlock) != "":
		// Promotion names an observation the RUNTIME produced in this execution.
		// The model supplies no content: the platform takes it from the
		// observation, together with its outcome and provenance.
		return fmt.Sprintf(`{"type":"memory_write","key":"promoted-observation","observation_id":%q}`,
			observationID(obsBlock))
	case strings.Contains(subject, "replan") && budgetUsed(budgetLine, "replans") == 0:
		return `{"type":"replan","reason":"the first approach was not sufficient"}`
	default:
		if ids, ok := scriptedEvidenceIDs(lower); ok {
			status := "complete"
			if len(ids) == 0 {
				status = "inconclusive"
			} else if scriptedPlanHasIncompleteStep(lower) {
				status = "partial"
			}
			return scriptedSynthesisAction(ids, status)
		}
		return `{"type":"complete","result":"objective pursued deterministically"}`
	}
}

// scriptedEvidenceIDs reads only the runtime's current-execution EVIDENCE block
// from the prompt. The second return value reports that a v2 evidence block is
// present (even when it captured zero sources), so the simulation provider can
// return an honest typed synthesis. Legacy v1 evidence is deliberately ignored.
func scriptedEvidenceIDs(lower string) ([]string, bool) {
	const marker = "evidence (validated, current execution only;"
	i := strings.Index(lower, marker)
	if i < 0 {
		return nil, false
	}
	header := lower[i:]
	if end := strings.Index(header, "\n"); end >= 0 {
		header = header[:end]
	}
	if !strings.Contains(header, "version=2") {
		return nil, false
	}
	ids := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for _, line := range strings.Split(lower[i:], "\n") {
		const idMarker = "- source_id="
		j := strings.Index(line, idMarker)
		if j < 0 {
			continue
		}
		rest := line[j+len(idMarker):]
		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			end = len(rest)
		}
		id := strings.TrimSpace(rest[:end])
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
		if len(ids) >= 4 {
			break
		}
	}
	return ids, true
}

// scriptedPlanHasIncompleteStep inspects the runtime's STEP_STATES line. The
// currently executing step is excluded because its completion is the action
// being decided. This is simulation context reading, never model authority.
func scriptedPlanHasIncompleteStep(lower string) bool {
	current := ""
	if step := extractAfter(lower, "step: "); step != "" {
		if fields := strings.Fields(step); len(fields) > 0 {
			current = fields[0]
		}
	}
	states := extractAfter(lower, "step_states (runtime facts, read-only): ")
	if states == "" {
		return false
	}
	for _, entry := range strings.Split(states, ",") {
		entry = strings.TrimSpace(entry)
		eq := strings.Index(entry, "=")
		if eq <= 0 {
			continue
		}
		id := entry[:eq]
		state := entry[eq+1:]
		if open := strings.IndexAny(state, "("); open >= 0 {
			state = state[:open]
		}
		if id == current {
			continue
		}
		if state != "completed" {
			return true
		}
	}
	return false
}

type scriptedSynthesisStatement struct {
	Statement string   `json:"statement"`
	SourceIDs []string `json:"source_ids"`
}

type scriptedSynthesisInference struct {
	Statement string `json:"statement"`
}

type scriptedSynthesisPayload struct {
	Status              string                       `json:"status"`
	Summary             string                       `json:"summary"`
	EvidenceBacked      []scriptedSynthesisStatement `json:"evidence_backed"`
	Inferences          []scriptedSynthesisInference `json:"inferences"`
	Conflicts           []scriptedSynthesisStatement `json:"conflicts"`
	UnresolvedQuestions []string                     `json:"unresolved_questions"`
}

type scriptedSynthesisEnvelope struct {
	Type      string                   `json:"type"`
	Result    string                   `json:"result,omitempty"`
	Synthesis scriptedSynthesisPayload `json:"synthesis"`
}

// scriptedCompletion emits a typed synthesis whenever the runtime has provided
// a v2 EVIDENCE block. Deterministic branches that finish after a web.search
// must still satisfy the completion gate; the plain result form is only for
// objectives that never captured evidence.
func scriptedCompletion(lower, result string) string {
	if ids, ok := scriptedEvidenceIDs(lower); ok {
		status := "complete"
		if len(ids) == 0 {
			status = "inconclusive"
		} else if scriptedPlanHasIncompleteStep(lower) {
			status = "partial"
		}
		return scriptedSynthesisAction(ids, status)
	}
	return fmt.Sprintf(`{"type":"complete","result":%q}`, result)
}

func scriptedSynthesisAction(ids []string, status string) string {
	payload := scriptedSynthesisPayload{
		Status:              status,
		Summary:             "Synthesized from 0 captured sources in this execution.",
		EvidenceBacked:      []scriptedSynthesisStatement{},
		Inferences:          []scriptedSynthesisInference{},
		Conflicts:           []scriptedSynthesisStatement{},
		UnresolvedQuestions: []string{},
	}
	switch status {
	case "inconclusive":
		payload.Summary = "The search returned no source records, so no evidence-backed conclusion is supported."
		payload.UnresolvedQuestions = []string{"What evidence would resolve the objective?"}
	default:
		cited := append([]string(nil), ids...)
		if len(cited) > 4 {
			cited = cited[:4]
		}
		payload.Summary = fmt.Sprintf("Synthesized from %d captured source(s) in this execution.", len(cited))
		payload.EvidenceBacked = append(payload.EvidenceBacked, scriptedSynthesisStatement{
			Statement: fmt.Sprintf("The captured source records (%d) describe the requested subject.", len(cited)),
			SourceIDs: cited,
		})
		payload.Inferences = append(payload.Inferences, scriptedSynthesisInference{
			Statement: "The runtime boundary between model proposals and authoritative execution appears deliberate.",
		})
		if status == "partial" {
			payload.Summary = fmt.Sprintf("Synthesized from %d captured source(s); one or more plan steps did not complete.", len(cited))
			payload.UnresolvedQuestions = append(payload.UnresolvedQuestions, "Which incomplete step is still required to settle the objective?")
		}
	}
	body, _ := json.Marshal(scriptedSynthesisEnvelope{
		Type: "complete", Result: "source-grounded synthesis", Synthesis: payload,
	})
	return string(body)
}

// forgetMemory selects the destructive memory branch: only an objective that
// explicitly asks to forget/purge memory ever emits memory_delete, so a
// deterministic objective can never mutate memory it did not name.
func forgetMemory(subject string) bool {
	return strings.Contains(subject, "forget") || strings.Contains(subject, "purge")
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

// toolExecuted reports whether an observation shows the named tool actually
// RUNNING, i.e. a line whose status token is "ok" and whose source is that
// tool. A governance refusal (pending_approval, denied, escalated) is
// recorded too, but it never executed anything — after an approval resume the
// decision table must re-propose the waiting action, exactly as a real model
// would when it sees an action that was paused, not performed.
func toolExecuted(obsBlock, name string) bool {
	const src = "] source=tool:"
	for _, line := range strings.Split(obsBlock, "\n") {
		i := strings.Index(line, src)
		if i < 0 {
			continue
		}
		// The source token is followed by " text=..." on the same line.
		if !strings.HasPrefix(line[i:], src+name+" ") {
			continue
		}
		if strings.HasSuffix(line[:i+1], " ok]") {
			return true
		}
	}
	return false
}

// webSearchCount reports how many DISTINCT web.search observations completed
// with an ok status in the runtime's observation data, so the decision table
// can bound multi-step research to a fixed number of search steps. The same
// observation can appear more than once in the prompt (the context assembler
// echoes observations alongside the direct observation block), so identity is
// the observation id, never the line count.
func webSearchCount(obsBlock string) int {
	seen := map[string]struct{}{}
	const src = "] source=tool:web.search "
	for _, line := range strings.Split(obsBlock, "\n") {
		i := strings.Index(line, src)
		if i < 0 {
			continue
		}
		if !strings.HasSuffix(line[:i+1], " ok]") {
			continue
		}
		id := observationIDFromLine(line[:i])
		if id == "" {
			id = line[:i+1]
		}
		seen[id] = struct{}{}
	}
	return len(seen)
}

// observationIDFromLine extracts the [obs-...] token from the metadata prefix
// of one prompt observation line.
func observationIDFromLine(prefix string) string {
	start := strings.Index(prefix, "[obs-")
	if start < 0 {
		return ""
	}
	rest := prefix[start+1:]
	end := strings.IndexAny(rest, " ]")
	if end < 0 {
		return ""
	}
	return rest[:end]
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
