/**
 * Research result model.
 *
 * The intelligence execution read exposes one human-readable string
 * (`outcome.summary`). It does not expose tool results, observations, or
 * source records after the execution finishes. This module parses that
 * string defensively and never infers citations from prose.
 *
 * Actual agent-intelligence summary (internal/agentintel/loop.go):
 *
 *   state=completed | objective=<description> | agent=<id> |
 *   iterations=N tool_calls=N delegations=N model_calls=N replans=N
 *   observations=N | result=<model completion text>
 *
 * Failed and cancelled outcomes carry only `state=<state>`. The `result=`
 * segment is model-generated prose; it is displayed as text and is never
 * linkified or treated as source metadata.
 *
 * The only structured form recognized here is the documented `web.search`
 * tool output schema (internal/capability/adapters.go):
 *
 *   { "sources": "1. Title | URL | Source | Snippet\n...", "count": "N" }
 *
 * It is recognized only when the whole execution output is that JSON object.
 * Any other JSON (malformed, partial, or a different shape) is rendered as
 * plain text. Unknown JSON is not pretty-printed into an implied structure.
 *
 * v52 adds the durable evidence read: the recovery detail endpoint can return
 * a persisted web.search source snapshot (`research_evidence`) and the real
 * terminal timestamp. That snapshot is provider-grounded data captured at the
 * capability boundary, and it survives restarts. It is parsed defensively with
 * the same wholesale-rejection rules the journal writer applies, and rendered
 * through the same ParsedWebSearchSource shape as the process-local envelope.
 *
 * v54 extends that snapshot (schema v2) with runtime-owned plan identity,
 * ordered step evidence and operation-to-source associations. Attribution is
 * read only from those persisted operation records; it is never inferred from
 * source order, titles, URLs or timestamps. Legacy v1 evidence remains
 * readable without invented ids or provenance.
 */

import type {
  DurableResearchEvidence,
  DurableResearchPlan,
  DurableResearchStepStatus,
  ParsedWebSearchSource,
  ResearchSynthesis as APIResearchSynthesis,
  TraceEntry,
  WebSearchResult,
} from './api';
import { isSafeExternalUrl } from './api';

export interface ResearchSummaryMetrics {
  iterations?: number;
  toolCalls?: number;
  delegations?: number;
  modelCalls?: number;
  replans?: number;
  observations?: number;
}

export type ResearchResultKind = 'empty' | 'text' | 'summary' | 'structured';

export interface ResearchResult {
  kind: ResearchResultKind;
  /** Raw execution output, exactly as returned by the backend. */
  raw: string;
  /** `result=` content from an agent-intelligence summary. Model-generated. */
  resultText?: string;
  /** `objective=` content from an agent-intelligence summary. */
  objective?: string;
  /** `agent=` content from an agent-intelligence summary. */
  agent?: string;
  /** `state=` value from an agent-intelligence summary. */
  runtimeState?: string;
  /** Runtime counters from the summary prefix. Real values only. */
  metrics: ResearchSummaryMetrics;
  /** Documented web.search output envelope, only when the whole output is it. */
  webSearch?: WebSearchResult;
}

export interface ResearchToolActivity {
  /** Call identity from the trace, when present. Used only as a list key. */
  callId?: string;
  toolId: string;
  outcome: string;
  attempts?: number;
  durationMs?: number;
  resultBytes?: number;
  errorKind?: string;
}

const SUMMARY_FIELD_NAMES = [
  'objective',
  'agent',
  'iterations',
  'tool_calls',
  'delegations',
  'model_calls',
  'replans',
  'observations',
] as const;

const METRIC_NAMES: Record<string, keyof ResearchSummaryMetrics> = {
  iterations: 'iterations',
  tool_calls: 'toolCalls',
  delegations: 'delegations',
  model_calls: 'modelCalls',
  replans: 'replans',
  observations: 'observations',
};

const TERMINAL_TOOL_EVENTS = new Set([
  'tool.invocation.completed',
  'tool.invocation.failed',
  'tool.invocation.unknown',
  'tool.invocation.rejected',
]);

/**
 * Parse one execution output into a display model. This function is total:
 * every input produces a result and no input is allowed to throw.
 */
export function parseResearchResult(output?: string | null): ResearchResult {
  const raw = output ?? '';
  if (raw.trim() === '') return { kind: 'empty', raw, metrics: {} };

  const structured = parseStructuredWebSearch(raw);
  if (structured) {
    return { kind: 'structured', raw, metrics: {}, webSearch: structured };
  }

  if (/^state=[^\s|]+/.test(raw)) {
    return parseSummary(raw);
  }

  return { kind: 'text', raw, metrics: {} };
}

