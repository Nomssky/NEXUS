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
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
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
	mu        sync.RWMutex
	verifiers map[string]credentialVerifier
	// st is the optional write-through store (nil = in-memory only).
	st store.Store
	// registry, when wired, makes authentication consult the identity record:
	// a credential alone is not enough — the record must exist and be usable
	// (status active, not expired). Nil keeps the M1 credential-only posture
	// for embedders that do not run a registry.
	registry *Registry
	// now is injectable for deterministic tests.
	now func() time.Time
	// ttl bounds the validity of a successful authentication (0 = no expiry).
	ttl time.Duration
}

// NewLocalAuthenticator constructs an empty, in-memory-only local
// authenticator: registered credentials do not survive the process.
func NewLocalAuthenticator() *LocalAuthenticator {
	return &LocalAuthenticator{
		verifiers: map[string]credentialVerifier{},
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// credentialRecord is the stored form of a verifier (SCHEMA_IDENTITIES_ORG
// §10). It carries the verification hash and how the credential is presented —
// never the raw credential, which never leaves the caller's hands.
type credentialRecord struct {
	SchemaVersion string     `json:"schema_version"`
	EntityType    string     `json:"entity_type"`
	IdentityID    string     `json:"identity_id"`
	Hash          string     `json:"hash"`
	Method        AuthMethod `json:"method"`
}

// Stored-envelope stamps for the two record types this package owns beyond
// the registry's entities (SCHEMA_COMMON §3.1).
const (
	credentialSchemaVersion = "1.0.0"
	entityTypeCredential    = "credential"
	membershipSchemaVersion = "1.0.0"
	entityTypeMembership    = "membership"
)

// Store record ids are unique across the whole store (Store.Get/Delete are
// keyed by id alone, and FileStore keeps one index for every type), so a
// credential or membership record cannot reuse its identity's id. Both are
// therefore namespaced by their type, mirroring the SCHEMA_COMMON §7
// {prefix}:{type}:{unique} shape with the identity id as the unique part.
func credentialRecordID(identityID string) string {
	return entityTypeCredential + ":" + identityID
}

func membershipRecordID(identityID string) string {
	return entityTypeMembership + ":" + identityID
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
	a := NewLocalAuthenticator()
	if st == nil {
		return a, nil
	}
	a.st = st
	if err := a.hydrate(); err != nil {
		return nil, err
	}
	return a, nil
}

// hydrate loads every stored verification hash into memory. Soft-deleted
// records are already excluded by the store's List. Call with a fresh
// authenticator (nothing else holds a reference yet).
func (a *LocalAuthenticator) hydrate() error {
	records, err := a.st.List(store.Filter{Type: store.RecordTypeCredential})
	if err != nil {
		return fmt.Errorf("auth: hydrate credentials: %w", err)
	}
	for _, rec := range records {
		var c credentialRecord
		if err := json.Unmarshal(rec.Data, &c); err != nil {
			return fmt.Errorf("auth: corrupt credential record %q: %w", rec.ID, err)
		}
		if credentialRecordID(c.IdentityID) != rec.ID {
			return fmt.Errorf("auth: credential record %q carries identity_id %q", rec.ID, c.IdentityID)
		}
		if strings.TrimSpace(c.IdentityID) == "" || strings.TrimSpace(c.Hash) == "" {
			return fmt.Errorf("auth: invalid credential record %q", rec.ID)
		}
		if !c.Method.IsValid() {
			return fmt.Errorf("auth: credential record %q carries unknown method %q", rec.ID, c.Method)
		}
		a.verifiers[c.IdentityID] = credentialVerifier{
			identityID: c.IdentityID,
			hash:       c.Hash,
			method:     c.Method,
		}
	}
	return nil
}

// SetClock injects a clock for deterministic tests.
func (a *LocalAuthenticator) SetClock(now func() time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.now = now
}

// SetTTL sets how long a successful authentication remains valid.
func (a *LocalAuthenticator) SetTTL(d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ttl = d
}

// SetRegistry wires the identity-record registry for status/expiry enforcement
// (contract §2.4: an identity "ceases to be valid" at expires_at; §2.2 status
// active is the only usable state). With a registry wired, authentication
// fails closed when the identity record is missing, not active, or expired —
// the credential alone no longer suffices.
func (a *LocalAuthenticator) SetRegistry(r *Registry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.registry = r
}

// Register records a verification hash for an identity. The raw credential is
// hashed by the caller via security.HashCredential and is never stored here.
//
// Write-through order matches the registry: persist → memory, so a store
// failure leaves the authenticator untouched (no credential that exists only
// in memory). A nil store keeps the historical in-memory-only posture.
func (a *LocalAuthenticator) Register(identityID string, credentialHash string, method AuthMethod) error {
	if strings.TrimSpace(identityID) == "" {
		return nerrors.Validation("auth.identity_required", "identity id is required for authentication registration")
	}
	if strings.TrimSpace(credentialHash) == "" {
		return nerrors.Validation("auth.credential_hash_required", "credential hash is required")
	}
	// Reject at the write rather than at the next boot: an unknown method
	// would otherwise be persisted happily and then fail hydration closed.
	if !method.IsValid() {
		return nerrors.Validation("auth.method_invalid",
			fmt.Sprintf("auth method %q is not a canonical method", method))
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.st != nil {
		rec, err := marshalRecord(store.RecordTypeCredential, credentialRecordID(identityID), "", "", credentialRecord{
			SchemaVersion: credentialSchemaVersion,
			EntityType:    entityTypeCredential,
			IdentityID:    identityID,
			Hash:          credentialHash,
			Method:        method,
		})
		if err != nil {
			return err
		}
		if err := persistRecord(a.st, rec); err != nil {
			return err
		}
	}
	a.verifiers[identityID] = credentialVerifier{identityID: identityID, hash: credentialHash, method: method}
	return nil
}

// unknownIdentityHash is what an identity with no verifier is compared
// against. The unknown-id path therefore performs the same SHA-256 and the
// same constant-time compare as a known id with a wrong credential — without
// it the skipped work is a timing oracle for which identity ids exist (F11).
// It can never authenticate anyone: the !ok branch fails regardless of the
// comparison result.
var unknownIdentityHash = security.HashCredential([]byte("nexus:unknown-identity"))

// Authenticate implements Authenticator. It fails closed: unknown identities,
// empty credentials, mismatches, and unusable records all return a
// non-authenticated result with the same canonical AUTH error, the same
// reason and the same method — nothing in the result distinguishes which
// check failed — and all of them cost the same amount of work.
func (a *LocalAuthenticator) Authenticate(identityID string, credential []byte) (AuthResult, error) {
	a.mu.RLock()
	v, ok := a.verifiers[identityID]
	nowFn := a.now
	reg := a.registry
	a.mu.RUnlock()

	// Hash and compare unconditionally: the branch below must not change how
	// much work an unknown identity costs (F11).
	expected := unknownIdentityHash
	if ok {
		expected = v.hash
	}
	matched := security.ConstantTimeEqual(security.HashCredential(credential), expected)

	if !ok || len(credential) == 0 || !matched {
		// Uniform failure: reason, method and error are identical whatever
		// the cause, so neither the response nor its cost leaks which
		// identities exist or which check failed.
		return AuthResult{
			Authenticated: false,
			IdentityID:    identityID,
			Method:        AuthMethodNone,
			Reason:        "authentication failed",
		}, nerrors.New("auth.failed", nerrors.CategoryAuth, "authentication failed")
	}
	now := nowFn()
	if reg != nil {
		// Fail closed: no record, non-active status, or past expires_at all
		// deny with the same uniform error (never reveal which).
		ident, found := reg.GetIdentity(identityID)
		if !found || ident.Status != StatusActive ||
			(ident.ExpiresAt != nil && !now.Before(*ident.ExpiresAt)) {
			return AuthResult{
				Authenticated: false,
				IdentityID:    identityID,
				Method:        AuthMethodNone,
				Reason:        "authentication failed",
			}, nerrors.New("auth.failed", nerrors.CategoryAuth, "authentication failed")
		}
	}
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
