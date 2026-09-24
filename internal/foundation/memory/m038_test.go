package memory

import (
	"sync"
	"testing"
	"time"
)

// TEST-M8-022 (M-038): Retrieve() holds the exclusive write lock because it
// mutates shared entry state (AccessCount/LastAccessed). Concurrent retrieves
// must therefore serialize with no lost updates: N goroutines × M retrieves
// against a single entry must yield exactly N*M accesses. This is the
// invariant that makes the write lock load-bearing — under RLock the
// read-modify-write on AccessCount would race and lose updates.
func TestM038ConcurrentRetrieveNoLostUpdates(t *testing.T) {
	ms := NewMemoryStore()
	entry := &MemoryEntry{
		Type:       MemoryTypeSemantic,
		BusinessID: "biz-1",
		Content:    "content under concurrent read",
		Provenance: Provenance{Confidence: 1.0},
	}
	if err := ms.Admit(entry); err != nil {
		t.Fatalf("admit: %v", err)
	}

	const goroutines = 8
	const retrievesPerGoroutine = 500

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < retrievesPerGoroutine; i++ {
				ms.Retrieve(&MemoryQuery{BusinessID: "biz-1"})
			}
		}()
	}
	wg.Wait()

	// Get (RLock, no access tracking) reads the counter without mutating it.
	got, ok := ms.Get(entry.ID)
	if !ok {
		t.Fatal("expected entry to exist")
	}
	want := goroutines * retrievesPerGoroutine
	if got.AccessCount != want {
		t.Errorf("lost updates under concurrent retrieve: AccessCount=%d, want %d", got.AccessCount, want)
	}
	if got.LastAccessed == nil {
		t.Error("expected LastAccessed to be set")
	}
}

// TEST-M8-023 (M-038): no deadlock or lifecycle hazard — every MemoryStore
// operation completes under concurrent mixed load. The store's critical
// sections hold only ms.mu across in-memory map work (no channels, no I/O,
// no callbacks), so completion is bounded; the timeout only catches a
// lockup regression rather than assuming any timing.
func TestM038ConcurrentMixedOperationsComplete(t *testing.T) {
	ms := NewMemoryStore()

	const seed = 20
	ids := make([]string, 0, seed)
	for i := 0; i < seed; i++ {
		e := &MemoryEntry{
			Type:       MemoryTypeWorking,
			BusinessID: "biz-1",
			Content:    "seed entry",
			Provenance: Provenance{Confidence: 1.0},
		}
		if err := ms.Admit(e); err != nil {
			t.Fatalf("seed admit %d: %v", i, err)
		}
		ids = append(ids, e.ID)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	launch := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	// Readers (the M-038 write-lock path).
	for g := 0; g < 4; g++ {
		launch(func() {
			for i := 0; i < 300; i++ {
				ms.Retrieve(&MemoryQuery{BusinessID: "biz-1", MaxResults: 5})
			}
		})
	}
	// Writers.
	launch(func() {
		for i := 0; i < 100; i++ {
			ms.Admit(&MemoryEntry{
				Type:       MemoryTypeEpisodic,
				BusinessID: "biz-1",
				Content:    "written under contention",
				Provenance: Provenance{Confidence: 1.0},
			})
		}
	})
	// Lifecycle mutators (Archive/Forget take the same lock).
	launch(func() {
		for i := 0; i < 100; i++ {
			ms.Archive(ids[i%seed])
			ms.Forget(ids[(i+7)%seed])
		}
	})
	// Plain readers.
	launch(func() {
		for i := 0; i < 300; i++ {
			ms.Get(ids[i%seed])
			ms.Count()
		}
	})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("memory store operations did not complete within 10s — lockup under mixed load")
	}

	// Post-conditions: store still coherent (entries are never deleted, only
	// status-changed; newly admitted entries keep Count positive).
	if got := ms.Count(); got <= 0 {
		t.Errorf("expected positive count after mixed load, got %d", got)
	}
	if _, ok := ms.Get(ids[0]); !ok {
		t.Error("expected seeded entry to remain retrievable by ID")
	}
}
