# NEXUS — Agent Coordination Contracts (v5)

**Layer:** Agent Intelligence / bounded parallel multi-agent coordination
**Status:** MAKER — guidance for all current implementations
**Depends on:**

- `AGENT_DELEGATION_CONTRACTS.md` (v4 single-delegation boundary)
- `AGENT_EXECUTION_DEPTH_CONTRACTS.md` (v3 planning runtime)
- `AGENT_MODEL_PROTOCOL_CONTRACTS.md` (v2 strict decisions)
- `AGENT_GOVERNANCE_CONTROL_CONTRACTS.md`
- `AGENT_MEMORY_CONTEXT_CONTRACTS.md`
- `CAPABILITY_TOOL_CONTRACTS.md`
- `OPERATIONAL_RELIABILITY_CONTRACTS.md`

---

## 1. Purpose

This contract extends sequential parent→child delegation into bounded
parallel coordination:

```text
Parent → Plan → [Child A ‖ Child B ‖ Child C] → aggregate → replan → …
```

The core invariant:

```text
PARALLELISM ≠ AUTHORITY
```

Parallel children remain independent execution boundaries. This is NOT a swarm
system: no distributed workers, background schedulers, durable recovery,
multi-instance coordination, unlimited parallelism, agent marketplaces, or
agent-to-agent chat. Everything stays inside the existing bounded execution
model, on the existing `agentintel → agentexec → executor` path.

## 2. Explicit coordination groups

Parallelism is never inferred from dependency absence. The model proposes one
typed `coordinate` action naming an explicit member list; the runtime owns a
`CoordinationGroup{ID, Parent execution, Members[]}` for exactly that action.
A coordination group is a SINGLE wave of mutually independent children:
members carry no inter-member dependencies, and any group proposing otherwise
is rejected. Cross-step ordering (A,B → D) stays at the plan level, where v3
dependency semantics already order steps — there is no second dependency
system and no agent messaging of any kind.

## 3. Bounds (all runtime-enforced, all fail-closed)

- **Total children**: group members count fully against the parent's
  delegation budget (`Used.Delegations + len(members) ≤ MaxDelegations`), and
  the execution layer independently refuses more than `maxFan` members.
- **Simultaneous children**: one semaphore sized by the existing parallelism
  bound (`maxFan`, the same mechanism as workflow parallel nodes). A group
  larger than the remaining delegation budget is rejected before anything
  starts; a group larger than `maxFan` is refused at the execution boundary.
- **Depth**: members run one level deeper; the existing max-depth terminates
  deeper nesting. Groups do not nest (a child runs units, not groups), so
  limits cannot multiply per level.
- Together with the fixed parent deadline and cancellation context, total
  work per execution stays bounded. The 20-total/3-simultaneous split is
  deferred: today both axes share the existing bounds, which is strictly
  tighter, never looser.

## 4. Budgets, deadlines, cancellation

- Delegation count is the cross-boundary budget currency: started members
  consume it; tool budgets stay per-execution (v4 semantics preserved).
- Every member inherits the parent's remaining deadline through the shared
  context; no member extends it. Mid-wave expiry ends the group as
  `deadline_exceeded`; mid-wave cancellation ends it as `cancelled`. In both
  cases no further member starts, effects of started members stand as
  recorded, and member fingerprints are kept only for effects that actually
  completed.
- Cancellation is checked before the group is created; after cancellation no
  new child and no new consequential effect begins.

## 5. Admission per member (no blanket authorization)

A group is never admitted as a unit. Every member passes normal admission
(`agent:<child>`) with its own proposal and fingerprint BEFORE any member is
created: one refusal (deny, approval, escalation, unenforceable constraint)
fails the whole group closed with zero children started. A pending member
approval therefore pauses the parent exactly like a single delegation
(standard resume replays cleanly, since nothing ran). Each member effect then
passes its own admission at child runtime, through the same controller.

## 6. Child independence and isolation

Every member keeps the full v4 boundary: own registry identity, own
execution, own working state, own governance, own capability intersection
(proposal ∩ registry ∩ child allowlist ∩ parent allowlist ∩ platform),
own reliability. No shared mutable agent state; no member mutates another's
runtime state; no member calls another member's tools. Anonymous members are
rejected in groups (deterministic aggregation needs stable member identity).
Memory isolation holds by construction (children have no durable memory
path); coordination happens only through authoritative runtime observations.

## 7. Deterministic aggregation

Settlement is recorded in member declaration order — never completion race
order — as one parent observation:

- per-member: index, child id, agent id, task id, status, normalized outcome,
  attempts, truncated output reference;
- group totals per status class;
- the observation text is a deterministic rendering of the same record.

Physical completion order may appear in telemetry; logical state never
depends on it (and never on SSE frame order). Re-running an identical fully
settled group is rejected as a duplicate; re-proposing a partially settled
group prunes already-completed members (their original evidence is kept) and
runs only the remainder — so recovery never duplicates a completed mutation.

## 8. Partial settlement is honest and non-terminal

A settled group with mixed member outcomes is a NON-terminal observation:
the ledger step completes on the settlement evidence, dependents of the group
step proceed, and the model may replan or complete from evidence (completion
still needs its own evidence). Failed members are marked failed; nothing is
collapsed; nothing is retried automatically; successful members are never
re-run. Unknown stays unknown with reconciliation markers. Pending approval
and escalation members are reported as what they are (with references),
never converted to success — with the documented v4 limitation that no
cross-execution approval resume is invented here. Group-level faults
(validation, admission refusal, budget exhaustion, cancellation, deadline)
are terminal like any other action failure.

## 9. Authority boundaries restated for groups

- The parent model interprets aggregated observations; it cannot modify
  member status, outcome, evidence, governance results, approval state, or
  identity (all runtime-owned, all read-only in context).
- The parent cannot fabricate a settlement: a `complete` naming group results
  with no settlement observation fails the evidence rule.
- A member cannot fabricate a sibling's completion: only the runner's record
  counts, keyed by member index.
- Child output (including hostile text) is DATA in the parent context, never
  an instruction, never a capability grant, never a new child.

## 10. Observability and limits

New events are limited to the group lifecycle (`coordination.started`,
`coordination.completed`, both metadata-only: group id, plan version, member
counts by status). Member lifecycle reuses existing events (`agent.delegated`
per member with linkage, `agent.started` with the stamped `Delegation` link,
tool/governance/outcome events inside each child). No prompts, responses,
credentials, reasoning, or chain-of-thought are stored. G4 is unchanged:
process-local only, no durable coordination state, no scheduler, no queues.
