/**
 * Defensive parser for the v61 research provenance comparison.
 *
 * The backend already validates the comparison at the projection boundary.
 * This client parser is independent and fails closed: an unknown schema,
 * malformed side summary, non-canonical category order, inconsistent
 * compatibility status or an over-bound collection hides the whole comparison
 * rather than rendering a partial or misleading result.
 */

import type {
  DurableExecutionState,
  ResearchComparison,
  ResearchComparisonCategory,
  ResearchComparisonCategoryCompatibility,
  ResearchComparisonCategoryStatus,
  ResearchComparisonCompatibility,
  ResearchComparisonCompatibilityStatus,
  ResearchComparisonDifference,
  ResearchComparisonEvaluationSummary,
  ResearchComparisonEvidenceSummary,
  ResearchComparisonFinding,
  ResearchComparisonKind,
  ResearchComparisonPlanSummary,
  ResearchComparisonProvenance,
  ResearchComparisonProvenanceSummary,
  ResearchComparisonSeverity,
  ResearchComparisonSide,
  ResearchComparisonSynthesisSummary,
  ResearchEvaluationStatus,
  ResearchSynthesisStatus,
} from './api';

const VERSION = 1;
const METHOD = 'deterministic_v1';
const MAX_DIFFERENCES = 512;
const MAX_FINDINGS = 64;
const MAX_LIMITATIONS = 16;
const MAX_TEXT = 512;
const MAX_ID = 128;
const MAX_EXECUTION_ID = 512;
const MAX_SOURCES = 50;
const MAX_STEPS = 64;
const MAX_OPERATIONS = 64;
const MAX_SYNTHESIS_ITEMS = 256;
const MAX_EVALUATION_FINDINGS = 256;
const MAX_PLAN_VERSION = 1_000_000;
const MAX_COMPARISON_BYTES = 2 * 1024 * 1024;
const FINGERPRINT = /^[0-9a-f]{64}$/;
const FINDING_CODE = /^[a-z][a-z0-9_]{0,63}$/;

const CATEGORY_ORDER: readonly ResearchComparisonCategory[] = [
  'plan',
  'steps',
  'operations',
  'evidence',
  'synthesis',
  'evaluation',
];
const CATEGORY_SET = new Set<string>(CATEGORY_ORDER);

const KIND_SET = new Set<ResearchComparisonKind>([
  'added',
  'removed',
  'changed',
  'unavailable',
  'incompatible',
  'legacy',
]);

const PROVENANCE_SET = new Set<ResearchComparisonProvenance>([
  'authoritative',
  'derived',
  'unavailable',
  'legacy',
]);

const CATEGORY_STATUS_SET = new Set<ResearchComparisonCategoryStatus>([
  'comparable',
  'partial',
  'unavailable',
  'incompatible',
]);

const COMPATIBILITY_STATUS_SET = new Set<ResearchComparisonCompatibilityStatus>([
  'comparable',
  'partial',
  'incompatible',
]);

const SEVERITY_SET = new Set<ResearchComparisonSeverity>(['info', 'warning', 'error']);

const EXECUTION_STATE_SET = new Set<DurableExecutionState>([
  'accepted',
  'running',
  'pending_approval',
  'completed',
  'failed',
  'cancelled',
  'timed_out',
  'unknown',
]);

const SYNTHESIS_STATUS_SET = new Set<ResearchSynthesisStatus>([
  'complete',
  'partial',
  'inconclusive',
]);

const EVALUATION_STATUS_SET = new Set<ResearchEvaluationStatus>([
  'passed',
  'warnings',
  'incomplete',
  'failed',
]);

/**
 * Parse a comparison response. Returns undefined for anything that cannot be
 * proven to match the bounded v1 schema. A comparison is never synthesized
 * from the selected executions on the client.
 */
