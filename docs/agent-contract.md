# NEXUS — Agent Contract (v1)

Normative contract: `contracts/AGENT_EXECUTION_CONTRACTS.md` §2–§3.
Implementation: `internal/agentexec/definition.go`, `registry.go`, `selector.go`.

An **agent** in NEXUS is a durable, scoped execution *entity*:

```
AgentDefinition
├── id                 stable, client-drawn
├── business_id        mandatory (SCHEMA_IDENTITIES_ORG §8)
├── division_id        optional; narrows the agent (§4.3)
├── name/description   human-facing
├── capabilities[]     declarative selection metadata (research, analysis, coding, …)
├── model              requirements, not a vendor binding
├── allowed_tools[]    allowlist of registered tool ids
├── memory             declared memory boundary (none|business|division)
├── parameters         opaque static parameters
└── status             active | suspended | archived  (archived terminal)
```

## Lifecycle

`active ⇄ suspended → archived (terminal)`, the same matrix organizations
use (G2 addendum, §12.2). Only `active` agents are selectable. A suspended
agent's definitions and history stay readable; a restart never resurrects a
suspended agent (status is a durable record, like every other org record).

## Persistence

Durable: write-through to the same record store as identities
(`store` type `agent`), fail-closed hydration, `persist → memory` ordering.
`GET /api/v1/agents` after a restart returns exactly what was registered.

## Scope (G3, unchanged)

* business-wide agent ⇒ servable by any task in the business, including
  division tasks;
* division agent ⇒ servable only inside its division; a *business-scope* task
  never selects it;
* a foreign-business agent is invisible (`404`, G5) and cannot be transitioned.

## Selection is deterministic

```
candidates = agents(business)
  drop: status != active            -> rejected "lifecycle status ..."
  drop: division mismatch           -> rejected "division scope mismatch"
  drop: missing required capability -> rejected "missing required capabilities: ..."
  drop: tool not allowlisted/unknown-> rejected
winner  = max(optional coverage), tie-break lexicographic id
```

The winner travels to `agent.selected` events and, when a preference was given,
the reason string explains whether the preference was honored or rejected.

## What an agent is not

Not a prompt, not a role, not an authority. Capabilities never grant tools or
permissions; tools never grant scope; the scope rules above are the only
authority statement in this document, and they are the G3 rules.