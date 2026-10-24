# NEXUS — Research Quality & Source Reliability Evaluation (v58–v60)

Authoritative for the deterministic research quality verdict. It extends the
existing research architecture; it never replaces it. Cross-references (never
redefines): `AGENT_INTELLIGENCE_CONTRACTS.md`,
`DURABLE_EXECUTION_RECOVERY_CONTRACTS.md`,
`DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md`,
`CAPABILITY_TOOL_CONTRACTS.md`, `CORE_INTERFACE_CONTRACTS.md`.

## 0. Scope

This milestone adds a **typed, versioned, deterministic evaluation** of one
execution's research output. It is a projection over two things the runtime
already owns:

1. the validated typed synthesis (`executor.ResearchSynthesis`), and
2. the durable evidence snapshot (`execjournal.ResearchEvidence` v3), including
   the exact plan/step snapshot, completion relationship and source-to-step
   operation records it captured.

It introduces **no second research engine, store, planner or evaluator**. It
adds no model call, no network call, no page fetch and no new persistence
system.

### The one rule that governs everything

> **A quality verdict may only state what the stored records can prove.**

Every finding is derived from execution-scoped identifiers, counts and
deterministic string facts. The evaluator never invents a source, a
provenance link, a date, an author, a score, a confidence value or a trust
ranking. Missing information is reported as missing — never as a pass and
never as a failure of the research itself.

### Explicit non-goals

| Not done | Why |
|---|---|
| No overall score, percentage, rating or "trust score" | A weighted number would imply a calibration the runtime does not have. |
| No source reputation, authority, freshness or independence claim | None of those is derivable from the stored provider envelope. |
| No page fetch, redirect follow or link resolution | Source quality is evaluated from the captured record only. |
| No semantic entailment / claim-support proof | Semantic verification is not available. Citation resolution is proven; entailment is reported as a limitation. |
| No model-assisted evaluation | v58 introduces no model-assisted assessment, so "malformed model evaluation output" is not applicable. |
| No new durable store | The evaluation is written through the existing execution journal, atomically with the evidence it evaluated. |

## 1. The evaluation object

`execjournal.ResearchEvaluation` (schema version `1`, method
`deterministic_v1`):

| Field | Meaning |
|---|---|
| `version` | evaluation schema version (`1`); unsupported → fail closed |
| `method` | `deterministic_v1`; the only writer of version 1 |
| `execution_id` | the execution this verdict belongs to; never another |
| `evidence_version` / `evidence_count` | the evidence snapshot that was evaluated (`0`/`0` when no snapshot existed) |
| `evidence_fingerprint` | SHA-256 over the normalized evidence JSON; binds the verdict to that exact snapshot |
| `plan_version` | the plan revision recorded in that evidence snapshot; `0` when the snapshot has no plan or a preserved v2 record never captured it |
| `synthesis_status` / `synthesis_fingerprint` | the synthesis that was evaluated. Since v59 the validated synthesis itself is also persisted (see `RESEARCH_SYNTHESIS_DURABLE_CONTRACTS.md`); this verdict still records only its status and fingerprint |
| `status` | overall verdict (§2) |
| `dimensions` | exactly eight dimension results (§3) |
| `findings` | stable machine-readable findings (§4) |
| `limitations` | what the evaluation could not determine |

Collections serialize as JSON arrays, never `null`.

The verdict contains **no model prose, no source title, URL, snippet or query,
no credential and no raw error text**. Findings carry a stable code, a generic
message, and execution-scoped ids only.

## 2. Statuses

Overall status — never a boolean, never a score:

| Status | Meaning |
|---|---|
| `passed` | every measurable deterministic check passed |
| `warnings` | no integrity failure, but at least one check reported a caveat |
| `incomplete` | a required input was missing or unavailable; the result could not be established |
| `failed` | an integrity violation was found (e.g. a citation that does not resolve in this execution) |

Dimension status:

