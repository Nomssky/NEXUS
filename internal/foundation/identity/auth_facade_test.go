package identity

import (
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// The facade must remain a drop-in Authenticator.
var _ Authenticator = (*LocalAuthenticator)(nil)

// A nil store degrades to the in-memory posture instead of failing.
func TestOpenLocalAuthenticatorNilStore(t *testing.T) {
	a, err := OpenLocalAuthenticator(nil)
	if err != nil {
		t.Fatalf("open nil store: %v", err)
	}
	const id = "nx:human:nil-store"
	if err := a.Register(id, security.HashCredential([]byte("x")), AuthMethodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	if res, err := a.Authenticate(id, []byte("x")); err != nil || !res.Authenticated {
		t.Fatalf("in-memory fallback must authenticate: %+v / %v", res, err)
	}
}

// SetRegistry wires enforcement; SetRegistry(nil) removes it again.
func TestSetRegistryTogglesEnforcement(t *testing.T) {
	a := NewLocalAuthenticator()
	const id = "nx:human:toggle"
	if err := a.Register(id, security.HashCredential([]byte("x")), AuthMethodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}

	// A registry that does not contain the identity must deny.
	empty := NewRegistry(testNexus)
	a.SetRegistry(empty)
	if res, err := a.Authenticate(id, []byte("x")); err == nil || res.Authenticated {
		t.Fatalf("missing identity record must fail closed: %+v / %v", res, err)
	}

	// Removing the binding restores the credential-only posture.
	a.SetRegistry(nil)
	if res, err := a.Authenticate(id, []byte("x")); err != nil || !res.Authenticated {
		t.Fatalf("credential-only posture must authenticate: %+v / %v", res, err)
	}

	// Wiring a registry that does contain the identity authenticates.
	full := NewRegistry(testNexus)
	ident := mustIdentity(t, TypeHuman, "Toggle", GlobalScope())
	if _, err := full.CreateIdentity(ident, "test"); err != nil {
		t.Fatalf("create identity: %v", err)
	}
	if err := a.Register(ident.ID, security.HashCredential([]byte("y")), AuthMethodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	a.SetRegistry(full)
	if res, err := a.Authenticate(ident.ID, []byte("y")); err != nil || !res.Authenticated {
		t.Fatalf("active identity must authenticate: %+v / %v", res, err)
	}
}

func TestRequireAuthenticated(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	if err := RequireAuthenticated(AuthResult{Authenticated: true, IdentityID: "nx:human:x"}, now); err != nil {
		t.Fatalf("valid authentication must pass: %v", err)
	}
	if err := RequireAuthenticated(AuthResult{Authenticated: false}, now); err == nil {
		t.Fatal("unauthenticated result must be rejected")
	}
	expired := AuthResult{Authenticated: true, ExpiresAt: now.Add(-time.Second)}
	if err := RequireAuthenticated(expired, now); err == nil {
		t.Fatal("expired authentication must be rejected")
	}
}