export function parseResearchComparison(raw: unknown): ResearchComparison | undefined {
  if (!isRecord(raw)) return undefined;
  let encoded: string;
  try {
    encoded = JSON.stringify(raw);
  } catch {
    return undefined;
  }
  if (new TextEncoder().encode(encoded).length > MAX_COMPARISON_BYTES) return undefined;

  if (raw.version !== VERSION || raw.method !== METHOD) return undefined;
  if (!isBoundedID(raw.source, 64)) return undefined;

  const left = parseSide(raw.left, 'left');
  const right = parseSide(raw.right, 'right');
  if (!left || !right || left.execution_id === right.execution_id) return undefined;

  const compatibility = parseCompatibility(raw.compatibility);
  if (!compatibility) return undefined;

  const differences = parseDifferences(raw.differences);
  if (!differences) return undefined;
  const findings = parseFindings(raw.findings);
  if (!findings) return undefined;
  const limitations = parseLimitations(raw.limitations);
  if (!limitations) return undefined;
  if (typeof raw.truncated !== 'boolean') return undefined;

  if (raw.truncated !== findings.some((finding) => finding.code === 'comparison_output_truncated')) {
    return undefined;
  }

  return {
    source: raw.source,
    version: 1,
    method: 'deterministic_v1',
    left,
    right,
    compatibility,
    differences,
    findings,
    limitations,
    truncated: raw.truncated,
  };
}

function parseSide(raw: unknown, role: 'left' | 'right'): ResearchComparisonSide | undefined {
  if (!isRecord(raw) || raw.role !== role) return undefined;
  if (!isBoundedID(raw.execution_id, MAX_EXECUTION_ID)) return undefined;
  if (raw.division_id !== undefined && !isBoundedID(raw.division_id, MAX_ID)) return undefined;
  if (typeof raw.state !== 'string' || !EXECUTION_STATE_SET.has(raw.state as DurableExecutionState)) {
    return undefined;
  }
  if (!isTimestamp(raw.created_at)) return undefined;
  if (raw.completed_at !== undefined && !isTimestamp(raw.completed_at)) return undefined;

  const provenance = parseProvenanceSummary(raw.provenance);
  if (!provenance) return undefined;

  const evidence = raw.evidence === undefined ? undefined : parseEvidenceSummary(raw.evidence);
  if (raw.evidence !== undefined && !evidence) return undefined;

  const plan = raw.plan === undefined ? undefined : parsePlanSummary(raw.plan);
  if (raw.plan !== undefined && !plan) return undefined;
  if (plan && !evidence) return undefined;
  if (plan && evidence) {
    if (plan.plan_id !== raw.execution_id) return undefined;
    if (evidence.version === 3) {
      if (plan.provenance !== 'authoritative' || !plan.completion_recorded) return undefined;
    } else if (plan.provenance !== 'legacy' || plan.completion_recorded) {
      return undefined;
    }
  }
  if (evidence) {
    if (evidence.version >= 2 && !plan && (evidence.step_count > 0 || evidence.operation_count > 0)) {
      return undefined;
    }
    if (evidence.version >= 2 && plan && evidence.step_count === 0) return undefined;
    if (evidence.version === 3) {
      const expectedAttribution = evidence.operation_count > 0 ? 'derived' : 'unavailable';
      if (evidence.attribution !== expectedAttribution) return undefined;
    }
  }

  const synthesis = raw.synthesis === undefined ? undefined : parseSynthesisSummary(raw.synthesis);
  if (raw.synthesis !== undefined && !synthesis) return undefined;
  if (synthesis && !evidence) return undefined;
  if (synthesis && evidence) {
    if (synthesis.evidence_fingerprint !== evidence.fingerprint) return undefined;
    if (synthesis.plan_version !== 0) {
      if (!plan || synthesis.plan_version !== plan.version) return undefined;
    } else if (evidence.version === 3 && plan) {
      return undefined;
    }
  }

  const evaluation = raw.evaluation === undefined ? undefined : parseEvaluationSummary(raw.evaluation);
  if (raw.evaluation !== undefined && !evaluation) return undefined;
  if (evaluation) {
    if (evidence) {
      if (
        evaluation.evidence_version !== evidence.version ||
        evaluation.evidence_count !== evidence.count ||
        evaluation.evidence_fingerprint !== evidence.fingerprint
      ) {
        return undefined;
      }
    } else {
      if (evaluation.evidence_version !== 0 || evaluation.evidence_count !== 0) {
        return undefined;
      }
      if (evaluation.synthesis_status !== undefined) return undefined;
    }
    if (evaluation.plan_version !== 0) {
      if (!plan || evaluation.plan_version !== plan.version) return undefined;
    } else if (evidence && evidence.version === 3 && plan) {
      return undefined;
    }
    if (synthesis) {
      // The evaluation's synthesis fingerprint covers the pre-persistence
      // executor synthesis, not the durable synthesis record. Only the
      // recorded status is comparable; the fingerprint is metadata.
      if (
        evaluation.synthesis_status !== undefined &&
        evaluation.synthesis_status !== synthesis.status
      ) {
        return undefined;
      }
    }
  }

  return {
    role,
    execution_id: raw.execution_id,
    ...(typeof raw.division_id === 'string' ? { division_id: raw.division_id } : {}),
    state: raw.state as DurableExecutionState,
    created_at: raw.created_at,
    ...(typeof raw.completed_at === 'string' ? { completed_at: raw.completed_at } : {}),
    provenance,
    ...(evidence ? { evidence } : {}),
    ...(plan ? { plan } : {}),
    ...(synthesis ? { synthesis } : {}),
    ...(evaluation ? { evaluation } : {}),
  };
}