/**
 * Recognize only the documented web.search output envelope. The whole output
 * must be a JSON object with a string `sources` field and a string or numeric
 * `count`. Everything else falls back to text — including JSON arrays,
 * nested objects, and partial payloads.
 */
function parseStructuredWebSearch(raw: string): WebSearchResult | undefined {
  const trimmed = raw.trim();
  if (!trimmed.startsWith('{') || !trimmed.endsWith('}')) return undefined;

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return undefined;
  }
  if (!isRecord(parsed)) return undefined;

  const sources = parsed.sources;
  const count = parsed.count;
  if (typeof sources !== 'string') return undefined;
  if (typeof count !== 'string' && typeof count !== 'number') return undefined;
  if (typeof count === 'number' && !Number.isFinite(count)) return undefined;

  return { sources, count: String(count) };
}

function parseSummary(raw: string): ResearchResult {
  const stateMatch = /^state=([^\s|]+)/.exec(raw);
  const runtimeState = stateMatch?.[1];

  // `result=` is the final field and may itself contain " | ". Split it off
  // before parsing the key=value prefix so result prose is never re-split.
  const resultMarker = ' | result=';
  const resultIndex = raw.indexOf(resultMarker);
  const prefix = resultIndex >= 0 ? raw.slice(0, resultIndex) : raw;
  const resultText = resultIndex >= 0 ? raw.slice(resultIndex + resultMarker.length) : undefined;

  const metrics: ResearchSummaryMetrics = {};
  const metricPattern = /\b(iterations|tool_calls|delegations|model_calls|replans|observations)=(\d+)\b/g;
  for (const match of prefix.matchAll(metricPattern)) {
    const key = METRIC_NAMES[match[1]];
    if (key) metrics[key] = Number(match[2]);
  }

  return {
    kind: 'summary',
    raw,
    runtimeState,
    resultText: resultText?.trim() ? resultText : undefined,
    objective: extractSummaryField(prefix, 'objective'),
    agent: extractSummaryField(prefix, 'agent'),
    metrics,
  };
}

/**
 * Extract ` | key=value` from the summary prefix, stopping at the next known
 * ` | field=` marker. Unknown markers are treated as part of the value, which
 * keeps objective/result prose with pipes intact.
 */
function extractSummaryField(prefix: string, key: string): string | undefined {
  const marker = ` | ${key}=`;
  const markerIndex = prefix.indexOf(marker);
  if (markerIndex < 0) return undefined;

  const valueStart = markerIndex + marker.length;
  let valueEnd = prefix.length;
  for (const next of SUMMARY_FIELD_NAMES) {
    if (next === key) continue;
    const nextIndex = prefix.indexOf(` | ${next}=`, valueStart);
    if (nextIndex >= 0 && nextIndex < valueEnd) valueEnd = nextIndex;
  }

  const value = prefix.slice(valueStart, valueEnd).trim();
  return value === '' ? undefined : value;
}

/**
 * Extract completed/failed/unknown/rejected tool invocations from an
 * execution trace. Trace entries are execution metadata, not evidence: the
 * trace allowlist drops tool results, prompts, and model output, so this
 * function can never recover source records.
 */
export function extractToolActivity(
  entries?: TraceEntry[] | null,
  toolId?: string
): ResearchToolActivity[] {
  if (!entries || entries.length === 0) return [];

  const activity: ResearchToolActivity[] = [];
  for (const entry of entries) {
    if (!TERMINAL_TOOL_EVENTS.has(entry.type)) continue;

    const data = entry.data ?? {};
    const entryToolId = data.tool_id;
    if (!entryToolId) continue;
    if (toolId && entryToolId !== toolId) continue;

    activity.push({
      callId: data.call_id || undefined,
      toolId: entryToolId,
      outcome: data.outcome || outcomeForEvent(entry.type),
      attempts: parseNonNegativeInt(data.attempts),
      durationMs: parseNonNegativeInt(data.duration_ms),
      resultBytes: parseNonNegativeInt(data.result_bytes),
      errorKind: data.error_kind || undefined,
    });
  }
  return activity;
}

function outcomeForEvent(type: string): string {
  switch (type) {
    case 'tool.invocation.completed':
      return 'completed';
    case 'tool.invocation.failed':
      return 'failed';
    case 'tool.invocation.unknown':
      return 'unknown';
    case 'tool.invocation.rejected':
      return 'rejected';
    default:
      return 'unknown';
  }
}

