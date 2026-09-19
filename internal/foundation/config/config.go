// Package config implements the M0 configuration foundation (component C01,
// layer L0).
//
// Scope note: this is NOT the full Configuration & Control Plane defined in
// Core/NEXUS_CONFIGURATION_CONTROL_PLANE.md and contracts/SCHEMA_OBSERVABILITY_CONFIG.md
// §3. M0 establishes only the foundation later milestones build on:
//
//   - deterministic loading
//   - environment-specific values
//   - validation with safe failure on invalid/missing required configuration
//   - documented defaults where explicitly allowed
//   - secret *references* (never secret values) — see SecretRef
//   - clear precedence: defaults < file < environment
//   - startup validation
//
// Deferred to later milestones (recorded, not implemented):
//   - DEFERRED → M1+ : versioned/audited/scoped config store, change sets,
//     approval integration, rollback, drift detection, hot reload, feature
//     flags, multi-scope (business/division/agent) effective-config resolution.
//
// Invariants preserved: configuration is NOT authority (configuration cannot
// grant authority, and restrictive governance always wins). Secrets are never
// stored as plain configuration values.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// Config is the top-level configuration. M0 defined nexus/logging/health/
// lifecycle; M1 adds a security section whose validation fails closed.
type Config struct {
	Nexus     NexusConfig     `json:"nexus"`
	Logging   LoggingConfig   `json:"logging"`
	Health    HealthConfig    `json:"health"`
	Lifecycle LifecycleConfig `json:"lifecycle"`
	Security  SecurityConfig  `json:"security"`
}

// SecurityConfig configures the security primitives foundation (component C04
// posture). It deliberately contains NO field that grants authority: it may only
// restrict or enable safe behavior, never expand privileges (CONFIGURATION ≠
// AUTHORITY). Secret-bearing values are represented only as SecretRef.
type SecurityConfig struct {
	// AuditEnabled toggles security audit emission. It may be disabled only
	// outside production; disabling it in production fails closed (§18).
	AuditEnabled bool `json:"audit_enabled"`
	// RequireAuthentication makes an authenticated identity mandatory for any
	// context entering the foundation. It is only an *enforcement switch*: turning
	// it on adds a check, it never grants anyone permission.
	RequireAuthentication bool `json:"require_authentication"`
	// EnforceBusinessScope requires an explicit business context on scoped
	// operations. It narrows behavior; it never widens access.
	EnforceBusinessScope bool `json:"enforce_business_scope"`
	// DevAllowUnsafeOverrides enables development-only convenience behavior.
	// It MUST be false in production (fails closed if true there) and is recorded
	// as an explicit, isolated development affordance.
	DevAllowUnsafeOverrides bool `json:"dev_allow_unsafe_overrides"`
	// EgressAllowList is the set of allowed outbound destinations (host[:port]).
	// Empty means no egress is permitted by default (deny-by-default). It is a
	// restriction, never an authority grant.
	EgressAllowList []string `json:"egress_allow_list"`
	// SandboxEnabled reports whether untrusted execution must run sandboxed.
	// Enabling it is defensive; it can only be disabled outside production.
	SandboxEnabled bool `json:"sandbox_enabled"`
}

// NexusConfig identifies this NEXUS installation.
type NexusConfig struct {
	// ID is the global installation identifier (contract field `nexus_id`).
	ID string `json:"id"`
	// Environment is one of: development, staging, production.
	Environment string `json:"environment"`
}

// LoggingConfig controls the structured logging foundation.
type LoggingConfig struct {
	// Level is one of: debug, info, warn, error, fatal.
	Level string `json:"level"`
	// Format is one of: json, text.
	Format string `json:"format"`
}

// HealthConfig controls the health/readiness foundation.
type HealthConfig struct {
	// Enabled toggles the health/readiness surface.
	Enabled bool `json:"enabled"`
	// Host to bind. Defaults to 127.0.0.1 (loopback) for safety.
	Host string `json:"host"`
	// Port to bind.
	Port int `json:"port"`
}

// LifecycleConfig controls process lifecycle behavior.
type LifecycleConfig struct {
	// ShutdownTimeoutSeconds is how long to wait for graceful drain.
	ShutdownTimeoutSeconds int `json:"shutdown_timeout_seconds"`
}

// SecretRef names a secret to be resolved at runtime by a secret store.
//
// M0 defines only the *reference* type; resolution is deferred to M1 (C02/C04).
// A SecretRef intentionally has no field capable of holding a raw secret value.
type SecretRef struct {
	// Ref is an opaque pointer such as "vault:nexus/model/openrouter".
	Ref string `json:"ref"`
}

