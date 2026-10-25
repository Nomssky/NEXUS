// Package identity — authentication adapters.
//
// Each adapter in this file implements one port from auth_ports.go against a
// concrete technology: process memory, the record store, the identity registry
// and the security package's SHA-256 helper. The core never imports any of
// them; only the facade (authenticate.go) chooses which adapters to wire.
package identity

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// Stored-envelope stamps for credential records (SCHEMA_COMMON §3.1).
const (
	credentialSchemaVersion = "1.0.0"
	entityTypeCredential    = "credential"
)

// credentialRecord is the stored form of a credential (SCHEMA_IDENTITIES_ORG
// §10). It carries the verification hash and how the credential is presented —
// never the raw credential, which never leaves the caller's hands.
type credentialRecord struct {
	SchemaVersion string     `json:"schema_version"`
	EntityType    string     `json:"entity_type"`
	IdentityID    string     `json:"identity_id"`
	Hash          string     `json:"hash"`
	Method        AuthMethod `json:"method"`
}

// Store record ids are unique across the whole store (Store.Get/Delete are
// keyed by id alone, and FileStore keeps one index for every type), so a
// credential record cannot reuse its identity's id. It is therefore namespaced
// by type, mirroring the SCHEMA_COMMON §7 {prefix}:{type}:{unique} shape with
// the identity id as the unique part.
func credentialRecordID(identityID string) string {
	return entityTypeCredential + ":" + identityID
}

// sha256Verifier adapts the security package to the CredentialVerifier port.
// Comparison is constant time.
type sha256Verifier struct{}

func (sha256Verifier) Hash(credential []byte) string { return security.HashCredential(credential) }

func (sha256Verifier) Equal(a, b string) bool { return security.ConstantTimeEqual(a, b) }

// memoryCredentialRepository is the in-process adapter. It is the write-through
// target of storeCredentialRepository and the whole repository when no store is
// configured.
type memoryCredentialRepository struct {
	mu      sync.RWMutex
	records map[string]Credential
}

func newMemoryCredentialRepository() *memoryCredentialRepository {
	return &memoryCredentialRepository{records: map[string]Credential{}}
}

func (r *memoryCredentialRepository) Find(identityID string) (Credential, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.records[identityID]
	return c, ok
}

func (r *memoryCredentialRepository) LoadAll() ([]Credential, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Credential, 0, len(r.records))
	for _, c := range r.records {
		out = append(out, c)
	}
	// Deterministic order keeps hydration and tests reproducible.
	sort.Slice(out, func(i, j int) bool { return out[i].IdentityID < out[j].IdentityID })
	return out, nil
}

func (r *memoryCredentialRepository) Save(c Credential) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[c.IdentityID] = c
	return nil
}

// storeCredentialRepository is the write-through adapter: Save persists the
// record first and only then makes it visible in memory, and construction
// hydrates every stored verifier before returning. Hydration fails closed — a
// record that cannot be decoded or validated aborts the open so boot never
// continues on a partially restored authenticator.
type storeCredentialRepository struct {
	st store.Store
	// saveMu serialises the read-before-persist + persist + memory sequence so
	// concurrent registrations cannot interleave a version check and a write.
	saveMu sync.Mutex
	mem    *memoryCredentialRepository
}

// newStoreCredentialRepository hydrates st into a ready repository. A nil store
// is a programming error at this layer (the facade degrades before calling).
func newStoreCredentialRepository(st store.Store) (*storeCredentialRepository, error) {
	r := &storeCredentialRepository{st: st, mem: newMemoryCredentialRepository()}
	if err := r.hydrate(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *storeCredentialRepository) Find(identityID string) (Credential, bool) {
	return r.mem.Find(identityID)
}

func (r *storeCredentialRepository) LoadAll() ([]Credential, error) {
	return r.mem.LoadAll()
}

func (r *storeCredentialRepository) Save(c Credential) error {
	r.saveMu.Lock()
	defer r.saveMu.Unlock()
	rec, err := marshalRecord(store.RecordTypeCredential, credentialRecordID(c.IdentityID), "", "", credentialRecord{
		SchemaVersion: credentialSchemaVersion,
		EntityType:    entityTypeCredential,
		IdentityID:    c.IdentityID,
		Hash:          c.Hash,
		Method:        c.Method,
	})
	if err != nil {
		return err
	}
	if err := persistRecord(r.st, rec); err != nil {
		return err
	}
	return r.mem.Save(c)
}

// hydrate loads every stored verification hash into memory. Soft-deleted
// records are already excluded by the store's List.
func (r *storeCredentialRepository) hydrate() error {
	records, err := r.st.List(store.Filter{Type: store.RecordTypeCredential})
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
		if err := r.mem.Save(Credential{
			IdentityID: c.IdentityID,
			Hash:       c.Hash,
			Method:     c.Method,
		}); err != nil {
			return err
		}
	}
	return nil
}

// registryDirectory adapts the organization registry to the IdentityDirectory
// port. It is the only place where the authentication module reads the registry.
type registryDirectory struct {
	registry *Registry
}

func (d registryDirectory) Lookup(identityID string) (Identity, bool) {
	if d.registry == nil {
		return Identity{}, false
	}
	return d.registry.GetIdentity(identityID)
}

// Compile-time proof that every adapter satisfies its port.
var (
	_ CredentialRepository = (*memoryCredentialRepository)(nil)
	_ CredentialRepository = (*storeCredentialRepository)(nil)
	_ IdentityDirectory    = registryDirectory{}
	_ CredentialVerifier   = sha256Verifier{}
)
