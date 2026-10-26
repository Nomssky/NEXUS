// Package config — M1 additions: immutable startup snapshot, configuration
// source metadata, and a deterministic configuration fingerprint.
//
// Scope note: this is still NOT the full Configuration Control Plane
// (Core/NEXUS_CONFIGURATION_CONTROL_PLANE.md). M1 adds only what later
// foundation components need:
//
//   - a frozen, immutable snapshot of the effective configuration
//   - per-section source metadata (which layer supplied the effective value)
//   - a deterministic fingerprint that identifies the effective configuration
//     for correlation/audit (and, later, drift detection)
//   - safe redaction so a snapshot can be logged/serialized without secrets
//
// Deferred (recorded, not implemented): versioned/audited change sets, approval
// integration, rollback, drift reconciliation, hot reload, feature-flag
// orchestration, staged rollout/canary, distributed configuration.
//
// Invariants preserved: CONFIGURATION ≠ AUTHORITY (a snapshot cannot grant
// authority; it only describes configuration), SECRET_REF ≠ SECRET_VALUE (no
// snapshot field can hold a raw secret).
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

// Source identifies which precedence layer supplied a value.
type Source string

const (
	// SourceDefault is the built-in documented default.
	SourceDefault Source = "default"
	// SourceFile is a JSON configuration file.
	SourceFile Source = "file"
	// SourceEnvironment is a process/environment override.
	SourceEnvironment Source = "environment"
	// SourceMixed means both the file and the environment supplied at least
	// one value in the section. Source metadata is section-level, so it cannot
	// name a single winning layer in that case.
	SourceMixed Source = "mixed"
)

// Fingerprint schema identifiers. The schema version is part of both the
// emitted identifier and the canonical input, so a future change of meaning is
// distinguishable from a change of configuration.
const (
	FingerprintAlgorithm = "sha256"
	FingerprintVersion   = "v2"
	FingerprintSchema    = FingerprintAlgorithm + ":" + FingerprintVersion
)

// configurationSections is the stable, documented set of sections for which
// source metadata is reported. Keep this list in sync with Config.
var configurationSections = []string{
	"nexus",
	"logging",
	"health",
	"lifecycle",
	"security",
	"storage",
	"models",
	"rate_limit",
	"http",
}

// SectionNames returns the configuration sections that can carry source
// metadata, in stable order. Callers must not assume the returned slice is
// shared: it is a fresh copy.
func SectionNames() []string {
	return append([]string(nil), configurationSections...)
}

// Snapshot is an immutable view of the effective configuration.
//
// It is deliberately copy-on-construct and copy-on-access: the settings field
// is a private deep copy of the validated Config, and all accessors return
// deep copies, so a caller cannot mutate a running process's configuration
// through the snapshot (including through nested slices such as provider
// capabilities, the egress allow-list or CORS origins).
type Snapshot struct {
	settings Config
	sources  map[string]Source
	finger   string
}

// Snapshot freezes the validated configuration into an immutable snapshot.
//
// A zero Config is not valid; callers obtain a Snapshot from Load.
func (c Config) Snapshot() Snapshot {
	return newSnapshot(c, nil)
}

func newSnapshot(c Config, sources map[string]Source) Snapshot {
	settings := cloneConfig(c)
	srcCopy := make(map[string]Source, len(sources))
	for k, v := range sources {
		srcCopy[k] = v
	}
	return Snapshot{
		settings: settings,
		sources:  srcCopy,
		finger:   fingerprint(settings),
	}
}

// Settings returns a deep copy of the effective configuration.
func (s Snapshot) Settings() Config { return cloneConfig(s.settings) }

// Fingerprint returns the deterministic configuration fingerprint.
//
// The fingerprint covers the non-secret, effective settings and uses the
// versioned schema prefix FingerprintSchema ("sha256:v2") followed by ':' and
// the hex digest. It is stable for
// equivalent settings and changes when any covered effective value changes. It
// carries no authority — it is an identifier for tracing and drift detection
// only.
func (s Snapshot) Fingerprint() string { return s.finger }

// Source reports which precedence layer supplied the effective value for a
// section key (e.g. "logging", "security"). It returns SourceDefault when the
// value came from built-in defaults, and SourceMixed when both the file and
// the environment supplied values in that section.
func (s Snapshot) Source(section string) Source {
	if src, ok := s.sources[section]; ok {
		return src
	}
	return SourceDefault
}

