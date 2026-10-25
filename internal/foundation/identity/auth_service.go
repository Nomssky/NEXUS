// Package identity — authentication use-case core.
//
// authenticationService is the application core of the authentication module.
// It orchestrates the ports declared in auth_ports.go and contains every
// authentication rule: validation, uniform failure (F11), status/expiry
// enforcement and result construction. It knows nothing about how credentials
// are stored, how identities are looked up, or which hash function is used.
//
// Authentication answers exactly one question: "has this identity been
// authenticated?". It never grants permission — AUTHORIZATION is a separate
// concern (authorize.go).
package identity

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// unknownIdentityHashSeed is hashed once at construction and used for identities
// that have no credential. The unknown-id path therefore performs the same hash
// and the same constant-time compare as a known id with a wrong credential —
// without it the skipped work is a timing oracle for which identity ids exist
// (F11). It can never authenticate anyone: the !ok branch fails regardless of
// the comparison result.
const unknownIdentityHashSeed = "nexus:unknown-identity"

// authenticationService is the authentication use-case core. All fields except
// the repository are guarded by mu because they may be reconfigured at runtime
// (SetClock, SetTTL, SetDirectory).
type authenticationService struct {
	creds    CredentialRepository
	verifier CredentialVerifier
	// unknownHash is verifier.Hash(unknownIdentityHashSeed), precomputed so the
	// failure path costs the same as a real lookup.
	unknownHash string

	mu        sync.RWMutex
	directory IdentityDirectory
	// now is injectable for deterministic tests.
	now func() time.Time
	// ttl bounds the validity of a successful authentication (0 = no expiry).
	ttl time.Duration
}

// newAuthenticationService wires the use-case core to its ports. It never
// returns nil; the only required dependency is a credential repository.
func newAuthenticationService(creds CredentialRepository, verifier CredentialVerifier) *authenticationService {
	return &authenticationService{
		creds:       creds,
		verifier:    verifier,
		unknownHash: verifier.Hash([]byte(unknownIdentityHashSeed)),
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// SetDirectory wires the identity directory used for status/expiry enforcement
// (contract §2.4: an identity "ceases to be valid" at expires_at; §2.2 status
// active is the only usable state). A nil directory disables the check.
func (s *authenticationService) SetDirectory(d IdentityDirectory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.directory = d
}

// SetClock injects a clock for deterministic tests. A nil clock is ignored.
func (s *authenticationService) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// SetTTL sets how long a successful authentication remains valid.
func (s *authenticationService) SetTTL(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttl = d
}

// Register records a verification hash for an identity. The raw credential is
// hashed by the caller and is never accepted or stored here.
//
// Write-through order is owned by the repository adapter: persist → memory, so
// a store failure leaves the authenticator untouched (no credential that exists
// only in memory).
func (s *authenticationService) Register(identityID string, credentialHash string, method AuthMethod) error {
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
	return s.creds.Save(Credential{
		IdentityID: identityID,
		Hash:       credentialHash,
		Method:     method,
	})
}

// Authenticate verifies a credential against the claimed identity. It fails
// closed: unknown identities, empty credentials, mismatches, and unusable
// records all return a non-authenticated result with the same canonical AUTH
// error, the same reason and the same method — nothing in the result
// distinguishes which check failed — and all of them cost the same amount of
// work.
func (s *authenticationService) Authenticate(identityID string, credential []byte) (AuthResult, error) {
	v, ok := s.creds.Find(identityID)

	s.mu.RLock()
	nowFn := s.now
	dir := s.directory
	ttl := s.ttl
	s.mu.RUnlock()

	// Hash and compare unconditionally: the branch below must not change how
	// much work an unknown identity costs (F11).
	expected := s.unknownHash
	if ok {
		expected = v.Hash
	}
	matched := s.verifier.Equal(s.verifier.Hash(credential), expected)

	if !ok || len(credential) == 0 || !matched {
		return authenticationFailed(identityID)
	}

	now := nowFn()
	if dir != nil {
		// Fail closed: no record, non-active status, or past expires_at all
		// deny with the same uniform error (never reveal which).
		ident, found := dir.Lookup(identityID)
		if !found || ident.Status != StatusActive ||
			(ident.ExpiresAt != nil && !now.Before(*ident.ExpiresAt)) {
			return authenticationFailed(identityID)
		}
	}

	res := AuthResult{
		Authenticated:   true,
		IdentityID:      identityID,
		Method:          v.Method,
		AuthenticatedAt: now,
	}
	if ttl > 0 {
		res.ExpiresAt = now.Add(ttl)
	}
	return res, nil
}

// authenticationFailed builds the single, uniform failure response. Reason,
// method and error are identical whatever the cause, so neither the response
// nor its cost leaks which identities exist or which check failed.
func authenticationFailed(identityID string) (AuthResult, error) {
	return AuthResult{
		Authenticated: false,
		IdentityID:    identityID,
		Method:        AuthMethodNone,
		Reason:        "authentication failed",
	}, nerrors.New("auth.failed", nerrors.CategoryAuth, "authentication failed")
}