function parseNonNegativeInt(value: string | undefined): number | undefined {
  if (value === undefined || value.trim() === '') return undefined;
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed < 0 || !Number.isInteger(parsed)) return undefined;
  return parsed;
}

// ---- Durable research evidence (v52, extended by v54) ----

/**
 * Bounds mirror internal/execjournal/evidence.go. A durable snapshot that
 * exceeds any bound is rejected wholesale; the UI never renders a partial or
 * silently truncated provenance record.
 */
const MAX_DURABLE_TITLE = 512;
const MAX_DURABLE_URL = 2048;
const MAX_DURABLE_SOURCE = 256;
const MAX_DURABLE_SNIPPET = 1024;
const MAX_DURABLE_SOURCES = 50;
const MAX_DURABLE_PLAN_STEPS = 64;
const MAX_DURABLE_STEP_DEPENDENCIES = 32;
const MAX_DURABLE_OPERATIONS = 64;
const MAX_DURABLE_OPERATION_SOURCE_REFS = 50;
const MAX_DURABLE_PLAN_ID = 128;
const MAX_DURABLE_STEP_ID = 128;
const MAX_DURABLE_OBSERVATION_ID = 128;
const MAX_DURABLE_OPERATION_ID = 128;
const MAX_DURABLE_SOURCE_ID = 64;
const MAX_DURABLE_PLAN_VERSION = 1_000_000;

/** Schema version the journal writes today; v1 is readable legacy evidence. */
const DURABLE_EVIDENCE_VERSION = 2;
const DURABLE_EVIDENCE_LEGACY_VERSION = 1;
const DURABLE_EVIDENCE_CAPABILITY = 'web.search';
const DURABLE_EVIDENCE_OPERATION = 'search';

/**
 * One recovered plan step. Order is the durable plan order, not source order
 * or URL order. Sources are attached only through operation records the
 * runtime wrote at tool-mediation time.
 */
export interface ParsedDurableStep {
  stepId: string;
  status: DurableResearchStepStatus;
  dependencies: string[];
  observationId?: string;
  sources: ParsedWebSearchSource[];
}

export interface ParsedDurableEvidence {
  /**
   * True only when the recovery detail read returned a structurally valid
   * web.search evidence snapshot. Presence means the execution journal holds a
   * complete capture; absence means no valid capture was persisted — never
   * "the search returned nothing". An empty search is present evidence with
   * zero sources.
   */
  present: boolean;
  /** Evidence schema version (2 today; 1 is readable legacy evidence). */
  version?: number;
  capability?: string;
  operation?: string;
  /** The count declared by the provider envelope; equals sources.length. */
  count?: number;
  sources: ParsedWebSearchSource[];
  /**
   * Plan identity from the durable record. Absent on legacy v1 evidence and on
   * v2 source-only captures that were never associated with a plan.
   */
  plan?: DurableResearchPlan;
  /** Ordered durable steps. Empty for legacy or source-only evidence. */
  steps: ParsedDurableStep[];
  /**
   * Sources that no operation references. Kept explicit instead of guessing a
   * step association; legacy v1 evidence places every source here.
   */
  unattributedSources: ParsedWebSearchSource[];
}

const NO_DURABLE_EVIDENCE: ParsedDurableEvidence = {
  present: false,
  sources: [],
  steps: [],
  unattributedSources: [],
};

const DURABLE_STEP_STATUSES = new Set<DurableResearchStepStatus>([
  'pending',
  'ready',
  'executing',
  'observing',
  'completed',
  'failed',
  'blocked',
  'cancelled',
]);

/**
 * Parse the durable research_evidence field from the recovery detail read.
 * Like the journal writer, validation is wholesale: one malformed row, a
 * count mismatch, an unsupported version, an over-bound value, or provenance
 * that references an unknown step/source rejects the whole snapshot instead
 * of rendering a partial record as evidence.
 *
 * v1 legacy evidence remains readable exactly as written: no plan, steps,
 * operations, source ids, or timestamps are invented for it.
 */
