package gateway

// Isolated model acceptance for the local research workflow (Phase 12):
//
//	start NEXUS (real child process, isolated data dir, loopback ports, private
//	auth) → submit a research objective against the SCRIPTED web provider
//	(no external network calls) → wait for a terminal result →
//	verify the durable research record over HTTP → stop the child → start a new
//	child on the same data directory → assert the record and its artifacts are
//	identical and that recovery created nothing.
//
// This test ISOLATES local-model decision quality from external web-provider
// failures (Wikipedia HTTP 503/429). It uses the deterministic
// ScriptedResearchProvider built into NEXUS, which returns fixed sources
// for known queries. The model, planner, tool invocation, synthesis,
// evaluation, and durability paths are ALL real — only the web backend is
// faked at the capability boundary.
//
// It is OPT-IN: ordinary `go test ./...` never starts Ollama and never reaches
// the network. Enable it explicitly, and treat a failure as a failure:
//
//	NEXUS_ISOLATED_ACCEPTANCE=1 go test ./internal/gateway/ -run TestIsolatedOllamaScriptedRestartAcceptance -v -timeout 10m
//
// The test owns its configuration: it writes a temporary config declaring the
// installed local Ollama model (bonsai-2:9b by default) and ONLY that model,
// never touching any deployment configuration or credential file, and it never
// sends anything to a paid or credentialed provider.

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const isolatedAcceptanceEnv = "NEXUS_ISOLATED_ACCEPTANCE"

