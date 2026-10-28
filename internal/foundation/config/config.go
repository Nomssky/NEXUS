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
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

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
	Storage   StorageConfig   `json:"storage"`
	Models    ModelsConfig    `json:"models"`
	RateLimit RateLimitConfig `json:"rate_limit"`
	HTTP      HTTPConfig      `json:"http"`
}

// ModelsConfig configures the model layer's seeded simulated provider
// (PROVIDER_CONTRACTS §12). It exists so a provider failure is reachable
// through the shipped binary without registering a real provider; it cannot
// add a health state, an error category or an event.
type ModelsConfig struct {
	// SeededProviderStatus is the health the simulated provider starts with
	// when the launcher seeds it (no model registered). "" and "healthy" keep
	// the shipped behaviour; "offline" makes every model invocation fail
	// deterministically. Rejected values abort boot — a typo must not
	// silently mean "healthy".
	SeededProviderStatus string `json:"seeded_provider_status"`

	// SeededProviderMode selects *which* simulation provider the launcher
	// seeds when nothing is registered: "" (default) = the plain simulated
	// provider (empty completions, today's behaviour); "scripted" = the
	// deterministic decision-table provider used to exercise the agent
	// intelligence control loop without a real LLM
	// (AGENT_INTELLIGENCE_CONTRACTS §16/§17). It adds no health state, no
	// error category and no event; rejected values abort boot.
	SeededProviderMode string `json:"seeded_provider_mode"`

	// PreferredModel is the GLOBAL model preference: the model identifier that
	// hierarchical selection prefers before division/agent overrides and above
	// automatic selection (modelselect precedence). It is advisory input to the
	// router's allowed-candidate constrainer — it grants no authority and is
	// distinguishable in every routing decision from the model actually
	// selected. An unknown/unavailable model does not break boot: it is
	// reported as an unmet preference in routing diagnostics (fail-safe
	// fallback policy applies, never a silent explicit-model substitution).
	// Override precedence: file < NEXUS_MODEL_PREFERRED_MODEL.
	PreferredModel string `json:"preferred_model,omitempty"`

	// Providers lists real production inference adapters. When any entry is
	// configured the launcher registers them instead of the seeded simulator,
	// so real inference is always an explicit configuration choice.
	Providers []ModelProviderConfig `json:"providers"`
}

// ModelProviderConfig is one production provider registration. It never holds
// the API key: APIKeyEnv names the environment variable that carries it.
type ModelProviderConfig struct {
	// Kind selects the provider constructor: "openai" (default, the shipped
	// OpenAI-compatible adapter) or "ollama" (the local Ollama runtime). A
	// local Ollama entry never carries a credential and never routes a prompt
	// off the machine. Empty means "openai"; an unknown kind aborts boot.
	Kind              string        `json:"kind,omitempty"`
	ID                string        `json:"id"`
	Name              string        `json:"name,omitempty"`
	Endpoint          string        `json:"endpoint"`
	ModelID           string        `json:"model_id"`
	APIKeyEnv         string        `json:"api_key_env"`
	Timeout           time.Duration `json:"timeout,omitempty"`
	AllowInsecureHTTP bool          `json:"allow_insecure_http,omitempty"`
	MaxRequestBytes   int64         `json:"max_request_bytes,omitempty"`
	MaxResponseBytes  int64         `json:"max_response_bytes,omitempty"`
	// Capabilities defaults to reasoning/tool_calling/structured_output —
	// the exact set the agent intelligence loop requires.
	Capabilities []string `json:"capabilities,omitempty"`
	// DisableThinking disables the model's internal reasoning/thinking channel
	// when the local Ollama model supports it (Ollama >= 0.40, thinking-capable
	// models such as qwen35). Thinking models can exhaust their token budget on
	// reasoning and emit no visible content; disabling thinking yields a final
	// answer faster. Default false preserves existing behavior.
	DisableThinking bool `json:"disable_thinking,omitempty"`
}

// StorageConfig configures durable storage (C05). The default is empty:
// records stay in-memory and do not survive restart (the historical
// posture). Setting data_dir enables the file-backed store; boot fails
// closed if the directory cannot be created or loaded.
type StorageConfig struct {
	// DataDir is the root directory for durable JSON records. Empty disables
	// persistence entirely (no partial durability — nothing is written).
	DataDir string `json:"data_dir"`
}

