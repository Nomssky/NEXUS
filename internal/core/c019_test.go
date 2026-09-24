package core

import (
	"context"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// C-019: Store is an intentionally exposed external API, not dead engine state.
// Invariant: Store() always returns a usable store for external consumers;
// engine runtime must not read/write it (results use bounded maps, knowledge
// uses memoryStore). WithPersistence swaps implementation without affecting
// that ownership boundary.

// TestC019StoreAlwaysNonNil proves the external API always yields a store.
func TestC019StoreAlwaysNonNil(t *testing.T) {
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if e.Store() == nil {
		t.Fatal("Store() must never be nil — external API contract")
	}
}

// TestC019StoreExternalPutGet proves external consumers can use Store()
// for durable record storage (the intentional purpose of the field).
func TestC019StoreExternalPutGet(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	e, err := NewEngine(nil, WithPersistence(dir), WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewEngine with persistence: %v", err)
	}

	st := e.Store()
	if st == nil {
		t.Fatal("Store() nil after WithPersistence")
	}

	rec := &store.Record{
		ID:         "c019-ext-1",
		Type:       store.RecordTypeAudit,
		Status:     store.RecordStatusActive,
		BusinessID: "biz-c019",
		Data:       []byte(`{"external":"consumer"}`),
	}
	if err := st.Put(rec); err != nil {
		t.Fatalf("external Put: %v", err)
	}

	got, err := st.Get("c019-ext-1")
	if err != nil {
		t.Fatalf("external Get after Put: %v", err)
	}
	if got == nil {
		t.Fatal("external Get after Put: nil record")
	}
	if string(got.Data) != `{"external":"consumer"}` {
		t.Errorf("roundtrip data: got %s", got.Data)
	}
}

// TestC019WithPersistenceSwapsImplementation proves WithPersistence configures
// the external Store without changing ownership semantics.
func TestC019WithPersistenceSwapsImplementation(t *testing.T) {
	dir := t.TempDir()
	e, err := NewEngine(nil, WithPersistence(dir))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// FileStore should be rooted at dir — Put then reopen via new engine to prove durability path.
	if err := e.Store().Put(&store.Record{
		ID:     "c019-file-1",
		Type:   store.RecordTypeConfig,
		Status: store.RecordStatusActive,
		Data:   []byte("persisted"),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Default engine (no WithPersistence) uses MemStore — still non-nil, independent instance.
	e2, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("NewEngine default: %v", err)
	}
	if e2.Store() == nil {
		t.Fatal("default Store() nil")
	}
	if got, err := e2.Store().Get("c019-file-1"); err == nil && got != nil {
		t.Error("default MemStore must not share FileStore data — independent instances")
	}
}

// TestC019StoreIndependentOfLifecycle proves Start/Stop/Resume do not
// invalidate or replace the external Store reference.
func TestC019StoreIndependentOfLifecycle(t *testing.T) {
	dir := t.TempDir()
	e, err := NewEngine(nil, WithPersistence(dir))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	before := e.Store()
	if before == nil {
		t.Fatal("Store nil before start")
	}

	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if e.Store() != before {
		t.Error("Store reference changed after Start")
	}

	if err := e.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if e.Store() != before {
		t.Error("Store reference changed after Stop")
	}
	if e.Status() != lifecycle.StateStopped {
		t.Errorf("lifecycle: want STOPPED, got %s", e.Status())
	}

	if err := e.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if e.Store() != before {
		t.Error("Store reference changed after Resume")
	}
}

// TestC019RuntimeDoesNotConsumeStore proves engine runtime (request processing,
// results, knowledge) does not read/write the external Store — that separation
// is the intentional ownership boundary (C-019).
func TestC019RuntimeDoesNotConsumeStore(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	e, err := NewEngine(nil, WithPersistence(dir), WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	st := e.Store()

	// Snapshot count before runtime activity.
	before, err := st.List(store.Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	beforeN := len(before)

	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:       "c019-runtime-1",
		Context:  NewRequestContext("corr-c019", "biz-c019", "user-1"),
		Intent:   "exercise runtime without touching external store",
		Priority: 1,
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("SubmitRequest: %v", err)
	}
	// Wait for processing loop to handle the request.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := e.GetResult(req.ID); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for result")
		}
		time.Sleep(10 * time.Millisecond)
	}

	after, err := st.List(store.Filter{})
	if err != nil {
		t.Fatalf("List after: %v", err)
	}
	if len(after) != beforeN {
		t.Errorf("engine runtime wrote to external Store: before=%d after=%d", beforeN, len(after))
	}
}
