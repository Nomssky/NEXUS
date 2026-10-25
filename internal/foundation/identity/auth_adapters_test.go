package identity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// runCredentialRepositoryContract is the shared behavioural contract every
// CredentialRepository adapter must satisfy. Adding an adapter means adding one
// call here, not duplicating the tests.
func runCredentialRepositoryContract(t *testing.T, newRepo func(t *testing.T) CredentialRepository) {
	t.Helper()

	t.Run("empty", func(t *testing.T) {
		repo := newRepo(t)
		all, err := repo.LoadAll()
		if err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		if len(all) != 0 {
			t.Fatalf("fresh repository must be empty, got %d", len(all))
		}
		if _, ok := repo.Find("nx:human:missing"); ok {
			t.Fatal("Find must report false for an absent credential")
		}
	})

	t.Run("save-and-find", func(t *testing.T) {
		repo := newRepo(t)
		want := Credential{IdentityID: "nx:human:a", Hash: "hash-a", Method: AuthMethodPassword}
		if err := repo.Save(want); err != nil {
			t.Fatalf("Save: %v", err)
		}
		got, ok := repo.Find(want.IdentityID)
		if !ok {
			t.Fatal("saved credential must be findable")
		}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("overwrite", func(t *testing.T) {
		repo := newRepo(t)
		id := "nx:human:overwrite"
		if err := repo.Save(Credential{IdentityID: id, Hash: "old", Method: AuthMethodPassword}); err != nil {
			t.Fatalf("Save old: %v", err)
		}
		if err := repo.Save(Credential{IdentityID: id, Hash: "new", Method: AuthMethodToken}); err != nil {
			t.Fatalf("Save new: %v", err)
		}
		got, ok := repo.Find(id)
		if !ok || got.Hash != "new" || got.Method != AuthMethodToken {
			t.Fatalf("overwrite lost: %+v ok=%v", got, ok)
		}
		all, err := repo.LoadAll()
		if err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("overwrite must replace, not append: got %d", len(all))
		}
	})

	t.Run("load-all-ordered", func(t *testing.T) {
		repo := newRepo(t)
		for _, id := range []string{"nx:human:c", "nx:human:a", "nx:human:b"} {
			if err := repo.Save(Credential{IdentityID: id, Hash: id, Method: AuthMethodPassword}); err != nil {
				t.Fatalf("Save %s: %v", id, err)
			}
		}
		all, err := repo.LoadAll()
		if err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
		if len(all) != 3 {
			t.Fatalf("got %d credentials, want 3", len(all))
		}
		for i, want := range []string{"nx:human:a", "nx:human:b", "nx:human:c"} {
			if all[i].IdentityID != want {
				t.Fatalf("LoadAll[%d] = %q, want %q", i, all[i].IdentityID, want)
			}
		}
	})
}

func TestMemoryCredentialRepositoryContract(t *testing.T) {
	runCredentialRepositoryContract(t, func(*testing.T) CredentialRepository {
		return newMemoryCredentialRepository()
	})
}

func TestStoreCredentialRepositoryContractMemStore(t *testing.T) {
	runCredentialRepositoryContract(t, func(*testing.T) CredentialRepository {
		repo, err := newStoreCredentialRepository(store.NewMemStore())
		if err != nil {
			t.Fatalf("new store repository: %v", err)
		}
		return repo
	})
}

func TestStoreCredentialRepositoryContractFileStore(t *testing.T) {
	runCredentialRepositoryContract(t, func(t *testing.T) CredentialRepository {
		st, err := store.NewFileStore(t.TempDir())
		if err != nil {
			t.Fatalf("file store: %v", err)
		}
		repo, err := newStoreCredentialRepository(st)
		if err != nil {
			t.Fatalf("new store repository: %v", err)
		}
		return repo
	})
}

// Hydration restores exactly what the store holds, including records written by
// a different process (here: a raw record seeded behind the repository).
func TestStoreCredentialRepositoryHydratesExistingRecords(t *testing.T) {
	st := store.NewMemStore()
	rec := credentialRecord{
		SchemaVersion: credentialSchemaVersion,
		EntityType:    entityTypeCredential,
		IdentityID:    "nx:human:seeded",
		Hash:          "seeded-hash",
		Method:        AuthMethodDevice,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := st.Put(&store.Record{
		ID:     credentialRecordID(rec.IdentityID),
		Type:   store.RecordTypeCredential,
		Status: store.RecordStatusActive,
		Data:   data,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo, err := newStoreCredentialRepository(st)
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	got, ok := repo.Find("nx:human:seeded")
	if !ok {
		t.Fatal("seeded credential must be hydrated")
	}
	if got.Hash != "seeded-hash" || got.Method != AuthMethodDevice {
		t.Fatalf("hydrated credential: %+v", got)
	}
}

func TestStoreCredentialRepositoryHydrationFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"corrupt json", `{"identity_id":`},
		{"identity mismatch", `{"schema_version":"1.0.0","entity_type":"credential","identity_id":"nx:human:other","hash":"h","method":"password"}`},
		{"empty hash", `{"schema_version":"1.0.0","entity_type":"credential","identity_id":"nx:human:bad","hash":"","method":"password"}`},
		{"unknown method", `{"schema_version":"1.0.0","entity_type":"credential","identity_id":"nx:human:bad","hash":"h","method":"telepathy"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := store.NewMemStore()
			if err := st.Put(&store.Record{
				ID:     credentialRecordID("nx:human:bad"),
				Type:   store.RecordTypeCredential,
				Status: store.RecordStatusActive,
				Data:   []byte(tc.data),
			}); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if _, err := newStoreCredentialRepository(st); err == nil {
				t.Fatal("hydration must fail closed on a malformed record")
			}
		})
	}
}

func TestRegistryDirectoryAdapter(t *testing.T) {
	r := NewRegistry(testNexus)
	ident := mustIdentity(t, TypeHuman, "Directory User", GlobalScope())
	if _, err := r.CreateIdentity(ident, "test"); err != nil {
		t.Fatalf("create identity: %v", err)
	}

	dir := registryDirectory{registry: r}
	if got, ok := dir.Lookup(ident.ID); !ok || got.ID != ident.ID {
		t.Fatalf("known identity: ok=%v got=%q", ok, got.ID)
	}
	if _, ok := dir.Lookup("nx:human:absent"); ok {
		t.Fatal("unknown identity must not be found")
	}
	// A nil registry adapter fails closed rather than panicking.
	if _, ok := (registryDirectory{}).Lookup(ident.ID); ok {
		t.Fatal("nil registry must fail closed")
	}
}

func TestSha256VerifierAdapter(t *testing.T) {
	v := sha256Verifier{}
	h := v.Hash([]byte("secret"))
	if h == "" {
		t.Fatal("hash must not be empty")
	}
	if h != v.Hash([]byte("secret")) {
		t.Fatal("hash must be deterministic")
	}
	if h == v.Hash([]byte("other")) {
		t.Fatal("different credentials must not collide in this sample")
	}
	if !v.Equal(h, v.Hash([]byte("secret"))) {
		t.Fatal("Equal must match equal hashes")
	}
	if v.Equal(h, v.Hash([]byte("other"))) {
		t.Fatal("Equal must reject different hashes")
	}
	if v.Equal("", "") != true {
		// security.ConstantTimeEqual is exact-equality; empty equals empty.
		t.Fatal("Equal must be exact equality")
	}
}

// Concurrent registrations of distinct identities must be race-free and all
// persisted. Run with -race.
func TestStoreCredentialRepositoryConcurrentSave(t *testing.T) {
	st := store.NewMemStore()
	repo, err := newStoreCredentialRepository(st)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	const n = 40
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "nx:human:concurrent-" + string(rune('A'+i%26)) + string(rune('0'+i/26))
			if err := repo.Save(Credential{IdentityID: id, Hash: "h-" + id, Method: AuthMethodToken}); err != nil {
				t.Errorf("save %s: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	all, err := repo.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != n {
		t.Fatalf("got %d credentials, want %d", len(all), n)
	}

	// Re-open the same store: every credential must hydrate.
	reopened, err := newStoreCredentialRepository(st)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	again, err := reopened.LoadAll()
	if err != nil {
		t.Fatalf("reopened LoadAll: %v", err)
	}
	if len(again) != n {
		t.Fatalf("rehydrated %d credentials, want %d", len(again), n)
	}
}

// The repository must never persist the raw credential, only the hash.
func TestStoreCredentialRepositoryPersistsHashOnly(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("file store: %v", err)
	}
	repo, err := newStoreCredentialRepository(st)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	const id = "nx:human:hash-only"
	if err := repo.Save(Credential{IdentityID: id, Hash: "verification-hash", Method: AuthMethodPassword}); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, string(store.RecordTypeCredential), credentialRecordID(id)+".json"))
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if string(raw) == "" {
		t.Fatal("record file must not be empty")
	}
	var envelope store.Record
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var stored credentialRecord
	if err := json.Unmarshal(envelope.Data, &stored); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if stored.Hash != "verification-hash" {
		t.Fatalf("stored hash: %q", stored.Hash)
	}
}