export function parseDurableResearchEvidence(
  evidence?: DurableResearchEvidence | null
): ParsedDurableEvidence {
  if (!isRecord(evidence)) return NO_DURABLE_EVIDENCE;
  if (evidence.capability !== DURABLE_EVIDENCE_CAPABILITY) return NO_DURABLE_EVIDENCE;

  const version = evidence.version;
  if (version !== DURABLE_EVIDENCE_VERSION && version !== DURABLE_EVIDENCE_LEGACY_VERSION) {
    return NO_DURABLE_EVIDENCE;
  }

  const operation = evidence.operation;
  if (
    operation !== undefined &&
    operation !== '' &&
    operation !== DURABLE_EVIDENCE_OPERATION
  ) {
    return NO_DURABLE_EVIDENCE;
  }

  const count = evidence.count;
  if (
    typeof count !== 'number' ||
    !Number.isInteger(count) ||
    count < 0 ||
    count > MAX_DURABLE_SOURCES
  ) {
    return NO_DURABLE_EVIDENCE;
  }

  if (!Array.isArray(evidence.sources) || evidence.sources.length !== count) {
    return NO_DURABLE_EVIDENCE;
  }

  const sources: ParsedWebSearchSource[] = [];
  const sourceIDs = new Set<string>();
  const v2 = version === DURABLE_EVIDENCE_VERSION;
  for (let i = 0; i < evidence.sources.length; i++) {
    const row = evidence.sources[i];
    if (!isRecord(row)) return NO_DURABLE_EVIDENCE;
    const { id, title, url, source, snippet } = row;
    if (
      typeof title !== 'string' ||
      typeof url !== 'string' ||
      typeof source !== 'string' ||
      typeof snippet !== 'string' ||
      title.length > MAX_DURABLE_TITLE ||
      url.length > MAX_DURABLE_URL ||
      source.length > MAX_DURABLE_SOURCE ||
      snippet.length > MAX_DURABLE_SNIPPET ||
      containsLineBreak(title) ||
      containsLineBreak(url) ||
      containsLineBreak(source) ||
      containsLineBreak(snippet)
    ) {
      return NO_DURABLE_EVIDENCE;
    }
    if (v2) {
      if (!isDurableID(id, MAX_DURABLE_SOURCE_ID) || sourceIDs.has(id)) {
        return NO_DURABLE_EVIDENCE;
      }
      sourceIDs.add(id);
    } else if (id !== undefined) {
      // Legacy evidence never carried ids; accepting one would invent
      // execution-scoped identity that was not persisted.
      return NO_DURABLE_EVIDENCE;
    }
    sources.push({
      index: i + 1,
      id: v2 ? (id as string) : undefined,
      title,
      url,
      source,
      snippet,
      // The same http(s)-only link rule used for the process-local envelope.
      linkable: isSafeExternalUrl(url),
    });
  }

  if (!v2) {
    if (evidence.plan !== undefined || evidence.steps !== undefined || evidence.operations !== undefined) {
      return NO_DURABLE_EVIDENCE;
    }
    return {
      present: true,
      version: DURABLE_EVIDENCE_LEGACY_VERSION,
      capability: DURABLE_EVIDENCE_CAPABILITY,
      operation: DURABLE_EVIDENCE_OPERATION,
      count,
      sources,
      steps: [],
      unattributedSources: sources,
    };
  }

  const rawPlan = evidence.plan;
  const rawSteps = evidence.steps;
  const rawOperations = evidence.operations;
  const hasSteps = rawSteps !== undefined && rawSteps !== null;
  const hasOperations = rawOperations !== undefined && rawOperations !== null;

  if (rawPlan === undefined || rawPlan === null) {
    // Source-only v2 evidence is valid, but it cannot smuggle in provenance
    // without the plan identity it belongs to.
    if (hasSteps || hasOperations) return NO_DURABLE_EVIDENCE;
    return {
      present: true,
      version: DURABLE_EVIDENCE_VERSION,
      capability: DURABLE_EVIDENCE_CAPABILITY,
      operation: DURABLE_EVIDENCE_OPERATION,
      count,
      sources,
      steps: [],
      unattributedSources: sources,
    };
  }

  if (!isRecord(rawPlan)) return NO_DURABLE_EVIDENCE;
  const planId = rawPlan.plan_id;
  const planVersion = rawPlan.version;
  if (
    !isDurableID(planId, MAX_DURABLE_PLAN_ID) ||
    typeof planVersion !== 'number' ||
    !Number.isInteger(planVersion) ||
    planVersion < 1 ||
    planVersion > MAX_DURABLE_PLAN_VERSION
  ) {
    return NO_DURABLE_EVIDENCE;
  }
  if (!Array.isArray(rawSteps) || rawSteps.length < 1 || rawSteps.length > MAX_DURABLE_PLAN_STEPS) {
    return NO_DURABLE_EVIDENCE;
  }

  const stepIndex = new Set<string>();
  const steps: ParsedDurableStep[] = [];
  for (const rawStep of rawSteps) {
    if (!isRecord(rawStep)) return NO_DURABLE_EVIDENCE;
    const stepId = rawStep.step_id;
    const status = rawStep.status;
    if (!isDurableID(stepId, MAX_DURABLE_STEP_ID) || stepIndex.has(stepId)) {
      return NO_DURABLE_EVIDENCE;
    }
    if (typeof status !== 'string' || !DURABLE_STEP_STATUSES.has(status as DurableResearchStepStatus)) {
      return NO_DURABLE_EVIDENCE;
    }
    const dependencies = parseDurableDependencies(rawStep.dependencies);
    if (dependencies === undefined) return NO_DURABLE_EVIDENCE;
    if (
      rawStep.observation_id !== undefined &&
      rawStep.observation_id !== null &&
      rawStep.observation_id !== '' &&
      !isDurableID(rawStep.observation_id, MAX_DURABLE_OBSERVATION_ID)
    ) {
      return NO_DURABLE_EVIDENCE;
    }
    stepIndex.add(stepId);
    steps.push({
      stepId,
      status: status as DurableResearchStepStatus,
      dependencies,
      observationId:
        typeof rawStep.observation_id === 'string' && rawStep.observation_id !== ''
          ? rawStep.observation_id
          : undefined,
      sources: [],
    });
  }
  for (const step of steps) {
    for (const dep of step.dependencies) {
      if (!stepIndex.has(dep)) return NO_DURABLE_EVIDENCE;
    }
  }
  if (durableStepsHaveCycle(steps)) return NO_DURABLE_EVIDENCE;

  const sourcesByID = new Map<string, ParsedWebSearchSource>();
  for (const source of sources) {
    if (source.id) sourcesByID.set(source.id, source);
  }
  const stepByID = new Map(steps.map((step) => [step.stepId, step]));
  const operations = rawOperations ?? [];
  if (!Array.isArray(operations) || operations.length > MAX_DURABLE_OPERATIONS) {
    return NO_DURABLE_EVIDENCE;
  }

  const operationIDs = new Set<string>();
  const referencedSources = new Set<string>();
  for (const rawOperation of operations) {
    if (!isRecord(rawOperation)) return NO_DURABLE_EVIDENCE;
    const operationId = rawOperation.operation_id;
    const stepId = rawOperation.step_id;
    const toolId = rawOperation.tool_id;
    const operationName = rawOperation.operation;
    const refs = rawOperation.source_ids;
    if (!isDurableID(operationId, MAX_DURABLE_OPERATION_ID) || operationIDs.has(operationId)) {
      return NO_DURABLE_EVIDENCE;
    }
    if (!isDurableID(stepId, MAX_DURABLE_STEP_ID) || !stepByID.has(stepId)) {
      return NO_DURABLE_EVIDENCE;
    }
    if (toolId !== DURABLE_EVIDENCE_CAPABILITY || operationName !== DURABLE_EVIDENCE_OPERATION) {
      return NO_DURABLE_EVIDENCE;
    }
    if (!Array.isArray(refs) || refs.length > MAX_DURABLE_OPERATION_SOURCE_REFS) {
      return NO_DURABLE_EVIDENCE;
    }
    operationIDs.add(operationId);
    const operationSourceIDs = new Set<string>();
    for (const sourceId of refs) {
      if (
        !isDurableID(sourceId, MAX_DURABLE_SOURCE_ID) ||
        !sourcesByID.has(sourceId) ||
        operationSourceIDs.has(sourceId) ||
        referencedSources.has(sourceId)
      ) {
        return NO_DURABLE_EVIDENCE;
      }
      operationSourceIDs.add(sourceId);
      referencedSources.add(sourceId);
      stepByID.get(stepId)?.sources.push(sourcesByID.get(sourceId) as ParsedWebSearchSource);
    }
  }

  return {
    present: true,
    version: DURABLE_EVIDENCE_VERSION,
    capability: DURABLE_EVIDENCE_CAPABILITY,
    operation: DURABLE_EVIDENCE_OPERATION,
    count,
    sources,
    plan: { plan_id: planId, version: planVersion },
    steps,
    unattributedSources: sources.filter(
      (source) => !source.id || !referencedSources.has(source.id)
    ),
  };
}

