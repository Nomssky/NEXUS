# NEXUS — Research Provenance Comparison (v61)

Authoritative for the deterministic comparison of two durable research
snapshots. It extends the existing research architecture; it never replaces it.
Cross-references (never redefines): `RESEARCH_QUALITY_EVALUATION_CONTRACTS.md`,
`RESEARCH_SYNTHESIS_DURABLE_CONTRACTS.md`,
`DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md`,
`DURABLE_EXECUTION_RECOVERY_CONTRACTS.md`, `SCHEMA_IDENTITIES_ORG.md`,
`CORE_INTERFACE_CONTRACTS.md`.

## 0. Scope

v61 adds a **read-only, bounded, deterministic comparison** of the durable
research provenance already owned by two execution records:

1. the plan snapshot,
2. the step lifecycle snapshot and dependencies,
3. the mediated operation/source attribution,
4. the evidence snapshot,
5. the durable synthesis and its citations, and
6. the durable quality evaluation and its findings/dimensions.

It introduces **no second store, planner, graph, evaluator, model call, network
call or background process**. A comparison is a projection over two validated
`execjournal.ExecutionRecord` values. No comparison record is persisted.

### The one rule that governs every comparison

> A comparison reports **recorded artifact differences only**. It never
> establishes factual truth, source support, contradiction, correctness,
> improvement, trust, confidence or which execution is better.

Missing, legacy or incompatible facts are reported as unavailable. They are
never inferred from ordering, runtime state, similarity, another execution, or
the model's prose. An absent synthesis or evaluation is not an empty or clean
artifact. A changed fingerprint proves that the recorded record changed; it
does not prove which record is true.

## 1. API

### Route

```
GET /api/v1/recovery/comparisons
    ?business_id=<business>
    &left_execution_id=<execution>
    &right_execution_id=<execution>
```

The route is separate from the single-execution detail route
(`GET /api/v1/recovery/executions/{id}`) and the list route. It is a read-only
projection; no mutation endpoint exists.

### Response

```json
{
  "source": "durable_journal",
  "version": 1,
  "method": "deterministic_v1",
  "left": { "...": "side summary" },
  "right": { "...": "side summary" },
  "compatibility": {
    "status": "comparable | partial | incompatible",
    "categories": [
      { "category": "plan", "status": "comparable | partial | unavailable | incompatible" },
      { "category": "steps", "status": "..." },
      { "category": "operations", "status": "..." },
      { "category": "evidence", "status": "..." },
      { "category": "synthesis", "status": "..." },
      { "category": "evaluation", "status": "..." }
    ]
  },
  "differences": [],
  "findings": [],
  "limitations": [],
  "truncated": false
}
```

The side summary contains only: role, execution id, division id (when
recorded), lifecycle state, real `created_at`/`completed_at`, bounded evidence
metadata, plan metadata, synthesis metadata, evaluation metadata and provenance
labels. It never contains actor ids, recovery reasons, prompts, raw durable
bytes, source snippets, synthesis prose or evaluation finding messages.

### Status semantics

- `200` — both executions were independently authorized, both are eligible,
  and the comparison is valid.
- `400` — missing/invalid ids, same id, oversized id, or missing
  `business_id`.
- `401` — unauthenticated.
- `403` — the caller is not a member of the requested business.
- `404` — a record is missing, foreign, unreadable or outside the caller's
  division scope. These cases are indistinguishable.
- `409` — both records were authorized, but at least one is not a settled
  comparison snapshot (nonterminal, or a provisional
  `failed`/`APPROVAL_REQUIRED` approval pause).
- `503` — the journal or a validated comparison is unavailable.

Authorization is checked **independently for both sides**. An authorized left
record never grants access to the right record. A business-wide membership
explicitly permits divisionless and cross-division records inside that
business; a division-scoped membership never covers divisionless scope or
another division. Cross-business comparisons are rejected because both records
must match the single requested `business_id`.

### Eligibility

Eligible states are the terminal states (`completed`, `failed`, `cancelled`,
`timed_out`) and the recovery-settled `unknown` state. A provisional approval
pause (`failed` + `APPROVAL_REQUIRED`) is excluded because the narrow
approval-resume path can still supersede it. Ineligible authorized comparisons
return `409`.

## 2. Snapshot coherence

The gateway reads both records through `Journal.GetPair`. It holds the journal
mutex across both reads, so a journal transition cannot land between them. Each
returned record is still one independently validated atomic record: the pair
read never merges fields from different versions. Evidence, synthesis and
evaluation are written atomically by the journal, so each side's artifacts are
bound to the same execution and evidence fingerprint.

## 3. Identity rules

### Sources

Sources are paired only by a canonical fingerprint of the **recorded source
fields** (id when present, title, URL, source and snippet). They are never
paired by URL, title, snippet, source name or execution-scoped source id alone.
If an identical URL appears on both sides but the recorded fingerprints differ,
the comparison emits `source_url_not_identity`; it does not pair the records.
The same URL or similar text does not establish source identity.

### Steps and operations

Steps and operations are paired by their plan/execution-scoped id labels so the
reader can inspect corresponding labels. Matching labels are **not** proof of
the same logical step or operation; the comparison always carries
`step_identity_scope` and `operation_identity_scope` findings when those
categories are compared. Dependencies and source references are compared as
recorded sets.

### Plans and objectives

