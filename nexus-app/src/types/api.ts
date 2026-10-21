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
  duration_ms?: number;
  /** Not exposed by the intelligence read; present only when supplied. */
  created_at?: string;
  completed_at?: string;
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
  | 'agent.delegated';

// Alias for backward compatibility
export type ExecutionTraceEntry = TraceEntry;

// Agents
export interface Agent {
  entity_id: string;
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
  recovery?: DurableRecoveryView;
  approvals?: DurableApprovalView[];
}

export interface DurableExecutionList {
  source: string;
  executions: DurableExecution[];
  count: number;
  limit: number;
  offset: number;
  has_more: boolean;
}

// Error response wrapper
export interface APIError extends Error {
  status: number;
  envelope?: Envelope;
}