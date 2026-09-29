package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
	"github.com/Nomssky/NEXUS/internal/gateway"
)

// TEST-LAUNCH-C1: the startup deadline bounds the startup barrier only — the
// engine and gateway must keep serving after it expires. Regression for the
// C-1 defect where Run passed a 30s WithTimeout context into life.Start, so
// every hook inherited it as a lifetime context and the whole system silently
// stopped serving 30 seconds after boot while the health server kept
// answering 200.
func TestComponentsSurviveStartupDeadline(t *testing.T) {
	cfg := testConfig()
	var (
		addrMu    sync.Mutex
		boundAddr string
	)
	l := New(Options{
		Config:    cfg,
		Logger:    logging.New(logging.Options{}),
		Health:    health.NewServer(),
		Lifecycle: lifecycle.New(lifecycle.Options{}),
		Addr:      "127.0.0.1:0",
		GatewayOptions: []gateway.ServerOption{
			gateway.WithListenFunc(func(network, addr string) (net.Listener, error) {
				ln, err := net.Listen(network, addr)
				if err == nil {
					addrMu.Lock()
					boundAddr = ln.Addr().String()
					addrMu.Unlock()
				}
				return ln, err
			}),
		},
	})
	// Deadline far shorter than the test's observation window: if the
	// startup deadline leaks into component lifetime, the gateway dies
	// before the assertions below.
	l.startupTimeout = 200 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := make(chan lifecycle.ExitCode, 1)
	go func() { exit <- l.Run(ctx) }()

	// Wait for the gateway to serve.
	gwAddr := ""
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		addrMu.Lock()
		a := boundAddr
		addrMu.Unlock()
		if a != "" {
			if resp, err := http.Get("http://" + a + "/health"); err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					gwAddr = a
					break
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if gwAddr == "" {
		t.Fatal("gateway never became reachable")
	}

	// Observe well past the startup deadline.
	time.Sleep(500 * time.Millisecond)

	resp, err := http.Get("http://" + gwAddr + "/health")
	if err != nil {
		t.Fatalf("C-1: gateway stopped serving after startup deadline: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("C-1: health after startup deadline: got %d", resp.StatusCode)
	}

	// The engine processing loop must also still be alive: a submitted
	// request has to reach a terminal result.
	submitBody := []byte(`{"intent":"survive deadline","business_id":"biz-1","actor_id":"user-1"}`)
	sreq, _ := http.NewRequest(http.MethodPost, "http://"+gwAddr+"/api/v1/requests", bytes.NewReader(submitBody))
	sreq.Header.Set("Content-Type", "application/json")
	sresp, err := http.DefaultClient.Do(sreq)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	var submitted struct {
		RequestID string `json:"request_id"`
	}
	_ = json.NewDecoder(sresp.Body).Decode(&submitted)
	sresp.Body.Close()
	if sresp.StatusCode != http.StatusAccepted || submitted.RequestID == "" {
		t.Fatalf("submit: status=%d body=%+v", sresp.StatusCode, submitted)
	}

	var status string
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		gresp, err := http.Get(fmt.Sprintf("http://%s/api/v1/requests/%s?business_id=biz-1", gwAddr, submitted.RequestID))
		if err != nil {
			t.Fatalf("GET result: %v", err)
		}
		var payload struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(gresp.Body).Decode(&payload)
		gresp.Body.Close()
		status = payload.Status
		if status != "" && status != "pending" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "completed" {
		t.Fatalf("C-1: engine loop dead after startup deadline: terminal status=%q", status)
	}

	// Clean shutdown: cancelling the run context must end Run.
	cancel()
	select {
	case code := <-exit:
		if code != lifecycle.ExitOK {
			t.Fatalf("Run exit code: %v", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}
