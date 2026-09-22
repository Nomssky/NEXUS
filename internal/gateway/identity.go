// Package gateway — identity-bound authorization (A6).
//
// When security.require_authentication or security.enforce_business_scope is
// enabled, the gateway binds every scoped request to a verified actor and
// validates business scope against foundation/identity MembershipSet — not
// client-asserted query/body fields alone.
//
// Fail-closed rules:
//   - Enforcement on + missing/invalid credential → 401
//   - Enforcement on + no authenticator configured → 401
//   - Enforcement on + actor not an active member of business_id → 403
//   - Enforcement on + empty/missing membership store → 403
//   - Submit actor_id must equal the authenticated identity → 403
package gateway

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/Nomssky/NEXUS/internal/foundation/identity"
)

// requestActorKey stores the AuthResult for an authenticated request.
type requestActorKey struct{}

// WithIdentity wires the authenticator and membership set used for
// identity-bound authorization. Both are required when either security flag
// enables enforcement; missing components fail closed at request time.
func WithIdentity(auth identity.Authenticator, members *identity.MembershipSet) ServerOption {
	return func(s *Server) {
		s.authenticator = auth
		s.memberships = members
	}
}

// WithRequireAuthentication enables trusted actor authentication on scoped
// API paths (submit, result retrieval, SSE). Maps to security.require_authentication.
func WithRequireAuthentication(v bool) ServerOption {
	return func(s *Server) { s.requireAuth = v }
}

// WithEnforceBusinessScope enables membership-validated business scope on
// scoped API paths. Maps to security.enforce_business_scope.
func WithEnforceBusinessScope(v bool) ServerOption {
	return func(s *Server) { s.enforceBusinessScope = v }
}

// identityEnforced reports whether identity-bound authorization is active.
func (s *Server) identityEnforced() bool {
	return s.requireAuth || s.enforceBusinessScope
}

// isScopedAPIPath reports whether the path carries business-scoped data
// (submit, result retrieval, SSE) and must be identity-bound when enforcement
// is on. Health/status and control paths are excluded (control has API-key auth).
func isScopedAPIPath(path string) bool {
	if path == "/api/v1/requests" || strings.HasPrefix(path, "/api/v1/requests/") {
		return true
	}
	return path == "/events"
}

// identityMiddleware authenticates scoped requests when enforcement is on and
// stores the AuthResult on the request context for handlers.
func (s *Server) identityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.identityEnforced() || !isScopedAPIPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		if s.authenticator == nil {
			// Fail-closed: enforcement requested but no way to verify identity.
			s.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED",
				"authentication not configured")
			return
		}

		actorID, credential, ok := extractActorCredentials(r)
		if !ok {
			s.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED",
				"authentication required")
			return
		}

		res, err := s.authenticator.Authenticate(actorID, credential)
		if err != nil || !res.Authenticated {
			s.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED",
				"invalid credentials")
			return
		}

		ctx := context.WithValue(r.Context(), requestActorKey{}, res)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// actorFromContext returns the AuthResult stored by identityMiddleware.
func actorFromContext(ctx context.Context) (identity.AuthResult, bool) {
	res, ok := ctx.Value(requestActorKey{}).(identity.AuthResult)
	return res, ok
}

// extractActorCredentials pulls actor identity and credential from the request.
// Supported forms:
//   - X-Actor-ID + X-Actor-Credential headers
//   - Authorization: Basic base64(actorID:credential)
func extractActorCredentials(r *http.Request) (string, []byte, bool) {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Basic ") {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
		if err != nil {
			return "", nil, false
		}
		parts := strings.SplitN(string(raw), ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", nil, false
		}
		return parts[0], []byte(parts[1]), true
	}

	actorID := r.Header.Get("X-Actor-ID")
	cred := r.Header.Get("X-Actor-Credential")
	if actorID == "" || cred == "" {
		return "", nil, false
	}
	return actorID, []byte(cred), true
}

// authorizeMembership fails closed unless actorID is an active member of
// businessID. A nil membership store denies every request.
func (s *Server) authorizeMembership(actorID, businessID string) error {
	if actorID == "" || businessID == "" {
		return errNotMember
	}
	if s.memberships == nil {
		return errNotMember
	}
	if !s.memberships.IsMember(actorID, businessID, "") {
		return errNotMember
	}
	return nil
}

// requireActorMembership enforces identity-bound business scope on a scoped
// handler. Returns a write-and-stop signal: true means the response was written.
func (s *Server) requireActorMembership(w http.ResponseWriter, r *http.Request, businessID string) (identity.AuthResult, bool) {
	if !s.identityEnforced() {
		return identity.AuthResult{}, false
	}

	res, ok := actorFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return identity.AuthResult{}, true
	}

	if err := s.authorizeMembership(res.IdentityID, businessID); err != nil {
		s.writeError(w, http.StatusForbidden, "AUTHORIZATION",
			"access denied: actor is not a member of the requested business")
		return res, true
	}
	return res, false
}

// errNotMember is the internal fail-closed sentinel for membership denials.
// It is never returned to clients; handlers map it to 403 AUTHORIZATION.
type notMemberError struct{}

func (notMemberError) Error() string { return "identity: not a member" }

var errNotMember error = notMemberError{}
