// Command nexus is the NEXUS process entrypoint.
//
// M0 scope: foundation only. It loads and validates configuration, initializes
// structured logging, exposes health/readiness, and manages process lifecycle.
// It contains NO autonomous intelligence and NO later-milestone components.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Nomssky/NEXUS/internal/foundation/app"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configFile  = flag.String("config", "", "path to a JSON configuration file (optional)")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("nexus %s (%s)\n", version.Version, version.Commit)
		return int(lifecycle.ExitOK)
	}

	a, err := app.New(app.Options{ConfigFile: *configFile})
	if err != nil {
		// Safe failure: configuration errors happen before the structured logger
		// exists. Emit a secret-free diagnostic to stderr and exit non-zero.
		// Never construct a partially valid application.
		fmt.Fprintf(os.Stderr, "nexus: startup aborted: %s\n", safeMessage(err))
		return int(lifecycle.ExitFailure)
	}

	return int(a.Run(context.Background()))
}

// safeMessage renders an error for pre-logger output without leaking details
// beyond the canonical code/category/message.
func safeMessage(err error) string {
	var ne *nerrors.Error
	if errors.As(err, &ne) {
		return fmt.Sprintf("[%s] %s", ne.Category, ne.Message)
	}
	return "internal startup error"
}
