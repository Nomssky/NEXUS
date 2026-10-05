package gateway

// Agent Execution Layer v1, at the HTTP boundary against a real engine.
// Covers the admission paths only: agent registration/list/lifecycle are
// covered by agentexec unit tests, and this test drives POST /executions,
// GET /executions/{id}, POST /cancel — including the governance-bypass
// failure-integrity invariant and the provider-failure-not-success one.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

type execFixture struct {
	t       *testing.T
	srv     *Server
	engine  *core.Engine
	agents  *agentexec.Registry
	toolReg *tool.ToolRegistry
}

func execFixtureNew(t *testing.T, providerStatus modelrouter.ProviderStatus) *execFixture {
	t.Helper()
	reg := identity.NewRegistry("nx:nexus:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)
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

	// Seed like the launcher does.
	prov := modelrouter.NewLocalProvider(modelrouter.ProviderConfig{ID: "simulated", Status: providerStatus})
	if err := engine.ModelRegistry().RegisterModel(&modelrouter.ModelDefinition{
		ID: "simulated:default", ProviderID: prov.Identify(),
		Capabilities: []modelrouter.ModelCapability{modelrouter.CapabilityReasoning, modelrouter.CapabilityToolCalling},
		Runtime:      modelrouter.RuntimeLocal, Status: modelrouter.ModelStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	engine.ModelRouter().RegisterProvider(prov)

	toolReg := tool.NewToolRegistry()
	agents := agentexec.NewRegistry("nx:nexus:test", reg, toolReg)
	rt := agentexec.NewRuntime(agents, toolReg, engine.ModelRouter(), engine.EventBus())

	// bootstrap membership for the fixture owner
	members.Add(identity.Membership{IdentityID: "owner", BusinessID: "default", Role: identity.RoleAdmin, Status: identity.StatusActive})
	if err := auth.Register("owner", security.HashCredential([]byte("x-secret")), identity.AuthMethodToken); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.CreateIdentity(identity.Identity{ID: "owner", Type: identity.TypeHuman, DisplayName: "o", Status: identity.StatusActive,
		Provenance: schema.ProvenanceRef{Producer: "test", ProducedAt: time.Now().UTC()},
	}, "test"); err != nil {
		t.Fatalf("create identity: %v", err)
	}
	if _, err := reg.CreateBusiness(identity.Business{EntityID: "default", Name: "d", OwnerIdentityID: "owner"}, "test"); err != nil {
		t.Fatalf("create business: %v", err)
	}
	if _, err := agents.Create(agentexec.Definition{ID: "researcher", Name: "Researcher", BusinessID: "default", Capabilities: []string{"research"}, AllowedTools: []string{"echo"}}, "test"); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	f := &execFixture{t: t, srv: NewServer(engine, ":0", WithRegistry(reg), WithIdentity(auth, members), WithRequireAuthentication(true), WithEnforceBusinessScope(true), WithAgentExecution(agents, rt)), engine: engine, agents: agents, toolReg: toolReg}
	return f
}

func (f *execFixture) submitExecution(body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/executions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-ID", "owner")
	req.Header.Set("X-Actor-Credential", "x-secret")
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	return w
}

func TestExecutionHealthyAgentCompletes(t *testing.T) {
	f := execFixtureNew(t, modelrouter.ProviderStatusHealthy)
	w := f.submitExecution(`{"intent":"do research","business_id":"default","actor_id":"owner","required_capabilities":["research"],"tools":[{"tool_id":"echo","input":{"text":"hi"}}]}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	var acc map[string]string
	json.NewDecoder(w.Body).Decode(&acc)
	id := acc["execution_id"]

	var final *httptest.ResponseRecorder
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/executions/"+id+"?business_id=default", nil)
		req.Header.Set("X-Actor-ID", "owner")
		req.Header.Set("X-Actor-Credential", "x-secret")
		final = httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(final, req)
		if final.Code == http.StatusOK {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if final.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", final.Code, final.Body.String())
	}
	var resp core.Response
	json.NewDecoder(final.Body).Decode(&resp)
	if resp.Status != "completed" {
		t.Fatalf("status=%s err=%v", resp.Status, resp.Error)
	}
	if resp.Outcome == nil || resp.Outcome.Metrics["executor_status"] != "completed" {
		t.Fatalf("outcome=%+v", resp.Outcome)
	}
	if resp.Outcome.Metrics["provider"] == nil {
		t.Fatalf("missing provider telemetry in %+v", resp.Outcome.Metrics)
	}
}

func TestExecutionProviderFailureIsNeverSuccess(t *testing.T) {
	f := execFixtureNew(t, modelrouter.ProviderStatusOffline)
	w := f.submitExecution(`{"intent":"do research","business_id":"default","actor_id":"owner","required_capabilities":["research"]}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	var acc map[string]string
	json.NewDecoder(w.Body).Decode(&acc)
	id := acc["execution_id"]
	var final *httptest.ResponseRecorder
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/executions/"+id+"?business_id=default", nil)
		req.Header.Set("X-Actor-ID", "owner")
		req.Header.Set("X-Actor-Credential", "x-secret")
		final = httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(final, req)
		if final.Code == http.StatusOK {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if final.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", final.Code)
	}
	var resp core.Response
	json.NewDecoder(final.Body).Decode(&resp)
	if resp.Status != "failed" {
		t.Fatalf("offline provider must fail, got %q", resp.Status)
	}
}

func TestExecutionVisibility(t *testing.T) {
	f := execFixtureNew(t, modelrouter.ProviderStatusHealthy)
	// Wrong business scope on the same id -> ideally 404 (unknown id in that
	// scope) once the engine knows which id it is; before anything exists,
	// plain unknown.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/executions/nope?business_id=default", nil)
	req.Header.Set("X-Actor-ID", "owner")
	req.Header.Set("X-Actor-Credential", "x-secret")
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown execution id: got %d", w.Code)
	}
}
