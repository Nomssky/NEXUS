package launcher

import (
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
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
