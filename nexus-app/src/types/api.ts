/**
 * NEXUS API Types
 * Based on the actual backend contracts and HTTP routes
 */

// Base types
export interface Envelope {
  code: string;
  category: string;
  message: string;
  details?: Record<string, unknown>;
  retryable: boolean;
  correlation_id: string;
  timestamp: string;
}

export type EnvelopeCategory =
  | 'VALIDATION'
  | 'AUTH'
  | 'AUTHORIZATION'
  | 'POLICY_DENIED'
  | 'APPROVAL_REQUIRED'
  | 'RESOURCE_UNAVAILABLE'
  | 'TIMEOUT'
  | 'DEPENDENCY_FAILURE'
  | 'RATE_LIMIT'
  | 'CONFLICT'
  | 'UNKNOWN_OUTCOME'
  | 'CANCELLATION'
  | 'SECURITY_REJECTION'
  | 'INTERNAL_FAILURE';

// Authentication
export interface AuthHeaders {
  'X-Actor-ID': string;
  'X-Actor-Credential': string;
}

export interface ControlHeaders {
  'X-API-Key': string;
}

// Execution / Intelligence
export interface SubmitIntelligenceRequest {
  business_id: string;
  actor_id: string;
  objective: {
    description: string;
  };
  /** Agent/execution context. Use agent_id to select a specific registered agent. */
  context?: Record<string, string>;
}

export interface SubmitIntelligenceResponse {
  execution_id: string;
}

export interface ExecutionStatus {
  execution_id: string;
  status: ExecutionStatusValue;
  error?: {
    code: string;
    category: string;
    message: string;
    details?: Record<string, unknown>;
  };
  output?: string;
  /**
   * Process-local typed synthesis for a research execution. Present only while
   * the process that ran the execution still holds the result; it is never part
   * of the durable recovery read.
   */
  research_synthesis?: ResearchSynthesis;
  /**
   * Deterministic quality verdict. Present on a live research execution; the
   * same persisted object is returned by the durable detail read after restart.
   * It contains no model prose or source text.
   */
  research_evaluation?: ResearchEvaluation;
  duration_ms?: number;
  /** Not exposed by the intelligence read; present only when supplied. */
  created_at?: string;
  completed_at?: string;
}

export type ResearchSynthesisStatus = 'complete' | 'partial' | 'inconclusive';

export interface ResearchSynthesisStatement {
  statement: string;
  source_ids: string[];
}

export interface ResearchSynthesisInference {
  statement: string;
}

/**
 * Backend-validated answer over one execution's evidence. The model supplies
 * only source ids as citations; titles and URLs never appear in this object.
 * The UI re-resolves every id against the authoritative durable evidence.
 */
export interface ResearchSynthesis {
  status: ResearchSynthesisStatus;
  summary: string;
  evidence_backed: ResearchSynthesisStatement[];
  inferences: ResearchSynthesisInference[];
  conflicts: ResearchSynthesisStatement[];
  unresolved_questions: string[];
}

/**
 * Durable, versioned representation of a validated typed synthesis, persisted
 * through the execution journal (v59). Mirrors backend
 * internal/execjournal.ResearchSynthesis. Like the live ResearchSynthesis it
 * carries only source ids as citations; titles and URLs never appear here and
 * are re-resolved against the durable evidence snapshot.
 *
 * The synthesis is bound to exactly one execution id and one evidence snapshot
 * (via evidence_fingerprint). The backend validates this binding at write and
 * decode time; the client re-validates structure defensively before rendering.
 */
export interface DurableResearchSynthesisStatement {
  statement: string;
  source_ids: string[];
}

export interface DurableResearchSynthesisInference {
  statement: string;
}

export interface DurableResearchSynthesis {
  version: 1;
  method: 'deterministic_v1';
  /** The execution this synthesis belongs to; never another. */
  execution_id: string;
  status: ResearchSynthesisStatus;
  /** Bounded model-authored overview, rendered as text only. */
  summary: string;
  /** Evidence-backed statements; every citation resolves in this execution's evidence. */
  evidence_backed: DurableResearchSynthesisStatement[];
  /** Model conclusions that are not themselves source claims. */
  inferences: DurableResearchSynthesisInference[];
  /** Disagreements the captured sources support. */
  conflicts: DurableResearchSynthesisStatement[];
  /** Gaps the execution could not settle. */
  unresolved_questions: string[];
  /** SHA-256 over the normalized evidence snapshot this synthesis was validated against. */
  evidence_fingerprint: string;
  /**
   * Plan revision this synthesis was validated against. Must equal the durable
   * evidence plan revision when one was captured. Zero means the revision was
   * not recorded (legacy v2 evidence only); v3 plan evidence requires it.
   * Absent on v59 records written before this field existed; readers treat it
   * as zero/unavailable rather than rejecting the record.
   */
  plan_version?: number;
}

