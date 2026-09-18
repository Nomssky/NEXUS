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
