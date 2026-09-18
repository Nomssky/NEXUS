package security

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// TEST-M1-013: secret reference != secret value.
func TestSecretRefIsNotSecretValue(t *testing.T) {
	ref, err := ParseSecretRef("store:instagram_token?business=biz-a&purpose=posting")
	if err != nil {
		t.Fatalf("valid ref rejected: %v", err)
	}
	if !ref.Valid() {
		t.Fatal("parsed ref should be valid")
	}
	if ref.Store != "store" || ref.Name != "instagram_token" {
		t.Fatalf("unexpected parsed ref: %+v", ref)
	}
	if ref.BusinessID != "biz-a" || ref.Purpose != "posting" {
		t.Fatalf("ref metadata not parsed: %+v", ref)
	}
	// The ref is a reference, never the value. There is no field holding a value.
	if strings.Contains(ref.String(), "secret-value") {
		t.Fatal("ref string must never carry a value")
	}
}

// TEST-M1-014: dev resolver is development-only and scope-enforcing.
func TestDevResolverScopeEnforcement(t *testing.T) {
	r := NewDevResolver()
	if !r.DevelopmentOnly() {
		t.Fatal("dev resolver must be marked development-only")
	}
	ref, _ := ParseSecretRef("store:ig_token?business=biz-a")
	if err := r.Put(ref, []byte("s3cr3t-value")); err != nil {
		t.Fatal(err)
	}
	// Resolving with the matching scope succeeds.
	sec, err := r.Resolve(ref, "biz-a", "")
	if err != nil {
		t.Fatalf("expected resolve to succeed: %v", err)
	}
	if string(sec.Reveal()) != "s3cr3t-value" {
		t.Fatal("resolved value mismatch")
	}
	// Resolving a ref for a different business fails closed.
	if _, err := r.Resolve(ref, "biz-b", ""); err == nil {
		t.Fatal("cross-business secret resolution must fail closed")
	}
	// Missing secret fails closed.
	missing, _ := ParseSecretRef("store:does_not_exist?business=biz-a")
	if _, err := r.Resolve(missing, "biz-a", ""); err == nil {
		t.Fatal("missing secret must fail closed")
	}
}

// TEST-M1-015: secret value is redacted in logs / string / json.
func TestSecretRedaction(t *testing.T) {
	s := NewSecret("store", "name", []byte("super-secret"))
	if strings.Contains(s.String(), "super-secret") {
		t.Fatal("String() must not expose the secret value")
	}
	if !strings.Contains(s.String(), "REDACTED") {
		t.Fatalf("String() should be redacted, got %q", s.String())
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "super-secret") {
		t.Fatal("MarshalJSON must not expose the secret value")
	}
	// Reveal is the only explicit accessor.
	if string(s.Reveal()) != "super-secret" {
		t.Fatal("Reveal must return the value")
	}
}

// TEST-M1-016: log redaction of nested maps.
func TestRedactMap(t *testing.T) {
	in := map[string]any{
		"username": "alice",
		"api_key":  "abc123",
		"nested": map[string]any{
			"password": "hunter2",
			"note":     "ok",
		},
	}
	out := RedactMap(in)
	if out["api_key"] != RedactMarker() {
		t.Fatalf("api_key must be redacted, got %v", out["api_key"])
	}
	if out["username"] != "alice" {
		t.Fatal("non-secret key must be preserved")
	}
	nm := out["nested"].(map[string]any)
	if nm["password"] != RedactMarker() {
		t.Fatalf("nested password must be redacted, got %v", nm["password"])
	}
	if nm["note"] != "ok" {
		t.Fatal("nested non-secret must be preserved")
	}
	if !IsSecretKey("access_token") || !IsSecretKey("SECRET") {
		t.Fatal("common secret key names must be detected")
	}
}

// TEST-M1-017: ID generation — random, opaque, non-empty.
func TestIDGeneration(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 200; i++ {
		id, err := NewID("agent")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(id, "nx:agent:") {
			t.Fatalf("unexpected id prefix: %s", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id: %s", id)
		}
		seen[id] = struct{}{}
	}
	cid, err := NewCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	if cid == "" {
		t.Fatal("correlation id must be non-empty")
	}
}

// TEST-M1-018: constant-time compare correctness.
func TestConstantTimeCompare(t *testing.T) {
	if !ConstantTimeEqual("abc", "abc") {
		t.Fatal("equal strings must compare equal")
	}
	if ConstantTimeEqual("abc", "abd") {
		t.Fatal("different strings must not compare equal")
	}
	if ConstantTimeEqual("abc", "abcd") {
		t.Fatal("different-length strings must not compare equal")
	}
}

// TEST-M1-021: sandbox/egress deny-by-default.
func TestEgressDenyByDefault(t *testing.T) {
	p := NewEgressPolicy(nil)
	if !p.Empty() {
		t.Fatal("empty allow-list must produce deny-all")
	}
	if p.Allow("api.instagram.com") {
		t.Fatal("deny-by-default egress must not allow anything")
	}
	p2 := NewEgressPolicy([]string{"api.instagram.com"})
	if p2.Empty() {
		t.Fatal("non-empty allow-list must not be Empty()")
	}
	if !p2.Allow("api.instagram.com") {
		t.Fatal("allow-listed host must be allowed")
	}
	if p2.Allow("evil.example.com") {
		t.Fatal("non-allow-listed host must be denied")
	}
}

// TEST-M1-027: secret ref validation rejects malformed & path traversal.
func TestSecretRefValidation(t *testing.T) {
	bad := []string{
		"",
		"storesecretwithoutprefix",
		"store:",
		"store:../etc/passwd?business=biz-a",
		"store:a?business=",
	}
	for _, s := range bad {
		if _, err := ParseSecretRef(s); err == nil {
			t.Fatalf("malformed ref must be rejected: %q", s)
		} else if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
			t.Fatalf("expected VALIDATION for %q, got %s", s, nerrors.CategoryOf(err))
		}
	}
}

// TEST-M1-028: scoped credential refs enforce business/division isolation.
func TestSecretRefScoping(t *testing.T) {
	ref, _ := ParseSecretRef("store:tok?business=biz-a&division=media")
	if !ref.ScopedTo("biz-a", "media") {
		t.Fatal("ref should be scoped to biz-a/media")
	}
	if ref.ScopedTo("biz-b", "media") {
		t.Fatal("ref must not be scoped to another business")
	}
	if ref.ScopedTo("biz-a", "research") {
		t.Fatal("ref must not be scoped to another division")
	}
	// An unscoped ref is only usable in a global (empty-business) context.
	global, _ := ParseSecretRef("store:tok")
	if !global.ScopedTo("", "") {
		t.Fatal("unscoped ref applies to the global context")
	}
	if global.ScopedTo("biz-a", "") {
		t.Fatal("unscoped ref must not silently apply inside a business context")
	}
}
