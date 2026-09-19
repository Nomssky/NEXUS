// Package lifecycle implements the M0 process lifecycle foundation (component
// C01, layer L0).
//
// It models the canonical worker lifecycle from
// contracts/RUNTIME_EXECUTION_CONTRACTS.md §6.1:
//
//	CREATED -> STARTING -> READY -> BUSY -> DRAINING -> STOPPED
//
// with an explicit SHUTDOWN_REQUESTED entry into DRAINING, and FAILED as a
// terminal error state. The mission's requested vocabulary
// (START -> INITIALIZE -> READY -> RUNNING -> SHUTDOWN_REQUESTED -> DRAINING ->
// STOPPED) maps onto these canonical states; canonical names are used to avoid
// introducing a competing vocabulary.
//
// Guarantees on shutdown:
//   - stop accepting new work when shutdown begins
//   - release resources (in reverse registration order)
//   - flush safe logs
//   - close persistence connections if any (future milestones register closers)
//   - terminate cleanly and return an appropriate exit status
//
// Invariants preserved: runtime state is not durable state; lifecycle grants no
// authority. This milestone does NOT implement workflow draining.
package lifecycle

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// State is the canonical process lifecycle state.
type State string

const (
	StateCreated           State = "CREATED"
	StateStarting          State = "STARTING"
	StateReady             State = "READY"
	StateRunning           State = "RUNNING"
	StateShutdownRequested State = "SHUTDOWN_REQUESTED"
	StateDraining          State = "DRAINING"
	StateStopped           State = "STOPPED"
	StateFailed            State = "FAILED"
)

// ExitCode is the process exit status.
type ExitCode int

const (
	// ExitOK is a clean termination.
	ExitOK ExitCode = 0
	// ExitFailure is an abnormal termination.
	ExitFailure ExitCode = 1
)

// Hook is a lifecycle callback. Init hooks run during STARTING; Close hooks run
// during DRAINING in reverse registration order.
type Hook struct {
	Name  string
	Init  func(ctx context.Context) error
	Close func(ctx context.Context) error
}

// Manager owns the process lifecycle.
type Manager struct {
	mu             sync.Mutex
	state          State
	hooks          []Hook
	shutdownBudget time.Duration
	stateObservers []func(from, to State)
}

// Options configures a Manager.
type Options struct {
	// ShutdownTimeout bounds the total drain phase.
	ShutdownTimeout time.Duration
}

// New constructs a Manager in the CREATED state.
func New(opts Options) *Manager {
	budget := opts.ShutdownTimeout
	if budget <= 0 {
		budget = 30 * time.Second
	}
	return &Manager{
		state:          StateCreated,
		shutdownBudget: budget,
	}
}

// State returns the current lifecycle state.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// RegisterHook adds an init/close hook. Hooks are closed in reverse order.
func (m *Manager) RegisterHook(h Hook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks = append(m.hooks, h)
}

// OnStateChange registers an observer notified on state transitions.
func (m *Manager) OnStateChange(fn func(from, to State)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateObservers = append(m.stateObservers, fn)
}

func (m *Manager) setState(to State) {
	m.mu.Lock()
	from := m.state
	if from == to {
		m.mu.Unlock()
		return
	}
	m.state = to
	observers := make([]func(from, to State), len(m.stateObservers))
	copy(observers, m.stateObservers)
	m.mu.Unlock()
	for _, o := range observers {
		o(from, to)
	}
}

// Start runs init hooks and moves to READY. On error it moves to FAILED.
func (m *Manager) Start(ctx context.Context) error {
	m.setState(StateStarting)

	m.mu.Lock()
	hooks := append([]Hook(nil), m.hooks...)
	m.mu.Unlock()

	for _, h := range hooks {
		if h.Init == nil {
			continue
		}
		if err := h.Init(ctx); err != nil {
			m.setState(StateFailed)
			return err
		}
	}
	m.setState(StateReady)
	m.setState(StateRunning)
	return nil
}

// WaitForShutdownSignal blocks until an OS termination signal is received or the
// context is cancelled, then performs a graceful shutdown and returns the exit
// code.
func (m *Manager) WaitForShutdownSignal(ctx context.Context) ExitCode {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-ctx.Done():
	case <-sigCh:
	}
	return m.Shutdown(context.Background())
}

// Shutdown performs the graceful drain and returns the exit code. It is
// idempotent: a second call after STOPPED returns ExitOK without re-running
// hooks.
func (m *Manager) Shutdown(ctx context.Context) ExitCode {
	m.mu.Lock()
	if m.state == StateStopped {
		m.mu.Unlock()
		return ExitOK
	}
	hooks := append([]Hook(nil), m.hooks...)
	m.mu.Unlock()

	m.setState(StateShutdownRequested)
	m.setState(StateDraining)

	drainCtx, cancel := context.WithTimeout(ctx, m.shutdownBudget)
	defer cancel()

	var failed bool
	// Close in reverse registration order.
	for i := len(hooks) - 1; i >= 0; i-- {
		h := hooks[i]
		if h.Close == nil {
			continue
		}
		if err := h.Close(drainCtx); err != nil {
			failed = true
		}
	}

	m.setState(StateStopped)
	if failed {
		return ExitFailure
	}
	return ExitOK
}

// ErrAlreadyStopped is returned by Run when the manager is already stopped.
var ErrAlreadyStopped = errors.New("lifecycle: manager already stopped")
