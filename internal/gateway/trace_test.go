package gateway

// Execution trace at the HTTP boundary
// (contracts/EXECUTION_TRACE_CONTRACTS.md §10): the read-only trace route
// over real intelligence executions — causally consistent capture,
// proposal-vs-dispatch separation, refusal evidence without dispatch,
// approval pause/resume correlation, authorization parity with the detail
// read, pagination, honest pending representation, and redaction of caller
// and approver free text.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/agentintel"
	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	memmemory "github.com/Nomssky/NEXUS/internal/memory"
)

type traceFixture struct {
	srv     *Server
	engine  *core.Engine
	orgs    *identity.Registry
	members *identity.MembershipSet
	auth    *identity.LocalAuthenticator
}

func newTraceFixture(t *testing.T) *traceFixture {
	t.Helper()
	orgs := identity.NewRegistry("nx:nexus:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(orgs)
	members := identity.NewMembershipSet()

	engine, err := core.NewEngine(nil, core.WithIdentity(members, true, true))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	mr := modelrouter.NewModelRegistry()
	router := modelrouter.NewModelRouter(mr, modelrouter.RoutingPolicyLocalFirst)
	prov := modelrouter.NewScriptedProvider(modelrouter.ProviderConfig{ID: "simulated", Status: modelrouter.ProviderStatusHealthy})
	router.RegisterProvider(prov)
	if err := mr.RegisterModel(&modelrouter.ModelDefinition{
		ID: "simulated:default", ProviderID: prov.Identify(),
		Capabilities: []modelrouter.ModelCapability{modelrouter.CapabilityReasoning, modelrouter.CapabilityStructuredOutput},
		Runtime:      modelrouter.RuntimeLocal, Status: modelrouter.ModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	tools := tool.NewToolRegistry()
	agents := agentexec.NewRegistry("nx:nexus:test", orgs, tools)
	exec := agentexec.NewRuntime(agents, tools, router, engine.EventBus())
	exec.Controller = engine.Controller()
	mem, err := agentintel.OpenMemory(nil, agentintel.Deps{
		Scopes: memmemory.NewMembershipScopes(members.AllowsScope)})
	if err != nil {
		t.Fatal(err)
	}
	intel := agentintel.New(agents, exec, &agentintel.Decision{Router: router}, engine.EventBus(), mem)

	now := time.Now().UTC()
	orgs.CreateIdentity(identity.Identity{ID: "owner", Type: identity.TypeHuman, DisplayName: "o",
		Provenance: schema.ProvenanceRef{Producer: "test", ProducedAt: now}}, "t")
	orgs.CreateBusiness(identity.Business{EntityID: "default", Name: "d", OwnerIdentityID: "owner", CreatedAt: now}, "t")
	members.Add(identity.Membership{IdentityID: "owner", BusinessID: "default", Role: identity.RoleAdmin, Status: identity.StatusActive})
	if err := auth.Register("owner", security.HashCredential([]byte("s3cret")), identity.AuthMethodToken); err != nil {
		t.Fatal(err)
	}
	if _, err := agents.Create(agentexec.Definition{
		ID: "analyst", Name: "Analyst", BusinessID: "default",
		Capabilities: []string{"analysis"}, AllowedTools: []string{"calculator", "echo"},
	}, "t"); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(engine, ":0",
		WithRegistry(orgs), WithIdentity(auth, members),
		WithRequireAuthentication(true), WithEnforceBusinessScope(true),
		WithAgentExecution(agents, exec), WithIntelligence(intel))
	return &traceFixture{srv: srv, engine: engine, orgs: orgs, members: members, auth: auth}
}

func (f *traceFixture) authHeaders() map[string]string {
	return map[string]string{"X-Actor-ID": "owner", "X-Actor-Credential": "s3cret"}
}

func (f *traceFixture) get(path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range f.authHeaders() {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	return w
}

func (f *traceFixture) getWith(path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	return w
}

func (f *traceFixture) submitObjective(t *testing.T, description string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"business_id": "default", "actor_id": "owner",
		"objective": map[string]string{"description": description},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/execute", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range f.authHeaders() {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	var acc map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &acc)
	return acc["execution_id"]
}

// pollUntil polls the detail read until pred sees a terminal episode.
func (f *traceFixture) pollUntil(t *testing.T, id string, pred func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		w := f.get("/api/v1/intelligence/" + id + "?business_id=default")
		if w.Code == http.StatusOK {
			var out map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			if pred(out) {
				return out
			}
		}
		if w.Code != http.StatusAccepted && w.Code != http.StatusOK && w.Code != http.StatusNotFound {
			t.Fatalf("poll %d: %s", w.Code, w.Body.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("execution %s never reached the expected state", id)
	return nil
}

func (f *traceFixture) terminalPoll(id string) map[string]interface{} {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		w := f.get("/api/v1/intelligence/" + id + "?business_id=default")
		if w.Code == http.StatusOK {
			var out map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			return out
		}
		time.Sleep(5 * time.Millisecond)
	}
	return map[string]interface{}{"status": "timeout"}
}

type traceEntryJSON struct {
	Seq             uint64            `json:"seq"`
	EventID         string            `json:"event_id"`
	Type            string            `json:"type"`
	Source          string            `json:"source"`
	Data            map[string]string `json:"data"`
	RedactedKeys    []string          `json:"redacted_keys"`
	DataUnparseable bool              `json:"data_unparseable"`
}

type traceResponseJSON struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
	Durability  string `json:"durability"`
	Capture     struct {
		Available    bool   `json:"available"`
		Entries      int    `json:"entries"`
		Dropped      int    `json:"dropped"`
		Limit        int    `json:"limit"`
		Truncated    bool   `json:"truncated"`
		FirstSeq     uint64 `json:"first_seq"`
		LastSeq      uint64 `json:"last_seq"`
		TerminalSeq  uint64 `json:"terminal_seq"`
		PostTerminal int    `json:"post_terminal"`
		HasMore      bool   `json:"has_more"`
	} `json:"capture"`
	Entries []traceEntryJSON `json:"entries"`
}

func (f *traceFixture) trace(t *testing.T, id string, query string) (*traceResponseJSON, *httptest.ResponseRecorder) {
	t.Helper()
	w := f.get("/api/v1/intelligence/" + id + "/trace?business_id=default" + query)
	if w.Code != http.StatusOK {
		return nil, w
	}
	var out traceResponseJSON
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("trace body: %v: %s", err, w.Body.String())
	}
	return &out, w
}

func entryTypes(tr *traceResponseJSON) []string {
	out := make([]string, 0, len(tr.Entries))
	for _, e := range tr.Entries {
		out = append(out, e.Type)
	}
	return out
}

// okToolObservation returns the index of an observation proving the named
// tool actually ran (status ok), or -1. In the pre-platform fixture path
// this is the only post-hoc dispatch evidence (contract §6).
func okToolObservation(tr *traceResponseJSON, tool string) int {
	for i, e := range tr.Entries {
		if e.Type == "observation.created" && e.Data["source"] == "tool:"+tool && e.Data["status"] == "ok" {
			return i
		}
	}
	return -1
}

func countType(tr *traceResponseJSON, typ string) int {
	n := 0
	for _, e := range tr.Entries {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func entryIndex(tr *traceResponseJSON, typ string, dataKey, dataValue string) int {
	for i, e := range tr.Entries {
		if e.Type != typ {
			continue
		}
		if dataKey == "" || e.Data[dataKey] == dataValue {
			return i
		}
	}
	return -1
}

func (f *traceFixture) withPolicies(t *testing.T, policies ...*governance.Policy) {
	t.Helper()
	seeded := f.engine.Governance().Policies()
	all := append([]*governance.Policy{}, seeded...)
	all = append(all, policies...)
	f.engine.Governance().SetPolicies(all)
}

func toolCallPolicy(id string, effect governance.Outcome) *governance.Policy {
	return &governance.Policy{
		PolicyID: id, Name: id, Status: governance.PolicyStatusActive,
		Effect:     effect,
		Subject:    governance.Subject{SubjectType: "all"},
		Action:     governance.Action{ActionType: "custom", ActionIDs: []string{"tool_call"}},
		Resource:   governance.Resource{ResourceType: "tool"},
		Precedence: 1000,
	}
}

// A normal execution produces a causally consistent trace: proposal before
// dispatch, decision evidence, terminal marker, authoritative status.
func TestIntelligenceTraceNormalLifecycle(t *testing.T) {
	f := newTraceFixture(t)
	id := f.submitObjective(t, "multiply six by seven with the calculator")
	if res := f.terminalPoll(id); res["status"] != "completed" {
		t.Fatalf("expected completed, got %v (%v)", res["status"], res["error"])
	}

	tr, w := f.trace(t, id, "")
	if tr == nil {
		t.Fatalf("trace: %d %s", w.Code, w.Body.String())
	}
	if tr.ExecutionID != id || tr.Status != "completed" || tr.Durability != "process_local" {
		t.Fatalf("trace identity/status: %+v", tr)
	}
	c := tr.Capture
	if !c.Available || c.Entries != len(tr.Entries) || c.FirstSeq != 1 || c.LastSeq != uint64(len(tr.Entries)) {
		t.Fatalf("capture stats: %+v", c)
	}
	for i, e := range tr.Entries {
		if e.Seq != uint64(i+1) {
			t.Fatalf("capture order broken at %d: seq %d", i, e.Seq)
		}
	}
	// Proposal vs attempt vs execution evidence vs terminal, in causal order.
	// tool.requested is attempt evidence (contract §6); the ok observation is
	// the post-hoc proof the builtin executor actually ran.
	proposal := entryIndex(tr, "action.proposed", "action", "tool_call")
	decision := entryIndex(tr, "governance.decided", "action", "tool_call")
	attempt := entryIndex(tr, "tool.requested", "tool_id", "calculator")
	observed := okToolObservation(tr, "calculator")
	terminal := entryIndex(tr, "objective.completed", "state", "completed")
	if proposal < 0 || decision < 0 || attempt < 0 || observed < 0 || terminal < 0 {
		t.Fatalf("missing evidence classes: %v", entryTypes(tr))
	}
	if !(proposal < decision && decision < attempt && attempt < observed && observed < terminal) {
		t.Fatalf("causal order broken: proposal=%d decision=%d attempt=%d observed=%d terminal=%d",
			proposal, decision, attempt, observed, terminal)
	}
	if c.TerminalSeq == 0 {
		t.Fatal("terminal marker must be located")
	}
	// chain.completed is a legitimate post-marker fact, never a rewrite.
	if tr.Entries[len(tr.Entries)-1].Type != "chain.completed" {
		t.Fatalf("expected final chain.completed, got %s", tr.Entries[len(tr.Entries)-1].Type)
	}
	// Capture-time redaction of caller free text (contract §9).
	if idx := entryIndex(tr, "chain.started", "", ""); idx >= 0 {
		if !containsString(tr.Entries[idx].RedactedKeys, "outcome") {
			t.Fatalf("chain.started outcome (request intent) must be redacted: %+v", tr.Entries[idx])
		}
	} else {
		t.Fatal("chain.started missing")
	}
	objStarted := entryIndex(tr, "objective.started", "", "")
	if objStarted < 0 || !containsString(tr.Entries[objStarted].RedactedKeys, "objective") {
		t.Fatalf("objective.started must redact the objective text: %+v", objStarted)
	}
}

// A rejected structured response is recorded as a rejection and never as a
// dispatch (mission tests 2 and 3).
func TestIntelligenceTraceRejectedProposalIsNotDispatched(t *testing.T) {
	f := newTraceFixture(t)
	id := f.submitObjective(t, "malformed objective that breaks the protocol")
	if res := f.terminalPoll(id); res["status"] != "failed" {
		t.Fatalf("expected failed, got %v", res["status"])
	}
	tr, _ := f.trace(t, id, "")
	if entryIndex(tr, "action.rejected", "", "") < 0 {
		t.Fatalf("rejection must be recorded: %v", entryTypes(tr))
	}
	for _, typ := range []string{"tool.requested", "tool.invocation.started", "tool.completed"} {
		if entryIndex(tr, typ, "", "") >= 0 {
			t.Fatalf("a rejected proposal must never appear as dispatch evidence (%s): %v", typ, entryTypes(tr))
		}
	}
	if entryIndex(tr, "objective.completed", "state", "failed") < 0 {
		t.Fatalf("terminal failure must be recorded: %v", entryTypes(tr))
	}
}

// A governance denial records the refusal and never invokes the tool
// (mission test 4).
func TestIntelligenceTraceGovernanceDenialNeverDispatches(t *testing.T) {
	f := newTraceFixture(t)
	denied := toolCallPolicy("deny-tool-calls", governance.DENY)
	f.withPolicies(t, denied)

	id := f.submitObjective(t, "echo the greeting to everyone")
	if res := f.terminalPoll(id); res["status"] != "failed" {
		t.Fatalf("expected failed under DENY, got %v", res["status"])
	}
	tr, _ := f.trace(t, id, "")
	decision := entryIndex(tr, "governance.decided", "decision", governance.DENY.String())
	if decision < 0 {
		t.Fatalf("denial must be evidenced: %v", entryTypes(tr))
	}
	if idx := okToolObservation(tr, "echo"); idx >= 0 {
		t.Fatalf("a denied call must leave no execution evidence: %+v", tr.Entries[idx])
	}
	if entryIndex(tr, "objective.completed", "state", string(agentintel.StateDenied)) < 0 {
		t.Fatalf("denied terminal must be recorded: %v", entryTypes(tr))
	}
}

// Approval pause and resume correlate to the same authorized action in one
// trace (mission tests 5, 6, 7; contract §7), and approver prose is
// redacted at capture.
func TestIntelligenceTraceApprovalPauseResumeCorrelates(t *testing.T) {
	f := newTraceFixture(t)
	policy := toolCallPolicy("approve-tool-calls", governance.REQUIRE_APPROVAL)
	policy.ApprovalConfig = &governance.ApprovalConfig{TimeoutSeconds: 3600, AutoDenyOnTimeout: true}
	f.withPolicies(t, policy)

	id := f.submitObjective(t, "echo the paused greeting")
	// First episode: the execution waits for approval.
	res := f.pollUntil(t, id, func(r map[string]interface{}) bool { return r["status"] == "failed" })
	if res["status"] != "failed" {
		t.Fatalf("expected approval-wait failure, got %v", res["status"])
	}
	tr, _ := f.trace(t, id, "")
	if entryIndex(tr, "approval.requested", "", "") < 0 {
		t.Fatalf("approval request must be recorded: %v", entryTypes(tr))
	}
	if entryIndex(tr, "objective.completed", "state", string(agentintel.StatePendingApproval)) < 0 {
		t.Fatalf("the wait state must be recorded: %v", entryTypes(tr))
	}
	if idx := okToolObservation(tr, "echo"); idx >= 0 {
		t.Fatalf("nothing may execute before approval: %+v", tr.Entries[idx])
	}

	// Decide the pending approval; the rationale carries a sentinel that must
	// never reach the trace.
	list := f.get("/api/v1/approvals?business_id=default")
	var listResp struct {
		Approvals []map[string]interface{} `json:"approvals"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listResp); err != nil || len(listResp.Approvals) != 1 {
		t.Fatalf("pending approvals: %d %s", len(listResp.Approvals), list.Body.String())
	}
	approvalID, _ := listResp.Approvals[0]["entity_id"].(string)
	if approvalID == "" {
		t.Fatalf("approval record lacks an id: %v", listResp.Approvals[0])
	}
	body, _ := json.Marshal(map[string]string{"reason": "SENTINEL-APPROVER-REASON"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/approvals/"+approvalID+"/approve?business_id=default", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range f.authHeaders() {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}

	// Second episode: the same execution resumes and completes.
	f.pollUntil(t, id, func(r map[string]interface{}) bool { return r["status"] == "completed" })
	tr, _ = f.trace(t, id, "")

	requested := entryIndex(tr, "approval.requested", "", "")
	approved := entryIndex(tr, "approval.approved", "approval_id", approvalID)
	waited := entryIndex(tr, "objective.completed", "state", string(agentintel.StatePendingApproval))
	observed := okToolObservation(tr, "echo")
	done := entryIndex(tr, "objective.completed", "state", string(agentintel.StateCompleted))
	if requested < 0 || approved < 0 || waited < 0 || observed < 0 || done < 0 {
		t.Fatalf("pause/resume evidence incomplete: %v", entryTypes(tr))
	}
	if !(requested < waited && waited < approved && approved < observed && observed < done) {
		t.Fatalf("resume order broken: requested=%d waited=%d approved=%d observed=%d done=%d",
			requested, waited, approved, observed, done)
	}
	// One approval, one execution: the resume never replays the effect
	// (mission test 7). Two attempts are visible; one execution happened.
	if n := countType(tr, "approval.approved"); n != 1 {
		t.Fatalf("expected exactly one approval decision, got %d", n)
	}
	if n := countType(tr, "tool.requested"); n != 2 {
		t.Fatalf("expected two invocation attempts (refused then resumed), got %d", n)
	}
	// The first episode end (the paused episode's chain.failed) is the first
	// terminal marker; the resumed episode's completion is a visible later
	// fact that never rewrites it (contract §8).
	markerIdx := -1
	for i, e := range tr.Entries {
		if e.Seq == tr.Capture.TerminalSeq {
			markerIdx = i
			break
		}
	}
	if markerIdx < 0 {
		t.Fatalf("terminal_seq %d must reference a captured entry", tr.Capture.TerminalSeq)
	}
	marker := tr.Entries[markerIdx]
	if marker.Type != "chain.failed" &&
		!(marker.Type == "objective.completed" && marker.Data["state"] != string(agentintel.StatePendingApproval)) {
		t.Fatalf("terminal marker must be an episode end, got %+v", marker)
	}
	if done <= markerIdx || tr.Capture.PostTerminal != len(tr.Entries)-markerIdx-1 {
		t.Fatalf("resume episode must be counted after the marker: done=%d marker=%d post=%d",
			done, markerIdx, tr.Capture.PostTerminal)
	}
	// Approver prose is redacted at capture; identifiers survive.
	if !containsString(tr.Entries[approved].RedactedKeys, "decision_rationale") {
		t.Fatalf("decision_rationale must be redacted: %+v", tr.Entries[approved])
	}
	blob, _ := json.Marshal(tr)
	if strings.Contains(string(blob), "SENTINEL-APPROVER-REASON") {
		t.Fatal("approver prose leaked into the trace")
	}
}

// Trace access follows the existing visibility model exactly
// (mission tests 15, 16) and never mutates runtime state (mission test 23).
func TestIntelligenceTraceAuthorizationParity(t *testing.T) {
	f := newTraceFixture(t)
	id := f.submitObjective(t, "multiply six by seven with the calculator")
	f.terminalPoll(id)

	// Unknown execution: 404, indistinguishable from foreign.
	if w := f.get("/api/v1/intelligence/does-not-exist/trace?business_id=default"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", w.Code)
	}
	// Missing scope: 400 fail-closed.
	if w := f.get("/api/v1/intelligence/" + id + "/trace"); w.Code != http.StatusBadRequest {
		t.Fatalf("missing business_id: %d", w.Code)
	}
	// No or wrong credential: 401.
	if w := f.getWith("/api/v1/intelligence/"+id+"/trace?business_id=default",
		map[string]string{"X-Actor-ID": "owner"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing credential: %d", w.Code)
	}
	if w := f.getWith("/api/v1/intelligence/"+id+"/trace?business_id=default",
		map[string]string{"X-Actor-ID": "owner", "X-Actor-Credential": "wrong"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong credential: %d", w.Code)
	}

	// A member of another business gets 404 — no cross-business traces.
	now := time.Now().UTC()
	f.orgs.CreateIdentity(identity.Identity{ID: "peer", Type: identity.TypeHuman, DisplayName: "p",
		Provenance: schema.ProvenanceRef{Producer: "test", ProducedAt: now}}, "t")
	f.orgs.CreateBusiness(identity.Business{EntityID: "foreign", Name: "f", OwnerIdentityID: "peer", CreatedAt: now}, "t")
	f.members.Add(identity.Membership{IdentityID: "peer", BusinessID: "foreign", Role: identity.RoleAdmin, Status: identity.StatusActive})
	if err := f.auth.Register("peer", security.HashCredential([]byte("peer-s3cret")), identity.AuthMethodToken); err != nil {
		t.Fatal(err)
	}
	if w := f.getWith("/api/v1/intelligence/"+id+"/trace?business_id=foreign",
		map[string]string{"X-Actor-ID": "peer", "X-Actor-Credential": "peer-s3cret"}); w.Code != http.StatusNotFound {
		t.Fatalf("cross-business trace: %d", w.Code)
	}

	// Invalid filters are rejected strictly.
	for _, q := range []string{"&limit=0", "&limit=abc", "&limit=999999", "&after_seq=-1", "&after_seq=abc"} {
		if w := f.get("/api/v1/intelligence/" + id + "/trace?business_id=default" + q); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid filter %q: %d", q, w.Code)
		}
	}

	// Retrieval is read-only: identical reads, intact result, no side effects.
	before, _ := f.trace(t, id, "")
	after, w := f.trace(t, id, "")
	if w.Code != http.StatusOK || !bytes.Equal(mustJSON(t, before), mustJSON(t, after)) {
		t.Fatal("trace reads must be deterministic")
	}
	res := f.terminalPoll(id)
	if res["status"] != "completed" {
		t.Fatalf("trace retrieval altered the execution: %v", res["status"])
	}
	if list := f.get("/api/v1/approvals?business_id=default"); !strings.Contains(list.Body.String(), "[]") {
		t.Fatalf("trace retrieval must not create approvals: %s", list.Body.String())
	}
}

// Capture-order pagination is stable and complete (mission test 17).
func TestIntelligenceTracePagination(t *testing.T) {
	f := newTraceFixture(t)
	id := f.submitObjective(t, "multiply six by seven with the calculator")
	f.terminalPoll(id)

	full, _ := f.trace(t, id, "")
	if len(full.Entries) < 4 {
		t.Fatalf("expected a multi-entry trace, got %d", len(full.Entries))
	}
	var walked []uint64
	after := uint64(0)
	for {
		page, _ := f.trace(t, id, fmt.Sprintf("&limit=3&after_seq=%d", after))
		if len(page.Entries) == 0 {
			t.Fatal("pagination returned an empty page before exhaustion")
		}
		for _, e := range page.Entries {
			walked = append(walked, e.Seq)
		}
		after = page.Entries[len(page.Entries)-1].Seq
		if !page.Capture.HasMore {
			break
		}
	}
	if len(walked) != len(full.Entries) {
		t.Fatalf("paged walk %d != full trace %d", len(walked), len(full.Entries))
	}
	for i, seq := range walked {
		if seq != uint64(i+1) {
			t.Fatalf("paged walk out of order at %d: %d", i, seq)
		}
	}
}

// A pending execution returns its partial trace honestly (mission test 18).
func TestIntelligenceTracePendingRepresentation(t *testing.T) {
	f := newTraceFixture(t)
	id := f.submitObjective(t, "infinite loop that never completes this objective")
	// The trace read answers 200 for a pending execution with partial data.
	var tr *traceResponseJSON
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var w *httptest.ResponseRecorder
		tr, w = f.trace(t, id, "")
		if w.Code != http.StatusOK {
			t.Fatalf("trace of a pending execution: %d %s", w.Code, w.Body.String())
		}
		if tr.Status == "pending" || tr.Status == "completed" || tr.Status == "failed" {
			break
		}
	}
	if tr.Status == "" {
		t.Fatal("trace must carry an authoritative status")
	}
	// Whatever the window, capture stats must describe it honestly.
	if tr.Capture.Entries != len(tr.Entries) || (tr.Capture.HasMore && len(tr.Entries) > core.MaxTraceEntries) {
		t.Fatalf("inconsistent capture: %+v entries=%d", tr.Capture, len(tr.Entries))
	}
}

// A delegated child's events correlate into the parent's trace with
// runtime-stamped lineage (mission test 12; contract §7).
func TestIntelligenceTraceDelegationCorrelates(t *testing.T) {
	f := newTraceFixture(t)
	id := f.submitObjective(t, "delegate the supporting research")
	if res := f.terminalPoll(id); res["status"] != "completed" {
		t.Fatalf("expected completed delegation, got %v (%v)", res["status"], res["error"])
	}
	tr, _ := f.trace(t, id, "")
	if entryIndex(tr, "agent.delegated", "", "") < 0 {
		t.Fatalf("delegation must be recorded: %v", entryTypes(tr))
	}
	lineage := -1
	for i, e := range tr.Entries {
		if e.Data["delegation_depth"] == "1" && e.Data["parent_agent_id"] != "" {
			lineage = i
			break
		}
	}
	if lineage < 0 {
		t.Fatalf("child lineage must appear in the parent trace: %v", entryTypes(tr))
	}
	if tr.Entries[lineage].Data["parent_task_id"] == "" {
		t.Fatalf("runtime-stamped parent linkage missing: %+v", tr.Entries[lineage])
	}
}

// containsString reports whether list holds s.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// mustJSON renders v for equality comparisons.
func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A successful builtin call on the pre-platform path now emits dispatch
// evidence without fabricating physical attempts (v8). The invocation
// started/terminal pair is the honest signal; source agentexec and
// side_effect_class read distinguish it from the capability path.
func TestTracePrePlatformBuiltinEmitsDispatchEvidenceNoAttempts(t *testing.T) {
	f := newTraceFixture(t)
	id := f.submitObjective(t, "multiply six by seven with the calculator")
	if res := f.terminalPoll(id); res["status"] != "completed" {
		t.Fatalf("expected completed, got %v (%v)", res["status"], res["error"])
	}

	tr, w := f.trace(t, id, "")
	if tr == nil {
		t.Fatalf("trace: %d %s", w.Code, w.Body.String())
	}
	proposal := entryIndex(tr, "action.proposed", "action", "tool_call")
	decision := entryIndex(tr, "governance.decided", "action", "tool_call")
	started := entryIndex(tr, "tool.invocation.started", "tool_id", "calculator")
	completed := entryIndex(tr, "tool.invocation.completed", "tool_id", "calculator")
	terminal := entryIndex(tr, "objective.completed", "state", "completed")
	if proposal < 0 || decision < 0 || started < 0 || completed < 0 || terminal < 0 {
		t.Fatalf("pre-platform dispatch evidence missing: %v", entryTypes(tr))
	}
	if !(proposal < decision && decision < started && started < completed && completed < terminal) {
		t.Fatalf("causal order broken: proposal=%d decision=%d started=%d completed=%d terminal=%d",
			proposal, decision, started, completed, terminal)
	}
	entry := tr.Entries[started]
	if entry.Source != "agentexec" {
		t.Fatalf("expected agentexec source for pre-platform dispatch, got %q", entry.Source)
	}
	if entry.Data["side_effect_class"] != "read" {
		t.Fatalf("builtin side_effect_class should be read, got %q", entry.Data["side_effect_class"])
	}
	// No fabricated physical attempts: the pure local invocation is ONE
	// call, evidenced by started + completed with zero attempt events.
	for _, typ := range []string{"tool.attempt.started", "tool.attempt.completed", "tool.retry.scheduled"} {
		if entryIndex(tr, typ, "", "") >= 0 {
			t.Fatalf("pre-platform path must not fabricate %s: %v", typ, entryTypes(tr))
		}
	}
	// The ok observation remains independent (post-hoc) confirmation.
	if okToolObservation(tr, "calculator") < 0 {
		t.Fatalf("ok observation missing: %v", entryTypes(tr))
	}
}
