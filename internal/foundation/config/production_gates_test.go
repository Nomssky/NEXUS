package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// env builds an Environ lookup from literal KEY=VALUE pairs.
func env(pairs ...string) func() []string {
	return func() []string { return pairs }
}

// writeConfigFile writes a JSON config file into a fresh temp dir and returns
// its path.
func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// F1 — production with no configured real model provider is rejected before
// the server can accept work. The simulated/scripted provider is
// development-only and must never be an accidental production fallback.
func TestProductionRequiresRealProvider(t *testing.T) {
	_, err := Load(LoadOptions{Environ: env(
		"NEXUS_ENVIRONMENT=production",
		"NEXUS_DATA_DIR=/var/lib/nexus",
	)})
	if err == nil {
		t.Fatal("production without a real model provider must be rejected")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
	if !strings.Contains(err.Error(), "provider") {
		t.Fatalf("expected an actionable provider message, got %q", err.Error())
	}
}

// F1 — selecting the scripted simulation explicitly does not satisfy the
// production requirement: the gate looks at configured real providers, not at
// the seeded-simulation mode.
func TestProductionScriptedSimulationStillRejected(t *testing.T) {
	_, err := Load(LoadOptions{Environ: env(
		"NEXUS_ENVIRONMENT=production",
		"NEXUS_SEEDED_PROVIDER_MODE=scripted",
		"NEXUS_DATA_DIR=/var/lib/nexus",
	)})
	if err == nil {
		t.Fatal("production with only a scripted simulation must be rejected")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
}

// F1 — a complete real provider configuration passes validation when storage
// is also durable.
func TestProductionRealProviderAndStorageAccepted(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, `{
		"nexus": {"environment": "production"},
		"storage": {"data_dir": "`+dir+`"},
		"models": {"providers": [
			{"id": "openai", "endpoint": "https://api.example/v1", "model_id": "gpt-x", "api_key_env": "OPENAI_API_KEY"}
		]}
	}`)
	cfg, err := Load(LoadOptions{FilePath: path, Environ: env()})
	if err != nil {
		t.Fatalf("a complete provider + durable storage config must validate: %v", err)
	}
	if len(cfg.Models.Providers) != 1 || cfg.Models.Providers[0].ID != "openai" {
		t.Fatalf("provider not carried through: %+v", cfg.Models.Providers)
	}
}

// F1 — a provider entry missing its credential env reference is malformed and
// rejected in any environment (the secret VALUE is never read here).
func TestProviderShapeMissingAPIKeyEnvRejected(t *testing.T) {
	path := writeConfigFile(t, `{
		"models": {"providers": [
			{"id": "openai", "endpoint": "https://api.example/v1", "model_id": "gpt-x"}
		]}
	}`)
	_, err := Load(LoadOptions{FilePath: path, Environ: env()})
	if err == nil {
		t.Fatal("a provider without api_key_env must be rejected")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
}

// F1 — a provider entry missing its endpoint is rejected.
func TestProviderShapeMissingEndpointRejected(t *testing.T) {
	path := writeConfigFile(t, `{
		"models": {"providers": [
			{"id": "openai", "model_id": "gpt-x", "api_key_env": "OPENAI_API_KEY"}
		]}
	}`)
	_, err := Load(LoadOptions{FilePath: path, Environ: env()})
	if err == nil {
		t.Fatal("a provider without an endpoint must be rejected")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
}

// F2 — production requires an explicit durable storage directory; the default
// in-memory posture is not permitted.
func TestProductionRequiresDataDir(t *testing.T) {
	path := writeConfigFile(t, `{
		"nexus": {"environment": "production"},
		"models": {"providers": [
			{"id": "openai", "endpoint": "https://api.example/v1", "model_id": "gpt-x", "api_key_env": "OPENAI_API_KEY"}
		]}
	}`)
	_, err := Load(LoadOptions{FilePath: path, Environ: env()})
	if err == nil {
		t.Fatal("production without storage.data_dir must be rejected")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
	if !strings.Contains(err.Error(), "storage") {
		t.Fatalf("expected an actionable storage message, got %q", err.Error())
	}
}

// F2 — a whitespace-only data_dir in production is rejected.
func TestProductionWhitespaceDataDirRejected(t *testing.T) {
	path := writeConfigFile(t, `{
		"nexus": {"environment": "production"},
		"models": {"providers": [
			{"id": "openai", "endpoint": "https://api.example/v1", "model_id": "gpt-x", "api_key_env": "OPENAI_API_KEY"}
		]}
	}`)
	_, err := Load(LoadOptions{FilePath: path, Environ: env("NEXUS_DATA_DIR=   ")})
	if err == nil {
		t.Fatal("production with a whitespace-only data_dir must be rejected")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
}

// F2 — the environment override satisfies the production storage requirement
// (precedence: defaults < file < environment).
func TestProductionDataDirEnvSatisfies(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, `{
		"nexus": {"environment": "production"},
		"models": {"providers": [
			{"id": "openai", "endpoint": "https://api.example/v1", "model_id": "gpt-x", "api_key_env": "OPENAI_API_KEY"}
		]}
	}`)
	cfg, err := Load(LoadOptions{FilePath: path, Environ: env("NEXUS_DATA_DIR=" + dir)})
	if err != nil {
		t.Fatalf("env data_dir must satisfy production: %v", err)
	}
	if cfg.Storage.DataDir != dir {
		t.Fatalf("expected env data_dir %q, got %q", dir, cfg.Storage.DataDir)
	}
}

// Development keeps its intended in-memory + simulation posture.
func TestDevelopmentKeepsInMemoryAndSimulation(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: env("NEXUS_ENVIRONMENT=development")})
	if err != nil {
		t.Fatalf("development must load: %v", err)
	}
	if cfg.Storage.DataDir != "" {
		t.Fatalf("development default must remain in-memory, got %q", cfg.Storage.DataDir)
	}
	if len(cfg.Models.Providers) != 0 {
		t.Fatalf("development must not require providers, got %+v", cfg.Models.Providers)
	}
}

// Staging is not production: it keeps the development posture (no provider and
// no durable storage required). This documents the gate is production-specific.
func TestStagingDoesNotRequireProviderOrStorage(t *testing.T) {
	if _, err := Load(LoadOptions{Environ: env("NEXUS_ENVIRONMENT=staging")}); err != nil {
		t.Fatalf("staging must load without provider/storage requirements: %v", err)
	}
}
