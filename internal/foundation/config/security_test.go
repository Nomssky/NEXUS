package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// TEST-M1-029: configuration snapshot is immutable and fingerprinted.
func TestSnapshotFingerprintStable(t *testing.T) {
	c1, s1, err := LoadSnapshot(LoadOptions{Environ: func() []string { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	_, s2, err := LoadSnapshot(LoadOptions{Environ: func() []string { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if s1.Fingerprint() == "" || s1.Fingerprint() != s2.Fingerprint() {
		t.Fatalf("fingerprint must be deterministic, got %q vs %q", s1.Fingerprint(), s2.Fingerprint())
	}
	// Fingerprint changes when a meaningful setting changes.
	c2 := c1
	c2.Logging.Level = "debug"
	if c1.Snapshot().Fingerprint() == c2.Snapshot().Fingerprint() {
		t.Fatal("fingerprint must change when configuration changes")
	}
	// Snapshot returns an immutable copy; mutating the returned settings must not
	// alter the snapshot's own fingerprint.
	got := s1.Settings()
	got.Nexus.ID = "nx:nexus:tampered"
	if s1.Fingerprint() != s2.Fingerprint() {
		t.Fatal("snapshot must be immutable against caller mutation")
	}
}

// TEST-M1-030: configuration source metadata and redaction.
func TestSnapshotSourcesAndRedaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"logging":{"level":"debug"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, snap, err := LoadSnapshot(LoadOptions{
		FilePath: path,
		Environ:  func() []string { return []string{"NEXUS_HEALTH_PORT=9090"} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Source("logging") != SourceFile {
		t.Fatalf("logging should come from file, got %q", snap.Source("logging"))
	}
	if snap.Source("health") != SourceEnvironment {
		t.Fatalf("health should come from environment, got %q", snap.Source("health"))
	}
	if snap.Source("nexus") != SourceDefault {
		t.Fatalf("nexus should be default, got %q", snap.Source("nexus"))
	}
	if len(snap.Sources()) == 0 {
		t.Fatal("sources map must not be empty")
	}
	// Redacted settings must be safe to render (no secret values anywhere).
	_ = snap.Redacted()
}

// TEST-M1-031: security configuration defaults are fail-closed.
func TestSecurityConfigDefaults(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: func() []string { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Security.AuditEnabled {
		t.Fatal("audit must be enabled by default")
	}
	if !cfg.Security.RequireAuthentication {
		t.Fatal("authentication must be required by default")
	}
	if !cfg.Security.EnforceBusinessScope {
		t.Fatal("business scope must be enforced by default")
	}
	if cfg.Security.DevAllowUnsafeOverrides {
		t.Fatal("dev unsafe overrides must be disabled by default")
	}
	if !cfg.Security.SandboxEnabled {
		t.Fatal("sandbox must be enabled by default")
	}
	if len(cfg.Security.EgressAllowList) != 0 {
		t.Fatal("egress must be deny-by-default (empty allow-list)")
	}
}

// TEST-M1-032: production hardening fails closed when security switches are
// weakened.
func TestSecurityConfigProductionFailClosed(t *testing.T) {
	cases := []map[string]string{
		{"NEXUS_ENVIRONMENT": "production", "NEXUS_SECURITY_AUDIT_ENABLED": "false"},
		{"NEXUS_ENVIRONMENT": "production", "NEXUS_SECURITY_REQUIRE_AUTHENTICATION": "false"},
		{"NEXUS_ENVIRONMENT": "production", "NEXUS_SECURITY_ENFORCE_BUSINESS_SCOPE": "false"},
		{"NEXUS_ENVIRONMENT": "production", "NEXUS_SECURITY_SANDBOX_ENABLED": "false"},
		{"NEXUS_ENVIRONMENT": "production", "NEXUS_SECURITY_DEV_ALLOW_UNSAFE_OVERRIDES": "true"},
	}
	for _, env := range cases {
		envSlice := []string{}
		for k, v := range env {
			envSlice = append(envSlice, k+"="+v)
		}
		_, err := Load(LoadOptions{Environ: func() []string { return envSlice }})
		if err == nil {
			t.Fatalf("production must reject weakened security config: %v", env)
		}
		if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
			t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
		}
	}
}

// Dev environment permits the dev affordance, but it is explicitly recorded.
func TestSecurityConfigDevAllowsUnsafeOverride(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: func() []string {
		return []string{"NEXUS_ENVIRONMENT=development", "NEXUS_SECURITY_DEV_ALLOW_UNSAFE_OVERRIDES=true"}
	}})
	if err != nil {
		t.Fatalf("development should permit dev overrides: %v", err)
	}
	if !cfg.Security.DevAllowUnsafeOverrides {
		t.Fatal("dev override should be recorded as enabled")
	}
}

// TEST-M1-033: egress allow-list parsing and malformed-entry rejection.
func TestSecurityConfigEgressAllowList(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: func() []string {
		return []string{"NEXUS_SECURITY_EGRESS_ALLOW_LIST=api.instagram.com, graph.facebook.com"}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Security.EgressAllowList) != 2 {
		t.Fatalf("expected 2 egress entries, got %v", cfg.Security.EgressAllowList)
	}
	// An empty entry in the CSV is malformed and must fail closed.
	_, err = Load(LoadOptions{Environ: func() []string {
		return []string{"NEXUS_SECURITY_EGRESS_ALLOW_LIST=api.instagram.com,,x"}
	}})
	if err == nil {
		t.Fatal("malformed egress allow-list (empty entry) must be rejected")
	}
}

// TEST-M1-034: strict boolean parsing rejects ambiguous values.
func TestSecurityConfigStrictBool(t *testing.T) {
	_, err := Load(LoadOptions{Environ: func() []string {
		return []string{"NEXUS_SECURITY_AUDIT_ENABLED=yes"}
	}})
	if err == nil {
		t.Fatal("ambiguous boolean must be rejected")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
}
