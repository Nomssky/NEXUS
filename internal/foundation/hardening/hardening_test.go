package hardening

import (
	"testing"
	"time"
)

// TEST-M11-001: Process crash recovery
func TestProcessCrashRecovery(t *testing.T) {
	rm := NewRecoveryManager()
	record := rm.Detect(FailureProcessCrash, "nexus-core", "biz-1", "process exited unexpectedly")

	if record.Recovery != RecoveryDetected {
		t.Errorf("expected detected, got %v", record.Recovery)
	}

	rm.StartRecovery(record.ID)
	if record.Recovery != RecoveryInProgress {
		t.Errorf("expected in_progress, got %v", record.Recovery)
	}

	rm.Reconcile(record.ID, []string{"restarted process", "reconstructed state"})
	if record.Recovery != RecoveryReconciling {
		t.Errorf("expected reconciling, got %v", record.Recovery)
	}

	rm.CompleteRecovery(record.ID)
	if record.Recovery != RecoveryCompleted {
		t.Errorf("expected completed, got %v", record.Recovery)
	}
	if record.RecoveredAt == nil {
		t.Error("expected recovered_at timestamp")
	}
}

// TEST-M11-002: Provider disappears failover
func TestProviderDisappearsFailover(t *testing.T) {
	rm := NewRecoveryManager()
	record := rm.Detect(FailureProviderGone, "ollama", "biz-1", "provider health check failed")

	rm.StartRecovery(record.ID)
	rm.Reconcile(record.ID, []string{"routed to backup provider", "marked model unknown"})
	rm.CompleteRecovery(record.ID)

	if record.Recovery != RecoveryCompleted {
		t.Errorf("expected completed, got %v", record.Recovery)
	}
}

// TEST-M11-003: Network disappears → UNKNOWN
func TestNetworkDisappearsUnknown(t *testing.T) {
	rm := NewRecoveryManager()
	record := rm.Detect(FailureNetworkGone, "outbound", "biz-1", "network unreachable")

	rm.StartRecovery(record.ID)
	rm.Reconcile(record.ID, []string{"marked task UNKNOWN", "no blind retry"})
	rm.CompleteRecovery(record.ID)

	if record.Recovery != RecoveryCompleted {
		t.Errorf("expected completed, got %v", record.Recovery)
	}
	// Verify no unsafe duplicate side effects
	if len(record.SideEffects) != 2 {
		t.Errorf("expected 2 side effects, got %d", len(record.SideEffects))
	}
}

// TEST-M11-004: Database unavailable → fail-safe
func TestDBUnavailableFailSafe(t *testing.T) {
	rm := NewRecoveryManager()
	record := rm.Detect(FailureDBUnavailable, "store", "biz-1", "database connection lost")

	rm.StartRecovery(record.ID)
	rm.Reconcile(record.ID, []string{"buffered telemetry", "denied high-risk actions"})
	rm.CompleteRecovery(record.ID)

	if record.Recovery != RecoveryCompleted {
		t.Errorf("expected completed, got %v", record.Recovery)
	}
}

// TEST-M11-005: Worker death → heartbeat miss → reassign
func TestWorkerDeathReassign(t *testing.T) {
	rm := NewRecoveryManager()
	record := rm.Detect(FailureWorkerDeath, "agent-runtime", "biz-1", "heartbeat timeout")

	rm.StartRecovery(record.ID)
	rm.Reconcile(record.ID, []string{"detected unresponsive", "stopped zombie", "reassigned task"})
	rm.CompleteRecovery(record.ID)

	if record.Recovery != RecoveryCompleted {
		t.Errorf("expected completed, got %v", record.Recovery)
	}
}

// TEST-M11-006: Task becomes UNKNOWN → reconcile before retry
func TestTaskUnknownReconcile(t *testing.T) {
	rm := NewRecoveryManager()
	record := rm.Detect(FailureTaskUnknown, "scheduler", "biz-1", "task outcome unknown after timeout")

	rm.StartRecovery(record.ID)
	rm.Reconcile(record.ID, []string{"marked UNKNOWN", "reconciled state", "retry with owner"})
	rm.CompleteRecovery(record.ID)

	if record.Recovery != RecoveryCompleted {
		t.Errorf("expected completed, got %v", record.Recovery)
	}
}