| Status | Meaning |
|---|---|
| `pass` | the dimension was measurable and no problem was found |
| `warn` | the dimension was measurable and a caveat was found |
| `incomplete` | the dimension could not be measured (input missing) |
| `fail` | the dimension found an integrity violation |
| `not_measurable` | the available data cannot establish the property at all (e.g. conflict detection without model-declared conflicts) |
| `not_applicable` | the dimension does not apply to this result (e.g. relevance with zero sources) |

`not_measurable` and `not_applicable` never escalate the overall status; they
are always accompanied by a limitation entry. `failed` always outranks
`incomplete`, which outranks `warnings`, which outranks `passed`. The journal
rejects a record whose status contradicts its findings.

## 3. Dimensions

| Dimension | What is measured | What is explicitly not measured |
|---|---|---|
| `evidence_coverage` | every evidence-backed statement carries at least one citation that resolves in this execution | semantic support of the statement by the cited source |
| `citation_integrity` | statement/citation shape, bounds, uniqueness, execution-scoped source resolution, the evidence snapshot's binding to this execution, and the synthesis plan-revision binding | whether the citation is a good citation |
| `relevance` | deterministic meaningful-token overlap between a captured source and the objective, and (when the step intent is itself related to the objective) the step that retrieved it; step intent comes from the durable snapshot when v3 recorded it | semantic relevance, ranking quality, or "the answer is right" |
| `source_quality` | absolute `http(s)` URL with a host, usable title/excerpt, and explicit source-to-step attribution state | page content, reputation, authorship, freshness, safety of the page |
| `source_diversity` | exact canonical-URL duplication, exact normalized title+excerpt duplication, and host count | independence of sources, corroboration, or "one host is one source" |
| `conflict_handling` | conflicts the synthesis explicitly declares are cited and resolvable | contradictions between excerpts that the synthesis did not declare |
| `uncertainty_handling` | a `partial`/`inconclusive` synthesis states unresolved questions, and a `complete` synthesis is not asserted over a plan the runtime recorded as incomplete | whether the stated uncertainty is the *right* uncertainty |
| `plan_completeness` | the exact durable step states, dependencies, completion relationship and snapshot/runtime consistency, including failed, blocked, cancelled and nonterminal steps | whether the plan was a good plan |

Deterministic rules that make the verdict reproducible:

* Tokens are Unicode letters/digits, lowercased; tokens shorter than three
  characters and a fixed English stop-word list are dropped.
* A step intent is only used for the per-source relevance check when it has at
  least two meaningful tokens **and** shares at least one with the objective.
  A generic instruction ("gather the required inputs") cannot establish
  relevance and is skipped instead of producing a meaningless warning.
* Canonical URL normalization lowercases scheme/host, removes default ports and
  fragments, drops only well-known tracking parameters (`utm_*`, `gclid`,
  `fbclid`, `mc_cid`, `mc_eid`) and sorts the remaining query. Path case,
  meaningful query values and `www`/non-`www` stay distinct. The original URL
  is never rewritten in evidence or displayed.
* Content duplication compares the exact normalized title+excerpt pair. Near
  duplicates are not guessed.
* The step that carried a completed run's completion action is excluded from
  plan completeness, exactly like the runtime's own `completionIsPartial`
  verdict excludes it: a completed run settles that step through the
  completion action, not through an observation. The durable
  `completion_step_id` is authoritative when v3 recorded it.
* When v3 evidence carries a plan snapshot, its step states, dependencies,
  completion relationship and intents are authoritative. The process-local
  runtime plan is used only to detect a step the snapshot failed to record (a
  completeness gap) and as the intent fallback for preserved v2 evidence. A
  runtime status never overrides a durable status.
* A completed step whose recorded dependency is absent from the snapshot or did
  not settle makes the plan incomplete. An unknown dependency reference is
  reported, never guessed.
* Source-to-step attribution is `derived` only when the runtime wrote
  operation records that deterministically join sources to steps. A source with
  no operation record is `source_unattributed`; the evaluator never infers a
  step from order or similarity.

## 4. Findings

Findings use stable codes, a fixed generic message, a severity
(`warning`/`error`), the dimension they belong to and, when useful, an
execution-scoped `source_id`/`step_id`.

