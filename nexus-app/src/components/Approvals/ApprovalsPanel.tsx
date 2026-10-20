/**
 * ApprovalsPanel - Shows pending approvals with clear actions.
 *
 * Displays the requested action and execution context from the backend
 * ApprovalRecord contract, the decision rationale when an approval has been
 * decided, and actionable error feedback. Renders pending, approved, denied,
 * and expired states using the established NEXUS visual language.
 */

import React from 'react';
import { Card, Button, Badge } from '../primitives';
import { useApprovals, useApproveApproval, useDenyApproval } from '../../api/hooks';
import {
  approvalStatusVariant,
  approvalStatusLabel,
  type Approval,
} from '../../types/api';
import styles from './ApprovalsPanel.module.css';

interface ApprovalsPanelProps {
  businessId: string;
}

export const ApprovalsPanel: React.FC<ApprovalsPanelProps> = ({ businessId }) => {
  const { data: approvalsData, isLoading, isError, error, refetch } = useApprovals(businessId);
  const approveMutation = useApproveApproval({
    onSuccess: () => refetch(),
  });
  const denyMutation = useDenyApproval({
    onSuccess: () => refetch(),
  });

  const approvals = approvalsData?.approvals || [];
  const pendingApprovals = approvals.filter((a) => a.status === 'PENDING');
  const decidedApprovals = approvals.filter((a) => a.status !== 'PENDING');

  if (isLoading) {
    return (
      <Card variant="default" padding="md">
        <div className={styles.loading}>
          <div className={styles.skeleton} style={{ width: '60%' }} />
          <div className={styles.skeleton} style={{ width: '40%' }} />
          <div className={styles.skeleton} style={{ width: '80%' }} />
        </div>
      </Card>
    );
  }

  if (isError) {
    return (
      <Card variant="default" padding="md">
        <div className={styles.error}>
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2" />
            <path d="M12 8v4M12 16v.01" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>Failed to load approvals</p>
          <span>{error instanceof Error ? error.message : 'Unknown error'}</span>
          <Button variant="ghost" size="sm" onClick={() => refetch()}>
            Retry
          </Button>
        </div>
      </Card>
    );
  }

  if (pendingApprovals.length === 0 && decidedApprovals.length === 0) {
    return (
      <Card variant="default" padding="md">
        <div className={styles.empty}>
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
            <path d="M24 16v16M16 24h16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>No pending approvals</p>
          <span>Approvals requiring your action will appear here</span>
        </div>
      </Card>
    );
  }

  return (
    <div className={styles.panel}>
      {pendingApprovals.length > 0 && (
        <Card variant="default" padding="none">
          <div className={styles.header}>
            <h3 className={styles.title}>Pending Approvals</h3>
            <Badge variant="pending_approval" size="sm" dot>
              {pendingApprovals.length}
            </Badge>
          </div>
          <div className={styles.list}>
            {pendingApprovals.map((approval) => (
              <PendingApprovalItem
                key={approval.entity_id}
                approval={approval}
                onApprove={(reason) =>
                  approveMutation.mutate({
                    id: approval.entity_id,
                    businessId,
                    request: { reason },
                  })
                }
                onDeny={(reason) =>
                  denyMutation.mutate({
                    id: approval.entity_id,
                    businessId,
                    request: { reason },
                  })
                }
                isApprovePending={
                  approveMutation.isPending &&
                  approveMutation.variables?.id === approval.entity_id
                }
                isDenyPending={
                  denyMutation.isPending &&
                  denyMutation.variables?.id === approval.entity_id
                }
                approveError={
                  approveMutation.variables?.id === approval.entity_id &&
                  approveMutation.error instanceof Error
                    ? approveMutation.error.message
                    : null
                }
                denyError={
                  denyMutation.variables?.id === approval.entity_id &&
                  denyMutation.error instanceof Error
                    ? denyMutation.error.message
                    : null
                }
              />
            ))}
          </div>
        </Card>
      )}

      {decidedApprovals.length > 0 && (
        <Card variant="default" padding="none">
          <div className={styles.header}>
            <h3 className={styles.title}>Recent Decisions</h3>
            <Badge variant="default" size="sm">
              {decidedApprovals.length}
            </Badge>
          </div>
          <div className={styles.list}>
            {decidedApprovals.map((approval) => (
              <DecidedApprovalItem key={approval.entity_id} approval={approval} />
            ))}
          </div>
        </Card>
      )}
    </div>
  );
};

// ---------- pending approval ----------

interface PendingApprovalItemProps {
  approval: Approval;
  onApprove: (reason: string) => void;
  onDeny: (reason: string) => void;
  isApprovePending: boolean;
  isDenyPending: boolean;
  approveError: string | null;
  denyError: string | null;
}