function parseProvenanceSummary(raw: unknown): ResearchComparisonProvenanceSummary | undefined {
  if (!isRecord(raw)) return undefined;
  const keys = [
    'evidence',
    'plan',
    'steps',
    'operations',
    'source_attribution',
    'synthesis',
    'evaluation',
  ] as const;
  const out: Partial<ResearchComparisonProvenanceSummary> = {};
  for (const key of keys) {
    if (typeof raw[key] !== 'string' || !PROVENANCE_SET.has(raw[key] as ResearchComparisonProvenance)) {
      return undefined;
    }
    out[key] = raw[key] as ResearchComparisonProvenance;
  }
  return out as ResearchComparisonProvenanceSummary;
}

function parseEvidenceSummary(raw: unknown): ResearchComparisonEvidenceSummary | undefined {
  if (!isRecord(raw)) return undefined;
  if (!isIntInRange(raw.version, 1, 3)) return undefined;
  if (!isBoundedID(raw.capability, MAX_ID) || !isBoundedID(raw.operation, MAX_ID)) {
    return undefined;
  }
  if (!isIntInRange(raw.count, 0, MAX_SOURCES)) return undefined;
  if (!isFingerprint(raw.fingerprint)) return undefined;
  if (!isIntInRange(raw.source_count, 0, MAX_SOURCES)) return undefined;
  if (raw.source_count !== raw.count) return undefined;
  if (!isIntInRange(raw.step_count, 0, MAX_STEPS)) return undefined;
  if (!isIntInRange(raw.operation_count, 0, MAX_OPERATIONS)) return undefined;

  let attribution: 'derived' | 'unavailable' | undefined;
  if (raw.attribution !== undefined) {
    if (raw.attribution !== 'derived' && raw.attribution !== 'unavailable') return undefined;
    attribution = raw.attribution;
  }
  if (raw.version === 3 && attribution === undefined) return undefined;
  if (raw.version < 3 && attribution !== undefined) return undefined;
  if (raw.version === 1 && (raw.step_count !== 0 || raw.operation_count !== 0)) return undefined;

  return {
    version: raw.version,
    capability: raw.capability,
    operation: raw.operation,
    count: raw.count,
    fingerprint: raw.fingerprint,
    ...(attribution ? { attribution } : {}),
    source_count: raw.source_count,
    step_count: raw.step_count,
    operation_count: raw.operation_count,
  };
}

function parsePlanSummary(raw: unknown): ResearchComparisonPlanSummary | undefined {
  if (!isRecord(raw)) return undefined;
  if (!isBoundedID(raw.plan_id, MAX_ID)) return undefined;
  if (!isIntInRange(raw.version, 1, MAX_PLAN_VERSION)) return undefined;
  if (raw.objective_id !== undefined && !isBoundedID(raw.objective_id, MAX_ID)) return undefined;
  if (raw.completion_step_id !== undefined && !isBoundedID(raw.completion_step_id, MAX_ID)) {
    return undefined;
  }
  if (typeof raw.completion_recorded !== 'boolean') return undefined;
  if (raw.provenance !== 'authoritative' && raw.provenance !== 'legacy') return undefined;
  if (raw.provenance === 'authoritative' && !raw.completion_recorded) return undefined;
  if (raw.provenance === 'legacy' && raw.completion_recorded) return undefined;

  return {
    plan_id: raw.plan_id,
    version: raw.version,
    ...(typeof raw.objective_id === 'string' ? { objective_id: raw.objective_id } : {}),
    ...(typeof raw.completion_step_id === 'string'
      ? { completion_step_id: raw.completion_step_id }
      : {}),
    completion_recorded: raw.completion_recorded,
    provenance: raw.provenance,
  };
}