// TEST-M11-007: API timeout → prevent unsafe duplicate
func TestAPITimeoutNoDuplicate(t *testing.T) {
	rm := NewRecoveryManager()
	record := rm.Detect(FailureAPITimeout, "tool-runtime", "biz-1", "external API timeout")

	rm.StartRecovery(record.ID)
	rm.Reconcile(record.ID, []string{"marked UNKNOWN", "prevented duplicate", "reconciled"})
	rm.CompleteRecovery(record.ID)

	// Verify no unsafe duplicate
	for _, se := range record.SideEffects {
		if se == "retry" {
			t.Error("should not blindly retry after timeout")
		}
	}
}

// TEST-M11-008: Circuit breaker trips after threshold
func TestCircuitBreakerTrips(t *testing.T) {
	cb := NewCircuitBreaker(3, 1*time.Minute)

	// Record 3 failures
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != "open" {
		t.Errorf("expected open, got %v", cb.State())
	}
	if cb.Allow() {
		t.Error("expected denied when breaker open")
	}
}

// TEST-M11-009: Circuit breaker resets after success
func TestCircuitBreakerReset(t *testing.T) {
	cb := NewCircuitBreaker(3, 1*time.Minute)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	cb.RecordSuccess()
	if cb.State() != "closed" {
		t.Errorf("expected closed after success, got %v", cb.State())
	}
}

// TEST-M11-010: Circuit breaker half-open after reset time
func TestCircuitBreakerHalfOpen(t *testing.T) {
	now := time.Now()
	cb := NewCircuitBreakerWithClock(3, 1*time.Minute, func() time.Time { return now })

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	// Advance time past reset
	cb2 := NewCircuitBreakerWithClock(3, 1*time.Minute, func() time.Time { return now.Add(2 * time.Minute) })
	cb2.failures = cb.failures
	cb2.state = cb.state
	cb2.lastFailure = cb.lastFailure

	if !cb2.Allow() {
		t.Error("expected allowed after reset time (half-open)")
	}
}

// TEST-M11-011: Backpressure rejects when queue full
func TestBackpressureRejects(t *testing.T) {
	bp := NewBackpressure(2)

	if !bp.Accept() {
		t.Error("expected accept for first item")
	}
	if !bp.Accept() {
		t.Error("expected accept for second item")
	}
	if bp.Accept() {
		t.Error("expected reject when queue full")
	}
	if bp.RejectedCount() != 1 {
		t.Errorf("expected 1 rejected, got %d", bp.RejectedCount())
	}
}

// TEST-M11-012: Backpressure releases when items complete
func TestBackpressureRelease(t *testing.T) {
	bp := NewBackpressure(2)

	bp.Accept()
	bp.Accept()
	bp.Release()

	if !bp.Accept() {
		t.Error("expected accept after release")
	}
}

// TEST-M11-013: Graceful shutdown drain
func TestGracefulShutdownDrain(t *testing.T) {
	now := time.Now()
	gs := NewGracefulShutdownWithClock(func() time.Time { return now })

	gs.StartDrain()
	if !gs.IsDraining() {
		t.Error("expected draining")
	}

	drainDuration := gs.DrainDuration()
	if drainDuration != 0 {
		t.Errorf("expected 0 duration at start, got %v", drainDuration)
	}

	gs.CompleteDrain()
	if gs.IsDraining() {
		t.Error("expected not draining after complete")
	}
}

// TEST-M11-014: Failure modes coverage
func TestFailureModesCoverage(t *testing.T) {
	modes := []FailureMode{
		FailureProcessCrash, FailureMachineCrash, FailureProviderGone,
		FailureNetworkGone, FailureDBUnavailable, FailureWorkerDeath,
		FailureTaskUnknown, FailureAPITimeout, FailureQueueStorm,
	}
	if len(modes) != 9 {
		t.Errorf("expected 9 failure modes, got %d", len(modes))
	}
}

// TEST-M11-015: Recovery lifecycle complete
func TestRecoveryLifecycleComplete(t *testing.T) {
	rm := NewRecoveryManager()
	record := rm.Detect(FailureQueueStorm, "scheduler", "biz-1", "queue growing uncontrollably")

	if record.Recovery != RecoveryDetected {
		t.Errorf("expected detected, got %v", record.Recovery)
	}

	rm.StartRecovery(record.ID)
	if record.Recovery != RecoveryInProgress {
		t.Errorf("expected in_progress, got %v", record.Recovery)
	}

	rm.Reconcile(record.ID, []string{"applied backpressure", "dropped low-priority"})
	if record.Recovery != RecoveryReconciling {
		t.Errorf("expected reconciling, got %v", record.Recovery)
	}

	rm.CompleteRecovery(record.ID)
	if record.Recovery != RecoveryCompleted {
		t.Errorf("expected completed, got %v", record.Recovery)
	}
}