// verifyIsolatedRecord asserts the invariants of an isolated-generated durable
// research record: it uses scripted sources (from the deterministic provider),
// its schema versions are the current ones, citations resolve, and the
// evaluation fingerprint is bound to the synthesis it evaluated.
func verifyIsolatedRecord(t *testing.T, rec map[string]interface{}, execID string) {
	t.Helper()
	if rec["source"] != "durable_journal" {
		t.Fatalf("source = %v", rec["source"])
	}
	if rec["execution_id"] != execID {
		t.Fatalf("execution id = %v, want %v", rec["execution_id"], execID)
	}
	if rec["state"] != "completed" {
		t.Fatalf("state = %v, want completed", rec["state"])
	}
	for _, f := range []string{"created_at", "updated_at", "completed_at"} {
		raw, _ := rec[f].(string)
		if raw == "" {
			t.Fatalf("missing %q on a completed isolated record", f)
		}
		if _, err := time.Parse(time.RFC3339Nano, raw); err != nil {
			if _, err2 := time.Parse(time.RFC3339, raw); err2 != nil {
				t.Fatalf("unparseable timestamp %s=%q", f, raw)
			}
		}
	}
	if _, ok := rec["recovery"]; ok {
		t.Fatalf("a completed record must not carry recovery metadata: %v", rec["recovery"])
	}

	// --- evidence from the SCRIPTED capability ---
	ev, ok := rec["research_evidence"].(map[string]interface{})
	if !ok {
		t.Fatalf("research_evidence missing from a completed isolated record: keys=%v", keysOf(rec))
	}
	if n, _ := ev["version"].(float64); int(n) < int(ResearchEvidenceProvenanceVersion) {
		t.Fatalf("evidence schema version = %v", ev["version"])
	}
	if ev["capability"] != ResearchEvidenceCapability {
		t.Fatalf("capability = %v, want the mediated web capability", ev["capability"])
	}
	sources, _ := ev["sources"].([]interface{})
	if len(sources) == 0 {
		t.Fatal("an isolated research run must capture at least one source")
	}
	if n, _ := ev["count"].(float64); int(n) != len(sources) {
		t.Fatalf("count %v != %d sources", ev["count"], len(sources))
	}
	captured := map[string]bool{}
	for _, rawS := range sources {
		s, ok := rawS.(map[string]interface{})
		if !ok {
			t.Fatalf("source not an object: %v", rawS)
		}
		id, _ := s["id"].(string)
		title, _ := s["title"].(string)
		url, _ := s["url"].(string)
		if id == "" || title == "" || url == "" {
			t.Fatalf("source missing id/title/url: %+v", s)
		}
		// The run must use the scripted provider (example.com/docs.example), NOT Wikipedia.
		if strings.Contains(url, "wikipedia.org") {
			t.Fatalf("source url is Wikipedia but should be scripted: %q", url)
		}
		if !strings.Contains(url, "example") {
			t.Logf("source url (expected scripted): %q", url)
		}
		captured[id] = true
	}
	// Provenance, when present, must be the runtime's own authoritative record.
	if plan, ok := ev["plan"].(map[string]interface{}); ok {
		if pid, _ := plan["plan_id"].(string); pid != execID {
			t.Fatalf("plan_id = %q, want %q", pid, execID)
		}
		if prov, _ := plan["provenance"].(string); prov != ResearchProvenanceAuthoritative {
			t.Fatalf("plan provenance = %q", prov)
		}
	}

	// --- synthesis, bound to the execution and its sources ---
	syn, ok := rec["research_synthesis"].(map[string]interface{})
	if !ok {
		t.Fatalf("research_synthesis missing from a completed isolated record: keys=%v", keysOf(rec))
	}
	if n, _ := syn["version"].(float64); int(n) != ResearchSynthesisVersion {
		t.Fatalf("synthesis version = %v", syn["version"])
	}
	if m, _ := syn["method"].(string); m != ResearchSynthesisMethod {
		t.Fatalf("synthesis method = %v", syn["method"])
	}
	if sid, _ := syn["execution_id"].(string); sid != execID {
		t.Fatalf("synthesis task binding = %q", sid)
	}
	st, _ := syn["status"].(string)
	if st != ResearchSynthesisStatusComplete && st != ResearchSynthesisStatusPartial {
		// Honest partial is acceptable; fabricated or absent status is not.
		t.Fatalf("synthesis status = %q (complete or honest partial required)", st)
	}
	if sum, _ := syn["summary"].(string); strings.TrimSpace(sum) == "" {
		t.Fatal("synthesis summary must be non-empty")
	}
	fp, _ := syn["evidence_fingerprint"].(string)
	if len(fp) != 64 {
		t.Fatalf("synthesis evidence_fingerprint = %d chars, want 64", len(fp))
	}
	stmts, _ := syn["evidence_backed"].([]interface{})
	if len(stmts) == 0 {
		t.Fatalf("synthesis must cite at least one captured source, status=%q", st)
	}
	for _, rawS := range stmts {
		row, _ := rawS.(map[string]interface{})
		ids, _ := row["source_ids"].([]interface{})
		if len(ids) == 0 {
			t.Fatalf("cited statement has no source ids: %+v", row)
		}
		for _, rawID := range ids {
			id, _ := rawID.(string)
			if !captured[id] {
				t.Fatalf("synthesis cites %q which is not a captured source id", id)
			}
		}
	}

	// --- evaluation bound to that same synthesis by fingerprint ---
	eval, ok := rec["research_evaluation"].(map[string]interface{})
	if !ok {
		t.Fatalf("research_evaluation missing from a completed isolated record: keys=%v", keysOf(rec))
	}
	if n, _ := eval["version"].(float64); int(n) != ResearchEvaluationVersion {
		t.Fatalf("evaluation version = %v", eval["version"])
	}
	if esf, _ := eval["evidence_fingerprint"].(string); esf != fp {
		t.Fatalf("evaluation fingerprint is not bound to the synthesis evidence: eval=%s syn=%s", esf[:24], fp[:24])
	}
	if _, ok := eval["status"].(string); !ok {
		t.Fatal("evaluation status missing")
	}
	dims, _ := eval["dimensions"].([]interface{})
	if len(dims) != 8 {
		t.Fatalf("evaluation dimensions = %d, want 8", len(dims))
	}
	t.Logf("isolated record OK: sources=%d cited_rows=%d synth_status=%s eval_status=%v",
		len(sources), len(stmts), st, eval["status"])
}