export type ResearchEvaluationStatus = 'passed' | 'warnings' | 'incomplete' | 'failed';

export type ResearchEvaluationDimensionId =
  | 'evidence_coverage'
  | 'citation_integrity'
  | 'relevance'
  | 'source_quality'
  | 'source_diversity'
  | 'conflict_handling'
  | 'uncertainty_handling'
  | 'plan_completeness';

export type ResearchEvaluationDimensionStatus =
  | 'pass'
  | 'warn'
  | 'incomplete'
  | 'fail'
  | 'not_measurable'
  | 'not_applicable';

export interface ResearchEvaluationDimension {
  id: ResearchEvaluationDimensionId;
  status: ResearchEvaluationDimensionStatus;
}

export interface ResearchEvaluationFinding {
  code: string;
  dimension: ResearchEvaluationDimensionId;
  severity: 'warning' | 'error';
  message: string;
  source_id?: string;
  step_id?: string;
}

/**
 * Deterministic, versioned quality verdict over one execution's synthesis and
 * evidence. The backend evaluator never calls a model or fetches a page, and
 * this object never contains model prose, titles, URLs, snippets or queries.
 */
export interface ResearchEvaluation {
  version: 1;
  method: 'deterministic_v1';
  execution_id: string;
  evidence_version: number;
  evidence_count: number;
  evidence_fingerprint: string;
  /**
   * Plan revision of the evidence snapshot that was evaluated. Required for v3
   * plan evidence; absent/zero for legacy records or evidence without a plan.
   */
  plan_version?: number;
  synthesis_status?: ResearchSynthesisStatus;
  synthesis_fingerprint?: string;
  status: ResearchEvaluationStatus;
  dimensions: ResearchEvaluationDimension[];
  findings: ResearchEvaluationFinding[];
  limitations: string[];
}

export type ExecutionStatusValue =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'timed_out'
  | 'unknown';

export interface ExecutionTrace {
  execution_id: string;
  correlation_id: string;
  business_id: string;
  status: string;
  durability: 'process_local';
  capture: {
    available: boolean;
    entries: number;
    dropped: number;
    limit: number;
    truncated: boolean;
    first_seq: number;
    last_seq: number;
    terminal_seq?: number;
    post_terminal?: number;
    has_more?: boolean;
  };
  entries: TraceEntry[];
}

export interface TraceEntry {
  seq: number;
  event_id: string;
  type: string;
  source: string;
  timestamp: string;
  data?: Record<string, string>;
  redacted_keys?: string[];
  data_unparseable?: boolean;
}

export type TraceEventType =
  | 'action.proposed'
  | 'action.rejected'
  | 'governance.decided'
  | 'tool.invocation.started'
  | 'tool.invocation.completed'
  | 'tool.invocation.failed'
  | 'tool.invocation.unknown'
  | 'tool.invocation.rejected'
  | 'tool.attempt.started'
  | 'tool.attempt.completed'
  | 'tool.retry.scheduled'
  | 'tool.requested'
  | 'observation.created'
  | 'objective.started'
  | 'objective.completed'
  | 'chain.started'
  | 'chain.completed'
  | 'chain.failed'
  | 'approval.requested'
  | 'approval.approved'
  | 'approval.denied'
  | 'agent.delegated'
  // Agent intelligence control-loop step lifecycle (metadata only: step id,
  // plan version, status, observation ref — intent text is redacted at capture).
  | 'plan.created'
  | 'plan.validated'
  | 'plan.replanned'
  | 'step.ready'
  | 'step.started'
  | 'step.observing'
  | 'step.completed'
  | 'step.failed'
  | 'step.blocked';

// Alias for backward compatibility
export type ExecutionTraceEntry = TraceEntry;

// Businesses
export interface Business {
  entity_id: string;
  name: string;
  owner_identity_id: string;
  settings?: unknown;
  metadata?: Record<string, string>;
  status?: string;
  created_at?: string;
  updated_at?: string;
}

export interface CreateBusinessRequest {
  name: string;
  owner_identity_id: string;
  entity_id?: string;
  settings?: unknown;
  metadata?: Record<string, string>;
}

// Agents
export interface Agent {
  entity_id?: string;
  id?: string;
  name: string;
  business_id: string;
  capabilities: string[];
  allowed_tools: string[];
  status: 'active' | 'suspended' | 'archived';
  created_at: string;
  updated_at: string;
}