// Defaults returns the documented default configuration.
//
// Defaults are safe: health binds to loopback, debug is off, and no dangerous
// behavior is enabled by default.
func Defaults() Config {
	return Config{
		Nexus: NexusConfig{
			ID:          "nx:nexus:local",
			Environment: "development",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Health: HealthConfig{
			Enabled: true,
			Host:    "127.0.0.1",
			Port:    8080,
		},
		Lifecycle: LifecycleConfig{
			ShutdownTimeoutSeconds: 30,
		},
		Security: SecurityConfig{
			AuditEnabled:            true,
			RequireAuthentication:   true,
			EnforceBusinessScope:    true,
			DevAllowUnsafeOverrides: false,
			EgressAllowList:         nil, // deny-by-default
			SandboxEnabled:          true,
		},
	}
}

// LoadOptions controls how configuration is loaded.
type LoadOptions struct {
	// FilePath, when non-empty, is a JSON config file to load.
	FilePath string
	// Environ is the environment lookup (defaults to os.Environ). Injecting it
	// makes precedence and isolation deterministic and testable.
	Environ func() []string
}

// Load resolves the effective configuration using precedence
// defaults < file < environment.
//
// It performs startup validation and returns a *nerrors.Error with category
// VALIDATION on any invalid or missing required configuration. It never returns
// a partially valid Config on error (atomic load; no partial active state).
func Load(opts LoadOptions) (Config, error) {
	cfg := Defaults()

	// Layer 1: file (overrides defaults).
	if opts.FilePath != "" {
		fileCfg, err := loadFile(opts.FilePath)
		if err != nil {
			return Config{}, err
		}
		applyOverlay(&cfg, fileCfg)
	}

	// Layer 2: environment (overrides file).
	env := opts.Environ
	if env == nil {
		env = os.Environ
	}
	if err := applyEnv(&cfg, env()); err != nil {
		return Config{}, err
	}

	// Startup validation (safe failure).
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// fileConfig mirrors Config but uses pointers so that only explicitly present
// keys override lower-precedence layers (distinguishing "absent" from "zero").
type fileConfig struct {
	Nexus *struct {
		ID          *string `json:"id"`
		Environment *string `json:"environment"`
	} `json:"nexus"`
	Logging *struct {
		Level  *string `json:"level"`
		Format *string `json:"format"`
	} `json:"logging"`
	Health *struct {
		Enabled *bool   `json:"enabled"`
		Host    *string `json:"host"`
		Port    *int    `json:"port"`
	} `json:"health"`
	Lifecycle *struct {
		ShutdownTimeoutSeconds *int `json:"shutdown_timeout_seconds"`
	} `json:"lifecycle"`
	Security *struct {
		AuditEnabled            *bool    `json:"audit_enabled"`
		RequireAuthentication   *bool    `json:"require_authentication"`
		EnforceBusinessScope    *bool    `json:"enforce_business_scope"`
		DevAllowUnsafeOverrides *bool    `json:"dev_allow_unsafe_overrides"`
		EgressAllowList         []string `json:"egress_allow_list"`
		SandboxEnabled          *bool    `json:"sandbox_enabled"`
	} `json:"security"`
}

func loadFile(path string) (fileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileConfig{}, nerrors.Configuration("config.file_missing",
				fmt.Sprintf("configuration file not found: %s", path)).WithDetail("path", path)
		}
		return fileConfig{}, nerrors.Configuration("config.file_unreadable",
			fmt.Sprintf("cannot read configuration file: %s", path)).WithDetail("path", path)
	}

	var fc fileConfig
	dec := json.NewDecoder(strings.NewReader(string(data)))
	// Reject unknown fields so typos cannot silently disable safety settings.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fc); err != nil {
		return fileConfig{}, nerrors.Configuration("config.file_invalid",
			fmt.Sprintf("configuration file is not valid: %s", path)).WithDetail("path", path)
	}
	return fc, nil
}

func applyOverlay(cfg *Config, fc fileConfig) {
	if fc.Nexus != nil {
		if fc.Nexus.ID != nil {
			cfg.Nexus.ID = *fc.Nexus.ID
		}
		if fc.Nexus.Environment != nil {
			cfg.Nexus.Environment = *fc.Nexus.Environment
		}
	}
	if fc.Logging != nil {
		if fc.Logging.Level != nil {
			cfg.Logging.Level = *fc.Logging.Level
		}
		if fc.Logging.Format != nil {
			cfg.Logging.Format = *fc.Logging.Format
		}
	}
	if fc.Health != nil {
		if fc.Health.Enabled != nil {
			cfg.Health.Enabled = *fc.Health.Enabled
		}
		if fc.Health.Host != nil {
			cfg.Health.Host = *fc.Health.Host
		}
		if fc.Health.Port != nil {
			cfg.Health.Port = *fc.Health.Port
		}
	}
	if fc.Lifecycle != nil && fc.Lifecycle.ShutdownTimeoutSeconds != nil {
		cfg.Lifecycle.ShutdownTimeoutSeconds = *fc.Lifecycle.ShutdownTimeoutSeconds
	}
	if fc.Security != nil {
		s := fc.Security
		if s.AuditEnabled != nil {
			cfg.Security.AuditEnabled = *s.AuditEnabled
		}
		if s.RequireAuthentication != nil {
			cfg.Security.RequireAuthentication = *s.RequireAuthentication
		}
		if s.EnforceBusinessScope != nil {
			cfg.Security.EnforceBusinessScope = *s.EnforceBusinessScope
		}
		if s.DevAllowUnsafeOverrides != nil {
			cfg.Security.DevAllowUnsafeOverrides = *s.DevAllowUnsafeOverrides
		}
		if s.EgressAllowList != nil {
			cfg.Security.EgressAllowList = append([]string(nil), s.EgressAllowList...)
		}
		if s.SandboxEnabled != nil {
			cfg.Security.SandboxEnabled = *s.SandboxEnabled
		}
	}
}

