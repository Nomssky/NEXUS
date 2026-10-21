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
 */

import type { TraceEntry, WebSearchResult } from './api';

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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
