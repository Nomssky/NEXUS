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
	"net/http"
	"os"
	"time"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/agentintel"
	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
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
	addr    string // actual gateway listen address
	initErr error
	// startupTimeout bounds the startup barrier only (L-002). It must never
	// be inherited by components as a lifetime context — see Run.
	startupTimeout time.Duration
}

// Options configures the launcher.
type Options struct {
	Config    config.Config
	Logger    *logging.Logger
	Health    *health.Server
	Lifecycle *lifecycle.Manager
	Addr      string // HTTP listen address (e.g., ":8080")
	// ControlAPIKey is the API key required for /api/v1/control/* endpoints.
	// If empty, control endpoints are disabled (403 fail-closed), not open.
	ControlAPIKey string

	// Identity-bound authorization (A6). When RequireAuthentication or
	// EnforceBusinessScope is true, Authenticator and Memberships must be set;
	// the gateway fails closed at request time if they are missing.
	Authenticator         identity.Authenticator
	Memberships           *identity.MembershipSet
	RequireAuthentication bool
	EnforceBusinessScope  bool

	// Registry is the organization entity registry (Identity/Business/
	// Division). Optional: nil leaves the org endpoints fail-closed (503).
	Registry *identity.Registry

	// Store is the durable record store used for the agent registry too
	// (durable agent definitions across restarts, G4 Level 1). Nil makes
	// the agent definitions in-memory (Level 0 durability) and the agent
	// surface cap its guarantees on that — matching registry's Posture).
	Store store.Store

	// Capability configures the shipped capability catalogue (optional; the
	// zero value registers only the deterministic self-contained tools).
	Capability CapabilityOptions

	// GatewayOptions are passed through to the gateway server (L-002 test
	// seam for deterministic listen behavior). Nil/empty in production.
	GatewayOptions []gateway.ServerOption
}

