package gateway

// Agent Memory & Context Platform v1 at the HTTP boundary
// (contracts/AGENT_MEMORY_CONTEXT_CONTRACTS.md §15): the minimal memory surface
// on the existing identity and membership gates, optimistic concurrency, G5
// visibility, and the fact that a response never carries authority.

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

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	"github.com/Nomssky/NEXUS/internal/memory"
)

const memorySecret = "tok-abcdef-123456"

type memoryFixture struct {
	srv    *Server
	ts     *httptest.Server
	orgs   *identity.Registry
	memory *memory.Platform
}

func newMemoryFixture(t *testing.T) *memoryFixture {
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
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })

	// The capability platform owns the redaction layer; memory reuses it.
	tools := tool.NewToolRegistry()
	platform := capability.New(tools, members,
		capability.NewScopedCredentialResolver(security.NewDevResolver()), engine.EventBus())
	platform.RegisterSecret(memorySecret)

	memPlatform, err := memory.Open(store.NewMemStore(), memory.Options{
		Scopes: memory.NewMembershipScopes(members.AllowsScope),
		Redact: memory.RedactorFunc(platform.Redactor),
		Bus:    memory.NewBusSink(engine.EventBus()),
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	orgs.CreateIdentity(identity.Identity{ID: "owner", Type: identity.TypeHuman, DisplayName: "o",
		Provenance: schema.ProvenanceRef{Producer: "test", ProducedAt: now}}, "t")
	orgs.CreateIdentity(identity.Identity{ID: "outsider", Type: identity.TypeHuman, DisplayName: "out",
		Provenance: schema.ProvenanceRef{Producer: "test", ProducedAt: now}}, "t")
	orgs.CreateBusiness(identity.Business{EntityID: "biz-1", Name: "b", OwnerIdentityID: "owner", CreatedAt: now}, "t")
	orgs.CreateBusiness(identity.Business{EntityID: "biz-2", Name: "b2", OwnerIdentityID: "owner", CreatedAt: now}, "t")
	if err := members.Add(identity.Membership{IdentityID: "owner", BusinessID: "biz-1",
		Role: identity.RoleMember, Status: identity.StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := members.Add(identity.Membership{IdentityID: "outsider", BusinessID: "biz-2",
		Role: identity.RoleMember, Status: identity.StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := auth.Register("owner", security.HashCredential([]byte("s3cret")), identity.AuthMethodToken); err != nil {
		t.Fatal(err)
	}
	if err := auth.Register("outsider", security.HashCredential([]byte("s3cret")), identity.AuthMethodToken); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(engine, "127.0.0.1:0",
		WithRegistry(orgs), WithIdentity(auth, members),
		WithRequireAuthentication(true), WithEnforceBusinessScope(true),
		WithMemory(memPlatform))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &memoryFixture{srv: srv, ts: ts, orgs: orgs, memory: memPlatform}
}

func (f *memoryFixture) call(t *testing.T, method, path, actor, cred string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, f.ts.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if actor != "" {
		req.Header.Set("X-Actor-ID", actor)
		req.Header.Set("X-Actor-Credential", cred)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return resp.StatusCode, payload
}

func (f *memoryFixture) owner(t *testing.T, method, path string, body any) (int, map[string]any) {
	return f.call(t, method, path, "owner", "s3cret", body)
}

func TestMemoryLifecycleOverHTTP(t *testing.T) {
	f := newMemoryFixture(t)
	code, created := f.owner(t, http.MethodPost, "/api/v1/memory?business_id=biz-1", map[string]any{
		"key": "endpoint", "value": "https://api.example", "type": "fact", "scope": "business",
	})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, created)
	}
	if created["source"] != "user_instruction" || created["trust"] != "explicit" {
		t.Fatalf("an authenticated write must be explicit user memory: %v", created)
	}
	if created["capability_state"] != nil {
		t.Fatalf("a memory response must carry no authority fields: %v", created)
	}
	id, _ := created["memory_id"].(string)
	if id == "" {
		t.Fatalf("create must return the record id: %v", created)
	}
	version, _ := created["version"].(float64)
	if version < 1 {
		t.Fatalf("create must return the version: %v", created)
	}

	if code, got := f.owner(t, http.MethodGet, "/api/v1/memory/"+id+"?business_id=biz-1", nil); code != http.StatusOK {
		t.Fatalf("read: %d %v", code, got)
	}
	if code, got := f.owner(t, http.MethodPost, "/api/v1/memory/query?business_id=biz-1",
		map[string]any{"key": "endpoint"}); code != http.StatusOK {
		t.Fatalf("query: %d %v", code, got)
	} else if got["count"].(float64) != 1 {
		t.Fatalf("query must find the record: %v", got)
	}

	// Optimistic concurrency: a stale update conflicts, the right one succeeds.
	code, conflict := f.owner(t, http.MethodPatch, "/api/v1/memory/"+id+"?business_id=biz-1",
		map[string]any{"value": "https://api.v2", "expected_version": int(version)})
	if code != http.StatusOK {
		t.Fatalf("update: %d %v", code, conflict)
	}
	code, stale := f.owner(t, http.MethodPatch, "/api/v1/memory/"+id+"?business_id=biz-1",
		map[string]any{"value": "https://api.v3", "expected_version": int(version)})
	if code != http.StatusConflict {
		t.Fatalf("a stale update must be a conflict: %d %v", code, stale)
	}
	code, _ = f.owner(t, http.MethodPatch, "/api/v1/memory/"+id+"?business_id=biz-1",
		map[string]any{"value": "x"})
	if code != http.StatusBadRequest {
		t.Fatalf("an update without expected_version must be rejected: %d", code)
	}

	code, deleted := f.owner(t, http.MethodDelete, "/api/v1/memory/"+id+"?business_id=biz-1", nil)
	if code != http.StatusOK || deleted["status"] != "deleted" {
		t.Fatalf("delete: %d %v", code, deleted)
	}
	if code, _ := f.owner(t, http.MethodGet, "/api/v1/memory/"+id+"?business_id=biz-1", nil); code != http.StatusNotFound {
		t.Fatalf("a deleted record must be gone: %d", code)
	}
}

func TestMemorySurfaceAuthorizationAndVisibility(t *testing.T) {
	f := newMemoryFixture(t)
	_, created := f.owner(t, http.MethodPost, "/api/v1/memory?business_id=biz-1", map[string]any{
		"key": "k", "value": "v", "scope": "business",
	})
	id := created["memory_id"].(string)

	// No credentials at all.
	if code, _ := f.call(t, http.MethodGet, "/api/v1/memory/"+id+"?business_id=biz-1", "", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated read must be 401: %d", code)
	}
	// A member of another business must not see it, and must not learn it exists.
	code, hidden := f.call(t, http.MethodGet, "/api/v1/memory/"+id+"?business_id=biz-2", "outsider", "s3cret", nil)
	if code != http.StatusNotFound {
		t.Fatalf("cross-business read must be a not-found: %d %v", code, hidden)
	}
	// Same question against an id that never existed: identical envelope.
	codeMissing, missing := f.call(t, http.MethodGet,
		"/api/v1/memory/mem:biz-1:_:never-existed?business_id=biz-2", "outsider", "s3cret", nil)
	if codeMissing != code {
		t.Fatalf("a hidden record must answer exactly like a missing one: %d vs %d", code, codeMissing)
	}
	if missing["error"].(map[string]any)["message"] != hidden["error"].(map[string]any)["message"] {
		t.Fatalf("the two answers must be indistinguishable: %v vs %v", missing, hidden)
	}
	// Missing scope on every verb that exists on the collection.
	if code, _ := f.owner(t, http.MethodPost, "/api/v1/memory", nil); code != http.StatusBadRequest {
		t.Fatalf("create without business_id must be 400: %d", code)
	}
	if code, _ := f.owner(t, http.MethodPost, "/api/v1/memory/query", nil); code != http.StatusBadRequest {
		t.Fatalf("query without business_id must be 400: %d", code)
	}
	// A foreign business query is refused at membership.
	if code, _ := f.call(t, http.MethodPost, "/api/v1/memory/query?business_id=biz-2",
		"outsider", "s3cret", map[string]any{}); code != http.StatusOK {
		t.Fatalf("a member must be able to query its own business: %d", code)
	}
	if code, _ := f.call(t, http.MethodPost, "/api/v1/memory/query?business_id=biz-1",
		"outsider", "s3cret", map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("a foreign business query must be refused: %d", code)
	}
	// Malformed and out-of-vocabulary bodies are validation failures.
	if code, _ := f.owner(t, http.MethodPost, "/api/v1/memory?business_id=biz-1",
		map[string]any{"value": "no key"}); code != http.StatusBadRequest {
		t.Fatalf("a keyless create must be 400: %d", code)
	}
	if code, _ := f.owner(t, http.MethodPost, "/api/v1/memory?business_id=biz-1",
		map[string]any{"key": "k", "value": "v", "type": "quantum"}); code != http.StatusBadRequest {
		t.Fatalf("a non-canonical type must be 400: %d", code)
	}
}

func TestMemoryAPIIsBoundedAndSecretFree(t *testing.T) {
	f := newMemoryFixture(t)
	// An oversized value is refused, not truncated.
	code, _ := f.owner(t, http.MethodPost, "/api/v1/memory?business_id=biz-1", map[string]any{
		"key": "big", "value": strings.Repeat("x", 16*1024),
	})
	if code != http.StatusBadRequest {
		t.Fatalf("an oversized value must be rejected: %d", code)
	}
	// A secret never survives into durable memory.
	code, created := f.owner(t, http.MethodPost, "/api/v1/memory?business_id=biz-1", map[string]any{
		"key": "with-secret", "value": "the token is " + memorySecret,
	})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, created)
	}
	value, _ := created["value"].(string)
	if strings.Contains(value, memorySecret) {
		t.Fatalf("a secret must never be stored or returned: %q", value)
	}
	if !strings.Contains(value, "[redacted]") {
		t.Fatalf("redaction must be visible in the response: %q", value)
	}
	// A query response carries bounded metadata, never credentials.
	_, q := f.owner(t, http.MethodPost, "/api/v1/memory/query?business_id=biz-1", map[string]any{"limit": 1000})
	if strings.Contains(fmt.Sprint(q), memorySecret) {
		t.Fatalf("a query response leaked a secret: %v", q)
	}
	if q["note"] == nil {
		t.Fatalf("the query response should state that memory grants nothing: %v", q)
	}
}

func TestMemoryEndpointFailsClosedWithoutPlatform(t *testing.T) {
	members := identity.NewMembershipSet()
	engine, err := core.NewEngine(nil, core.WithIdentity(members, true, true))
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	srv := NewServer(engine, "127.0.0.1:0", WithEnforceBusinessScope(true))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + "/api/v1/memory?business_id=biz-1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a missing memory platform must fail closed: %d", resp.StatusCode)
	}
}
