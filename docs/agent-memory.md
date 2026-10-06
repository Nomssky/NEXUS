# Agent Memory & Context Platform v1

Contracts: `contracts/AGENT_MEMORY_CONTEXT_CONTRACTS.md` (this milestone) and
`contracts/AGENT_INTELLIGENCE_CONTRACTS.md` §11, §13, §14 (the explicit-write and
no-automatic-persistence rules, unchanged).
Implementation: `internal/memory` (store, retrieval, context),
`internal/agentintel/memory.go` + `memoryops.go` (loop wiring),
`internal/agentintel/working.go` (working memory), `internal/gateway/memory.go`
(HTTP surface).

> **Memory is data, not authority. Durable memory is not durable execution.
> Unknown remains unknown.**

## One store, one path

```
objective → [authorized memory] + [observations] + [capability hints]
          → Assembler (bounded)  → decision prompt
```

```text
agentintel ──► memory.Platform ──► store.Store (RecordTypeMemory)
                    ▲                     ▲
                    │                     │
        gateway /api/v1/memory      identity + membership + capabilities
```

There is exactly one durable agent-memory store, one authorized retrieval path
and one context-assembly boundary. `internal/foundation/memory` (the chain-level
knowledge scratch used by `core.Engine`) is a different concern and is untouched.

## Memory types

| type | lifetime | how it is written |
|---|---|---|
| `working` | one execution, process-local | the loop itself (`agentintel.WorkingMemory`) |
| `durable` | survives restart | an explicit write only |
| `observation` | survives restart | promotion of a real observation |

Durable records additionally declare what they are: `fact`, `instruction`,
`preference`, `observation` — a closed vocabulary.

## The record

```json
{
  "id": "mem:biz-1:a1:endpoint",
  "business_id": "biz-1", "division_id": "", "agent_id": "a1",
  "scope": "agent", "type": "fact",
  "key": "endpoint", "value": "https://api.example", "subject": "endpoint",
  "source": "validated_agent_output", "trust": "unverified", "writer": "agent",
  "outcome": "", "attempts": 0, "retry_recommended": false,
  "reconciliation_required": false,
  "metadata": {"observation_id": "obs-7"},
  "created_at": "…", "updated_at": "…", "expires_at": null,
  "version": 1, "status": "active", "conflict": false, "conflict_with": []
}
```

* `id` is derived from scope, never from content: two records with identical text
  are still two records.
* `subject` is the conflict namespace and defaults to `key`.
* The id shape is the historical one, extended with the division:
  `mem:<business>:<division>@<agent|_>:<key>`.

## Scope

| record scope | who may see it |
|---|---|
| `business` | any active member of the business |
| `division` | identities whose membership covers that division |
| `agent` | the writing agent only |

Authorization is `identity.MembershipSet.AllowsScope` — the same mechanism the
tool platform uses. Memory defines no roles. Answers are deliberately distinct:

| situation | answer |
|---|---|
| caller is not a member / lacks the division | `ErrScope` |
| record belongs to another business | `ErrNotFound` (G5: no existence leak) |
| another agent's private record | `ErrNotFound` |

An acting agent's declared `memory.mode` (`none|business|division`, from
`agentexec.Definition`) can only *narrow* what it may write. `none` refuses every
durable write.

## Bounds

| bound | default |
|---|---|
| value size | 8 KiB (rejected, never truncated into existence) |
| metadata | 16 fields, 256 B per value |
| records per (business, agent) scope | 500 |
| query results | 50 |

## Provenance, trust and writers

| writer | source | trust |
|---|---|---|
| `user` (authenticated API) | `user_instruction` | `explicit` |
| `system` (application code) | `system_record` | `validated` |
| `application_event` | `application_event` | `validated` |
| `agent` (model-proposed) | `validated_agent_output` | `unverified` |
| `observation` (promotion) | `tool_observation` | `observed` |

The writer kind — not the payload — decides provenance and trust. The API has no
field for either, so a caller cannot relabel a record. An agent write is capped at
`unverified` and can never create observation memory.

## Conflicts

Two active records in the same scope sharing a `subject` with different values are
both marked `conflict=true` with a sorted `conflict_with` list, and
`memory.conflict` is emitted. Memory marks the disagreement; it never decides a
winner and never overwrites by insertion order.

## Versioning, deletion, expiry

* `Update` requires `expected_version`; a mismatch is `409` and the stored record
  is untouched. `key`, `business`, `division`, `scope`, `agent` and `subject` are
  immutable after creation.
* `Delete` is an immediate soft delete: the record is invisible to every normal
  read, in both the in-memory and file stores.
* `Expire` sets `expires_at`; expired records are excluded from retrieval and stay
  auditable by id. There are no cleanup workers and no undeclared retention.

## Retrieval

```text
authorize → query the store at the caller's business → narrow per record
          → rank (total order) → bound
```

Ranking: narrower scope → exact key/subject match → non-conflicting → requested
type → higher trust → newer `updated_at` → ascending `id`. No model is involved,
and the same query over the same state returns the same order.

## HTTP surface

```text
POST   /api/v1/memory          create          (identity + membership)
POST   /api/v1/memory/query    bounded query    (identity + membership)
GET    /api/v1/memory/{id}     read one
PATCH  /api/v1/memory/{id}     update (expected_version required)
DELETE /api/v1/memory/{id}     delete
```

```bash
curl -XPOST "$BASE/api/v1/memory?business_id=default" \
  -H "X-Actor-ID: nx:human:bootstrap" -H "X-Actor-Credential: $BOOT" \
  -H 'Content-Type: application/json' \
  -d '{"key":"endpoint","value":"https://api.example","type":"fact","scope":"business"}'
# 201 {"memory_id":"mem:default:_:endpoint","source":"user_instruction","trust":"explicit",…}
```

## Explicit operations only

There is **no** automatic persistence of model output, prompts, conversation
context or working memory. If the model did not ask for a write, nothing is
stored.

```
memory_read    { "type": "memory_read",   "key": "notes" }
memory_write   { "type": "memory_write",  "key": "notes", "value": "…",
                 "memory_type": "fact", "memory_scope": "agent" }
memory_write   { "type": "memory_write",  "key": "ledger",
                 "observation_id": "obs-7" }        # promotion
memory_delete  { "type": "memory_delete", "key": "notes" }
```

## The loop

* `memory_write` proposes key/value/type/scope — the platform clamps scope, assigns
  provenance and trust, bounds and redacts.
* `memory_write` with `observation_id` **promotes** a real observation of the
  current execution. The content, the outcome and the provenance come from the
  runtime's observation; the model supplies neither. A promotion that also carries
  a `value` is rejected.
* `memory_read` goes through the authorized query and returns the record with its
  provenance and outcome.
* `memory_delete` deletes exactly what the caller could have written.

```json
{"type":"memory_write","key":"ledger","observation_id":"obs-7"}
```

## Events

`memory.created`, `memory.updated`, `memory.deleted`, `memory.expired`,
`memory.retrieved`, `memory.conflict`, `context.assembled`, `context.truncated` —
on the existing bus, metadata only (ids, scope, type, provenance, counts,
outcome). Never content, never secrets, never reasoning.

## Out of scope

No vector database, no embeddings, no semantic retrieval, no external RAG, no
knowledge graph, no distributed or federated memory, no autonomous cleanup
workers, no chain-of-thought storage, no RBAC, no second governance layer and no
durable execution recovery. Structured records are sufficient for v1 and keep the
scope story honest.