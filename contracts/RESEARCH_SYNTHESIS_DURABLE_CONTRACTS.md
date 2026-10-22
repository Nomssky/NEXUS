# NEXUS — Durable Research Synthesis (v59)

Authoritative for the durable representation of a validated research
synthesis. It extends the existing research architecture; it never replaces
it. Cross-references (never redefines): `AGENT_INTELLIGENCE_CONTRACTS.md`,
`DURABLE_EXECUTION_RECOVERY_CONTRACTS.md`,
`DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md`,
`RESEARCH_QUALITY_EVALUATION_CONTRACTS.md`, `CAPABILITY_TOOL_CONTRACTS.md`,
`CORE_INTERFACE_CONTRACTS.md`.

## 0. Scope

This milestone makes the **already-validated typed synthesis** a durable,
execution-scoped artifact. A completed research execution now has one
consistent durable snapshot of three things:

1. the durable synthesis (`execjournal.ResearchSynthesis`, v59),
2. the durable evidence snapshot (`execjournal.ResearchEvidence` v2), and
3. the deterministic quality verdict (`execjournal.ResearchEvaluation` v1).

Before v59 the synthesis existed only for the lifetime of the process that ran
the execution. A restart destroyed the user-facing research conclusion even
though the evidence and the verdict survived. After v59 the conclusion survives
too, and it survives *with* the evidence and verdict it was computed from.

It introduces **no second research engine, planner, synthesis pipeline,
evidence store or recovery mechanism**. The synthesis is not regenerated, not
re-derived from prose, and not recomputed on read.

### The one rule that governs everything

> **A persisted synthesis may only ever be the synthesis that was validated
> against the evidence it is stored next to.**

Every durable synthesis carries the execution id it belongs to and the
fingerprint of the exact evidence snapshot it was validated against. The
journal re-validates that binding at write time and at decode time. A
synthesis that cannot be bound is never persisted as a valid result.

### Explicit non-goals

| Not done | Why |
|---|---|
| No raw model output, prompt, or hidden reasoning persisted | Only the validator-accepted structured synthesis is stored. The `result=` prose stays process-local. |
| No credential, transport payload, or raw provider response persisted | The durable synthesis carries statements and source **ids** only. |
| No regeneration on read | A legacy record without a synthesis reports it as unavailable; it is never recomputed. |
| No new endpoint | The existing durable detail read carries the snapshot; the list stays bounded. |
| No change to the synthesis contract | The same fields, the same meanings, the same validation limits. |
| No claim of semantic truth | Persisting a synthesis does not make it true. v58's limitations remain authoritative. |

## 1. The durable synthesis object

`execjournal.ResearchSynthesis` (schema version `1`, method
`deterministic_v1`):

| Field | Meaning |
|---|---|
| `version` | synthesis schema version (`1`); unsupported → fail closed |
| `method` | `deterministic_v1`; the only writer of version 1 |
| `execution_id` | the execution this synthesis belongs to; never another |
| `status` | `complete`, `partial` or `inconclusive` — the completion status |
| `summary` | the bounded model-authored overview, rendered as text only |
| `evidence_backed` | statements plus the execution-scoped source ids that support them |
| `inferences` | model conclusions that are not themselves source claims |
| `conflicts` | disagreements the captured sources support |
| `unresolved_questions` | gaps the execution could not settle |
| `evidence_fingerprint` | SHA-256 over the normalized evidence snapshot this synthesis was validated against |

The field set mirrors `executor.ResearchSynthesis` exactly. No field was added
or removed to make persistence easier, and no field carries source metadata:
titles, URLs and snippets live in the evidence snapshot and are re-resolved by
the UI from the source ids.

Collections serialize as JSON arrays, never `null`.

### Validation rules

`ValidateResearchSynthesisForExecution(executionID, synthesis, evidence)`
applies at every durable write and decode boundary:

1. **Schema version** must be `1`; an unsupported version fails closed.
2. **Method** must be `deterministic_v1`.
3. **Execution identity**: `execution_id` must be non-empty and equal the
   durable record id.
4. **Evidence binding**: a non-nil synthesis requires a non-nil evidence
   snapshot whose fingerprint equals `evidence_fingerprint`. A synthesis
   without evidence is rejected.
