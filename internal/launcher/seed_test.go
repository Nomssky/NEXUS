package launcher

import (
	"context"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
)

// TEST-LAUNCHER-SEED-01: launcher.New seeds a simulated default model when the
// engine's model registry is empty, so the documented submit → GET result
// lifecycle can reach a completed terminal state (regression guard for the
// no-registration-path gap; without the seed every request fails honestly at
// model invocation and never completes).
func TestLauncherSeedsSimulatedModel(t *testing.T) {
	l := New(Options{
		Config:    config.Config{},
		Logger:    logging.New(logging.Options{}),
		Health:    health.NewServer(),
		Lifecycle: lifecycle.New(lifecycle.Options{}),
		Addr:      ":0",
	})

	if l.Engine().ModelRegistry().ModelCount() == 0 {
		t.Fatal("expected a seeded default model, registry is empty")
	}
}

// TEST-LAUNCHER-SEED-02: the seeded provider starts in the configured
// models.seeded_provider_status, so a provider failure is reachable through
// the shipped binary's own configuration surface without registering a real
// provider (PROVIDER_CONTRACTS §12.1).
func TestLauncherSeedsProviderInConfiguredStatus(t *testing.T) {
	l := New(Options{
		Config:    config.Config{Models: config.ModelsConfig{SeededProviderStatus: "offline"}},
		Logger:    logging.New(logging.Options{}),
		Health:    health.NewServer(),
		Lifecycle: lifecycle.New(lifecycle.Options{}),
		Addr:      ":0",
	})

	if l.Engine().ModelRegistry().ModelCount() == 0 {
		t.Fatal("expected a seeded default model, registry is empty")
	}

	_, _, err := l.Engine().ModelRouter().Invoke(context.Background(),
		&modelrouter.RoutingRequest{
			RequestID:    "req-seed-offline",
			RequiredCaps: []modelrouter.ModelCapability{modelrouter.CapabilityReasoning, modelrouter.CapabilityToolCalling},
			PreferLocal:  true,
		},
		&modelrouter.GenerateRequest{RequestID: "req-seed-offline"},
	)
	if err == nil {
		t.Fatal("expected the seeded offline provider to fail the invocation")
	}
	if !strings.Contains(err.Error(), "provider simulated is offline") {
		t.Fatalf("expected the offline cause to surface, got %q", err.Error())
	}
}

// TEST-LAUNCHER-SEED-03: an unset status keeps the shipped behaviour — the
// seeded provider is healthy and the submit → GET result lifecycle can still
// reach a completed terminal state.
func TestLauncherSeedsHealthyProviderByDefault(t *testing.T) {
	l := New(Options{
		Config:    config.Config{},
		Logger:    logging.New(logging.Options{}),
		Health:    health.NewServer(),
		Lifecycle: lifecycle.New(lifecycle.Options{}),
		Addr:      ":0",
	})

	_, _, err := l.Engine().ModelRouter().Invoke(context.Background(),
		&modelrouter.RoutingRequest{
			RequestID:    "req-seed-default",
			RequiredCaps: []modelrouter.ModelCapability{modelrouter.CapabilityReasoning, modelrouter.CapabilityToolCalling},
			PreferLocal:  true,
		},
		&modelrouter.GenerateRequest{RequestID: "req-seed-default"},
	)
	if err != nil {
		t.Fatalf("expected the default seeded provider to succeed, got %v", err)
	}
}