export interface CreateAgentRequest {
  entity_id: string;
  name: string;
  business_id: string;
  capabilities: string[];
  allowed_tools: string[];
}

// Approvals — mirrors the backend ApprovalRecord JSON contract.
export interface Approval {
  entity_id: string;
  status: 'PENDING' | 'APPROVED' | 'DENIED' | 'EXPIRED';
  requested_action: string;
  requester_id: string;
  approver_id?: string;
  approver_type: string;
  authority_context?: string;
  policy_ref?: string;
  scope?: string;
  created_at: string;
  expires_at?: string;
  decided_at?: string;
  decision_rationale?: string;
  self_approval_prohibited: boolean;
  delegation_allowed: boolean;
  audit_ref: string; // the execution/request ID this approval is for
  correlation_id?: string;
}

export interface ApproveRequest {
  reason: string;
}

/**
 * Approval context items for display. Returns only the fields that are
 * populated on the backend ApprovalRecord, in a stable order suitable for
 * rendering as a compact metadata line next to an approval item.
 *
 * Fields absent or empty on the backend are omitted rather than invented.
 */
export function approvalContextItems(approval: Approval): { label: string; value: string }[] {
  const items: { label: string; value: string }[] = [];
  if (approval.scope) items.push({ label: 'Scope', value: approval.scope });
  if (approval.authority_context) items.push({ label: 'Authority context', value: approval.authority_context });
  if (approval.policy_ref) items.push({ label: 'Policy ref', value: approval.policy_ref });
  return items;
}

/** Map a backend approval status to a Badge variant. */
export function approvalStatusVariant(status: string): 'pending_approval' | 'completed' | 'failed' | 'warning' | 'default' {
  switch (status) {
    case 'PENDING':
      return 'pending_approval';
    case 'APPROVED':
      return 'completed';
    case 'DENIED':
      return 'failed';
    case 'EXPIRED':
      return 'warning';
    default:
      return 'default';
  }
}

/** Human-readable label for an approval status. */
export function approvalStatusLabel(status: string): string {
  switch (status) {
    case 'PENDING':
      return 'Pending';
    case 'APPROVED':
      return 'Approved';
    case 'DENIED':
      return 'Denied';
    case 'EXPIRED':
      return 'Expired';
    default:
      return status;
  }
}

// Tools
export interface Tool {
  id: string;
  name: string;
  version: string;
  category: string;
  description: string;
  side_effect_class: 'read' | 'write' | 'mutation';
  operations: string[];
}

// Web Search (research capability)
export interface WebSearchInput {
  query: string;
  limit?: number;
}

export interface WebSearchSource {
  title: string;
  url: string;
  snippet: string;
  source: string;
  published_at?: string;
}

export interface WebSearchResult {
  sources: string;  // Concatenated list of "1. Title | URL | Source | Snippet"
  count: string;
}

export interface ParsedWebSearchSource {
  index: number;
  /**
   * Stable durable source identity when the record carries one (v2). Legacy
   * v1 evidence has no id and remains deliberately unattributed.
   */
  id?: string;
  title: string;
  url: string;
  source: string;
  snippet: string;
  /**
   * True only for absolute http(s) URLs with a host and no embedded
   * credentials. Non-linkable records are still rendered, just without an
   * anchor, so a malformed provider value can never become a clickable URI.
   */
  linkable: boolean;
}

/**
 * Validate a URL before it is exposed as a link. Only http(s) is accepted;
 * `javascript:`, `data:`, credentials-in-URL, and relative values are
 * rejected. This is presentation safety, not an SSRF boundary.
 */
export function isSafeExternalUrl(value: string): boolean {
  if (!value) return false;
  try {
    const url = new URL(value);
    return (
      (url.protocol === 'http:' || url.protocol === 'https:') &&
      url.hostname.length > 0 &&
      url.username === '' &&
      url.password === ''
    );
  } catch {
    return false;
  }
}

/**
 * Parse the documented web.search `sources` string:
 *   "1. Title | URL | Source | Snippet\n..."
 *
 * Provider data is untrusted and may be malformed. Missing fields fall back
 * deterministically, duplicates are dropped (first occurrence wins), and
 * invalid URLs remain visible as records but are not linkable.
 */
