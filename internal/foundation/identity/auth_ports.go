// Package identity — authentication ports (ports & adapters / hexagonal).
//
// The authentication use-case (auth_service.go) depends only on the small set
// of ports declared here. Concrete persistence (in-memory, store-backed),
// identity lookup (the organization registry) and credential hashing are
// adapters (auth_adapters.go), so the core can be exercised without a store,
// a registry, or a crypto primitive.
//
//	core ──> ports <── adapters
//
// Nothing in this file imports the store, the registry or the security
// package: the dependency arrows point inward.
package identity

// Credential is the credential material the authentication core understands:
// an already-hashed verification value plus the method it was issued for. It
// never carries the raw credential — callers hash at the boundary.
type Credential struct {
	// IdentityID is the identity this credential authenticates.
	IdentityID string
	// Hash is the verification hash of the credential material.
	Hash string
	// Method is how the credential is presented (password, token, ...).
	Method AuthMethod
}

// CredentialRepository is the driven port that owns credential persistence
// (repository pattern). Implementations must be safe for concurrent use and
// must fail closed: a record that cannot be decoded or validated is an error,
// never a silently skipped entry that leaves the repository partly restored.
type CredentialRepository interface {
	// Find returns the credential stored for identityID. The boolean is false
	// when no credential exists; an absent credential is not an error.
	Find(identityID string) (Credential, bool)
	// LoadAll returns every credential currently stored.
	LoadAll() ([]Credential, error)
	// Save persists a credential. Write-through implementations must persist
	// before the credential becomes observable through Find, so a store
	// failure can never leave a credential that exists only in memory.
	Save(Credential) error
}

// IdentityDirectory is the driven port used to answer "is this identity
// usable?" independently of the credential. A nil directory preserves the
// credential-only posture for embedders that do not run a registry.
type IdentityDirectory interface {
	// Lookup returns the identity record and whether it exists.
	Lookup(identityID string) (Identity, bool)
}

// CredentialVerifier is the driven port for hashing and comparing credentials.
// It keeps a concrete crypto primitive out of the core and lets tests inject a
// deterministic verifier. Equal must compare in constant time.
type CredentialVerifier interface {
	// Hash returns the verification hash for raw credential bytes.
	Hash(credential []byte) string
	// Equal reports whether two verification hashes match, in constant time.
	Equal(a, b string) bool
}
