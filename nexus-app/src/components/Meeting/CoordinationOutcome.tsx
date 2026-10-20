/**
 * CoordinationOutcome — aggregate result and real task relationships.
 *
 * Everything here is derived strictly from the trace the backend returns.
 * No participants are invented: a "participant" is only shown when the
 * trace contains a real delegation event that names a child agent/task.
 * The aggregate status is the authoritative execution status; constituent
 * child statuses come from `observation.created` result fields
 * (`child_status`, `child_agent_id`, `child_task_id`) and `agent.delegated`
 * linkage (`parent_agent_id`, `delegation_depth`, `parent_task_id`).
 */

import React, { useMemo } from 'react';
import { Card, Badge, getStatusVariant } from '../primitives';
import type { TraceEntry } from '../../types/api';
import styles from './CoordinationOutcome.module.css';

export interface ChildTask {
  parentAgentID: string;
  childAgentID: string;
  childTaskID: string;
  depth: string;
  childTools: string;
  status: string;
  seq: number;
}

interface CoordinationOutcomeProps {
  status?: string;
  entries?: TraceEntry[];
  executionId?: string;
}

/**
 * extractChildren reads delegation evidence from the trace. It returns one
 * record per delegation the runtime actually performed, using only fields
 * the trace exposes. An agent.delegated event carries the parent agent and
 * depth; the child's identity is only shown when the backend names it.
 */
export function extractChildren(entries: TraceEntry[] | undefined): ChildTask[] {
  if (!entries) return [];
  const records: ChildTask[] = [];
  let pending: ChildTask | null = null;

  for (const e of entries) {
    const d = e.data;
    if (!d) continue;

    if (e.type === 'agent.delegated') {
      pending = {
        parentAgentID: d.parent_agent_id || d.agent_id || '',
        childAgentID: d.child_agent_id || d.delegate_id || '',
        childTaskID: d.child_task_id || '',
        depth: d.depth || d.delegation_depth || '',
        childTools: d.child_tools || '',
        status: 'dispatched',
        seq: e.seq,
      };
      records.push(pending);
      continue;
    }

    if (e.type === 'observation.created' && (d.source === 'delegate' || d.child_status)) {
      const status = d.child_status || d.status || 'unknown';
      if (pending && !pending.childAgentID && d.child_agent_id) {
        pending.childAgentID = d.child_agent_id;
      }
      if (pending && d.child_task_id && !pending.childTaskID) {
        pending.childTaskID = d.child_task_id;
      }
      if (pending) {
        pending.status = status;
      } else {
        records.push({
          parentAgentID: d.parent_agent_id || '',
          childAgentID: d.child_agent_id || '',
          childTaskID: d.child_task_id || '',
          depth: d.delegation_depth || '',
          childTools: '',
          status,
          seq: e.seq,
        });
      }
    }
  }

  return records.sort((a, b) => a.seq - b.seq);
}

function aggregateVariant(status: string | undefined): ReturnType<typeof getStatusVariant> {
  return getStatusVariant(status || 'unknown');
}

export const CoordinationOutcome: React.FC<CoordinationOutcomeProps> = ({ status, entries, executionId }) => {
  const children = useMemo(() => extractChildren(entries), [entries]);

  const tally = useMemo(() => {
    const t: Record<string, number> = {};
    for (const c of children) {
      const s = (c.status || 'unknown').toLowerCase();
      t[s] = (t[s] || 0) + 1;
    }
    return t;
  }, [children]);

  return (
    <div className={styles.wrap}>
      <Card variant="default" padding="lg" className={styles.summaryCard}>
        <div className={styles.summaryHeader}>
          <h3 className={styles.summaryTitle}>Coordination Outcome</h3>
          <Badge variant={aggregateVariant(status)} size="md" dot>
            {status || 'unknown'}
          </Badge>
        </div>
        {executionId && (
          <div className={styles.execId}>
            <span className={styles.execIdLabel}>execution</span>
            <code>{executionId}</code>
          </div>
        )}
        <p className={styles.summaryNote}>
          The status above is the authoritative execution status returned by the
          backend. Child outcomes below come from the trace; absent child
          evidence is not treated as success.
        </p>
      </Card>

      <Card variant="default" padding="lg" className={styles.childrenCard}>
        <div className={styles.childrenHeader}>
          <h3 className={styles.childrenTitle}>Delegated Tasks</h3>
          <Badge variant={children.length > 0 ? 'info' : 'default'} size="sm">
            {children.length}
          </Badge>
        </div>

        {children.length === 0 ? (
          <div className={styles.noChildren}>
            <p>No delegated tasks in this execution.</p>
            <span>
              Delegation is decided by the runtime, not selected here. If the
              runtime did not delegate, the trace contains no child tasks.
            </span>
          </div>
        ) : (
          <>
            <div className={styles.tallyRow}>
              {Object.entries(tally).map(([k, v]) => (
                <span key={k} className={styles.tallyItem}>
                  <Badge variant={getStatusVariant(k)} size="sm">{k}</Badge>
                  <span className={styles.tallyCount}>{v}</span>
                </span>
              ))}
            </div>
            <ul className={styles.childList}>
              {children.map((c, i) => (
                <li key={`${c.seq}-${i}`} className={styles.childItem}>
                  <div className={styles.childMain}>
                    <span className={styles.childAgent}>
                      {c.childAgentID || (c.parentAgentID ? `child of ${c.parentAgentID}` : 'delegated task')}
                    </span>
                    {c.depth && <span className={styles.childDepth}>depth {c.depth}</span>}
                    {c.childTools && <span className={styles.childDepth}>{c.childTools} tools</span>}
                  </div>
                  {c.childTaskID && <code className={styles.childTask}>{c.childTaskID}</code>}
                  <Badge variant={getStatusVariant(c.status)} size="sm">
                    {c.status}
                  </Badge>
                </li>
              ))}
            </ul>
          </>
        )}
      </Card>
    </div>
  );
};
