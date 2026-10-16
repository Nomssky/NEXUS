package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// ---- fixture ---------------------------------------------------------------

type rlFixture struct {
	srv     *Server
	advance func(time.Duration)
	clock   func() time.Time
}

// newRLFixture builds an identity-enforced gateway with two registered actors
// (alice, bob) and an injected, controllable clock so rate-limit tests are
// deterministic (no real sleeps).
func newRLFixture(t *testing.T, policy RateLimitPolicy, cors []string) *rlFixture {
	t.Helper()
	reg := identity.NewRegistry("nx:nexus:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)
	members := identity.NewMembershipSet()

	engine, err := core.NewEngine(nil, core.WithIdentity(members, true, true))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	// Registry bootstrap order (mirrors the production launcher): a global
	// owner identity, then the business it owns, then business-scoped actors.
	if _, err := reg.CreateIdentity(identity.Identity{
		ID: "nx:human:owner", Type: identity.TypeHuman, DisplayName: "owner",
	}, "system"); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if _, err := reg.CreateBusiness(identity.Business{
		EntityID: "biz-1", BusinessID: "biz-1", Name: "biz-1", OwnerIdentityID: "nx:human:owner",
		Status: identity.BusinessActive,
	}, "system"); err != nil {
		t.Fatalf("create business: %v", err)
	}

	for _, id := range []string{"alice", "bob"} {
		if _, err := reg.CreateIdentity(identity.Identity{
			ID: id, Type: identity.TypeHuman, DisplayName: id, BusinessID: "biz-1",
		}, "system"); err != nil {
			t.Fatalf("create identity %s: %v", id, err)
		}
		if err := auth.Register(id, security.HashCredential([]byte(id+"-secret")), identity.AuthMethodToken); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}

	var cur = time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return cur }

	opts := []ServerOption{
		WithRegistry(reg), WithIdentity(auth, members),
		WithRequireAuthentication(true), WithEnforceBusinessScope(true),
		WithClock(clock), WithRateLimit(policy),
	}
	if cors != nil {
		opts = append(opts, WithCORS(CORSPolicy{AllowedOrigins: cors}))
	}
	return &rlFixture{
		srv:     NewServer(engine, ":0", opts...),
		clock:   clock,
		advance: func(d time.Duration) { cur = cur.Add(d) },
	}
}

