// Package security implements the M1 security primitives foundation (component
// C04 Security & Threat Defense, layer L1, cross-cutting).
//
// Scope note: this is NOT the full Security & Threat Defense system
// (Core/NEXUS_SECURITY_THREAT_DEFENSE.md). M1 provides only the primitives later
// components depend on:
//
//   - secure, collision-resistant identifier generation (standard library)
//   - constant-time comparison for secret/credential material
//   - secret *references* (SecretRef) and a minimal resolution abstraction
//   - a development-only secret resolver, clearly labelled as such
//   - secret redaction for logs/errors/dumps
//   - a safe, deny-by-default egress allow-list
//
// Explicitly deferred (recorded, not implemented): SSRF/injection/replay/DLP
// engines, real sandboxing, production secret-store integrations, anomaly
// detection, automated containment, incident response, trust scoring.
//
// Invariants preserved here:
//   - SECRET_REF != SECRET_VALUE (a reference is not the secret)
//   - raw credentials are never embedded in context, logs, errors, or dumps
//   - security primitives grant NO authority (Security != Authority)
//   - a development resolver never masquerades as production secret management
package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// ---------- Secure identifier generation ----------

// EntityPrefix prefixes generated NEXUS identifiers, per
// contracts/SCHEMA_COMMON.md §7: {prefix}:{entity_type}:{unique_part}.
const EntityPrefix = "nx"

// NewID generates a cryptographically random, collision-resistant identifier of
// the form "nx:<entityType>:<hex>". It uses crypto/rand (standard library) and
// never returns a guessable, time-based identifier.
func NewID(entityType string) (string, error) {
	t := sanitizeIDPart(entityType)
	if t == "" {
		return "", nerrors.Validation("security.id_invalid_type", "entity type must be non-empty")
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", nerrors.Wrap(err, "security.id_entropy_unavailable",
			nerrors.CategoryInternalFailure, "secure random source unavailable")
	}
	return EntityPrefix + ":" + t + ":" + hex.EncodeToString(buf), nil
}

// MustNewID is NewID but returns an empty string on the (practically impossible)
// entropy failure. Prefer NewID at boundaries where the error is actionable.
func MustNewID(entityType string) string {
	id, err := NewID(entityType)
	if err != nil {
		return ""
	}
	return id
}

// sanitizeIDPart lowercases and strips characters that would break the ID format
// or enable injection into logs/IDs.
func sanitizeIDPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NewCorrelationID generates a correlation identifier. Correlation IDs are for
// tracing only and grant no authority (INV-19).
func NewCorrelationID() (string, error) { return NewID("corr") }

// ---------- Constant-time comparison ----------

// ConstantTimeEqual compares two strings in constant time relative to their
// content, suitable for comparing tokens, hashes, and credential material.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// HashCredential returns a hex-encoded SHA-256 digest of a credential.
//
// This is a BOUNDARY helper for comparing credentials without retaining the raw
// value. It is NOT a password KDF: callers that store human passwords must apply
// a memory-hard KDF upstream (that is a later-milestone concern). Using the
// standard library here avoids custom cryptography.
func HashCredential(credential []byte) string {
	sum := sha256.Sum256(credential)
	return hex.EncodeToString(sum[:])
}

// ---------- Secret references ----------

// Store kinds supported by the SecretRef abstraction. A kind names the
// resolution backend; it does not embed credentials.
const (
	StoreKindDev     = "dev"
	StoreKindEnv     = "env"
	StoreKindVault   = "vault"
	StoreKindUnknown = "unknown"
)

// SecretRef is an opaque reference to a secret held in a secret store. It
// describes WHERE a secret lives and how it is scoped; it NEVER contains the
// secret value. Safe to log, serialize, and place in a security context.
type SecretRef struct {
	// Store is the store backend kind (e.g. "dev", "env", "vault").
	Store string `json:"store"`
	// Name is the secret's name/key within the store (e.g. "openrouter/api_key").
	Name string `json:"name"`
	// Version is an optional version/reference within the store.
	Version string `json:"version,omitempty"`
	// BusinessID/DivisionID scope the secret so that a credential for one
	// business cannot silently serve another.
	BusinessID string `json:"business_id,omitempty"`
	DivisionID string `json:"division_id,omitempty"`
	// Purpose documents what action the secret is intended for (least privilege).
	Purpose string `json:"purpose,omitempty"`
}

