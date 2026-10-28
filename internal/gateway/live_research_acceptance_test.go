package gateway

// Explicit live acceptance for the local research workflow (Phase 5/6):
//
//	start NEXUS (real child process, isolated data dir, loopback ports, private
//	auth) → submit a real research objective → wait for a terminal result →
//	verify the durable research record over HTTP → stop the child → start a new
//	child on the same data directory → assert the record and its artifacts are
//	identical and that recovery created nothing.
//
// It is OPT-IN: ordinary `go test ./...` never starts Ollama and never reaches
// the network. Enable it explicitly, and treat a failure as a failure:
//
//	NEXUS_LIVE_ACCEPTANCE=1 go test ./internal/gateway/ -run TestLiveOllamaWikipediaRestartAcceptance -v -timeout 15m
//
// The test owns its configuration: it writes a temporary config declaring the
// installed local Ollama model (bonzai id passed by NEXUS_LIVE_OLLAMA_MODEL,
// default bonsai-2:9b) and ONLY that model/routes the digests, never touching
// any deployment configuration or credential file, and it never sends anything
// to a paid or credentialed provider.

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const liveAcceptanceEnv = "NEXUS_LIVE_ACCEPTANCE"

// verifyLiveRecord asserts the invariants of a live-generated durable research
// record: it is real (Wikipedia sources from the mediated capability), its
// schema versions are the current ones, citations resolve, and the evaluation
// fingerprint is bound to the synthesis it evaluated.
func verifyLiveRecord(t *testing.T, rec map[string]interface{}, execID string) {
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
			t.Fatalf("missing %q on a completed live record", f)
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

	// --- real evidence from the mediated capability ---
	ev, ok := rec["research_evidence"].(map[string]interface{})
	if !ok {
		t.Fatalf("research_evidence missing from a completed live record: keys=%v", keysOf(rec))
	}
	if n, _ := ev["version"].(float64); int(n) < int(ResearchEvidenceProvenanceVersion) {
		t.Fatalf("evidence schema version = %v", ev["version"])
	}
	if ev["capability"] != ResearchEvidenceCapability {
		t.Fatalf("capability = %v, want the mediated web capability", ev["capability"])
	}
	sources, _ := ev["sources"].([]interface{})
	if len(sources) == 0 {
		t.Fatal("a live research run must capture at least one real source")
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
		// The run must be a real keyless Wikipedia retrieval, not a fixture.
		if !strings.Contains(url, "wikipedia.org") {
			t.Fatalf("source url is not a real Wikipedia result: %q", url)
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
		t.Fatalf("research_synthesis missing from a completed live record: keys=%v", keysOf(rec))
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
		t.Fatalf("research_evaluation missing from a completed live record: keys=%v", keysOf(rec))
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
	t.Logf("live record OK: sources=%d cited_rows=%d synth_status=%s eval_status=%v",
		len(sources), len(stmts), st, eval["status"])
}

// TestLiveOllamaWikipediaRestartAcceptance is the explicit live gate. It is
// skipped unless NEXUS_LIVE_ACCEPTANCE is set, so a default CI run never
// reaches Ollama or the network.
func TestLiveOllamaWikipediaRestartAcceptance(t *testing.T) {
	if os.Getenv(liveAcceptanceEnv) == "" {
		t.Skip("live acceptance is opt-in: set " + liveAcceptanceEnv + "=1")
	}
	endpoint, model := liveOllama()

	binPath := buildNexusBinary(t)
	dataDir := t.TempDir()
	healthPort, gatewayPort := reservePorts(t)
	configPath := writeTestConfig(t, healthPort, endpoint, model)
	extraEnv := map[string]string{
		"NEXUS_CAPABILITY_WEB":          "wikipedia",
		"NEXUS_CAPABILITY_WEB_LANGUAGE": "en",
	}

	// ---- boundary 1: first process lifetime --------------------------------
	proc := startNexusProcessWith(t, binPath, configPath, dataDir, healthPort, gatewayPort, extraEnv)
	defer proc.stop(t)
	proc.waitForHealth(t)

	// A research agent allowed to use the mediated capability.
	const agentID = "research-live-acceptance"
	code, body := livePost(t, proc.baseURL+"/api/v1/agents",
		`{"entity_id":"`+agentID+`","name":"Live Research Analyst","business_id":"default",`+
			`"capabilities":["analysis"],"allowed_tools":["web.search"]}`,
		map[string]string{"X-Actor-ID": "nx:human:bootstrap", "X-Actor-Credential": "boot-secret"})
	if code >= 300 && !strings.Contains(body, "CONFLICT") {
		t.Fatalf("register agent: HTTP %d %s", code, body)
	}

	// Submit one bounded research objective.
	objBody := `{"business_id":"default","actor_id":"nx:human:bootstrap","objective":` +
		`{"description":"Search the web for the Wikipedia article on Gemini (software) and summarize the sources",` +
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
	t.Logf("submitted live objective execution=%s model=%s", execID, model)

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
		t.Fatalf("live research run must complete, got status=%q message=%q", status, msg)
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
		verifyLiveRecord(t, rec, execID)
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
		t.Fatalf("live record changed across restart:\n--- before ---\n%s\n--- after ---\n%s", a, b)
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

	t.Logf("LIVE ACCEPTANCE PASSED: execution=%s model=%s sources=%d records_before=%d records_after=%d (identical across restart)",
		execID, model, len(func() []interface{} {
			ev, _ := rec1["research_evidence"].(map[string]interface{})
			s, _ := ev["sources"].([]interface{})
			return s
		}()), count1, count2)
}