const PendingApprovalItem: React.FC<PendingApprovalItemProps> = ({
  approval,
  onApprove,
  onDeny,
  isApprovePending,
  isDenyPending,
  approveError,
  denyError,
}) => {
  const [showApprove, setShowApprove] = React.useState(false);
  const [showDeny, setShowDeny] = React.useState(false);
  const [approveReason, setApproveReason] = React.useState('');
  const [denyReason, setDenyReason] = React.useState('');

  const busy = isApprovePending || isDenyPending;
  const actionError = approveError ?? denyError;

  const handleApprove = () => {
    if (!approveReason.trim()) return;
    onApprove(approveReason.trim());
    setShowApprove(false);
    setApproveReason('');
  };

  const handleDeny = () => {
    if (!denyReason.trim()) return;
    onDeny(denyReason.trim());
    setShowDeny(false);
    setDenyReason('');
  };

  return (
    <div className={styles.item}>
      <div className={styles.itemMain}>
        <div className={styles.itemInfo}>
          <div className={styles.itemId}>{approval.audit_ref.slice(0, 12)}…</div>
          <div className={styles.itemAction}>
            <span className={styles.itemActionLabel}>Action</span>
            <span className={styles.itemActionValue}>{approval.requested_action || 'Unknown'}</span>
          </div>
          <div className={styles.itemMeta}>
            <time dateTime={approval.created_at}>
              {new Date(approval.created_at).toLocaleString()}
            </time>
            {approval.expires_at && (
              <>
                <span className={styles.separator} aria-hidden="true">·</span>
                <span>Expires {new Date(approval.expires_at).toLocaleString()}</span>
              </>
            )}
            <span className={styles.separator} aria-hidden="true">·</span>
            <span>Approver: {approval.approver_type || 'any'}</span>
          </div>
        </div>
        <Badge variant="pending_approval" size="md" dot>
          Pending
        </Badge>
      </div>

      {actionError && (
        <div className={styles.actionError} role="alert">
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
            <circle cx="7" cy="7" r="6" stroke="currentColor" strokeWidth="1.5" />
            <path d="M7 4v4M7 10v.01" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          <span>{actionError}</span>
        </div>
      )}

      {showApprove && (
        <div className={styles.reasonForm}>
          <label htmlFor={`approve-reason-${approval.entity_id}`} className={styles.reasonLabel}>
            Approval reason (required)
          </label>
          <textarea
            id={`approve-reason-${approval.entity_id}`}
            value={approveReason}
            onChange={(e) => setApproveReason(e.target.value)}
            placeholder="Enter reason for approval…"
            className={styles.reasonInput}
            rows={3}
            autoFocus
          />
          <div className={styles.reasonActions}>
            <Button variant="ghost" size="sm" onClick={() => setShowApprove(false)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={handleApprove}
              disabled={!approveReason.trim() || busy}
              isLoading={isApprovePending}
            >
              Approve
            </Button>
          </div>
        </div>
      )}

      {showDeny && (
        <div className={styles.reasonForm}>
          <label htmlFor={`deny-reason-${approval.entity_id}`} className={styles.reasonLabel}>
            Denial reason (required)
          </label>
          <textarea
            id={`deny-reason-${approval.entity_id}`}
            value={denyReason}
            onChange={(e) => setDenyReason(e.target.value)}
            placeholder="Enter reason for denial…"
            className={styles.reasonInput}
            rows={3}
            autoFocus
          />
          <div className={styles.reasonActions}>
            <Button variant="ghost" size="sm" onClick={() => setShowDeny(false)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={handleDeny}
              disabled={!denyReason.trim() || busy}
              isLoading={isDenyPending}
            >
              Deny
            </Button>
          </div>
        </div>
      )}

      {!showApprove && !showDeny && (
        <div className={styles.itemActions}>
          <Button
            variant="success"
            size="sm"
            onClick={() => setShowApprove(true)}
            disabled={busy}
          >
            Approve
          </Button>
          <Button
            variant="danger"
            size="sm"
            onClick={() => setShowDeny(true)}
            disabled={busy}
          >
            Deny
          </Button>
        </div>
      )}
    </div>
  );
};

// ---------- decided approval ----------

interface DecidedApprovalItemProps {
  approval: Approval;
}

const DecidedApprovalItem: React.FC<DecidedApprovalItemProps> = ({ approval }) => {
  const variant = approvalStatusVariant(approval.status);
  const label = approvalStatusLabel(approval.status);

  return (
    <div className={styles.item}>
      <div className={styles.itemMain}>
        <div className={styles.itemInfo}>
          <div className={styles.itemId}>{approval.audit_ref.slice(0, 12)}…</div>
          <div className={styles.itemAction}>
            <span className={styles.itemActionLabel}>Action</span>
            <span className={styles.itemActionValue}>{approval.requested_action || 'Unknown'}</span>
          </div>
          <div className={styles.itemMeta}>
            <time dateTime={approval.created_at}>
              {new Date(approval.created_at).toLocaleString()}
            </time>
            {approval.decided_at && (
              <>
                <span className={styles.separator} aria-hidden="true">·</span>
                <span>Decided {new Date(approval.decided_at).toLocaleString()}</span>
              </>
            )}
            {approval.approver_id && (
              <>
                <span className={styles.separator} aria-hidden="true">·</span>
                <span>By {approval.approver_id}</span>
              </>
            )}
          </div>
          {approval.decision_rationale && (
            <div className={styles.itemRationale}>
              <span className={styles.rationaleLabel}>Reason</span>
              <span>{approval.decision_rationale}</span>
            </div>
          )}
        </div>
        <Badge variant={variant} size="md" dot={approval.status === 'PENDING'}>
          {label}
        </Badge>
      </div>
    </div>
  );
};
