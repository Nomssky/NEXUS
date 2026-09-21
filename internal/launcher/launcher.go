// Package launcher wires the Core Runtime and HTTP Gateway together,
// providing a runnable NEXUS system.
//
// Boot sequence:
//
//	START → INITIALIZE (config, logging) → WIRE (core, gateway) → RUNNING
//	     → SHUTDOWN_REQUESTED → DRAINING → STOPPED
package launcher

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
	"github.com/Nomssky/NEXUS/internal/gateway"
)

// Launcher wires the Core Runtime and HTTP Gateway into a running system.
type Launcher struct {
	cfg     config.Config
	log     *logging.Logger
	health  *health.Server
	engine  *core.Engine
	gateway *gateway.Server
	life    *lifecycle.Manager
	initErr error
}

// Options configures the launcher.
type Options struct {
	Config    config.Config
	Logger    *logging.Logger
	Health    *health.Server
	Lifecycle *lifecycle.Manager
	Addr      string // HTTP listen address (e.g., ":8080")
}

// New creates a new launcher with all components wired.
func New(opts Options) *Launcher {
	engine, err := core.NewEngine(&opts.Config)
	if err != nil {
		// Store error; will be returned by Start
		return &Launcher{
			cfg:     opts.Config,
			log:     opts.Logger,
			health:  opts.Health,
			life:    opts.Lifecycle,
			initErr: err,
		}
	}
	gw := gateway.NewServer(engine, opts.Addr)

	return &Launcher{
		cfg:     opts.Config,
		log:     opts.Logger,
		health:  opts.Health,
		engine:  engine,
		gateway: gw,
		life:    opts.Lifecycle,
	}
}

// Start starts the Core Runtime and HTTP Gateway.
func (l *Launcher) Start(ctx context.Context) error {
	if l.initErr != nil {
		return fmt.Errorf("initialization failed: %w", l.initErr)
	}

	// Start the core engine
	if err := l.engine.Start(ctx); err != nil {
		return fmt.Errorf("core engine start: %w", err)
	}
	l.log.Info("core engine started", logging.Fields{})

	// Start the HTTP gateway
	go func() {
		if err := l.gateway.Start(ctx); err != nil && err != http.ErrServerClosed {
			l.log.Error("gateway server error", logging.Fields{
				Context: map[string]any{"err": err.Error()},
			})
		}
	}()
	l.log.Info("gateway started", logging.Fields{
		Context: map[string]any{"addr": l.cfg.Health.Host},
	})

	return nil
}

// Stop gracefully shuts down the gateway and core engine.
func (l *Launcher) Stop(ctx context.Context) error {
	// Stop gateway first (stop accepting new requests)
	if err := l.gateway.Stop(ctx); err != nil {
		l.log.Error("gateway stop error", logging.Fields{
			Context: map[string]any{"err": err.Error()},
		})
	}

	// Then stop the core engine (drain in-flight)
	if err := l.engine.Stop(ctx); err != nil {
		l.log.Error("engine stop error", logging.Fields{
			Context: map[string]any{"err": err.Error()},
		})
	}

	return nil
}

// Engine returns the core engine for direct access.
func (l *Launcher) Engine() *core.Engine {
	return l.engine
}

// Gateway returns the HTTP gateway for direct access.
func (l *Launcher) Gateway() *gateway.Server {
	return l.gateway
}

// Run starts the launcher and blocks until shutdown signal.
func (l *Launcher) Run(ctx context.Context) lifecycle.ExitCode {
	l.log.Info("nexus system starting", logging.Fields{})

	// Register lifecycle hook
	l.life.RegisterHook(lifecycle.Hook{
		Name: "core-runtime",
		Init: func(ctx context.Context) error {
			return l.Start(ctx)
		},
		Close: func(ctx context.Context) error {
			return l.Stop(ctx)
		},
	})

	// Start lifecycle
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := l.life.Start(startCtx); err != nil {
		l.log.Error("startup failed", logging.Fields{
			Context: map[string]any{"err": err.Error()},
		})
		return lifecycle.ExitFailure
	}

	l.health.MarkReady()
	l.log.Info("nexus system ready", logging.Fields{
		Context: map[string]any{
			"addr":          l.cfg.Health.Host,
			"engine_status": string(l.engine.Status()),
		},
	})

	// Wait for shutdown
	code := l.life.WaitForShutdownSignal(ctx)
	l.log.Info("nexus system stopped", logging.Fields{})
	return code
}

// defaultAddr returns the default HTTP listen address.
func defaultAddr(cfg config.Config) string {
	host := cfg.Health.Host
	if host == "" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, "8080")
}