// ParseSecretRef parses a compact "store:name[@version][?business=..&division=..]"
// form into a SecretRef. Blank/malformed input fails closed with a VALIDATION
// error rather than producing a ref that resolves to nothing meaningful.
func ParseSecretRef(s string) (SecretRef, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return SecretRef{}, nerrors.Validation("security.secretref_empty", "secret reference is empty")
	}
	store, rest, ok := strings.Cut(raw, ":")
	if !ok || strings.TrimSpace(store) == "" || strings.TrimSpace(rest) == "" {
		return SecretRef{}, nerrors.Validation("security.secretref_malformed",
			`secret reference must be "store:name"`)
	}
	query := ""
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		query = rest[i+1:]
		rest = rest[:i]
	}
	name := rest
	version := ""
	if i := strings.IndexByte(rest, '@'); i >= 0 {
		name = rest[:i]
		version = rest[i+1:]
	}
	ref := SecretRef{
		Store:   strings.TrimSpace(store),
		Name:    strings.TrimSpace(name),
		Version: strings.TrimSpace(version),
	}
	if ref.Name == "" {
		return SecretRef{}, nerrors.Validation("security.secretref_malformed", "secret reference name is empty")
	}
	// Secret names must not smuggle path traversal or control characters into a
	// backend lookup. A name is a logical key, not a filesystem path.
	if strings.Contains(ref.Name, "..") || strings.ContainsAny(ref.Name, "/\\") {
		return SecretRef{}, nerrors.Validation("security.secretref_unsafe_name",
			"secret reference name must not contain path traversal or separators")
	}
	for _, kv := range strings.Split(query, "&") {
		if kv == "" {
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "business":
			if v == "" {
				return SecretRef{}, nerrors.Validation("security.secretref_empty_business",
					"secret reference business scope must not be empty when specified")
			}
			ref.BusinessID = v
		case "division":
			if v == "" {
				return SecretRef{}, nerrors.Validation("security.secretref_empty_division",
					"secret reference division scope must not be empty when specified")
			}
			ref.DivisionID = v
		case "purpose":
			ref.Purpose = v
		}
	}
	return ref, nil
}

// Valid reports whether the reference is structurally usable.
func (r SecretRef) Valid() bool {
	return strings.TrimSpace(r.Store) != "" && strings.TrimSpace(r.Name) != ""
}

// String renders the reference in a safe, loggable form. It never includes a
// secret value (there is none to include).
func (r SecretRef) String() string {
	s := r.Store + ":" + r.Name
	if r.Version != "" {
		s += "@" + r.Version
	}
	var q []string
	if r.BusinessID != "" {
		q = append(q, "business="+r.BusinessID)
	}
	if r.DivisionID != "" {
		q = append(q, "division="+r.DivisionID)
	}
	if r.Purpose != "" {
		q = append(q, "purpose="+r.Purpose)
	}
	if len(q) > 0 {
		s += "?" + strings.Join(q, "&")
	}
	return s
}

// ScopedTo reports whether the reference is usable within the given business
// (and, if the ref is division-scoped, division). A ref with no business scope
// is only usable in a global (empty business) context. This enforces
// business/division credential isolation: a Business A credential is NEVER
// silently usable for Business B.
func (r SecretRef) ScopedTo(businessID, divisionID string) bool {
	if r.BusinessID == "" {
		return businessID == ""
	}
	if r.BusinessID != businessID {
		return false
	}
	if r.DivisionID != "" && r.DivisionID != divisionID {
		return false
	}
	return true
}

// ---------- Secret value (minimally exposed) ----------

// Secret is an in-memory, minimally-exposed secret value.
//
// String() and MarshalJSON() return a redacted form, so a secret is never
// printed or serialized in the clear by accident. Callers must explicitly call
// Reveal() at the exact point of use, making exposure an explicit, auditable
// action.
type Secret struct {
	store string
	name  string
	value []byte
}

// NewSecret constructs a Secret from a raw value. The value is copied and the
// caller's slice is not retained.
func NewSecret(store, name string, value []byte) Secret {
	v := make([]byte, len(value))
	copy(v, value)
	return Secret{store: store, name: name, value: v}
}

// Reveal returns the raw secret bytes. This is the single, explicit exposure
// point. Callers must not log, serialize, or embed the result.
func (s Secret) Reveal() []byte { return s.value }

// Ref returns a reference describing this secret (no value).
func (s Secret) Ref() SecretRef { return SecretRef{Store: s.store, Name: s.name} }

// String redacts the value; a Secret is never printed in the clear.
func (s Secret) String() string { return "[REDACTED secret " + s.store + ":" + s.name + "]" }

// MarshalJSON redacts the value.
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redactMarker + `"`), nil
}