5. **Status** must be one of the three documented completion statuses.
6. **Shape and bounds** (mirroring the executor's limits):
   `summary` non-empty and ≤ 4000 characters; ≤ 12 evidence-backed
   statements, ≤ 12 inferences, ≤ 8 conflicts, ≤ 8 unresolved questions;
   statement ≤ 2000 characters; question ≤ 1000 characters.
7. **Statement uniqueness**: no statement repeats an earlier one across
   evidence-backed statements and conflicts.
8. **Citations**: every statement carries at least one and at most 12 source
   ids; each id is well-formed (non-empty, ≤ 64 characters, no control
   characters), unique within its statement, and resolves to a source id in
   **this** execution's evidence snapshot. A source id from another execution
   does not resolve and is rejected.
9. **Status semantics**: `inconclusive` carries no evidence-backed statement
   or conflict; `partial` carries at least one; `complete` carries at least
   one evidence-backed statement; a synthesis over zero captured sources must
   be `inconclusive`.

A malformed, oversized, unsupported, cross-execution or unbound synthesis is
never persisted, and a record containing one fails closed on decode.

### Empty versus unavailable

* **Empty** — a synthesis with no evidence-backed statements and no conflicts
  is legal only as `inconclusive`.
* **Unavailable** — the record carries no `research_synthesis` at all. This
  means no synthesis was captured or validated for that execution (including
  every record written before v59). Absence is never interpreted as an empty
  answer, and it is never filled in on read.

### Partial and inconclusive

Both statuses are first-class durable values and keep their meaning. A
`partial` synthesis keeps its unresolved questions; an `inconclusive` synthesis
keeps its questions without claims. The UI renders them as partial and
inconclusive — never as a complete answer.

### Legacy records

A record written before v59 has no `research_synthesis` field and decodes
unchanged. The durable read omits the field, and the UI reports the synthesis
as unavailable for that record. Nothing is back-filled, migrated, or
reconstructed: no destructive migration and no rewrite of historical records.

## 2. Atomic lifecycle

The three artifacts form one snapshot. They are written in the same durable
record update, under the journal's existing mutex and optimistic-concurrency
check.

| Lifecycle path | Evidence | Evaluation | Synthesis |
|---|---|---|---|
| completed | written | written | written (validated) |
| failed / cancelled | written if captured | written (honest verdict) | none — a non-completed execution has no validated conclusion |
| approval pause | written if captured | written | none — the run has not produced a conclusion yet |
| approval resume (new evidence) | replaced | replaced | replaced |
| approval resume (no new evidence) | preserved | preserved | preserved |
| recovery classification | untouched | untouched | untouched |

Rules:

1. **Atomic.** `TerminalWithResearch` and `SupersedeApprovedPauseWithResearch`
   write evidence, evaluation and synthesis in one `putExecution` call. There
   is no second write and therefore no observable intermediate state in which
   one artifact is present and another is not.
2. **Bound.** The synthesis is validated against the exact evidence snapshot
   that will be persisted, and the evaluation is validated against the same
   snapshot. A synthesis can never be stored beside evidence it did not
   describe, and an evaluation can never describe evidence it did not evaluate.
3. **Never blocking.** The synthesis and the evaluation are derived data. One
   that does not bind to the snapshot being persisted is dropped rather than
   written, and it never fails the terminal lifecycle write. A valid evidence
   snapshot and the terminal record are always written.
4. **Replacement, not mutation.** There is no in-place update path. A new
   evidence snapshot replaces the evaluation and the synthesis together, so
   neither can outlive the snapshot it described. When the snapshot is
   preserved (a resumed run that captured no new evidence), the evaluation
   and synthesis that describe the preserved snapshot are preserved with it.
5. **No accidental inheritance.** A newer execution result never inherits a
   previous synthesis or evaluation. Each terminal write supplies its own
   artifacts, and a mismatched one is dropped rather than paired with the
   wrong evidence.
6. **First-wins.** `Terminal` keeps its first-wins semantics: a record that is
   already terminal is never rewritten. The single narrow approval-resume
   supersede path remains the only way a terminal record changes.
7. **No reconstruction.** Recovery classifies interrupted executions; it never
   rebuilds a synthesis, an evaluation or an evidence snapshot from guesses,
   from old process memory, or from another execution.

## 3. Approval and resume

The existing approval-grant-before-resume ordering and every governance
invariant are unchanged. Who can approve, execute or access an execution does
not change.

* An approval pause records evidence and an evaluation but **no synthesis**: the
  run has not produced a conclusion, and a pause is not an answer.
* On resume, a new evidence snapshot replaces the evidence, the evaluation and
  the synthesis together. The new synthesis describes the new evidence.
* On resume without a new evidence snapshot, the pause's evidence, evaluation
  and synthesis are preserved together. A retained synthesis is acceptable
  only because it still describes the exact retained evidence and the same
  execution id — the journal re-validates that binding on every read.

## 4. Surfaces

| Surface | Durable synthesis |
|---|---|
| Live intelligence read (`GET /api/v1/intelligence/{id}`) | unchanged: the process-local `outcome.research_synthesis` is still returned while the process holds it |
| Durable detail read (`GET /api/v1/recovery/executions/{id}`) | `research_synthesis`, returned only next to its evidence and evaluation |
| Durable list read | never repeated per row (bounded), like the evidence snapshot and the verdict |
| Journal decode | validated; an inconsistent record fails closed |

No new endpoint was created: the existing durable detail read already carries
the snapshot, and adding one field to it does not change the response's
authorization, scoping or shape rules.

### Intentional difference between live and recovered state

| | Live (same process) | Recovered (after restart) |
|---|---|---|
| Synthesis | process-local `outcome.research_synthesis` | persisted `research_synthesis` |
| Evidence | process-local + durable | durable only |
| Verdict | process-local `outcome.research_evaluation` | durable `research_evaluation` |
| Raw `result=` prose | available | **unavailable** (process-local) |

The synthesis itself is identical in both. Only the raw execution result text
is lost by a restart, and the UI says exactly that.

## 5. UI contract

The Research workspace distinguishes, explicitly:

* **Live execution state** — the process-local live synthesis is labeled
  "Synthesis" and is the answer on screen while the process holds it.
* **Persisted synthesis** — after a restart the same validated answer is
  rendered in a block labeled "Durable synthesis" and badged "Durable
  record", with the provenance stated in the panel. It is never presented as
  live state and never regenerated.
* **Evidence supporting the synthesis** — citations are re-resolved against
  the durable source records; titles and URLs never come from the synthesis.
* **The quality verdict and its limitations** — unchanged from v58.
* **Partial, inconclusive, failed or unavailable results** — a `partial` or
  `inconclusive` synthesis is rendered as such. A record with no persisted
  synthesis reports the synthesis as unavailable; it is never shown as an
  empty successful answer.
* **Legacy records** — a record written before v59 shows the same
  unavailable state, without invented content.

The live synthesis takes precedence over the durable one when both are
available, so the same answer is never rendered twice.

The client re-validates the durable synthesis defensively — version, method,
execution id, status, bounds, statement uniqueness, and citation resolution
against the parsed durable evidence — and hides the whole block rather than
rendering a partial or unverifiable answer.

## 6. Reference corpus

`internal/execjournal/research_synthesis_test.go` covers the schema, the
fingerprint, the execution/evidence binding, every bound, unknown and
malformed citations, duplicate statements, status semantics, atomic
persistence, restart survival, drop-on-mismatch, supersession, and concurrent
terminal writes.
`internal/core/execjournal_test.go` covers the engine path end to end: a
validated synthesis is written with the terminal record and survives a real
restart over the same durable directory.
`internal/gateway/research_synthesis_test.go` covers the API: the detail read
returns the synthesis, the list read does not, a legacy record stays readable
without one, and no source content leaks into the durable record.
`internal/agentintel/research_synthesis_test.go` covers the runtime: the loop
attaches the validated synthesis, it passes durable validation, and a
cross-execution citation is rejected before it can be persisted.
`e2e/tests/28-research.spec.ts` covers the real-binary restart: after a
genuine process restart the persisted synthesis, its citations and the quality
verdict are all rendered from the durable record.