function parseDurableDependencies(value: unknown): string[] | undefined {
  if (value === undefined || value === null) return [];
  if (!Array.isArray(value) || value.length > MAX_DURABLE_STEP_DEPENDENCIES) return undefined;
  const deps: string[] = [];
  const seen = new Set<string>();
  for (const dep of value) {
    if (!isDurableID(dep, MAX_DURABLE_STEP_ID) || seen.has(dep)) return undefined;
    seen.add(dep);
    deps.push(dep);
  }
  return deps;
}

function durableStepsHaveCycle(steps: ParsedDurableStep[]): boolean {
  const deps = new Map(steps.map((step) => [step.stepId, step.dependencies]));
  const state = new Map<string, 'visiting' | 'done'>();
  const visit = (id: string): boolean => {
    const current = state.get(id);
    if (current === 'visiting') return true;
    if (current === 'done') return false;
    state.set(id, 'visiting');
    for (const dep of deps.get(id) ?? []) {
      if (visit(dep)) return true;
    }
    state.set(id, 'done');
    return false;
  };
  return steps.some((step) => visit(step.stepId));
}

function isDurableID(value: unknown, max: number): value is string {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    value.length <= max &&
    !value.includes('\u0000') &&
    !value.includes('\n') &&
    !value.includes('\r')
  );
}

