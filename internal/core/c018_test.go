package core

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
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

// gatedProvider completes immediately while no hold is engaged; once block()
// is called every invoke parks until unblock() or context cancellation.
type gatedProvider struct {
	mu      sync.Mutex
	holdCh  chan struct{}
	started chan struct{}
}

func newGatedProvider() *gatedProvider {
	return &gatedProvider{started: make(chan struct{}, 64)}
}

func (g *gatedProvider) Identify() string              { return "gated-provider" }
func (g *gatedProvider) HealthCheck() error            { return nil }
func (g *gatedProvider) ListModels() ([]string, error) { return nil, nil }

func (g *gatedProvider) Invoke(ctx context.Context, req *modelrouter.GenerateRequest) (*modelrouter.GenerateResponse, error) {
	g.mu.Lock()
	hold := g.holdCh
	g.mu.Unlock()
	select {
	case g.started <- struct{}{}:
	default:
	}
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &modelrouter.GenerateResponse{
		RequestID:    req.RequestID,
		ModelID:      req.ModelID,
		Content:      "ok",
		FinishReason: "stop",
	}, nil
}

func (g *gatedProvider) block() {
	g.mu.Lock()
	g.holdCh = make(chan struct{})
	g.mu.Unlock()
}

func (g *gatedProvider) unblock() {
	g.mu.Lock()
	if g.holdCh != nil {
		close(g.holdCh)
		g.holdCh = nil
	}
	g.mu.Unlock()
}

func registerGatedProvider(t *testing.T, e *Engine) *gatedProvider {
	t.Helper()
	p := newGatedProvider()
	if err := e.ModelRegistry().RegisterModel(&modelrouter.ModelDefinition{
		ID:         "gated-model",
		ProviderID: p.Identify(),
		Capabilities: []modelrouter.ModelCapability{
			modelrouter.CapabilityReasoning,
			modelrouter.CapabilityToolCalling,
		},
		Runtime: modelrouter.RuntimeLocal,
		Status:  modelrouter.ModelStatusActive,
	}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	e.ModelRouter().RegisterProvider(p)
	return p
}

// TEST-R4 (C-018): an approval resume re-admits the original request through
// the same queue SubmitRequest uses, so it must take an Accept slot — the
// dequeue path Releases once per Accept for every item it pops
// (engine.go:453). Resuming without Accept left a Release with no matching
// Accept, permanently under-counting QueueSize() and letting later submits
// pass the gate with a full channel (a blocking send where a rejection was
// owed).
func TestApprovalResumeCountsAgainstBackpressure(t *testing.T) {
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	p := registerGatedProvider(t, e)
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:       "require-approval-resume",
			Name:           "Require Approval",
			Status:         governance.PolicyStatusActive,
			Effect:         governance.REQUIRE_APPROVAL,
			Subject:        governance.Subject{SubjectType: "all"},
			Action:         governance.Action{ActionType: "custom"},
			Resource:       governance.Resource{ResourceType: "all"},
			ApprovalConfig: &governance.ApprovalConfig{TimeoutSeconds: 3600},
		},
	})

	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		p.unblock()
		_ = e.Stop(ctx)
	}()

	const gated = 3
	approvalIDs := make([]string, 0, gated)
	for i := 0; i < gated; i++ {
		id := fmt.Sprintf("bp-resume-%d", i)
		if err := e.SubmitRequest(&Request{
			ID:      id,
			Context: NewRequestContext("corr-"+id, "biz-1", "user-1"),
			Intent:  "gated work",
		}); err != nil {
			t.Fatalf("submit %s: %v", id, err)
		}
		result := waitForResult(t, e, id)
		if result.Error == nil || result.Error.Details["approval_id"] == "" {
			t.Fatalf("expected approval_id for %s, got %+v", id, result.Error)
		}
		approvalIDs = append(approvalIDs, result.Error.Details["approval_id"])
	}
	if got := e.Backpressure().QueueSize(); got != 0 {
		t.Fatalf("queue must drain before the probe, QueueSize()=%d", got)
	}

	// Hold the loop so the resumed requests stay parked in the queue and the
	// counter is observable instead of racing to zero.
	p.block()
	for _, approvalID := range approvalIDs {
		if err := e.ApproveRequest(approvalID, "biz-1", "user-2", "approved"); err != nil {
			t.Fatalf("approve %s: %v", approvalID, err)
		}
	}
	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first resume to reach the provider")
	}

	// One resume has been dequeued (its slot released); the other two are
	// still queued and must be counted.
	want := gated - 1
	if got := e.Backpressure().QueueSize(); got != want {
		t.Errorf("QueueSize() = %d, want %d — approval resume re-enqueued without an Accept slot (C-018: exactly one Release per Accept)",
			got, want)
	}

	p.unblock()
	deadline := time.Now().Add(10 * time.Second)
	for e.Backpressure().QueueSize() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("backpressure count leaked: QueueSize=%d", e.Backpressure().QueueSize())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
