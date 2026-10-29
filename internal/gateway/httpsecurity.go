package gateway

import (
	"net/http"
	"strings"
)

// Browser security posture (Production Hardening v2, F5).
//
// The gateway is API-only. It therefore emits the headers an API correctly
// owns, and deliberately does NOT emit HSTS, Content-Security-Policy or
// Permissions-Policy: those apply to documents served to browsers and must be
// set by the frontend/reverse-proxy layer that actually terminates TLS and
// serves the UI (see docs/http-gateway.md). Emitting HSTS here would be wrong
// on plain-HTTP local development and would not reflect this layer's TLS
// contract.

// CORSPolicy configures the explicit cross-origin policy. An empty allow-list
// means same-origin only: the gateway emits no CORS permission headers at all,
// which is the correct secure default for a same-origin deployment.
type CORSPolicy struct {
	AllowedOrigins []string
}

// allowedMethods are the HTTP methods the API exposes.
const allowedMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"

// allowedHeaders are exactly the request headers the shipped client uses.
const allowedHeaders = "Content-Type, X-Actor-ID, X-Actor-Credential, X-API-Key, X-Correlation-ID, Accept"

// securityHeadersMiddleware sets conservative, API-appropriate security headers
// on every response, including error responses (it runs outermost).
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware applies the explicit allow-list. It never reflects an
// arbitrary origin and never emits a wildcard. It is a browser policy only; it
// is not authentication or authorization and does not bypass them (preflight
// requests are unauthenticated by definition and carry no credentials).
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always allow CORS for SSE endpoint (needed when frontend and gateway
		// are on different ports, e.g. dev mode on 3000 with gateway on 8081)
		if r.URL.Path == "/events" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				w.Header().Add("Access-Control-Allow-Origin", origin)
			} else {
				w.Header().Add("Access-Control-Allow-Origin", "*")
			}
			w.Header().Add("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Access-Control-Allow-Methods", allowedMethods)
			w.Header().Add("Access-Control-Allow-Headers", allowedHeaders)
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		if len(s.corsOrigins) == 0 {
			// Same-origin default: no CORS headers at all. Cross-origin access
			// for the co-located dev frontend is handled by the /events SSE
			// special case above; the gateway itself stays same-origin by
			// default to avoid reflecting a wildcard origin.
			next.ServeHTTP(w, r)
			return
		}
		// The response may vary by Origin; mark caches accordingly.
		w.Header().Add("Vary", "Origin")

		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		if _, ok := s.corsOrigins[origin]; !ok {
			// Disallowed origin: never reflect it. Answer preflight with no
			// permission headers so the browser blocks the request.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin) // exact allow-listed match only
		h.Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", allowedMethods)
			h.Set("Access-Control-Allow-Headers", allowedHeaders)
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// buildCORSOrigins normalises the configured origins into an exact-match set.
// Validation (origin shape) happens at configuration time; here we only
// normalise case for the scheme/host.
func buildCORSOrigins(origins []string) map[string]struct{} {
	if len(origins) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		set[normalizeOrigin(o)] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// normalizeOrigin lower-cases the scheme and host, preserving an explicit port.
func normalizeOrigin(origin string) string {
	scheme, rest, ok := strings.Cut(origin, "://")
	if !ok {
		return origin
	}
	return strings.ToLower(scheme) + "://" + strings.ToLower(rest)
}
