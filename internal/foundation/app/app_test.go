package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

func noEnv() func() []string { return func() []string { return nil } }

// TEST-M1-035: M1 security primitives are wired into startup — the app exposes a
// config snapshot, a deny-by-default egress policy, and an (empty) membership set.
func TestAppSecurityWiring(t *testing.T) {
	a, err := New(Options{Environ: func() []string {
		return []string{
			"NEXUS_ID=nx:nexus:test",
			"NEXUS_ENVIRONMENT=development",
			"NEXUS_HEALTH_ENABLED=false",
		}
	}})
	if err != nil {
		t.Fatalf("expected app to construct: %v", err)
	}
	if a.ConfigSnapshot().Fingerprint() == "" {
		t.Fatal("config snapshot must have a fingerprint")
	}
	if a.Egress() == nil || !a.Egress().Empty() {
		t.Fatal("default egress must be deny-by-default (empty allow-list)")
	}
	if a.Memberships() == nil {
		t.Fatal("membership set must be initialized")
	}
}

// TEST-M1-036: production start fails closed when a security switch is weakened.
func TestAppProductionSecurityFailsClosed(t *testing.T) {
	_, err := New(Options{Environ: func() []string {
		return []string{
			"NEXUS_ID=nx:nexus:prod",
			"NEXUS_ENVIRONMENT=production",
			"NEXUS_SECURITY_REQUIRE_AUTHENTICATION=false",
			"NEXUS_HEALTH_ENABLED=false",
		}
	}})
	if err == nil {
		t.Fatal("production with weakened security must fail to start")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
}

// TEST-M0-001: valid configuration starts the application and reaches a running
// lifecycle state.
func TestAppStartsWithValidConfig(t *testing.T) {
	// Use a port of 0-equivalent by disabling health to avoid binding a fixed
	// port during tests; lifecycle is still exercised.
	a, err := New(Options{
		Environ: func() []string {
			return []string{
				"NEXUS_ID=nx:nexus:test",
				"NEXUS_ENVIRONMENT=development",
				"NEXUS_HEALTH_ENABLED=false",
			}
		},
	})
	if err != nil {
		t.Fatalf("expected app to construct, got %v", err)
	}
	if a.Config().Nexus.ID != "nx:nexus:test" {
		t.Fatalf("unexpected nexus id: %s", a.Config().Nexus.ID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan lifecycle.ExitCode, 1)
	go func() { done <- a.Run(ctx) }()

	// Wait for RUNNING.
	deadline := time.After(2 * time.Second)
	for a.Lifecycle().State() != lifecycle.StateRunning {
		select {
		case <-deadline:
			t.Fatalf("app did not reach RUNNING; state=%s", a.Lifecycle().State())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	cancel()
	select {
	case code := <-done:
		if code != lifecycle.ExitOK {
			t.Fatalf("expected clean exit, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app did not shut down")
	}
}

// TEST-M0-002 / TEST-M0-003: invalid or missing required configuration fails
// safely (no App constructed).
func TestAppInvalidConfigFailsSafely(t *testing.T) {
	_, err := New(Options{Environ: func() []string {
		return []string{"NEXUS_ENVIRONMENT=banana"}
	}})
	if err == nil {
		t.Fatal("expected error for invalid configuration")
	}
}

func TestAppMissingConfigFileFailsSafely(t *testing.T) {
	_, err := New(Options{
		ConfigFile: "/nonexistent/nexus.json",
		Environ:    noEnv(),
	})
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

// TEST-M0-006 / TEST-M0-007: health and readiness are reachable end-to-end.
func TestAppHealthEndpoints(t *testing.T) {
	a, err := New(Options{
		Environ: func() []string {
			return []string{
				"NEXUS_ID=nx:nexus:test",
				"NEXUS_HEALTH_ENABLED=true",
				"NEXUS_HEALTH_HOST=127.0.0.1",
				"NEXUS_HEALTH_PORT=18080",
			}
		},
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan lifecycle.ExitCode, 1)
	go func() { done <- a.Run(ctx) }()

	// Wait until the health server responds.
	client := &http.Client{Timeout: time.Second}
	var liveOK bool
	deadline := time.After(3 * time.Second)
	for !liveOK {
		select {
		case <-deadline:
			t.Fatal("health endpoint did not come up")
		default:
			resp, err := client.Get("http://127.0.0.1:18080/health")
			if err == nil {
				var body map[string]any
				_ = json.NewDecoder(resp.Body).Decode(&body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					liveOK = true
				}
			}
			if !liveOK {
				time.Sleep(10 * time.Millisecond)
			}
		}
	}

	// Readiness should be 200 once running.
	var readyCode int
	readyDeadline := time.After(2 * time.Second)
	for {
		resp, err := client.Get("http://127.0.0.1:18080/readiness")
		if err == nil {
			readyCode = resp.StatusCode
			resp.Body.Close()
			if readyCode == http.StatusOK {
				break
			}
		}
		select {
		case <-readyDeadline:
			t.Fatalf("readiness did not become 200, last=%d", readyCode)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("app did not shut down")
	}
}

// TEST-M0-012 (file+env isolation at app level): a config file is honored.
func TestAppLoadsConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := []byte(`{"nexus":{"id":"nx:nexus:file","environment":"staging"},"health":{"enabled":false}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(Options{
		ConfigFile: path,
		Environ:    noEnv(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Config().Nexus.ID != "nx:nexus:file" {
		t.Fatalf("expected file nexus id, got %s", a.Config().Nexus.ID)
	}
	if a.Config().Nexus.Environment != "staging" {
		t.Fatalf("expected staging, got %s", a.Config().Nexus.Environment)
	}
}
