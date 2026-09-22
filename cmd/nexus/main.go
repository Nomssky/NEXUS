// Command nexus is the NEXUS process entrypoint.
//
// It loads configuration, initializes logging, wires the Core Runtime
// and HTTP Gateway, and manages the process lifecycle.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/Nomssky/NEXUS/internal/foundation/app"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/version"
	"github.com/Nomssky/NEXUS/internal/launcher"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configFile  = flag.String("config", "", "path to a JSON configuration file (optional)")
		showVersion = flag.Bool("version", false, "print version and exit")
		httpAddr    = flag.String("http-addr", "", "HTTP listen address (default: 0.0.0.0:8080)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("nexus %s (%s)\n", version.Version, version.Commit)
		return int(lifecycle.ExitOK)
	}

	// Load configuration using the foundation app package
	a, err := app.New(app.Options{ConfigFile: *configFile})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus: startup aborted: %s\n", safeMessage(err))
		return int(lifecycle.ExitFailure)
	}

	cfg := a.Config()
	log := a.Logger()
	healthSrv := a.Health()
	life := a.Lifecycle()

	// Determine HTTP address
	addr := *httpAddr
	if addr == "" {
		addr = defaultAddr(cfg)
	}

	// Wire the launcher. Control API key is a secret: loaded from the
	// environment only (never from the config file — config holds SecretRef
	// references, not raw secrets).
	//
	// Identity binding (A6): pass security flags and foundation identity
	// components so the gateway enforces authenticated actor + membership on
	// scoped paths when require_authentication / enforce_business_scope are on.
	// The authenticator starts empty (fail-closed until credentials are
	// registered); memberships are the process membership set from app.
	launch := launcher.New(launcher.Options{
		Config:        cfg,
		Logger:        log,
		Health:        healthSrv,
		Lifecycle:     life,
		Addr:          addr,
		ControlAPIKey: os.Getenv(config.EnvControlAPIKey),

		Authenticator:         identity.NewLocalAuthenticator(),
		Memberships:           a.Memberships(),
		RequireAuthentication: cfg.Security.RequireAuthentication,
		EnforceBusinessScope:  cfg.Security.EnforceBusinessScope,
	})

	return int(launch.Run(context.Background()))
}

func defaultAddr(cfg config.Config) string {
	host := cfg.Health.Host
	if host == "" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, "8080")
}

func safeMessage(err error) string {
	var ne *nerrors.Error
	if errors.As(err, &ne) {
		return fmt.Sprintf("[%s] %s", ne.Category, ne.Message)
	}
	return "internal startup error"
}