function parseSynthesisSummary(raw: unknown): ResearchComparisonSynthesisSummary | undefined {
  if (!isRecord(raw)) return undefined;
  if (raw.version !== 1 || raw.method !== METHOD) return undefined;
  if (
    typeof raw.status !== 'string' ||
    !SYNTHESIS_STATUS_SET.has(raw.status as ResearchSynthesisStatus)
  ) {
    return undefined;
  }
  const planVersion = raw.plan_version === undefined ? 0 : raw.plan_version;
  if (!isIntInRange(planVersion, 0, MAX_PLAN_VERSION)) return undefined;
  if (typeof raw.plan_version_recorded !== 'boolean') return undefined;
  if (raw.plan_version_recorded !== (planVersion > 0)) return undefined;
  if (!isFingerprint(raw.evidence_fingerprint) || !isFingerprint(raw.fingerprint)) return undefined;
  if (!isIntInRange(raw.evidence_backed_count, 0, MAX_SYNTHESIS_ITEMS)) return undefined;
  if (!isIntInRange(raw.inference_count, 0, MAX_SYNTHESIS_ITEMS)) return undefined;
  if (!isIntInRange(raw.conflict_count, 0, MAX_SYNTHESIS_ITEMS)) return undefined;
  if (!isIntInRange(raw.question_count, 0, MAX_SYNTHESIS_ITEMS)) return undefined;

  return {
    version: 1,
    method: 'deterministic_v1',
    status: raw.status as ResearchSynthesisStatus,
    plan_version: planVersion,
    plan_version_recorded: raw.plan_version_recorded,
    evidence_fingerprint: raw.evidence_fingerprint,
    fingerprint: raw.fingerprint,
    evidence_backed_count: raw.evidence_backed_count,
    inference_count: raw.inference_count,
    conflict_count: raw.conflict_count,
    question_count: raw.question_count,
  };
}

function parseEvaluationSummary(raw: unknown): ResearchComparisonEvaluationSummary | undefined {
  if (!isRecord(raw)) return undefined;
  if (raw.version !== 1 || raw.method !== METHOD) return undefined;
  if (
    typeof raw.status !== 'string' ||
    !EVALUATION_STATUS_SET.has(raw.status as ResearchEvaluationStatus)
  ) {
    return undefined;
  }
  if (!isIntInRange(raw.evidence_version, 0, 3)) return undefined;
  if (!isIntInRange(raw.evidence_count, 0, MAX_SOURCES)) return undefined;
  if (raw.evidence_version === 0 && raw.evidence_count !== 0) return undefined;
  if (!isFingerprint(raw.evidence_fingerprint)) return undefined;
  const planVersion = raw.plan_version === undefined ? 0 : raw.plan_version;
  if (!isIntInRange(planVersion, 0, MAX_PLAN_VERSION)) return undefined;
  if (typeof raw.plan_version_recorded !== 'boolean') return undefined;
  if (raw.plan_version_recorded !== (planVersion > 0)) return undefined;
  if (raw.dimension_count !== 8) return undefined;
  if (!isIntInRange(raw.finding_count, 0, MAX_EVALUATION_FINDINGS)) return undefined;

  let synthesisStatus: ResearchSynthesisStatus | undefined;
  if (raw.synthesis_status !== undefined) {
    if (
      typeof raw.synthesis_status !== 'string' ||
      !SYNTHESIS_STATUS_SET.has(raw.synthesis_status as ResearchSynthesisStatus)
    ) {
      return undefined;
    }
    synthesisStatus = raw.synthesis_status as ResearchSynthesisStatus;
  }
  if (raw.synthesis_fingerprint !== undefined) {
    if (!isFingerprint(raw.synthesis_fingerprint)) return undefined;
    if (synthesisStatus === undefined) return undefined;
  } else if (synthesisStatus !== undefined) {
    return undefined;
  }

  return {
    version: 1,
    method: 'deterministic_v1',
    status: raw.status as ResearchEvaluationStatus,
    evidence_version: raw.evidence_version,
    evidence_count: raw.evidence_count,
    evidence_fingerprint: raw.evidence_fingerprint,
    plan_version: planVersion,
    plan_version_recorded: raw.plan_version_recorded,
    ...(synthesisStatus !== undefined
      ? {
          synthesis_status: synthesisStatus,
          synthesis_fingerprint: raw.synthesis_fingerprint as string,
        }
      : {}),
    dimension_count: raw.dimension_count,
    finding_count: raw.finding_count,
  };
}

