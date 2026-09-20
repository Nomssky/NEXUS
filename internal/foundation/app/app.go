// Package app wires the M0 foundation together into a runnable process.
//
// It is intentionally thin: load & validate configuration, build the structured
// logger, start the health/readiness surface, and manage lifecycle. It contains
// NO autonomous intelligence and NO later-milestone components.
//
// Boot sequence (mission §11 / plan §9 subset for M0):
//
//	START -> INITIALIZE (config, logging) -> READY -> RUNNING
//	     -> SHUTDOWN_REQUESTED -> DRAINING -> STOPPED
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/version"
)

// Options controls application startup. It exists so tests can inject paths and
// environments deterministically.
type Options struct {
	// ConfigFile is an optional path to a JSON configuration file.
	ConfigFile string
	// Environ overrides the process environment (for tests).
	Environ func() []string
	// Stdout/Stderr override output sinks (for tests). Defaults to os.Stdout/Stderr.
	Stdout *os.File
	Stderr *os.File
}

// App is a runnable NEXUS process instance.
type App struct {
	cfg    config.Config
	log    *logging.Logger
	health *health.Server
	life   *lifecycle.Manager
	srv    *http.Server
	opts   Options
}

// New loads configuration and constructs an App. It performs startup validation;
// on invalid/missing required configuration it returns a canonical VALIDATION
// error and does NOT construct a partially valid App.
func New(opts Options) (*App, error) {
	cfg, err := config.Load(config.LoadOptions{
		FilePath: opts.ConfigFile,
		Environ:  opts.Environ,
	})
	if err != nil {
		return nil, err
	}

	out := os.Stdout
	if opts.Stdout != nil {
		out = opts.Stdout
	}

	log := logging.New(logging.Options{
		Out:      out,
		MinLevel: logging.ParseLevel(cfg.Logging.Level),
		Format:   cfg.Logging.Format,
		Service:  "nexus",
		NexusID:  cfg.Nexus.ID,
	})

	a := &App{
		cfg:    cfg,
		log:    log,
		health: health.NewServer(),
		life: lifecycle.New(lifecycle.Options{
			ShutdownTimeout: time.Duration(cfg.Lifecycle.ShutdownTimeoutSeconds) * time.Second,
		}),
		opts: opts,
	}
	a.registerHooks()
	return a, nil
}

// Config returns the effective configuration.
func (a *App) Config() config.Config { return a.cfg }

// Logger returns the structured logger.
func (a *App) Logger() *logging.Logger { return a.log }

// Health returns the health server.
func (a *App) Health() *health.Server { return a.health }

// Lifecycle returns the lifecycle manager.
func (a *App) Lifecycle() *lifecycle.Manager { return a.life }

func (a *App) registerHooks() {
	a.life.RegisterHook(lifecycle.Hook{
		Name: "health-server",
		Init: func(_ context.Context) error {
			if !a.cfg.Health.Enabled {
				a.log.Info("health surface disabled by configuration", logging.Fields{})
				return nil
			}
			addr := net.JoinHostPort(a.cfg.Health.Host, fmt.Sprintf("%d", a.cfg.Health.Port))
			a.srv = &http.Server{
				Addr:              addr,
				Handler:           a.health.Mux(),
				ReadHeaderTimeout: 5 * time.Second,
			}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return nerrors.Dependency("health.listen_failed",
					fmt.Sprintf("cannot bind health endpoint on %s", addr)).WithDetail("addr", addr)
			}
			go func() {
				if serveErr := a.srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					a.log.Error("health server stopped unexpectedly", logging.Fields{
						Context: map[string]any{"err": serveErr.Error()},
					})
				}
			}()
			a.log.Info("health endpoint listening", logging.Fields{
				Context: map[string]any{"addr": addr},
			})
			return nil
		},
		Close: func(ctx context.Context) error {
			if a.srv == nil {
				return nil
			}
			return a.srv.Shutdown(ctx)
		},
	})
}

// Run starts the application and blocks until a shutdown signal is received,
// then returns the process exit code.
func (a *App) Run(ctx context.Context) lifecycle.ExitCode {
	a.log.Info("nexus starting", logging.Fields{
		Context: map[string]any{
			"version":     version.Version,
			"commit":      version.Commit,
			"environment": a.cfg.Nexus.Environment,
		},
	})

	startCtx, cancel := context.WithTimeout(ctx, a.shutdownBudgetOrDefault())
	defer cancel()
	if err := a.life.Start(startCtx); err != nil {
		a.log.Error("startup failed", logging.Fields{Context: map[string]any{"err": err.Error()}})
		return lifecycle.ExitFailure
	}

	// Decline readiness during shutdown.
	a.health.MarkReady()
	a.life.OnStateChange(func(_, to lifecycle.State) {
		if to == lifecycle.StateShutdownRequested || to == lifecycle.StateDraining {
			a.health.MarkNotReady()
		}
	})
	a.log.Info("nexus ready", logging.Fields{})

	code := a.life.WaitForShutdownSignal(ctx)
	a.log.Info("nexus stopped", logging.Fields{})
	return code
}

func (a *App) shutdownBudgetOrDefault() time.Duration {
	d := time.Duration(a.cfg.Lifecycle.ShutdownTimeoutSeconds) * time.Second
	if d <= 0 {
		return 30 * time.Second
	}
	return d
}