export function parseWebSearchResult(result: WebSearchResult): ParsedWebSearchSource[] {
  if (!result || !result.sources) return [];

  const parsed: ParsedWebSearchSource[] = [];
  const seen = new Set<string>();

  for (const rawLine of result.sources.split(/\r?\n/)) {
    if (!rawLine.trim()) continue;

    // Keep trailing whitespace until after the split so an empty final field
    // (a source with no snippet) still produces the documented four parts.
    const parts = rawLine.split(' | ');
    // A usable record needs at least a title and a URL field. Missing source
    // and snippet fields are allowed and rendered as empty.
    if (parts.length < 2) continue;

    const titlePart = parts[0].trim();
    const titleMatch = /^\d+\.\s*(.*)$/.exec(titlePart);
    const rawTitle = (titleMatch ? titleMatch[1] : titlePart).trim();
    const url = (parts[1] ?? '').trim();
    const source = (parts[2] ?? '').trim();
    // Snippets may themselves contain " | "; the backend does not escape it.
    const snippet = parts.slice(3).join(' | ').trim();

    const linkable = isSafeExternalUrl(url);
    let hostname = '';
    if (linkable) {
      try {
        hostname = new URL(url).hostname;
      } catch {
        // isSafeExternalUrl already validated it; keep the fallback safe.
      }
    }

    // Deduplicate by URL when present, otherwise by visible content. Keeping
    // the first occurrence makes the order stable across renders.
    const dedupeKey = url ? `url:${url}` : `text:${rawTitle}|${source}|${snippet}`;
    if (seen.has(dedupeKey)) continue;
    seen.add(dedupeKey);

    parsed.push({
      index: parsed.length + 1,
      title: rawTitle || source || hostname || 'Untitled source',
      url,
      source: source || hostname,
      snippet,
      linkable,
    });
  }

  return parsed;
}

// Git (read-only, sandboxed)
export type GitOperation = 'status' | 'diff' | 'log';

export interface GitInput {
  repo?: string;
  path?: string;
  limit?: number;
}

export interface GitResult {
  output: string;
  truncated: string;  // "true" or "false"
  repo: string;
}

export interface ParsedGitStatus {
  branch?: string;
  staged: Array<{ status: string; path: string }>;
  unstaged: Array<{ status: string; path: string }>;
  untracked: string[];
}

export function parseGitStatus(output: string): ParsedGitStatus {
  const lines = output.split('\n').filter(l => l.trim());
  const result: ParsedGitStatus = {
    staged: [],
    unstaged: [],
    untracked: [],
  };

  for (const line of lines) {
    if (line.startsWith('## ')) {
      // Branch info: "## branch-name...origin/branch-name [ahead X, behind Y]"
      result.branch = line.slice(3).trim();
      continue;
    }
    if (line.length < 3) continue;
    const status = line.slice(0, 2);
    const path = line.slice(3);
    if (status[0] !== ' ' && status[0] !== '?') {
      // Staged change
      result.staged.push({ status: status[0], path });
    }
    if (status[1] !== ' ' && status[1] !== '?') {
      // Unstaged change
      result.unstaged.push({ status: status[1], path });
    }
    if (status === '??') {
      // Untracked
      result.untracked.push(path);
    }
  }
  return result;
}

export interface ParsedGitLogEntry {
  hash: string;
  author: string;
  message: string;
}

export function parseGitLog(output: string): ParsedGitLogEntry[] {
  const lines = output.split('\n').filter(l => l.trim());
  return lines.map(line => {
    const match = line.match(/^([a-f0-9]+)\s+([^ ]+)\s+(.+)$/);
    if (match) {
      return { hash: match[1], author: match[2], message: match[3] };
    }
    return { hash: '', author: '', message: line };
  });
}

export interface ParsedGitDiffHunk {
  oldStart: number;
  oldLines: number;
  newStart: number;
  newLines: number;
  lines: Array<{ type: 'context' | 'add' | 'remove'; content: string }>;
}

export interface ParsedGitDiffFile {
  oldPath: string;
  newPath: string;
  hunks: ParsedGitDiffHunk[];
  isBinary: boolean;
  isNew: boolean;
  isDeleted: boolean;
}

