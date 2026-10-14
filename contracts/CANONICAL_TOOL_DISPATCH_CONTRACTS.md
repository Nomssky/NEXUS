# NEXUS — Canonical Tool Dispatch & Evidence Completeness (v8)

Contract status: authoritative for the dispatch boundary, evidence classes,
and path classification. Cross-references (never redefines):
`AGENT_GOVERNANCE_CONTROL_CONTRACTS.md` (admission),
`APPROVAL_LIFECYCLE_CONTRACTS.md` (pause/resume),
`CAPABILITY_TOOL_CONTRACTS.md` (platform gates),
`OPERATIONAL_RELIABILITY_CONTRACTS.md` (attempts, retries, unknown),
`EXECUTION_TRACE_CONTRACTS.md` (capture, redaction, ordering),
`AGENT_EXECUTION_CONTRACTS.md` (workflow, delegation, coordination),
`RUNTIME_EXECUTION_CONTRACTS.md` (intelligence loop).

## 1. Purpose

v7's execution trace identified a known evidence gap: the pre-platform
builtin-tool path emitted no dispatch-start event. v8 answers one question
with evidence:

> When NEXUS says an action was dispatched, admitted, attempted, or
> completed, what runtime path and evidence justify that claim?

The answer is one mediated boundary with two sub-paths behind it, each with
honest evidence. There is no second tool-execution engine, no duplicate
governance check, and no fabricated physical attempt.

## 2. The single mediated boundary

`agentexec.Runtime.InvokeToolScoped` (internal/agentexec/runtime.go) is the
**only** function that dispatches a tool. Every caller — the intelligence
loop's `ActionToolCall`, the workflow gather phase (`runAgentUnit`), a
delegated child (`DelegateChild` → `execute` → `runAgentUnit`), and a
coordination member (`RunCoordinationGroup` → `DelegateChild`) — reaches
tools through it. No production code calls `r.ToolExec[...]` directly; no
production code calls `Platform.Invoke` except `InvokeToolScoped`.

`InvokeTool` (unscoped) is a compatibility wrapper that delegates to
`InvokeToolScoped` with a reduced scope. No production caller uses it; it is
kept for API stability.

Model inference (`modelrouter.Provider.Invoke`) and memory operations
(`WriteBound`/`DeleteExact`) are **not** tool dispatches and are out of scope
for this contract; they are governed by their existing contracts.

## 3. Evidence vocabulary

These terms have exactly one meaning each. Event names are reused from the
existing vocabulary; no new event type is introduced.

| Term | Meaning | Evidenced by |
|---|---|---|
| Proposal | The model (or a workflow spec) suggested a tool call. | `action.proposed` |
| Validation accepted | The runtime accepted the proposal's structure (tool id non-empty, tool registered). | Absence of `action.rejected`; the flow continues to admission. |
| Validation rejected | The runtime refused the proposal before admission (malformed model output, unknown tool). | `action.rejected` (loop) or an error return with no dispatch event. |
| Admission | Governance evaluated the proposal and allowed, denied, or required approval. | `governance.decided` (allow/deny), `approval.requested` (wait). |
| Dispatch (logical invocation started) | The mediated boundary began executing the tool. | `tool.invocation.started` |
| Physical attempt | One adapter/executor invocation within the logical call. | `tool.attempt.started` + `tool.attempt.completed` (platform path only). |
| Retry scheduled | The reliability layer decided another attempt is admissible. | `tool.retry.scheduled` (platform path only). |
| Confirmed outcome (completed) | The invocation finished with a known success. | `tool.invocation.completed` |
| Confirmed outcome (failed) | The invocation finished with a known failure. | `tool.invocation.failed` |
| Confirmed outcome (unknown) | The invocation's outcome could not be determined. | `tool.invocation.unknown` |
| Terminal | The execution reached a terminal state. | `objective.completed` / `chain.completed`/`failed`/`cancelled`. |

### Evidence-class invariants

- A proposal is never evidence of dispatch. (`action.proposed` precedes
  admission and dispatch.)
- A successful governance decision is never evidence of dispatch.
  (`governance.decided` precedes `tool.invocation.started`.)
