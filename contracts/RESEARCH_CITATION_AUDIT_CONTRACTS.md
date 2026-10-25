# NEXUS — Research Citation Integrity Audit (v62)

Authoritative for the deterministic, claim-level audit of citation integrity
over one durable research synthesis and its exact evidence snapshot. It
extends the existing research architecture; it never replaces it.
Cross-references (never redefines): `RESEARCH_SYNTHESIS_DURABLE_CONTRACTS.md`,
`RESEARCH_PROVENANCE_COMPARISON_CONTRACTS.md`,
`DURABLE_EXECUTION_RECOVERY_CONTRACTS.md`,
`DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md`.

## 0. Scope

v62 adds an **on-demand, read-only, bounded, deterministic claim-level audit**
of the citation integrity of one durable research synthesis:

1. every persisted claim (evidence-backed statement, conflict, inference,
   unresolved question),
2. each recorded citation identity and its exact execution-scoped resolution,
3. evidence availability (`content_available`, `metadata_available`),
4. recorded provenance,
5. snapshot/plan bindings,
6. structural findings, and
7. stable audit statuses.

It introduces **no second store, planner, graph, model call, network call,
background process or durable audit state**. The audit is a pure projection
over the exact `execjournal.ResearchSynthesis` and
`execjournal.ResearchEvidence` passed by the caller. No audit record is
persisted and no execution is mutated.

### The one rule that governs every audit

> The audit assesses **structural citation integrity and evidence
> availability only**. It never establishes factual truth, source quality,
> relevance, contradiction or semantic entailment.

A resolved citation means the reference exactly matches a recorded source id
in this execution's exact evidence snapshot. It does not mean the source
supports the statement, that the source is true, or that the statement is
entailed by the source. Evidence available means a source record is present;
it says nothing about the source content.

Missing, legacy or incompatible bindings are reported explicitly. They are
never inferred from URLs, titles, ordering, neighboring records or newer
snapshots. An absent synthesis is not an empty or clean synthesis. The audit
never consults another execution.

## 1. API

### Route

```
GET /api/v1/recovery/executions/{id}/audit?business_id=<business>
```

The route is scoped under the existing single-execution recovery detail
route. It is a bounded, authenticated, membership/business/division-authorized
read-only projection; no mutation endpoint exists.

### Response

```json
{
  "source": "durable_journal",
  "version": 1,
  "method": "deterministic_v1",
  "execution_id": "<execution>",
  "evidence_version": 3,
  "evidence_fingerprint": "<64-hex>",
  "synthesis_version": 1,
  "synthesis_method": "deterministic_v1",
  "synthesis_fingerprint": "<64-hex>",
  "synthesis_status": "complete | partial | inconclusive | ''",
  "plan_version": 1,
  "plan_version_recorded": true,
  "evidence_plan_version": 1,
  "evidence_plan_recorded": true,
  "provenance": "derived",
  "status": "passed | warnings | failed | incomplete | unavailable | incompatible",
  "claims": [],
  "findings": [],
  "limitations": [],
  "truncated": false
}
```

Each claim carries: `claim_id`, `category` (`evidence_backed`, `conflict`,
`inference`, `unresolved_question`), `index`, `text`,
`citation_requirement` (`required` | `not_required`), `status`,
`provenance` and its `citations`. Each citation carries `index`, `reference`
(never trimmed or repaired), `status`, `evidence_status`,
`content_available`, `metadata_available` and `provenance`.

Each finding carries `code`, `severity` (`warning` | `error`) and optional
`claim_id`/`claim_index`, `citation_index` and `source_id`.

### Status semantics

- `200` — the caller is authorized for the execution's business and division,
  and a bounded audit projection is returned. The audit body itself reports
  `passed`, `warnings`, `failed`, `incomplete`, `unavailable` or
  `incompatible`.
- `400` — missing/invalid `business_id` or execution id.
- `401` — unauthenticated.
- `403` — the caller is not a member of the requested business.
- `404` — the execution record is missing, foreign, unreadable or outside the
  caller's division scope. These cases are indistinguishable; no existence
  leak.
- `503` — the journal is corrupt or the projection is invalid; fails closed.

Authorized missing synthesis returns `200` with an `unavailable` audit.
Incompatible bindings return `200` with an `incompatible` audit. Claims are
not audited against a different evidence snapshot.

Authorization is membership/business/division-scoped and follows the existing
recovery authorization. Cross-business access is rejected.

## 2. Claims and categories

Claim categories mirror the durable synthesis schema:

- `evidence_backed` — a persisted statement requiring citations.
- `conflict` — a persisted conflict requiring citations.
- `inference` — a persisted inference; citations not required.
- `unresolved_question` — a persisted question; citations not required.

Citation-bearing categories (`evidence_backed`, `conflict`) require at least
one recorded citation. Inferences and unresolved questions carry no citations
and their provenance is `not_applicable`. Claim identity hashes normalized
statement text across a shared evidence/conflict namespace; inferences and
questions include category and ordinal because duplicates are allowed.

### Claim statuses