function parseCompatibility(raw: unknown): ResearchComparisonCompatibility | undefined {
  if (!isRecord(raw)) return undefined;
  if (
    typeof raw.status !== 'string' ||
    !COMPATIBILITY_STATUS_SET.has(raw.status as ResearchComparisonCompatibilityStatus)
  ) {
    return undefined;
  }
  if (!Array.isArray(raw.categories) || raw.categories.length !== CATEGORY_ORDER.length) {
    return undefined;
  }
  const categories: ResearchComparisonCategoryCompatibility[] = [];
  for (let i = 0; i < raw.categories.length; i += 1) {
    const item = raw.categories[i];
    if (!isRecord(item)) return undefined;
    if (item.category !== CATEGORY_ORDER[i]) return undefined;
    if (
      typeof item.status !== 'string' ||
      !CATEGORY_STATUS_SET.has(item.status as ResearchComparisonCategoryStatus)
    ) {
      return undefined;
    }
    categories.push({
      category: CATEGORY_ORDER[i],
      status: item.status as ResearchComparisonCategoryStatus,
    });
  }
  if (overallCompatibility(categories) !== raw.status) return undefined;
  return {
    status: raw.status as ResearchComparisonCompatibilityStatus,
    categories,
  };
}

function parseDifferences(raw: unknown): ResearchComparisonDifference[] | undefined {
  if (!Array.isArray(raw) || raw.length > MAX_DIFFERENCES) return undefined;
  const out: ResearchComparisonDifference[] = [];
  for (const item of raw) {
    if (!isRecord(item)) return undefined;
    if (
      typeof item.category !== 'string' ||
      !CATEGORY_SET.has(item.category as ResearchComparisonCategory)
    ) {
      return undefined;
    }
    if (typeof item.kind !== 'string' || !KIND_SET.has(item.kind as ResearchComparisonKind)) {
      return undefined;
    }
    if (!isBoundedID(item.entity, MAX_ID)) return undefined;
    if (item.id !== undefined && !isBoundedID(item.id, MAX_ID)) return undefined;
    if (item.field !== undefined && !isBoundedID(item.field, MAX_ID)) return undefined;
    if (!isOptionalDisplayText(item.left) || !isOptionalDisplayText(item.right)) {
      return undefined;
    }
    if (item.left_fingerprint !== undefined && !isFingerprint(item.left_fingerprint)) {
      return undefined;
    }
    if (item.right_fingerprint !== undefined && !isFingerprint(item.right_fingerprint)) {
      return undefined;
    }
    if (
      item.provenance !== undefined &&
      (typeof item.provenance !== 'string' ||
        !PROVENANCE_SET.has(item.provenance as ResearchComparisonProvenance))
    ) {
      return undefined;
    }
    out.push({
      category: item.category as ResearchComparisonCategory,
      kind: item.kind as ResearchComparisonKind,
      entity: item.entity,
      ...(typeof item.id === 'string' ? { id: item.id } : {}),
      ...(typeof item.field === 'string' ? { field: item.field } : {}),
      ...(typeof item.left === 'string' ? { left: item.left } : {}),
      ...(typeof item.right === 'string' ? { right: item.right } : {}),
      ...(typeof item.left_fingerprint === 'string'
        ? { left_fingerprint: item.left_fingerprint }
        : {}),
      ...(typeof item.right_fingerprint === 'string'
        ? { right_fingerprint: item.right_fingerprint }
        : {}),
      ...(typeof item.provenance === 'string'
        ? { provenance: item.provenance as ResearchComparisonProvenance }
        : {}),
    });
  }
  if (!isSortedDifferences(out)) return undefined;
  return out;
}

