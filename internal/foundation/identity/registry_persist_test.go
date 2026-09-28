package identity

import (
	"errors"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// TEST-IDR-08: write-through persistence — entities created and transitioned
// through a registry backed by a FileStore are fully restored (including
// lifecycle statuses and the business membership list) by OpenRegistry on the
// next open of the same directory.
func TestRegistryPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	r, err := OpenRegistry(testNexus, st)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	owner := mustIdentity(t, TypeHuman, "Owner", GlobalScope())
	owner, err = r.CreateIdentity(owner, "test")
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	bizSeed, _ := NewBusiness(testNexus, "Acme", owner.ID)
	biz, err := r.CreateBusiness(bizSeed, "test")
	if err != nil {
		t.Fatalf("create business: %v", err)
	}
	divSeed, _ := NewDivision(testNexus, biz.EntityID, "Eng", owner.ID)
	div, err := r.CreateDivision(divSeed, "test")
	if err != nil {
		t.Fatalf("create division: %v", err)
	}

	// Lifecycle transitions must persist too, not just creates.
	if _, err := r.SetIdentityStatus(owner.ID, StatusSuspended, "test"); err != nil {
		t.Fatalf("suspend identity: %v", err)
	}
	if _, err := r.SetDivisionStatus(div.EntityID, DivisionSuspended, "test"); err != nil {
		t.Fatalf("suspend division: %v", err)
	}

	// Reopen the same directory with a fresh store and registry.
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	r2, err := OpenRegistry(testNexus, st2)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}

	gotIdent, ok := r2.GetIdentity(owner.ID)
	if !ok {
		t.Fatal("identity not restored after reopen")
	}
	if gotIdent.Status != StatusSuspended {
		t.Errorf("identity status: got %s, want suspended", gotIdent.Status)
	}
	gotBiz, ok := r2.GetBusiness(biz.EntityID)
	if !ok {
		t.Fatal("business not restored after reopen")
	}
	if len(gotBiz.Divisions) != 1 || gotBiz.Divisions[0] != div.EntityID {
		t.Errorf("business memberships not restored: %+v", gotBiz.Divisions)
	}
	gotDiv, ok := r2.GetDivision(div.EntityID)
	if !ok {
		t.Fatal("division not restored after reopen")
	}
	if gotDiv.Status != DivisionSuspended {
		t.Errorf("division status: got %s, want suspended", gotDiv.Status)
	}
}

// TEST-IDR-09: boot fails closed — a stored record that cannot be decoded,
// carries a mismatched id, or fails validation aborts OpenRegistry instead of
// serving a partially hydrated registry.
func TestOpenRegistryFailsClosedOnBadRecord(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"corrupt_json", "not-json{{{"},                                 // undecodable payload
		{"mismatched_id", `{"id":"nx:human:other"}`},                    // envelope id != payload id
		{"invalid_entity", `{"id":"nx:human:bad","schema_version":""}`}, // fails Validate
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			st, err := store.NewFileStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Put(&store.Record{
				ID:     "nx:human:bad",
				Type:   store.RecordTypeIdentity,
				Status: store.RecordStatusActive,
				Data:   []byte(tt.data),
			}); err != nil {
				t.Fatalf("seed record: %v", err)
			}
			if _, err := OpenRegistry(testNexus, st); err == nil {
				t.Fatal("expected OpenRegistry to fail closed on a bad record")
			}
		})
	}
}

// failStore is a store.Store whose Put can be switched to fail on demand.
type failStore struct {
	store.Store
	failPut bool
}

func (f *failStore) Put(rec *store.Record) error {
	if f.failPut {
		return errors.New("store unavailable")
	}
	return f.Store.Put(rec)
}

// TEST-IDR-10: a store failure rejects the mutation, leaves the in-memory
// registry untouched (no partial state), and publishes no audit event.
func TestRegistryStoreFailureFailsClosed(t *testing.T) {
	fs := &failStore{Store: store.NewMemStore()}
	r, err := OpenRegistry(testNexus, fs)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	pub := &recPublisher{}
	r.SetPublisher(pub)

	// Create fails at the persist step: nothing is inserted, nothing is
	// published.
	owner := mustIdentity(t, TypeHuman, "Owner", GlobalScope())
	fs.failPut = true
	if _, err := r.CreateIdentity(owner, "test"); err == nil {
		t.Fatal("expected create to fail when the store rejects the write")
	}
	if _, ok := r.GetIdentity(owner.ID); ok {
		t.Error("registry must not hold the identity after a store failure")
	}
	if pub.has(event.EventTypeIdentityCreated) {
		t.Error("no identity.created event may be published for a failed create")
	}

	// Status transitions behave the same: the old status stays in memory.
	fs.failPut = false
	created, err := r.CreateIdentity(owner, "test")
	if err != nil {
		t.Fatalf("create after store recovery: %v", err)
	}
	eventsAfterCreate := len(pub.events)
	fs.failPut = true
	if _, err := r.SetIdentityStatus(created.ID, StatusSuspended, "test"); err == nil {
		t.Fatal("expected status change to fail when the store rejects the write")
	}
	got, ok := r.GetIdentity(created.ID)
	if !ok || got.Status != StatusActive {
		t.Errorf("identity status must remain active after failed transition, got %+v", got)
	}
	if len(pub.events) != eventsAfterCreate {
		t.Error("no status_changed event may be published for a failed transition")
	}
}

// TEST-IDR-11: RemoveIdentity also removes the persisted record (best effort
// soft delete) — a reopened registry no longer sees the identity.
func TestRegistryRemoveIdentityPersists(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := OpenRegistry(testNexus, st)
	if err != nil {
		t.Fatal(err)
	}
	owner := mustIdentity(t, TypeHuman, "Owner", GlobalScope())
	created, err := r.CreateIdentity(owner, "test")
	if err != nil {
		t.Fatal(err)
	}

	r.RemoveIdentity(created.ID)
	if _, ok := r.GetIdentity(created.ID); ok {
		t.Fatal("identity must be gone from memory")
	}

	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := OpenRegistry(testNexus, st2)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r2.GetIdentity(created.ID); ok {
		t.Error("removed identity must not be rehydrated from the store")
	}
}