// Sources returns a copy of all recorded (explicit) source metadata.
func (s Snapshot) Sources() map[string]Source {
	out := make(map[string]Source, len(s.sources))
	for k, v := range s.sources {
		out[k] = v
	}
	return out
}

// SourceMap returns the source of every documented configuration section,
// including SourceDefault for sections that were not explicitly overridden.
// This is the accurate, complete section-level view for a read-only surface;
// callers must not infer per-field provenance from it.
func (s Snapshot) SourceMap() map[string]Source {
	out := make(map[string]Source, len(configurationSections))
	for _, section := range configurationSections {
		out[section] = s.Source(section)
	}
	return out
}

// Redacted returns a deep copy of the effective settings with every
// secret-bearing field replaced by a marker, so the snapshot can be logged or
// serialized safely. No field in Config holds a raw secret today; this guards
// the boundary as configuration grows.
func (s Snapshot) Redacted() Config {
	// Config currently holds no raw secret values (secrets are SecretRef only).
	// Egress allow-list entries are not secrets. Keep the method so future
	// additions must consciously extend redaction here.
	return cloneConfig(s.settings)
}

// cloneConfig deep-copies every slice reachable from Config. It deliberately
// copies nil slices as nil and empty slices as empty, preserving the observed
// shape without sharing backing storage.
func cloneConfig(c Config) Config {
	out := c
	out.Security.EgressAllowList = cloneStrings(c.Security.EgressAllowList)
	out.Models.Providers = make([]ModelProviderConfig, len(c.Models.Providers))
	for i, p := range c.Models.Providers {
		out.Models.Providers[i] = p
		out.Models.Providers[i].Capabilities = cloneStrings(p.Capabilities)
	}
	out.HTTP.CORSAllowedOrigins = cloneStrings(c.HTTP.CORSAllowedOrigins)
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// fingerprint computes a stable hash over the effective, non-secret settings.
func fingerprint(c Config) string {
	sum := sha256.Sum256(canonicalFingerprintInput(c))
	return FingerprintSchema + ":" + hex.EncodeToString(sum[:])
}

// fingerprintProvider is the explicit canonical view of a model provider.
// APIKeyEnv is the *name* of the credential variable, never its value.
type fingerprintProvider struct {
	ID                string   `json:"id"`
	Name              string   `json:"name,omitempty"`
	Endpoint          string   `json:"endpoint"`
	ModelID           string   `json:"model_id"`
	APIKeyEnv         string   `json:"api_key_env"`
	TimeoutNS         int64    `json:"timeout_ns"`
	AllowInsecureHTTP bool     `json:"allow_insecure_http"`
	MaxRequestBytes   int64    `json:"max_request_bytes"`
	MaxResponseBytes  int64    `json:"max_response_bytes"`
	Capabilities      []string `json:"capabilities"`
}

// fingerprintConfig is an explicit allow-list of covered settings. A future
// Config field is intentionally not fingerprinted until it is added here and
// covered by a test; this prevents an accidental new secret from entering the
// canonical input just because it was added to Config.
type fingerprintConfig struct {
	Schema string `json:"schema"`
	Nexus  struct {
		ID          string `json:"id"`
		Environment string `json:"environment"`
	} `json:"nexus"`
	Logging struct {
		Level  string `json:"level"`
		Format string `json:"format"`
	} `json:"logging"`
	Health struct {
		Enabled bool   `json:"enabled"`
		Host    string `json:"host"`
		Port    int    `json:"port"`
	} `json:"health"`
	Lifecycle struct {
		ShutdownTimeoutSeconds int `json:"shutdown_timeout_seconds"`
	} `json:"lifecycle"`
	Security struct {
		AuditEnabled            bool     `json:"audit_enabled"`
		RequireAuthentication   bool     `json:"require_authentication"`
		EnforceBusinessScope    bool     `json:"enforce_business_scope"`
		DevAllowUnsafeOverrides bool     `json:"dev_allow_unsafe_overrides"`
		EgressAllowList         []string `json:"egress_allow_list"`
		SandboxEnabled          bool     `json:"sandbox_enabled"`
	} `json:"security"`
	Storage struct {
		DataDir string `json:"data_dir"`
	} `json:"storage"`
	Models struct {
		SeededProviderStatus string                `json:"seeded_provider_status"`
		SeededProviderMode   string                `json:"seeded_provider_mode"`
		Providers            []fingerprintProvider `json:"providers"`
	} `json:"models"`
	RateLimit struct {
		Enabled          bool `json:"enabled"`
		SubmitPerMinute  int  `json:"submit_per_minute"`
		RequestPerMinute int  `json:"request_per_minute"`
		PerIPPerMinute   int  `json:"per_ip_per_minute"`
	} `json:"rate_limit"`
	HTTP struct {
		CORSAllowedOrigins []string `json:"cors_allowed_origins"`
	} `json:"http"`
}

// canonicalFingerprintInput builds the deterministic canonical representation.
//
// Ordering rules (deliberate and documented):
//   - Model provider entries preserve configuration order. The launcher
//     registers providers in that order, so it is material to routing and must
//     not be sorted.
//   - Provider capabilities are a set; they are sorted because their order has
//     no routing meaning.
//   - Security egress entries and CORS origins are sets; they are sorted.
//     Egress entries are also trimmed, matching the loader's documented
//     normalization.
//   - Environment credential values never enter this input. Only the credential
//     variable name (APIKeyEnv) is covered.
//   - Provider endpoints are stripped of userinfo, query and fragment before
//     serialization so an accidental credential-bearing URL cannot enter the
//     canonical input.
func canonicalFingerprintInput(c Config) []byte {
	canonical := fingerprintConfig{Schema: FingerprintSchema}
	canonical.Nexus.ID = c.Nexus.ID
	canonical.Nexus.Environment = c.Nexus.Environment
	canonical.Logging.Level = c.Logging.Level
	canonical.Logging.Format = c.Logging.Format
	canonical.Health.Enabled = c.Health.Enabled
	canonical.Health.Host = c.Health.Host
	canonical.Health.Port = c.Health.Port
	canonical.Lifecycle.ShutdownTimeoutSeconds = c.Lifecycle.ShutdownTimeoutSeconds
	canonical.Security.AuditEnabled = c.Security.AuditEnabled
	canonical.Security.RequireAuthentication = c.Security.RequireAuthentication
	canonical.Security.EnforceBusinessScope = c.Security.EnforceBusinessScope
	canonical.Security.DevAllowUnsafeOverrides = c.Security.DevAllowUnsafeOverrides
	canonical.Security.EgressAllowList = normalizedStrings(c.Security.EgressAllowList, true)
	canonical.Security.SandboxEnabled = c.Security.SandboxEnabled
	canonical.Storage.DataDir = c.Storage.DataDir
	canonical.Models.SeededProviderStatus = c.Models.SeededProviderStatus
	canonical.Models.SeededProviderMode = c.Models.SeededProviderMode
	canonical.Models.Providers = make([]fingerprintProvider, len(c.Models.Providers))
	for i, p := range c.Models.Providers {
		canonical.Models.Providers[i] = fingerprintProvider{
			ID:                p.ID,
			Name:              p.Name,
			Endpoint:          fingerprintEndpoint(p.Endpoint),
			ModelID:           p.ModelID,
			APIKeyEnv:         p.APIKeyEnv,
			TimeoutNS:         int64(p.Timeout),
			AllowInsecureHTTP: p.AllowInsecureHTTP,
			MaxRequestBytes:   p.MaxRequestBytes,
			MaxResponseBytes:  p.MaxResponseBytes,
			Capabilities:      normalizedStrings(p.Capabilities, false),
		}
	}
	canonical.RateLimit.Enabled = c.RateLimit.Enabled
	canonical.RateLimit.SubmitPerMinute = c.RateLimit.SubmitPerMinute
	canonical.RateLimit.RequestPerMinute = c.RateLimit.RequestPerMinute
	canonical.RateLimit.PerIPPerMinute = c.RateLimit.PerIPPerMinute
	canonical.HTTP.CORSAllowedOrigins = normalizedStrings(c.HTTP.CORSAllowedOrigins, false)

	data, err := json.Marshal(canonical)
	if err != nil {
		// json.Marshal of a fixed struct of primitives cannot fail; fall back to
		// a stable sentinel rather than panicking in a foundational package.
		return []byte(`{"schema":"` + FingerprintSchema + `","error":"canonicalization_unavailable"}`)
	}
	return data
}

func normalizedStrings(in []string, trim bool) []string {
	out := make([]string, len(in))
	for i, value := range in {
		if trim {
			value = strings.TrimSpace(value)
		}
		out[i] = value
	}
	sort.Strings(out)
	return out
}

// fingerprintEndpoint removes URL userinfo, query and fragment so a malformed
// or legacy endpoint cannot place a credential in the canonical fingerprint
// input. Unparseable endpoints collapse to one stable marker; a process with
// such an endpoint fails later provider construction, not here.
func fingerprintEndpoint(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "(unparseable)"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// LoadSnapshot loads, validates, and freezes the configuration, returning both
// the effective Config and its immutable Snapshot with source metadata.
//
// On any error it returns no partial state (atomic load, matching Load).
func LoadSnapshot(opts LoadOptions) (Config, Snapshot, error) {
	cfg := Defaults()
	sources := map[string]Source{}

	if opts.FilePath != "" {
		fileCfg, err := loadFile(opts.FilePath)
		if err != nil {
			return Config{}, Snapshot{}, err
		}
		applyOverlay(&cfg, fileCfg)
		markSources(sources, fileCfg, SourceFile)
	}

	env := opts.Environ
	if env == nil {
		env = os.Environ
	}
	// Capture the environment once: loading and source attribution must see
	// exactly the same values even if the injector is not a pure function.
	environ := env()
	if err := applyEnv(&cfg, environ); err != nil {
		return Config{}, Snapshot{}, err
	}
	markEnvSources(sources, environ)

	if err := cfg.Validate(); err != nil {
		return Config{}, Snapshot{}, err
	}
	snap := newSnapshot(cfg, sources)
	return cfg, snap, nil
}

// markSources records which sections the file explicitly supplied.
func markSources(sources map[string]Source, fc fileConfig, src Source) {
	mark := func(present bool, section string) {
		if present {
			setSource(sources, section, src)
		}
	}
	mark(fc.Nexus != nil, "nexus")
	mark(fc.Logging != nil, "logging")
	mark(fc.Health != nil, "health")
	mark(fc.Lifecycle != nil, "lifecycle")
	mark(fc.Security != nil, "security")
	mark(fc.Storage != nil, "storage")
	mark(fc.Models != nil, "models")
	mark(fc.RateLimit != nil, "rate_limit")
	mark(fc.HTTP != nil, "http")
}

// markEnvSources records which sections the environment explicitly supplied.
func markEnvSources(sources map[string]Source, environ []string) {
	envMap := parseEnviron(environ)
	mark := func(section string, keys ...string) {
		for _, k := range keys {
			if _, ok := envMap[k]; ok {
				setSource(sources, section, SourceEnvironment)
				return
			}
		}
	}
	mark("nexus", EnvNexusID, EnvEnvironment)
	mark("logging", EnvLogLevel, EnvLogFormat)
	mark("health", EnvHealthEnabled, EnvHealthHost, EnvHealthPort)
	mark("lifecycle", EnvShutdownTimeoutS)
	mark("security", EnvSecurityAuditEnabled, EnvSecurityRequireAuthentication,
		EnvSecurityEnforceBusinessScope, EnvSecurityDevAllowUnsafeOverrides,
		EnvSecurityEgressAllowList, EnvSecuritySandboxEnabled)
	mark("storage", EnvDataDir)
	mark("models", EnvSeededProviderStatus, EnvSeededProviderMode)
	mark("rate_limit", EnvRateLimitEnabled, EnvRateLimitSubmitPerMinute,
		EnvRateLimitRequestPerMinute, EnvRateLimitPerIPPerMinute)
	mark("http", EnvHTTPCORSAllowedOrigins)
}

// setSource preserves the fact that a section was supplied by more than one
// explicit layer. Defaults are not recorded: Source reports SourceDefault when
// no explicit layer touched the section.
func setSource(sources map[string]Source, section string, src Source) {
	if previous, ok := sources[section]; ok && previous != src {
		sources[section] = SourceMixed
		return
	}
	sources[section] = src
}

// String renders a safe, secret-free summary for logs.
func (s Snapshot) String() string {
	return fmt.Sprintf("config{fingerprint=%s env=%s sources=%d}", s.finger, s.settings.Nexus.Environment, len(s.sources))
}
