package core

import (
	"errors"
	"fmt"
	"time"

	"github.com/Nomssky/NEXUS/internal/executor"
)

// In-flight request registry and external cancellation (E-005).
//
// Every admitted request is registered here before it is handed to the
// processing channel and deregistered only after its terminal result is
// stored, so a cancellation can always find either its registration or the
// authoritative stored result. Two checks — dequeue-time (executeChain) and
// submit-time (chainExecute) — guarantee a request cancelled while queued
// never reaches the executor.
//
// Locking: inflightMu is a leaf lock. It is never held while acquiring
// another engine lock, and executor calls (executor activeMu) always happen
// strictly after inflightMu is released — no nesting in either direction.

// Cancellation sentinels mapped to HTTP semantics by the gateway:
// ErrRequestNotFound → 404, ErrScopeMismatch → 403,
// ErrAlreadyCompleted/ErrCompletionRace → 409.
var (
	// ErrRequestNotFound reports an unknown request ID.
	ErrRequestNotFound = errors.New("core: request not found")
	// ErrScopeMismatch reports a request belonging to another business.
	ErrScopeMismatch = errors.New("core: business scope mismatch")
	// ErrAlreadyCompleted reports a non-cancellable terminal state.
	// Match with errors.Is; *TerminalStateError carries the status.
	ErrAlreadyCompleted = errors.New("core: request already in a non-cancellable terminal state")
	// ErrCompletionRace reports that a terminal state raced the cancellation.
	ErrCompletionRace = errors.New("core: request reached a terminal state while cancelling")
)

// TerminalStateError reports the terminal status alongside ErrAlreadyCompleted.
type TerminalStateError struct {
	Status string
}

func (e *TerminalStateError) Error() string {
	return fmt.Sprintf("core: request already in terminal state %q", e.Status)
}

// Is matches the ErrAlreadyCompleted sentinel so errors.Is works.
func (e *TerminalStateError) Is(target error) bool { return target == ErrAlreadyCompleted }

// errCancelledBeforeSubmit is the internal sentinel chainExecute returns when
// a cancellation landed after dequeue but before the executor submit.
// executeChain maps it to a cancelled response — never a failure.
var errCancelledBeforeSubmit = errors.New("core: request cancelled before executor submission")

// inflightState is where a request sits in the processing pipeline.
type inflightState int

const (
	inflightQueued inflightState = iota
	inflightExecuting
)

// inflightRequest is the registry entry for an admitted request. All fields
// are guarded by the engine's inflightMu.
type inflightRequest struct {
	requestID       string
	businessID      string
	correlationID   string
	taskID          string // executor task ID (wf.ID); empty until submitted
	state           inflightState
	cancelRequested bool
	cancelReason    string
	cancelActor     string
}

// registerInflight records an admitted request before its channel send.
// Requests without a context (validation rejects them downstream) are not
// registered — there is no ownership to check and nothing meaningful to
// cancel. A duplicate ID replaces the entry, matching the results map's
// overwrite semantics.
func (e *Engine) registerInflight(req *Request) {
	if req == nil || req.Context == nil {
		return
	}
	e.inflightMu.Lock()
	defer e.inflightMu.Unlock()
	e.inflight[req.ID] = &inflightRequest{
		requestID:     req.ID,
		businessID:    req.Context.BusinessID,
		correlationID: req.Context.CorrelationID,
		state:         inflightQueued,
	}
}

// deregisterInflight removes the entry once the terminal result is stored (or
// the admission send failed). Safe to call when no entry exists.
func (e *Engine) deregisterInflight(requestID string) {
	e.inflightMu.Lock()
	defer e.inflightMu.Unlock()
	delete(e.inflight, requestID)
}

// isCancelRequested reports whether cancellation was requested for requestID.
func (e *Engine) isCancelRequested(requestID string) bool {
	e.inflightMu.RLock()
	defer e.inflightMu.RUnlock()
	inf := e.inflight[requestID]
	return inf != nil && inf.cancelRequested
}

