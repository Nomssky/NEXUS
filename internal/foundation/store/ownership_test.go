package store

import (
	"testing"
	"time"
)

// ---- v33: store ownership / mutation isolation ---------------------------
//
// A record crossing the store boundary is a VALUE, not a loan. Record has two
// reference-typed fields — Data []byte and ExpiresAt *time.Time — so a shallow
// struct copy is NOT isolation: the returned record would still share the
// payload backing array and the expiry pointer with stored state.
//
// The contract these tests pin:
//   - Get and List hand back caller-owned snapshots; mutating one, or any
//     nested mutable field, must not change stored state;
//   - Put must not keep the caller's record: mutating the input afterwards must
//     not change stored state;
//   - stored Version, status and timestamps stay authoritative either way.

// ownedStores returns both backends with a deterministic clock.
func ownedStores(t *testing.T) map[string]Store {
	t.Helper()
	tick := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := func() time.Time {
		tick = tick.Add(time.Hour)
		return tick
	}
	fs, err := NewFileStoreWithClock(t.TempDir(), clk)
	if err != nil {
		t.Fatalf("open filestore: %v", err)
	}
	return map[string]Store{"MemStore": NewMemStoreWithClock(clk), "FileStore": fs}
}

func ownedRecord(id string) *Record {
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	return &Record{
		ID: id, Type: RecordTypeMemory, Status: RecordStatusActive,
		BusinessID: "b1", Data: []byte("payload"), ExpiresAt: &exp,
	}
}

// Get must return a snapshot: neither the struct fields nor the two
// reference-typed fields may alias stored state.
func TestGetReturnsCallerOwnedSnapshot(t *testing.T) {
	for name, st := range ownedStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := st.Put(ownedRecord("k")); err != nil {
				t.Fatal(err)
			}
			got, err := st.Get("k")
			if err != nil || got == nil {
				t.Fatalf("get: %v", err)
			}

			// Mutate everything a caller could reach.
			got.Status = RecordStatusDeleted
			got.Version = 999
			got.BusinessID = "hijacked"
			got.Data[0] = 'X'                // shared backing array?
			*got.ExpiresAt = time.Unix(0, 0) // shared pointer?

			again, err := st.Get("k")
			if err != nil || again == nil {
				t.Fatalf("re-get: %v", err)
			}
			if again.Status != RecordStatusActive {
				t.Errorf("status leaked out of Get on %s: %q", name, again.Status)
			}
			if again.Version != 1 {
				t.Errorf("version leaked out of Get on %s: %d", name, again.Version)
			}
			if again.BusinessID != "b1" {
				t.Errorf("business_id leaked out of Get on %s: %q", name, again.BusinessID)
			}
			if string(again.Data) != "payload" {
				t.Errorf("data backing array is shared on %s: %q", name, again.Data)
			}
			if again.ExpiresAt == nil || !again.ExpiresAt.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
				t.Errorf("expires_at pointer is shared on %s: %v", name, again.ExpiresAt)
			}
		})
	}
}

// List must return snapshots too, for every element.
func TestListReturnsCallerOwnedSnapshots(t *testing.T) {
	for name, st := range ownedStores(t) {
		t.Run(name, func(t *testing.T) {
			for _, id := range []string{"a", "b"} {
				if err := st.Put(ownedRecord(id)); err != nil {
					t.Fatal(err)
				}
			}
			recs, err := st.List(Filter{Type: RecordTypeMemory})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(recs) != 2 {
				t.Fatalf("expected 2 records, got %d", len(recs))
			}
			for _, r := range recs {
				// Status is deliberately NOT mutated here: flipping it to
				// deleted would hide the record from the default List, and the
				// re-list below would iterate nothing and pass vacuously.
				// Status isolation is covered by TestGetReturnsCallerOwnedSnapshot.
				r.Version = 999
				r.BusinessID = "hijacked"
				r.Data[0] = 'X'
				*r.ExpiresAt = time.Unix(0, 0)
			}
			fresh, err := st.List(Filter{Type: RecordTypeMemory})
			if err != nil {
				t.Fatal(err)
			}
			if len(fresh) != 2 {
				t.Fatalf("a caller must not be able to remove records from List on %s: got %d", name, len(fresh))
			}
			for _, r := range fresh {
				if r.Version != 1 {
					t.Errorf("List leaked version state on %s: %+v", name, r)
				}
				if r.BusinessID != "b1" {
					t.Errorf("List leaked business_id on %s: %q", name, r.BusinessID)
				}
				if string(r.Data) != "payload" {
					t.Errorf("List shared a data backing array on %s: %q", name, r.Data)
				}
				if !r.ExpiresAt.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
					t.Errorf("List shared an expires_at pointer on %s: %v", name, r.ExpiresAt)
				}
			}
		})
	}
}

