package gateway

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Rate limiting (Production Hardening v2, F4). This is a bounded, process-local
// token-bucket limiter that complements the existing queue-level backpressure;
// it is NOT a distributed quota. Multi-instance global quotas require an
// external/shared limiter, which is out of scope.
//
// Classes:
//   - "submit": expensive work-creation endpoints (a new objective/execution).
//   - "api":    every other scoped API request (reads, polling, cancellation,
//     approvals, memory, agents, tools). Kept generous and SEPARATE so that
//     exhausting the submit bucket can never block cancellation or other
//     safety-relevant operations.
//   - "ip":     every non-exempt request, keyed on the direct peer address.
//     Bounds unauthenticated flooding and control-plane traffic. It never uses
//     X-Forwarded-For/Forwarded: with no trusted-proxy contract the direct peer
//     is the only defensible source.

const (
	rateClassSubmit = "submit"
	rateClassAPI    = "api"
	rateClassIP     = "ip"

	// Conservative documented defaults (requests per minute).
	defaultSubmitPerMinute  = 60
	defaultRequestPerMinute = 600
	defaultPerIPPerMinute   = 6000

	// limiterMaxKeys bounds the number of live buckets so arbitrary client
	// identities cannot cause unbounded map growth.
	limiterMaxKeys = 50000
	// limiterIdleTTL is how long an unused bucket is retained before an
	// opportunistic sweep drops it.
	limiterIdleTTL = 10 * time.Minute
	// limiterSweepInterval bounds how often the full idle sweep runs.
	limiterSweepInterval = time.Minute
)

// RateLimitPolicy configures the process-local HTTP rate limiter. A zero
// PerMinute falls back to the conservative default; Enabled=false disables all
// limiting (used only when the launcher does not install a policy).
type RateLimitPolicy struct {
	Enabled          bool
	SubmitPerMinute  int
	RequestPerMinute int
	PerIPPerMinute   int
}

type classPolicy struct {
	capacity   float64 // burst, in tokens
	ratePerSec float64 // refill, in tokens per second
}

type tokenBucket struct {
	tokens     float64
	last       time.Time
	capacity   float64
	ratePerSec float64
}

// rateLimiter is a set of token buckets keyed by (class, identity). It is
// concurrency-safe and uses an injectable clock so tests are deterministic.
type rateLimiter struct {
	mu        sync.Mutex
	now       func() time.Time
	classes   map[string]classPolicy
	buckets   map[string]*tokenBucket
	order     []string // insertion order, for O(1) amortized FIFO eviction
	orderHead int      // index of the first live entry in order
	lastSweep time.Time
	maxKeys   int
	idleTTL   time.Duration
}

func newRateLimiter(now func() time.Time, policy RateLimitPolicy) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	submit := policy.SubmitPerMinute
	if submit <= 0 {
		submit = defaultSubmitPerMinute
	}
	request := policy.RequestPerMinute
	if request <= 0 {
		request = defaultRequestPerMinute
	}
	perIP := policy.PerIPPerMinute
	if perIP <= 0 {
		perIP = defaultPerIPPerMinute
	}
	mk := func(perMinute int) classPolicy {
		return classPolicy{capacity: float64(perMinute), ratePerSec: float64(perMinute) / 60.0}
	}
	return &rateLimiter{
		now:       now,
		classes:   map[string]classPolicy{rateClassSubmit: mk(submit), rateClassAPI: mk(request), rateClassIP: mk(perIP)},
		buckets:   map[string]*tokenBucket{},
		maxKeys:   limiterMaxKeys,
		idleTTL:   limiterIdleTTL,
		lastSweep: now(),
	}
}

// allow consumes one token for (class, identity). It returns ok=false with the
// accurate Retry-After duration when the bucket is empty. It never blocks.
func (rl *rateLimiter) allow(class, identity string) (bool, time.Duration) {
	pol, known := rl.classes[class]
	if !known {
		return true, 0
	}
	now := rl.now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	key := class + "\x00" + identity
	b, ok := rl.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: pol.capacity, last: now, capacity: pol.capacity, ratePerSec: pol.ratePerSec}
		rl.buckets[key] = b
		rl.order = append(rl.order, key)
		rl.evictLocked(now)
	} else {
		if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
			b.tokens = math.Min(b.capacity, b.tokens+elapsed*b.ratePerSec)
			b.last = now
		}
	}

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true, 0
	}
	need := (1.0 - b.tokens) / b.ratePerSec
	retry := time.Duration(math.Ceil(need)) * time.Second
	if retry < time.Second {
		retry = time.Second
	}
	return false, retry
}

