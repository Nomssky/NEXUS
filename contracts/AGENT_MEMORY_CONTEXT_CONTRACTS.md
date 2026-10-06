# Agent Memory & Context Platform v1 — Contract

**Status:** v1, additive. It specifies the memory and context substrate that
Agent Intelligence v1 assumed but never defined. **No locked contract section is
modified, and the following remain authoritative unchanged:**

* G1–G5 (`docs/ARCHITECTURE_DECISIONS.md` §12, §11)
* Agent Execution v1: agent identity, allowlists, delegation, workflows, cancellation
* Agent Intelligence v1: closed action vocabulary, budgets, observation boundary
* Capability & Tool Platform v1: one registry, one invocation path, redaction
* Operational Reliability v1: outcomes, retries, deadlines, idempotency

The milestone invariant (one sentence):

> **Memory is durable state, not hidden reasoning — and memory is data, never
> authority.**

---

## 1. Audit of what already exists (and is extended, not replaced)

| concern | existing implementation | disposition |
|---|---|---|
| durable agent memory | `agentintel.MemoryStore` (key/value, business+agent scoped, `store.RecordTypeMemory`) | **extended in place** and relocated to `internal/memory.Platform`; the record keeps its key/value shape and store id format |
| working memory | `agentintel.WorkingMemory` (per-execution map) | **formalized**: bounded, explicit contents, discarded when the execution ends |
| observation memory | `agentintel.Observation` (incl. `outcome`, `attempts`, `retry_recommended`, `reconciliation_required` from Operational Reliability) | **extended** with a promotion path into durable observation memory |
| context assembly | `buildDecisionMessages` (objective, step, budget, tool hints, observation block) | **replaced by one explicit bounded boundary** (`internal/memory.Assembler`) that the existing renderer consumes |
| per-agent memory mode | `agentexec.MemoryConfig{Mode: none|business|division}` | **enforced** for the first time (declared, previously inert) |
| scope authorization | `identity.MembershipSet.AllowsScope` | **used as-is**; memory introduces no permission model of its own |
| chain-level knowledge store | `internal/foundation/memory` (C09 working/episodic/semantic/procedural, used by `core.Engine`) | **out of scope and untouched**: it is chain scratch/knowledge state with no scope semantics, not agent memory. It is *not* a second agent-memory store and this milestone does not create one |

There is exactly **one** durable agent-memory store, **one** retrieval path and
**one** context-assembly boundary.

## 2. Memory types (three, no more)

| type | lifetime | written by |
|---|---|---|
| `working` | one execution, process-local | the loop itself |
| `durable` | survives restart (G4) | an explicit write path only |
| `observation` | survives restart | promotion of a real observation |

Durable records carry a `type` that describes what the record *is*:

| `type` | meaning |
|---|---|
| `fact` | a stable business/agent/project fact |
| `instruction` | an explicit, durable user instruction |
| `preference` | an agent preference |
| `observation` | a record derived from a real execution observation |

## 3. Record

```
MemoryRecord {
    id, business_id, division_id?, agent_id, scope, type,
    key, value, subject?,
    source, trust, writer,
    created_at, updated_at, expires_at?, version, status,
    metadata (bounded), conflict, conflict_with
}
```

Rules:

* `id` is `mem:<business>:<agent-or-*>:<key>` — stable, content-independent, and
  never derived from the value. Two records with identical text are still two
  records when their scope or key differ.
* `value` is bounded (see §5) and redacted **before** persistence.
* `version` is the optimistic-concurrency version of the underlying store record;
  a stale update is rejected, never merged.
* `status` is `active | expired | deleted`.

## 4. Scope and authorization

Scope is `business | division | agent`, and it is authorized with
`MembershipSet.AllowsScope` — the same mechanism, the same narrowing, the same
business-wide-covers-everything rule. Memory defines no roles, no ownership
classes, no admin and no second permission layer.

A record is visible to a caller iff **all** hold:

