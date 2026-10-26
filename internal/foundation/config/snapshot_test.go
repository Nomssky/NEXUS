package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSnapshotDefaults(t *testing.T) Snapshot {
	t.Helper()
	return Defaults().Snapshot()
}

func testProvider(id string) ModelProviderConfig {
	return ModelProviderConfig{
		ID:           id,
		Endpoint:     "https://api." + id + ".example/v1",
		ModelID:      id + "-model",
		APIKeyEnv:    strings.ToUpper(id) + "_API_KEY",
		Capabilities: []string{"reasoning", "tool_calling"},
	}
}

func TestSnapshotFingerprintSchema(t *testing.T) {
	fp := testSnapshotDefaults(t).Fingerprint()
	if !strings.HasPrefix(fp, FingerprintSchema+":") {
		t.Fatalf("fingerprint must carry versioned schema prefix %q, got %q", FingerprintSchema, fp)
	}
	digest := strings.TrimPrefix(fp, FingerprintSchema+":")
	if len(digest) != sha256HexLength {
		t.Fatalf("expected %d hex characters, got %d (%q)", sha256HexLength, len(digest), digest)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		t.Fatalf("digest must be hex: %v", err)
	}
}

const sha256HexLength = 64

// TestSnapshotFingerprintSectionCoverage is table-driven per documented
// configuration section: every effective, material setting must change the
// fingerprint, and recomputing from the same settings must be stable.
func TestSnapshotFingerprintSectionCoverage(t *testing.T) {
	base := Defaults().Snapshot().Fingerprint()
	cases := []struct {
		section string
		mutate  func(*Config)
	}{
		{"nexus.id", func(c *Config) { c.Nexus.ID = "nx:nexus:other" }},
		{"nexus.environment", func(c *Config) { c.Nexus.Environment = "staging" }},
		{"logging.level", func(c *Config) { c.Logging.Level = "debug" }},
		{"logging.format", func(c *Config) { c.Logging.Format = "text" }},
		{"health.enabled", func(c *Config) { c.Health.Enabled = false }},
		{"health.host", func(c *Config) { c.Health.Host = "0.0.0.0" }},
		{"health.port", func(c *Config) { c.Health.Port = 9090 }},
		{"lifecycle.shutdown", func(c *Config) { c.Lifecycle.ShutdownTimeoutSeconds = 5 }},
		{"security.audit", func(c *Config) { c.Security.AuditEnabled = false }},
		{"security.auth", func(c *Config) { c.Security.RequireAuthentication = false }},
		{"security.scope", func(c *Config) { c.Security.EnforceBusinessScope = false }},
		{"security.dev_overrides", func(c *Config) { c.Security.DevAllowUnsafeOverrides = true }},
		{"security.egress", func(c *Config) { c.Security.EgressAllowList = []string{"api.example.com"} }},
		{"security.sandbox", func(c *Config) { c.Security.SandboxEnabled = false }},
		{"storage.data_dir", func(c *Config) { c.Storage.DataDir = "/var/lib/nexus" }},
		{"models.seeded_status", func(c *Config) { c.Models.SeededProviderStatus = "offline" }},
		{"models.seeded_mode", func(c *Config) { c.Models.SeededProviderMode = "scripted" }},
		{"models.providers", func(c *Config) { c.Models.Providers = []ModelProviderConfig{testProvider("alpha")} }},
		{"rate_limit.enabled", func(c *Config) { c.RateLimit.Enabled = false }},
		{"rate_limit.submit", func(c *Config) { c.RateLimit.SubmitPerMinute = 7 }},
		{"rate_limit.request", func(c *Config) { c.RateLimit.RequestPerMinute = 8 }},
		{"rate_limit.per_ip", func(c *Config) { c.RateLimit.PerIPPerMinute = 9 }},
		{"http.cors", func(c *Config) { c.HTTP.CORSAllowedOrigins = []string{"https://app.example"} }},
	}
	for _, tc := range cases {
		t.Run(tc.section, func(t *testing.T) {
			cfg := Defaults()
			tc.mutate(&cfg)
			got := cfg.Snapshot().Fingerprint()
			if got == base {
				t.Fatalf("fingerprint must change when %s changes", tc.section)
			}
			again := cfg.Snapshot().Fingerprint()
			if got != again {
				t.Fatalf("fingerprint must be deterministic: %q vs %q", got, again)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("mutation for %s must remain valid: %v", tc.section, err)
			}
		})
	}
}