- `passed` — every citation resolved against available evidence, no findings.
- `warnings` — citations resolved, but availability/provenance warnings remain.
- `failed` — a structural citation or claim-identity error was recorded.
- `unavailable` — the claim was not audited because the bindings are
  incompatible.
- `not_applicable` — citations not required (inference or question).

## 3. Citations

Each recorded citation reference is resolved exactly as recorded against the
source ids in this execution's evidence snapshot.

### Citation statuses

- `resolved` — exactly one source record matches the reference; evidence
  available.
- `unresolved` — no source record matches; evidence missing.
- `malformed` — the reference is empty, over-bound or carries a forbidden
  control character.
- `duplicate` — the claim repeats an earlier reference.
- `ambiguous` — more than one source record carries the same id.
- `unavailable` — the binding is incompatible; no resolution attempted.

Cross-execution and out-of-scope citation ids are indistinguishable from
unresolved ids in an exact snapshot. The audit never reads another execution.

## 4. Evidence

Evidence statuses: `available`, `missing`, `ambiguous`, `unavailable`.
Separate `content_available` and `metadata_available` booleans report only
whether recorded fields are non-empty.

Provenance: `authoritative` (v3), `legacy` (v2), `unavailable`
(v1/incompatible/unresolved), `not_applicable` (inferences/questions). The
top-level audit provenance is always `derived`.

Compatibility: v1 evidence plus synthesis is `incompatible`; v2 evidence is
audited with `legacy_provenance` warnings and incomplete plan binding when
`plan_version=0`; v3 evidence is authoritative. Fingerprint, execution and
plan mismatches are `incompatible`; claims are not audited.

## 5. Stable findings

Stable finding codes within audit schema version 1:

- `citation_missing`
- `citation_malformed`
- `citation_duplicate`
- `citation_unresolved`
- `citation_ambiguous`
- `evidence_content_unavailable`
- `evidence_metadata_unavailable`
- `legacy_provenance`
- `provenance_unavailable`
- `synthesis_snapshot_mismatch`
- `evidence_snapshot_mismatch`
- `plan_revision_unavailable`
- `plan_revision_mismatch`
- `synthesis_schema_incompatible`
- `evidence_schema_incompatible`
- `synthesis_invalid`
- `evidence_invalid`
- `duplicate_claim_identity`
- `claim_identity_malformed`
- `synthesis_unavailable`
- `evidence_unavailable`
- `no_cited_claims`
- `audit_output_truncated`

Findings are diagnostics, not research-quality verdicts. `warning` limits
confidence; `error` is structural. No "info" severity exists.

## 6. Bounds

- `MaxClaims = 64`
- `MaxCitationsPerClaim = 12`
- `MaxCitationReference = 512` bytes
- `MaxClaimText = 2000` bytes
- `MaxQuestionText = 1000` bytes
- `MaxFindings = 256`
- `MaxLimitations = 16`
- `MaxText = 512` bytes
- `MaxID = 128` bytes
- `MaxExecutionID = 512` bytes
- `MaxAuditBytes = 2 MiB`

Inputs outside these bounds are rejected before projection. Output truncation
is explicit: `truncated: true`, the first `MaxFindings - 1` canonical
findings retained, plus one `audit_output_truncated` finding.

## 7. Legacy and missing artifacts

- v1 evidence is `incompatible`: legacy v1 has no stable source ids, so
  citations cannot be resolved against it.
- v2 evidence is audited against its recorded source ids with
  `legacy_provenance` warnings; v3 authoritative markers are unavailable.
- v3 evidence supplies full authoritative provenance.
- A missing synthesis is `unavailable`, never empty or clean.
- Incompatible synthesis/evidence/execution/plan bindings are `incompatible`;
  claims are not audited against a different snapshot.
- Missing v3 provenance stays `unavailable`; it is never inferred from URLs,
  titles, order or newer snapshots.

## 8. Research workspace UI

The existing Research workspace gains one restrained citation audit section,
rendered near the durable synthesis:

- fetched only when a durable synthesis is present; never polls;
- status label (`passed` / `warnings` / `failed` / `incomplete` /
  `unavailable` / `incompatible`);
- per-claim status and category; per-citation resolution, evidence
  availability and provenance;
- stable finding codes/severity and fixed limitations;
- explicit copy: "Citation resolved," "Evidence available," "Semantic support
  not verified," "Absence is not an empty or clean synthesis."

It adds no dashboard, KPI, chart, trust score, confidence score, semantic
verdict or automatic navigation. Audit text is rendered as text, never
linkified. Source references are labels only.

## 9. Non-goals

- No audit persistence, backfill, migration or upgrade of old records.
- No factual verification, source-support judgment, contradiction detection or
  semantic similarity.
- No trust/confidence scoring or "verified facts" labels.
- No prompts, credentials, hidden reasoning, model scratch text, snippets or
  raw provider payloads in the audit.
- No process-local runtime state, replay or reconciliation.
- No cross-business access; no implied access from one authorized execution to
  another.
- No repair or rewrite of citations; incompatible bindings are marked, never
  silently corrected.