// Put must take a copy: mutating the caller's record afterwards must not reach
// into stored state.
func TestPutDoesNotRetainTheCallersRecord(t *testing.T) {
	for name, st := range ownedStores(t) {
		t.Run(name, func(t *testing.T) {
			rec := ownedRecord("k")
			if err := st.Put(rec); err != nil {
				t.Fatal(err)
			}
			// The caller keeps using its own record after Put returns.
			rec.Status = RecordStatusDeleted
			rec.Version = 999
			rec.BusinessID = "hijacked"
			rec.Data[0] = 'X'
			*rec.ExpiresAt = time.Unix(0, 0)

			stored, err := st.Get("k")
			if err != nil || stored == nil {
				t.Fatalf("get: %v", err)
			}
			if stored.Status != RecordStatusActive || stored.Version != 1 {
				t.Errorf("Put retained the caller's record on %s: %+v", name, stored)
			}
			if stored.BusinessID != "b1" {
				t.Errorf("Put retained caller scalar state on %s: %q", name, stored.BusinessID)
			}
			if string(stored.Data) != "payload" {
				t.Errorf("Put shared the caller's data array on %s: %q", name, stored.Data)
			}
			if !stored.ExpiresAt.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
				t.Errorf("Put shared the caller's expires_at on %s: %v", name, stored.ExpiresAt)
			}
		})
	}
}

// Isolation must hold for the tombstone returned by Get too (v30 contract).
func TestOwnershipHoldsForTombstones(t *testing.T) {
	for name, st := range ownedStores(t) {
		t.Run(name, func(t *testing.T) {
			if err := st.Put(ownedRecord("k")); err != nil {
				t.Fatal(err)
			}
			if err := st.Delete("k"); err != nil {
				t.Fatal(err)
			}
			tomb, err := st.Get("k")
			if err != nil || tomb == nil || tomb.Status != RecordStatusDeleted {
				t.Fatalf("tombstone must be visible on %s: %v", name, err)
			}
			tomb.Version = 999
			tomb.Data[0] = 'X'
			again, err := st.Get("k")
			if err != nil || again == nil {
				t.Fatal(err)
			}
			if again.Version != 2 || string(again.Data) != "payload" {
				t.Errorf("tombstone leaked out of Get on %s: %+v", name, again)
			}
		})
	}
}

// Isolation must survive a reopen: what a caller mutated before the restart was
// never stored, so the reopened store must show the original.
func TestOwnershipSurvivesFileStoreReopen(t *testing.T) {
	dir := t.TempDir()
	tick := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := func() time.Time { tick = tick.Add(time.Hour); return tick }
	fs, err := NewFileStoreWithClock(dir, clk)
	if err != nil {
		t.Fatal(err)
	}
	rec := ownedRecord("k")
	if err := fs.Put(rec); err != nil {
		t.Fatal(err)
	}
	// Mutate through a Get handle before the restart.
	got, err := fs.Get("k")
	if err != nil {
		t.Fatal(err)
	}
	got.Data[0] = 'X'
	*got.ExpiresAt = time.Unix(0, 0)

	closeStore(t, fs)
	fs2, err := NewFileStoreWithClock(dir, clk)
	if err != nil {
		t.Fatal(err)
	}
	after, err := fs2.Get("k")
	if err != nil || after == nil {
		t.Fatal(err)
	}
	if string(after.Data) != "payload" {
		t.Errorf("an out-of-band mutation was persisted by FileStore: %q", after.Data)
	}
	if !after.ExpiresAt.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("an out-of-band expires_at mutation was persisted: %v", after.ExpiresAt)
	}
}