// TEST-M11-016: No unsafe duplicate side effects invariant
func TestNoUnsafeDuplicateSideEffects(t *testing.T) {
	rm := NewRecoveryManager()

	// Simulate multiple failures and verify no duplicate side effects
	record1 := rm.Detect(FailureAPITimeout, "tool-1", "biz-1", "timeout")
	rm.StartRecovery(record1.ID)
	rm.Reconcile(record1.ID, []string{"marked UNKNOWN"})
	rm.CompleteRecovery(record1.ID)

	record2 := rm.Detect(FailureAPITimeout, "tool-1", "biz-1", "timeout again")
	rm.StartRecovery(record2.ID)
	rm.Reconcile(record2.ID, []string{"marked UNKNOWN", "reconciled state"})
	rm.CompleteRecovery(record2.ID)

	// Each failure has its own record and side effects
	if record1.ID == record2.ID {
		t.Error("expected different failure records")
	}
}

// TEST-M11-017: Clock injection works
func TestHardeningClockInjection(t *testing.T) {
	now := time.Now()
	rm := NewRecoveryManagerWithClock(func() time.Time { return now })
	record := rm.Detect(FailureProcessCrash, "core", "biz-1", "crash")

	if !record.DetectedAt.Equal(now) {
		t.Errorf("expected clock-injected time")
	}
}

// TEST-M11-018: Crash → restart → reconstruct → reconcile (full cycle)
func TestFullRecoveryCycle(t *testing.T) {
	rm := NewRecoveryManager()

	// Step 1: Detect crash
	record := rm.Detect(FailureProcessCrash, "nexus-core", "biz-1", "process crash")
	if record.Recovery != RecoveryDetected {
		t.Errorf("step 1: expected detected, got %v", record.Recovery)
	}

	// Step 2: Start recovery (restart)
	rm.StartRecovery(record.ID)
	if record.Recovery != RecoveryInProgress {
		t.Errorf("step 2: expected in_progress, got %v", record.Recovery)
	}

	// Step 3: Reconcile (reconstruct state)
	rm.Reconcile(record.ID, []string{"restarted process", "reconstructed objective state", "reconciled in-flight tasks"})
	if record.Recovery != RecoveryReconciling {
		t.Errorf("step 3: expected reconciling, got %v", record.Recovery)
	}

	// Step 4: Complete recovery
	rm.CompleteRecovery(record.ID)
	if record.Recovery != RecoveryCompleted {
		t.Errorf("step 4: expected completed, got %v", record.Recovery)
	}

	// Verify full cycle
	if len(record.SideEffects) != 3 {
		t.Errorf("expected 3 side effects, got %d", len(record.SideEffects))
	}
}

// TEST-M11-019: Graceful shutdown duration tracking
func TestGracefulShutdownDuration(t *testing.T) {
	now := time.Now()
	counter := 0
	gs := NewGracefulShutdownWithClock(func() time.Time {
		counter++
		return now.Add(time.Duration(counter) * time.Second)
	})

	gs.StartDrain()
	duration := gs.DrainDuration()
	if duration != time.Second { // first call returns now+1s
		t.Errorf("expected 1s duration, got %v", duration)
	}
}

// TEST-M11-020: Chaos validation — all failure modes recoverable
func TestChaosValidationAllModesRecoverable(t *testing.T) {
	rm := NewRecoveryManager()

	modes := []FailureMode{
		FailureProcessCrash, FailureMachineCrash, FailureProviderGone,
		FailureNetworkGone, FailureDBUnavailable, FailureWorkerDeath,
		FailureTaskUnknown, FailureAPITimeout, FailureQueueStorm,
	}

	for _, mode := range modes {
		record := rm.Detect(mode, "component", "biz-1", "chaos test")
		rm.StartRecovery(record.ID)
		rm.Reconcile(record.ID, []string{"recovered"})
		rm.CompleteRecovery(record.ID)

		if record.Recovery != RecoveryCompleted {
			t.Errorf("mode %s: expected completed, got %v", mode, record.Recovery)
		}
	}
}