// New creates a new launcher with all components wired.
func New(opts Options) *Launcher {
	// Identity-bound admission stages (A: chain IDENTITY/AUTHORIZATION)
	// share the A6 flags and membership store — one posture for both
	// boundaries (gateway edge, chain defense in depth).
	engine, err := core.NewEngine(&opts.Config,
		core.WithIdentity(opts.Memberships, opts.RequireAuthentication, opts.EnforceBusinessScope),
	)
	if err != nil {
		// Store error; will be returned by Start
		return &Launcher{
			cfg:            opts.Config,
			log:            opts.Logger,
			health:         opts.Health,
			life:           opts.Lifecycle,
			initErr:        err,
			startupTimeout: 30 * time.Second,
		}
	}
	// Seed the model layer when nothing is registered (docs/m6-model-router.md:
	// callers register models and providers). The shipped binary has no other
	// registration path — no model API endpoint — so without a seed every
	// request fails honestly at model invocation (E-008, no model) and the
	// documented submit → GET result lifecycle can never reach a completed
	// terminal state. The seed uses modelrouter's simulated LocalProvider
	// stub; real inference requires registering real models and providers per
	// m6 (the warning below keeps the simulation explicit).
	//
	// The seeded provider's starting health comes from
	// models.seeded_provider_status (PROVIDER_CONTRACTS §12): ""/"healthy" is
	// the shipped behaviour, "offline" makes the seed fail at the provider so
	// a deterministic failed-provider run is reachable without registering a
	// real provider. Only an existing ProviderStatus is selected here.
	if engine.ModelRegistry().ModelCount() == 0 {
		seedStatus := modelrouter.ProviderStatus(opts.Config.Models.SeededProviderStatus)
		if seedStatus == "" {
			seedStatus = modelrouter.ProviderStatusHealthy
		}
		// Provider selection: the plain simulator answers with empty
		// completions (today's shipped behaviour); the scripted decision-table
		// provider is the deterministic simulation used to exercise the agent
		// intelligence control loop without a real LLM
		// (AGENT_INTELLIGENCE_CONTRACTS §16). Both are simulations behind the
		// provider abstraction; neither is a production provider.
		seedID := "simulated"
		var simProvider modelrouter.Provider
		if opts.Config.Models.SeededProviderMode == "scripted" {
			seedID = "scripted"
			simProvider = modelrouter.NewScriptedProvider(modelrouter.ProviderConfig{ID: seedID, Status: seedStatus})
		} else {
			simProvider = modelrouter.NewLocalProvider(modelrouter.ProviderConfig{ID: seedID, Status: seedStatus})
		}
		if regErr := engine.ModelRegistry().RegisterModel(&modelrouter.ModelDefinition{
			ID:         seedID + ":default",
			ProviderID: simProvider.Identify(),
			Capabilities: []modelrouter.ModelCapability{
				modelrouter.CapabilityReasoning,
				modelrouter.CapabilityToolCalling,
				// Structured output: the agent intelligence loop asks for a
				// JSON decision (AGENT_INTELLIGENCE_CONTRACTS §6). The plain
				// simulator still answers with an empty completion — the loop
				// then fails honestly at the protocol check — while the
				// scripted simulation provider answers with a structured plan
				// or action.
				modelrouter.CapabilityStructuredOutput,
			},
			Runtime: modelrouter.RuntimeLocal,
			Status:  modelrouter.ModelStatusActive,
		}); regErr != nil {
			if opts.Logger != nil {
				opts.Logger.Error("failed to seed simulated default model", logging.Fields{
					Context: map[string]any{"err": regErr.Error()},
				})
			}
		} else {
			engine.ModelRouter().RegisterProvider(simProvider)
			if opts.Logger != nil {
				opts.Logger.Warn("no model registered: seeded simulated default model", logging.Fields{
					Context: map[string]any{
						"model_id": seedID + ":default",
						"provider": seedID,
						"status":   seedStatus,
						"note":     "simulated execution via LocalProvider stub; register real models per docs/m6-model-router.md for real inference",
					},
				})
			}
		}
	}
	gwOpts := []gateway.ServerOption{
		gateway.WithControlAPIKey(opts.ControlAPIKey),
		gateway.WithIdentity(opts.Authenticator, opts.Memberships),
		gateway.WithRegistry(opts.Registry),
		gateway.WithNexusID(opts.Config.Nexus.ID),
		gateway.WithRequireAuthentication(opts.RequireAuthentication),
		gateway.WithEnforceBusinessScope(opts.EnforceBusinessScope),
	}
	// Agent execution layer v1: durable agent definitions + runtime wired
	// through the canonical request path (governance inherits unchanged).
	agentTools := tool.NewToolRegistry()
	agentReg, err := agentexec.OpenRegistry(opts.Config.Nexus.ID, opts.Registry, agentTools, opts.Store)
	if err != nil {
		return &Launcher{cfg: opts.Config, log: opts.Logger, health: opts.Health, life: opts.Lifecycle, initErr: err}
	}
	agentRt := agentexec.NewRuntime(agentReg, agentTools, engine.ModelRouter(), engine.EventBus())
	gwOpts = append(gwOpts, gateway.WithAgentExecution(agentReg, agentRt))

	// Capability & Tool Platform v1 (contracts/CAPABILITY_TOOL_CONTRACTS.md):
	// the SAME tool registry gains validated manifests and mediated adapters,
	// and the agent execution runtime's tool boundary runs through the
	// platform. There is one registry, one invocation path, one credential
	// boundary and one result/redaction pipeline. Governance stays upstream
	// and unchanged — the platform performs no policy evaluation of its own.
	platform, capErr := capabilityPlatform(os.Getenv, opts.Config.Storage.DataDir,
		agentTools, opts.Memberships, engine.EventBus(), opts.Capability)
	if capErr != nil {
		if opts.Logger != nil {
			opts.Logger.Error("capability platform not registered", logging.Fields{
				Context: map[string]any{"err": capErr.Error()},
			})
		}
	} else {
		agentRt.Platform = platform
		// Registered secret values are handed to the platform for redaction.
		if v := os.Getenv("NEXUS_TOOL_CREDENTIAL_VALUE"); v != "" {
			platform.RegisterSecret(v)
		}
		gwOpts = append(gwOpts, gateway.WithCapabilityPlatform(platform))
	}

	// Agent intelligence layer v1: the bounded control loop. It runs inside
	// the same admitted request pipeline and reuses the agent execution
	// primitives (tool boundary + delegation) rather than a second framework.
	intelMem, err := agentintel.OpenMemory(opts.Store)
	if err != nil {
		return &Launcher{cfg: opts.Config, log: opts.Logger, health: opts.Health, life: opts.Lifecycle, initErr: err}
	}
	intelRt := agentintel.New(agentReg, agentRt,
		&agentintel.Decision{Router: engine.ModelRouter()}, engine.EventBus(), intelMem)
	if platform != nil {
		// The bounded capability catalog reaches the decision prompt as
		// informational discovery; it grants nothing (contract §4/§31).
		intelRt.Catalog = capabilityCatalog{platform: platform}
	}
	gwOpts = append(gwOpts, gateway.WithIntelligence(intelRt))
	gwOpts = append(gwOpts, opts.GatewayOptions...)
	gw := gateway.NewServer(engine, opts.Addr, gwOpts...)

	return &Launcher{
		cfg:     opts.Config,
		log:     opts.Logger,
		health:  opts.Health,
		engine:  engine,
		gateway: gw,
		life:    opts.Lifecycle,
		addr:    opts.Addr,
		// C-1: default 30s, bounds the startup barrier only.
		startupTimeout: 30 * time.Second,
	}
}

