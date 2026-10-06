# Tool Observability (v1)

Contract: `contracts/OPERATIONAL_RELIABILITY_CONTRACTS.md` §7–§10.
Implementation: `internal/capability/platform.go` (`Auditor.emit`, `runAttempts`),
`internal/foundation/event/event.go`, `internal/agentintel/action.go`.

Everything below is metadata. No secret, no raw adapter payload, no internal
topology, no filesystem root.

## Event vocabulary (added in this milestone)

```text
tool.attempt.started          tool.invocation.cancelled
tool.attempt.completed        tool.invocation.timed_out
tool.retry.scheduled          tool.invocation.unknown
tool.capability.disabled      tool.reconciliation.available
tool.capability.deprecated    tool.reconciliation.completed
```

They ride the existing `event.MemBus` — there is no second bus. The event name
on the SSE stream is the event type, so a black-box client can match
`event: tool.invocation.unknown` directly.

### Frames

```text
tool.attempt.started     {"tool_id":"http.request","operation":"get",
                          "call_id":"api-…-int","attempt":"1","max_attempts":"2"}

tool.attempt.completed   {"tool_id":"http.request","operation":"get",
                          "call_id":"api-…-int","attempt":"1","outcome":"failed",
                          "duration_ms":"3","error_class":"external error"}

tool.retry.scheduled     {"tool_id":"http.request","operation":"get",
                          "call_id":"api-…-int","attempt":"1","error_class":"external error"}

tool.invocation.completed{"tool_id":"http.request","operation":"get","agent_id":"agent-…",
                          "status":"success","duration_ms":"9","result_bytes":"214",
                          "truncated":"false","side_effect_class":"write","outcome":"completed",
                          "attempts":"2","capability_state":"enabled","call_id":"api-…-int"}

tool.invocation.unknown  {"tool_id":"http.request","operation":"post","agent_id":"agent-…",
                          "error_kind":"unknown outcome","outcome":"unknown","attempts":"1",
                          "duration_ms":"7","call_id":"api-…-int","capability_state":"enabled",
                          "retry_recommended":"false"}
```

The terminal event is chosen by outcome: `completed` →
`tool.invocation.completed`; otherwise `tool.invocation.failed`,
`…cancelled`, `…timed_out` or `…unknown`. An `unknown` outcome never appears as
`tool.invocation.failed`.

Note on the bus: `MemBus` is a priority heap, so frame **order** is not
guaranteed; the contract fixes which frames exist and what they carry.

## Observation normalization

The intelligence layer wraps every capability result in exactly one shape
(`internal/agentintel.Observation`):

```json
{
  "observation_id": "obs-…",
  "source": "tool:http.request",
  "action_type": "tool_call",
  "status": "failed",
  "outcome": "unknown",
  "attempts": 1,
  "retry_recommended": false,
  "reconciliation_required": true,
  "text": "unknown outcome: mutation outcome unobserved on POST (…)",
  "timestamp": "2026-10-06T11:06:57Z"
}
```

* `outcome` is copied from `tool.Result.Outcome`, never re-derived from text;
* `retry_recommended` is the platform's verdict, not a model judgement;
* `reconciliation_required` is true exactly when the outcome is `unknown`;
* a successful call reports `outcome=completed` with its attempt count;
* the payload carries the bounded, already-redacted adapter result — never raw
  provider output, credentials, roots or internal error internals.

The objective's terminal summary still reports the loop-level counters:

```text
state=completed | objective=… | agent=… | iterations=2 tool_calls=1 … replans=1 observations=1
```

## Discovery metadata

`GET /api/v1/tools` reports the reliability posture per capability, alongside the
manifest. It is informational: it grants nothing and re-validates nothing.

```json
{
  "tool_id": "http.request",
  "side_effect_class": "write",
  "capability_state": "enabled",
  "supports_idempotency": true,
  "supports_reconciliation": false,
  "retry_policy": "not_sent_retry",
  "max_attempts": 2,
  "max_duration_ms": 10000
}
```

`retry_policy` is one of `none`, `bounded_read_retry`, `not_sent_retry`.
`capability_state` is the live lifecycle state (`internal/capability.Platform.CapabilityState`).

## Secret-freedom, asserted

* the redaction layer is mandatory on results, errors, headers and audit data;
* the idempotency key / `call_id` is an identity and never a secret;
* `TestAttemptTelemetryIsStructuredAndSecretFree` fails if a registered secret
  appears in any event payload;
* `e2e/tests/18-operational-reliability.spec.ts` fails if the bootstrap
  credential, the control key, `Bearer `, `client_secret`, `private_key` or
  `"secret"` appear anywhere in the SSE stream of a mutation run.

## Operational surfaces

| question | where to look |
|---|---|
| what happened to a call? | `tool.invocation.*` terminal frame + `result.error.message` |
| how many physical attempts? | `tool.attempt.*` frames, `Result.Attempts` |
| was a retry considered, and why not? | `Result.RetryRecommended`, `error_class` |
| is a capability usable at all? | `POST /api/v1/control/capabilities/{id}/state`, `capability_state` in discovery |
| what would a caller expect? | `retry_policy`, `max_attempts`, `supports_idempotency` |