export function parseGitDiff(output: string): ParsedGitDiffFile[] {
  const files: ParsedGitDiffFile[] = [];
  const lines = output.split('\n');
  let currentFile: ParsedGitDiffFile | null = null;
  let currentHunk: ParsedGitDiffHunk | null = null;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    
    // File header
    if (line.startsWith('diff --git ')) {
      if (currentFile) files.push(currentFile);
      const match = line.match(/diff --git a\/(.+) b\/(.+)/);
      currentFile = {
        oldPath: match ? match[1] : '',
        newPath: match ? match[2] : '',
        hunks: [],
        isBinary: false,
        isNew: false,
        isDeleted: false,
      };
      currentHunk = null;
      continue;
    }

    if (!currentFile) continue;

    // Binary file
    if (line.startsWith('Binary files ')) {
      currentFile.isBinary = true;
      continue;
    }

    // New file
    if (line.startsWith('new file mode ')) {
      currentFile.isNew = true;
      continue;
    }

    // Deleted file
    if (line.startsWith('deleted file mode ')) {
      currentFile.isDeleted = true;
      continue;
    }

    // Hunk header
    const hunkMatch = line.match(/^@@ -(\d+),?(\d*) \+(\d+),?(\d*) @@/);
    if (hunkMatch) {
      currentHunk = {
        oldStart: parseInt(hunkMatch[1], 10),
        oldLines: parseInt(hunkMatch[2] || '1', 10),
        newStart: parseInt(hunkMatch[3], 10),
        newLines: parseInt(hunkMatch[4] || '1', 10),
        lines: [],
      };
      currentFile.hunks.push(currentHunk);
      continue;
    }

    // Hunk content
    if (currentHunk) {
      if (line.startsWith('+')) {
        currentHunk.lines.push({ type: 'add', content: line.slice(1) });
      } else if (line.startsWith('-')) {
        currentHunk.lines.push({ type: 'remove', content: line.slice(1) });
      } else if (line.startsWith(' ')) {
        currentHunk.lines.push({ type: 'context', content: line.slice(1) });
      }
    }
  }

  if (currentFile) files.push(currentFile);
  return files;
}

// Memory
export interface MemoryRecord {
  id: string;
  kind: string;
  key: string;
  value: unknown;
  business_id: string;
  division_id?: string;
  created_at: string;
  updated_at: string;
}

export interface MemoryQueryRequest {
  business_id: string;
  key?: string;
  limit?: number;
  offset?: number;
}

export interface MemoryQueryResponse {
  records: MemoryRecord[];
  total: number;
  has_more: boolean;
}

// Capabilities
export interface CapabilityState {
  tool_id: string;
  state: 'enabled' | 'disabled' | 'deprecated';
  reason?: string;
}

// Events (SSE)
export interface SSEEvent {
  event: string;
  data: string;
  id?: string;
  retry?: number;
}

// Health / Status
export interface HealthResponse {
  status: 'healthy' | 'degraded' | 'unhealthy';
  version: string;
  uptime_seconds: number;
}

export interface StatusResponse {
  status: string;
  components: Record<string, { status: string; details?: string }>;
}

// Pagination
export interface PaginationParams {
  limit?: number;
  after_seq?: number;
  after_id?: string;
}

// ---- Durable recovery read (authoritative execution state) ----
// Mirrors the gateway's durableExecutionView projection.

export type DurableExecutionState =
  | 'accepted'
  | 'running'
  | 'pending_approval'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'timed_out'
  | 'unknown';

export interface DurableApprovalView {
  approval_id: string;
  outcome: string;
  decided_at: string;
}

export interface DurableRecoveryView {
  recovered?: boolean;
  prior_state?: string;
  recovered_at?: string;
}

export interface DurableExecution {
  source: string;
  execution_id: string;
  business_id: string;
  division_id?: string;
  state: DurableExecutionState;
  error_class?: string;
  created_at: string;
  updated_at: string;
  /**
   * The real terminal transition time. Omitted for records written before
   * v52 and for executions that never reached a terminal transition, so the
   * UI can never pass `updated_at` off as a completion time.
   */
  completed_at?: string;
  /**
   * Durable web.search source snapshot. Returned only by the single-execution
   * detail read; the list stays bounded and never repeats it.
   */
  research_evidence?: DurableResearchEvidence;
  /**
   * Persisted deterministic quality verdict, returned only by the detail read
   * alongside the evidence snapshot it evaluated.
   */
  research_evaluation?: ResearchEvaluation;
  /**
   * Persisted validated typed synthesis, returned only by the detail read
   * alongside the evidence and evaluation it describes. Present only for
   * executions written by v59 or later; legacy records omit it.
   */
  research_synthesis?: DurableResearchSynthesis;
  recovery?: DurableRecoveryView;
  approvals?: DurableApprovalView[];
}

// ---- Durable research evidence (v52, extended by v54) ----
// Mirrors internal/execjournal.ResearchEvidence: normalized provider records
// from one or more web.search calls, bounded, and validated wholesale at write
// time. Dates and authors are deliberately absent — the provider envelope has
// no grounded value for them, so none is exposed.
//
// v2 adds optional runtime-owned plan/step/operation provenance. The backend
// derives every association from execution-scoped identifiers known at tool
// mediation time; the frontend never infers attribution from URLs or order.

