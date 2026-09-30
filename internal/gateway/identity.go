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
//   - Enforcement off + client actor_id → untrusted; fixed marker bound (G-009)
package gateway

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/Nomssky/NEXUS/internal/foundation/identity"
)

// unauthenticatedActorID is the fixed identity bound to gateway requests when
// identity enforcement is off (G-009). Client-asserted actor_id is never
// trusted as a verified identity without authentication.
const unauthenticatedActorID = "unauthenticated"

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

// submitActorID resolves the actor identity bound to a submit request (G-009).
//
// When enforcement is on, handleSubmitRequest has already verified the
// client-supplied actor_id against the authenticated AuthResult (A6); that
// verified value is bound. When enforcement is off there is no trusted
// identity (identity middleware is inert, AuthResult is never set), so the
// claimed value is discarded and a fixed marker is bound — unauthenticated
// callers cannot spoof governance Actor, objective Owner, or executor
// attribution.
func (s *Server) submitActorID(claimedActorID string) string {
	if s.identityEnforced() {
		return claimedActorID
	}
	return unauthenticatedActorID
}

// isScopedAPIPath reports whether the path carries business-scoped data
// (submit, result retrieval, SSE, approvals) and must be identity-bound when
// enforcement is on. Health/status and control paths are excluded (control
// has API-key auth).
func isScopedAPIPath(path string) bool {
	if path == "/api/v1/requests" || strings.HasPrefix(path, "/api/v1/requests/") {
		return true
	}
	if path == "/events" {
		return true
	}
	// Approval records and decisions are business-scoped governance data.
	if path == "/api/v1/approvals" || strings.HasPrefix(path, "/api/v1/approvals/") {
		return true
	}
	// Escalation alerts and human responses (CTR-ATT) are likewise scoped.
	if path == "/api/v1/escalations" || strings.HasPrefix(path, "/api/v1/escalations/") {
		return true
	}
	// Organization entity records (identity/business/division, §2–§4).
	if path == "/api/v1/identities" || strings.HasPrefix(path, "/api/v1/identities/") {
		return true
	}
	if path == "/api/v1/businesses" || strings.HasPrefix(path, "/api/v1/businesses/") {
		return true
	}
	return path == "/api/v1/divisions" || strings.HasPrefix(path, "/api/v1/divisions/")
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
			s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED",
				"authentication not configured")
			return
		}

		candidates, ok := extractActorCredentials(r)
		if !ok {
			s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED",
				"authentication required")
			return
		}

		var res identity.AuthResult
		authenticated := false
		for _, cand := range candidates {
			out, err := s.authenticator.Authenticate(cand.identityID, []byte(cand.credential))
			if err == nil && out.Authenticated {
				res, authenticated = out, true
				break
			}
		}
		if !authenticated {
			// Uniform for every candidate: an unknown id, a wrong credential
			// and a missplit all answer the same way (F11).
			s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED",
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

// actorCredential is one candidate (identity, credential) pair decoded from a
// single credential presentation.
type actorCredential struct {
	identityID string
	credential string
}

// maxBasicAuthSplits bounds how many colon positions of an Authorization:
// Basic payload are turned into candidates. The correct split is always the
// last colon of the identity id, and ids are short ({nx}:{entity_type}:{unique},
// SCHEMA_COMMON §7), so 16 candidates covers every real id while keeping a
// header full of colons from turning into an authentication loop.
const maxBasicAuthSplits = 16

// extractActorCredentials pulls the actor identity and credential from the
// request. Supported forms:
//   - X-Actor-ID + X-Actor-Credential headers
//   - Authorization: Basic base64(actorID:credential)
//
// The Basic form is deliberately ambiguous: SCHEMA_COMMON §7 ids contain
// colons themselves, while credentials are opaque and may contain them too, so
// no single colon position is a safe separator. Every colon is therefore
// returned as a candidate and the caller authenticates each until one
// verifies. The candidate count is a property of the client's own payload, so
// it discloses nothing about the secret (F11), and a missplit simply fails to
// verify instead of rejecting a legitimate credential.
func extractActorCredentials(r *http.Request) ([]actorCredential, bool) {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Basic ") {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
		if err != nil {
			return nil, false
		}
		decoded := string(raw)
		candidates := make([]actorCredential, 0, 4)
		for i := 0; i < len(decoded) && len(candidates) < maxBasicAuthSplits; i++ {
			if decoded[i] != ':' {
				continue
			}
			id, credential := decoded[:i], decoded[i+1:]
			if id == "" || credential == "" {
				continue
			}
			candidates = append(candidates, actorCredential{identityID: id, credential: credential})
		}
		return candidates, len(candidates) > 0
	}

	actorID := r.Header.Get("X-Actor-ID")
	cred := r.Header.Get("X-Actor-Credential")
	if actorID == "" || cred == "" {
		return nil, false
	}
	return []actorCredential{{identityID: actorID, credential: cred}}, true
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
		s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return identity.AuthResult{}, true
	}

	if err := s.authorizeMembership(res.IdentityID, businessID); err != nil {
		s.writeError(w, r, http.StatusForbidden, "AUTHORIZATION",
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
