# NEXUS — Development (M11 24/7 Hardening)

This document covers **only** M11: 24/7 hardening, recovery, and chaos validation.
It does not duplicate architecture or contract docs.

> M11 is the **final milestone**. It adds the full failure playbook,
> recovery mechanisms, circuit breaker, backpressure, graceful shutdown,
> and chaos validation. It ensures NEXUS can survive any single failure
> and recover without unsafe duplicate side effects.

---

## Scope (All Layers)

| Component | What M11 adds |
|---|---|
| **Recovery Manager** | failure detection → recovery → reconcile → complete lifecycle |
| **Circuit Breaker** | prevent cascade failures, half-open probe |
| **Backpressure** | queue limits, storm protection |
| **Graceful Shutdown** | drain, checkpoint, release, persist |
| **Chaos Validation** | 9 failure modes verified recoverable |

### Invariants preserved by M11

- **Crash → restart → reconstruct → reconcile**
- **No unsafe duplicate side effects**
- **Graceful shutdown/drain verified**
- **Unknown outcome → reconcile before retry**
- **Backpressure prevents queue storms**
- **Circuit breaker prevents cascade failures**

---

## Layout

```
internal/foundation/hardening/
  hardening.go        Recovery, circuit breaker, backpressure, shutdown
  hardening_test.go   20 tests (TEST-M11-001..020)
```

---

## Usage

```go
import "github.com/Nomssky/NEXUS/internal/foundation/hardening"

// 1. Circuit breaker
cb := hardening.NewCircuitBreaker(3, 1*time.Minute)
cb.RecordFailure() // trips after 3 failures
if !cb.Allow() {
    // requests blocked
}

// 2. Backpressure
bp := hardening.NewBackpressure(1000)
if !bp.Accept() {
    // queue full, apply backpressure
}

// 3. Recovery manager
rm := hardening.NewRecoveryManager()
record := rm.Detect(hardening.FailureProcessCrash, "core", "biz-1", "crash")
rm.StartRecovery(record.ID)
rm.Reconcile(record.ID, []string{"restarted", "reconstructed"})
rm.CompleteRecovery(record.ID)

// 4. Graceful shutdown
gs := hardening.NewGracefulShutdown()
gs.StartDrain()
// ... drain in-flight work ...
gs.CompleteDrain()
```

---

## Testing

- 20 new tests covering TEST-M11-001..020
- All 9 failure modes verified recoverable
- M0–M10 tests unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)
