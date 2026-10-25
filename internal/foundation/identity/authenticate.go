// Package identity — authentication primitives.
//
// Authentication answers exactly one question: "Has this identity been
// authenticated?" It does NOT answer "what may they do?" (that is authorization)
// and it does NOT grant permissions of any kind.
//
//	AUTHENTICATION != AUTHORIZATION
//
// An authenticated identity is not automatically authorized (see authorize.go).
// The module is structured as ports & adapters: the use-case core
// (auth_service.go) depends only on the interfaces in auth_ports.go, while
// concrete storage, registry lookup and hashing live in auth_adapters.go.
// LocalAuthenticator below is a thin facade that wires those adapters and keeps
// the original public API stable for embedders.
//
// M1 implements only the minimal, local primitives the foundation needs; no
// external federation, OIDC, or network identity provider is implemented.
//
// Deferred (recorded, not implemented): session stores, token issuance/rotation,
// password hashing policies, MFA/passkeys, device binding, federation, service
// mesh authn, and token audience/audience enforcement at real boundaries.
package identity

import (
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
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

// authMethods is the closed set of AuthMethod values (SCHEMA_IDENTITIES_ORG
// §10: only these can be stored or hydrated).
var authMethods = map[AuthMethod]struct{}{
	AuthMethodNone:     {},
	AuthMethodPassword: {},
	AuthMethodToken:    {},
	AuthMethodService:  {},
	AuthMethodDevice:   {},
}

// IsValid reports whether m is one of the canonical methods.
func (m AuthMethod) IsValid() bool {
	_, ok := authMethods[m]
	return ok
}

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

// LocalAuthenticator is the authentication module's public facade.
//
// It is deliberately simple and NOT a production identity provider. It exists so
// the foundation can exercise the authentication boundary deterministically. It
// stores only verification hashes, never raw credentials, and never grants
// permission.
//
// The facade owns no authentication logic: every rule lives in
// authenticationService and every storage detail lives in an adapter. Callers
// keep this API so the internal architecture can change without a repo-wide
// migration.
type LocalAuthenticator struct {
	svc *authenticationService
}

// NewLocalAuthenticator constructs an empty, in-memory-only local
// authenticator: registered credentials do not survive the process.
func NewLocalAuthenticator() *LocalAuthenticator {
	return &LocalAuthenticator{
		svc: newAuthenticationService(newMemoryCredentialRepository(), sha256Verifier{}),
	}
}

// OpenLocalAuthenticator constructs a write-through local authenticator: every
// Register persists the verification hash, and the stored verifiers are
// hydrated before OpenLocalAuthenticator returns. Hydration fails closed — a
// record that cannot be decoded or validated aborts the call so boot never
// continues on a partially restored authenticator. A nil st degrades to
// NewLocalAuthenticator.
//
// The registry binding is separate (SetRegistry): durability answers "does
// this hash survive?", the registry answers "is this identity usable?".
func OpenLocalAuthenticator(st store.Store) (*LocalAuthenticator, error) {
	if st == nil {
		return NewLocalAuthenticator(), nil
	}
	repo, err := newStoreCredentialRepository(st)
	if err != nil {
		return nil, err
	}
	return &LocalAuthenticator{
		svc: newAuthenticationService(repo, sha256Verifier{}),
	}, nil
}

// SetClock injects a clock for deterministic tests.
func (a *LocalAuthenticator) SetClock(now func() time.Time) {
	a.svc.SetClock(now)
}

// SetTTL sets how long a successful authentication remains valid.
func (a *LocalAuthenticator) SetTTL(d time.Duration) {
	a.svc.SetTTL(d)
}

// SetRegistry wires the identity-record registry for status/expiry enforcement
// (contract §2.4: an identity "ceases to be valid" at expires_at; §2.2 status
// active is the only usable state). With a registry wired, authentication
// fails closed when the identity record is missing, not active, or expired —
// the credential alone no longer suffices. A nil registry removes the binding.
func (a *LocalAuthenticator) SetRegistry(r *Registry) {
	if r == nil {
		a.svc.SetDirectory(nil)
		return
	}
	a.svc.SetDirectory(registryDirectory{registry: r})
}

// Register records a verification hash for an identity. The raw credential is
// hashed by the caller via security.HashCredential and is never stored here.
func (a *LocalAuthenticator) Register(identityID string, credentialHash string, method AuthMethod) error {
	return a.svc.Register(identityID, credentialHash, method)
}

// Authenticate implements Authenticator. It fails closed with a uniform result
// and error for every failure cause (see authenticationService.Authenticate).
func (a *LocalAuthenticator) Authenticate(identityID string, credential []byte) (AuthResult, error) {
	return a.svc.Authenticate(identityID, credential)
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
