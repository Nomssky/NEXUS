package gateway

// Phase 10 — invocation-record durability across a real OS-process restart.
//
// These tests are fully deterministic: they never start Ollama, never touch the
// network beyond loopback HTTP, and build their data from the production code
// paths (the compiled binary's /api/v1/executions surface for the end-to-end
// case; the exported execjournal API for the classification cases).
//
// READ-SURFACE LIMITATION (stated, not worked around): there is NO supported
// HTTP or exported read API for invocation records. `Journal.getInvocation` is
// unexported and the recovery endpoints expose execution records only. The
// tests therefore read the durable artifact the FileStore documents —
// <dataDir>/invocation/<id>.json — as a black-box inspection of persisted state.
// No production endpoint is invented for the test.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// bootstrapAuth is the header set the child's bootstrap identity accepts.
func bootstrapAuth() map[string]string {
	return map[string]string{"X-Actor-ID": "nx:human:bootstrap", "X-Actor-Credential": "boot-secret"}
}

// invocationRecord is the decoded durable payload (not the store envelope).
type invocationRecord map[string]interface{}

// readInvocationRecords decodes every <dataDir>/invocation/*.json envelope and
// returns its base64 payload keyed by payload id. A malformed payload is
// returned as an error so a test can assert fail-closed behaviour instead of
// silently skipping the file.
func readInvocationRecords(t *testing.T, dataDir string) (map[string]invocationRecord, error) {
	t.Helper()
	dir := filepath.Join(dataDir, "invocation")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]invocationRecord{}, nil
		}
		return nil, err
	}
	out := map[string]invocationRecord{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var env struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, err
		}
		payload, err := base64.StdEncoding.DecodeString(env.Data)
		if err != nil {
			return nil, err
		}
		var rec invocationRecord
		if err := json.Unmarshal(payload, &rec); err != nil {
			return nil, err
		}
		id, _ := rec["id"].(string)
		out[id] = rec
	}
	return out, nil
}

// mustReadInvocationRecords is the happy-path read used everywhere except the
// corruption test.
func mustReadInvocationRecords(t *testing.T, dataDir string) map[string]invocationRecord {
	t.Helper()
	recs, err := readInvocationRecords(t, dataDir)
	if err != nil {
		t.Fatalf("read invocation records: %v", err)
	}
	return recs
}

// registerAgent posts an agent definition and accepts 200 or a duplicate
// CONFLICT (a restart re-registration is idempotent for the test).
func registerAgent(t *testing.T, baseURL, body string) {
	t.Helper()
	code, resp := livePost(t, baseURL+"/api/v1/agents", body, bootstrapAuth())
	if code >= 300 && !containsStr(resp, "CONFLICT") {
		t.Fatalf("register agent: HTTP %d %s", code, resp)
	}
}

// submitExecutionWithTool posts one execution with an explicit consequential
// tool and polls to a terminal state. It asserts the documented lifecycle: 202
// accepted (carrying execution_id), then 202 pending until 200 terminal. It
// returns the accepted execution id and the terminal body.
func submitExecutionWithTool(t *testing.T, baseURL, body string) (string, map[string]interface{}) {
	t.Helper()
	code, resp := livePost(t, baseURL+"/api/v1/executions", body, bootstrapAuth())
	if code != 202 {
		t.Fatalf("submit execution: HTTP %d %s", code, resp)
	}
	var accepted struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := json.Unmarshal([]byte(resp), &accepted); err != nil {
		t.Fatalf("decode acceptance: %v (%s)", err, resp)
	}
	if accepted.ExecutionID == "" {
		t.Fatalf("no execution_id in acceptance: %s", resp)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		st, raw := liveGet(t, baseURL+"/api/v1/executions/"+accepted.ExecutionID+"?business_id=default", bootstrapAuth())
		switch st {
		case 200:
			var terminal map[string]interface{}
			if err := json.Unmarshal([]byte(raw), &terminal); err != nil {
				t.Fatalf("decode terminal: %v", err)
			}
			return accepted.ExecutionID, terminal
		case 202:
			time.Sleep(100 * time.Millisecond)
		default:
			t.Fatalf("poll HTTP %d: %s", st, raw)
		}
	}
	t.Fatalf("execution %s never reached a terminal state", accepted.ExecutionID)
	return "", nil
}

