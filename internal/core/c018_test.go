package core

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TEST-CORE-043 (C-018): SubmitRequest's admission path must not race with
// Stop()/Resume() lifecycle writes. e.status and e.shutdownCh are written
// under e.mu by Stop (engine.go:267) and Resume (engine.go:282/287), but
// SubmitRequest read e.status after e.mu.RUnlock() (engine.go:301) and read
// e.shutdownCh with no lock at all in its select (engine.go:318). This test
// exercises concurrent submits against Stop/Resume churn; under `go test -race`
// it reports the data race on the pre-fix code.
func TestC018SubmitAdmissionLifecycleRace(t *testing.T) {
	e, _ := NewEngine(nil)
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var subWg sync.WaitGroup
	for i := 0; i < 8; i++ {
		subWg.Add(1)
		go func(i int) {
			defer subWg.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				req := &Request{
					ID:      fmt.Sprintf("c018-race-%d-%d", i, j),
					Context: NewRequestContext(fmt.Sprintf("c018-rc-%d-%d", i, j), "biz-1", "user-1"),
					Intent:  "c018 lifecycle race",
				}
				// Rejections during stop windows are expected; the race is
				// on the shared fields, not on the returned error.
				_ = e.SubmitRequest(req)
			}
		}(i)
	}

	for k := 0; k < 100; k++ {
		if err := e.Stop(ctx); err != nil {
			t.Errorf("stop %d: %v", k, err)
		}
		if err := e.Resume(ctx); err != nil {
			t.Errorf("resume %d: %v", k, err)
		}
	}
	close(stop)
	subWg.Wait()
	if err := e.Stop(ctx); err != nil {
		t.Errorf("final stop: %v", err)
	}
}

// TEST-CORE-044 (C-018): Backpressure count must balance — exactly one
// Release per Accept, no leak across Stop/Resume churn. After the final
// Resume drains every queued request, QueueSize() must return to 0.
func TestC018BackpressureCountBalance(t *testing.T) {
	e, _ := NewEngine(nil)
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}

	var subWg sync.WaitGroup
	for i := 0; i < 4; i++ {
		subWg.Add(1)
		go func(i int) {
			defer subWg.Done()
			for j := 0; j < 200; j++ {
				req := &Request{
					ID:      fmt.Sprintf("c018-bal-%d-%d", i, j),
					Context: NewRequestContext(fmt.Sprintf("c018-bc-%d-%d", i, j), "biz-1", "user-1"),
					Intent:  "c018 count balance",
				}
				_ = e.SubmitRequest(req)
			}
		}(i)
	}
	subWg.Wait()

	// Churn stop/resume while the queue may still hold items: queued
	// requests must be drained (and their slots released) after Resume.
	for k := 0; k < 20; k++ {
		if err := e.Stop(ctx); err != nil {
			t.Fatalf("stop %d: %v", k, err)
		}
		if err := e.Resume(ctx); err != nil {
			t.Fatalf("resume %d: %v", k, err)
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for e.Backpressure().QueueSize() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("backpressure count leaked: QueueSize=%d", e.Backpressure().QueueSize())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := e.Stop(ctx); err != nil {
		t.Errorf("final stop: %v", err)
	}
}
