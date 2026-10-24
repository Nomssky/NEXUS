/**
 * Deterministic research quality panel.
 *
 * The backend evaluator emits no score, percentage or trust claim. This panel
 * keeps the same posture: it shows dimension states, stable findings and the
 * evaluator's own limitations. It never upgrades an incomplete/failed verdict
 * into a green summary.
 */

import React from 'react';
import { Badge } from '../primitives';
import type { BadgeVariant } from '../primitives';
import type {
  ResearchEvaluation,
  ResearchEvaluationDimensionId,
  ResearchEvaluationDimensionStatus,
  ResearchEvaluationStatus,
} from '../../types/api';
import styles from './ResearchQuality.module.css';

interface ResearchQualityProps {
  evaluation: ResearchEvaluation;
  /**
   * True when the verdict was recorded at completion and is now being read back
   * from the durable record (e.g. after a restart). The panel then says so
   * instead of implying the verdict is live state.
   */
  recorded?: boolean;
  /**
   * True when the persisted synthesis this verdict describes is also available
   * and rendered. The panel then notes that the synthesis shown alongside it is
   * the same persisted snapshot, not a regenerated answer.
   */
  synthesisAvailable?: boolean;
}

const DIMENSION_LABELS: Record<ResearchEvaluationDimensionId, string> = {
  evidence_coverage: 'Evidence coverage',
  citation_integrity: 'Citation integrity',
  relevance: 'Relevance',
  source_quality: 'Source quality',
  source_diversity: 'Source diversity',
  conflict_handling: 'Conflict handling',
  uncertainty_handling: 'Uncertainty handling',
  plan_completeness: 'Plan completeness',
};

const DIMENSION_DETAILS: Record<ResearchEvaluationDimensionId, string> = {
  evidence_coverage: 'Cited structure only; semantic claim support is not verified.',
  citation_integrity: 'Citation shape, uniqueness and execution-scoped source resolution.',
  relevance: 'Meaningful word overlap with the objective or retrieving step; not semantics.',
  source_quality: 'Absolute http(s) URL shape and usable title/excerpt. No page is fetched.',
  source_diversity: 'Canonical URL/content duplication and host count; not independence.',
  conflict_handling: 'Only conflicts the synthesis explicitly declares can be checked.',
  uncertainty_handling: 'Partial/inconclusive status and unresolved-question disclosure.',
  plan_completeness: 'Final durable step statuses, including failed and nonterminal steps.',
};

const STATUS_LABELS: Record<ResearchEvaluationStatus, string> = {
  passed: 'Checks passed',
  warnings: 'Warnings',
  incomplete: 'Incomplete',
  failed: 'Integrity failure',
};

const DIMENSION_STATUS_LABELS: Record<ResearchEvaluationDimensionStatus, string> = {
  pass: 'Passed',
  warn: 'Warning',
  incomplete: 'Unavailable',
  fail: 'Failed',
  not_measurable: 'Not measured',
  not_applicable: 'Not applicable',
};

export const ResearchQuality: React.FC<ResearchQualityProps> = ({
  evaluation,
  recorded = false,
  synthesisAvailable = false,
}) => {
  const failed = evaluation.status === 'failed';
  const synthesisStatus = evaluation.synthesis_status;
  const showSynthesisNote = synthesisStatus !== undefined && recorded;

  return (
    <section
      className={`${styles.quality} ${failed ? styles.qualityFailed : ''}`}
      aria-label="Research quality evaluation"
      data-evaluation-status={evaluation.status}
      data-evaluation-version={evaluation.version}
    >
      <div className={styles.header}>
        <div className={styles.heading}>
          <h4 className={styles.title}>Quality checks</h4>
          <p className={styles.provenance}>
            Deterministic checks over this execution&apos;s evidence and cited synthesis.
            No model judgment, trust score or page fetch is involved.
            {evaluation.plan_version
              ? ` Bound to plan revision ${evaluation.plan_version}.`
              : ''}
          </p>
        </div>
        <Badge variant={evaluationStatusVariant(evaluation.status)} size="sm">
          {STATUS_LABELS[evaluation.status]}
        </Badge>
      </div>

      {failed && (
        <p className={styles.integrityNotice} role="alert">
          Deterministic integrity checks failed. The synthesis must not be read as
          a verified conclusion; the findings below identify what could not be
          established.
        </p>
      )}

      {showSynthesisNote && (
        <p className={styles.recordedNote}>
          {synthesisAvailable
            ? 'Recorded when this execution completed. The synthesis shown alongside is the same persisted snapshot, read back from the durable execution record — it is not regenerated.'
            : 'Recorded when this execution completed. The typed synthesis is not available for this record, so only this deterministic verdict is shown.'}
        </p>
      )}

      <ul className={styles.dimensions} aria-label="Quality dimensions">
        {evaluation.dimensions.map((dimension) => (
          <li
            key={dimension.id}
            className={styles.dimension}
            data-dimension={dimension.id}
            data-dimension-status={dimension.status}
          >
            <div className={styles.dimensionText}>
              <span className={styles.dimensionLabel}>{DIMENSION_LABELS[dimension.id]}</span>
              <span className={styles.dimensionDetail}>{DIMENSION_DETAILS[dimension.id]}</span>
            </div>
            <Badge variant={dimensionStatusVariant(dimension.status)} size="sm">
              {DIMENSION_STATUS_LABELS[dimension.status]}
            </Badge>
          </li>
        ))}
      </ul>

      {evaluation.findings.length > 0 ? (
        <div className={styles.findings}>
          <h5 className={styles.findingsTitle}>Findings</h5>
          <ul className={styles.findingList}>
            {evaluation.findings.map((finding, index) => (
              <li
                key={`${finding.code}-${finding.source_id || ''}-${finding.step_id || ''}-${index}`}
                className={styles.finding}
                data-finding-code={finding.code}
                data-finding-severity={finding.severity}
              >
                <Badge variant={finding.severity === 'error' ? 'error' : 'warning'} size="sm">
                  {finding.severity === 'error' ? 'Error' : 'Warning'}
                </Badge>
                <div className={styles.findingText}>
                  <span className={styles.findingMessage}>{finding.message}</span>
                  <span className={styles.findingMeta}>
                    {DIMENSION_LABELS[finding.dimension]}
                    {finding.source_id ? ` · ${finding.source_id}` : ''}
                    {finding.step_id ? ` · step ${finding.step_id}` : ''}
                  </span>
                </div>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <p className={styles.noFindings}>
          No deterministic integrity finding was recorded.
        </p>
      )}

      {evaluation.limitations.length > 0 && (
        <details className={styles.limitations}>
          <summary>What these checks do not verify</summary>
          <ul>
            {evaluation.limitations.map((limitation, index) => (
              <li key={`limitation-${index}`}>{limitation}</li>
            ))}
          </ul>
        </details>
      )}
    </section>
  );
};

function evaluationStatusVariant(status: ResearchEvaluationStatus): BadgeVariant {
  switch (status) {
    case 'passed':
      return 'success';
    case 'warnings':
      return 'warning';
    case 'failed':
      return 'error';
    default:
      return 'info';
  }
}

function dimensionStatusVariant(status: ResearchEvaluationDimensionStatus): BadgeVariant {
  switch (status) {
    case 'pass':
      return 'success';
    case 'warn':
      return 'warning';
    case 'fail':
      return 'error';
    case 'incomplete':
    case 'not_measurable':
      return 'info';
    default:
      return 'default';
  }
}
