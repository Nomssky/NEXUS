# NEXUS — Agent Delegation Contracts (v4)

**Layer:** Agent Execution / multi-agent delegation boundary
**Status:** MAKER — guidance for all current implementations
**Depends on:**

- `AGENT_EXECUTION_CONTRACTS.md` (§10 delegation primitive)
- `AGENT_EXECUTION_DEPTH_CONTRACTS.md` (v3 planning runtime)
- `AGENT_GOVERNANCE_CONTROL_CONTRACTS.md`
- `AGENT_MODEL_PROTOCOL_CONTRACTS.md` (v2 strict decisions)
- `AGENT_MEMORY_CONTEXT_CONTRACTS.md`
- `CAPABILITY_TOOL_CONTRACTS.md`
- `OPERATIONAL_RELIABILITY_CONTRACTS.md`

---

## 1. Purpose

This contract hardens delegation from "the runtime can start a child" into a
bounded multi-agent runtime. The target shape is:

```text
Parent Agent → Plan → Delegate Child → Child Execution → Child Governance →
Child Effects → Child Observation → Parent Observation → Parent Replan → …
```

The fundamental rule:

```text
PARENT AUTHORITY ≠ CHILD AUTHORITY
```

Delegation is a request to create another execution. It is NOT capability
inheritance, governance inheritance, memory inheritance, scope expansion,
approval inheritance, identity impersonation, credential inheritance, or
budget multiplication.

## 2. The delegation boundary

Delegation creates a new execution boundary. Every child execution carries
explicit runtime-owned metadata:

```text
parent_execution_id   (the delegating execution's correlation/task id)
parent_agent_id       (the delegating agent, as resolved by the runtime)
child_execution_id    (parent task id + "/delegate/" + child id)
delegation_action_id  (the governance proposal id that admitted it)
```

The runtime establishes the relationship; the model names only a target
agent, an objective, and optionally explicit tool calls. A child is never
attachable to another business, another execution, an unrelated parent, or an
unrelated actor: business/division/actor are inherited from the parent
request, never widened, and the parent linkage is stamped by the runtime.

## 3. Delegation is a consequential action

```text
model proposal → structural validation → runtime target resolution →
governance admission → child creation → child execution → observation
```

Never `model → directly create child`. Never `plan approved → all future
delegations automatically approved`: every delegation passes normal
admission against the resolved child-agent target (`agent:<child id>`).

## 4. Child authority (intersection, not inheritance)

The effective child capability set is runtime-owned:

```text
effective child tools
  = model-proposed tools
    ∩ registered tools (manifest exists, operation declared)
    ∩ child agent allowlist (registry definition)
    ∩ parent agent allowlist (the delegator cannot give what it lacks)
    ∩ governance admission (per effect, at child runtime)
    ∩ capability platform gates (scope, permission, credential, redaction)
```

Bounds (deny, never silently widen):

```text
child business scope   ≤ parent business scope     (inherited, never widened)
child division scope   ≤ parent permitted scope    (inherited, never widened)
child memory scope     = none by construction      (§6: children have no
                                                    durable memory path)
child budget           ≤ parent remaining bounds   (count + depth + fan-out)
child deadline         ≤ parent remaining deadline (context carries it)
```

A delegation naming an unknown, inactive, out-of-scope, or tool-mismatched
agent fails closed. A delegation proposing tools is rejected unless it names
an explicit, eligible child agent — the runtime never guesses whose
allowlist applies.

## 5. Tool boundary

Child tool calls run through the SAME `Platform.Invoke` path with the SAME
gates (manifest, operation, allowlist, scope, permission, credential,
adapter, reliability, redaction). No `child.invokeTool()` bypass exists.
Every child effect keeps manifest validation, operation validation, scope
validation, permission audit, credential resolution, budget accounting,
operational reliability, governance admission, and redaction.

## 6. Memory isolation

Children have no durable memory path: the execution layer owns no memory
platform reference, and no memory-reaching tool is registered for children,
so isolation holds by construction rather than by convention:

```text
child working memory  = private to the child unit (never exists at this layer)
child durable writes  = impossible (no platform, no tool)
parent durable memory ≠ automatically readable by the child
```

Parent context assembly never includes child-scoped records (there are
none). What the parent learns about the child arrives only as the child's
output text inside the parent's observation — DATA, never instructions, and
never memory provenance. A replan may depend on that observation; it cannot
rewrite it.

## 7. Result propagation (runtime evidence only)

The child outcome normalizes into the parent's existing observation/outcome
model with its status preserved: `completed`, `failed`, `cancelled`,
`timed_out`, `unknown`, `budget_exhausted`, `deadline_exceeded`,
`pending_approval`, `escalated`, `denied`. In particular:

- `unknown` stays `unknown` on the parent observation (with reconciliation
  markers), the parent run fails honestly, and nothing re-dispatches the child;
- `pending_approval` / `escalated` / `denied` are reported as what they are —
  the parent run fails naming the child outcome and its approval/escalation
  reference, never mistaking it for success and never auto-resuming;
- the parent model cannot fabricate `child completed`: completion still needs
  the parent's own runtime evidence, and a child result is one observation
  among others, never a state rewrite.

## 8. Cancellation, deadline, budget, depth, fan-out

- Cancellation is checked before the delegation is created; the child runs
  under the parent's context, so a parent cancel aborts in-flight child work
  through the existing context propagation. After cancellation no new child
  is created. No distributed cancellation is introduced.
- The child deadline is the parent's remaining deadline (context-carried);
  the model cannot extend it, and expiry yields the existing
  `deadline_exceeded` outcome.
- Delegation count is bounded by the parent's delegation budget; nesting
  depth is bounded by the execution max-depth (agentexec `maxDepth`, enforced
  on every nested `execute`); spec-level fan-out is bounded by the existing
  parallelism bound (`maxFan`). Together these make unlimited-work
  (`parent → child → grandchild → …`) unrepresentable: depth terminates it,
  fan-out contains it, and no layer multiplies budgets.
- The intelligence loop tracks its own delegation depth and validates
  `depth+1` against its `MaxDepth` before creating a child.

## 9. Governance per effect, approval and escalation stay bound

Parent delegation passes governance #1 (admission on `agent:<child>`); every
child effect passes governance #2 through the same controller (admission on
the child's own targets). Approving the parent delegation never approves a
child effect: the child admission runs with the child's own proposal and
fingerprint. A child escalation reaches the existing attention surface and
is reported to the parent as an escalation, never as success.

## 10. Observability and limits

Delegation reuses the existing events (`agent.delegated` with delegate and
agent ids, `governance.decided` per admission, tool/memory/outcome events
inside the child correlated by execution id, child TaskID lineage
`parent/delegate/id`). No raw model reasoning, no unrestricted prompts, no
chain-of-thought is stored. Execution stays process-local (G4): no worker
queues, no durable scheduling, no distributed locks, no background workers,
no swarm behavior — every delegation is explicitly bounded by one parent
execution's budget, deadline, depth and fan-out.
