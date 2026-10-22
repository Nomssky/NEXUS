/**
 * Defensive parser for the deterministic research quality evaluation.
 *
 * The backend validates the same schema at the durable write and decode
 * boundaries. This client parser is deliberately independent and fails closed:
 * an unknown version, unknown dimension, inconsistent status, mismatched
 * execution id or mismatched evidence snapshot hides the entire panel instead
 * of rendering a partial verdict.
 */

import type {
  ResearchEvaluation,
  ResearchEvaluationDimension,
  ResearchEvaluationDimensionId,
  ResearchEvaluationDimensionStatus,
  ResearchEvaluationFinding,
  ResearchEvaluationStatus,
  ResearchSynthesisStatus,
} from './api';
import type { ParsedDurableEvidence } from './research';

const EVALUATION_VERSION = 1;
const EVALUATION_METHOD = 'deterministic_v1';
const MAX_FINDINGS = 256;
const MAX_LIMITATIONS = 16;
const MAX_TEXT = 512;
const MAX_CODE = 64;
const MAX_SOURCE_ID = 64;
const MAX_STEP_ID = 128;
const MAX_EVIDENCE_COUNT = 50;
const FINGERPRINT = /^[0-9a-f]{64}$/;
const CODE = /^[a-z0-9_]{1,64}$/;

const DIMENSION_IDS: readonly ResearchEvaluationDimensionId[] = [
  'evidence_coverage',
  'citation_integrity',
  'relevance',
  'source_quality',
  'source_diversity',
  'conflict_handling',
  'uncertainty_handling',
  'plan_completeness',
];
const DIMENSION_ID_SET = new Set<string>(DIMENSION_IDS);

const DIMENSION_STATUSES = new Set<ResearchEvaluationDimensionStatus>([
  'pass',
  'warn',
  'incomplete',
  'fail',
  'not_measurable',
  'not_applicable',
]);

const EVALUATION_STATUSES = new Set<ResearchEvaluationStatus>([
  'passed',
  'warnings',
  'incomplete',
  'failed',
]);

const SYNTHESIS_STATUSES = new Set<ResearchSynthesisStatus>([
  'complete',
  'partial',
  'inconclusive',
]);

/**
 * Parse a live or durable evaluation. `expectedExecutionID` is the execution
 * the UI is currently displaying; an evaluation from any other execution is
 * discarded. When durable evidence is present, its version/count must match the
 * binding recorded by the evaluator.
 */
export function parseResearchEvaluation(
  raw: unknown,
  expectedExecutionID: string | undefined,
  durableEvidence?: ParsedDurableEvidence
): ResearchEvaluation | undefined {
  if (!isRecord(raw)) return undefined;

  if (raw.version !== EVALUATION_VERSION || raw.method !== EVALUATION_METHOD) {
    return undefined;
  }
  if (
    !isBoundedID(raw.execution_id, 128) ||
    !expectedExecutionID ||
    raw.execution_id !== expectedExecutionID
  ) {
    return undefined;
  }
  if (
    !isNonNegativeInt(raw.evidence_version) ||
    raw.evidence_version > 2 ||
    !isNonNegativeInt(raw.evidence_count) ||
    raw.evidence_count > MAX_EVIDENCE_COUNT ||
    typeof raw.evidence_fingerprint !== 'string' ||
    !FINGERPRINT.test(raw.evidence_fingerprint)
  ) {
    return undefined;
  }
  if (raw.evidence_version === 0 && raw.evidence_count !== 0) return undefined;

  const synthesisStatus = raw.synthesis_status;
  if (synthesisStatus !== undefined) {
    if (typeof synthesisStatus !== 'string' || !SYNTHESIS_STATUSES.has(synthesisStatus as ResearchSynthesisStatus)) {
      return undefined;
    }
    if (typeof raw.synthesis_fingerprint !== 'string' || !FINGERPRINT.test(raw.synthesis_fingerprint)) {
      return undefined;
    }
    if (raw.evidence_version === 0) return undefined;
  } else if (raw.synthesis_fingerprint !== undefined) {
    return undefined;
  }

  if (typeof raw.status !== 'string' || !EVALUATION_STATUSES.has(raw.status as ResearchEvaluationStatus)) {
    return undefined;
  }

  const dimensions = parseDimensions(raw.dimensions);
  if (!dimensions) return undefined;
  const findings = parseFindings(raw.findings);
  if (!findings) return undefined;
  const limitations = parseLimitations(raw.limitations);
  if (!limitations) return undefined;

  if (durableEvidence?.present) {
    if (
      raw.evidence_version !== durableEvidence.version ||
      raw.evidence_count !== durableEvidence.count
    ) {
      return undefined;
    }
  }

  const hasError = findings.some((finding) => finding.severity === 'error');
  const hasWarning = findings.some((finding) => finding.severity === 'warning');
  const hasIncomplete = dimensions.some((dimension) => dimension.status === 'incomplete');
  const hasWarnDimension = dimensions.some((dimension) => dimension.status === 'warn');
  const hasFailDimension = dimensions.some((dimension) => dimension.status === 'fail');

  switch (raw.status as ResearchEvaluationStatus) {
    case 'passed':
      if (hasError || hasWarning || hasIncomplete || hasWarnDimension || hasFailDimension) {
        return undefined;
      }
      break;
    case 'warnings':
      if (
        hasError ||
        hasFailDimension ||
        hasIncomplete ||
        (!hasWarning && !hasWarnDimension)
      ) {
        return undefined;
      }
      break;
    case 'incomplete':
      if (hasError || hasFailDimension || !hasIncomplete) return undefined;
      break;
    case 'failed':
      if (!hasError && !hasFailDimension) return undefined;
      break;
  }

  return {
    version: 1,
    method: 'deterministic_v1',
    execution_id: raw.execution_id,
    evidence_version: raw.evidence_version,
    evidence_count: raw.evidence_count,
    evidence_fingerprint: raw.evidence_fingerprint,
    ...(synthesisStatus !== undefined
      ? {
          synthesis_status: synthesisStatus as ResearchSynthesisStatus,
          synthesis_fingerprint: raw.synthesis_fingerprint as string,
        }
      : {}),
    status: raw.status as ResearchEvaluationStatus,
    dimensions,
    findings,
    limitations,
  };
}