func doReq(srv *Server, method, path, actor string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if actor != "" {
		req.Header.Set("X-Actor-ID", actor)
		req.Header.Set("X-Actor-Credential", actor+"-secret")
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func rlErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code     string `json:"code"`
			Category string `json:"category"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, w.Body.String())
	}
	return payload.Error.Code
}

// ---- limiter unit tests ----------------------------------------------------

func TestRateLimiterAllowsBelowLimitThenBlocks(t *testing.T) {
	var cur = time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(func() time.Time { return cur }, RateLimitPolicy{Enabled: true, SubmitPerMinute: 2})

	for i := 0; i < 2; i++ {
		if ok, _ := rl.allow(rateClassSubmit, "a"); !ok {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	ok, retry := rl.allow(rateClassSubmit, "a")
	if ok {
		t.Fatal("third request must be blocked at the configured limit")
	}
	if retry < time.Second {
		t.Fatalf("retry-after must be a positive second, got %s", retry)
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	cur := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(func() time.Time { return cur }, RateLimitPolicy{Enabled: true, SubmitPerMinute: 60})
	// Drain the burst (60 tokens).
	for i := 0; i < 60; i++ {
		if ok, _ := rl.allow(rateClassSubmit, "a"); !ok {
			t.Fatalf("burst %d should be allowed", i)
		}
	}
	if ok, _ := rl.allow(rateClassSubmit, "a"); ok {
		t.Fatal("bucket should be empty after the burst")
	}
	cur = cur.Add(time.Second) // 1 token refills at 60/min
	if ok, _ := rl.allow(rateClassSubmit, "a"); !ok {
		t.Fatal("one token should have refilled after one second")
	}
}

func TestRateLimiterDistinctActorsIndependent(t *testing.T) {
	cur := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(func() time.Time { return cur }, RateLimitPolicy{Enabled: true, SubmitPerMinute: 1})
	if ok, _ := rl.allow(rateClassSubmit, "alice"); !ok {
		t.Fatal("alice first request allowed")
	}
	if ok, _ := rl.allow(rateClassSubmit, "alice"); ok {
		t.Fatal("alice second request blocked")
	}
	if ok, _ := rl.allow(rateClassSubmit, "bob"); !ok {
		t.Fatal("bob must have an independent bucket")
	}
}

func TestRateLimiterSeparateClassesIndependent(t *testing.T) {
	cur := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(func() time.Time { return cur }, RateLimitPolicy{Enabled: true, SubmitPerMinute: 1, RequestPerMinute: 5})
	if ok, _ := rl.allow(rateClassSubmit, "a"); !ok {
		t.Fatal("submit allowed")
	}
	if ok, _ := rl.allow(rateClassSubmit, "a"); ok {
		t.Fatal("submit exhausted")
	}
	// The "api" class is a separate bucket: cancellation/polling still works.
	if ok, _ := rl.allow(rateClassAPI, "a"); !ok {
		t.Fatal("api class must not be blocked by an exhausted submit bucket")
	}
}

func TestRateLimiterStateIsBounded(t *testing.T) {
	cur := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(func() time.Time { return cur }, RateLimitPolicy{Enabled: true, SubmitPerMinute: 10})
	for i := 0; i < limiterMaxKeys*2; i++ {
		rl.allow(rateClassIP, strconv.Itoa(i))
	}
	if got := rl.size(); got > limiterMaxKeys {
		t.Fatalf("limiter state must stay bounded at %d, got %d", limiterMaxKeys, got)
	}
}

func TestRateLimiterIdleExpirySweeps(t *testing.T) {
	cur := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(func() time.Time { return cur }, RateLimitPolicy{Enabled: true, SubmitPerMinute: 10})
	rl.allow(rateClassIP, "1.2.3.4")
	if rl.size() != 1 {
		t.Fatalf("expected 1 bucket, got %d", rl.size())
	}
	// Advance past the idle TTL and the sweep interval, then touch a new key so
	// the opportunistic sweep runs.
	cur = cur.Add(limiterIdleTTL + limiterSweepInterval + time.Second)
	rl.allow(rateClassIP, "5.6.7.8")
	if rl.size() != 1 {
		t.Fatalf("the idle bucket should have been swept, size=%d", rl.size())
	}
}

func TestRateLimiterConcurrentIsRaceSafe(t *testing.T) {
	rl := newRateLimiter(time.Now, RateLimitPolicy{Enabled: true, SubmitPerMinute: 1000000})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				rl.allow(rateClassSubmit, strconv.Itoa(n%8))
			}
		}(i)
	}
	wg.Wait()
}

// ---- HTTP middleware tests -------------------------------------------------

func TestRateLimitSubmitReturns429WithEnvelope(t *testing.T) {
	f := newRLFixture(t, RateLimitPolicy{Enabled: true, SubmitPerMinute: 1, RequestPerMinute: 100, PerIPPerMinute: 1000}, nil)
	if w := doReq(f.srv, http.MethodPost, "/api/v1/intelligence/execute", "alice"); w.Code == http.StatusTooManyRequests {
		t.Fatal("first submit should not be throttled")
	}
	w := doReq(f.srv, http.MethodPost, "/api/v1/intelligence/execute", "alice")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second submit must be 429, got %d body=%s", w.Code, w.Body.String())
	}
	if code := rlErrorCode(t, w); code != "RATE_LIMIT" {
		t.Fatalf("429 envelope code must be RATE_LIMIT, got %q", code)
	}
	if ra := w.Header().Get("Retry-After"); ra == "" {
		t.Fatal("429 must carry a Retry-After header")
	} else if secs, err := strconv.Atoi(ra); err != nil || secs < 1 {
		t.Fatalf("Retry-After must be a positive integer, got %q", ra)
	}
}

func TestRateLimitActorsIndependentOverHTTP(t *testing.T) {
	f := newRLFixture(t, RateLimitPolicy{Enabled: true, SubmitPerMinute: 1, RequestPerMinute: 100, PerIPPerMinute: 1000}, nil)
	doReq(f.srv, http.MethodPost, "/api/v1/intelligence/execute", "alice") // exhaust alice
	if w := doReq(f.srv, http.MethodPost, "/api/v1/intelligence/execute", "alice"); w.Code != 429 {
		t.Fatalf("alice should be throttled, got %d", w.Code)
	}
	if w := doReq(f.srv, http.MethodPost, "/api/v1/intelligence/execute", "bob"); w.Code == 429 {
		t.Fatal("bob must not inherit alice's quota")
	}
}

// A credential-less request cannot reach the actor limiter, so it cannot
// consume or poison a real actor's bucket.
func TestRateLimitUnauthenticatedDoesNotConsumeActorBucket(t *testing.T) {
	f := newRLFixture(t, RateLimitPolicy{Enabled: true, SubmitPerMinute: 2, RequestPerMinute: 100, PerIPPerMinute: 1000}, nil)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/execute", strings.NewReader("{}"))
		w := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated request must be 401, got %d", w.Code)
		}
	}
	// alice's quota is untouched: her first two submits are allowed.
	if w := doReq(f.srv, http.MethodPost, "/api/v1/intelligence/execute", "alice"); w.Code == 429 {
		t.Fatal("alice's bucket must be untouched by unauthenticated traffic")
	}
	if w := doReq(f.srv, http.MethodPost, "/api/v1/intelligence/execute", "alice"); w.Code == 429 {
		t.Fatal("alice's second submit must still be allowed")
	}
}

// An invalid credential with a target actor's X-Actor-ID header must not select
// or consume that actor's bucket.
func TestRateLimitSpoofedActorHeaderRejected(t *testing.T) {
	f := newRLFixture(t, RateLimitPolicy{Enabled: true, SubmitPerMinute: 1, RequestPerMinute: 100, PerIPPerMinute: 1000}, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/execute", strings.NewReader("{}"))
	req.Header.Set("X-Actor-ID", "alice")
	req.Header.Set("X-Actor-Credential", "wrong-secret")
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("spoofed credential must be 401, got %d", w.Code)
	}
	// alice's real quota is intact.
	if w := doReq(f.srv, http.MethodPost, "/api/v1/intelligence/execute", "alice"); w.Code == 429 {
		t.Fatal("a spoofed header must not consume the real actor's quota")
	}
}

// Cancellation must not be blocked by an exhausted submit bucket: the classes
// are separate buckets.
func TestRateLimitCancellationSurvivesExhaustedSubmit(t *testing.T) {
	f := newRLFixture(t, RateLimitPolicy{Enabled: true, SubmitPerMinute: 1, RequestPerMinute: 100, PerIPPerMinute: 1000}, nil)
	doReq(f.srv, http.MethodPost, "/api/v1/intelligence/execute", "alice") // exhaust submit
	if w := doReq(f.srv, http.MethodPost, "/api/v1/intelligence/execute", "alice"); w.Code != 429 {
		t.Fatalf("submit should be throttled, got %d", w.Code)
	}
	// A cancellation is on the generous, separate "api" class.
	if w := doReq(f.srv, http.MethodPost, "/api/v1/intelligence/exec-x/cancel", "alice"); w.Code == 429 {
		t.Fatal("cancellation must not be blocked by an exhausted submit bucket")
	}
}

// High-frequency reads (polling) must never consume the write bucket and
// thereby throttle an unrelated write such as agent registration.
func TestRateLimitReadsDoNotStarveWrites(t *testing.T) {
	f := newRLFixture(t, RateLimitPolicy{Enabled: true, SubmitPerMinute: 100, RequestPerMinute: 3, PerIPPerMinute: 100000}, nil)
	// Many reads (polling) — they are not per-actor limited.
	for i := 0; i < 50; i++ {
		if w := doReq(f.srv, http.MethodGet, "/api/v1/intelligence/exec-x", "alice"); w.Code == 429 {
			t.Fatalf("read %d must not be throttled by the actor limiter", i)
		}
	}
	// A write (agent registration) still has its full bucket.
	if w := doReq(f.srv, http.MethodPost, "/api/v1/agents", "alice"); w.Code == 429 {
		t.Fatal("a write must not be starved by read traffic")
	}
}

// Health/readiness are exempt from IP throttling so deployment monitoring is
// never blocked.
func TestRateLimitHealthExempt(t *testing.T) {
	f := newRLFixture(t, RateLimitPolicy{Enabled: true, SubmitPerMinute: 100, RequestPerMinute: 100, PerIPPerMinute: 1}, nil)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("health check %d must succeed (exempt), got %d", i, w.Code)
		}
	}
}

// The per-IP throttle ignores X-Forwarded-For (no trusted-proxy contract): a
// caller cannot bypass IP throttling by spoofing the header.
func TestRateLimitIPThrottleIgnoresForwardedFor(t *testing.T) {
	f := newRLFixture(t, RateLimitPolicy{Enabled: true, SubmitPerMinute: 100, RequestPerMinute: 100, PerIPPerMinute: 1}, nil)

	first := httptest.NewRequest(http.MethodGet, "/api/v1/control/status", nil)
	first.RemoteAddr = "203.0.113.9:5555"
	first.Header.Set("X-Forwarded-For", "10.0.0.1")
	w1 := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w1, first)
	if w1.Code == http.StatusTooManyRequests {
		t.Fatal("first request from the peer should not be throttled")
	}

	second := httptest.NewRequest(http.MethodGet, "/api/v1/control/status", nil)
	second.RemoteAddr = "203.0.113.9:5555"           // same direct peer
	second.Header.Set("X-Forwarded-For", "10.0.0.2") // spoofed, must be ignored
	w2 := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w2, second)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("a spoofed X-Forwarded-For must not bypass IP throttling, got %d", w2.Code)
	}
}
