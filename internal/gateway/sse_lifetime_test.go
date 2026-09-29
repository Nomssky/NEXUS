package gateway

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
)

// listenAndServe starts the gateway on a real listener, applying tweak before
// Start, and returns the bound address.
func listenAndServe(t *testing.T, engine *core.Engine, tweak func(*Server)) string {
	t.Helper()
	var (
		addrMu    sync.Mutex
		boundAddr string
	)
	opts := []ServerOption{WithListenFunc(func(network, addr string) (net.Listener, error) {
		ln, err := net.Listen(network, addr)
		if err == nil {
			addrMu.Lock()
			boundAddr = ln.Addr().String()
			addrMu.Unlock()
		}
		return ln, err
	})}
	srv := NewServer(engine, "127.0.0.1:0", opts...)
	if tweak != nil {
		tweak(srv)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Start(ctx) }()
	select {
	case <-srv.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("gateway did not become ready")
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		_ = srv.Stop(stopCtx)
	})
	addrMu.Lock()
	defer addrMu.Unlock()
	return boundAddr
}

// TEST-GW-SSE-TIMEOUT-01 (C-4): an SSE stream must outlive the server's
// WriteTimeout. http.Server applies WriteTimeout once, when request headers
// are read — without clearing it per stream, every connection died at the
// deadline (here: 400ms) and events published after that were never
// delivered.
func TestSSEOutlivesServerWriteTimeout(t *testing.T) {
	engine := g010Engine(t)
	addr := listenAndServe(t, engine, func(s *Server) {
		s.server.WriteTimeout = 400 * time.Millisecond
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+addr+"/events?business_id=biz-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status: %d", resp.StatusCode)
	}

	// Read frames in the background; capture the marker published later.
	got := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "deadline-marker") {
				got <- line
			}
		}
		close(got)
	}()

	// Well past the (cleared) per-request WriteTimeout.
	time.Sleep(700 * time.Millisecond)
	publishDispatch(t, engine, &event.Event{ID: "deadline-marker"})

	select {
	case line, ok := <-got:
		if !ok || line == "" {
			t.Fatal("stream closed before the post-deadline event arrived")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("C-4: event published after the server WriteTimeout was never delivered")
	}
}

// TEST-GW-SSE-RACE-01 (R-2): events dispatched while clients disconnect must
// not race the ResponseWriter (single-writer restructure). Run under -race:
// the pre-fix handler wrote from the shared dispatch goroutine while
// net/http finished the request on disconnect.
func TestSSEConcurrentDispatchAndDisconnect(t *testing.T) {
	engine := g010Engine(t)
	addr := listenAndServe(t, engine, nil)

	const (
		clients = 6
		burst   = 40
	)
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Dispatcher: continuous events through the production dispatch loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = engine.EventBus().Publish(&event.Event{
				ID:         "race-" + time.Now().Format("150405.000000000"),
				Type:       event.EventType("custom"),
				BusinessID: "biz-1",
				Source:     "race-test",
				Timestamp:  time.Now(),
				Data:       []byte(`{"race":true}`),
			})
			i++
			time.Sleep(time.Millisecond)
		}
	}()

	// Clients: connect, read a bit, disconnect — repeatedly.
	for c := 0; c < clients; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < burst; r++ {
				func() {
					rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
					defer rcancel()
					req, _ := http.NewRequestWithContext(rctx, http.MethodGet,
						"http://"+addr+"/events?business_id=biz-1", nil)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						return
					}
					_, _ = io.CopyN(io.Discard, resp.Body, 256)
					resp.Body.Close()
				}()
			}
		}()
	}

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()
}
