package launcher

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
)

// F4 — the launcher must pass the configured rate-limit policy into the
// gateway. With a per-IP limit of 1, the second request is throttled.
func TestLauncherWiresRateLimitFromConfig(t *testing.T) {
	l := New(Options{
		Config: config.Config{
			Nexus:     config.NexusConfig{Environment: "development"},
			RateLimit: config.RateLimitConfig{Enabled: true, SubmitPerMinute: 100, RequestPerMinute: 100, PerIPPerMinute: 1},
		},
		Logger:    logging.New(logging.Options{}),
		Health:    health.NewServer(),
		Lifecycle: lifecycle.New(lifecycle.Options{}),
		Addr:      ":0",
	})
	h := l.Gateway().Handler()

	first := httptest.NewRequest(http.MethodGet, "/api/v1/control/status", nil)
	first.RemoteAddr = "203.0.113.7:1234"
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, first)
	if w1.Code == http.StatusTooManyRequests {
		t.Fatal("first request must not be throttled")
	}

	second := httptest.NewRequest(http.MethodGet, "/api/v1/control/status", nil)
	second.RemoteAddr = "203.0.113.7:1235"
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, second)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request from the same peer must be 429, got %d", w2.Code)
	}
}

// F5 — the launcher must pass the configured CORS allow-list into the gateway.
func TestLauncherWiresCORSFromConfig(t *testing.T) {
	l := New(Options{
		Config: config.Config{
			Nexus: config.NexusConfig{Environment: "development"},
			HTTP:  config.HTTPConfig{CORSAllowedOrigins: []string{"https://app.example"}},
		},
		Logger:    logging.New(logging.Options{}),
		Health:    health.NewServer(),
		Lifecycle: lifecycle.New(lifecycle.Options{}),
		Addr:      ":0",
	})
	h := l.Gateway().Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://app.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("configured origin must be allowed, got ACAO=%q", got)
	}
}
