// Package hardening implements NEXUS M11 — 24/7 Hardening + Recovery + Chaos Validation.
//
// M11 provides the full failure playbook, recovery mechanisms, and chaos
// validation tests. It ensures NEXUS can survive process crashes, machine
// failures, provider disappearances, network outages, database unavailability,
// worker deaths, unknown outcomes, and queue storms.
//
// Key invariants:
//   - Crash → restart → reconstruct → reconcile
//   - No unsafe duplicate side effects
//   - Graceful shutdown/drain verified
//   - Unknown outcome → reconcile before retry
//   - Backpressure prevents queue storms
//   - Circuit breaker prevents cascade failures
package hardening

import (
	"fmt"
	"sync"
	"time"
)

// FailureMode classifies the type of failure.
type FailureMode string

const (
	FailureProcessCrash  FailureMode = "process_crash"
	FailureMachineCrash  FailureMode = "machine_crash"
	FailureProviderGone  FailureMode = "provider_disappears"
	FailureNetworkGone   FailureMode = "network_disappears"
	FailureDBUnavailable FailureMode = "db_unavailable"
	FailureWorkerDeath   FailureMode = "worker_death"
	FailureTaskUnknown   FailureMode = "task_unknown"
	FailureAPITimeout    FailureMode = "api_timeout"
	FailureQueueStorm    FailureMode = "queue_storm"
)

// RecoveryStatus tracks the state of a recovery operation.
type RecoveryStatus string

const (
	RecoveryDetected    RecoveryStatus = "detected"
	RecoveryInProgress  RecoveryStatus = "in_progress"
	RecoveryReconciling RecoveryStatus = "reconciling"
	RecoveryCompleted   RecoveryStatus = "completed"
	RecoveryFailed      RecoveryStatus = "failed"
)

// FailureRecord represents a recorded failure event.
type FailureRecord struct {
	ID          string         `json:"id"`
	Mode        FailureMode    `json:"mode"`
	Component   string         `json:"component"`
	BusinessID  string         `json:"business_id"`
	Description string         `json:"description"`
	Recovery    RecoveryStatus `json:"recovery"`
	DetectedAt  time.Time      `json:"detected_at"`
	RecoveredAt *time.Time     `json:"recovered_at,omitempty"`
	SideEffects []string       `json:"side_effects,omitempty"` // actions taken during recovery
}

// CircuitBreaker prevents cascade failures by stopping requests to failing components.
type CircuitBreaker struct {
	failures    int
	threshold   int
	resetTime   time.Duration
	lastFailure time.Time
	state       string // "closed", "open", "half-open"
	mu          sync.RWMutex
	now         func() time.Time
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(threshold int, resetTime time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		resetTime: resetTime,
		state:     "closed",
		now:       time.Now,
	}
}

// NewCircuitBreakerWithClock creates a new circuit breaker with an injectable clock.
func NewCircuitBreakerWithClock(threshold int, resetTime time.Duration, now func() time.Time) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		resetTime: resetTime,
		state:     "closed",
		now:       now,
	}
}

// RecordFailure records a failure and may trip the breaker.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = cb.now()

	if cb.failures >= cb.threshold {
		cb.state = "open"
	}
}

// RecordSuccess records a success and may reset the breaker.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = "closed"
}

// Allow checks if a request is allowed through the breaker.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == "closed" {
		return true
	}

	if cb.state == "open" {
		// Check if reset time has passed
		if cb.now().Sub(cb.lastFailure) > cb.resetTime {
			return true // allow probe request (half-open)
		}
		return false
	}

	return true // half-open allows one request
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Backpressure manages queue limits and storm protection.
type Backpressure struct {
	maxQueueSize  int
	currentSize   int
	rejectedCount int
	mu            sync.RWMutex
	now           func() time.Time
}

// NewBackpressure creates a new backpressure manager.
func NewBackpressure(maxQueueSize int) *Backpressure {
	return &Backpressure{
		maxQueueSize: maxQueueSize,
		now:          time.Now,
	}
}

// NewBackpressureWithClock creates a new backpressure manager with an injectable clock.
func NewBackpressureWithClock(maxQueueSize int, now func() time.Time) *Backpressure {
	return &Backpressure{
		maxQueueSize: maxQueueSize,
		now:          now,
	}
}

