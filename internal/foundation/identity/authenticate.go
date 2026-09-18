// Package identity — authentication primitives.
//
// Authentication answers exactly one question: "Has this identity been
// authenticated?" It does NOT answer "what may they do?" (that is authorization)
// and it does NOT grant permissions of any kind.
//
//	AUTHENTICATION != AUTHORIZATION
//
// An authenticated identity is not automatically authorized (see authorize.go).
// M1 implements only the minimal, local primitives the foundation needs; no
// external federation, OIDC, or network identity provider is implemented.
//
// Deferred (recorded, not implemented): session stores, token issuance/rotation,
// password hashing policies, MFA/passkeys, device binding, federation, service
// mesh authn, and token audience/audience enforcement at real boundaries.
package identity

import (
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// AuthMethod names how an identity was authenticated.
type AuthMethod string

const (
	AuthMethodNone     AuthMethod = "none"
	AuthMethodPassword AuthMethod = "password"
	AuthMethodToken    AuthMethod = "token"
	AuthMethodService  AuthMethod = "service"
	AuthMethodDevice   AuthMethod = "device"
)

// AuthResult is the outcome of an authentication attempt.
//
// It reports whether the identity was authenticated and, if so, by what method,
// when, and for how long the authentication is valid. It carries NO permission,
// capability, or authority.
type AuthResult struct {
	// Authenticated is true only when the identity was successfully verified.
	Authenticated bool `json:"authenticated"`
	// IdentityID is the identity that was (attempted to be) authenticated.
	IdentityID string `json:"identity_id"`
	// Method is how authentication was performed.
	Method AuthMethod `json:"method"`
	// AuthenticatedAt is when authentication succeeded (zero if it did not).
	AuthenticatedAt time.Time `json:"authenticated_at,omitempty"`
	// ExpiresAt bounds the authentication (zero = no explicit expiry).
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	// Reason is a short, secret-free explanation for a failed attempt.
	Reason string `json:"reason,omitempty"`
}

// Expired reports whether the authentication has lapsed as of now. A zero
// ExpiresAt never expires.
func (r AuthResult) Expired(now time.Time) bool {
	return !r.ExpiresAt.IsZero() && !now.Before(r.ExpiresAt)
}

// Valid reports whether the result represents a currently-valid authentication.
func (r AuthResult) Valid(now time.Time) bool {
	return r.Authenticated && !r.Expired(now)
}

// Authenticator verifies a credential *reference* against the claimed identity.
//
// Implementations must:
//   - fail closed (deny) on any uncertainty;
//   - never log or embed the raw credential;
//   - return an AuthResult that carries no permission.
//
// The credential is supplied as raw bytes at the boundary (the caller already
// holds it); the authenticator compares it in constant time against a
// verification hash and never stores the raw value.
type Authenticator interface {
	Authenticate(identityID string, credential []byte) (AuthResult, error)
}

// credentialVerifier holds a verification hash for an identity. It never holds
// the raw credential.
type credentialVerifier struct {
	identityID string
	// hash is the SHA-256 of the (salted) credential; comparison is by hash to
	// avoid storing the secret. Salting is left to the credential issuer (C02 at
	// a later milestone); M1 demonstrates the boundary, not a production KDF.
	hash   string
	method AuthMethod
}

// LocalAuthenticator is a minimal, in-process authenticator.
//
// It is deliberately simple and NOT a production identity provider. It exists so
// the foundation can exercise the authentication boundary deterministically. It
// stores only verification hashes, never raw credentials, and never grants
// permission.
type LocalAuthenticator struct {
	verifiers map[string]credentialVerifier
	// now is injectable for deterministic tests.
	now func() time.Time
	// ttl bounds the validity of a successful authentication (0 = no expiry).
	ttl time.Duration
}

// NewLocalAuthenticator constructs an empty local authenticator.
func NewLocalAuthenticator() *LocalAuthenticator {
	return &LocalAuthenticator{
		verifiers: map[string]credentialVerifier{},
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// SetClock injects a clock for deterministic tests.
func (a *LocalAuthenticator) SetClock(now func() time.Time) { a.now = now }

// SetTTL sets how long a successful authentication remains valid.
func (a *LocalAuthenticator) SetTTL(d time.Duration) { a.ttl = d }

// Register records a verification hash for an identity. The raw credential is
// hashed by the caller via security.HashCredential and is never stored here.
func (a *LocalAuthenticator) Register(identityID string, credentialHash string, method AuthMethod) error {
	if strings.TrimSpace(identityID) == "" {
		return nerrors.Validation("auth.identity_required", "identity id is required for authentication registration")
	}
	if strings.TrimSpace(credentialHash) == "" {
		return nerrors.Validation("auth.credential_hash_required", "credential hash is required")
	}
	a.verifiers[identityID] = credentialVerifier{identityID: identityID, hash: credentialHash, method: method}
	return nil
}

// Authenticate implements Authenticator. It fails closed: unknown identities,
// empty credentials, and mismatches all return a non-authenticated result with a
// canonical AUTH error.
func (a *LocalAuthenticator) Authenticate(identityID string, credential []byte) (AuthResult, error) {
	v, ok := a.verifiers[identityID]
	if !ok || len(credential) == 0 {
		// Uniform failure reason avoids leaking which identities exist.
		return AuthResult{
			Authenticated: false,
			IdentityID:    identityID,
			Method:        AuthMethodNone,
			Reason:        "authentication failed",
		}, nerrors.New("auth.failed", nerrors.CategoryAuth, "authentication failed")
	}
	got := security.HashCredential(credential)
	if !security.ConstantTimeEqual(got, v.hash) {
		return AuthResult{
			Authenticated: false,
			IdentityID:    identityID,
			Method:        v.method,
			Reason:        "authentication failed",
		}, nerrors.New("auth.failed", nerrors.CategoryAuth, "authentication failed")
	}
	now := a.now()
	res := AuthResult{
		Authenticated:   true,
		IdentityID:      identityID,
		Method:          v.method,
		AuthenticatedAt: now,
	}
	if a.ttl > 0 {
		res.ExpiresAt = now.Add(a.ttl)
	}
	return res, nil
}

// RequireAuthenticated returns a canonical AUTH error unless the identity is
// currently authenticated. This is the single helper later components should use
// to enforce the authentication gate; it enforces only the "who" question and
// never grants permission.
func RequireAuthenticated(res AuthResult, now time.Time) error {
	if !res.Valid(now) {
		return nerrors.New("auth.required", nerrors.CategoryAuth, "an authenticated identity is required")
	}
	return nil
}
