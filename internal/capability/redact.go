package capability

// Secret redaction (contracts/CAPABILITY_TOOL_CONTRACTS.md §7, §11) and JSON
// encoding helper. Redaction is runtime-side and mandatory: it runs on every
// adapter result, header, metadata value and error message before those can
// reach a model prompt, observation, event, log, HTTP response or durable
// memory.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// sensitiveHeaders are never returned verbatim.
var sensitiveHeaders = map[string]struct{}{
	"authorization": {}, "proxy-authorization": {}, "cookie": {}, "set-cookie": {},
	"x-api-key": {}, "api-key": {}, "x-auth-token": {}, "x-access-token": {},
	"x-session-token": {}, "x-nxs-credential": {},
}

func isSensitiveHeader(name string) bool {
	_, ok := sensitiveHeaders[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// Pattern-based redactions for secret-shaped values that do not come from the
// registered secret list (bearer tokens, api keys, private keys …).
var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._\-+/=]{8,}`),
	regexp.MustCompile(`(?i)\b(sk|pk|ghp|gho|ghu|ghs|glpat|xox[baprs])[-_][A-Za-z0-9._\-]{8,}`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|client[_-]?secret|password|passwd|secret|token)\b\s*[:=]\s*"?[^\s",}]{4,}"?`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
}

const redactedMarker = "[redacted]"

// redact removes registered secrets and secret-shaped patterns from s.
func (p *Platform) redact(s string) string { return p.redactString(s) }

// Redactor exposes the platform's mandatory redaction to other boundaries that
// must not become a place where a secret survives (AGENT_MEMORY_CONTEXT_CONTRACTS
// §16/§31 of the Capability contract: "before durable memory"). It is the same
// implementation, with the same registered secrets — never a second redactor.
func (p *Platform) Redactor(s string) string { return p.redactString(s) }

func (p *Platform) redactString(s string) string {
	if s == "" {
		return s
	}
	out := s
	for _, secret := range p.secretsSnapshot() {
		if secret != "" && strings.Contains(out, secret) {
			out = strings.ReplaceAll(out, secret, redactedMarker)
		}
	}
	for _, re := range redactionPatterns {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			// keep the label, drop the value
			if i := strings.IndexAny(m, ":="); i > 0 && strings.ContainsAny(m[:i], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
				return m[:i+1] + " " + redactedMarker
			}
			return redactedMarker
		})
	}
	return out
}

func (p *Platform) secretsSnapshot() []string {
	p.secretMu.Lock()
	defer p.secretMu.Unlock()
	return append([]string(nil), p.secrets...)
}

func encodeJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

// CredentialResolver implementation over the existing security.Resolver
// (contracts/CAPABILITY_TOOL_CONTRACTS.md §6). It returns an OPAQUE handle: the
// raw secret is reachable only by the adapter, never by the intelligence layer
// or a model, and never serialized.

// ScopedCredentialResolver resolves named, business/division-scoped credentials
// through the repository's existing secret resolver.
type ScopedCredentialResolver struct {
	Resolver security.Resolver
	// Denied holds references that must fail closed (e.g. revoked).
	mu     sync.RWMutex
	denied map[string]bool
}

// NewScopedCredentialResolver wraps an existing security.Resolver.
func NewScopedCredentialResolver(r security.Resolver) *ScopedCredentialResolver {
	return &ScopedCredentialResolver{Resolver: r, denied: map[string]bool{}}
}

// Deny marks a reference unavailable (lifecycle validation), fail closed.
func (s *ScopedCredentialResolver) Deny(reference string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.denied[reference] = true
}

// Put stores a secret value under a named reference (operator-provided
// configuration only). The value is copied and never leaves this resolver.
func (s *ScopedCredentialResolver) Put(reference, businessID, divisionID string, value []byte) error {
	dev, ok := s.Resolver.(*security.DevResolver)
	if !ok {
		return fmt.Errorf("%w: credential store is not writable", ErrCredential)
	}
	if err := dev.Put(scopedRef(reference, businessID, divisionID), value); err != nil {
		return fmt.Errorf("%w: credential store rejected the reference", ErrCredential)
	}
	s.mu.Lock()
	delete(s.denied, reference)
	s.mu.Unlock()
	return nil
}

// scopedRef builds the canonical reference string used for BOTH Put and
// Resolve, so an operator-provided secret is always looked up under the same
// key it was stored with. Scope is enforced by security.Resolver at resolve
// time.
func scopedRef(reference, businessID, divisionID string) security.SecretRef {
	return security.SecretRef{
		Store: "dev", Name: reference, BusinessID: businessID, DivisionID: divisionID,
		Purpose: "capability:" + reference,
	}
}

// Resolve implements CredentialResolver. Scope is enforced before any adapter
// runs: a credential for another business or division is unavailable here.
func (s *ScopedCredentialResolver) Resolve(_ context.Context, req tool.CredentialRequirement, businessID, divisionID string) (tool.CredentialHandle, error) {
	s.mu.RLock()
	denied := s.denied[req.Reference]
	s.mu.RUnlock()
	if denied {
		return tool.CredentialHandle{}, fmt.Errorf("%w: credential %q is not available", ErrCredential, req.Reference)
	}
	if s.Resolver == nil {
		return tool.CredentialHandle{}, fmt.Errorf("%w: no secret resolver configured", ErrCredential)
	}
	ref := scopedRef(req.Reference, req.BusinessID, req.DivisionID)
	secret, err := s.Resolver.Resolve(ref, businessID, divisionID)
	if err != nil {
		return tool.CredentialHandle{}, fmt.Errorf("%w: credential %q unavailable", ErrCredential, req.Reference)
	}
	return tool.NewCredentialHandle(req.Reference, func() (string, error) {
		return string(secret.Value()), nil
	}), nil
}
