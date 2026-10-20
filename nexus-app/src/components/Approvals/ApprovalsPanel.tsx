/**
 * ApprovalsPanel - Shows pending approvals with clear actions
 */

import React from 'react';
import { Card, CardContent, Button, Badge } from '../primitives';
import { useApprovals, useApproveApproval, useDenyApproval } from '../../api/hooks';
import styles from './ApprovalsPanel.module.css';

interface ApprovalsPanelProps {
  businessId: string;
}

export const ApprovalsPanel: React.FC<ApprovalsPanelProps> = ({ businessId }) => {
  const { data: approvalsData, isLoading, isError, error, refetch } = useApprovals(businessId);
  const approveMutation = useApproveApproval({
    onSuccess: () => refetch(),
    onError: (err) => console.error('Approve failed:', err),
  });
  const denyMutation = useDenyApproval({
    onSuccess: () => refetch(),
    onError: (err) => console.error('Deny failed:', err),
  });

  const approvals = approvalsData?.approvals || [];
  const pendingApprovals = approvals.filter((a) => a.status === 'PENDING');

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
        </div>
      </Card>
    );
  }

  if (pendingApprovals.length === 0) {
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
    <Card variant="default" padding="none">
      <div className={styles.header}>
        <h3 className={styles.title}>Pending Approvals</h3>
        <Badge variant="pending_approval" size="sm" dot>
          {pendingApprovals.length}
        </Badge>
      </div>
      <div className={styles.list}>
        {pendingApprovals.map((approval) => (
          <ApprovalItem
            key={approval.entity_id}
            approval={approval}
            onApprove={(reason) =>
              approveMutation.mutate({ id: approval.entity_id, businessId, request: { reason } })
            }
            onDeny={(reason) =>
              denyMutation.mutate({ id: approval.entity_id, businessId, request: { reason } })
            }
            isApprovePending={approveMutation.isPending && approveMutation.variables?.id === approval.entity_id}
            isDenyPending={denyMutation.isPending && denyMutation.variables?.id === approval.entity_id}
          />
        ))}
      </div>
    </Card>
  );
};

interface ApprovalItemProps {
  approval: {
    entity_id: string;
    status: string;
    audit_ref: string;
    approver_type: string;
    approver_id?: string;
    created_at: string;
  };
  onApprove: (reason: string) => void;
  onDeny: (reason: string) => void;
  isApprovePending: boolean;
  isDenyPending: boolean;
}

const ApprovalItem: React.FC<ApprovalItemProps> = ({
  approval,
  onApprove,
  onDeny,
  isApprovePending,
  isDenyPending,
}) => {
  const [showApprove, setShowApprove] = React.useState(false);
  const [showDeny, setShowDeny] = React.useState(false);
  const [approveReason, setApproveReason] = React.useState('');
  const [denyReason, setDenyReason] = React.useState('');

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
          <div className={styles.itemMeta}>
            <time dateTime={approval.created_at}>
              {new Date(approval.created_at).toLocaleString()}
            </time>
            <span className={styles.separator} aria-hidden="true">·</span>
            <span>Approver: {approval.approver_type}</span>
          </div>
        </div>
        <Badge variant="pending_approval" size="md" dot>
          Pending
        </Badge>
      </div>

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
              disabled={!approveReason.trim() || isApprovePending}
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
              disabled={!denyReason.trim() || isDenyPending}
              isLoading={isDenyPending}
            >
              Deny
            </Button>
          </div>
        </div>
      )}

      {!showApprove && !showDeny && (
        <div className={styles.itemActions}>
          <Button variant="success" size="sm" onClick={() => setShowApprove(true)}>
            Approve
          </Button>
          <Button variant="danger" size="sm" onClick={() => setShowDeny(true)}>
            Deny
          </Button>
        </div>
      )}
    </div>
  );
};