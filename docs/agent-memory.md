# NEXUS — Agent Memory (v1)

Contract: `contracts/AGENT_INTELLIGENCE_CONTRACTS.md` §11, §13, §14.
Implementation: `internal/agentintel/memory.go` (durable store),
`action.go` (`WorkingMemory`).

## Two stores, deliberately separate

| | Working memory | Agent memory |
|---|---|---|
| scope | one objective execution | an agent inside a business |
| lifetime | dies with the execution | durable organization record |
| storage | in-process map (`WorkingMemory`) | record store, type `memory` |
| written by | the loop (observations) | an explicit `memory_write` action only |
| after restart | gone | still there |

```
Record { key, value, scope, agent_id, business_id, created_at, updated_at, metadata }
record id = mem:<business_id>:<agent_id>:<key>      # namespaced: no shadowing
```

Hydration is fail-closed like the org registry: a corrupt or inconsistent
record aborts boot rather than running on partial memory.

## Explicit operations only

```
memory_read    { "type": "memory_read",   "key": "notes" }
memory_write   { "type": "memory_write",  "key": "notes", "value": "…" }
memory_delete  { "type": "memory_delete", "key": "notes" }
```

There is **no** automatic persistence of model output, prompts or
conversation context. If the model did not ask for a write, nothing is stored.

## Scope rules

* Every operation is stamped with the *execution's* business and the acting
  agent — never with anything the caller or model supplies.
* Reading another business' or another agent' key is denied (`memory: no entry
  for key …` / `access denied`), not silently empty-success.
* A division-scoped execution can only reach its own agents' records.
* Memory values are **data**: they enter the prompt inside observation blocks
  and can never become instructions, actions or authority.

## Memory ≠ recovery

Durable memory does not imply durable execution (G4). After a restart:

* agent memory records are still readable;
* the objective execution that wrote them is gone — its execution id answers
  `404`, and nothing resumes, replays or re-plans.

## Out of scope for v1

No vector database, no embeddings, no semantic retrieval, no cross-agent shared
scratchpad, no business-wide "knowledge base" write path beyond a
business-scoped key. Structured records are sufficient for v1 and keep the
scope story honest.