function parseFindings(raw: unknown): ResearchComparisonFinding[] | undefined {
  if (!Array.isArray(raw) || raw.length > MAX_FINDINGS) return undefined;
  const out: ResearchComparisonFinding[] = [];
  for (const item of raw) {
    if (!isRecord(item)) return undefined;
    if (typeof item.code !== 'string' || !FINDING_CODE.test(item.code)) return undefined;
    if (typeof item.severity !== 'string' || !SEVERITY_SET.has(item.severity as ResearchComparisonSeverity)) {
      return undefined;
    }
    if (
      item.category !== undefined &&
      (typeof item.category !== 'string' ||
        !CATEGORY_SET.has(item.category as ResearchComparisonCategory))
    ) {
      return undefined;
    }
    if (!isBoundedText(item.message)) return undefined;
    out.push({
      code: item.code,
      severity: item.severity as ResearchComparisonSeverity,
      ...(typeof item.category === 'string'
        ? { category: item.category as ResearchComparisonCategory }
        : {}),
      message: item.message,
    });
  }
  return out;
}

function parseLimitations(raw: unknown): string[] | undefined {
  if (!Array.isArray(raw) || raw.length > MAX_LIMITATIONS) return undefined;
  const out: string[] = [];
  for (const item of raw) {
    if (!isBoundedText(item)) return undefined;
    out.push(item);
  }
  return out;
}

function overallCompatibility(
  categories: ResearchComparisonCategoryCompatibility[]
): ResearchComparisonCompatibilityStatus {
  let overall: ResearchComparisonCompatibilityStatus = 'comparable';
  let anyComparable = false;
  let allUnavailableOrIncompatible = true;
  for (const category of categories) {
    if (category.status === 'comparable') {
      anyComparable = true;
      allUnavailableOrIncompatible = false;
    }
    if (category.status !== 'unavailable' && category.status !== 'incompatible') {
      allUnavailableOrIncompatible = false;
    }
    if (category.status !== 'comparable') {
      overall = 'partial';
    }
  }
  if (!anyComparable && allUnavailableOrIncompatible) return 'incompatible';
  return overall;
}

function isSortedDifferences(differences: ResearchComparisonDifference[]): boolean {
  for (let i = 1; i < differences.length; i += 1) {
    if (compareDifferences(differences[i], differences[i - 1]) < 0) return false;
  }
  return true;
}

function compareDifferences(
  a: ResearchComparisonDifference,
  b: ResearchComparisonDifference
): number {
  const category = categoryRank(a.category) - categoryRank(b.category);
  if (category !== 0) return category;
  return (
    compareStrings(a.entity, b.entity) ||
    compareStrings(a.id ?? '', b.id ?? '') ||
    compareStrings(a.field ?? '', b.field ?? '') ||
    compareStrings(a.kind, b.kind) ||
    compareStrings(a.left ?? '', b.left ?? '') ||
    compareStrings(a.right ?? '', b.right ?? '') ||
    compareStrings(a.left_fingerprint ?? '', b.left_fingerprint ?? '') ||
    compareStrings(a.right_fingerprint ?? '', b.right_fingerprint ?? '')
  );
}

function categoryRank(category: ResearchComparisonCategory): number {
  return CATEGORY_ORDER.indexOf(category);
}

function compareStrings(a: string, b: string): number {
  if (a < b) return -1;
  if (a > b) return 1;
  return 0;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isIntInRange(value: unknown, min: number, max: number): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= min && value <= max;
}

function isBoundedID(value: unknown, max: number): value is string {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    value.length <= max &&
    !hasForbiddenControl(value)
  );
}

function isOptionalDisplayText(value: unknown): value is string | undefined {
  return (
    value === undefined ||
    (typeof value === 'string' && value.length <= MAX_TEXT && !hasForbiddenControl(value))
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

function isFingerprint(value: unknown): value is string {
  return typeof value === 'string' && FINGERPRINT.test(value);
}

function isTimestamp(value: unknown): value is string {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    value.length <= 64 &&
    !hasForbiddenControl(value) &&
    !Number.isNaN(Date.parse(value))
  );
}

/** NUL, LF, CR and tab are the control characters the comparison rejects. */
function hasForbiddenControl(value: string): boolean {
  for (let i = 0; i < value.length; i += 1) {
    const code = value.charCodeAt(i);
    if (code === 0 || code === 9 || code === 10 || code === 13) return true;
  }
  return false;
}