// Provider fields are fingerprinted individually: a field silently dropped
// from the canonical allow-list must fail this test even when provider order
// and secret exclusion tests still pass.
func TestSnapshotFingerprintProviderFieldCoverage(t *testing.T) {
	baseCfg := Defaults()
	baseCfg.Models.Providers = []ModelProviderConfig{testProvider("alpha")}
	base := baseCfg.Snapshot().Fingerprint()

	cases := []struct {
		name   string
		mutate func(*ModelProviderConfig)
	}{
		{"id", func(p *ModelProviderConfig) { p.ID = "beta" }},
		{"name", func(p *ModelProviderConfig) { p.Name = "Renamed provider" }},
		{"endpoint", func(p *ModelProviderConfig) { p.Endpoint = "https://api.beta.example/v1" }},
		{"model_id", func(p *ModelProviderConfig) { p.ModelID = "beta-model" }},
		{"api_key_env", func(p *ModelProviderConfig) { p.APIKeyEnv = "BETA_API_KEY" }},
		{"timeout", func(p *ModelProviderConfig) { p.Timeout = 45 * time.Second }},
		{"allow_insecure_http", func(p *ModelProviderConfig) { p.AllowInsecureHTTP = true }},
		{"max_request_bytes", func(p *ModelProviderConfig) { p.MaxRequestBytes = 1 << 20 }},
		{"max_response_bytes", func(p *ModelProviderConfig) { p.MaxResponseBytes = 2 << 20 }},
		{"capabilities", func(p *ModelProviderConfig) { p.Capabilities = []string{"vision"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := cloneConfig(baseCfg)
			tc.mutate(&cfg.Models.Providers[0])
			got := cfg.Snapshot().Fingerprint()
			if got == base {
				t.Fatalf("fingerprint must change when provider %s changes", tc.name)
			}
			if again := cfg.Snapshot().Fingerprint(); got != again {
				t.Fatalf("fingerprint must be deterministic: %q vs %q", got, again)
			}
		})
	}
}

func TestSnapshotFingerprintProviderOrderIsMaterial(t *testing.T) {
	a := Defaults()
	a.Models.Providers = []ModelProviderConfig{testProvider("alpha"), testProvider("beta")}
	b := Defaults()
	b.Models.Providers = []ModelProviderConfig{testProvider("beta"), testProvider("alpha")}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().Fingerprint() == b.Snapshot().Fingerprint() {
		t.Fatal("provider registration order is material and must change the fingerprint")
	}
}

func TestSnapshotFingerprintSetOrderingIsImmaterial(t *testing.T) {
	base := Defaults()
	base.Security.EgressAllowList = []string{"api.one.example", "api.two.example"}
	base.HTTP.CORSAllowedOrigins = []string{"https://one.example", "https://two.example"}
	base.Models.Providers = []ModelProviderConfig{testProvider("alpha")}
	base.Models.Providers[0].Capabilities = []string{"reasoning", "tool_calling"}

	reordered := cloneConfig(base)
	reordered.Security.EgressAllowList = []string{"api.two.example", "api.one.example"}
	reordered.HTTP.CORSAllowedOrigins = []string{"https://two.example", "https://one.example"}
	reordered.Models.Providers[0].Capabilities = []string{"tool_calling", "reasoning"}

	if base.Snapshot().Fingerprint() != reordered.Snapshot().Fingerprint() {
		t.Fatal("set-shaped settings must not change the fingerprint when only their order changes")
	}

	trimmed := cloneConfig(base)
	trimmed.Security.EgressAllowList = []string{"  api.one.example ", "api.two.example"}
	if trimmed.Snapshot().Fingerprint() != base.Snapshot().Fingerprint() {
		t.Fatal("egress entries are trimmed by the loader and must fingerprint identically")
	}
}

func TestSnapshotFingerprintEndpointSanitization(t *testing.T) {
	clean := Defaults()
	clean.Models.Providers = []ModelProviderConfig{testProvider("alpha")}
	dirty := cloneConfig(clean)
	dirty.Models.Providers[0].Endpoint = " https://user:supersecret@api.alpha.example/v1?token=supersecret#fragment "

	if clean.Snapshot().Fingerprint() != dirty.Snapshot().Fingerprint() {
		t.Fatal("endpoint userinfo/query/fragment must not affect the fingerprint")
	}
	if strings.Contains(dirty.Snapshot().Fingerprint(), "supersecret") {
		t.Fatal("fingerprint must never contain endpoint credentials")
	}
}

func TestSnapshotFingerprintSecretValuesExcluded(t *testing.T) {
	withProvider := Defaults()
	withProvider.Models.Providers = []ModelProviderConfig{testProvider("alpha")}

	_, a, err := LoadSnapshot(LoadOptions{Environ: func() []string {
		return []string{"ALPHA_API_KEY=first-secret-value", "NEXUS_CONTROL_API_KEY=control-one"}
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := LoadSnapshot(LoadOptions{Environ: func() []string {
		return []string{"ALPHA_API_KEY=second-secret-value", "NEXUS_CONTROL_API_KEY=control-two"}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("secret values must not affect the fingerprint")
	}
	for _, fp := range []string{a.Fingerprint(), b.Fingerprint()} {
		if strings.Contains(fp, "secret") || strings.Contains(fp, "control-") {
			t.Fatalf("fingerprint must not contain secret material: %q", fp)
		}
	}

	// The credential variable *name* is configuration and remains material.
	base := withProvider.Snapshot().Fingerprint()
	renamed := cloneConfig(withProvider)
	renamed.Models.Providers[0].APIKeyEnv = "BETA_API_KEY"
	if renamed.Snapshot().Fingerprint() == base {
		t.Fatal("api_key_env name must be covered by the fingerprint")
	}
}

func TestSnapshotDeepImmutability(t *testing.T) {
	cfg := Defaults()
	cfg.Security.EgressAllowList = []string{"api.one.example", "api.two.example"}
	cfg.HTTP.CORSAllowedOrigins = []string{"https://one.example"}
	cfg.Models.Providers = []ModelProviderConfig{testProvider("alpha")}
	cfg.Models.Providers[0].Capabilities = []string{"reasoning", "tool_calling"}

	snap := cfg.Snapshot()
	want := snap.Fingerprint()

	// Mutating the source config after snapshotting must not leak in.
	cfg.Models.Providers[0].Capabilities[0] = "tampered"
	cfg.Security.EgressAllowList[0] = "tampered.example"
	cfg.HTTP.CORSAllowedOrigins[0] = "https://tampered.example"

	// Mutating returned settings must not leak in either.
	settings := snap.Settings()
	settings.Models.Providers[0].Capabilities[1] = "tampered"
	settings.Security.EgressAllowList[1] = "tampered.example"
	settings.HTTP.CORSAllowedOrigins[0] = "https://tampered.example"

	// Mutating a fresh Settings() read must be independent of the previous read.
	again := snap.Settings()
	if again.Models.Providers[0].Capabilities[0] != "reasoning" ||
		again.Models.Providers[0].Capabilities[1] != "tool_calling" {
		t.Fatalf("provider capabilities leaked through Settings(): %v", again.Models.Providers[0].Capabilities)
	}
	if again.Security.EgressAllowList[0] != "api.one.example" ||
		again.Security.EgressAllowList[1] != "api.two.example" {
		t.Fatalf("egress allow-list leaked through Settings(): %v", again.Security.EgressAllowList)
	}
	if again.HTTP.CORSAllowedOrigins[0] != "https://one.example" {
		t.Fatalf("CORS origins leaked through Settings(): %v", again.HTTP.CORSAllowedOrigins)
	}
	if snap.Fingerprint() != want {
		t.Fatal("snapshot fingerprint must be immutable against caller mutation")
	}

	// Redacted settings are independently deep-copied as well.
	redacted := snap.Redacted()
	redacted.Security.EgressAllowList[0] = "tampered.example"
	redacted.Models.Providers[0].Capabilities[0] = "tampered"
	if snap.Redacted().Security.EgressAllowList[0] != "api.one.example" {
		t.Fatal("Redacted() must not expose mutable backing storage")
	}

	// Source metadata must be copied too.
	cfg2 := Defaults()
	cfg2.Logging.Level = "debug"
	snap2 := cfg2.Snapshot()
	sources := snap2.Sources()
	sources["logging"] = SourceFile
	if snap2.Source("logging") != SourceDefault {
		t.Fatal("Sources() must return a copy")
	}
}

func TestSnapshotSourceDefaults(t *testing.T) {
	_, snap, err := LoadSnapshot(LoadOptions{Environ: func() []string { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	wantSections := []string{
		"nexus", "logging", "health", "lifecycle", "security",
		"storage", "models", "rate_limit", "http",
	}
	got := snap.SourceMap()
	if len(got) != len(wantSections) {
		t.Fatalf("source map must include every documented section: got %v", got)
	}
	for _, section := range wantSections {
		if got[section] != SourceDefault {
			t.Fatalf("section %q should be default, got %q", section, got[section])
		}
	}
	if len(snap.Sources()) != 0 {
		t.Fatalf("no explicit source should be recorded for defaults: %v", snap.Sources())
	}
	if snap.Source("not-a-section") != SourceDefault {
		t.Fatal("unknown sections must report default")
	}

	names := SectionNames()
	if len(names) != len(wantSections) {
		t.Fatalf("SectionNames() must report every documented section: %v", names)
	}
	names[0] = "tampered"
	if SectionNames()[0] != "nexus" {
		t.Fatal("SectionNames() must return a fresh copy")
	}
}

func TestSnapshotSourceAttribution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
		"logging": {"level": "debug"},
		"rate_limit": {"submit_per_minute": 10},
		"http": {"cors_allowed_origins": ["https://file.example"]},
		"models": {"seeded_provider_status": "offline"},
		"security": {"egress_allow_list": ["api.file.example"]}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	_, snap, err := LoadSnapshot(LoadOptions{
		FilePath: path,
		Environ: func() []string {
			return []string{
				"NEXUS_LOG_FORMAT=text", // same section as the file -> mixed
				"NEXUS_SECURITY_EGRESS_ALLOW_LIST=api.env.example",
				"NEXUS_SEEDED_PROVIDER_MODE=scripted", // same section as the file -> mixed
				"NEXUS_RATE_LIMIT_REQUEST_PER_MINUTE=99",
				"NEXUS_HTTP_CORS_ALLOWED_ORIGINS=https://env.example",
				"NEXUS_HEALTH_PORT=9090",
				"NEXUS_DATA_DIR=/from/env",
				"NEXUS_SHUTDOWN_TIMEOUT_SECONDS=12",
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]Source{
		"nexus":      SourceDefault,
		"logging":    SourceMixed,
		"health":     SourceEnvironment,
		"lifecycle":  SourceEnvironment,
		"security":   SourceMixed,
		"storage":    SourceEnvironment,
		"models":     SourceMixed,
		"rate_limit": SourceMixed,
		"http":       SourceMixed,
	}
	for section, src := range want {
		if got := snap.Source(section); got != src {
			t.Fatalf("section %q source = %q, want %q", section, got, src)
		}
	}
	if len(snap.Sources()) == 0 {
		t.Fatal("explicit source metadata must be recorded")
	}
}

func TestSnapshotSourceFileOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"logging":{"level":"debug"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, snap, err := LoadSnapshot(LoadOptions{FilePath: path, Environ: func() []string { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Source("logging"); got != SourceFile {
		t.Fatalf("logging source = %q, want file", got)
	}
	if got := snap.Source("health"); got != SourceDefault {
		t.Fatalf("health source = %q, want default", got)
	}
}

func TestLoadSnapshotCapturesEnvironmentOnce(t *testing.T) {
	calls := 0
	_, snap, err := LoadSnapshot(LoadOptions{Environ: func() []string {
		calls++
		if calls > 1 {
			return []string{"NEXUS_LOG_LEVEL=error"}
		}
		return []string{"NEXUS_LOG_LEVEL=debug"}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("environment must be captured exactly once, got %d calls", calls)
	}
	if got := snap.Settings().Logging.Level; got != "debug" {
		t.Fatalf("effective configuration must use the captured environment, got %q", got)
	}
	if got := snap.Source("logging"); got != SourceEnvironment {
		t.Fatalf("source attribution must use the same captured environment, got %q", got)
	}
}

func TestSnapshotStringIsSafe(t *testing.T) {
	shown := Defaults().Snapshot().String()
	if !strings.Contains(shown, FingerprintSchema) {
		t.Fatalf("summary must include the versioned fingerprint: %q", shown)
	}
	for _, secret := range []string{"secret", "api_key=", "token="} {
		if strings.Contains(shown, secret) {
			t.Fatalf("summary must not contain secret-like material %q: %q", secret, shown)
		}
	}
}

// Guard against accidental schema drift: v2 is the current fingerprint schema.
func TestFingerprintSchemaVersion(t *testing.T) {
	if FingerprintSchema != "sha256:v2" {
		t.Fatalf("fingerprint schema changed without a migration: %q", FingerprintSchema)
	}
}