// Start starts the Core Runtime and HTTP Gateway.
//
// L-002 deterministic startup barrier: Start does not return success until
// the gateway HTTP listener is established (gateway.Ready closed) or a
// definitive startup failure is observed. There is no timing window during
// which a gateway startup error can be silently lost.
//
// L-004: on any startup failure (gateway error, not-ready exit, cancellation)
// Start aborts cleanly — it stops partially started owned components so no
// engine/gateway is left running after Start returns an error.
func (l *Launcher) Start(ctx context.Context) error {
	if l.initErr != nil {
		return fmt.Errorf("initialization failed: %w", l.initErr)
	}

	// Start the core engine
	if err := l.engine.Start(ctx); err != nil {
		return fmt.Errorf("core engine start: %w", err)
	}
	l.log.Info("core engine started", logging.Fields{})

	// Start the HTTP gateway — capture error for propagation. The goroutine
	// outlives this function so post-startup runtime errors are still logged.
	gwErr := make(chan error, 1)
	go func() {
		if err := l.gateway.Start(ctx); err != nil && err != http.ErrServerClosed {
			gwErr <- err
			l.log.Error("gateway server error", logging.Fields{
				Context: map[string]any{"err": err.Error()},
			})
		}
		close(gwErr)
	}()

	// Deterministic barrier: wait for listener established (Ready) or a
	// definitive startup failure — never an arbitrary timer. The startup
	// deadline lives here (a local timer), NOT in the context handed to
	// components: engine/gateway receive the run-lifetime context from
	// Run (C-1 fix) and must keep serving after startup completes.
	startupTimer := time.NewTimer(l.startupTimeout)
	defer startupTimer.Stop()
	select {
	case err := <-gwErr:
		if err != nil {
			l.abortStart(ctx)
			return fmt.Errorf("gateway start: %w", err)
		}
		// Start exited without error — check whether ready was established
		// (race with close) before treating as success.
		select {
		case <-l.gateway.Ready():
			// Ready won the race; treat as success.
		default:
			l.abortStart(ctx)
			return fmt.Errorf("gateway start: exited before ready")
		}
	case <-l.gateway.Ready():
		// Listener bound — startup barrier satisfied.
	case <-startupTimer.C:
		// Startup deadline exceeded: abort owned components (Stop shuts the
		// gateway down, which releases the gateway goroutine), then report
		// the timeout. abortStart FIRST — nothing else will cancel the
		// gateway's lifetime context here.
		abortCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l.abortStart(abortCtx)
		<-gwErr
		return fmt.Errorf("gateway start: startup timeout after %s", l.startupTimeout)
	case <-ctx.Done():
		// Cancellation during startup: wait for gateway goroutine cleanup,
		// then abort owned components and report cancellation.
		<-gwErr
		// Use a fresh context — ctx is already cancelled.
		abortCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l.abortStart(abortCtx)
		return ctx.Err()
	}

	l.log.Info("gateway started", logging.Fields{
		Context: map[string]any{"addr": l.addr},
	})

	return nil
}

