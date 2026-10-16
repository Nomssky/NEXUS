package config

import (
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

func TestRateLimitDefaultsEnabled(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: env()})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.RateLimit.Enabled {
		t.Fatal("rate limiting must be enabled by default")
	}
	if cfg.RateLimit.SubmitPerMinute != 60 || cfg.RateLimit.RequestPerMinute != 600 || cfg.RateLimit.PerIPPerMinute != 6000 {
		t.Fatalf("unexpected rate-limit defaults: %+v", cfg.RateLimit)
	}
	if len(cfg.HTTP.CORSAllowedOrigins) != 0 {
		t.Fatalf("CORS must default to same-origin (empty), got %v", cfg.HTTP.CORSAllowedOrigins)
	}
}

func TestRateLimitEnvOverrides(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: env(
		"NEXUS_RATE_LIMIT_SUBMIT_PER_MINUTE=5",
		"NEXUS_RATE_LIMIT_REQUEST_PER_MINUTE=50",
		"NEXUS_RATE_LIMIT_PER_IP_PER_MINUTE=150",
	)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.RateLimit.SubmitPerMinute != 5 || cfg.RateLimit.RequestPerMinute != 50 || cfg.RateLimit.PerIPPerMinute != 150 {
		t.Fatalf("env overrides not applied: %+v", cfg.RateLimit)
	}
}

func TestRateLimitDisableViaEnv(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: env("NEXUS_RATE_LIMIT_ENABLED=false")})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.RateLimit.Enabled {
		t.Fatal("rate limiting should be disabled via env")
	}
}

func TestRateLimitEnabledWithZeroRateRejected(t *testing.T) {
	_, err := Load(LoadOptions{Environ: env("NEXUS_RATE_LIMIT_SUBMIT_PER_MINUTE=0")})
	if err == nil {
		t.Fatal("a zero rate with limiting enabled must fail validation")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
}

func TestRateLimitDisabledAllowsZeroRates(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: env(
		"NEXUS_RATE_LIMIT_ENABLED=false",
		"NEXUS_RATE_LIMIT_SUBMIT_PER_MINUTE=0",
	)})
	if err != nil {
		t.Fatalf("disabled limiter with zero rates must load: %v", err)
	}
	if cfg.RateLimit.Enabled {
		t.Fatal("expected disabled")
	}
}

func TestRateLimitInvalidEnvIntRejected(t *testing.T) {
	_, err := Load(LoadOptions{Environ: env("NEXUS_RATE_LIMIT_SUBMIT_PER_MINUTE=abc")})
	if err == nil {
		t.Fatal("a non-integer rate env value must fail")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
}

func TestCORSOriginsEnvAndValidation(t *testing.T) {
	cfg, err := Load(LoadOptions{Environ: env(
		"NEXUS_HTTP_CORS_ALLOWED_ORIGINS=https://app.example,https://admin.example:8443",
	)})
	if err != nil {
		t.Fatalf("valid origins must load: %v", err)
	}
	if len(cfg.HTTP.CORSAllowedOrigins) != 2 {
		t.Fatalf("expected 2 origins, got %v", cfg.HTTP.CORSAllowedOrigins)
	}
}

func TestCORSInvalidOriginsRejected(t *testing.T) {
	cases := []string{
		"*",                        // wildcard
		"https://app.example/path", // path
		"app.example",              // no scheme
		"https://app.example?x=1",  // query
		"https://app.example#frag", // fragment
		"ftp://app.example",        // non-http scheme
	}
	for _, bad := range cases {
		_, err := Load(LoadOptions{Environ: env("NEXUS_HTTP_CORS_ALLOWED_ORIGINS=" + bad)})
		if err == nil {
			t.Fatalf("origin %q must be rejected", bad)
		}
		if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
			t.Fatalf("origin %q: expected VALIDATION, got %s", bad, nerrors.CategoryOf(err))
		}
	}
}

func TestCORSFromFile(t *testing.T) {
	path := writeConfigFile(t, `{"http":{"cors_allowed_origins":["https://app.example"]}}`)
	cfg, err := Load(LoadOptions{FilePath: path, Environ: env()})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.HTTP.CORSAllowedOrigins) != 1 || cfg.HTTP.CORSAllowedOrigins[0] != "https://app.example" {
		t.Fatalf("file origins not applied: %v", cfg.HTTP.CORSAllowedOrigins)
	}
}

func TestRateLimitFromFile(t *testing.T) {
	path := writeConfigFile(t, `{"rate_limit":{"enabled":false,"submit_per_minute":9}}`)
	cfg, err := Load(LoadOptions{FilePath: path, Environ: env()})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.RateLimit.Enabled || cfg.RateLimit.SubmitPerMinute != 9 {
		t.Fatalf("file rate-limit not applied: %+v", cfg.RateLimit)
	}
}
