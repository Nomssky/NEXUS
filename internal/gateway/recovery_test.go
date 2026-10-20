package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
