# Execution Trace (v7)

Contract: `contracts/EXECUTION_TRACE_CONTRACTS.md`.
Implementation: `internal/foundation/event/membus.go` (publish-time tap),
`internal/core/tracing.go` (bounded registry + capture-time redaction),
`internal/gateway/intelligence.go` (`handleGetIntelligenceTrace`).

The execution trace is a **read-only, process-local, capture-time-redacted
view** over the events one agent execution already publishes on the single
in-process bus. It is not a second event bus, not a durable journal, not an
audit store, and not a source of authority: the stored execution record stays
authoritative for status, and organization audit records stay whatever the
identity contracts already provide.

## Read route

```text
GET /api/v1/intelligence/{id}/trace?business_id=...&limit=...&after_seq=...
```

- Authorization is exactly the execution-detail sequence: actor membership
  for `business_id`, business match, division read check. Unknown, foreign
  and unreadable executions are a 404 like everywhere else; missing or
  invalid parameters are 400; no credential is 401. No new auth mode, role,
  or key exists for the trace.
- `limit` (1..256, default 256) and `after_seq` (default 0) page the trace in
  capture order; continuation is `after_seq = last returned seq`. There is no
  search, no wildcard, no filter service.
- Retrieval mutates nothing: no execution, approval, governance, memory or
  tool state changes because a trace was read.

## Response shape

```json
{
  "execution_id": "api-…-int",
  "correlation_id": "api-…-int",
  "business_id": "default",
  "status": "completed",
  "durability": "process_local",
  "capture": {
    "available": true, "entries": 47, "dropped": 0, "limit": 256,
    "truncated": false, "first_seq": 1, "last_seq": 47,
    "terminal_seq": 45, "post_terminal": 2
  },
  "entries": [
    {
      "seq": 31, "event_id": "api-…-int-tool.invocation.started-…",
      "type": "tool.invocation.started", "source": "capability",
      "timestamp": "2026-10-14T…Z",
      "data": {"tool_id": "http.request", "operation": "get", "call_id": "api-…-int"}
    }
  ]
}
```

- `status` comes from the stored execution record (the trace never re-derives
  it); a pending execution answers 200 with `status: "pending"` and the
  partial entries captured so far.
- `capture.dropped`/`truncated` expose capacity loss, `redacted_keys` on an
  entry names every payload key dropped at capture, and `data_unparseable`
  marks a source payload that could not be parsed — missing evidence stays
  visibly missing.
- An execution whose trace was evicted at capacity answers 200 with
  `capture.available: false`. It is never fabricated.

## What the entries prove

Trace identity is the runtime **correlation identity**, which on the
intelligence surface equals the execution id; delegated children inherit it,
so a child's events join the parent's trace with runtime-stamped lineage
(`parent_agent_id`, `delegation_depth`, `parent_task_id`).

Evidence classes stay strictly distinct, in capture (= publish) order:

```text
action.proposed            the model suggested this — nothing ran
governance.decided         the runtime decided (allow/deny/require approval)
tool.invocation.started    the mediated platform began dispatch
tool.attempt.started       one physical attempt (attempt / max_attempts)
tool.invocation.completed  the adapter completed (NOT external confirmation)
tool.invocation.unknown    the outcome is unknown — never collapsed to failed
tool.requested             the loop's post-call record (emitted when the call returns)
objective.completed        episode end; state=pending_approval is a wait, not terminal
```

An unknown outcome stays unknown; a denial leaves no dispatch evidence at all;
one logical call shows N physical attempts sharing one `call_id`; a resumed
approval episode continues the same trace and never replays the effect.

## Redaction

Redaction happens at capture, before any buffer holds the data: only
allowlisted keys (ids, enums, counts, durations, error classes) are kept,
values are bounded to 200 characters, and caller/approver free text
(`objective`, `intent`, `decision_rationale`, and the request intent inside
`chain.started`) is always dropped and named. Prompts, model output, tool
arguments and results, and credentials never enter a trace.

## Limits and durability

- 256 entries per execution (drop-oldest FIFO with a visible `dropped`
  counter and sequence gaps), 512 traced executions globally (oldest-first).
- Traces are process-local exactly like execution results and approvals: a
  restart loses them, and the API says so in every response
  (`durability: "process_local"`). No crash-safe continuity, no replay, no
  exactly-once, no global total order — see the contract's unsupported
  guarantees list.
