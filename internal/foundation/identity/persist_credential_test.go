package identity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// TEST-IDR-12: credential write-through — a verification hash registered
// through OpenLocalAuthenticator is restored by the next open of the same
// directory, so an identity created with a credential still authenticates
// after a restart. The stored record carries the hash and the method, never
// the raw credential.
func TestAuthenticatorCredentialPersistence(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	auth, err := OpenLocalAuthenticator(st)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const id = "nx:human:persisted"
	raw := []byte("raw-credential-material")
	hash := security.HashCredential(raw)
	if err := auth.Register(id, hash, AuthMethodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}

	// The record on disk holds the hash, not the secret.
	onDisk, err := os.ReadFile(filepath.Join(dir, string(store.RecordTypeCredential), credentialRecordID(id)+".json"))
	if err != nil {
		t.Fatalf("read stored credential: %v", err)
	}
	// Record.Data is []byte, so the envelope JSON carries it base64-encoded;
	// decode the envelope before looking at the payload.
	var rec store.Record
	if err := json.Unmarshal(onDisk, &rec); err != nil {
		t.Fatalf("stored credential is not a store envelope: %v", err)
	}
	if bytes.Contains(rec.Data, raw) {
		t.Fatal("stored credential record must never contain the raw credential")
	}
	if !bytes.Contains(rec.Data, []byte(hash)) {
		t.Fatal("stored credential record must contain the verification hash")
	}
	if !bytes.Contains(rec.Data, []byte(string(AuthMethodPassword))) {
		t.Fatal("stored credential record must contain the auth method")
	}

	// Reopen the same directory: the verifier is hydrated.
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	auth2, err := OpenLocalAuthenticator(st2)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	res, err := auth2.Authenticate(id, raw)
	if err != nil || !res.Authenticated {
		t.Fatalf("credential must authenticate after a restart: res=%+v err=%v", res, err)
	}
	res.Method = AuthMethodPassword
	bad, _ := auth2.Authenticate(id, []byte("wrong"))
	if bad.Authenticated {
		t.Error("wrong credential must not authenticate")
	}
	unknown, _ := auth2.Authenticate("nx:human:nobody", raw)
	if unknown.Authenticated {
		t.Error("an identity with no stored hash must not authenticate")
	}
}