| Code | Severity | Meaning |
|---|---|---|
| `evidence_unavailable` | warning | a research attempt produced no complete snapshot |
| `evidence_invalid` | error | the captured snapshot is not structurally valid |
| `evidence_execution_mismatch` | error | the snapshot belongs to another execution |
| `legacy_evidence_no_provenance` | warning | v1 evidence has no stable ids or provenance |
| `synthesis_unavailable` | warning | no typed synthesis exists, so claim checks are unavailable |
| `synthesis_without_evidence` | error | a synthesis was produced without any evidence |
| `synthesis_without_stable_evidence` | error | a synthesis cannot be checked against legacy evidence |
| `synthesis_invalid` | error | the synthesis violates the completion contract |
| `synthesis_status_invalid` | error | unknown synthesis status |
| `synthesis_inconclusive_with_claims` | error | an inconclusive synthesis carries claims |
| `synthesis_partial_without_grounding` | error | a partial synthesis carries no cited statement or conflict |
| `synthesis_complete_without_evidence` | error | a complete synthesis has no evidence-backed statement, or asserts a conclusion over zero sources |
| `synthesis_complete_with_incomplete_plan` | error | a complete synthesis is asserted over a plan with incomplete steps |
| `statement_duplicate` / `statement_empty` / `statement_too_long` | error | statement shape violations |
| `statement_no_citations` / `statement_too_many_citations` | error | citation count violations |
| `citation_malformed` / `citation_duplicate` / `citation_unknown_source` | error | citation integrity violations (a source id that is not in this execution's evidence is an unknown source, including one from another execution) |
| `no_evidence_backed_claims` | warning | the synthesis carries qualifications but no evidence-backed conclusion |
| `source_invalid_url` / `source_low_information` | warning | a captured record is not usable as a source |
| `sources_all_low_information` | warning | no captured source has both a usable URL and usable text |
| `no_search_results` | warning | the search returned zero source records |
| `source_duplicate_url` / `source_duplicate_content` / `duplicate_only_evidence` | warning | duplication |
| `single_host_evidence` | warning | every source shares one host (not a claim of non-independence) |
| `source_unattributed` | warning | a source is not attributed to a recorded plan step |
| `source_no_query_overlap` / `no_relevant_sources` | warning | lexical relevance to the objective |
| `source_no_step_overlap` | warning | lexical relevance to the retrieving step |
| `unresolved_questions_missing` | warning | a partial/inconclusive synthesis states no unresolved question |
| `plan_unavailable` | warning | no durable plan/step status was available |
| `plan_step_state_unavailable` | warning | a step has no provable lifecycle status |
| `plan_step_failed` | warning | a step did not complete successfully |
| `plan_incomplete` | warning | a step was still nonterminal when the execution settled |
| `evidence_provenance_unavailable` | warning | v2 evidence predates v3 step intent, objective, completion and authoritative markers |
| `plan_step_missing_from_snapshot` | warning | the runtime plan contains a step the durable snapshot did not record |
| `plan_step_not_in_runtime_plan` | warning | the durable snapshot contains a step absent from the runtime plan |
| `plan_step_unknown_reference` | warning | a step depends on a step that is not present in the plan |
| `plan_dependency_incomplete` | warning | a completed step depends on a step that did not complete |
| `plan_completion_step_mismatch` | warning | the durable completion step differs from the runtime completion step |
| `source_attribution_unavailable` | warning | the v3 snapshot cannot prove which step captured a source |
| `synthesis_plan_version_mismatch` | error | the synthesis claims a plan revision that does not match its evidence |
| `synthesis_plan_version_unavailable` | warning/error | a v3 synthesis lacks the required plan revision (error); a preserved v2 synthesis without one stays readable (warning) |

## 5. Lifecycle, persistence and binding

1. **Attach.** The control loop computes the evaluation in its terminal defer,
   after the evidence snapshot is finalized and the synthesis (if any) has been
   validated. It is attached to the executor outcome on **every** terminal
   status, including failure, cancellation and approval pause.
2. **Absent for non-research runs.** An execution that never invoked
   `web.search` and has neither evidence nor a synthesis carries no
   evaluation at all.
3. **Persist atomically.** The journal writes evidence **and** evaluation in
   the same single record update (`TerminalWithResearch`,
   `SupersedeApprovedPauseWithResearch`). If the terminal write fails, the
   record stays nonterminal: no evaluation can exist without its evidence. An
   evaluation that does not bind to the snapshot being persisted — stale, or
   belonging to another execution — is dropped, never paired with the wrong
   evidence.
4. **Bind.** The evaluation carries the execution id, the evidence
   fingerprint and (when present) the evidence plan revision. The journal
   re-validates all three at write time and at decode time, so an evaluation
   can never be read against different evidence, a different plan revision, or
   another execution.
5. **Invalidate by replacement.** A non-nil evidence snapshot replaces the
   evaluation, even when the execution id is unchanged; a nil snapshot
   preserves both. The same rule applies to the synthesis. There is no in-place
   update path, so an evaluation can never outlive its snapshot or its plan
   revision.
6. **Auxiliary, never blocking.** The evaluation is derived data. If it fails
   validation it is dropped rather than persisted; it must never block the
   durable terminal record. When the persisted snapshot is preserved (a
   resumed run that captured no new evidence while the approval pause holds
   one), the verdict that describes the preserved snapshot is kept.
7. **Legacy.** A record written before this milestone decodes with no
   evaluation, and the UI reports the quality verdict as unavailable. Nothing
   is recomputed, back-filled or invented for it. Legacy v1 *evidence* stays
   readable as unattributed evidence and yields an `incomplete` verdict, never
   an upgraded one. Preserved v2 evidence is evaluated from its durable
   plan/step facts but explicitly reports `evidence_provenance_unavailable`;
   no v3 field is inferred.

## 6. Surfaces

| Surface | Evaluation |
|---|---|
| Live intelligence read (`GET /api/v1/intelligence/{id}`) | present as `outcome.research_evaluation` for a research execution |
| Durable detail read (`GET /api/v1/recovery/executions/{id}`) | present as `research_evaluation`, only next to its evidence snapshot |
| Durable list read | never repeated per row (bounded), like the evidence snapshot |
| Journal decode | validated; an inconsistent record fails closed |

## 7. UI contract

The Research workspace renders a quiet quality panel next to the synthesis:

* It is labeled "Quality checks" and states that it is deterministic: no
  model judgment, trust score or page fetch is involved.
* It shows the overall verdict, every dimension state, the findings with
  their stable codes, and the evaluator's limitations.
* A `failed` verdict is never rendered as a green summary; the panel states
  that the synthesis must not be read as a verified conclusion.
* After a restart the panel states that the verdict was recorded when the
  execution completed and that the synthesis shown alongside is the same
  persisted snapshot. The plan revision is stated when the verdict carries one.
* The client re-validates the object defensively: an unknown version, an
  unknown dimension, an execution-id mismatch, an evidence-binding mismatch, a
  plan-revision mismatch or a status that contradicts the findings hides the
  whole panel instead of rendering a partial verdict.

## 8. Reference corpus

`internal/researchquality/evaluate_test.go` is the version-controlled
evaluation corpus. It covers, with documented expected verdicts: strongly
supported research, zero search results, unavailable evidence, invalid URL and
low-information records, duplicate-URL-only evidence, evidence bound to another
execution, a failed plan step, a citation to a source absent from this
execution, legacy evidence, materially conflicting excerpts, a duplicated
citation, a complete synthesis over an incomplete plan, a partial synthesis
without unresolved questions, a statement without citations, content
duplication across hosts, and a malformed synthesis. The redaction test proves
the verdict carries no query, URL, snippet or title.
`internal/researchquality/provenance_test.go` covers the v60 inputs: durable
plan facts taking precedence over the runtime plan, v2 provenance unavailable,
runtime/snapshot step mismatches, unknown and incomplete dependencies,
completion-step mismatch, attribution unavailable, and synthesis plan-revision
mismatch/unavailability.