// Resolver resolves a SecretRef to a Secret at the point of use.
//
// Implementations must be swappable (TDR-007). A resolver is NOT an authority:
// resolving a secret never authorizes its use; that remains Governance's role.
type Resolver interface {
	// Resolve returns the secret for ref within the given scope. It fails closed
	// (returns a canonical error) when the ref is out of scope or unresolvable.
	Resolve(ref SecretRef, businessID, divisionID string) (Secret, error)
}

// ---------- Development resolver (NOT production secret management) ----------

// DevResolver is a DEVELOPMENT-ONLY secret resolver.
//
// It is intentionally NOT a production secret store. It holds secrets in process
// memory supplied by the developer/test and logs nothing. It exists so the
// foundation and its tests can exercise the resolver boundary without pretending
// to be real secret management.
type DevResolver struct {
	mu      sync.RWMutex
	secrets map[string]Secret
}

// NewDevResolver constructs an empty development resolver.
func NewDevResolver() *DevResolver {
	return &DevResolver{secrets: map[string]Secret{}}
}

// Put registers a development secret under a reference. Test/dev affordance
// only; must not be used to load production credentials.
func (d *DevResolver) Put(ref SecretRef, value []byte) error {
	if !ref.Valid() {
		return nerrors.Validation("security.devref_invalid", "development secret reference is invalid")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, ok := d.secrets[ref.String()]; ok {
		for i := range old.value {
			old.value[i] = 0
		}
	}
	d.secrets[ref.String()] = NewSecret(ref.Store, ref.Name, value)
	return nil
}

// Resolve implements Resolver. It enforces scope so credentials cannot cross
// businesses.
func (d *DevResolver) Resolve(ref SecretRef, businessID, divisionID string) (Secret, error) {
	if !ref.Valid() {
		return Secret{}, nerrors.Validation("security.secretref_invalid", "secret reference is invalid")
	}
	if !ref.ScopedTo(businessID, divisionID) {
		return Secret{}, nerrors.New("security.secret_out_of_scope",
			nerrors.CategorySecurityRejection,
			"secret reference is not scoped to the requested business/division")
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	sec, ok := d.secrets[ref.String()]
	if !ok {
		return Secret{}, nerrors.New("security.secret_not_found",
			nerrors.CategoryResourceUnavailable, "development secret not found")
	}
	return sec, nil
}

// DevelopmentOnly reports that this resolver is development-only. Callers that
// require production secret management must check this and refuse to operate.
func (d *DevResolver) DevelopmentOnly() bool { return true }

// ---------- Redaction ----------

const redactMarker = "[REDACTED]"

// RedactMarker exposes the redaction marker for tests and callers.
func RedactMarker() string { return redactMarker }

// secretKeyFragments marks key names as secret-bearing (case-insensitive).
var secretKeyFragments = []string{
	"secret", "password", "passwd", "token", "apikey", "api_key",
	"private_key", "privatekey", "credential", "authorization",
	"bearer", "cookie", "session", "client_secret",
}

// IsSecretKey reports whether a key name looks secret-bearing.
func IsSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, frag := range secretKeyFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// RedactMap returns a deep copy of m with secret-looking keys redacted. It never
// inspects values for content; redaction is by key name. Used at every boundary
// where arbitrary maps could carry secrets (logs, errors, dumps).
func RedactMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if IsSecretKey(k) {
			out[k] = redactMarker
			continue
		}
		out[k] = redactValue(v)
	}
	return out
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return RedactMap(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = redactValue(e)
		}
		return out
	default:
		return v
	}
}

// ---------- Egress policy (deny-by-default) ----------

// EgressPolicy is a deny-by-default allow-list for outbound destinations. An
// empty allow-list denies all egress. It is a restriction only and grants no
// authority; an allowed destination does not authorize any action to it.
type EgressPolicy struct {
	allow map[string]struct{}
}

// NewEgressPolicy builds a policy from an allow-list of "host" or "host:port"
// entries, normalized case-insensitively.
func NewEgressPolicy(allow []string) *EgressPolicy {
	m := make(map[string]struct{}, len(allow))
	for _, a := range allow {
		if n := normalizeHost(a); n != "" {
			m[n] = struct{}{}
		}
	}
	return &EgressPolicy{allow: m}
}

// Allow reports whether a destination is permitted. Denies by default.
func (p *EgressPolicy) Allow(host string) bool {
	if p == nil || len(p.allow) == 0 {
		return false
	}
	_, ok := p.allow[normalizeHost(host)]
	return ok
}

// Empty reports whether the policy denies everything.
func (p *EgressPolicy) Empty() bool { return p == nil || len(p.allow) == 0 }

func normalizeHost(h string) string {
	return strings.ToLower(strings.TrimSpace(h))
}
