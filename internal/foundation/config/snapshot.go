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
)

// Snapshot is an immutable view of the effective configuration.
//
// It is deliberately copy-on-construct: the Settings field is a private copy of
// the validated Config, and all accessors return values, so a caller cannot
// mutate a running process's configuration through the snapshot.
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
	if sources == nil {
		sources = map[string]Source{}
	}
	return Snapshot{
		settings: c,
		sources:  sources,
		finger:   fingerprint(c),
	}
}

// Settings returns a copy of the effective configuration.
func (s Snapshot) Settings() Config { return s.settings }

// Fingerprint returns the deterministic configuration fingerprint.
//
// The fingerprint covers only non-secret, effective settings. It is stable for
// identical settings and changes when any effective value changes. It carries no
// authority — it is an identifier for tracing and drift detection only.
func (s Snapshot) Fingerprint() string { return s.finger }

// Source reports which precedence layer supplied the effective value for a
// section key (e.g. "logging", "security"). It returns SourceDefault when the
// value came from built-in defaults.
func (s Snapshot) Source(section string) Source {
	if src, ok := s.sources[section]; ok {
		return src
	}
	return SourceDefault
}

// Sources returns a copy of all recorded source metadata.
func (s Snapshot) Sources() map[string]Source {
	out := make(map[string]Source, len(s.sources))
	for k, v := range s.sources {
		out[k] = v
	}
	return out
}

// Redacted returns a copy of the effective settings with every secret-bearing
// field replaced by a marker, so the snapshot can be logged or serialized
// safely. No field in Config holds a raw secret today; this guards the boundary
// as configuration grows.
func (s Snapshot) Redacted() Config {
	c := s.settings
	// Config currently holds no raw secret values (secrets are SecretRef only).
	// Egress allow-list entries are not secrets. Keep the method so future
	// additions must consciously extend redaction here.
	return c
}

// fingerprint computes a stable hash over the effective, non-secret settings.
func fingerprint(c Config) string {
	// Build a deterministic canonical representation. Slices are normalized
	// (trim + sort) so ordering does not change the fingerprint.
	egress := append([]string(nil), c.Security.EgressAllowList...)
	for i := range egress {
		egress[i] = strings.TrimSpace(egress[i])
	}
	sort.Strings(egress)

	canonical := struct {
		NexusID     string   `json:"nexus_id"`
		Environment string   `json:"environment"`
		LogLevel    string   `json:"log_level"`
		LogFormat   string   `json:"log_format"`
		HealthUp    bool     `json:"health_enabled"`
		HealthHost  string   `json:"health_host"`
		HealthPort  int      `json:"health_port"`
		ShutdownSec int      `json:"shutdown_timeout_seconds"`
		Audit       bool     `json:"security_audit_enabled"`
		RequireAuth bool     `json:"security_require_authentication"`
		EnforceBIZ  bool     `json:"security_enforce_business_scope"`
		DevUnsafe   bool     `json:"security_dev_allow_unsafe_overrides"`
		Egress      []string `json:"security_egress_allow_list"`
		Sandbox     bool     `json:"security_sandbox_enabled"`
	}{
		NexusID:     c.Nexus.ID,
		Environment: c.Nexus.Environment,
		LogLevel:    c.Logging.Level,
		LogFormat:   c.Logging.Format,
		HealthUp:    c.Health.Enabled,
		HealthHost:  c.Health.Host,
		HealthPort:  c.Health.Port,
		ShutdownSec: c.Lifecycle.ShutdownTimeoutSeconds,
		Audit:       c.Security.AuditEnabled,
		RequireAuth: c.Security.RequireAuthentication,
		EnforceBIZ:  c.Security.EnforceBusinessScope,
		DevUnsafe:   c.Security.DevAllowUnsafeOverrides,
		Egress:      egress,
		Sandbox:     c.Security.SandboxEnabled,
	}

	data, err := json.Marshal(canonical)
	if err != nil {
		// json.Marshal of a fixed struct of primitives cannot fail; fall back to
		// a stable sentinel rather than panicking in a foundational package.
		return "sha256:unavailable"
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
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
	if err := applyEnv(&cfg, env()); err != nil {
		return Config{}, Snapshot{}, err
	}
	markEnvSources(sources, env())

	if err := cfg.Validate(); err != nil {
		return Config{}, Snapshot{}, err
	}
	snap := newSnapshot(cfg, sources)
	return cfg, snap, nil
}

// markSources records which sections the file explicitly supplied.
func markSources(sources map[string]Source, fc fileConfig, src Source) {
	if fc.Nexus != nil {
		sources["nexus"] = src
	}
	if fc.Logging != nil {
		sources["logging"] = src
	}
	if fc.Health != nil {
		sources["health"] = src
	}
	if fc.Lifecycle != nil {
		sources["lifecycle"] = src
	}
	if fc.Security != nil {
		sources["security"] = src
	}
}

// markEnvSources records which sections the environment explicitly supplied.
func markEnvSources(sources map[string]Source, environ []string) {
	envMap := parseEnviron(environ)
	mark := func(keys ...string) bool {
		for _, k := range keys {
			if _, ok := envMap[k]; ok {
				return true
			}
		}
		return false
	}
	if mark(EnvNexusID, EnvEnvironment) {
		sources["nexus"] = SourceEnvironment
	}
	if mark(EnvLogLevel, EnvLogFormat) {
		sources["logging"] = SourceEnvironment
	}
	if mark(EnvHealthEnabled, EnvHealthHost, EnvHealthPort) {
		sources["health"] = SourceEnvironment
	}
	if mark(EnvShutdownTimeoutS) {
		sources["lifecycle"] = SourceEnvironment
	}
	if mark(EnvSecurityAuditEnabled, EnvSecurityRequireAuthentication,
		EnvSecurityEnforceBusinessScope, EnvSecurityDevAllowUnsafeOverrides,
		EnvSecurityEgressAllowList, EnvSecuritySandboxEnabled) {
		sources["security"] = SourceEnvironment
	}
}

// String renders a safe, secret-free summary for logs.
func (s Snapshot) String() string {
	return fmt.Sprintf("config{fingerprint=%s env=%s sources=%d}", s.finger, s.settings.Nexus.Environment, len(s.sources))
}