// TEST-IDR-13: credential hydration fails closed (F4) — a record that cannot
// be decoded, that disagrees with its record id, that is empty, or that
// carries a non-canonical method aborts the open instead of serving an
// authenticator that is only partly restored.
func TestAuthenticatorCredentialHydrationFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"corrupt json", `{"identity_id":`},
		{"identity mismatch", `{"schema_version":"1.0.0","entity_type":"credential","identity_id":"nx:human:other","hash":"h","method":"password"}`},
		{"empty hash", `{"schema_version":"1.0.0","entity_type":"credential","identity_id":"nx:human:bad","hash":"","method":"password"}`},
		{"unknown method", `{"schema_version":"1.0.0","entity_type":"credential","identity_id":"nx:human:bad","hash":"h","method":"telepathy"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := store.NewMemStore()
			if err := st.Put(&store.Record{
				ID:     credentialRecordID("nx:human:bad"),
				Type:   store.RecordTypeCredential,
				Status: store.RecordStatusActive,
				Data:   []byte(tt.data),
			}); err != nil {
				t.Fatalf("seed record: %v", err)
			}
			if _, err := OpenLocalAuthenticator(st); err == nil {
				t.Fatal("expected OpenLocalAuthenticator to fail closed on a bad record")
			}
		})
	}
}

// TEST-IDR-14: Register rejects a non-canonical method at the write, so an
// unknown method can never be persisted and then fail the next boot instead.
func TestRegisterRejectsUnknownMethod(t *testing.T) {
	st := store.NewMemStore()
	auth, err := OpenLocalAuthenticator(st)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	err = auth.Register("nx:human:x", security.HashCredential([]byte("c")), AuthMethod("telepathy"))
	if err == nil {
		t.Fatal("expected an unknown method to be rejected")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Errorf("expected a VALIDATION error, got %v", err)
	}
	recs, listErr := st.List(store.Filter{Type: store.RecordTypeCredential})
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if len(recs) != 0 {
		t.Errorf("a rejected registration must not write a record, got %d", len(recs))
	}
}

// TEST-IDR-15: a store failure rejects the registration and leaves the
// in-memory authenticator untouched (persist → memory, never the reverse).
func TestAuthenticatorStoreFailureFailsClosed(t *testing.T) {
	fs := &failStore{Store: store.NewMemStore()}
	auth, err := OpenLocalAuthenticator(fs)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	fs.failPut = true
	if err := auth.Register("nx:human:x", security.HashCredential([]byte("c")), AuthMethodToken); err == nil {
		t.Fatal("expected registration to fail when the store rejects the write")
	}
	fs.failPut = false
	if res, _ := auth.Authenticate("nx:human:x", []byte("c")); res.Authenticated {
		t.Error("authenticator must not hold a credential that was never persisted")
	}
}

// TEST-IDR-16: membership write-through — memberships added through
// OpenMembershipSet are restored by the next open, so "who belongs where"
// survives a restart alongside the identity records themselves.
func TestMembershipPersistence(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ms, err := OpenMembershipSet(st)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	seed := []Membership{
		{IdentityID: "nx:human:u", BusinessID: "default", Role: RoleAdmin, Status: StatusActive},
		{IdentityID: "nx:human:u", BusinessID: "acme", DivisionID: "nx:div:eng", Role: RoleWorker, Status: StatusActive},
	}
	for _, m := range seed {
		if err := ms.Add(m); err != nil {
			t.Fatalf("add %v: %v", m, err)
		}
	}
	if !ms.IsMember("nx:human:u", "default", "") {
		t.Fatal("membership must be visible before the restart")
	}

	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	ms2, err := OpenMembershipSet(st2)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if !ms2.IsMember("nx:human:u", "default", "") {
		t.Error("business membership must survive a restart")
	}
	if !ms2.IsMember("nx:human:u", "acme", "nx:div:eng") {
		t.Error("division membership must survive a restart")
	}
	if ms2.IsMember("nx:human:u", "acme", "nx:div:other") {
		t.Error("a division-scoped membership must not cover another division")
	}
	if ms2.IsMember("nx:human:u", "nowhere", "") {
		t.Error("an unknown business must never be a member")
	}
	got := ms2.For("nx:human:u")
	if len(got) != 2 {
		t.Errorf("restored memberships: got %d, want 2 (%+v)", len(got), got)
	}
	// Re-adding after hydration is still idempotent (bootstrap's second boot).
	if err := ms2.Add(seed[0]); err != nil {
		t.Fatalf("idempotent re-add: %v", err)
	}
	if len(ms2.For("nx:human:u")) != 2 {
		t.Error("re-adding a hydrated membership must not duplicate it")
	}
}

// TEST-IDR-17: membership hydration fails closed (F4) for the same four
// classes of bad record the credential path rejects.
func TestMembershipHydrationFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"corrupt json", `{"identity_id":`},
		{"identity mismatch", `{"schema_version":"1.0.0","entity_type":"membership","identity_id":"nx:human:other","memberships":[{"identity_id":"nx:human:other","business_id":"default","role":"MEMBER","status":"active"}]}`},
		{"membership for another identity", `{"schema_version":"1.0.0","entity_type":"membership","identity_id":"nx:human:bad","memberships":[{"identity_id":"nx:human:someone-else","business_id":"default","role":"MEMBER","status":"active"}]}`},
		{"invalid membership", `{"schema_version":"1.0.0","entity_type":"membership","identity_id":"nx:human:bad","memberships":[{"identity_id":"nx:human:bad","business_id":"","role":"MEMBER","status":"active"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := store.NewMemStore()
			if err := st.Put(&store.Record{
				ID:     membershipRecordID("nx:human:bad"),
				Type:   store.RecordTypeMembership,
				Status: store.RecordStatusActive,
				Data:   []byte(tt.data),
			}); err != nil {
				t.Fatalf("seed record: %v", err)
			}
			if _, err := OpenMembershipSet(st); err == nil {
				t.Fatal("expected OpenMembershipSet to fail closed on a bad record")
			}
		})
	}
}

// TEST-IDR-18: a store failure rejects Add and leaves the set untouched.
func TestMembershipStoreFailureFailsClosed(t *testing.T) {
	fs := &failStore{Store: store.NewMemStore()}
	ms, err := OpenMembershipSet(fs)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	fs.failPut = true
	if err := ms.Add(Membership{
		IdentityID: "nx:human:u",
		BusinessID: "default",
		Role:       RoleMember,
		Status:     StatusActive,
	}); err == nil {
		t.Fatal("expected Add to fail when the store rejects the write")
	}
	if ms.IsMember("nx:human:u", "default", "") {
		t.Error("membership set must not hold a membership that was never persisted")
	}
}
