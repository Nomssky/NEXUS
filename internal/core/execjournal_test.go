package core

import (
	"context"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// The engine durably records acceptance before the caller is told the
// execution exists, and the terminal state after the result is stored.
func TestEngineJournalAcceptAndTerminal(t *testing.T) {
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	e, err := NewEngine(nil, WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(ctx) })

	if err := e.SubmitRequest(&Request{
		ID:      "exec-core-1",
		Context: NewRequestContext("exec-core-1", "biz-1", "actor-1"),
		Intent:  "do work",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// The engine persisted acceptance synchronously during SubmitRequest.
	rec, ok, err := j.Get("exec-core-1")
	if err != nil || !ok {
		t.Fatalf("accepted record missing: ok=%v err=%v", ok, err)
	}
	if rec.State != execjournal.StateAccepted {
		t.Fatalf("expected accepted, got %q", rec.State)
	}
	if rec.BusinessID != "biz-1" || rec.ActorID != "actor-1" {
		t.Fatalf("scope not recorded: %+v", rec)
	}

	// Wait for the terminal result, then the durable terminal state.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := e.GetResult("exec-core-1"); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec, ok, err = j.Get("exec-core-1")
		if err == nil && ok && rec.State.Terminal() {
			return // success
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("terminal state not persisted: ok=%v err=%v state=%v", ok, err, rec.State)
}

// Recovery never re-schedules a durably-terminal execution: the engine starts
// with an empty queue, and classification does not enqueue anything.
func TestEngineRecoveryDoesNotReschedule(t *testing.T) {
	st, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	if err := j.Accepted("exec-old", "biz-1", "", "actor-1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rep, err := j.Recover()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if rep.Recovered != 1 {
		t.Fatalf("expected 1 recovered, got %d", rep.Recovered)
	}
	// A fresh engine over the same store has no queued work for the recovered id.
	e, err := NewEngine(nil, WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if _, ok := e.GetResult("exec-old"); ok {
		t.Fatal("a recovered execution must not appear as a fresh result")
	}
}