// RateLimitConfig configures the process-local HTTP rate limiter
// (Production Hardening v2, F4). It complements queue-level backpressure; it is
// NOT a distributed quota (multi-instance global limits need an external
// limiter). Values are requests per minute.
type RateLimitConfig struct {
	// Enabled turns the limiter on. Default true. Explicit per-minute values
	// must be >= 1; a zero/negative value fails validation rather than
	// silently disabling protection.
	Enabled bool `json:"enabled"`
	// SubmitPerMinute bounds the expensive work-creation endpoints
	// (POST /api/v1/requests, /api/v1/executions, /api/v1/intelligence/execute)
	// per authenticated actor.
	SubmitPerMinute int `json:"submit_per_minute"`
	// RequestPerMinute bounds every other scoped API request per authenticated
	// actor (separate bucket, so exhausting submit cannot block cancellation).
	RequestPerMinute int `json:"request_per_minute"`
	// PerIPPerMinute bounds every non-exempt request per direct peer address,
	// bounding unauthenticated flooding and control-plane traffic.
	PerIPPerMinute int `json:"per_ip_per_minute"`
}

// HTTPConfig configures the browser-facing HTTP policy (Production Hardening
// v2, F5). An empty CORS allow-list keeps the secure same-origin default: the
// gateway emits no CORS permission headers.
type HTTPConfig struct {
	// CORSAllowedOrigins is the explicit cross-origin allow-list. Each entry
	// must be an origin (scheme://host[:port]); wildcards, paths, queries and
	// fragments are rejected at startup. Used only for cross-origin browser
	// deployments; same-origin needs no configuration.
	CORSAllowedOrigins []string `json:"cors_allowed_origins"`
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
		Storage: StorageConfig{
			DataDir: "", // in-memory records (no durability)
		},
		Models: ModelsConfig{
			SeededProviderStatus: "", // seeded provider starts healthy (PROVIDER_CONTRACTS §12)
		},
		RateLimit: RateLimitConfig{
			Enabled:          true,
			SubmitPerMinute:  60,
			RequestPerMinute: 600,
			PerIPPerMinute:   6000,
		},
		HTTP: HTTPConfig{
			CORSAllowedOrigins: nil, // same-origin default: no CORS headers
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
	Storage *struct {
		DataDir *string `json:"data_dir"`
	} `json:"storage"`
	Models *struct {
		SeededProviderStatus *string `json:"seeded_provider_status"`
		SeededProviderMode   *string `json:"seeded_provider_mode"`
		PreferredModel       *string `json:"preferred_model"`
		Providers            []struct {
			ID                string   `json:"id"`
			Name              string   `json:"name"`
			Kind              string   `json:"kind"`
			Endpoint          string   `json:"endpoint"`
			ModelID           string   `json:"model_id"`
			APIKeyEnv         string   `json:"api_key_env"`
			TimeoutMS         *int     `json:"timeout_ms"`
			AllowInsecureHTTP *bool    `json:"allow_insecure_http"`
			MaxRequestBytes   *int64   `json:"max_request_bytes"`
			MaxResponseBytes  *int64   `json:"max_response_bytes"`
			Capabilities      []string `json:"capabilities"`
			DisableThinking   *bool    `json:"disable_thinking"`
		} `json:"providers"`
	} `json:"models"`
	RateLimit *struct {
		Enabled          *bool `json:"enabled"`
		SubmitPerMinute  *int  `json:"submit_per_minute"`
		RequestPerMinute *int  `json:"request_per_minute"`
		PerIPPerMinute   *int  `json:"per_ip_per_minute"`
	} `json:"rate_limit"`
	HTTP *struct {
		CORSAllowedOrigins []string `json:"cors_allowed_origins"`
	} `json:"http"`
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
	if fc.Storage != nil && fc.Storage.DataDir != nil {
		cfg.Storage.DataDir = *fc.Storage.DataDir
	}
	if fc.Models != nil && fc.Models.SeededProviderStatus != nil {
		cfg.Models.SeededProviderStatus = *fc.Models.SeededProviderStatus
	}
	if fc.Models != nil && fc.Models.SeededProviderMode != nil {
		cfg.Models.SeededProviderMode = *fc.Models.SeededProviderMode
	}
	if fc.Models != nil && fc.Models.PreferredModel != nil {
		cfg.Models.PreferredModel = strings.TrimSpace(*fc.Models.PreferredModel)
	}
	if fc.Models != nil && len(fc.Models.Providers) > 0 {
		for _, p := range fc.Models.Providers {
			entry := ModelProviderConfig{
				ID: p.ID, Name: p.Name, Endpoint: p.Endpoint, ModelID: p.ModelID,
				APIKeyEnv: p.APIKeyEnv, Capabilities: append([]string(nil), p.Capabilities...),
				Kind: strings.TrimSpace(p.Kind),
			}
			if p.TimeoutMS != nil {
				entry.Timeout = time.Duration(*p.TimeoutMS) * time.Millisecond
			}
			if p.AllowInsecureHTTP != nil {
				entry.AllowInsecureHTTP = *p.AllowInsecureHTTP
			}
			if p.MaxRequestBytes != nil {
				entry.MaxRequestBytes = *p.MaxRequestBytes
			}
			if p.MaxResponseBytes != nil {
				entry.MaxResponseBytes = *p.MaxResponseBytes
			}
			if p.DisableThinking != nil {
				entry.DisableThinking = *p.DisableThinking
			}
			cfg.Models.Providers = append(cfg.Models.Providers, entry)
		}
	}
	if fc.RateLimit != nil {
		if fc.RateLimit.Enabled != nil {
			cfg.RateLimit.Enabled = *fc.RateLimit.Enabled
		}
		if fc.RateLimit.SubmitPerMinute != nil {
			cfg.RateLimit.SubmitPerMinute = *fc.RateLimit.SubmitPerMinute
		}
		if fc.RateLimit.RequestPerMinute != nil {
			cfg.RateLimit.RequestPerMinute = *fc.RateLimit.RequestPerMinute
		}
		if fc.RateLimit.PerIPPerMinute != nil {
			cfg.RateLimit.PerIPPerMinute = *fc.RateLimit.PerIPPerMinute
		}
	}
	if fc.HTTP != nil {
		if fc.HTTP.CORSAllowedOrigins != nil {
			cfg.HTTP.CORSAllowedOrigins = append([]string(nil), fc.HTTP.CORSAllowedOrigins...)
		}
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

	// EnvDataDir overrides storage.data_dir. Non-secret operational setting:
	// the root directory for durable records (empty = in-memory).
	EnvDataDir = "NEXUS_DATA_DIR"

	// EnvSeededProviderStatus overrides models.seeded_provider_status: the
	// health the launcher-seeded simulated provider starts in ("" or
	// "healthy" = today's behaviour, "offline" = deterministic provider
	// failure). Non-secret operational setting; PROVIDER_CONTRACTS §12.
	EnvSeededProviderStatus = "NEXUS_SEEDED_PROVIDER_STATUS"

	// EnvSeededProviderMode overrides models.seeded_provider_mode: which
	// simulation provider the launcher seeds ("" = plain simulated provider,
	// "scripted" = deterministic decision-table provider for the agent
	// intelligence control loop). Non-secret operational setting.
	EnvSeededProviderMode = "NEXUS_SEEDED_PROVIDER_MODE"

	// EnvModelPreferredModel overrides models.preferred_model: the GLOBAL model
	// preference used by hierarchical selection (non-secret).
	EnvModelPreferredModel = "NEXUS_MODEL_PREFERRED_MODEL"

	// Rate limiting (F4). Non-secret operational settings.
	EnvRateLimitEnabled          = "NEXUS_RATE_LIMIT_ENABLED"
	EnvRateLimitSubmitPerMinute  = "NEXUS_RATE_LIMIT_SUBMIT_PER_MINUTE"
	EnvRateLimitRequestPerMinute = "NEXUS_RATE_LIMIT_REQUEST_PER_MINUTE"
	EnvRateLimitPerIPPerMinute   = "NEXUS_RATE_LIMIT_PER_IP_PER_MINUTE"

	// EnvHTTPCORSAllowedOrigins overrides http.cors_allowed_origins as a
	// comma-separated list of origins (F5). Non-secret operational setting.
	EnvHTTPCORSAllowedOrigins = "NEXUS_HTTP_CORS_ALLOWED_ORIGINS"

	// M1 security keys. Enforcement defaults on (Defaults() sets both true);
	// outside production they may be relaxed through these variables, while
	// Validate() forces require_authentication / enforce_business_scope /
	// audit_enabled / sandbox_enabled back on for production (§18 fail-closed).
	EnvSecurityAuditEnabled            = "NEXUS_SECURITY_AUDIT_ENABLED"
	EnvSecurityRequireAuthentication   = "NEXUS_SECURITY_REQUIRE_AUTHENTICATION"
	EnvSecurityEnforceBusinessScope    = "NEXUS_SECURITY_ENFORCE_BUSINESS_SCOPE"
	EnvSecurityDevAllowUnsafeOverrides = "NEXUS_SECURITY_DEV_ALLOW_UNSAFE_OVERRIDES"
	EnvSecurityEgressAllowList         = "NEXUS_SECURITY_EGRESS_ALLOW_LIST"
	EnvSecuritySandboxEnabled          = "NEXUS_SECURITY_SANDBOX_ENABLED"

	// EnvControlAPIKey is the API key for /api/v1/control/* endpoints.
	// It is a secret: read from the environment only, never stored in the
	// config file (config holds SecretRef references, not raw secret values).
	// Empty disables control endpoints (403 fail-closed).
	EnvControlAPIKey = "NEXUS_CONTROL_API_KEY"

	// EnvBootstrapCredential provisions the FIRST identity + credential +
	// membership on a fresh install (F3). Secret: environment only, same
	// posture as EnvControlAPIKey. Only consulted while identity enforcement
	// (require_authentication / enforce_business_scope) is active; empty
	// leaves the system fail-closed (401) and logged.
	EnvBootstrapCredential = "NEXUS_BOOTSTRAP_CREDENTIAL"
	// EnvBootstrapBusiness selects the business the bootstrap identity
	// belongs to (created if missing). Empty defaults to "default".
	EnvBootstrapBusiness = "NEXUS_BOOTSTRAP_BUSINESS"
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
	if v, ok := envMap[EnvDataDir]; ok {
		cfg.Storage.DataDir = v
	}
	if v, ok := envMap[EnvSeededProviderStatus]; ok {
		cfg.Models.SeededProviderStatus = v
	}
	if v, ok := envMap[EnvSeededProviderMode]; ok {
		cfg.Models.SeededProviderMode = v
	}
	if v, ok := envMap[EnvModelPreferredModel]; ok {
		cfg.Models.PreferredModel = strings.TrimSpace(v)
	}
	if v, ok := envMap[EnvRateLimitEnabled]; ok {
		b, err := parseStrictBool(v)
		if err != nil {
			return nerrors.Configuration("config.env_invalid",
				fmt.Sprintf("%s must be true or false", EnvRateLimitEnabled)).WithDetail("env", EnvRateLimitEnabled)
		}
		cfg.RateLimit.Enabled = b
	}
	rateInt := func(key string, dst *int) error {
		v, ok := envMap[key]
		if !ok {
			return nil
		}
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
			return nerrors.Configuration("config.env_invalid",
				fmt.Sprintf("%s must be an integer", key)).WithDetail("env", key)
		}
		*dst = n
		return nil
	}
	if err := rateInt(EnvRateLimitSubmitPerMinute, &cfg.RateLimit.SubmitPerMinute); err != nil {
		return err
	}
	if err := rateInt(EnvRateLimitRequestPerMinute, &cfg.RateLimit.RequestPerMinute); err != nil {
		return err
	}
	if err := rateInt(EnvRateLimitPerIPPerMinute, &cfg.RateLimit.PerIPPerMinute); err != nil {
		return err
	}
	if v, ok := envMap[EnvHTTPCORSAllowedOrigins]; ok {
		cfg.HTTP.CORSAllowedOrigins = splitCommaList(v)
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

// splitCommaList splits a comma-separated environment value into trimmed,
// non-empty entries. An empty value yields an empty slice.
func splitCommaList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// validateOrigin accepts only a bare origin: an http(s) scheme, a host, and an
// optional port. Wildcards, paths, queries, fragments and userinfo are
// rejected, so a configured CORS allow-list cannot silently match more than a
// single exact origin.
func validateOrigin(origin string) error {
	if strings.Contains(origin, "*") {
		return fmt.Errorf("wildcards are not allowed")
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("not a URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("host is required")
	}
	if u.User != nil {
		return fmt.Errorf("userinfo is not allowed")
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("a path is not allowed")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("a query or fragment is not allowed")
	}
	return nil
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

// validSeededProviderStatuses is deliberately closed and small: the seeded
// simulator implements exactly healthy and offline, so any other spelling is
// a configuration error rather than a silent no-op (PROVIDER_CONTRACTS §12.1).
var validSeededProviderStatuses = map[string]struct{}{
	"":        {},
	"healthy": {},
	"offline": {},
}

// validSeededProviderModes selects which simulation provider is seeded:
// "" keeps the plain simulated provider (empty completions — today's
// behaviour), "scripted" seeds the deterministic decision-table provider used
// to exercise the agent intelligence control loop without a real LLM
// (AGENT_INTELLIGENCE_CONTRACTS §16). Closed set; typos abort boot.
var validSeededProviderModes = map[string]struct{}{
	"":         {},
	"scripted": {},
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
	if c.Storage.DataDir != "" && strings.TrimSpace(c.Storage.DataDir) == "" {
		return nerrors.Configuration("config.invalid", "storage.data_dir must not be blank").
			WithDetail("value", c.Storage.DataDir)
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
	if !hasKey(validSeededProviderStatuses, c.Models.SeededProviderStatus) {
		return nerrors.Configuration("config.invalid",
			fmt.Sprintf("models.seeded_provider_status must be one of %s", keys(validSeededProviderStatuses))).
			WithDetail("value", c.Models.SeededProviderStatus)
	}
	if !hasKey(validSeededProviderModes, c.Models.SeededProviderMode) {
		return nerrors.Configuration("config.invalid",
			fmt.Sprintf("models.seeded_provider_mode must be one of %s", keys(validSeededProviderModes))).
			WithDetail("value", c.Models.SeededProviderMode)
	}
	// Each configured provider must be complete before the launcher attempts to
	// build it. This is a shape check only — the secret VALUE is read from the
	// named environment variable at provider construction (NewOpenAIProvider),
	// never here, and a missing/empty secret fails closed there.
	for i, p := range c.Models.Providers {
		kind := strings.TrimSpace(p.Kind)
		if kind == "" {
			kind = "openai"
		}
		if kind != "openai" && kind != "ollama" {
			return nerrors.Configuration("config.invalid",
				fmt.Sprintf("models.providers[%d].kind must be %q or %q", i, "openai", "ollama")).
				WithDetail("provider", p.ID).WithDetail("value", p.Kind)
		}
		switch {
		case strings.TrimSpace(p.ID) == "":
			return nerrors.Configuration("config.invalid",
				fmt.Sprintf("models.providers[%d].id is required", i))
		case strings.TrimSpace(p.Endpoint) == "":
			return nerrors.Configuration("config.invalid",
				fmt.Sprintf("models.providers[%d].endpoint is required", i)).WithDetail("provider", p.ID)
		case strings.TrimSpace(p.ModelID) == "":
			return nerrors.Configuration("config.invalid",
				fmt.Sprintf("models.providers[%d].model_id is required", i)).WithDetail("provider", p.ID)
		case kind == "openai" && strings.TrimSpace(p.APIKeyEnv) == "":
			return nerrors.Configuration("config.invalid",
				fmt.Sprintf("models.providers[%d].api_key_env must name the environment variable that carries the credential", i)).WithDetail("provider", p.ID)
		}
	}
	// Security-sensitive configuration fails closed (§18). These checks only
	// ever reject unsafe combinations; none of them can grant authority.
	if c.RateLimit.Enabled {
		for _, r := range []struct {
			name  string
			value int
		}{
			{"rate_limit.submit_per_minute", c.RateLimit.SubmitPerMinute},
			{"rate_limit.request_per_minute", c.RateLimit.RequestPerMinute},
			{"rate_limit.per_ip_per_minute", c.RateLimit.PerIPPerMinute},
		} {
			if r.value < 1 {
				return nerrors.Configuration("config.invalid",
					fmt.Sprintf("%s must be >= 1 when rate limiting is enabled", r.name)).
					WithDetail("value", r.value)
			}
		}
	}
	// CORS origins must be real origins (scheme://host[:port]); a wildcard,
	// path, query or fragment is rejected so the policy can never silently
	// open access wider than intended (F5).
	for i, origin := range c.HTTP.CORSAllowedOrigins {
		if err := validateOrigin(origin); err != nil {
			return nerrors.Configuration("config.invalid",
				fmt.Sprintf("http.cors_allowed_origins[%d] is not a valid origin (scheme://host[:port]): %v", i, err)).
				WithDetail("value", origin)
		}
	}
	if c.IsProduction() {
		// F1: production must never silently substitute a simulated/scripted
		// provider for a real one. A real provider is an explicit configuration
		// choice; without one, startup fails before the server accepts work.
		if len(c.Models.Providers) == 0 {
			return nerrors.Configuration("config.provider_missing",
				"production requires at least one real model provider in models.providers; the simulated/scripted provider is development-only")
		}
		// F2: production must not start with an accidental in-memory store.
		// Require an explicit durable storage directory (execution, approval,
		// escalation, cancellation and trace state remain process-local and are
		// NOT made durable by this requirement).
		if strings.TrimSpace(c.Storage.DataDir) == "" {
			return nerrors.Configuration("config.storage_missing",
				"production requires storage.data_dir for durable organizational state (identities, businesses, divisions, memberships, agents)")
		}
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