export interface DurableResearchSource {
  /** Stable execution-scoped source id. Absent only on legacy v1 records. */
  id?: string;
  title: string;
  url: string;
  source: string;
  snippet: string;
}

export interface DurableResearchPlan {
  plan_id: string;
  version: number;
  /** Runtime-owned objective reference, present on v3 records when available. */
  objective_id?: string;
  /** Step whose completion action settled a completed run, when recorded. */
  completion_step_id?: string;
  /** `authoritative` on v3 records; absent on legacy v2 records. */
  provenance?: 'authoritative';
}

export type DurableResearchStepStatus =
  | 'pending'
  | 'ready'
  | 'executing'
  | 'observing'
  | 'completed'
  | 'failed'
  | 'blocked'
  | 'cancelled';

export interface DurableResearchStep {
  step_id: string;
  /** Runtime-owned step declaration, recorded on v3 records. */
  intent?: string;
  /** Plan revision in which the step was declared, recorded on v3 records. */
  plan_version?: number;
  dependencies?: string[];
  status: DurableResearchStepStatus;
  observation_id?: string;
  /** `authoritative` on v3 records; absent on legacy v2 records. */
  provenance?: 'authoritative';
}

export interface DurableResearchOperation {
  operation_id: string;
  step_id: string;
  tool_id: string;
  operation: string;
  source_ids: string[];
  /** `authoritative` on v3 records; absent on legacy v2 records. */
  provenance?: 'authoritative';
}

export interface DurableResearchEvidence {
  version: number;
  capability: string;
  operation: string;
  /** The count declared by the provider envelope; equals sources.length. */
  count: number;
  sources: DurableResearchSource[];
  plan?: DurableResearchPlan;
  steps?: DurableResearchStep[];
  operations?: DurableResearchOperation[];
  /**
   * Evidence-level source-to-step grouping state. v3 requires `derived` when
   * operations exist and `unavailable` otherwise; absent on legacy records.
   */
  attribution?: 'derived' | 'unavailable';
}

export interface DurableExecutionList {
  source: string;
  executions: DurableExecution[];
  count: number;
  limit: number;
  offset: number;
  has_more: boolean;
}

// ---- Research provenance comparison (v61) ----
// Mirrors internal/researchcompare. A comparison is a read-only projection
// over two validated durable snapshots. It reports recorded differences only:
// no trust score, confidence, factual verification or "better run" verdict.

export type ResearchComparisonCategory =
  | 'plan'
  | 'steps'
  | 'operations'
  | 'evidence'
  | 'synthesis'
  | 'evaluation';

export type ResearchComparisonKind =
  | 'added'
  | 'removed'
  | 'changed'
  | 'unavailable'
  | 'incompatible'
  | 'legacy';

export type ResearchComparisonProvenance =
  | 'authoritative'
  | 'derived'
  | 'unavailable'
  | 'legacy';

export type ResearchComparisonCompatibilityStatus =
  | 'comparable'
  | 'partial'
  | 'incompatible';

export type ResearchComparisonCategoryStatus =
  | 'comparable'
  | 'partial'
  | 'unavailable'
  | 'incompatible';

export type ResearchComparisonSeverity = 'info' | 'warning' | 'error';

export interface ResearchComparisonProvenanceSummary {
  evidence: ResearchComparisonProvenance;
  plan: ResearchComparisonProvenance;
  steps: ResearchComparisonProvenance;
  operations: ResearchComparisonProvenance;
  source_attribution: ResearchComparisonProvenance;
  synthesis: ResearchComparisonProvenance;
  evaluation: ResearchComparisonProvenance;
}

export interface ResearchComparisonEvidenceSummary {
  version: number;
  capability: string;
  operation: string;
  count: number;
  fingerprint: string;
  attribution?: 'derived' | 'unavailable';
  source_count: number;
  step_count: number;
  operation_count: number;
}

export interface ResearchComparisonPlanSummary {
  plan_id: string;
  version: number;
  objective_id?: string;
  completion_step_id?: string;
  completion_recorded: boolean;
  provenance: 'authoritative' | 'legacy';
}

export interface ResearchComparisonSynthesisSummary {
  version: 1;
  method: 'deterministic_v1';
  status: ResearchSynthesisStatus;
  plan_version: number;
  plan_version_recorded: boolean;
  evidence_fingerprint: string;
  fingerprint: string;
  evidence_backed_count: number;
  inference_count: number;
  conflict_count: number;
  question_count: number;
}

