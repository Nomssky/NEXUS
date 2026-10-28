package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

type recoveryFixture struct {
	srv      *Server
	engine   *core.Engine
	storeDir string      // so tests can reopen the real store (restart durability)
	store    store.Store // the SAME store the engine's journal reads through
}

// closeStore releases exclusive ownership before a test reopens the directory
// as a second process would.
func closeStore(t *testing.T, st store.Store) {
	t.Helper()
	closer, ok := st.(interface{ Close() error })
	if !ok {
		return
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
}

const (
	recBiz      = "biz-1"
	recOtherBiz = "biz-other"
	recDiv      = "div-a"

	recWideID  = "rec-wide"
	recDivID   = "rec-div"
	recOtherID = "rec-other"
	recGoneID  = "rec-missing"

	recoveryWideCred  = "rec-wide-secret"
	recoveryDivCred   = "rec-div-secret"
	recoveryOtherCred = "rec-other-secret"
)

func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	reg := identity.NewRegistry("nx:nexus:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)
	members := identity.NewMembershipSet()

	storeDir := t.TempDir()
	st, err := store.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)

	engine, err := core.NewEngine(nil, core.WithIdentity(members, true, true), core.WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	now := time.Now().UTC()
	must := func(err error, what string) {
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	_, err = reg.CreateIdentity(identity.Identity{ID: "nx:human:owner", Type: identity.TypeHuman, DisplayName: "owner"}, "t")
	must(err, "owner identity")
	_, err = reg.CreateBusiness(identity.Business{EntityID: recBiz, BusinessID: recBiz, Name: recBiz, OwnerIdentityID: "nx:human:owner", Status: identity.BusinessActive, CreatedAt: now}, "t")
	must(err, "business")
	_, err = reg.CreateDivision(identity.Division{EntityID: recDiv, BusinessID: recBiz, ParentBusinessID: recBiz, Name: recDiv, OwnerIdentityID: "nx:human:owner", Status: identity.DivisionActive}, "t")
	must(err, "division")

	_, err = reg.CreateIdentity(identity.Identity{ID: "user-wide", Type: identity.TypeHuman, DisplayName: "wide", BusinessID: recBiz}, "t")
	must(err, "wide id")
	_, err = reg.CreateIdentity(identity.Identity{ID: "user-div", Type: identity.TypeHuman, DisplayName: "div", BusinessID: recBiz, DivisionID: recDiv}, "t")
	must(err, "div id")
	must(members.Add(identity.Membership{IdentityID: "user-wide", BusinessID: recBiz, Role: identity.RoleMember, Status: identity.StatusActive}), "wide membership")
	must(members.Add(identity.Membership{IdentityID: "user-div", BusinessID: recBiz, DivisionID: recDiv, Role: identity.RoleMember, Status: identity.StatusActive}), "div membership")
	must(auth.Register("user-wide", security.HashCredential([]byte(recoveryWideCred)), identity.AuthMethodToken), "wide cred")
	must(auth.Register("user-div", security.HashCredential([]byte(recoveryDivCred)), identity.AuthMethodToken), "div cred")

	// Seed durable records.
	must(j.Accepted(recWideID, recBiz, "", "user-wide"), "accept wide")
	must(j.Terminal(recWideID, execjournal.StateCompleted, ""), "terminal wide")
	must(j.Accepted(recDivID, recBiz, recDiv, "user-div"), "accept div")
	must(j.Accepted(recOtherID, recOtherBiz, "", "other-actor"), "accept other")
	must(j.Terminal(recOtherID, execjournal.StateFailed, "POLICY_DENIED"), "terminal other")
	// Recovery classifies the still-nonterminal division record as unknown.
	if _, err := j.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}

	srv := NewServer(engine, ":0",
		WithRegistry(reg), WithIdentity(auth, members),
		WithRequireAuthentication(true), WithEnforceBusinessScope(true))
	return &recoveryFixture{srv: srv, engine: engine, storeDir: storeDir, store: st}
}

func (f *recoveryFixture) get(t *testing.T, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	return w
}

func wideHeaders() map[string]string {
	return map[string]string{"X-Actor-ID": "user-wide", "X-Actor-Credential": recoveryWideCred}
}
func divHeaders() map[string]string {
	return map[string]string{"X-Actor-ID": "user-div", "X-Actor-Credential": recoveryDivCred}
}

// An authorized business-wide caller inspects a durable record after restart.
func TestRecoveryDetailAuthorized(t *testing.T) {
	f := newRecoveryFixture(t)
	w := f.get(t, "/api/v1/recovery/executions/"+recWideID+"?business_id="+recBiz, wideHeaders())
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var v map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v["source"] != "durable_journal" {
		t.Fatalf("source discriminator missing: %v", v["source"])
	}
	if v["execution_id"] != recWideID || v["state"] != "completed" || v["business_id"] != recBiz {
		t.Fatalf("projection wrong: %v", v)
	}
	// Internal/sensitive fields must be absent.
	for _, bad := range []string{"actor_id", "reason", "data"} {
		if _, ok := v[bad]; ok {
			t.Fatalf("response leaked internal field %q: %v", bad, v)
		}
	}
	// No raw bytes / secrets anywhere in the body.
	if strings.Contains(w.Body.String(), recoveryWideCred) || strings.Contains(w.Body.String(), "hash") {
		t.Fatalf("body looks sensitive: %s", w.Body.String())
	}
}

// A recovered record is exposed as unknown with recovery metadata; never
// collapsed into failed or completed.
func TestRecoveryDetailUnknownPreserved(t *testing.T) {
	f := newRecoveryFixture(t)
	w := f.get(t, "/api/v1/recovery/executions/"+recDivID+"?business_id="+recBiz, wideHeaders())
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var v map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if v["state"] != "unknown" {
		t.Fatalf("state = %v", v["state"])
	}
	rec, _ := v["recovery"].(map[string]interface{})
	if rec == nil || rec["recovered"] != true || rec["prior_state"] != "accepted" {
		t.Fatalf("recovery metadata wrong: %v", v["recovery"])
	}
}

// Cross-business, unauthorized-division and missing ids are all 404 (G5).
func TestRecoveryDetailVisibility(t *testing.T) {
	f := newRecoveryFixture(t)
	// Cross-business: user-wide is not a member of recOtherBiz -> 403 (not a
	// member of the requested business). The record is not revealed either way.
	if w := f.get(t, "/api/v1/recovery/executions/"+recOtherID+"?business_id="+recOtherBiz, wideHeaders()); w.Code != 403 {
		t.Fatalf("cross-business read must be 403 (not a member), got %d", w.Code)
	}
	// A divisionless record read by a division-scoped caller -> 404 (division
	// membership never covers business-level scope).
	if w := f.get(t, "/api/v1/recovery/executions/"+recWideID+"?business_id="+recBiz, divHeaders()); w.Code != 404 {
		t.Fatalf("division caller reading a divisionless record must be 404, got %d", w.Code)
	}
	// A division record read by its own division caller -> 200.
	if w := f.get(t, "/api/v1/recovery/executions/"+recDivID+"?business_id="+recBiz, divHeaders()); w.Code != 200 {
		t.Fatalf("division caller reading its own record must be 200, got %d", w.Code)
	}
	// Missing id -> 404.
	if w := f.get(t, "/api/v1/recovery/executions/"+recGoneID+"?business_id="+recBiz, wideHeaders()); w.Code != 404 {
		t.Fatalf("missing record must be 404, got %d", w.Code)
	}
}

// Input validation and auth boundaries.
func TestRecoveryDetailValidationAndAuth(t *testing.T) {
	f := newRecoveryFixture(t)
	if w := f.get(t, "/api/v1/recovery/executions/"+recWideID, wideHeaders()); w.Code != 400 {
		t.Fatalf("missing business_id must be 400, got %d", w.Code)
	}
	if w := f.get(t, "/api/v1/recovery/executions/"+recWideID+"?business_id="+recBiz, nil); w.Code != 401 {
		t.Fatalf("no credentials must be 401, got %d", w.Code)
	}
	// Wrong credential -> 401.
	bad := map[string]string{"X-Actor-ID": "user-wide", "X-Actor-Credential": "nope"}
	if w := f.get(t, "/api/v1/recovery/executions/"+recWideID+"?business_id="+recBiz, bad); w.Code != 401 {
		t.Fatalf("bad credential must be 401, got %d", w.Code)
	}
}

// The list is bounded, newest-first, scope-filtered, and validated.
func TestRecoveryList(t *testing.T) {
	f := newRecoveryFixture(t)
	w := f.get(t, "/api/v1/recovery/executions?business_id="+recBiz, wideHeaders())
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Source     string                   `json:"source"`
		Executions []map[string]interface{} `json:"executions"`
		Count      int                      `json:"count"`
		Limit      int                      `json:"limit"`
		HasMore    bool                     `json:"has_more"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Source != "durable_journal" || body.Count != 2 || len(body.Executions) != 2 {
		t.Fatalf("list wrong: %+v", body)
	}
	// Business scoping: the other business's record is never present.
	for _, e := range body.Executions {
		if e["business_id"] != recBiz {
			t.Fatalf("cross-business leak: %v", e)
		}
	}

	// limit=1 -> one row + has_more.
	w = f.get(t, "/api/v1/recovery/executions?business_id="+recBiz+"&limit=1", wideHeaders())
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Count != 1 || len(body.Executions) != 1 {
		t.Fatalf("limit=1 wrong: %+v", body)
	}
	// For a division-scoped caller the divisionless record is filtered out.
	w = f.get(t, "/api/v1/recovery/executions?business_id="+recBiz, divHeaders())
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Count != 1 || body.Executions[0]["execution_id"] != recDivID {
		t.Fatalf("division list wrong: %+v", body)
	}
}

func TestRecoveryListBounds(t *testing.T) {
	f := newRecoveryFixture(t)
	for _, q := range []string{"&limit=0", "&limit=201", "&limit=abc", "&offset=-1", "&offset=abc"} {
		w := f.get(t, "/api/v1/recovery/executions?business_id="+recBiz+q, wideHeaders())
		if w.Code != 400 {
			t.Fatalf("query %q must be 400, got %d", q, w.Code)
		}
	}
}

// With no durable store the read surface is unavailable (503), never a
// fabricated record.
func TestRecoveryUnavailableWithoutJournal(t *testing.T) {
	// Mirror the trace fixture: identity enforced, no durable journal.
	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })
	srv := NewServer(engine, ":0")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/recovery/executions/x?business_id=b", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without a journal, got %d", w.Code)
	}
}

// The read API never mutates execution state or adds records.
func TestRecoveryReadIsReadOnly(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()
	before, err := j.ListByBusiness(recBiz, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	stateBefore, _, _ := j.Get(recWideID)

	_ = f.get(t, "/api/v1/recovery/executions/"+recWideID+"?business_id="+recBiz, wideHeaders())
	_ = f.get(t, "/api/v1/recovery/executions?business_id="+recBiz, wideHeaders())

	after, err := j.ListByBusiness(recBiz, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("read created/removed records: before=%d after=%d", len(before), len(after))
	}
	stateAfter, _, _ := j.Get(recWideID)
	if stateAfter.State != stateBefore.State {
		t.Fatalf("read mutated state: %q -> %q", stateBefore.State, stateAfter.State)
	}
}

// ---- v52: research evidence is a durable read, not a process-local view ---

// newEvidenceServer builds a server whose journal reads the GIVEN directory.
// It exists so a test can literally reopen the backend over the same store, the
// way a restart does.
func newEvidenceServer(t *testing.T, dir string) (*Server, *core.Engine, store.Store) {
	t.Helper()
	reg := identity.NewRegistry("nx:nexus:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)
	members := identity.NewMembershipSet()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	engine, err := core.NewEngine(nil, core.WithIdentity(members, true, true), core.WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	now := time.Now().UTC()
	must := func(err error, what string) {
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	_, err = reg.CreateIdentity(identity.Identity{ID: "nx:human:owner", Type: identity.TypeHuman, DisplayName: "owner"}, "t")
	must(err, "owner identity")
	_, err = reg.CreateBusiness(identity.Business{EntityID: recBiz, BusinessID: recBiz, Name: recBiz, OwnerIdentityID: "nx:human:owner", Status: identity.BusinessActive, CreatedAt: now}, "t")
	must(err, "business")
	_, err = reg.CreateIdentity(identity.Identity{ID: "user-wide", Type: identity.TypeHuman, DisplayName: "wide", BusinessID: recBiz}, "t")
	must(err, "wide id")
	must(members.Add(identity.Membership{IdentityID: "user-wide", BusinessID: recBiz, Role: identity.RoleMember, Status: identity.StatusActive}), "wide membership")
	must(auth.Register("user-wide", security.HashCredential([]byte(recoveryWideCred)), identity.AuthMethodToken), "wide cred")
	s := NewServer(engine, ":0",
		WithRegistry(reg), WithIdentity(auth, members),
		WithRequireAuthentication(true), WithEnforceBusinessScope(true))
	return s, engine, st
}

func evidenceGet(t *testing.T, srv *Server, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func recoverySampleEvidence() *execjournal.ResearchEvidence {
	return &execjournal.ResearchEvidence{
		Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
		Operation: execjournal.ResearchEvidenceOperation, Count: 2,
		Sources: []execjournal.ResearchSource{
			{ID: "src-1", Title: "Nexus capability boundary", URL: "https://docs.example/nexus", Source: "docs.example", Snippet: "The runtime decides; the model proposes."},
			{ID: "src-2", Title: "External content is data", URL: "https://example.org/safety", Source: "example.org", Snippet: "Inert | data"},
		},
		Plan: &execjournal.ResearchPlanEvidence{
			PlanID: "exec-evi", Version: 1, ObjectiveID: "objective-exec-evi",
			CompletionStepID: "s2", Provenance: execjournal.ResearchProvenanceAuthoritative,
		},
		Steps: []execjournal.ResearchStepEvidence{
			{StepID: "s1", Intent: "gather sources", PlanVersion: 1, Status: "completed",
				ObservationID: "obs-1", Provenance: execjournal.ResearchProvenanceAuthoritative},
			{StepID: "s2", Intent: "synthesize findings", PlanVersion: 1, Status: "completed",
				ObservationID: "obs-2", Dependencies: []string{"s1"},
				Provenance: execjournal.ResearchProvenanceAuthoritative},
		},
		Operations: []execjournal.ResearchOperationEvidence{
			{OperationID: "obs-1", StepID: "s1", ToolID: execjournal.ResearchEvidenceCapability,
				Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-1", "src-2"},
				Provenance: execjournal.ResearchProvenanceAuthoritative},
		},
		Attribution: execjournal.ResearchAttributionDerived,
	}
}

// The detail read exposes the durable snapshot and the real terminal
// timestamp, and both survive a literal reopen of the same store (restart).
func TestRecoveryDetailEvidenceSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	ev := recoverySampleEvidence()

	// First process: seed a source-bearing record, then read it through the API.
	s1, e1, st1 := newEvidenceServer(t, dir)
	j1 := e1.ExecutionJournal()
	if err := j1.Accepted("exec-evi", recBiz, "", "user-wide"); err != nil {
		t.Fatalf("seed accepted: %v", err)
	}
	if err := j1.TerminalWithEvidence("exec-evi", execjournal.StateCompleted, "", ev); err != nil {
		t.Fatalf("seed terminal: %v", err)
	}
	w := evidenceGet(t, s1, "/api/v1/recovery/executions/exec-evi?business_id="+recBiz, wideHeaders())
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var v map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v["state"] != "completed" {
		t.Fatalf("state = %v", v["state"])
	}
	if _, ok := v["research_evidence"]; !ok {
		t.Fatalf("evidence missing from detail read: %v", v)
	}
	if _, ok := v["completed_at"]; !ok {
		t.Fatalf("completed_at missing from detail read: %v", v)
	}

	// Second process: reopen the same directory and read through a fresh API.
	closeStore(t, st1)
	s2, _, _ := newEvidenceServer(t, dir)
	w2 := evidenceGet(t, s2, "/api/v1/recovery/executions/exec-evi?business_id="+recBiz, wideHeaders())
	if w2.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w2.Code, w2.Body.String())
	}
	var v2 map[string]interface{}
	if err := json.Unmarshal(w2.Body.Bytes(), &v2); err != nil {
		t.Fatal(err)
	}
	if v2["state"] != "completed" {
		t.Fatalf("state after reopen = %v", v2["state"])
	}
	evAgain, ok := v2["research_evidence"].(map[string]interface{})
	if !ok {
		t.Fatalf("evidence missing after reopen: %v", v2)
	}
	if n, _ := evAgain["count"].(float64); int(n) != 2 {
		t.Fatalf("evidence count after reopen = %v", evAgain["count"])
	}
	sources, _ := evAgain["sources"].([]interface{})
	if len(sources) != 2 {
		t.Fatalf("evidence sources after reopen = %v", evAgain["sources"])
	}
	first, _ := sources[0].(map[string]interface{})
	if first["title"] != "Nexus capability boundary" || first["url"] != "https://docs.example/nexus" {
		t.Fatalf("source fields mangled after reopen: %+v", first)
	}
	plan, _ := evAgain["plan"].(map[string]interface{})
	if plan["plan_id"] != "exec-evi" || int(plan["version"].(float64)) != 1 {
		t.Fatalf("plan identity missing after reopen: %+v", evAgain["plan"])
	}
	if plan["objective_id"] != "objective-exec-evi" || plan["completion_step_id"] != "s2" ||
		plan["provenance"] != execjournal.ResearchProvenanceAuthoritative {
		t.Fatalf("v3 plan provenance missing after reopen: %+v", plan)
	}
	steps, _ := evAgain["steps"].([]interface{})
	if len(steps) != 2 {
		t.Fatalf("steps missing after reopen: %+v", evAgain["steps"])
	}
	secondStep, _ := steps[1].(map[string]interface{})
	if secondStep["step_id"] != "s2" || secondStep["status"] != "completed" ||
		secondStep["intent"] != "synthesize findings" ||
		int(secondStep["plan_version"].(float64)) != 1 ||
		secondStep["provenance"] != execjournal.ResearchProvenanceAuthoritative {
		t.Fatalf("step lifecycle/provenance missing after reopen: %+v", secondStep)
	}
	operations, _ := evAgain["operations"].([]interface{})
	if len(operations) != 1 {
		t.Fatalf("operations missing after reopen: %+v", evAgain["operations"])
	}
	op, _ := operations[0].(map[string]interface{})
	refs, _ := op["source_ids"].([]interface{})
	if len(refs) != 2 || refs[0] != "src-1" || refs[1] != "src-2" || op["step_id"] != "s1" ||
		op["provenance"] != execjournal.ResearchProvenanceAuthoritative {
		t.Fatalf("operation attribution mangled after reopen: %+v", op)
	}
	if evAgain["attribution"] != execjournal.ResearchAttributionDerived {
		t.Fatalf("attribution missing after reopen: %+v", evAgain["attribution"])
	}
}

// An older record exposes neither field: absence stays truthful.
func TestRecoveryDetailLegacyRecordHasNoFabricatedTimestampsOrEvidence(t *testing.T) {
	dir := t.TempDir()
	srv, _, st := newEvidenceServer(t, dir)
	// A v51-era record serialized without the new fields.
	raw := []byte(`{"version":1,"id":"exec-evi-legacy","business_id":"` + recBiz + `","state":"completed",` +
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:01:00Z"}`)
	if err := st.Put(&store.Record{
		ID:         "nexus.execution:exec-evi-legacy",
		Type:       "execution",
		Status:     store.RecordStatusActive,
		BusinessID: recBiz,
		Data:       raw,
	}); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	w := evidenceGet(t, srv, "/api/v1/recovery/executions/exec-evi-legacy?business_id="+recBiz, wideHeaders())
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var v map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	for _, field := range []string{"research_evidence", "completed_at"} {
		if _, ok := v[field]; ok {
			t.Fatalf("legacy record must omit %q, got %v", field, v[field])
		}
	}
}

// The list stays bounded: it carries timestamps but never repeats per-row
// source snapshots.
func TestRecoveryListOmitsEvidence(t *testing.T) {
	dir := t.TempDir()
	srv, e2, _ := newEvidenceServer(t, dir)
	j := e2.ExecutionJournal()
	if err := j.Accepted("exec-evi", recBiz, "", "user-wide"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := j.TerminalWithEvidence("exec-evi", execjournal.StateCompleted, "", recoverySampleEvidence()); err != nil {
		t.Fatalf("seed terminal: %v", err)
	}
	w := evidenceGet(t, srv, "/api/v1/recovery/executions?business_id="+recBiz, wideHeaders())
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body struct {
		Executions []map[string]interface{} `json:"executions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Executions) == 0 {
		t.Fatalf("empty list: %s", w.Body.String())
	}
	for _, e := range body.Executions {
		if _, ok := e["research_evidence"]; ok {
			t.Fatalf("list must not expose evidence, got %v", e["research_evidence"])
		}
		if _, ok := e["completed_at"]; !ok {
			t.Fatalf("list must expose completed_at: %v", e)
		}
	}
}

// ============================================================================
// Phase 3: Black-box process restart acceptance tests
// ============================================================================

// repoRoot finds the repository root by walking up from the current directory
// until it finds go.mod.
func repoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback: assume we're in internal/gateway
	return filepath.Dir(filepath.Dir(filepath.Dir(dir)))
}

// buildNexusBinary builds the real NEXUS binary for acceptance testing.
func buildNexusBinary(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "nexus")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/nexus")
	cmd.Dir = repoRoot()
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0") // faster builds for testing
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build NEXUS binary: %v\n%s", err, string(out))
	}
	return binPath
}

// nexusProcess wraps a running NEXUS child process.
type nexusProcess struct {
	cmd       *exec.Cmd
	baseURL   string
	healthURL string
	// exited is closed when the child process has been reaped by the Wait
	// goroutine. Reading it (non-blocking) tells us whether the process is
	// still alive without racing on exec.Cmd.ProcessState.
	exited chan struct{}
	logs   *strings.Builder
}

func startNexusProcess(t *testing.T, binPath, dataDir string, healthPort, gatewayPort int) *nexusProcess {
	t.Helper()
	return startNexusProcessWith(t, binPath, "", dataDir, healthPort, gatewayPort, nil)
}

// startNexusProcessWith is startNexusProcess plus an explicit `-config` file
// and extra environment. The live Ollama acceptance uses it to point a
// test-owned temporary configuration (never the deployment configuration) at
// the installed local model and capability.
func startNexusProcessWith(t *testing.T, binPath, configPath, dataDir string, healthPort, gatewayPort int, extraEnv map[string]string) *nexusProcess {
	t.Helper()
	args := []string{"-http-addr", net.JoinHostPort("127.0.0.1", strconv.Itoa(gatewayPort))}
	if configPath != "" {
		args = append(args, "-config", configPath)
	}
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(),
		"NEXUS_DATA_DIR="+dataDir,
		"NEXUS_HEALTH_HOST=127.0.0.1",
		"NEXUS_HEALTH_PORT="+strconv.Itoa(healthPort),
		"NEXUS_BOOTSTRAP_CREDENTIAL=boot-secret",
		"NEXUS_BOOTSTRAP_BUSINESS=default",
		"NEXUS_LOG_LEVEL=warn",
	)
	// Later entries win in Go's env slice for the child, so app extras override.
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Capture output into a bounded buffer so a startup failure is diagnosable
	// while never blocking the child on a full pipe.
	var logs strings.Builder
	cmd.Stdout = &boundedWriter{w: &logs}
	cmd.Stderr = &boundedWriter{w: &logs}
	// Start the child in its own process group so a signal to the test process
	// (or the eventual Kill) never leaks across unrelated children.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start NEXUS process: %v", err)
	}

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	baseURL := "http://127.0.0.1:" + strconv.Itoa(gatewayPort)
	healthURL := "http://127.0.0.1:" + strconv.Itoa(healthPort)

	return &nexusProcess{
		cmd:       cmd,
		baseURL:   baseURL,
		healthURL: healthURL,
		exited:    exited,
		logs:      &logs,
	}
}

// boundedWriter keeps at most the most recent 64 KiB so a chatty child can
// never block on a full pipe nor grow test memory without bound.
type boundedWriter struct {
	w *strings.Builder
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	if b.w.Len() < 64*1024 {
		b.w.Write(p)
	}
	return len(p), nil
}

// alive reports whether the child process has not yet exited.
func (p *nexusProcess) alive() bool {
	select {
	case <-p.exited:
		return false
	default:
		return true
	}
}

func (p *nexusProcess) waitForHealth(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		if !p.alive() {
			t.Fatalf("NEXUS process exited during startup (attempt %d)\nlogs: %s", attempt, p.logs.String())
		}
		resp, err := client.Get(p.healthURL + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("health endpoint not ready at %s/health after %d attempts\nlogs: %s", p.healthURL, attempt, p.logs.String())
}

func (p *nexusProcess) stop(t *testing.T) {
	t.Helper()
	if !p.alive() {
		return // already exited; nothing to stop
	}
	// Signal the whole process group so the child (and any helper it spawned)
	// terminates together.
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM); err != nil {
		_ = p.cmd.Process.Kill()
	}
	select {
	case <-p.exited:
		return
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
	select {
	case <-p.exited:
	case <-time.After(5 * time.Second):
		t.Fatalf("NEXUS process did not exit within 5s after SIGKILL")
	}
}

// reservePorts finds two consecutive free ports for health and gateway.
func reservePorts(t *testing.T) (healthPort, gatewayPort int) {
	t.Helper()
	// Try a more robust approach: find a port and check if next port is free
	for attempt := 0; attempt < 50; attempt++ {
		ln1, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		p1 := ln1.Addr().(*net.TCPAddr).Port
		ln1.Close()

		// Check if p1+1 is free by trying to listen on it
		ln2, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p1+1))
		if err != nil {
			continue
		}
		p2 := ln2.Addr().(*net.TCPAddr).Port
		ln2.Close()

		if p2 == p1+1 {
			return p1, p2
		}
	}
	t.Fatalf("could not reserve consecutive free ports")
	return 0, 0
}

// seedResearchRecord seeds a complete terminal research record (evidence + synthesis + evaluation)
// into the given data directory using the journal directly.
func seedResearchRecord(t *testing.T, dir string) string {
	t.Helper()

	reg := identity.NewRegistry("nx:nexus:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)
	members := identity.NewMembershipSet()

	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)

	now := time.Now().UTC()
	must := func(err error, what string) {
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	// Create bootstrap identity, business, membership
	_, err = reg.CreateIdentity(identity.Identity{ID: "nx:human:bootstrap", Type: identity.TypeHuman, DisplayName: "bootstrap", Status: identity.StatusActive, Scope: identity.GlobalScope(), CreatedAt: now}, "t")
	must(err, "bootstrap identity")
	_, err = reg.CreateBusiness(identity.Business{EntityID: "default", BusinessID: "default", Name: "default", OwnerIdentityID: "nx:human:bootstrap", Status: identity.BusinessActive, CreatedAt: now}, "t")
	must(err, "business")
	must(members.Add(identity.Membership{IdentityID: "nx:human:bootstrap", BusinessID: "default", Role: identity.RoleAdmin, Status: identity.StatusActive}), "membership")
	must(auth.Register("nx:human:bootstrap", security.HashCredential([]byte("boot-secret")), identity.AuthMethodToken), "credential")

	// Seed complete research record with correct execution ID in evidence
	execID := "exec-research-1"
	evidence := recoverySampleEvidenceForExec(execID)
	evaluation := recoverySampleEvaluation(execID, evidence)
	synthesis := recoverySampleSynthesis(execID, evidence)

	must(j.Accepted(execID, "default", "", "nx:human:bootstrap"), "accept")
	must(j.TerminalWithResearch(execID, execjournal.StateCompleted, "", evidence, evaluation, synthesis), "terminal")

	// Close store to release lock
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	return execID
}

// recoverySampleEvidenceForExec creates sample evidence with the correct PlanID matching the execution ID.
func recoverySampleEvidenceForExec(execID string) *execjournal.ResearchEvidence {
	ev := recoverySampleEvidence()
	if ev.Plan != nil {
		ev.Plan.PlanID = execID
	}
	return ev
}

// queryRecoveryAPI queries the recovery detail endpoint and returns the parsed JSON.
func queryRecoveryAPI(t *testing.T, baseURL, execID, businessID string) map[string]interface{} {
	t.Helper()
	req, err := http.NewRequest("GET", baseURL+"/api/v1/recovery/executions/"+execID+"?business_id="+businessID, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("X-Actor-ID", "nx:human:bootstrap")
	req.Header.Set("X-Actor-Credential", "boot-secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var v map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return v
}

// verifyResearchRecord checks that the full research record is present, valid,
// and internally consistent: schema versions, cited statements that resolve to
// captured sources, fingerprint bindings, provenance, and timestamps.
func verifyResearchRecord(t *testing.T, v map[string]interface{}) {
	t.Helper()
	verifyResearchRecordFor(t, v, "exec-research-1")
}

// verifyResearchRecordFor is verifyResearchRecord for an execution id that the
// caller does not choose (a live run's id). Every structural invariant is the
// same; only the expected id differs.
func verifyResearchRecordFor(t *testing.T, v map[string]interface{}, execID string) {
	t.Helper()
	if v["source"] != "durable_journal" {
		t.Fatalf("missing source discriminator: %v", v)
	}
	if v["execution_id"] != execID {
		t.Fatalf("execution_id mismatch: got %v want %v", v["execution_id"], execID)
	}
	if v["state"] != "completed" {
		t.Fatalf("state = %v", v["state"])
	}
	if v["business_id"] != "default" {
		t.Fatalf("business_id mismatch: %v", v["business_id"])
	}
	// Timestamps must be present and parseable (real recorded instants, never
	// fabricated zero values).
	for _, field := range []string{"created_at", "updated_at", "completed_at"} {
		raw, ok := v[field].(string)
		if !ok || raw == "" {
			t.Fatalf("%s missing or not a string: %v", field, v[field])
		}
		if _, err := time.Parse(time.RFC3339Nano, raw); err != nil {
			if _, err2 := time.Parse(time.RFC3339, raw); err2 != nil {
				t.Fatalf("%s is not a parseable timestamp: %q", field, raw)
			}
		}
	}
	// No recovery metadata: this record was never interrupted.
	if _, ok := v["recovery"]; ok {
		t.Fatalf("unexpected recovery metadata on non-recovered record: %v", v["recovery"])
	}

	// ---- evidence ----
	ev, ok := v["research_evidence"].(map[string]interface{})
	if !ok {
		t.Fatalf("research_evidence missing or not an object: %v", v["research_evidence"])
	}
	if n, _ := ev["version"].(float64); int(n) != execjournal.ResearchEvidenceVersion {
		t.Fatalf("evidence schema version = %v", ev["version"])
	}
	if ev["capability"] != execjournal.ResearchEvidenceCapability ||
		ev["operation"] != execjournal.ResearchEvidenceOperation {
		t.Fatalf("evidence capability/operation wrong: %+v", ev)
	}
	sources, ok := ev["sources"].([]interface{})
	if !ok || len(sources) != 2 {
		t.Fatalf("evidence sources = %v", ev["sources"])
	}
	if n, _ := ev["count"].(float64); int(n) != len(sources) {
		t.Fatalf("evidence count %v does not match %d sources", ev["count"], len(sources))
	}
	capturedIDs := map[string]bool{}
	for _, raw := range sources {
		s, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("evidence source not an object: %v", raw)
		}
		id, _ := s["id"].(string)
		if id == "" {
			t.Fatalf("evidence source has no id: %v", s)
		}
		capturedIDs[id] = true
		if s["title"] == "" || s["url"] == "" {
			t.Fatalf("evidence source missing title/url: %v", s)
		}
	}
	// v3 provenance: the plan snapshot must belong to this execution.
	plan, ok := ev["plan"].(map[string]interface{})
	if !ok {
		t.Fatalf("evidence plan missing: %v", ev["plan"])
	}
	if plan["plan_id"] != execID || plan["provenance"] != execjournal.ResearchProvenanceAuthoritative {
		t.Fatalf("evidence plan identity/provenance wrong: %+v", plan)
	}

	// ---- synthesis ----
	syn, ok := v["research_synthesis"].(map[string]interface{})
	if !ok {
		t.Fatalf("research_synthesis missing or not an object: %v", v["research_synthesis"])
	}
	if n, _ := syn["version"].(float64); int(n) != execjournal.ResearchSynthesisVersion {
		t.Fatalf("synthesis schema version = %v", syn["version"])
	}
	if syn["method"] != execjournal.ResearchSynthesisMethod {
		t.Fatalf("synthesis method = %v", syn["method"])
	}
	if syn["execution_id"] != execID {
		t.Fatalf("synthesis execution binding = %v", syn["execution_id"])
	}
	if syn["status"] != execjournal.ResearchSynthesisStatusComplete {
		t.Fatalf("synthesis status = %v", syn["status"])
	}
	if syn["summary"] == "" {
		t.Fatalf("synthesis summary is empty")
	}
	fp, _ := syn["evidence_fingerprint"].(string)
	if len(fp) != 64 {
		t.Fatalf("synthesis evidence fingerprint is not a sha-256 hex digest: %q", fp)
	}
	// Every cited source id must resolve to a captured source.
	statements, ok := syn["evidence_backed"].([]interface{})
	if !ok || len(statements) == 0 {
		t.Fatalf("synthesis evidence_backed = %v", syn["evidence_backed"])
	}
	cited := 0
	for _, raw := range statements {
		st, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("synthesis statement not an object: %v", raw)
		}
		if text, _ := st["statement"].(string); text == "" {
			t.Fatalf("synthesis statement is empty: %v", st)
		}
		ids, ok := st["source_ids"].([]interface{})
		if !ok || len(ids) == 0 {
			t.Fatalf("synthesis statement has no source ids: %v", st)
		}
		for _, rawID := range ids {
			id, _ := rawID.(string)
			if !capturedIDs[id] {
				t.Fatalf("synthesis cites %q which is not a captured source id %v", id, capturedIDs)
			}
			cited++
		}
	}
	if cited == 0 {
		t.Fatalf("synthesis carries no resolvable citations")
	}

	// ---- evaluation ----
	eval, ok := v["research_evaluation"].(map[string]interface{})
	if !ok {
		t.Fatalf("research_evaluation missing or not an object: %v", v["research_evaluation"])
	}
	if n, _ := eval["version"].(float64); int(n) != execjournal.ResearchEvaluationVersion {
		t.Fatalf("evaluation schema version = %v", eval["version"])
	}
	if eval["method"] != execjournal.ResearchEvaluationMethod {
		t.Fatalf("evaluation method = %v", eval["method"])
	}
	if eval["execution_id"] != execID {
		t.Fatalf("evaluation execution binding = %v", eval["execution_id"])
	}
	if eval["status"] == "" || eval["status"] == nil {
		t.Fatalf("evaluation status is empty: %v", eval)
	}
	dims, ok := eval["dimensions"].([]interface{})
	if !ok || len(dims) != 8 {
		t.Fatalf("evaluation dimensions = %v (want 8)", eval["dimensions"])
	}
}

// verifyRecordUnchangedAcrossRestart asserts the two detail reads are
// byte-for-byte equivalent, which subsumes state, timestamps, evidence,
// synthesis, evaluation, citations and fingerprints. A completed record must
// not be touched by a startup recovery pass.
func verifyRecordUnchangedAcrossRestart(t *testing.T, first, second map[string]interface{}) {
	t.Helper()
	if !reflect.DeepEqual(first, second) {
		a, _ := json.MarshalIndent(first, "", "  ")
		b, _ := json.MarshalIndent(second, "", "  ")
		t.Fatalf("record changed across restart:\n--- before ---\n%s\n--- after ---\n%s", a, b)
	}
}

// queryRecoveryListCount returns how many durable executions the recovery list
// endpoint reports for a business. It is used to prove recovery created no new
// executions (no replay).
func queryRecoveryListCount(t *testing.T, baseURL, businessID string) int {
	t.Helper()
	req, err := http.NewRequest("GET", baseURL+"/api/v1/recovery/executions?business_id="+businessID, nil)
	if err != nil {
		t.Fatalf("create list request: %v", err)
	}
	req.Header.Set("X-Actor-ID", "nx:human:bootstrap")
	req.Header.Set("X-Actor-Credential", "boot-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list query failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("list unexpected status %d: %s", resp.StatusCode, string(body))
	}
	var body struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return body.Count
}

// TestResearchRecordSurvivesProcessRestart verifies that a complete terminal research record
// (evidence + synthesis + evaluation) survives an actual NEXUS OS-process restart
// and is readable through the HTTP recovery API.
func TestResearchRecordSurvivesProcessRestart(t *testing.T) {
	// Build the real NEXUS binary
	binPath := buildNexusBinary(t)

	// Seed a complete research record into a temp data dir
	dir := t.TempDir()
	execID := seedResearchRecord(t, dir)

	// Reserve consecutive health+gateway ports
	healthPort, gatewayPort := reservePorts(t)

	// ---- First process ----
	proc1 := startNexusProcess(t, binPath, dir, healthPort, gatewayPort)
	defer proc1.stop(t)
	proc1.waitForHealth(t)

	// Query recovery API through the real HTTP server.
	baseURL1 := proc1.baseURL
	resp1 := queryRecoveryAPI(t, baseURL1, execID, "default")
	verifyResearchRecord(t, resp1)
	// Recovery must not have created any additional execution for this business.
	if got := queryRecoveryListCount(t, baseURL1, "default"); got != 1 {
		t.Fatalf("before restart: expected exactly 1 durable execution, got %d", got)
	}

	// Stop first process cleanly and wait for termination.
	proc1.stop(t)

	// ---- Second process (restart against the same data dir) ----
	proc2 := startNexusProcess(t, binPath, dir, healthPort, gatewayPort)
	defer proc2.stop(t)
	proc2.waitForHealth(t)

	// Query again through the fresh process.
	baseURL2 := proc2.baseURL
	resp2 := queryRecoveryAPI(t, baseURL2, execID, "default")
	verifyResearchRecord(t, resp2)

	// The complete record must be identical across the restart: state,
	// timestamps, evidence, synthesis, evaluation, citations and fingerprints.
	verifyRecordUnchangedAcrossRestart(t, resp1, resp2)
	// Startup recovery must not replay work into new executions.
	if got := queryRecoveryListCount(t, baseURL2, "default"); got != 1 {
		t.Fatalf("after restart: expected exactly 1 durable execution (no replay), got %d", got)
	}
}

// TestInterruptedExecutionSurvivesProcessRestart verifies that a non-terminal execution
// is conservatively classified as unknown across a process restart boundary
// and is never re-armed or re-executed.
func TestInterruptedExecutionSurvivesProcessRestart(t *testing.T) {
	// Build the real NEXUS binary
	binPath := buildNexusBinary(t)

	// Seed a non-terminal execution record (accepted but not completed)
	dir := t.TempDir()

	reg := identity.NewRegistry("nx:nexus:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)
	members := identity.NewMembershipSet()

	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)

	now := time.Now().UTC()
	must := func(err error, what string) {
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	_, err = reg.CreateIdentity(identity.Identity{ID: "nx:human:bootstrap", Type: identity.TypeHuman, DisplayName: "bootstrap", Status: identity.StatusActive, Scope: identity.GlobalScope(), CreatedAt: now}, "t")
	must(err, "bootstrap identity")
	_, err = reg.CreateBusiness(identity.Business{EntityID: "default", BusinessID: "default", Name: "default", OwnerIdentityID: "nx:human:bootstrap", Status: identity.BusinessActive, CreatedAt: now}, "t")
	must(err, "business")
	must(members.Add(identity.Membership{IdentityID: "nx:human:bootstrap", BusinessID: "default", Role: identity.RoleAdmin, Status: identity.StatusActive}), "membership")
	must(auth.Register("nx:human:bootstrap", security.HashCredential([]byte("boot-secret")), identity.AuthMethodToken), "credential")

	// Seed an ACCEPTED (non-terminal) record
	execID := "exec-interrupted-1"
	must(j.Accepted(execID, "default", "", "nx:human:bootstrap"), "accept")

	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Reserve ports
	healthPort, gatewayPort := reservePorts(t)

	// ---- First process: start against the data dir ----
	proc1 := startNexusProcess(t, binPath, dir, healthPort, gatewayPort)
	defer proc1.stop(t)
	proc1.waitForHealth(t)

	// The startup recovery pass should classify the non-terminal record as unknown
	// Query the recovery API
	resp1 := queryRecoveryAPI(t, proc1.baseURL, execID, "default")
	if resp1["state"] != "unknown" {
		t.Fatalf("interrupted execution should be classified as unknown, got %v", resp1["state"])
	}
	// Verify recovery metadata present
	recovery, ok := resp1["recovery"].(map[string]interface{})
	if !ok || !recovery["recovered"].(bool) {
		t.Fatalf("recovery metadata missing or not recovered: %v", resp1["recovery"])
	}
	if recovery["prior_state"] != "accepted" {
		t.Fatalf("prior_state should be accepted: %v", recovery["prior_state"])
	}
	// No completed_at for non-terminal
	if _, ok := resp1["completed_at"]; ok {
		t.Fatalf("non-terminal record must not have completed_at: %v", resp1)
	}

	proc1.stop(t)

	// ---- Second process (restart) ----
	proc2 := startNexusProcess(t, binPath, dir, healthPort, gatewayPort)
	defer proc2.stop(t)
	proc2.waitForHealth(t)

	// Query again - should still be unknown, not re-armed
	resp2 := queryRecoveryAPI(t, proc2.baseURL, execID, "default")
	if resp2["state"] != "unknown" {
		t.Fatalf("after restart, interrupted execution should still be unknown, got %v", resp2["state"])
	}
	// Verify recovery metadata preserved
	recovery2, ok := resp2["recovery"].(map[string]interface{})
	if !ok || !recovery2["recovered"].(bool) {
		t.Fatalf("recovery metadata missing after restart: %v", resp2["recovery"])
	}
	if recovery2["prior_state"] != "accepted" {
		t.Fatalf("prior_state changed: %v", recovery2["prior_state"])
	}
	// Verify fingerprint binding (if synthesis/evaluation were absent, they remain absent)
	if _, ok := resp2["research_synthesis"]; ok {
		t.Fatalf("synthesis must not be fabricated for interrupted record: %v", resp2)
	}
	if _, ok := resp2["research_evaluation"]; ok {
		t.Fatalf("evaluation must not be fabricated for interrupted record: %v", resp2)
	}
	// A recovery pass must never replay the interrupted work into new executions.
	if got := queryRecoveryListCount(t, proc2.baseURL, "default"); got != 1 {
		t.Fatalf("after restart: expected exactly 1 durable execution (no replay), got %d", got)
	}
}

// TestRecoveryCrossBusinessAccessAfterRestart verifies cross-business authorization
// boundaries are enforced after a process restart: an actor that belongs only to
// one business can never read another business's durable research record, and the
// denial does not disclose whether the record exists.
func TestRecoveryCrossBusinessAccessAfterRestart(t *testing.T) {
	binPath := buildNexusBinary(t)
	dir := t.TempDir()

	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)

	// Store-backed identity components so every provisioning write is durable
	// and hydrates into the restarted NEXUS process (the process opens the same
	// store-backed registry/authenticator/membership set on boot).
	reg, err := identity.OpenRegistry("nx:nexus:local", st)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	auth, err := identity.OpenLocalAuthenticator(st)
	if err != nil {
		t.Fatalf("open authenticator: %v", err)
	}
	auth.SetRegistry(reg)
	members, err := identity.OpenMembershipSet(st)
	if err != nil {
		t.Fatalf("open memberships: %v", err)
	}

	now := time.Now().UTC()
	must := func(err error, what string) {
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	// Business "default" owned by the bootstrap actor (which the NEXUS process
	// also re-provisions from NEXUS_BOOTSTRAP_CREDENTIAL on boot).
	_, err = reg.CreateIdentity(identity.Identity{ID: "nx:human:bootstrap", Type: identity.TypeHuman, DisplayName: "bootstrap", Status: identity.StatusActive, Scope: identity.GlobalScope(), CreatedAt: now}, "t")
	must(err, "bootstrap identity")
	_, err = reg.CreateBusiness(identity.Business{EntityID: "default", BusinessID: "default", Name: "default", OwnerIdentityID: "nx:human:bootstrap", Status: identity.BusinessActive, CreatedAt: now}, "t")
	must(err, "default business")
	must(members.Add(identity.Membership{IdentityID: "nx:human:bootstrap", BusinessID: "default", Role: identity.RoleAdmin, Status: identity.StatusActive}), "default membership")

	// A second, independent business with its own member identity and credential.
	_, err = reg.CreateIdentity(identity.Identity{ID: "user-biz2", Type: identity.TypeHuman, DisplayName: "biz2-user", Status: identity.StatusActive, Scope: identity.GlobalScope(), CreatedAt: now}, "t")
	must(err, "biz2 identity")
	_, err = reg.CreateBusiness(identity.Business{EntityID: "biz-2", BusinessID: "biz-2", Name: "biz-2", OwnerIdentityID: "user-biz2", Status: identity.BusinessActive, CreatedAt: now}, "t")
	must(err, "biz-2 business")
	must(members.Add(identity.Membership{IdentityID: "user-biz2", BusinessID: "biz-2", Role: identity.RoleAdmin, Status: identity.StatusActive}), "biz2 membership")
	must(auth.Register("user-biz2", security.HashCredential([]byte("biz2-secret")), identity.AuthMethodToken), "biz2 credential")

	// Seed a completed research record owned by business "default".
	execID := "exec-crossbiz-1"
	evidence := recoverySampleEvidenceForExec(execID)
	evaluation := recoverySampleEvaluation(execID, evidence)
	synthesis := recoverySampleSynthesis(execID, evidence)
	must(j.Accepted(execID, "default", "", "nx:human:bootstrap"), "accept")
	must(j.TerminalWithResearch(execID, execjournal.StateCompleted, "", evidence, evaluation, synthesis), "terminal")

	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	healthPort, gatewayPort := reservePorts(t)

	// assertAuthorization checks both the authorized read and the cross-business
	// denial against a running process. It is run against two process lifetimes
	// so authorization is proven to hold across the restart boundary.
	assertAuthorization := func(baseURL string) {
		t.Helper()
		// Authorized access: a member of "default" reads its own record → 200.
		resp := queryRecoveryAPI(t, baseURL, execID, "default")
		if resp["state"] != "completed" {
			t.Fatalf("authorized access failed: %v", resp)
		}
		// Unauthorized cross-business access: the biz-2 member requests the
		// record under "default" → 403 (not a member of that business);
		// existence is not disclosed.
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/recovery/executions/"+execID+"?business_id=default", nil)
		req.Header.Set("X-Actor-ID", "user-biz2")
		req.Header.Set("X-Actor-Credential", "biz2-secret")
		respCross, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("cross-biz query: %v", err)
		}
		defer respCross.Body.Close()
		body, _ := io.ReadAll(respCross.Body)
		if respCross.StatusCode != 403 {
			t.Fatalf("cross-business access should be 403, got %d body=%s", respCross.StatusCode, string(body))
		}
		if strings.Contains(string(body), execID) {
			t.Fatalf("cross-business denial leaked the execution id: %s", string(body))
		}
	}

	// First process against the seeded durable state.
	proc1 := startNexusProcess(t, binPath, dir, healthPort, gatewayPort)
	defer proc1.stop(t)
	proc1.waitForHealth(t)
	assertAuthorization(proc1.baseURL)
	proc1.stop(t)

	// Restart, and prove authorization still holds for the recovered record.
	proc2 := startNexusProcess(t, binPath, dir, healthPort, gatewayPort)
	defer proc2.stop(t)
	proc2.waitForHealth(t)
	assertAuthorization(proc2.baseURL)
}