function parseDimensions(raw: unknown): ResearchEvaluationDimension[] | undefined {
  if (!Array.isArray(raw) || raw.length !== DIMENSION_IDS.length) return undefined;
  const seen = new Set<string>();
  const dimensions: ResearchEvaluationDimension[] = [];
  for (const item of raw) {
    if (!isRecord(item)) return undefined;
    if (typeof item.id !== 'string' || !DIMENSION_ID_SET.has(item.id) || seen.has(item.id)) {
      return undefined;
    }
    if (
      typeof item.status !== 'string' ||
      !DIMENSION_STATUSES.has(item.status as ResearchEvaluationDimensionStatus)
    ) {
      return undefined;
    }
    seen.add(item.id);
    dimensions.push({
      id: item.id as ResearchEvaluationDimensionId,
      status: item.status as ResearchEvaluationDimensionStatus,
    });
  }
  return dimensions.length === DIMENSION_IDS.length ? dimensions : undefined;
}

function parseFindings(raw: unknown): ResearchEvaluationFinding[] | undefined {
  if (!Array.isArray(raw) || raw.length > MAX_FINDINGS) return undefined;
  const findings: ResearchEvaluationFinding[] = [];
  for (const item of raw) {
    if (!isRecord(item)) return undefined;
    if (typeof item.code !== 'string' || !CODE.test(item.code) || item.code.length > MAX_CODE) {
      return undefined;
    }
    if (typeof item.dimension !== 'string' || !DIMENSION_ID_SET.has(item.dimension)) {
      return undefined;
    }
    if (item.severity !== 'warning' && item.severity !== 'error') return undefined;
    if (!isBoundedText(item.message)) return undefined;
    if (item.source_id !== undefined && !isBoundedID(item.source_id, MAX_SOURCE_ID)) {
      return undefined;
    }
    if (item.step_id !== undefined && !isBoundedID(item.step_id, MAX_STEP_ID)) {
      return undefined;
    }
    findings.push({
      code: item.code,
      dimension: item.dimension as ResearchEvaluationDimensionId,
      severity: item.severity,
      message: item.message,
      ...(typeof item.source_id === 'string' ? { source_id: item.source_id } : {}),
      ...(typeof item.step_id === 'string' ? { step_id: item.step_id } : {}),
    });
  }
  return findings;
}

function parseLimitations(raw: unknown): string[] | undefined {
  if (!Array.isArray(raw) || raw.length > MAX_LIMITATIONS) return undefined;
  const limitations: string[] = [];
  for (const item of raw) {
    if (!isBoundedText(item)) return undefined;
    limitations.push(item);
  }
  return limitations;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isNonNegativeInt(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0;
}

function isBoundedID(value: unknown, max: number): value is string {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    value.length <= max &&
    !hasForbiddenControl(value)
  );
}

function isBoundedText(value: unknown): value is string {
  return (
    typeof value === 'string' &&
    value.trim().length > 0 &&
    value.length <= MAX_TEXT &&
    !hasForbiddenControl(value)
  );
}

/** NUL, LF and CR are the control characters the journal rejects. */
function hasForbiddenControl(value: string): boolean {
  for (let i = 0; i < value.length; i += 1) {
    const code = value.charCodeAt(i);
    if (code === 0 || code === 10 || code === 13) return true;
  }
  return false;
}