export interface ResearchComparisonEvaluationSummary {
  version: 1;
  method: 'deterministic_v1';
  status: ResearchEvaluationStatus;
  evidence_version: number;
  evidence_count: number;
  evidence_fingerprint: string;
  plan_version: number;
  plan_version_recorded: boolean;
  synthesis_status?: ResearchSynthesisStatus;
  synthesis_fingerprint?: string;
  dimension_count: number;
  finding_count: number;
}

export interface ResearchComparisonSide {
  role: 'left' | 'right';
  execution_id: string;
  division_id?: string;
  state: DurableExecutionState;
  created_at: string;
  completed_at?: string;
  provenance: ResearchComparisonProvenanceSummary;
  evidence?: ResearchComparisonEvidenceSummary;
  plan?: ResearchComparisonPlanSummary;
  synthesis?: ResearchComparisonSynthesisSummary;
  evaluation?: ResearchComparisonEvaluationSummary;
}

export interface ResearchComparisonDifference {
  category: ResearchComparisonCategory;
  kind: ResearchComparisonKind;
  entity: string;
  id?: string;
  field?: string;
  left?: string;
  right?: string;
  left_fingerprint?: string;
  right_fingerprint?: string;
  provenance?: ResearchComparisonProvenance;
}

export interface ResearchComparisonFinding {
  code: string;
  severity: ResearchComparisonSeverity;
  category?: ResearchComparisonCategory;
  message: string;
}

export interface ResearchComparisonCategoryCompatibility {
  category: ResearchComparisonCategory;
  status: ResearchComparisonCategoryStatus;
}

export interface ResearchComparisonCompatibility {
  status: ResearchComparisonCompatibilityStatus;
  categories: ResearchComparisonCategoryCompatibility[];
}

export interface ResearchComparison {
  source: string;
  version: 1;
  method: 'deterministic_v1';
  left: ResearchComparisonSide;
  right: ResearchComparisonSide;
  compatibility: ResearchComparisonCompatibility;
  differences: ResearchComparisonDifference[];
  findings: ResearchComparisonFinding[];
  limitations: string[];
  truncated: boolean;
}

// ---- Research citation audit (v62) ----
// A deterministic, on-demand projection over one durable synthesis and its
// exact evidence snapshot. It reports structural citation resolution and
// evidence availability only. It is not a truth, quality or entailment verdict.

export type ResearchCitationAuditStatus =
  | 'passed'
  | 'warnings'
  | 'failed'
  | 'incomplete'
  | 'unavailable'
  | 'incompatible';

export type ResearchCitationAuditClaimCategory =
  | 'evidence_backed'
  | 'conflict'
  | 'inference'
  | 'unresolved_question';

export type ResearchCitationAuditCitationRequirement = 'required' | 'not_required';

export type ResearchCitationAuditClaimStatus =
  | 'passed'
  | 'warnings'
  | 'failed'
  | 'unavailable'
  | 'not_applicable';

export type ResearchCitationAuditCitationStatus =
  | 'resolved'
  | 'unresolved'
  | 'malformed'
  | 'duplicate'
  | 'ambiguous'
  | 'unavailable';

export type ResearchCitationAuditEvidenceStatus =
  | 'available'
  | 'missing'
  | 'ambiguous'
  | 'unavailable';

export type ResearchCitationAuditProvenance =
  | 'authoritative'
  | 'derived'
  | 'legacy'
  | 'unavailable'
  | 'not_applicable';

export type ResearchCitationAuditSeverity = 'warning' | 'error';

export interface ResearchCitationAuditCitation {
  index: number;
  /** The exact recorded reference. It is never trimmed, repaired or normalized. */
  reference: string;
  status: ResearchCitationAuditCitationStatus;
  evidence_status: ResearchCitationAuditEvidenceStatus;
  content_available: boolean;
  metadata_available: boolean;
  provenance: ResearchCitationAuditProvenance;
}

export interface ResearchCitationAuditClaim {
  claim_id: string;
  category: ResearchCitationAuditClaimCategory;
  index: number;
  text: string;
  citation_requirement: ResearchCitationAuditCitationRequirement;
  status: ResearchCitationAuditClaimStatus;
  provenance: ResearchCitationAuditProvenance;
  citations: ResearchCitationAuditCitation[];
}

export interface ResearchCitationAuditFinding {
  code: string;
  severity: ResearchCitationAuditSeverity;
  claim_id?: string;
  claim_index?: number;
  citation_index?: number;
  source_id?: string;
  message: string;
}