- `tool.invocation.started` is never evidence of a confirmed external effect.
  (It proves the mediated boundary began; the outcome event that follows
  classifies the result.)
- Absence of dispatch evidence after a refusal is positive evidence that
  nothing ran. (A denied or pending-approval call never reaches the dispatch
  point, so no `tool.invocation.started` exists.)

## 4. Path classification

### 4.1 Canonical platform path (Platform != nil)

The capability platform is constructed at boot by
`launcher.capabilityPlatform`. When construction succeeds, every tool call —
including the deterministic builtins (echo, calculator, transform) — flows
through `Platform.Invoke`, which runs the full gate sequence
(CAPABILITY_TOOL_CONTRACTS §5): manifest lookup, schema validation, agent
allowlist, membership scope (`MembershipSet.AllowsScope`), credential
resolution, capability lifecycle, then adapter dispatch with bounded physical
attempts (OPERATIONAL_RELIABILITY_CONTRACTS §7).

**Evidence**: `tool.invocation.started` (source `capability`), zero or more
`tool.attempt.started`/`tool.attempt.completed` pairs, `tool.retry.scheduled`
on retry, and one terminal event (`tool.invocation.completed` / `failed` /
`unknown` / `cancelled` / `timed_out`).

**Unknown outcomes**: possible (an external request may be sent but its
response lost). Never converted to failed or completed; never blindly
retried (OPERATIONAL_RELIABILITY_CONTRACTS §9).

### 4.2 Intentionally-internal pre-platform path (Platform == nil)

The platform is absent only in a **degraded boot**: `capabilityPlatform`
construction failed (malformed HTTP policy, workspace error, registration
error), and the launcher logged the failure and continued without a platform
(launcher.go:255-262). In this mode the capability catalogue (http.request,
web.search, github, filesystem) is **not registered** and cannot be called.
Only the deterministic builtins — registered by `registerBuiltins` before
the platform is attempted — remain executable.

The builtins are **pure local functions** with no external side effects
(`SideEffectRead`, `ReadOnly: true`, `NetworkNone`). They are intentionally
internal operations, not capability effects. Forcing them through an
external adapter would fabricate a physical attempt that never happened.

**Governance**: the pre-platform path is **not** a governance bypass. It
calls `Runtime.Admit` (admission.go) — the same mandatory, fail-closed
admission as the platform path. A refusal (deny, pending approval,
escalate, governance unavailable) returns before any executor runs. The
agent allowlist and tool registry checks also run.

**Evidence**: `tool.invocation.started` (source `agentexec`,
`side_effect_class` from the manifest) and one terminal event
(`tool.invocation.completed` on success, `tool.invocation.failed` with
`error_kind: "executor failure"` on error). **No `tool.attempt.*` events and
no `attempts` count**: a pure local invocation has no physical attempts, and
fabricating them would misrepresent the trace. The absence of attempt events
plus `side_effect_class: "read"` is the honest signal.

**Unknown outcomes**: impossible. The executor returns
`(result, error)` — the outcome is always known. `tool.invocation.unknown`
never appears on this path.

**Retries**: none. A deterministic pure function's result is reproducible;
retrying it adds nothing and would misrepresent the reliability semantics.
The single synchronous call is the bounded attempt.

**Validation**: the pre-platform path checks tool id, registry presence, and
agent allowlist. It does not run `manifest.ValidateInput` (schema
validation) — the builtin executors validate their own inputs and return
plain errors. This is a documented divergence from the platform path, not a
safety gap: the executors are pure functions whose worst case is an input
error.

### 4.3 Classification summary

| Path | Governance | Schema validation | Membership check | Credential resolution | Physical attempts | Unknown possible | Evidence source |
|---|---|---|---|---|---|---|---|
| Platform (canonical) | `Admit` + platform gates | `ValidateInput` | `AllowsScope` | resolver | 1..N (bounded) | yes | `capability` |
| Pre-platform (internal) | `Admit` | executor self-validation | not re-checked (gateway already established scope) | none (builtins need none) | 1 (local, no attempt events) | no | `agentexec` |
| Governance refusal | `Admit` fails | — | — | — | 0 | — | `governance.decided` only |

## 5. Component ownership of transitions