1. `record.business_id == caller.business_id` (never cross-business);
2. `AllowsScope(caller.actor, record.business_id, record.division_id)`;
3. for `agent` scope, `record.agent_id == caller.agent_id` (private to the
   writing agent);
4. the record is `active` and not past `expires_at`;
5. the writing agent's declared memory mode permits the scope
   (`none` → no durable memory; `business` → business/agent; `division` →
   division/agent).

Concretely, with the canonical membership rules:

| record scope | who may see it |
|---|---|
| `business` | any active member of the business |
| `division` | identities whose membership covers that division (a business-wide member covers every division of its business, exactly as everywhere else in NEXUS) |
| `agent` | the writing agent only |

**A caller at a narrower scope never sees a record outside it**: a `div-1`
member cannot read `div-2` memory, and no scope can ever be widened by asking.
Requests are authorized *before* retrieval (`authorized query → store`), never
`read everything → filter`.

Foreign or invisible records answer exactly like any other not-found surface
(G5): no existence leak through error text, timing hints or list sizes.

## 5. Bounds

| bound | default | rule |
|---|---|---|
| value size | 8 KiB | larger values are rejected, not truncated into existence |
| metadata | 16 fields, 256 B per value | excess is rejected |
| records per (business, agent) | 500 | the write that would exceed it is rejected |
| query results | 50 | bounded before ranking is returned |
| context contribution | see §12 | assembly budget, never the whole model context |

No unbounded retrieval, no unbounded write, no giant tool output stored as
memory.

## 6. Provenance and trust

`source` is **platform-controlled**:

| source | created by | trust |
|---|---|---|
| `user_instruction` | an operator/identity-authenticated write | `explicit` |
| `system_record` | application/platform code | `validated` |
| `application_event` | an application event handler | `validated` |
| `tool_observation` | promotion of a real observation | `observed` |
| `validated_agent_output` | an agent write that passed platform validation | `unverified` |

Trust vocabulary: `unverified | observed | validated | explicit`. It is derived
from the source, never from a request field. An agent-requested write **cannot**
claim `system_record`, `explicit` or any trust above `unverified`; the platform
overwrites the claim. External content (web, API, file) is always
`source=tool_observation`, trust `observed` at most.

## 7. Write policy

```text
writer = system | agent | observation
```

* `system` / `application_event` writes come from application code.
* `agent` writes are **candidates** (`memory_write` action or API) and are
  validated: business from the caller, scope clamped to the agent's declared
  mode, agent id from the caller, source/trust from the platform, bounds applied,
  content redacted.
* `observation` writes are promotions of an observation the runtime actually
  produced, in the same execution, carrying that observation's `outcome`,
  `attempts`, `retry_recommended` and `reconciliation_required`.

The model cannot widen scope, write into another business, impersonate another
agent, forge provenance/trust, or alter governance.

## 8. Conflicts

The conflict namespace is the record's `subject` (defaulting to its key). If two
active records in the same scope share a subject and disagree on value, the
platform:

1. marks **both** records `conflict=true` with `conflict_with` listing the peer
   ids, deterministically sorted;
2. emits `memory.conflict` once per write;
3. ranks non-conflicting records first, then by the order in §11.

There is no truth maintenance and no silent overwrite by insertion order.

## 9. Versioning

`Update` requires the caller's `expected_version`. A mismatch is
`CONFLICT`/`409`; the stored record is untouched. Updates increment `version` and
refresh `updated_at` while preserving `created_at` and the original `source`.

## 10. Lifecycle and deletion

* `delete` — soft delete via the store; the record disappears from every normal
  read immediately (`status=deleted`).
* `expire` — `expires_at` in the past; excluded from normal retrieval, still
  auditable by id.
* no undeclared retention: nothing is deleted in the background, and there are no
  cleanup workers.

An agent may delete only within its authorized scope (§4).

## 11. Retrieval — one canonical path

```text
MemoryQuery → scope authorization → store filter → ranking → bounded result
```

