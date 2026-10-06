package gateway

// Agent intelligence layer v1 at the HTTP boundary: admission (G2/G3/G5),
// telemetry in the terminal result, and failure integrity.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/agentintel"
	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	memmemory "github.com/Nomssky/NEXUS/internal/memory"
)

type intelFixture struct {
	srv    *Server
	engine *core.Engine
	orgs   *identity.Registry
	intel  *agentintel.Runtime
}

func newIntelFixture(t *testing.T, providerStatus modelrouter.ProviderStatus) *intelFixture {
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
	prov := modelrouter.NewScriptedProvider(modelrouter.ProviderConfig{ID: "simulated", Status: providerStatus})
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
	orgs.CreateDivision(identity.Division{EntityID: "div-1", BusinessID: "default", Name: "div", OwnerIdentityID: "owner", CreatedAt: now}, "t")
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
	return &intelFixture{srv: srv, engine: engine, orgs: orgs, intel: intel}
}

func (f *intelFixture) post(path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-ID", "owner")
	req.Header.Set("X-Actor-Credential", "s3cret")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	return w
}

func (f *intelFixture) get(path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	return w
}

func (f *intelFixture) poll(id string) map[string]interface{} {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		w := f.get("/api/v1/intelligence/"+id+"?business_id=default", map[string]string{
			"X-Actor-ID": "owner", "X-Actor-Credential": "s3cret",
		})
		if w.Code == http.StatusOK {
			var out map[string]interface{}
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			return out
		}
		if w.Code != http.StatusAccepted {
			return map[string]interface{}{"status": w.Result().Status}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return map[string]interface{}{"status": "timeout"}
}

const intelObjectiveBody = `{"business_id":"default","actor_id":"owner","objective":{"description":"multiply six by seven with the calculator"}}`

func TestIntelligenceExecutionCompletesWithState(t *testing.T) {
	f := newIntelFixture(t, modelrouter.ProviderStatusHealthy)
	w := f.post("/api/v1/intelligence/execute", intelObjectiveBody, nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	var acc map[string]string
	json.Unmarshal(w.Body.Bytes(), &acc)
	res := f.poll(acc["execution_id"])
	if res["status"] != "completed" {
		t.Fatalf("expected completed, got %v (%v)", res["status"], res["error"])
	}
	summary := res["outcome"].(map[string]interface{})["summary"].(string)
	if !bytes.Contains([]byte(summary), []byte("state=completed")) {
		t.Fatalf("terminal state must be observable in the summary: %q", summary)
	}
	if f.intel.State(acc["execution_id"]) == "" {
		t.Fatal("loop state must be recorded")
	}
}

func TestIntelligenceProviderFailureIsNeverSuccess(t *testing.T) {
	f := newIntelFixture(t, modelrouter.ProviderStatusOffline)
	w := f.post("/api/v1/intelligence/execute", intelObjectiveBody, nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	var acc map[string]string
	json.Unmarshal(w.Body.Bytes(), &acc)
	res := f.poll(acc["execution_id"])
	if res["status"] != "failed" {
		t.Fatalf("offline provider must fail the objective, got %v", res["status"])
	}
}

func TestIntelligenceAdmissionRules(t *testing.T) {
	f := newIntelFixture(t, modelrouter.ProviderStatusHealthy)

	// Missing scope.
	if w := f.post("/api/v1/intelligence/execute", `{"actor_id":"owner","objective":{"description":"x"}}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing business_id: %d", w.Code)
	}
	// Unauthenticated.
	if w := f.post("/api/v1/intelligence/execute", intelObjectiveBody, map[string]string{"X-Actor-Credential": "wrong"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong credential: %d", w.Code)
	}
	// Actor spoofing.
	spoof := `{"business_id":"default","actor_id":"someone-else","objective":{"description":"x"}}`
	if w := f.post("/api/v1/intelligence/execute", spoof, nil); w.Code != http.StatusForbidden {
		t.Fatalf("actor spoof: %d", w.Code)
	}
	// Malformed objective.
	if w := f.post("/api/v1/intelligence/execute", `{"business_id":"default","actor_id":"owner","objective":{}}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("empty objective: %d", w.Code)
	}
	// G5: unknown execution id is 404; missing business_id on read is 400.
	if w := f.get("/api/v1/intelligence/nope?business_id=default", map[string]string{"X-Actor-ID": "owner", "X-Actor-Credential": "s3cret"}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", w.Code)
	}
	if w := f.get("/api/v1/intelligence/nope", map[string]string{"X-Actor-ID": "owner", "X-Actor-Credential": "s3cret"}); w.Code != http.StatusBadRequest {
		t.Fatalf("missing scope on read: %d", w.Code)
	}
	// G2: a suspended business admits no new objective.
	if _, err := f.orgs.SetBusinessStatus("default", identity.BusinessSuspended, "owner"); err != nil {
		t.Fatal(err)
	}
	w := f.post("/api/v1/intelligence/execute", intelObjectiveBody, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("suspended business: %d %s", w.Code, w.Body.String())
	}
}