Plan ids and objective references are execution-scoped. They are visible in
each side summary but are never compared as content or used to infer a shared
plan/objective identity. Plan version, completion-step relationship (v3),
step intent/plan revision (v3), and objective presence are compared only when
the recorded schema supports them.

### Synthesis and evaluation

Synthesis is compared by status, plan-revision binding, counts, summary
fingerprint, statement/citation fingerprints and inference/question/conflict
sets. Statement fingerprints cover the recorded statement text and the
canonical fingerprints of its citations. No synthesis prose is copied into the
comparison. Evaluation is compared by status, plan-revision binding, evidence
metadata, synthesis-status binding, dimensions, stable finding codes/severity
and limitation fingerprints. No evaluation message is copied into the
comparison. The evaluation's recorded synthesis fingerprint covers the
pre-persistence executor synthesis, not the durable synthesis record, so it is
retained as metadata and never equated with the durable synthesis fingerprint.

## 4. Difference model

Each difference has a bounded category, kind, entity, optional id/field,
bounded left/right display values, optional SHA-256 fingerprints and a
provenance label.

Categories, in canonical order: `plan`, `steps`, `operations`, `evidence`,
`synthesis`, `evaluation`.

Kinds: `added` (right-only), `removed` (left-only), `changed`, `unavailable`,
`incompatible`, `legacy`.

The canonical order is: category order, entity, id, field, kind, left, right,
left fingerprint, right fingerprint. Differences are deduplicated and
deterministically truncated.

## 5. Compatibility findings

Stable finding codes include:

- `comparison_incompatible`
- `comparison_no_recorded_differences`
- `comparison_output_truncated`
- `evidence_missing_on_left` / `evidence_missing_on_right`
- `evidence_unavailable_on_both_sides`
- `evidence_version_changed`
- `evidence_fingerprint_incomparable`
- `source_url_not_identity`
- `plan_provenance_unavailable_on_left` / `..._right` / `..._both_sides`
- `legacy_evidence_no_provenance`
- `legacy_plan_provenance_unavailable`
- `plan_objective_unavailable_on_left` / `..._right`
- `step_identity_scope`
- `operation_identity_scope`
- `synthesis_missing_on_left` / `..._right`
- `synthesis_unavailable_on_both_sides`
- `synthesis_schema_incompatible`
- `synthesis_plan_version_unavailable_on_left` / `..._right` / `..._both_sides`
- `evaluation_missing_on_left` / `..._right`
- `evaluation_unavailable_on_both_sides`
- `evaluation_schema_incompatible`
- `evaluation_absent_not_clean`
- `evaluation_plan_version_unavailable_on_left` / `..._right` / `..._both_sides`

Findings are diagnostics, not research-quality verdicts. `info`, `warning` and
`error` describe comparison availability only.

## 6. Bounds

- `MaxDifferences = 512`
- `MaxFindings = 64`
- `MaxLimitations = 16`
- `MaxText = 512` bytes per display value
- `MaxID = 128` bytes per entity/id/field label
- `MaxComparisonBytes = 2 MiB`

Truncation is explicit: `truncated: true` and a
`comparison_output_truncated` finding. The first canonical differences/findings
are retained. Strings are sanitized and clipped at UTF-8 boundaries.

## 7. Legacy and missing artifacts

- v1 evidence is readable legacy provenance: sources are compared by recorded
  content fingerprints, but there are no source ids, plan, steps, operations or
  attribution. Plan/step/operation categories are unavailable when neither
  side can supply them; a mixed v1/v3 comparison is partial when the v3 side
  records the artifact.
- v2 evidence has plan/step/operation records but no v3 intent, objective,
  completion, attribution marker or authoritative markers. Those fields are
  unavailable, not invented.
- v3 evidence supplies the full authoritative provenance.
- A missing synthesis or evaluation is reported as missing/unavailable, never
  as empty, clean or passing. A present evaluation with an absent synthesis is
  not a clean verdict. A present evaluation must carry exactly the eight
  durable dimensions.
- Different evidence schema versions still allow source-record comparison, but
  snapshot fingerprints are not compared across versions.
- Plan/step/operation/evidence-metadata differences are labeled
  `authoritative` only when both snapshots are v3; otherwise they are
  `legacy`. Source-record, synthesis and evaluation differences are `derived`.

## 8. Research workspace UI

The existing Research workspace gains one restrained comparison section:

- two eligible-run selectors (left/right) and a Compare action;
- side metadata: execution id, state, real timestamps, evidence schema, plan
  revision, synthesis/evaluation presence;
- compatibility status and category states;
- grouped differences by category with bounded left/right values;
- stable finding codes/severity and fixed limitations;
- explicit legacy/missing/unavailable copy.

It adds no dashboard, KPI, chart, trust score, confidence score, semantic
verdict or automatic navigation. Comparison text is rendered as text, never
linkified. Source titles/URLs are labels only.

## 9. Non-goals

- No comparison persistence, backfill, migration or upgrade of old records.
- No factual verification, source-support judgment, contradiction detection or
  semantic similarity.
- No trust/confidence scoring or "which run is better".
- No prompts, credentials, hidden reasoning, model scratch text, snippets or
  raw provider payloads in the comparison.
- No process-local runtime state, replay or reconciliation.
- No cross-business access; no implied access from one authorized side to the
  other.
