package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// TEST-M0-001 (config part): valid configuration loads and validates.
func TestLoadValidDefaults(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: func() []string { return nil }})
	if err != nil {
		t.Fatalf("expected valid defaults, got error: %v", err)
	}
	if cfg.Nexus.Environment != "development" {
		t.Fatalf("expected development environment, got %q", cfg.Nexus.Environment)
	}
	if cfg.Logging.Level != "info" {
		t.Fatalf("expected info log level, got %q", cfg.Logging.Level)
	}
}

// TEST-M0-002: invalid required configuration fails safely with a VALIDATION
// category error.
func TestLoadInvalidEnvironment(t *testing.T) {
	_, err := Load(LoadOptions{
		Environ: func() []string { return []string{"NEXUS_ENVIRONMENT=banana"} },
	})
	if err == nil {
		t.Fatal("expected error for invalid environment")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION category, got %s", nerrors.CategoryOf(err))
	}
}

// TEST-M0-003: missing required configuration fails safely.
func TestLoadMissingNexusID(t *testing.T) {
	_, err := Load(LoadOptions{
		Environ: func() []string { return []string{"NEXUS_ID="} },
	})
	if err == nil {
		t.Fatal("expected error for empty nexus id")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION category, got %s", nerrors.CategoryOf(err))
	}
}