// TestIsolatedOllamaScriptedRestartAcceptance is the explicit isolated gate.
// It is skipped unless NEXUS_ISOLATED_ACCEPTANCE is set, so a default CI run
// never starts Ollama and never reaches the network.
//
// It uses the SCRIPTED web provider (NEXUS_CAPABILITY_WEB=scripted) which
// returns deterministic sources from docs.example and example.org. No external
// network calls are made. The model, planner, tool invocation, synthesis,
// evaluation, and durability paths are all real.
func TestIsolatedOllamaScriptedRestartAcceptance(t *testing.T) {
	if os.Getenv(isolatedAcceptanceEnv) == "" {
		t.Skip("isolated acceptance is opt-in: set " + isolatedAcceptanceEnv + "=1")
	}
	endpoint, model := liveOllama()

	binPath := buildNexusBinary(t)
	dataDir := t.TempDir()
	healthPort, gatewayPort := reservePorts(t)
	configPath := writeTestConfig(t, healthPort, endpoint, model)
	extraEnv := map[string]string{
		"NEXUS_CAPABILITY_WEB": "scripted",
	}

	// ---- boundary 1: first process lifetime --------------------------------
	proc := startNexusProcessWith(t, binPath, configPath, dataDir, healthPort, gatewayPort, extraEnv)
	defer proc.stop(t)
	proc.waitForHealth(t)

	// A research agent allowed to use the mediated capability.
	const agentID = "research-isolated-acceptance"
	code, body := livePost(t, proc.baseURL+"/api/v1/agents",
		`{"entity_id":"`+agentID+`","name":"Isolated Research Analyst","business_id":"default",`+
			`"capabilities":["analysis"],"allowed_tools":["web.search"]}`,
		map[string]string{"X-Actor-ID": "nx:human:bootstrap", "X-Actor-Credential": "boot-secret"})
	if code >= 300 && !strings.Contains(body, "CONFLICT") {
		t.Fatalf("register agent: HTTP %d %s", code, body)
	}

	// Submit one bounded research objective. Use "capabilities" to trigger
	// the scripted provider's capability-specific sources.
	objBody := `{"business_id":"default","actor_id":"nx:human:bootstrap","objective":` +
		`{"description":"Search for Nexus capabilities and agent model, then summarize the sources",` +
		`"budget":{"max_iterations":8,"max_model_calls":8,"max_tool_calls":3}}}`
	code, body = livePost(t, proc.baseURL+"/api/v1/intelligence/execute", objBody,
		map[string]string{"X-Actor-ID": "nx:human:bootstrap", "X-Actor-Credential": "boot-secret"})
	if code != 202 {
		t.Fatalf("submit objective: HTTP %d %s", code, body)
	}
	var accepted struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := json.Unmarshal([]byte(body), &accepted); err != nil {
		t.Fatalf("decode acceptance: %v (%s)", err, body)
	}
	execID := accepted.ExecutionID
	if execID == "" {
		t.Fatalf("no execution id in acceptance: %s", body)
	}
	t.Logf("submitted isolated objective execution=%s model=%s", execID, model)

	// Poll to a terminal result with a bounded budget; a failure here is a
	// real failure, never a skip.
	var terminal map[string]interface{}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		st, raw := liveGet(t, proc.baseURL+"/api/v1/intelligence/"+execID+"?business_id=default",
			map[string]string{"X-Actor-ID": "nx:human:bootstrap", "X-Actor-Credential": "boot-secret"})
		if st == 202 || st == 200 {
			if st == 200 {
				if err := json.Unmarshal([]byte(raw), &terminal); err != nil {
					t.Fatalf("decode terminal result: %v", err)
				}
				break
			}
			time.Sleep(2 * time.Second)
			continue
		}
		t.Fatalf("poll HTTP %d: %s", st, raw)
	}
	if terminal == nil {
		t.Fatalf("execution %s never reached a terminal state within 4m", execID)
	}
	status, _ := terminal["status"].(string)
	if status != "completed" {
		// Sanitized: the message only carries the structured failure code/reason.
		msg := ""
		if e, ok := terminal["error"].(map[string]interface{}); ok {
			msg, _ = e["message"].(string)
		}
		t.Fatalf("isolated research run must complete, got status=%q message=%q", status, msg)
	}
	ot, _ := terminal["outcome"].(map[string]interface{})
	if ot == nil {
		t.Fatal("terminal result has no outcome")
	}

	// ---- durable record over HTTP -------------------------------------------
	capture := func() (map[string]interface{}, int) {
		st, raw := liveGet(t,
			proc.baseURL+"/api/v1/recovery/executions/"+execID+"?business_id=default",
			map[string]string{"X-Actor-ID": "nx:human:bootstrap", "X-Actor-Credential": "boot-secret"})
		if st != 200 {
			t.Fatalf("recovery detail HTTP %d: %s", st, raw)
		}
		var rec map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			t.Fatalf("decode recovery detail: %v", err)
		}
		verifyIsolatedRecord(t, rec, execID)
		stCount, rawCount := liveGet(t, proc.baseURL+"/api/v1/recovery/executions?business_id=default",
			map[string]string{"X-Actor-ID": "nx:human:bootstrap", "X-Actor-Credential": "boot-secret"})
		if stCount != 200 {
			t.Fatalf("recovery list HTTP %d: %s", stCount, rawCount)
		}
		var list struct {
			Count int `json:"count"`
		}
		if err := json.Unmarshal([]byte(rawCount), &list); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return rec, list.Count
	}

	rec1, count1 := capture()

	// Authorization is still enforced inside these lifetimes.
	if st, _ := liveGet(t,
		proc.baseURL+"/api/v1/recovery/executions?business_id=default",
		map[string]string{"X-Actor-ID": "nx:human:bootstrap", "X-Actor-Credential": "wrong"}); st != 401 {
		t.Fatalf("wrong credential must be 401, got %d", st)
	}
	if st, _ := liveGet(t,
		proc.baseURL+"/api/v1/recovery/executions?business_id=not-yours",
		map[string]string{"X-Actor-ID": "nx:human:bootstrap", "X-Actor-Credential": "boot-secret"}); st != 403 {
		t.Fatalf("cross-business access must be 403, got %d", st)
	}

	// ---- boundary 2: stop, then a NEW process on the same data dir ---------
	proc.stop(t)
	proc2 := startNexusProcessWith(t, binPath, configPath, dataDir, healthPort, gatewayPort, extraEnv)
	defer proc2.stop(t)
	proc2.waitForHealth(t)

	rec2, count2 := capture()
	if !reflect.DeepEqual(rec1, rec2) {
		a, _ := json.MarshalIndent(rec1, "", "  ")
		b, _ := json.MarshalIndent(rec2, "", "  ")
		t.Fatalf("isolated record changed across restart:\n--- before ---\n%s\n--- after ---\n%s", a, b)
	}
	if count1 != count2 {
		t.Fatalf("recovery created records: before=%d after=%d", count1, count2)
	}
	// The process-local result surface is deliberately not durable: it must
	// 404 while the durable record stays readable.
	if st, _ := liveGet(t, proc2.baseURL+"/api/v1/intelligence/"+execID+"?business_id=default",
		map[string]string{"X-Actor-ID": "nx:human:bootstrap", "X-Actor-Credential": "boot-secret"}); st != 404 {
		t.Fatalf("process-local execution surface must 404 after restart, got %d", st)
	}

	t.Logf("ISOLATED ACCEPTANCE PASSED: execution=%s model=%s sources=%d records_before=%d records_after=%d (identical across restart)",
		execID, model, len(func() []interface{} {
			ev, _ := rec1["research_evidence"].(map[string]interface{})
			s, _ := ev["sources"].([]interface{})
			return s
		}()), count1, count2)
}