func containsStr(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// waitExit waits for the child to exit and returns its exit code.
func (p *nexusProcess) waitExit(t *testing.T, d time.Duration) int {
	t.Helper()
	select {
	case <-p.exited:
	case <-time.After(d):
		p.stop(t)
		t.Fatalf("process did not exit within %s\nlogs: %s", d, p.logs.String())
	}
	if p.cmd.ProcessState == nil {
		t.Fatalf("process state unavailable after exit")
	}
	return p.cmd.ProcessState.ExitCode()
}

const p10WriteBody = `{"intent":"write a sandboxed note","business_id":"default",` +
	`"actor_id":"nx:human:bootstrap","agent_id":"p10-writer",` +
	`"required_capabilities":["filesystem.write"],` +
	`"tools":[{"tool_id":"filesystem.write","operation":"write",` +
	`"input":{"path":"note.txt","content":"hello durability"}}]}`

const p10AgentBody = `{"entity_id":"p10-writer","name":"P10 Writer","business_id":"default",` +
	`"capabilities":["filesystem.write"],"allowed_tools":["filesystem.write"]}`

// TestInvocationRecordSurvivesProcessRestart is the end-to-end case: a REAL
// child process creates a durable invocation record through the production
// /api/v1/executions path (a consequential filesystem.write), the record is
// read from durable storage, the child is stopped cleanly, a NEW child starts
// against the same data directory, and the record, its execution association,
// its side-effect proof and its timestamps must be unchanged — proving startup
// recovery neither replayed the capability nor reclassified a settled outcome.
func TestInvocationRecordSurvivesProcessRestart(t *testing.T) {
	binPath := buildNexusBinary(t)
	dataDir := t.TempDir()
	healthPort, gatewayPort := reservePorts(t)

	// ---- boundary 1: child creates the invocation --------------------------
	proc1 := startNexusProcess(t, binPath, dataDir, healthPort, gatewayPort)
	defer proc1.stop(t)
	proc1.waitForHealth(t)
	registerAgent(t, proc1.baseURL, p10AgentBody)

	execID, terminal := submitExecutionWithTool(t, proc1.baseURL, p10WriteBody)
	if got, _ := terminal["status"].(string); got != "completed" {
		t.Fatalf("expected a completed execution, got status=%v err=%v", got, terminal["error"])
	}
	if execID == "" {
		t.Fatalf("no execution_id: %v", terminal)
	}

	before := mustReadInvocationRecords(t, dataDir)
	if len(before) != 1 {
		t.Fatalf("expected exactly one invocation record, got %d: %v", len(before), keysOfInv(before))
	}
	var invID string
	for id := range before {
		invID = id
	}
	inv := before[invID]

	// Identity + association, per the actual durable schema.
	if got, _ := inv["execution_id"].(string); got != execID {
		t.Fatalf("invocation execution_id = %q, want %q", got, execID)
	}
	if got, _ := inv["tool_id"].(string); got != "filesystem.write" {
		t.Fatalf("invocation tool_id = %q", got)
	}
	if got, _ := inv["operation"].(string); got != "write" {
		t.Fatalf("invocation operation = %q", got)
	}
	if got, _ := inv["side_effect_class"].(string); got != "write" {
		t.Fatalf("invocation side_effect_class = %q", got)
	}
	if got, _ := inv["status"].(string); got != execjournal.InvCompleted {
		t.Fatalf("invocation status = %q, want %q", got, execjournal.InvCompleted)
	}
	if got, _ := inv["version"].(float64); int(got) != execjournal.SchemaVersion {
		t.Fatalf("invocation schema version = %v", inv["version"])
	}
	for _, f := range []string{"created_at", "updated_at"} {
		raw, _ := inv[f].(string)
		if raw == "" {
			t.Fatalf("invocation %s missing", f)
		}
		if _, err := time.Parse(time.RFC3339Nano, raw); err != nil {
			t.Fatalf("invocation %s is not parseable: %q", f, raw)
		}
	}

	// The execution record is terminally completed and visible through the
	// supported recovery read API.
	execBefore := queryRecoveryAPI(t, proc1.baseURL, execID, "default")
	if got, _ := execBefore["state"].(string); got != "completed" {
		t.Fatalf("durable execution state = %v, want completed", got)
	}
	countBefore := queryRecoveryListCount(t, proc1.baseURL, "default")

	// Proof of the one shot effect: the sandboxed write exists with its bytes.
	notePath := filepath.Join(dataDir, "workspaces", "default", "note.txt")
	noteInfoBefore, err := os.Stat(notePath)
	if err != nil {
		t.Fatalf("sandboxed write artifact missing: %v", err)
	}

	// ---- boundary 2: stop, restart, re-read --------------------------------
	proc1.stop(t)

	proc2 := startNexusProcess(t, binPath, dataDir, healthPort, gatewayPort)
	defer proc2.stop(t)
	proc2.waitForHealth(t)

	after := mustReadInvocationRecords(t, dataDir)
	if len(after) != 1 {
		t.Fatalf("restart must not create or drop invocation records: before=%d after=%d", len(before), len(after))
	}
	invAfter, ok := after[invID]
	if !ok {
		t.Fatalf("invocation %q vanished across restart; now: %v", invID, keysOfInv(after))
	}
	// Field-identical: identity, association, state, outcome and timestamps.
	// A recovery reclassification would change status/error_class/updated_at; a
	// capability replay would create another record.
	if !invRecordsEqual(inv, invAfter) {
		a, _ := json.MarshalIndent(inv, "", "  ")
		b, _ := json.MarshalIndent(invAfter, "", "  ")
		t.Fatalf("invocation record changed across restart:\n--- before ---\n%s\n--- after ---\n%s", a, b)
	}

	// The execution record is unchanged and its count did not grow (no replay).
	execAfter := queryRecoveryAPI(t, proc2.baseURL, execID, "default")
	verifyRecordUnchangedAcrossRestart(t, execBefore, execAfter)
	if got := queryRecoveryListCount(t, proc2.baseURL, "default"); got != countBefore {
		t.Fatalf("execution count changed across restart: before=%d after=%d", countBefore, got)
	}

	// The capability effect was NOT replayed: content and mtime are identical.
	noteInfoAfter, err := os.Stat(notePath)
	if err != nil {
		t.Fatalf("sandboxed write artifact disappeared: %v", err)
	}
	body, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if string(body) != "hello durability" {
		t.Fatalf("artifact content changed: %q", string(body))
	}
	if !noteInfoAfter.ModTime().Equal(noteInfoBefore.ModTime()) {
		t.Fatalf("artifact was rewritten during recovery: mtime %s -> %s",
			noteInfoBefore.ModTime(), noteInfoAfter.ModTime())
	}
}

// TestInvocationRecoveryClassifiesDispatchedOnlyForNonterminalExecution pins
// the documented Recover() rule at the child-process boundary: a dispatched
// invocation whose execution is nonterminal becomes `unknown`/`interrupted`,
// while a dispatched invocation whose execution was already terminal is left
// untouched (terminal is final; recovery must not rewrite settled evidence).
//
// A crash mid-effect is not safely reproducible, so the two dispatched records
// are created through the exported execjournal API — the same call the runtime
// makes — before the child starts. That is the narrowest supported seam; no
// production code is changed and no arbitrary JSON is hand-written.
func TestInvocationRecoveryClassifiesDispatchedOnlyForNonterminalExecution(t *testing.T) {
	const (
		nonTermExec = "exec-inv-nonterminal"
		termExec    = "exec-inv-terminal"
	)
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	if err := j.Accepted(nonTermExec, "default", "", "nx:human:bootstrap"); err != nil {
		t.Fatalf("accept nonterminal: %v", err)
	}
	nonTermID, err := j.RecordDispatchIntent(nonTermExec, "call-nt", "filesystem.write", "write", "write")
	if err != nil {
		t.Fatalf("dispatch intent (nonterminal): %v", err)
	}
	if err := j.Accepted(termExec, "default", "", "nx:human:bootstrap"); err != nil {
		t.Fatalf("accept terminal: %v", err)
	}
	if err := j.Terminal(termExec, execjournal.StateCompleted, ""); err != nil {
		t.Fatalf("terminal: %v", err)
	}
	termID, err := j.RecordDispatchIntent(termExec, "call-t", "filesystem.write", "write", "write")
	if err != nil {
		t.Fatalf("dispatch intent (terminal): %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	binPath := buildNexusBinary(t)
	healthPort, gatewayPort := reservePorts(t)
	proc := startNexusProcess(t, binPath, dir, healthPort, gatewayPort)
	defer proc.stop(t)
	proc.waitForHealth(t)

	recs := mustReadInvocationRecords(t, dir)
	if len(recs) != 2 {
		t.Fatalf("expected 2 invocation records, got %d", len(recs))
	}

	nt := recs[nonTermID]
	if got, _ := nt["status"].(string); got != execjournal.InvUnknown {
		t.Fatalf("dispatched invocation of a nonterminal execution = %q, want %q", got, execjournal.InvUnknown)
	}
	if got, _ := nt["error_class"].(string); got != "interrupted" {
		t.Fatalf("reclassified invocation error_class = %q, want interrupted", got)
	}
	if got, _ := nt["updated_at"].(string); got == "" {
		t.Fatal("reclassified invocation must carry an updated_at")
	}

	tt := recs[termID]
	if got, _ := tt["status"].(string); got != execjournal.InvDispatched {
		t.Fatalf("dispatched invocation of a TERMINAL execution = %q, want %q (must be untouched)",
			got, execjournal.InvDispatched)
	}

	// The nonterminal execution was classified unknown by the same pass; the
	// terminal one stays completed. Both are visible through the read API.
	ntExec := queryRecoveryAPI(t, proc.baseURL, nonTermExec, "default")
	if got, _ := ntExec["state"].(string); got != "unknown" {
		t.Fatalf("nonterminal execution state = %v, want unknown", got)
	}
	rec, ok := ntExec["recovery"].(map[string]interface{})
	if !ok || rec["recovered"] != true || rec["prior_state"] != "accepted" {
		t.Fatalf("nonterminal execution recovery metadata wrong: %v", ntExec["recovery"])
	}
	tExec := queryRecoveryAPI(t, proc.baseURL, termExec, "default")
	if got, _ := tExec["state"].(string); got != "completed" {
		t.Fatalf("terminal execution state = %v, want completed", got)
	}
}

// TestDuplicateInvocationOutcomeIsIdempotentPerRecord documents the invocation
// write semantics through the exported journal API: a repeated outcome update
// touches the SAME record (last write wins) and never creates a second record;
// an outcome for an unknown invocation id is a silent no-op, never a fabricated
// record. No child process is needed — these are store-level guarantees.
func TestDuplicateInvocationOutcomeIsIdempotentPerRecord(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer func() { _ = st.Close() }()
	j := execjournal.New(st, nil)

	id, err := j.RecordDispatchIntent("exec-dup", "call-dup", "filesystem.write", "write", "write")
	if err != nil {
		t.Fatalf("dispatch intent: %v", err)
	}
	if err := j.RecordOutcome(id, execjournal.InvCompleted, "completed"); err != nil {
		t.Fatalf("first outcome: %v", err)
	}
	if err := j.RecordOutcome(id, execjournal.InvFailed, "write_failed"); err != nil {
		t.Fatalf("second outcome: %v", err)
	}
	// An outcome for an id that never had an intent is a no-op.
	if err := j.RecordOutcome("inv-never-1", execjournal.InvCompleted, "completed"); err != nil {
		t.Fatalf("outcome for unknown id must be a no-op, got %v", err)
	}
	// An invalid status is rejected, not persisted.
	if err := j.RecordOutcome(id, "not_a_status", "x"); err == nil {
		t.Fatal("an invalid invocation status must be rejected")
	}

	recs := mustReadInvocationRecords(t, dir)
	if len(recs) != 1 {
		t.Fatalf("repeated outcomes must not create records: got %d (%v)", len(recs), keysOfInv(recs))
	}
	got := recs[id]
	if s, _ := got["status"].(string); s != execjournal.InvFailed {
		t.Fatalf("last outcome must win: status = %q", s)
	}
	if s, _ := got["error_class"].(string); s != "write_failed" {
		t.Fatalf("last outcome error_class = %q", s)
	}
}

// TestCorruptInvocationRecordFailsStartupClosed exercises the failure boundary
// that IS safely reproducible: a durable invocation record whose payload cannot
// be decoded must abort startup (fail closed) rather than be silently skipped.
// The whole data directory is an isolated temp dir owned by the test.
func TestCorruptInvocationRecordFailsStartupClosed(t *testing.T) {
	cases := map[string]string{
		// An unsupported payload version.
		"unsupported version": `{"version":99,"id":"inv-corrupt-1","execution_id":"exec-corrupt","status":"completed"}`,
		// A payload with no id.
		"missing id": `{"version":1,"id":"","execution_id":"exec-corrupt","status":"completed"}`,
		// A payload with an unknown status.
		"invalid status": `{"version":1,"id":"inv-corrupt-1","execution_id":"exec-corrupt","status":"teleported"}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			// A syntactically valid store envelope whose payload is the corrupt
			// bytes: this reaches journal Recover(), not the store loader.
			env := map[string]any{
				"id": "nexus.invocation:inv-corrupt-1", "type": "invocation", "status": "active",
				"version": 1, "created_at": time.Now().UTC(), "updated_at": time.Now().UTC(),
				"data": base64.StdEncoding.EncodeToString([]byte(payload)),
			}
			raw, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			invDir := filepath.Join(dir, "invocation")
			if err := os.MkdirAll(invDir, 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(invDir, "nexus.invocation:inv-corrupt-1.json"), raw, 0o600); err != nil {
				t.Fatalf("write corrupt record: %v", err)
			}

			binPath := buildNexusBinary(t)
			healthPort, gatewayPort := reservePorts(t)
			proc := startNexusProcess(t, binPath, dir, healthPort, gatewayPort)
			defer proc.stop(t)

			code := proc.waitExit(t, 30*time.Second)
			if code == 0 {
				t.Fatalf("a corrupt invocation record must abort startup (exit 0 seen)\nlogs: %s", proc.logs.String())
			}
			if !containsStr(proc.logs.String(), "recovery") {
				t.Fatalf("startup failure must be attributable to durable recovery\nlogs: %s", proc.logs.String())
			}
			// And no listener may have been left serving.
			if _, err := os.Stat(filepath.Join(dir, "invocation", "nexus.invocation:inv-corrupt-1.json")); err != nil {
				t.Fatalf("the corrupt record must be preserved for the operator: %v", err)
			}
		})
	}
}

// invRecordsEqual compares two decoded payloads by value.
func invRecordsEqual(a, b invocationRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || !jsonScalarEqual(v, w) {
			return false
		}
	}
	return true
}

func jsonScalarEqual(a, b interface{}) bool {
	aj, aerr := json.Marshal(a)
	bj, berr := json.Marshal(b)
	return aerr == nil && berr == nil && string(aj) == string(bj)
}

func keysOfInv(m map[string]invocationRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