export interface ResearchCitationAudit {
  source: string;
  version: 1;
  method: 'deterministic_v1';
  execution_id: string;
  evidence_version: number;
  evidence_fingerprint: string;
  synthesis_version: number;
  synthesis_method: string;
  synthesis_fingerprint: string;
  synthesis_status: ResearchSynthesisStatus | '';
  plan_version: number;
  plan_version_recorded: boolean;
  evidence_plan_version: number;
  evidence_plan_recorded: boolean;
  provenance: 'derived';
  status: ResearchCitationAuditStatus;
  claims: ResearchCitationAuditClaim[];
  findings: ResearchCitationAuditFinding[];
  limitations: string[];
  truncated: boolean;
}

// Error response wrapper
export interface APIError extends Error {
  status: number;
  envelope?: Envelope;
}

// ============================================================================
// Divisions
// ============================================================================

export interface Division {
  entity_id: string;
  name: string;
  business_id: string;
  description?: string;
  status: 'active' | 'suspended' | 'archived';
  created_at: string;
  updated_at: string;
}

export interface CreateDivisionRequest {
  name: string;
  business_id: string;
  description?: string;
  entity_id?: string;
}

export interface DivisionMember {
  entity_id: string;
  division_id: string;
  role: string;
  created_at: string;
}

// ============================================================================
// Executions (agent execution layer v1)
// ============================================================================

export type ExecutionState =
  | 'accepted'
  | 'running'
  | 'pending_approval'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'timed_out'
  | 'unknown';

export interface Execution {
  execution_id: string;
  business_id: string;
  division_id?: string;
  state: ExecutionState;
  error_class?: string;
  created_at: string;
  updated_at: string;
  completed_at?: string;
  approvals?: DurableApprovalView[];
}

export interface SubmitExecutionRequest {
  business_id: string;
  actor_id: string;
  objective: {
    description: string;
  };
  division_id?: string;
  context?: Record<string, string>;
}

export interface SubmitExecutionResponse {
  execution_id: string;
}

// ============================================================================
// Escalations
// ============================================================================

export type EscalationStatus = 'open' | 'acknowledged' | 'resolved';

export interface Escalation {
  entity_id: string;
  title: string;
  description?: string;
  status: EscalationStatus;
  requested_by: string;
  requested_at: string;
  acknowledged_at?: string;
  acknowledged_by?: string;
  resolved_at?: string;
  resolved_by?: string;
  resolution_reason?: string;
  correlation_id?: string;
}

export interface AckEscalationRequest {
  reason: string;
}

export interface ResolveEscalationRequest {
  reason: string;
}

// ============================================================================
// Identities (detail)
// ============================================================================

export interface Identity {
  entity_id: string;
  identity_type: 'human' | 'agent';
  display_name: string;
  business_id: string;
  status: 'active' | 'suspended' | 'revoked';
  credential_method?: 'password' | 'none';
  created_at: string;
  updated_at: string;
}

// ============================================================================
// Business Members
// ============================================================================

export interface BusinessMember {
  entity_id: string;
  business_id: string;
  identity_id: string;
  role: string;
  created_at: string;
}

export interface CreateBusinessMemberRequest {
  identity_id: string;
  role: string;
}

// ============================================================================
// Agent lifecycle
// ============================================================================

export interface UpdateAgentRequest {
  name?: string;
  capabilities?: string[];
  allowed_tools?: string[];
}

export interface AgentTransitionRequest {
  reason: string;
}

// ============================================================================
// Control surface
// ============================================================================

export interface ControlStatus {
  paused: boolean;
  pause_reason?: string;
  paused_at?: string;
  paused_by?: string;
}

export interface ControlComponent {
  id: string;
  name: string;
  status: 'healthy' | 'degraded' | 'unhealthy';
  details?: string;
  last_check?: string;
}

export interface ControlMetrics {
  uptime_seconds: number;
  requests_total: number;
  requests_active: number;
  requests_failed: number;
  sse_connections: number;
  executions_total: number;
  executions_active: number;
}

// ============================================================================
// Requests (generic request/result)
// ============================================================================

export interface SubmitRequestRequest {
  business_id: string;
  actor_id: string;
  objective: {
    description: string;
  };
  context?: Record<string, string>;
}

export interface SubmitRequestResponse {
  request_id: string;
}

export interface RequestResult {
  request_id: string;
  business_id: string;
  status: string;
  output?: string;
  error?: {
    code: string;
    category: string;
    message: string;
    details?: Record<string, unknown>;
  };
  created_at: string;
  completed_at?: string;
}