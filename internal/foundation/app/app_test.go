package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
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

// TEST-APP-PERSIST-01: with storage.data_dir set, registry mutations write
// through to the file store and are restored on the next boot.
func TestAppRegistryDurability(t *testing.T) {
	dir := t.TempDir()
	env := func() []string {
		return []string{
			"NEXUS_ID=nx:nexus:test",
			"NEXUS_ENVIRONMENT=development",
			"NEXUS_HEALTH_ENABLED=false",
			"NEXUS_DATA_DIR=" + dir,
		}
	}
	a, err := New(Options{Environ: env})
	if err != nil {
		t.Fatalf("first boot: %v", err)
	}
	ident, err := identity.New("nx:nexus:test", identity.TypeHuman, "Owner", identity.GlobalScope())
	if err != nil {
		t.Fatal(err)
	}
	created, err := a.Registry().CreateIdentity(ident, "test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close first boot: %v", err)
	}

	a2, err := New(Options{Environ: env})
	if err != nil {
		t.Fatalf("second boot: %v", err)
	}
	got, ok := a2.Registry().GetIdentity(created.ID)
	if !ok {
		t.Fatal("identity must survive a restart when data_dir is configured")
	}
	if got.DisplayName != "Owner" {
		t.Errorf("restored identity: %+v", got)
	}
}

// TEST-APP-PERSIST-02: fail-closed boot — a data_dir that cannot be opened
// (here: an existing file) aborts construction instead of silently degrading
// to in-memory records.
func TestAppDataDirFailureFailsClosed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(Options{Environ: func() []string {
		return []string{
			"NEXUS_ID=nx:nexus:test",
			"NEXUS_ENVIRONMENT=development",
			"NEXUS_HEALTH_ENABLED=false",
			"NEXUS_DATA_DIR=" + file,
		}
	}})
	if err == nil {
		t.Fatal("expected boot to fail closed when data_dir cannot be opened")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryInternalFailure {
		t.Errorf("expected INTERNAL_FAILURE, got %s", nerrors.CategoryOf(err))
	}
}

// A startup that fails after the store opened (here: corrupt membership
// hydration) must release storage ownership, so an operator can fix the
// data and boot again without hunting for a phantom writer.
func TestAppPartialStartupReleasesStorageOwnership(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if err := st.Put(&store.Record{
		ID: "nexus.membership:broken", Type: store.RecordTypeMembership,
		Status: store.RecordStatusActive, Data: []byte("{not json"),
	}); err != nil {
		t.Fatalf("seed corrupt membership: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	env := func() []string {
		return []string{
			"NEXUS_ID=nx:nexus:test",
			"NEXUS_ENVIRONMENT=development",
			"NEXUS_HEALTH_ENABLED=false",
			"NEXUS_DATA_DIR=" + dir,
		}
	}
	if _, err := New(Options{Environ: env}); err == nil {
		t.Fatal("corrupt membership data must abort startup")
	}

	// The failed boot must not leave ownership behind.
	probe, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("failed startup retained storage ownership: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatalf("close probe: %v", err)
	}

	// Removing the corrupt record makes the installation bootable again.
	if err := os.Remove(filepath.Join(dir, string(store.RecordTypeMembership), "nexus.membership:broken.json")); err != nil {
		t.Fatalf("remove corrupt record: %v", err)
	}
	a, err := New(Options{Environ: env})
	if err != nil {
		t.Fatalf("boot after repair: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close repaired app: %v", err)
	}
}

// The lifecycle close hook releases storage ownership, and the store refuses
// further operations instead of writing without the lock.
func TestAppLifecycleShutdownReleasesStorageOwnership(t *testing.T) {
	dir := t.TempDir()
	a, err := New(Options{Environ: func() []string {
		return []string{
			"NEXUS_ID=nx:nexus:test",
			"NEXUS_ENVIRONMENT=development",
			"NEXUS_HEALTH_ENABLED=false",
			"NEXUS_DATA_DIR=" + dir,
		}
	}})
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	if a.Store() == nil {
		t.Fatal("durable boot must expose its store")
	}

	if code := a.Lifecycle().Shutdown(context.Background()); code != lifecycle.ExitOK {
		t.Fatalf("shutdown exit code = %v, want ExitOK", code)
	}
	if err := a.Store().Put(&store.Record{ID: "after", Type: store.RecordTypeMemory, Status: store.RecordStatusActive}); err == nil {
		t.Fatal("store operation after lifecycle shutdown must fail")
	}
	// Ownership is available to the next boot immediately, and Close remains
	// safe to call again.
	next, err := New(Options{Environ: func() []string {
		return []string{
			"NEXUS_ID=nx:nexus:test",
			"NEXUS_ENVIRONMENT=development",
			"NEXUS_HEALTH_ENABLED=false",
			"NEXUS_DATA_DIR=" + dir,
		}
	}})
	if err != nil {
		t.Fatalf("next boot after shutdown: %v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatalf("close next boot: %v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatalf("second Close must be idempotent: %v", err)
	}
}