// CancelRequest cancels an in-flight request owned by businessID.
//
// Semantics (E-005):
//   - unknown request → ErrRequestNotFound (404)
//   - request of another business → ErrScopeMismatch (403), any state
//   - stored terminal result cancelled → nil (idempotent repeat)
//   - stored terminal result otherwise → *TerminalStateError (409), including
//     pending_approval (Category C 5d must revisit approval-state cancellation)
//   - queued → flagged; the dequeue/submit checks prevent execution (nil)
//   - executing → executor CancelTask (nil when accepted); a terminal race is
//     resolved against the stored result with a bounded wait
//
// actorID is the verified caller identity (the gateway binds the authenticated
// identity, or the fixed "unauthenticated" marker when enforcement is off).
// Authorization itself — identity, membership, ownership — is enforced at the
// API boundary; this method enforces ownership scope against the recorded
// business only.
func (e *Engine) CancelRequest(requestID, businessID, actorID string) error {
	if requestID == "" {
		return ErrRequestNotFound
	}
	// Fail closed on an empty scope: it can never match a recorded business.
	if businessID == "" {
		return ErrScopeMismatch
	}

	// A stored result is authoritative and terminal.
	if result, ok := e.GetResult(requestID); ok {
		if result.BusinessID != businessID {
			return ErrScopeMismatch
		}
		return terminalCancelError(result.Status)
	}

	e.inflightMu.Lock()
	inf := e.inflight[requestID]
	if inf == nil {
		e.inflightMu.Unlock()
		// The result may be racing into the store — one re-check.
		if result, ok := e.GetResult(requestID); ok {
			if result.BusinessID != businessID {
				return ErrScopeMismatch
			}
			return terminalCancelError(result.Status)
		}
		return ErrRequestNotFound
	}
	if inf.businessID != businessID {
		e.inflightMu.Unlock()
		return ErrScopeMismatch
	}

	// Record the cancellation (idempotent — flag and attribution persist).
	inf.cancelRequested = true
	inf.cancelReason = "cancellation requested"
	if inf.cancelActor == "" {
		inf.cancelActor = actorID
	}
	state, taskID := inf.state, inf.taskID
	reason, actor := inf.cancelReason, inf.cancelActor
	e.inflightMu.Unlock()

	if state == inflightExecuting && taskID != "" {
		return e.cancelExecutorTask(requestID, businessID, taskID, reason, actor)
	}
	// Queued: the executeChain dequeue check and the chainExecute submit check
	// together guarantee the request never reaches the executor.
	return nil
}

// cancelExecutorTask cancels the executor task, resolving terminal races
// against the stored result. Executor calls happen with no engine lock held
// (inflightMu is a leaf lock).
func (e *Engine) cancelExecutorTask(requestID, businessID, taskID, reason, actor string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			// Production backoff between retries — not test synchronization.
			time.Sleep(5 * time.Millisecond)
		}
		err := e.taskExec.CancelTask(taskID, reason, actor)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, executor.ErrTaskCompleted):
			// The task finished racing the cancel; the chain stores its result
			// shortly — wait for it and arbitrate against that status.
			return e.awaitTerminalResult(requestID, businessID)
		default:
			lastErr = err
		}
		// ErrTaskNotFound or unexpected: the outcome may be mid-store.
		if result, ok := e.GetResult(requestID); ok {
			if result.BusinessID != businessID {
				return ErrScopeMismatch
			}
			return terminalCancelError(result.Status)
		}
	}
	if errors.Is(lastErr, executor.ErrTaskNotFound) {
		return ErrRequestNotFound
	}
	return fmt.Errorf("%w (%v)", ErrCompletionRace, lastErr)
}

// awaitTerminalResult waits (bounded) for the chain to store the terminal
// result of a task that finished racing the cancellation, then arbitrates.
func (e *Engine) awaitTerminalResult(requestID, businessID string) error {
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(5 * time.Second)
	for {
		if result, ok := e.GetResult(requestID); ok {
			if result.BusinessID != businessID {
				return ErrScopeMismatch
			}
			return terminalCancelError(result.Status)
		}
		select {
		case <-deadline:
			return ErrCompletionRace
		case <-tick.C:
		}
	}
}

// terminalCancelError maps a stored terminal status to CancelRequest's result.
func terminalCancelError(status string) error {
	if status == "cancelled" {
		return nil // idempotent repeat cancel
	}
	// Everything else in the results map is terminal and non-cancellable —
	// completed, failed, denied, escalated … and pending_approval, which is
	// explicitly a 409 here. Category C 5d must revisit cancellation semantics
	// when approval states become first-class response statuses.
	return &TerminalStateError{Status: status}
}