// Environment variable keys. Only non-secret, operational settings are
// overridable via environment at M0. Secret values are never read directly;
// secrets are referenced by SecretRef.
const (
	EnvNexusID          = "NEXUS_ID"
	EnvEnvironment      = "NEXUS_ENVIRONMENT"
	EnvLogLevel         = "NEXUS_LOG_LEVEL"
	EnvLogFormat        = "NEXUS_LOG_FORMAT"
	EnvHealthEnabled    = "NEXUS_HEALTH_ENABLED"
	EnvHealthHost       = "NEXUS_HEALTH_HOST"
	EnvHealthPort       = "NEXUS_HEALTH_PORT"
	EnvShutdownTimeoutS = "NEXUS_SHUTDOWN_TIMEOUT_SECONDS"

	// M1 security keys. These can only tighten or enable defensive behavior.
	EnvSecurityAuditEnabled            = "NEXUS_SECURITY_AUDIT_ENABLED"
	EnvSecurityRequireAuthentication   = "NEXUS_SECURITY_REQUIRE_AUTHENTICATION"
	EnvSecurityEnforceBusinessScope    = "NEXUS_SECURITY_ENFORCE_BUSINESS_SCOPE"
	EnvSecurityDevAllowUnsafeOverrides = "NEXUS_SECURITY_DEV_ALLOW_UNSAFE_OVERRIDES"
	EnvSecurityEgressAllowList         = "NEXUS_SECURITY_EGRESS_ALLOW_LIST"
	EnvSecuritySandboxEnabled          = "NEXUS_SECURITY_SANDBOX_ENABLED"
)

func applyEnv(cfg *Config, environ []string) error {
	envMap := parseEnviron(environ)

	if v, ok := envMap[EnvNexusID]; ok {
		cfg.Nexus.ID = v
	}
	if v, ok := envMap[EnvEnvironment]; ok {
		cfg.Nexus.Environment = v
	}
	if v, ok := envMap[EnvLogLevel]; ok {
		cfg.Logging.Level = v
	}
	if v, ok := envMap[EnvLogFormat]; ok {
		cfg.Logging.Format = v
	}
	if v, ok := envMap[EnvHealthEnabled]; ok {
		cfg.Health.Enabled = v == "true" || v == "1"
	}
	if v, ok := envMap[EnvHealthHost]; ok {
		cfg.Health.Host = v
	}
	if v, ok := envMap[EnvHealthPort]; ok {
		var p int
		if _, err := fmt.Sscanf(v, "%d", &p); err != nil {
			return nerrors.Configuration("config.env_invalid",
				fmt.Sprintf("%s must be an integer", EnvHealthPort)).WithDetail("env", EnvHealthPort)
		}
		cfg.Health.Port = p
	}
	if v, ok := envMap[EnvShutdownTimeoutS]; ok {
		var s int
		if _, err := fmt.Sscanf(v, "%d", &s); err != nil {
			return nerrors.Configuration("config.env_invalid",
				fmt.Sprintf("%s must be an integer", EnvShutdownTimeoutS)).WithDetail("env", EnvShutdownTimeoutS)
		}
		cfg.Lifecycle.ShutdownTimeoutSeconds = s
	}

	// Security-sensitive environment values fail closed on malformed input: a
	// typo must not silently leave a security switch in a weaker state.
	var secErr error
	setBool := func(key string, dst *bool) {
		v, ok := envMap[key]
		if !ok || secErr != nil {
			return
		}
		b, err := parseStrictBool(v)
		if err != nil {
			secErr = nerrors.Configuration("config.env_invalid",
				fmt.Sprintf("%s must be true or false", key)).WithDetail("env", key)
			return
		}
		*dst = b
	}
	setBool(EnvSecurityAuditEnabled, &cfg.Security.AuditEnabled)
	setBool(EnvSecurityRequireAuthentication, &cfg.Security.RequireAuthentication)
	setBool(EnvSecurityEnforceBusinessScope, &cfg.Security.EnforceBusinessScope)
	setBool(EnvSecurityDevAllowUnsafeOverrides, &cfg.Security.DevAllowUnsafeOverrides)
	setBool(EnvSecuritySandboxEnabled, &cfg.Security.SandboxEnabled)
	if secErr != nil {
		return secErr
	}
	if v, ok := envMap[EnvSecurityEgressAllowList]; ok {
		list, err := parseEgressList(v)
		if err != nil {
			return err
		}
		cfg.Security.EgressAllowList = list
	}
	return nil
}

