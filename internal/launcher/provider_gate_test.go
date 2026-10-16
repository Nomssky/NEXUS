package launcher

import (
	"context"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
)

// F1 — the launcher must never seed a simulated provider into production. Even
// if a production configuration reaches New with an empty model registry, the
// launcher fails closed instead of substituting a simulator.
func TestLauncherProductionNeverSeedsSimulation(t *testing.T) {
	l := New(Options{
		Config:    config.Config{Nexus: config.NexusConfig{Environment: "production"}},
		Logger:    logging.New(logging.Options{}),
		Health:    health.NewServer(),
		Lifecycle: lifecycle.New(lifecycle.Options{}),
		Addr:      ":0",
	})

	err := l.Start(context.Background())
	if err == nil {
		t.Fatal("production launcher must refuse to start without a real provider")
	}
	if !strings.Contains(err.Error(), "provider") {
		t.Fatalf("expected an actionable provider error, got %q", err.Error())
	}
}

// F1 — a configured real provider whose credential environment variable is
// unset is unusable: startup fails closed with a non-sensitive error and does
// NOT fall back to a simulated provider.
func TestLauncherProductionMissingProviderSecretFailsClosed(t *testing.T) {
	const keyEnv = "NEXUS_LAUNCHER_TEST_UNSET_PROVIDER_KEY"
	t.Setenv(keyEnv, "") // present but empty → treated as unset

	l := New(Options{
		Config: config.Config{
			Nexus: config.NexusConfig{Environment: "production"},
			Models: config.ModelsConfig{Providers: []config.ModelProviderConfig{{
				ID: "openai", Endpoint: "https://api.example/v1", ModelID: "gpt-x", APIKeyEnv: keyEnv,
			}}},
		},
		Logger:    logging.New(logging.Options{}),
		Health:    health.NewServer(),
		Lifecycle: lifecycle.New(lifecycle.Options{}),
		Addr:      ":0",
	})

	err := l.Start(context.Background())
	if err == nil {
		t.Fatal("production with an unset provider credential must fail to start")
	}
	// The error names the variable, never its value.
	if !strings.Contains(err.Error(), keyEnv) {
		t.Fatalf("expected the missing credential variable to be named, got %q", err.Error())
	}
	if l.Engine() != nil && l.Engine().ModelRegistry().ModelCount() != 0 {
		t.Fatal("a failed provider registration must not be replaced by a seeded simulation")
	}
}

// F1 — development retains the deterministic simulation: an empty registry is
// still seeded (non-production is unchanged).
func TestLauncherDevelopmentStillSeedsSimulation(t *testing.T) {
	l := New(Options{
		Config:    config.Config{Nexus: config.NexusConfig{Environment: "development"}},
		Logger:    logging.New(logging.Options{}),
		Health:    health.NewServer(),
		Lifecycle: lifecycle.New(lifecycle.Options{}),
		Addr:      ":0",
	})
	if l.Engine() == nil || l.Engine().ModelRegistry().ModelCount() == 0 {
		t.Fatal("development must still seed the simulated provider when nothing is registered")
	}
}