// Accept checks if a new item can be accepted into the queue.
func (bp *Backpressure) Accept() bool {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if bp.currentSize >= bp.maxQueueSize {
		bp.rejectedCount++
		return false
	}
	bp.currentSize++
	return true
}

// Release removes an item from the queue.
func (bp *Backpressure) Release() {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if bp.currentSize > 0 {
		bp.currentSize--
	}
}

// QueueSize returns the current queue size.
func (bp *Backpressure) QueueSize() int {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.currentSize
}

// RejectedCount returns the number of rejected items.
func (bp *Backpressure) RejectedCount() int {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.rejectedCount
}

// RecoveryManager handles failure detection and recovery.
type RecoveryManager struct {
	failures map[string]*FailureRecord
	now      func() time.Time
}

// NewRecoveryManager creates a new recovery manager.
func NewRecoveryManager() *RecoveryManager {
	return &RecoveryManager{
		failures: make(map[string]*FailureRecord),
		now:      time.Now,
	}
}

// NewRecoveryManagerWithClock creates a new recovery manager with an injectable clock.
func NewRecoveryManagerWithClock(now func() time.Time) *RecoveryManager {
	return &RecoveryManager{
		failures: make(map[string]*FailureRecord),
		now:      now,
	}
}

// Detect records a failure detection.
func (rm *RecoveryManager) Detect(mode FailureMode, component, businessID, description string) *FailureRecord {
	now := rm.now()
	record := &FailureRecord{
		ID:          fmt.Sprintf("fail-%d", now.UnixNano()),
		Mode:        mode,
		Component:   component,
		BusinessID:  businessID,
		Description: description,
		Recovery:    RecoveryDetected,
		DetectedAt:  now,
	}

	rm.failures[record.ID] = record
	return record
}

// StartRecovery begins recovery for a failure.
func (rm *RecoveryManager) StartRecovery(failureID string) error {
	record, ok := rm.failures[failureID]
	if !ok {
		return fmt.Errorf("failure %s not found", failureID)
	}

	record.Recovery = RecoveryInProgress
	return nil
}

// Reconcile marks a failure as being reconciled.
func (rm *RecoveryManager) Reconcile(failureID string, sideEffects []string) error {
	record, ok := rm.failures[failureID]
	if !ok {
		return fmt.Errorf("failure %s not found", failureID)
	}

	record.Recovery = RecoveryReconciling
	record.SideEffects = sideEffects
	return nil
}

// CompleteRecovery marks a failure as recovered.
func (rm *RecoveryManager) CompleteRecovery(failureID string) error {
	record, ok := rm.failures[failureID]
	if !ok {
		return fmt.Errorf("failure %s not found", failureID)
	}

	now := rm.now()
	record.Recovery = RecoveryCompleted
	record.RecoveredAt = &now
	return nil
}

// GetRecord returns a failure record by ID.
func (rm *RecoveryManager) GetRecord(failureID string) (*FailureRecord, bool) {
	record, ok := rm.failures[failureID]
	return record, ok
}

// RecordCount returns the total number of failure records.
func (rm *RecoveryManager) RecordCount() int {
	return len(rm.failures)
}

// GracefulShutdown manages shutdown draining.
type GracefulShutdown struct {
	draining   bool
	drainStart time.Time
	now        func() time.Time
}

// NewGracefulShutdown creates a new graceful shutdown manager.
func NewGracefulShutdown() *GracefulShutdown {
	return &GracefulShutdown{
		now: time.Now,
	}
}

// NewGracefulShutdownWithClock creates a new graceful shutdown with an injectable clock.
func NewGracefulShutdownWithClock(now func() time.Time) *GracefulShutdown {
	return &GracefulShutdown{
		now: now,
	}
}

// StartDrain begins the graceful shutdown drain process.
func (gs *GracefulShutdown) StartDrain() {
	gs.draining = true
	gs.drainStart = gs.now()
}

// IsDraining returns true if shutdown is in progress.
func (gs *GracefulShutdown) IsDraining() bool {
	return gs.draining
}

// CompleteDrain marks the drain as complete.
func (gs *GracefulShutdown) CompleteDrain() {
	gs.draining = false
}

// DrainDuration returns how long the drain has been running.
func (gs *GracefulShutdown) DrainDuration() time.Duration {
	if !gs.draining {
		return 0
	}
	return gs.now().Sub(gs.drainStart)
}
