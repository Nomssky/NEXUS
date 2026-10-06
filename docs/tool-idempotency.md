# Tool Idempotency (v1)

Contract: `contracts/OPERATIONAL_RELIABILITY_CONTRACTS.md` §1, §3, §7, §9.
Implementation: `internal/capability/platform.go` (`callID`, `runAttempts`),
`internal/capability/http.go` (`Idempotency-Key`), `internal/agentexec`
(`InvokeToolScoped`).

## One identity per logical call

```
call_id        = the request's correlation id (one agent decision to use a tool)
idempotency_key = call_id            (stable across attempts, per logical call)
attempt_index  = 0, 1, ...           (0-based, monotonic within the call)
```

```go
callID := req.CorrelationID                       // already established by the runtime
if strings.TrimSpace(callID) == "" {              // never model-supplied
    callID = fmt.Sprintf("%s-%d", req.ToolID, p.now().UnixNano())
}
inv := tool.Invocation{ ..., CallID: callID, IdempotencyKey: callID }
```

Guarantees:

* the key **never changes** between attempts of the same call (asserted in
  `TestIdempotencyIdentityIsStableAcrossAttempts`);
* two logical calls **never share** a key, even for the same tool and input;
* the key is derived from runtime identity only — a model cannot choose it,
  reuse it or observe it;
* the key is an **identity, not a secret**: it carries no credential material
  and never reaches an observation, result or event body except as `call_id`.

## On the wire

`http.request` sends the key as a header, and **only for mutating methods**:

```text
POST /v1/entries HTTP/1.1
Idempotency-Key: api-1791259848721970788-int
```

GitHub issue mutations ride the same boundary and therefore the same header.
Reads never claim mutation idempotency (`isReadMethod` gates both the header and
the unknown-outcome classification).

A capability advertises this itself rather than being assumed to:

```go
func (t *HTTPTool) SupportsIdempotency() bool { return true } // -> capability.IdempotencyCapable
```

Discovery exposes it as `supports_idempotency`, and the retry gate reads it: a
mutation whose request provably never left the process is re-issued only when
the capability can de-duplicate a replay.

## Unknown outcomes and duplicate suppression

The rule that keeps duplicates impossible:

```text
deterministic "not sent"        → re-issue is safe (and idempotency-keyed)
anything else after dispatch    → unknown → NEVER re-dispatched
```

```go
// internal/capability/http.go
if neverSent(err) {                                    // DNS or dial failure
    return tool.RawResult{}, fmt.Errorf("%w: %v", ErrNotSent, err)
}
if !isReadMethod(rq.Method) {                           // dispatched, indeterminate
    return tool.RawResult{}, fmt.Errorf("%w: mutation outcome unobserved on %s (%v)",
        ErrUnknownOutcome, rq.Method, err)
}
```

Consequences, asserted black-box in `e2e/tests/18-operational-reliability.spec.ts`:

* one objective that posts to an endpoint which never answers produces
  **exactly one** remote mutation, across any number of attempts of the run;
* the terminal frame is `tool.invocation.unknown` with
  `outcome=unknown`, `attempts=1`, `retry_recommended=false`, and never
  `tool.invocation.failed`;
* the intelligence layer reports it as an observation with
  `reconciliation_required=true` and does not retry.

## Reconciliation, minimally

`reconciliation_available` is `true` only when an adapter implements
`ReconciliationCapable` and says it can resolve an unknown outcome by reading the
capability back. No shipped adapter does, so it is `false` everywhere today, and
no background worker exists or is planned: an unknown outcome is an **operator**
task, surfaced in telemetry, never an automatic action.