// evictLocked keeps the bucket map bounded. The idle sweep runs at most once
// per interval (it is an optimization); memory is bounded by evicting
// least-recently-inserted buckets in insertion order (amortized O(1)). Callers
// must hold rl.mu.
func (rl *rateLimiter) evictLocked(now time.Time) {
	if now.Sub(rl.lastSweep) >= limiterSweepInterval {
		rl.sweepIdleLocked(now)
		rl.lastSweep = now
	}
	for len(rl.buckets) >= rl.maxKeys && rl.orderHead < len(rl.order) {
		k := rl.order[rl.orderHead]
		rl.orderHead++
		if _, live := rl.buckets[k]; live {
			delete(rl.buckets, k)
		}
	}
	rl.compactOrderLocked()
}

// compactOrderLocked drops consumed order entries so the slice stays bounded.
func (rl *rateLimiter) compactOrderLocked() {
	if rl.orderHead == 0 {
		return
	}
	if rl.orderHead >= 4096 || rl.orderHead*2 >= len(rl.order) {
		rl.order = append([]string(nil), rl.order[rl.orderHead:]...)
		rl.orderHead = 0
	}
}

func (rl *rateLimiter) sweepIdleLocked(now time.Time) {
	for k, b := range rl.buckets {
		if now.Sub(b.last) > rl.idleTTL {
			delete(rl.buckets, k)
		}
	}
}

// size reports the live bucket count (test/diagnostics only).
func (rl *rateLimiter) size() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.buckets)
}

// isRateLimitExempt lists the operational endpoints that deployment health
// monitoring depends on. They are never throttled.
func isRateLimitExempt(path string) bool {
	switch path {
	case "/health", "/ready", "/status":
		return true
	}
	return false
}

// directPeerIP returns the direct TCP peer address with the port stripped. It
// deliberately ignores X-Forwarded-For/Forwarded: without an explicit
// trusted-proxy contract those headers are caller-controlled.
func directPeerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// classifyRateClass maps a request to its actor-based class. Read-only
// methods (GET/HEAD/OPTIONS) return "" — high-frequency polling must never
// consume a write bucket and thereby throttle unrelated writes such as agent
// registration or cancellation. Reads remain bounded by the coarse per-IP
// class and the existing queue backpressure.
func classifyRateClass(r *http.Request) string {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return ""
	case http.MethodPost:
		switch r.URL.Path {
		case "/api/v1/requests", "/api/v1/executions", "/api/v1/intelligence/execute":
			return rateClassSubmit
		}
	}
	return rateClassAPI
}

// writeRateLimited answers with a 429 in the standard error envelope and an
// accurate Retry-After. No limiter key, credential or IP is disclosed.
func (s *Server) writeRateLimited(w http.ResponseWriter, r *http.Request, retry time.Duration) {
	secs := int(retry.Seconds())
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	s.writeError(w, r, http.StatusTooManyRequests, "RATE_LIMIT", "request rate limit exceeded")
}

// ipRateLimitMiddleware throttles every non-exempt request by direct peer
// address. It bounds unauthenticated flooding and control-plane traffic before
// authentication runs.
func (s *Server) ipRateLimitMiddleware(next http.Handler) http.Handler {
	if s.rateLimiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isRateLimitExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if ok, retry := s.rateLimiter.allow(rateClassIP, directPeerIP(r)); !ok {
			s.writeRateLimited(w, r, retry)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// actorRateLimitMiddleware throttles authenticated scoped requests by the
// validated identity bound by identityMiddleware. It never reads X-Actor-ID and
// only runs when a verified actor is present, so an unauthenticated client can
// never consume or poison an authenticated actor's bucket.
func (s *Server) actorRateLimitMiddleware(next http.Handler) http.Handler {
	if s.rateLimiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res, ok := actorFromContext(r.Context())
		if !ok || res.IdentityID == "" {
			next.ServeHTTP(w, r)
			return
		}
		class := classifyRateClass(r)
		if class == "" { // read-only method: no per-actor limit
			next.ServeHTTP(w, r)
			return
		}
		if ok, retry := s.rateLimiter.allow(class, res.IdentityID); !ok {
			s.writeRateLimited(w, r, retry)
			return
		}
		next.ServeHTTP(w, r)
	})
}
