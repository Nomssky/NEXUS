package gateway

// Capability lifecycle control and reliability discovery at the HTTP boundary
// (contracts/OPERATIONAL_RELIABILITY_CONTRACTS.md §8, §10).
//
// Lifecycle is an operator action under the control prefix: it is gated by the
// existing X-API-Key check, it can only remove or restore invocations of an
// already-registered capability, and every transition outside the contract
// graph is refused rather than silently ignored.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

const capabilityLifecycleKey = "capability-lifecycle-key"

type capabilityFixture struct {
	ts       *httptest.Server
	platform *capability.Platform
}

func newCapabilityFixture(t *testing.T, key string) *capabilityFixture {
	t.Helper()
	orgs := identity.NewRegistry("nx:nexus:test")
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

	tools := tool.NewToolRegistry()
	platform := capability.New(tools, members,
		capability.NewScopedCredentialResolver(security.NewDevResolver()), engine.EventBus())
	if err := tools.Register(capability.HTTPToolManifest(), capability.NewHTTPTool(capability.DefaultHTTPPolicy())); err != nil {
		t.Fatal(err)
	}

	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(orgs)
	now := time.Now().UTC()
	orgs.CreateIdentity(identity.Identity{ID: "owner", Type: identity.TypeHuman, DisplayName: "o",
		Provenance: schema.ProvenanceRef{Producer: "test", ProducedAt: now}}, "t")
	orgs.CreateBusiness(identity.Business{EntityID: "biz-1", Name: "b", OwnerIdentityID: "owner", CreatedAt: now}, "t")
	members.Add(identity.Membership{IdentityID: "owner", BusinessID: "biz-1", Role: identity.RoleMember, Status: identity.StatusActive})
	if err := auth.Register("owner", security.HashCredential([]byte("s3cret")), identity.AuthMethodToken); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(engine, "127.0.0.1:0",
		WithRegistry(orgs), WithIdentity(auth, members),
		WithRequireAuthentication(true), WithEnforceBusinessScope(true),
		WithControlAPIKey(key), WithNexusID("nx:nexus:test"),
		WithCapabilityPlatform(platform))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &capabilityFixture{ts: ts, platform: platform}
}