// TEST-M0-011: precedence defaults < file < environment.
func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// File overrides default (info -> warn).
	if err := os.WriteFile(path, []byte(`{"logging":{"level":"debug"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Environment overrides file (debug -> error).
	cfg, err := Load(LoadOptions{
		FilePath: path,
		Environ:  func() []string { return []string{"NEXUS_LOG_LEVEL=error"} },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Logging.Level != "error" {
		t.Fatalf("environment should win over file; got %q", cfg.Logging.Level)
	}

	// Without the env override, the file value applies.
	cfg2, err := Load(LoadOptions{
		FilePath: path,
		Environ:  func() []string { return nil },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg2.Logging.Level != "debug" {
		t.Fatalf("file should win over default; got %q", cfg2.Logging.Level)
	}
}

// TEST-M0-012: environment isolation — an injected environment does not leak
// into other loads, and process environment is not read when injected.
func TestEnvironmentIsolation(t *testing.T) {
	t.Setenv("NEXUS_LOG_LEVEL", "fatal")

	cfg, err := Load(LoadOptions{
		Environ: func() []string { return []string{"NEXUS_LOG_LEVEL=warn"} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Logging.Level != "warn" {
		t.Fatalf("injected environment must be isolated; got %q", cfg.Logging.Level)
	}
}

// Unknown config file fields are rejected (typos cannot silently disable safety).
func TestLoadUnknownFieldRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"health":{"enabled":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The above is valid; this one is not:
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"health":{"enable":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{FilePath: bad, Environ: func() []string { return nil }}); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

// Missing config file is a safe failure (not a silent default).
func TestLoadMissingFileFails(t *testing.T) {
	_, err := Load(LoadOptions{FilePath: "/nonexistent/nexus.json"})
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION category, got %s", nerrors.CategoryOf(err))
	}
}

// Configuration validation is deterministic.
func TestValidateDeterministic(t *testing.T) {
	cfg := Defaults()
	cfg.Health.Port = 0
	first := cfg.Validate()
	second := cfg.Validate()
	if first == nil || second == nil {
		t.Fatal("expected validation errors")
	}
	if first.Error() != second.Error() {
		t.Fatalf("validation must be deterministic: %q vs %q", first, second)
	}
}

// TEST-CONF-STOR-01: durability is opt-in — default data_dir is empty
// (in-memory records, the historical posture).
func TestStorageDataDirDefaultEmpty(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: func() []string { return nil }})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Storage.DataDir != "" {
		t.Fatalf("expected empty data_dir (in-memory default), got %q", cfg.Storage.DataDir)
	}
}

// TEST-CONF-STOR-02: storage.data_dir loads from the config file.
func TestStorageDataDirFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"storage":{"data_dir":"/var/lib/nexus"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{FilePath: path, Environ: func() []string { return nil }})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Storage.DataDir != "/var/lib/nexus" {
		t.Fatalf("expected file data_dir, got %q", cfg.Storage.DataDir)
	}
}

// TEST-CONF-STOR-03: NEXUS_DATA_DIR overrides the file (defaults < file < env).
func TestStorageDataDirEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"storage":{"data_dir":"/from/file"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{
		FilePath: path,
		Environ:  func() []string { return []string{"NEXUS_DATA_DIR=/from/env"} },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Storage.DataDir != "/from/env" {
		t.Fatalf("expected env to beat file, got %q", cfg.Storage.DataDir)
	}
}

// TEST-CONF-STOR-04: a blank (whitespace-only) data_dir fails validation
// rather than silently producing a confusing directory name.
func TestStorageDataDirBlankRejected(t *testing.T) {
	_, err := Load(LoadOptions{
		Environ: func() []string { return []string{"NEXUS_DATA_DIR=  "} },
	})
	if err == nil {
		t.Fatal("expected validation error for blank data_dir")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION category, got %s", nerrors.CategoryOf(err))
	}
}

// TEST-CONF-STOR-05: snapshot source metadata records where storage came from.
func TestStorageSnapshotSources(t *testing.T) {
	_, snap, err := LoadSnapshot(LoadOptions{
		Environ: func() []string { return []string{"NEXUS_DATA_DIR=/data"} },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src := snap.Source("storage"); src != SourceEnvironment {
		t.Fatalf("expected storage source=environment, got %v", src)
	}
}

// TEST-CONF-MODEL-01: models.seeded_provider_status defaults to empty, which
// the launcher seed interprets as healthy — today's behaviour unchanged
// (PROVIDER_CONTRACTS §12.1).
func TestSeededProviderStatusDefaultEmpty(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: func() []string { return nil }})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Models.SeededProviderStatus != "" {
		t.Fatalf("expected empty seeded_provider_status, got %q", cfg.Models.SeededProviderStatus)
	}
}

// TEST-CONF-MODEL-02: models.seeded_provider_status loads from the config file.
func TestSeededProviderStatusFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"models":{"seeded_provider_status":"offline"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{FilePath: path, Environ: func() []string { return nil }})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Models.SeededProviderStatus != "offline" {
		t.Fatalf("expected file seeded_provider_status, got %q", cfg.Models.SeededProviderStatus)
	}
}

// TEST-CONF-MODEL-03: NEXUS_SEEDED_PROVIDER_STATUS overrides the file
// (defaults < file < env).
func TestSeededProviderStatusEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"models":{"seeded_provider_status":"healthy"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{
		FilePath: path,
		Environ:  func() []string { return []string{"NEXUS_SEEDED_PROVIDER_STATUS=offline"} },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Models.SeededProviderStatus != "offline" {
		t.Fatalf("expected env to beat file, got %q", cfg.Models.SeededProviderStatus)
	}
}

// TEST-CONF-MODEL-04: an unimplemented status fails closed with VALIDATION
// instead of silently meaning healthy — a typo must not disable the very
// failure the key was set to produce (PROVIDER_CONTRACTS §12.1).
func TestSeededProviderStatusInvalidRejected(t *testing.T) {
	_, err := Load(LoadOptions{
		Environ: func() []string { return []string{"NEXUS_SEEDED_PROVIDER_STATUS=quarantined"} },
	})
	if err == nil {
		t.Fatal("expected validation error for an unimplemented status")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION category, got %s", nerrors.CategoryOf(err))
	}
}

// TEST-CONF-MODEL-05: snapshot source metadata records where the seed status
// came from, and the fingerprint covers it like any other effective setting.
func TestSeededProviderStatusSnapshotSources(t *testing.T) {
	_, snap, err := LoadSnapshot(LoadOptions{
		Environ: func() []string { return []string{"NEXUS_SEEDED_PROVIDER_STATUS=offline"} },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src := snap.Source("models"); src != SourceEnvironment {
		t.Fatalf("expected models source=environment, got %q", src)
	}
	_, base, err := LoadSnapshot(LoadOptions{Environ: func() []string { return nil }})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Fingerprint() == base.Fingerprint() {
		t.Fatal("fingerprint must change when the seeded provider status changes")
	}
}