// parseEgressList parses the egress allow-list strictly: entries are trimmed of
// surrounding spaces, but an empty entry (e.g. "a,,b") is a configuration error
// rather than being silently dropped, so a typo cannot silently widen or narrow
// the egress posture.
func parseEgressList(v string) ([]string, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			return nil, nerrors.Validation("config.egress_entry_empty",
				"egress allow-list contains an empty entry")
		}
		out = append(out, t)
	}
	return out, nil
}

// parseStrictBool accepts only explicit true/false spellings so that an invalid
// security value fails closed rather than defaulting.
func parseStrictBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool %q", v)
	}
}

func parseEnviron(environ []string) map[string]string {
	m := make(map[string]string, len(environ))
	for _, kv := range environ {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

var validEnvironments = map[string]struct{}{
	"development": {}, "staging": {}, "production": {},
}

var validLogLevels = map[string]struct{}{
	"debug": {}, "info": {}, "warn": {}, "error": {}, "fatal": {},
}

var validLogFormats = map[string]struct{}{
	"json": {}, "text": {},
}

// Validate checks the effective configuration and returns a canonical
// VALIDATION error on the first failure. Deterministic and side-effect free.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Nexus.ID) == "" {
		return nerrors.Configuration("config.missing", "nexus.id is required")
	}
	if !hasKey(validEnvironments, c.Nexus.Environment) {
		return nerrors.Configuration("config.invalid",
			fmt.Sprintf("nexus.environment must be one of %s", keys(validEnvironments))).
			WithDetail("value", c.Nexus.Environment)
	}
	if !hasKey(validLogLevels, c.Logging.Level) {
		return nerrors.Configuration("config.invalid",
			fmt.Sprintf("logging.level must be one of %s", keys(validLogLevels))).
			WithDetail("value", c.Logging.Level)
	}
	if !hasKey(validLogFormats, c.Logging.Format) {
		return nerrors.Configuration("config.invalid",
			fmt.Sprintf("logging.format must be one of %s", keys(validLogFormats))).
			WithDetail("value", c.Logging.Format)
	}
	if c.Health.Enabled {
		if strings.TrimSpace(c.Health.Host) == "" {
			return nerrors.Configuration("config.invalid", "health.host is required when health is enabled")
		}
		if c.Health.Port < 1 || c.Health.Port > 65535 {
			return nerrors.Configuration("config.invalid", "health.port must be between 1 and 65535").
				WithDetail("value", c.Health.Port)
		}
	}
	if c.Lifecycle.ShutdownTimeoutSeconds < 0 {
		return nerrors.Configuration("config.invalid", "lifecycle.shutdown_timeout_seconds must be >= 0").
			WithDetail("value", c.Lifecycle.ShutdownTimeoutSeconds)
	}
	// Security-sensitive configuration fails closed (§18). These checks only
	// ever reject unsafe combinations; none of them can grant authority.
	if c.IsProduction() {
		if !c.Security.AuditEnabled {
			return nerrors.Configuration("config.security_unsafe",
				"security.audit_enabled must be true in production")
		}
		if !c.Security.RequireAuthentication {
			return nerrors.Configuration("config.security_unsafe",
				"security.require_authentication must be true in production")
		}
		if !c.Security.EnforceBusinessScope {
			return nerrors.Configuration("config.security_unsafe",
				"security.enforce_business_scope must be true in production")
		}
		if !c.Security.SandboxEnabled {
			return nerrors.Configuration("config.security_unsafe",
				"security.sandbox_enabled must be true in production")
		}
		if c.Security.DevAllowUnsafeOverrides {
			return nerrors.Configuration("config.security_unsafe",
				"security.dev_allow_unsafe_overrides must be false in production")
		}
	}
	for _, host := range c.Security.EgressAllowList {
		if strings.TrimSpace(host) == "" {
			return nerrors.Configuration("config.security_invalid",
				"security.egress_allow_list must not contain empty entries")
		}
	}
	return nil
}

// IsProduction reports whether the environment is production.
func (c Config) IsProduction() bool { return c.Nexus.Environment == "production" }

func hasKey(m map[string]struct{}, k string) bool { _, ok := m[k]; return ok }

func keys(m map[string]struct{}) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