function containsLineBreak(value: string): boolean {
  return value.includes('\u0000') || value.includes('\n') || value.includes('\r');
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

// ---- Multi-step research (v53) ----

/**
 * One step of a multi-step research plan. Status is reconstructed from the
 * execution trace (step.ready / step.started / step.completed / step.failed /
 * step.blocked). Intent text is redacted at capture (EXECUTION_TRACE_CONTRACTS
 * §9), so the trace carries step identity and status only.
 */
export interface ResearchStep {
  stepId: string;
  status: 'ready' | 'running' | 'completed' | 'failed' | 'blocked';
  /** Observation id produced by the step, when the trace records one. */
  observationId?: string;
  /** Action type the model proposed for the step, when observed. */
  actionType?: string;
}

/**
 * Reconstructed findings for one execution: the validated typed synthesis,
 * legacy agent prose, and the durable source evidence, kept distinct so the UI
 * never presents a conclusion as a citation.
 */
export interface ResearchFindings {
  /** Legacy agent-generated synthesis prose (never linkified as evidence). */
  synthesis?: string;
  /** Validated typed synthesis with citations resolved against durable evidence. */
  typedSynthesis?: ParsedResearchSynthesis;
  /**
   * Set when the backend supplied a typed synthesis that the UI could not
   * verify against this execution's durable evidence. The whole synthesis is
   * withheld; no citation or prose fallback is rendered.
   */
  synthesisRejected?: ResearchSynthesisRejection;
  /** Durable source records captured at the capability boundary. */
  sources: ParsedWebSearchSource[];
  /** True when the durable evidence snapshot is present (even if empty). */
  evidencePresent: boolean;
  /** True when any step failed/was blocked or the synthesis declares partial. */
  partial: boolean;
}

export type ResearchSynthesisStatus = 'complete' | 'partial' | 'inconclusive';
export type ResearchSynthesisRejection = 'invalid' | 'no_evidence';

export interface ParsedSynthesisCitation {
  sourceId: string;
  /** The authoritative durable record. Titles/URLs never come from the model. */
  source: ParsedWebSearchSource;
}

export interface ParsedSynthesisStatement {
  statement: string;
  citations: ParsedSynthesisCitation[];
}

export interface ParsedResearchSynthesis {
  status: ResearchSynthesisStatus;
  summary: string;
  evidenceBacked: ParsedSynthesisStatement[];
  inferences: string[];
  conflicts: ParsedSynthesisStatement[];
  unresolvedQuestions: string[];
}

export interface ResearchSynthesisParseResult {
  synthesis?: ParsedResearchSynthesis;
  rejection?: ResearchSynthesisRejection;
}

// Bounds mirror internal/executor/synthesis.go. A typed synthesis that exceeds
// any bound, repeats a citation, or cites a source absent from this execution's
// durable snapshot is rejected wholesale rather than partially rendered.
const MAX_SYNTHESIS_SUMMARY = 4000;
const MAX_SYNTHESIS_STATEMENTS = 12;
const MAX_SYNTHESIS_INFERENCES = 12;
const MAX_SYNTHESIS_CONFLICTS = 8;
const MAX_SYNTHESIS_QUESTIONS = 8;
const MAX_SYNTHESIS_STATEMENT = 2000;
const MAX_SYNTHESIS_QUESTION = 1000;
const MAX_SYNTHESIS_CITATIONS = 12;

const SYNTHESIS_STATUSES = new Set<ResearchSynthesisStatus>([
  'complete',
  'partial',
  'inconclusive',
]);

/**
 * Validate a backend synthesis against the durable evidence snapshot this UI
 * actually parsed. This is an independent display boundary, not a re-run of the
 * backend validator: every source id must resolve in THIS execution's evidence,
 * duplicates are rejected, and the model cannot supply titles or URLs.
 */
export function parseResearchSynthesis(
  synthesis: APIResearchSynthesis | null | undefined,
  durable: ParsedDurableEvidence
): ResearchSynthesisParseResult {
  if (synthesis === undefined || synthesis === null) return {};
  if (!isRecord(synthesis)) return { rejection: 'invalid' };
  if (!durable.present || durable.version !== DURABLE_EVIDENCE_VERSION) {
    return { rejection: 'no_evidence' };
  }

  const status = synthesis.status;
  if (typeof status !== 'string' || !SYNTHESIS_STATUSES.has(status as ResearchSynthesisStatus)) {
    return { rejection: 'invalid' };
  }
  const summary = synthesis.summary;
  if (
    typeof summary !== 'string' ||
    summary.trim() === '' ||
    summary.length > MAX_SYNTHESIS_SUMMARY
  ) {
    return { rejection: 'invalid' };
  }

  const rawEvidence = synthesis.evidence_backed;
  const rawInferences = synthesis.inferences;
  const rawConflicts = synthesis.conflicts;
  const rawQuestions = synthesis.unresolved_questions;
  if (
    !Array.isArray(rawEvidence) ||
    !Array.isArray(rawInferences) ||
    !Array.isArray(rawConflicts) ||
    !Array.isArray(rawQuestions) ||
    rawEvidence.length > MAX_SYNTHESIS_STATEMENTS ||
    rawInferences.length > MAX_SYNTHESIS_INFERENCES ||
    rawConflicts.length > MAX_SYNTHESIS_CONFLICTS ||
    rawQuestions.length > MAX_SYNTHESIS_QUESTIONS
  ) {
    return { rejection: 'invalid' };
  }

  const sourceByID = new Map<string, ParsedWebSearchSource>();
  for (const source of durable.sources) {
    if (source.id) sourceByID.set(source.id, source);
  }
  const seenStatements = new Set<string>();
  const parseStatements = (
    rows: unknown[]
  ): ParsedSynthesisStatement[] | undefined => {
    const statements: ParsedSynthesisStatement[] = [];
    for (const row of rows) {
      if (!isRecord(row)) return undefined;
      const statement = row.statement;
      const sourceIDs = row.source_ids;
      if (
        typeof statement !== 'string' ||
        statement.trim() === '' ||
        statement.length > MAX_SYNTHESIS_STATEMENT ||
        seenStatements.has(statement.trim())
      ) {
        return undefined;
      }
      if (
        !Array.isArray(sourceIDs) ||
        sourceIDs.length === 0 ||
        sourceIDs.length > MAX_SYNTHESIS_CITATIONS
      ) {
        return undefined;
      }
      seenStatements.add(statement.trim());
      const seenIDs = new Set<string>();
      const citations: ParsedSynthesisCitation[] = [];
      for (const sourceID of sourceIDs) {
        if (
          typeof sourceID !== 'string' ||
          sourceID.length === 0 ||
          sourceID.length > MAX_DURABLE_SOURCE_ID ||
          sourceID.includes('\u0000') ||
          sourceID.includes('\n') ||
          sourceID.includes('\r') ||
          seenIDs.has(sourceID) ||
          !sourceByID.has(sourceID)
        ) {
          return undefined;
        }
        seenIDs.add(sourceID);
        citations.push({
          sourceId: sourceID,
          source: sourceByID.get(sourceID) as ParsedWebSearchSource,
        });
      }
      statements.push({ statement: statement.trim(), citations });
    }
    return statements;
  };

  const evidenceBacked = parseStatements(rawEvidence);
  if (!evidenceBacked) return { rejection: 'invalid' };
  const conflicts = parseStatements(rawConflicts);
  if (!conflicts) return { rejection: 'invalid' };

  const inferences: string[] = [];
  for (const row of rawInferences) {
    if (!isRecord(row)) return { rejection: 'invalid' };
    const statement = row.statement;
    if (
      typeof statement !== 'string' ||
      statement.trim() === '' ||
      statement.length > MAX_SYNTHESIS_STATEMENT
    ) {
      return { rejection: 'invalid' };
    }
    inferences.push(statement.trim());
  }

  const unresolvedQuestions: string[] = [];
  for (const question of rawQuestions) {
    if (
      typeof question !== 'string' ||
      question.trim() === '' ||
      question.length > MAX_SYNTHESIS_QUESTION
    ) {
      return { rejection: 'invalid' };
    }
    unresolvedQuestions.push(question.trim());
  }

  const grounded = evidenceBacked.length + conflicts.length;
  if (status === 'inconclusive' && grounded !== 0) return { rejection: 'invalid' };
  if (status === 'partial' && grounded === 0) return { rejection: 'invalid' };
  if (status === 'complete' && evidenceBacked.length === 0) return { rejection: 'invalid' };
  if (durable.sources.length === 0 && status !== 'inconclusive') {
    return { rejection: 'invalid' };
  }

  return {
    synthesis: {
      status: status as ResearchSynthesisStatus,
      summary: summary.trim(),
      evidenceBacked,
      inferences,
      conflicts,
      unresolvedQuestions,
    },
  };
}

/** Step lifecycle statuses emitted by the control loop (metadata only). */
const STEP_EVENT_STATUS: Record<string, ResearchStep['status']> = {
  'step.ready': 'ready',
  'step.started': 'running',
  'step.completed': 'completed',
  'step.failed': 'failed',
  'step.blocked': 'blocked',
};

/** Action types that the model may propose for a step. */
const STEP_ACTION_TYPES = new Set([
  'tool_call',
  'delegate',
  'memory_write',
  'memory_read',
  'model_call',
  'complete',
  'fail',
  'replan',
  'continue',
]);

/**
 * Rebuild per-step states from the execution trace. A step's latest
 * step.* event wins; a step is considered running once started until a
 * terminal step event arrives. Steps with no events are not reported.
 */
export function parseResearchSteps(
  entries?: TraceEntry[] | null
): ResearchStep[] {
  const steps = new Map<string, ResearchStep>();
  if (!entries) return [];

  for (const entry of entries) {
    const status = STEP_EVENT_STATUS[entry.type];
    if (status) {
      const stepId = entry.data?.step;
      if (!stepId) continue;
      const current = steps.get(stepId) ?? { stepId, status: 'ready' };
      steps.set(stepId, {
        stepId,
        status,
        observationId:
          status === 'completed' ? entry.data?.observation : current.observationId,
      });
      continue;
    }

    if (entry.type === 'action.proposed') {
      const stepId = entry.data?.step;
      const actionType = entry.data?.action;
      if (stepId && actionType && STEP_ACTION_TYPES.has(actionType)) {
        const current = steps.get(stepId) ?? { stepId, status: 'ready' };
        steps.set(stepId, { ...current, actionType, status: 'running' });
      }
    }
  }

  // Preserve trace order: it is the runtime's lifecycle order (plan order for
  // planned steps, followed by the loop's finalize step). Sorting by id would
  // reorder steps the model happened to name non-lexicographically.
  return Array.from(steps.values());
}

/**
 * Build research findings from a parsed result plus durable evidence. The
 * synthesis (result text) and the sources are kept distinct: the UI renders
 * the synthesis as prose and the sources as separately-verifiable records.
 */
export function buildResearchFindings(
  parsed: ReturnType<typeof parseResearchResult>,
  durable: ParsedDurableEvidence,
  steps: ResearchStep[],
  durableSteps: ParsedDurableStep[] = [],
  typedSynthesis?: APIResearchSynthesis | null
): ResearchFindings {
  const stepPartial =
    durableSteps.length > 0
      ? durableSteps.some(
          (step) =>
            step.status === 'failed' || step.status === 'blocked' || step.status === 'cancelled'
        )
      : steps.some((s) => s.status === 'failed' || s.status === 'blocked');
  const hasTypedSynthesis = typedSynthesis !== undefined && typedSynthesis !== null;
  const parsedSynthesis = parseResearchSynthesis(typedSynthesis, durable);
  const typed = parsedSynthesis.synthesis;
  const legacySynthesis =
    parsed.kind === 'structured' || parsed.kind === 'summary'
      ? parsed.resultText
      : parsed.kind === 'text'
        ? parsed.raw
        : undefined;

  return {
    // A typed result is authoritative for the synthesis surface. If it is
    // rejected, do not fall back to model prose: that would silently convert an
    // unverifiable citation claim into a conclusion.
    synthesis: hasTypedSynthesis ? undefined : legacySynthesis,
    typedSynthesis: typed,
    synthesisRejected: hasTypedSynthesis ? parsedSynthesis.rejection : undefined,
    sources: durable.sources,
    evidencePresent: durable.present,
    partial: stepPartial || typed?.status === 'partial',
  };
}