func (f *capabilityFixture) lifecycleState(toolID, state, key string) (int, map[string]any) {
	body, _ := json.Marshal(map[string]string{"state": state, "reason": "test"})
	req, err := http.NewRequest(http.MethodPost, f.ts.URL+"/api/v1/control/capabilities/"+toolID+"/state", strings.NewReader(string(body)))
	if err != nil {
		return 0, nil
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	payload := map[string]any{}
	_ = json.Unmarshal(raw, &payload)
	return resp.StatusCode, payload
}

func (f *capabilityFixture) catalog() map[string]any {
	req, _ := http.NewRequest(http.MethodGet, f.ts.URL+"/api/v1/tools?business_id=biz-1", nil)
	req.Header.Set("X-Actor-ID", "owner")
	req.Header.Set("X-Actor-Credential", "s3cret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	payload := map[string]any{}
	_ = json.Unmarshal(raw, &payload)
	return payload
}

func catalogEntryByID(t *testing.T, payload map[string]any, id string) map[string]any {
	t.Helper()
	tools, ok := payload["tools"].([]any)
	if !ok {
		t.Fatalf("catalog payload has no tools array: %v", payload)
	}
	for _, raw := range tools {
		entry, _ := raw.(map[string]any)
		if entry["tool_id"] == id {
			return entry
		}
	}
	t.Fatalf("capability %q missing from the catalog", id)
	return nil
}

func TestCapabilityLifecycleRequiresControlKey(t *testing.T) {
	f := newCapabilityFixture(t, "")
	if code, _ := f.lifecycleState("http.request", "disabled", capabilityLifecycleKey); code != http.StatusForbidden {
		t.Fatalf("no configured key must fail closed: %d", code)
	}
	f2 := newCapabilityFixture(t, capabilityLifecycleKey)
	if code, _ := f2.lifecycleState("http.request", "disabled", ""); code != http.StatusUnauthorized {
		t.Fatalf("missing key must be rejected: %d", code)
	}
	if code, _ := f2.lifecycleState("http.request", "disabled", "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong key must be rejected: %d", code)
	}
	if f2.platform.CapabilityState("http.request") != capability.CapabilityEnabled {
		t.Fatalf("a refused control call must not change state")
	}
}

func TestCapabilityLifecycleTransitions(t *testing.T) {
	f := newCapabilityFixture(t, capabilityLifecycleKey)
	code, payload := f.lifecycleState("http.request", "disabled", capabilityLifecycleKey)
	if code != http.StatusOK {
		t.Fatalf("disable: %d %v", code, payload)
	}
	if payload["capability_state"] != "disabled" {
		t.Fatalf("state not echoed: %v", payload)
	}
	if f.platform.CapabilityState("http.request") != capability.CapabilityDisabled {
		t.Fatalf("platform state = %q, want disabled", f.platform.CapabilityState("http.request"))
	}
	// The catalog reports the lifecycle state to discovery clients.
	entry := catalogEntryByID(t, f.catalog(), "http.request")
	if entry["capability_state"] != "disabled" {
		t.Fatalf("catalog must report the lifecycle state, got %v", entry)
	}
	if code, _ := f.lifecycleState("http.request", "deprecated", capabilityLifecycleKey); code != http.StatusConflict {
		t.Fatalf("disabled -> deprecated must conflict, got %d", code)
	}
	if code, _ := f.lifecycleState("http.request", "enabled", capabilityLifecycleKey); code != http.StatusOK {
		t.Fatalf("re-enable must succeed, got %d", code)
	}
	if code, _ := f.lifecycleState("http.request", "bogus", capabilityLifecycleKey); code != http.StatusBadRequest {
		t.Fatalf("an unknown state must be a validation failure, got %d", code)
	}
	if code, _ := f.lifecycleState("not.registered", "disabled", capabilityLifecycleKey); code != http.StatusNotFound {
		t.Fatalf("an unregistered capability must be 404, got %d", code)
	}
}

func TestCapabilityLifecycleBlocksInvocation(t *testing.T) {
	f := newCapabilityFixture(t, capabilityLifecycleKey)
	if code, _ := f.lifecycleState("http.request", "disabled", capabilityLifecycleKey); code != http.StatusOK {
		t.Fatalf("disable: %d", code)
	}
	res := f.platform.Invoke(context.Background(), capability.Request{
		ToolID: "http.request", Operation: "get",
		Input:      map[string]string{"url": "https://example.com"},
		ActorID:    "owner",
		BusinessID: "biz-1",
		AgentID:    "agent-1",
		AgentTools: []string{"http.request"},
	})
	if res.Status == tool.StatusSuccess {
		t.Fatalf("a disabled capability must refuse invocation")
	}
	if !strings.Contains(res.Error, capability.ErrCapabilityDisabled.Error()) {
		t.Fatalf("refusal must carry the capability_disabled class, got %q", res.Error)
	}
}

func TestCapabilityDiscoveryReportsReliabilityPosture(t *testing.T) {
	f := newCapabilityFixture(t, capabilityLifecycleKey)
	entry := catalogEntryByID(t, f.catalog(), "http.request")
	if entry["capability_state"] != "enabled" {
		t.Fatalf("default state must be enabled, got %v", entry["capability_state"])
	}
	if entry["supports_idempotency"] != true {
		t.Fatalf("the HTTP capability must advertise idempotency support, got %v", entry)
	}
	if entry["retry_policy"] != "none" {
		t.Fatalf("an external mutation capability must not advertise an automatic retry policy, got %v", entry["retry_policy"])
	}
	if entry["max_attempts"] != float64(1) {
		t.Fatalf("a mutation capability must advertise a single attempt, got %v", entry["max_attempts"])
	}
	if entry["supports_reconciliation"] == true {
		t.Fatalf("no capability may advertise reconciliation without implementing it")
	}
	if _, leaked := entry["credential_value"]; leaked {
		t.Fatalf("discovery must never carry credential material")
	}
}