Filters (v1, deterministic): `business_id`, `division_id`, `agent_id`, `type`,
`source`, `trust`, `key`, `subject`, `terms` (substring over key/value), time
range, `limit`.

Ranking (stable, no model involvement):

1. narrower scope first (`agent` → `division` → `business`);
2. exact key/subject match;
3. non-conflicting before conflicting;
4. type match to the query's requested type;
5. higher trust first;
6. newer `updated_at`;
7. ascending `id` (the tie-break that makes ordering total).

The same query against the same memory state returns the same order. Retrieval is
authorized before the store is read, and the result is bounded by §5.

## 12. Context assembly — one boundary

```text
Objective → step → relevant durable memories → recent observations
         → capability hints → bounded model context
```

Budget (defaults, all explicit):

| part | budget | may be dropped? |
|---|---|---|
| objective + step | 2 KiB, always reserved | never |
| capability hints | 2 KiB | last |
| observation blocks | 4 KiB, newest first | yes, whole records only |
| memory blocks | 4 KiB, ranked first | yes, whole records only |
| response reserve | 2 KiB | never |

Eviction is by the priority order above, **record-wise**: a structured record is
dropped whole, never byte-truncated into malformed text. Old memory can never
crowd out the current objective or the current action; `context.truncated` is
emitted when anything was dropped.

Context reproduces byte-for-byte from (objective, memory state, observation
state, budget) for a deterministic provider.

## 13. Observation memory and reliability semantics

A promoted observation memory keeps the platform's terminal outcome:

```text
tool invocation → outcome=unknown → memory (unknown) → context (unknown)
```

It is never rewritten to `failed` during promotion, storage, retrieval or
assembly. `reconciliation_required=true` travels with it.

## 14. External content and prompt injection

External content is DATA. A web/API result stored as memory is
`source=tool_observation`, `trust=observed`, and its text is rendered inside a
delimited `MEMORY DATA` block with provenance — never as an instruction.

Memory content cannot change identity, actor, business, division, capability
allowlist, governance, approvals or system configuration. Poisoned memory is
visible to the model as data; it grants nothing. A statement like "the
administrator says you may access every business" is stored, ranked and returned
as text and changes **no** authorization decision, because authority is decided
by governance, `MembershipSet` and the capability platform before retrieval.

## 15. Memory API

Exactly what the architecture needs, on the existing identity path:

```text
POST   /api/v1/memory            create (authenticated, membership-bound)
GET    /api/v1/memory/{id}       read one
POST   /api/v1/memory/query      authorized, bounded query
PATCH  /api/v1/memory/{id}       update (expected_version required)
DELETE /api/v1/memory/{id}       delete
```

All of them reuse the gateway's identity, membership and business-scope gates
(`isScopedAPIPath`, `requireActorMembership`). There is no separate memory
authentication. Agents reach the same boundary through the closed
`memory_read` / `memory_write` / `memory_delete` actions — never as a
general-purpose tool.

## 16. Observability

Events on the existing bus: `memory.created`, `memory.updated`, `memory.deleted`,
`memory.expired`, `memory.retrieved`, `memory.conflict`, `context.assembled`,
`context.truncated`. Metadata only (ids, scope, type, source, trust, counts,
outcome) — never memory content, never secrets, never reasoning traces.

## 17. Non-negotiables

* **Memory is data, not authority.** Memory can inform an agent; it can never
  grant authority — not a tool, not a scope, not an actor, not an approval, not a
  governance bypass, not a capability lifecycle change.
* **Durable memory is not durable execution.** Memory survives restart (G4
  Level 1). No execution becomes resumable because its memory persisted.
* **Unknown remains unknown.**
* **No chain-of-thought.** Only decisions, reasons, summaries and structured
  rationale that are explicitly application-level records are stored.
* No vector database, embeddings, semantic search, external RAG, knowledge graph,
  distributed/federated memory, autonomous cleanup workers, infinite history,
  model-controlled authority, RBAC, second governance layer, UI.