| Transition | Owner |
|---|---|
| Proposal → validation | `agentintel` loop (model output) or workflow spec |
| Validation → admission | `agentexec.Runtime.Admit` (single enforcement point) |
| Admission → dispatch | `agentexec.Runtime.InvokeToolScoped` (both sub-paths) |
| Dispatch → physical attempt | `capability.Platform.runAttempts` (platform path only) |
| Physical attempt → retry decision | `capability.Platform` (RetryAdmitted) |
| Invocation → outcome | platform: `Platform.Invoke`; pre-platform: `InvokeToolScoped` |
| Outcome → observation | `agentintel` loop (`observation.created`) |
| Observation → terminal | `agentintel` loop state machine |

## 6. Logical calls, physical attempts, retries

One logical call (`call_id` = correlation identity) may have N physical
attempts (platform path) or exactly one local invocation (pre-platform
path). The platform's `runAttempts` is the **only** retry site: the bound
comes from the effective side-effect class, the verdict from
`RetryAdmitted`, and every attempt shares one call deadline
(OPERATIONAL_RELIABILITY_CONTRACTS §7). No other component retries a tool
call. Unknown outcomes are never retried.

The pre-platform path has no retry site: one synchronous local call, one
outcome.

## 7. Cancellation, deadlines, budgets

- `InvokeToolScoped` checks `ctx.Err()` at entry: a cancelled or
  deadline-exceeded context prevents any new dispatch.
- The platform path applies the call deadline (`limits.MaxDuration`, tightened
  by governance constraints) to every physical attempt via `callCtx`.
- The pre-platform path passes `ctx` to the executor; the builtins are pure
  and complete synchronously, so cancellation cannot interrupt them
  mid-flight but does prevent the call from starting.
- Budgets: the platform clamps to the manifest's `ResourceLimits` and the
  runtime caps; the pre-platform path uses the builtin's manifest timeout as
  documentation only (the pure function completes instantly).

## 8. Approval and resume

Unchanged from v6 (`APPROVAL_LIFECYCLE_CONTRACTS.md`):
- `Admit` returns `REQUIRE_APPROVAL` before any dispatch; the call does not
  run.
- Resume re-enters the loop; the approved action is dispatched **once**.
- Completed effects are never replayed to regenerate trace evidence.
- Approval consumption is single-use.
- Cancellation and expired deadlines still prevent new effects after resume.

On the pre-platform path, the same semantics hold: a pending approval stops
before `execFn`; the resumed call emits `tool.invocation.started` once.

## 9. Child and coordination authority

Delegated children and coordination members inherit the parent's scope
(business, division, actor, correlation) unwidened
(AGENT_DELEGATION_CONTRACTS.md). Each child's tool calls are independently
admitted through `InvokeToolScoped` with the child's own runtime-stamped
linkage. No approval is inherited from parent to child or sibling. Trace
correlation (shared `call_id`) is a runtime fact, not an access grant: it
does not widen `MembershipSet.AllowsScope`.

## 10. Compatibility

- Tool names, schemas, outputs, and error semantics are unchanged.
- The pre-platform path's new events (`tool.invocation.started` /
  `completed` / `failed`, source `agentexec`) are **additive**: existing
  consumers that ignore unknown event types are unaffected.
- `InvokeTool` remains a compatible wrapper.
- No endpoint changes; the trace route (v7) is unchanged.
- No builtin is silently rerouted through an external adapter.

## 11. Explicitly unsupported guarantees

- **No exactly-once**: the trace records events at publish time; a crash
  between publish and enqueue can lose the last events (process-local,
  G4 Level 1).
- **No cross-path total order**: events from the platform path (source
  `capability`) and the pre-platform path (source `agentexec`) share the
  bus's capture order but have no global sequence numbers.
- **No reconciliation**: the pre-platform path runs no background
  reconcilers; `tool.reconciliation.*` never appears for builtin calls.
- **No durability**: traces are process-local (`durability:
  "process_local"`), exactly like execution results.
- **No schema-validation event on the pre-platform path**: input validation
  is the executor's responsibility; its errors surface as
  `tool.invocation.failed` (error_kind `executor failure`), not as a
  distinct validation-rejection event.