// abortStart cleans up partially started owned components when Start fails.
// It stops gateway then engine (same order as Stop) but does NOT touch
// lifecycle state — lifecycle owns its own transitions via life.Shutdown.
// Safe to call when components were never started (Stop is idempotent/nil-safe).
func (l *Launcher) abortStart(ctx context.Context) {
	// Best-effort cleanup; errors during abort are non-fatal (Start already
	// returning the primary startup failure).
	_ = l.Stop(ctx)
}

// Stop gracefully shuts down the components owned by the launcher: the HTTP
// gateway and the core engine. Readiness is cleared first so load balancers
// stop routing before drain.
//
// L-004 ownership: Stop terminates only launcher-owned resources (gateway,
// engine). It does NOT call life.Shutdown() — lifecycle owns its own state
// transitions and hook execution; calling Shutdown from here would recurse
// (life.Shutdown → Close hook → Stop → life.Shutdown). The health HTTP
// server is owned by the app lifecycle hook, not by launcher. health.Server
// itself is pure readiness state — MarkNotReady is the correct action.
//
// Stop is idempotent and nil-safe (initErr path leaves engine/gateway nil).
func (l *Launcher) Stop(ctx context.Context) error {
	var errs []error

	// Flip readiness first so load balancers stop routing before drain.
	if l.health != nil {
		l.health.MarkNotReady()
	}

	// Stop gateway first (stop accepting new requests). Nil-safe: initErr
	// path never constructed a gateway.
	if l.gateway != nil {
		if err := l.gateway.Stop(ctx); err != nil {
			l.log.Error("gateway stop error", logging.Fields{
				Context: map[string]any{"err": err.Error()},
			})
			errs = append(errs, fmt.Errorf("gateway: %w", err))
		}
	}

	// Then stop the core engine (drain in-flight). Nil-safe on initErr path.
	if l.engine != nil {
		if err := l.engine.Stop(ctx); err != nil {
			l.log.Error("engine stop error", logging.Fields{
				Context: map[string]any{"err": err.Error()},
			})
			errs = append(errs, fmt.Errorf("engine: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
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

	// Start lifecycle. C-1 fix: components receive a RUN-LIFETIME context
	// (cancelled only when Run's parent context is cancelled), never a
	// deadline-bearing startup context — the previous
	// WithTimeout(ctx, 30s) was inherited by engine.Start and gateway.Start
	// as their lifetime context, so the whole system silently stopped
	// serving 30 seconds after boot while the app-owned health server kept
	// answering 200. The startup deadline now lives in Start's barrier
	// (startupTimeout) where it belongs.
	lifetimeCtx, cancelLifetime := context.WithCancel(ctx)
	defer cancelLifetime()

	if err := l.life.Start(lifetimeCtx); err != nil {
		l.log.Error("startup failed", logging.Fields{
			Context: map[string]any{"err": err.Error()},
		})
		return lifecycle.ExitFailure
	}

	l.health.MarkReady()
	l.log.Info("nexus system ready", logging.Fields{
		Context: map[string]any{
			"addr":          l.addr,
			"engine_status": string(l.engine.Status()),
		},
	})

	// Wait for shutdown
	code := l.life.WaitForShutdownSignal(ctx)
	l.log.Info("nexus system stopped", logging.Fields{})
	return code
}
