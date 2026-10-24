/**
 * Restrained research provenance comparison.
 *
 * This panel is a read-only projection over two settled durable records. It
 * deliberately has no score, percentage, chart, ranking or "better run" copy.
 * Missing synthesis/evaluation is shown as unavailable, never as clean.
 */

import React, { useMemo, useState } from 'react';
import { Badge } from '../primitives';
import type { BadgeVariant } from '../primitives';
import { useRecoveryComparison } from '../../api/hooks';
import type {
  DurableExecution,
  DurableExecutionState,
  ResearchComparison as ResearchComparisonData,
  ResearchComparisonCategory,
  ResearchComparisonCategoryStatus,
  ResearchComparisonDifference,
  ResearchComparisonEvaluationSummary,
  ResearchComparisonEvidenceSummary,
  ResearchComparisonFinding,
  ResearchComparisonKind,
  ResearchComparisonPlanSummary,
  ResearchComparisonSeverity,
  ResearchComparisonSide,
  ResearchComparisonSynthesisSummary,
} from '../../types/api';
import { parseResearchComparison } from '../../types/researchComparison';
import { isComparisonEligible } from './comparisonEligibility';
import styles from './ResearchComparison.module.css';

const CATEGORY_ORDER: readonly ResearchComparisonCategory[] = [
  'plan',
  'steps',
  'operations',
  'evidence',
  'synthesis',
  'evaluation',
];

const CATEGORY_LABELS: Record<ResearchComparisonCategory, string> = {
  plan: 'Plan',
  steps: 'Steps',
  operations: 'Operations',
  evidence: 'Evidence',
  synthesis: 'Synthesis',
  evaluation: 'Evaluation',
};

const KIND_LABELS: Record<ResearchComparisonKind, string> = {
  added: 'Right only',
  removed: 'Left only',
  changed: 'Changed',
  unavailable: 'Unavailable',
  incompatible: 'Incompatible',
  legacy: 'Legacy',
};

const COMPATIBILITY_LABELS: Record<ResearchComparisonCategoryStatus, string> = {
  comparable: 'Comparable',
  partial: 'Partial',
  unavailable: 'Unavailable',
  incompatible: 'Incompatible',
};

const SEVERITY_LABELS: Record<ResearchComparisonSeverity, string> = {
  info: 'Info',
  warning: 'Warning',
  error: 'Error',
};

interface ResearchComparisonProps {
  businessId: string;
  executions: DurableExecution[];
  activeExecutionId: string | null;
}

export const ResearchComparison: React.FC<ResearchComparisonProps> = ({
  businessId,
  executions,
  activeExecutionId,
}) => {
  const eligible = useMemo(() => executions.filter(isComparisonEligible), [executions]);
  const eligibleIDs = useMemo(
    () => eligible.map((execution) => execution.execution_id),
    [eligible]
  );

  const [selectedLeftID, setSelectedLeftID] = useState('');
  const [selectedRightID, setSelectedRightID] = useState('');
  const [pair, setPair] = useState<{ left: string; right: string } | null>(null);

  // Selection is derived during render so a refreshed execution list can never
  // leave a select showing an ineligible or duplicate run. The effect-free
  // derivation also avoids an extra render on every list poll.
  const canCompare = eligibleIDs.length >= 2;
  const preferredLeft = canCompare
    ? activeExecutionId && eligibleIDs.includes(activeExecutionId)
      ? activeExecutionId
      : eligibleIDs[0]
    : '';
  const leftID =
    canCompare && eligibleIDs.includes(selectedLeftID) ? selectedLeftID : preferredLeft;
  const rightID = canCompare
    ? eligibleIDs.includes(selectedRightID) && selectedRightID !== leftID
      ? selectedRightID
      : eligibleIDs.find((id) => id !== leftID) ?? ''
    : '';

  const pairValid =
    pair !== null &&
    canCompare &&
    pair.left !== pair.right &&
    eligibleIDs.includes(pair.left) &&
    eligibleIDs.includes(pair.right);

  const comparisonQuery = useRecoveryComparison(
    businessId,
    pairValid ? pair.left : '',
    pairValid ? pair.right : '',
    { enabled: pairValid }
  );

  const parsed = useMemo(
    () => (pairValid ? parseResearchComparison(comparisonQuery.data) : undefined),
    [pairValid, comparisonQuery.data]
  );

  const handleCompare = () => {
    if (!canCompare || !leftID || !rightID || leftID === rightID) return;
    setPair({ left: leftID, right: rightID });
  };

  const handleLeftChange = (value: string) => {
    setSelectedLeftID(value);
    if (value === rightID) {
      setSelectedRightID(eligibleIDs.find((id) => id !== value) ?? '');
    }
    setPair(null);
  };

  const handleRightChange = (value: string) => {
    if (value === leftID) return;
    setSelectedRightID(value);
    setPair(null);
  };

  return (
    <section className={styles.comparison} aria-labelledby="research-comparison-heading">
      <div className={styles.header}>
        <div className={styles.heading}>
          <h3 id="research-comparison-heading" className={styles.title}>
            Compare recorded runs
          </h3>
          <p className={styles.intro}>
            Read-only comparison of the plan, steps, operations, sources, synthesis and
            evaluation recorded in two settled runs. It reports recorded differences only;
            it does not verify facts, score confidence or decide which run is better.
          </p>
        </div>
      </div>

      <div className={styles.controls}>
        <label className={styles.control}>
          <span>Left run</span>
          <select
            value={leftID}
            onChange={(event) => handleLeftChange(event.target.value)}
            disabled={eligibleIDs.length < 2}
            aria-label="Left comparison run"
          >
            {eligibleIDs.length === 0 && <option value="">No eligible runs</option>}
            {eligibleIDs.length > 0 && eligibleIDs.length < 2 && (
              <option value="">Select a run</option>
            )}
            {eligible.map((execution) => (
              <option
                key={execution.execution_id}
                value={execution.execution_id}
                disabled={execution.execution_id === rightID}
              >
                {runOptionLabel(execution)}
              </option>
            ))}
          </select>
        </label>

        <label className={styles.control}>
          <span>Right run</span>
          <select
            value={rightID}
            onChange={(event) => handleRightChange(event.target.value)}
            disabled={eligibleIDs.length < 2}
            aria-label="Right comparison run"
          >
            {eligibleIDs.length === 0 && <option value="">No eligible runs</option>}
            {eligibleIDs.length > 0 && eligibleIDs.length < 2 && (
              <option value="">Select a run</option>
            )}
            {eligible.map((execution) => (
              <option
                key={execution.execution_id}
                value={execution.execution_id}
                disabled={execution.execution_id === leftID}
              >
                {runOptionLabel(execution)}
              </option>
            ))}
          </select>
        </label>

        <button
          type="button"
          className={styles.compareButton}
          onClick={handleCompare}
          disabled={!leftID || !rightID || leftID === rightID || eligibleIDs.length < 2}
        >
          Compare
        </button>
      </div>

      {eligibleIDs.length < 2 && (
        <p className={styles.notice} role="status">
          At least two settled runs are needed. Provisional approval pauses and
          in-progress runs are not comparison snapshots.
        </p>
      )}

      {!pairValid && eligibleIDs.length >= 2 && (
        <p className={styles.hint}>
          Choose two settled runs. Missing synthesis or evaluation will be shown as
          unavailable, not as empty or clean.
        </p>
      )}

      {pairValid && comparisonQuery.isLoading && (
        <p className={styles.notice} role="status">
          Reading the two recorded snapshots…
        </p>
      )}

      {pairValid && comparisonQuery.error && (
        <p className={styles.error} role="alert">
          {comparisonErrorMessage(comparisonQuery.error)}
        </p>
      )}

      {pairValid && comparisonQuery.data !== undefined && !parsed && (
        <p className={styles.error} role="alert">
          The comparison response was not valid, so no partial comparison is shown.
        </p>
      )}

      {parsed && <ResearchComparisonResult comparison={parsed} />}
    </section>
  );
};

export const ResearchComparisonResult: React.FC<{ comparison: ResearchComparisonData }> = ({
  comparison,
}) => {
  const grouped = useMemo(
    () =>
      CATEGORY_ORDER.map((category) => ({
        category,
        differences: comparison.differences.filter(
          (difference) => difference.category === category
        ),
      })).filter((group) => group.differences.length > 0),
    [comparison.differences]
  );

  return (
    <div
      className={styles.result}
      data-comparison-status={comparison.compatibility.status}
      data-comparison-version={comparison.version}
    >
      <div className={styles.verdict}>
        <Badge variant={compatibilityVariant(comparison.compatibility.status)} size="sm">
          {compatibilityLabel(comparison.compatibility.status)}
        </Badge>
        <p>{compatibilityCopy(comparison.compatibility.status)}</p>
      </div>

      <ul className={styles.categories} aria-label="Comparison categories">
        {comparison.compatibility.categories.map((category) => (
          <li key={category.category} className={styles.category}>
            <span>{CATEGORY_LABELS[category.category]}</span>
            <Badge variant={categoryVariant(category.status)} size="sm">
              {COMPATIBILITY_LABELS[category.status]}
            </Badge>
          </li>
        ))}
      </ul>

      <div className={styles.sides}>
        <SideSummary side={comparison.left} />
        <SideSummary side={comparison.right} />
      </div>

      {comparison.truncated && (
        <p className={styles.notice} role="status">
          The comparison was truncated to its bounded output. The first canonical
          differences and findings are shown.
        </p>
      )}

      <section className={styles.differences} aria-label="Recorded differences">
        <h4 className={styles.sectionHeading}>Recorded differences</h4>
        {grouped.length === 0 ? (
          <p className={styles.noDifferences}>
            No recorded differences in comparable fields. This is not proof that
            the runs are factually equivalent.
          </p>
        ) : (
          grouped.map((group) => (
            <div key={group.category} className={styles.differenceGroup}>
              <h5 className={styles.groupHeading}>
                {CATEGORY_LABELS[group.category]}
                <span>{group.differences.length}</span>
              </h5>
              <ul className={styles.differenceList}>
                {group.differences.map((difference, index) => (
                  <DifferenceRow
                    key={`${difference.category}-${difference.entity}-${difference.field ?? ''}-${difference.id ?? ''}-${index}`}
                    difference={difference}
                  />
                ))}
              </ul>
            </div>
          ))
        )}
      </section>

      <section className={styles.findings} aria-label="Comparison findings">
        <h4 className={styles.sectionHeading}>Findings</h4>
        {comparison.findings.length === 0 ? (
          <p className={styles.noDifferences}>No comparison finding was recorded.</p>
        ) : (
          <ul className={styles.findingList}>
            {comparison.findings.map((finding, index) => (
              <FindingRow key={`${finding.code}-${index}`} finding={finding} />
            ))}
          </ul>
        )}
      </section>

      {comparison.limitations.length > 0 && (
        <details className={styles.limitations}>
          <summary>Fixed limitations ({comparison.limitations.length})</summary>
          <ul>
            {comparison.limitations.map((limitation, index) => (
              <li key={`limitation-${index}`}>{limitation}</li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
};

const SideSummary: React.FC<{ side: ResearchComparisonSide }> = ({ side }) => (
  <article className={styles.side} aria-label={`${side.role} run metadata`}>
    <div className={styles.sideHeader}>
      <h4 className={styles.sideTitle}>{side.role === 'left' ? 'Left run' : 'Right run'}</h4>
      <Badge variant={stateVariant(side.state)} size="sm">
        {formatLabel(side.state)}
      </Badge>
    </div>
    <dl className={styles.metadata}>
      <div>
        <dt>Execution</dt>
        <dd className={styles.mono}>{side.execution_id}</dd>
      </div>
      <div>
        <dt>Scope</dt>
        <dd>{side.division_id ? `Division ${side.division_id}` : 'Business-wide'}</dd>
      </div>
      <div>
        <dt>Created</dt>
        <dd>{formatTimestamp(side.created_at)}</dd>
      </div>
      <div>
        <dt>Completed</dt>
        <dd>{side.completed_at ? formatTimestamp(side.completed_at) : 'Not recorded'}</dd>
      </div>
      <div>
        <dt>Provenance</dt>
        <dd>
          {side.provenance.evidence} evidence · {side.provenance.synthesis} synthesis ·{' '}
          {side.provenance.evaluation} evaluation
        </dd>
      </div>
    </dl>

    <div className={styles.artifact}>
      <h5>Evidence</h5>
      {side.evidence ? (
        <EvidenceDetails evidence={side.evidence} />
      ) : (
        <p>No durable evidence snapshot was recorded.</p>
      )}
    </div>

    <div className={styles.artifact}>
      <h5>Plan</h5>
      {side.plan ? (
        <PlanDetails plan={side.plan} />
      ) : (
        <p>No plan snapshot was recorded.</p>
      )}
    </div>

    <div className={styles.artifact}>
      <h5>Synthesis</h5>
      {side.synthesis ? (
        <SynthesisDetails synthesis={side.synthesis} />
      ) : (
        <p>Not recorded. This is not an empty or clean synthesis.</p>
      )}
    </div>

    <div className={styles.artifact}>
      <h5>Evaluation</h5>
      {side.evaluation ? (
        <EvaluationDetails evaluation={side.evaluation} />
      ) : (
        <p>Not recorded. This is not a clean verdict.</p>
      )}
    </div>
  </article>
);

const EvidenceDetails: React.FC<{ evidence: ResearchComparisonEvidenceSummary }> = ({
  evidence,
}) => (
  <div className={styles.artifactDetails}>
    <p>
      Evidence v{evidence.version} · {evidence.source_count} source
      {evidence.source_count === 1 ? '' : 's'} · {evidence.step_count} step
      {evidence.step_count === 1 ? '' : 's'} · {evidence.operation_count} operation
      {evidence.operation_count === 1 ? '' : 's'}
    </p>
    <p>
      {evidence.version < 3
        ? 'Legacy provenance: missing v3 fields stay unavailable.'
        : evidence.attribution === 'derived'
          ? 'Source-to-step attribution recorded as derived.'
          : 'Source-to-step attribution unavailable.'}
    </p>
  </div>
);

const PlanDetails: React.FC<{ plan: ResearchComparisonPlanSummary }> = ({ plan }) => (
  <div className={styles.artifactDetails}>
    <p>
      Plan revision {plan.version}
      {plan.provenance === 'legacy' ? ' · legacy provenance' : ' · authoritative provenance'}
    </p>
    <p className={styles.mono}>Plan id (execution-scoped): {plan.plan_id}</p>
    <p>
      {plan.objective_id
        ? 'Objective reference recorded (execution-scoped; not used to infer a shared objective).'
        : 'Objective reference not recorded.'}
    </p>
    <p>
      {plan.completion_recorded
        ? `Completion step recorded${plan.completion_step_id ? `: ${plan.completion_step_id}` : '.'}`
        : 'Completion-step provenance unavailable.'}
    </p>
  </div>
);

const SynthesisDetails: React.FC<{ synthesis: ResearchComparisonSynthesisSummary }> = ({
  synthesis,
}) => (
  <div className={styles.artifactDetails}>
    <p>
      {formatLabel(synthesis.status)} · {synthesis.evidence_backed_count} evidence-backed ·{' '}
      {synthesis.inference_count} inference{synthesis.inference_count === 1 ? '' : 's'}
    </p>
    <p>
      {synthesis.plan_version_recorded
        ? `Bound to plan revision ${synthesis.plan_version}.`
        : 'Plan revision unavailable.'}
    </p>
  </div>
);

const EvaluationDetails: React.FC<{ evaluation: ResearchComparisonEvaluationSummary }> = ({
  evaluation,
}) => (
  <div className={styles.artifactDetails}>
    <p>
      {formatLabel(evaluation.status)} · {evaluation.dimension_count} dimension
      {evaluation.dimension_count === 1 ? '' : 's'} · {evaluation.finding_count} finding
      {evaluation.finding_count === 1 ? '' : 's'}
    </p>
    <p>
      {evaluation.plan_version_recorded
        ? `Bound to plan revision ${evaluation.plan_version}.`
        : 'Plan revision unavailable.'}
      {evaluation.synthesis_status
        ? ` Synthesis status recorded as ${formatLabel(evaluation.synthesis_status)}.`
        : ''}
    </p>
  </div>
);

const DifferenceRow: React.FC<{ difference: ResearchComparisonDifference }> = ({
  difference,
}) => (
  <li className={styles.difference} data-difference-kind={difference.kind}>
    <div className={styles.differenceHeader}>
      <Badge variant={differenceVariant(difference.kind)} size="sm">
        {KIND_LABELS[difference.kind]}
      </Badge>
      <span className={styles.differenceEntity}>
        {formatLabel(difference.entity)}
        {difference.field ? ` · ${formatLabel(difference.field)}` : ''}
      </span>
      {difference.id && <code className={styles.differenceID}>{difference.id}</code>}
    </div>
    <dl className={styles.values}>
      <div>
        <dt>Left</dt>
        <dd>{difference.left || '—'}</dd>
      </div>
      <div>
        <dt>Right</dt>
        <dd>{difference.right || '—'}</dd>
      </div>
    </dl>
  </li>
);

const FindingRow: React.FC<{ finding: ResearchComparisonFinding }> = ({ finding }) => (
  <li
    className={styles.finding}
    data-finding-code={finding.code}
    data-finding-severity={finding.severity}
  >
    <Badge variant={findingVariant(finding.severity)} size="sm">
      {SEVERITY_LABELS[finding.severity]}
    </Badge>
    <div className={styles.findingText}>
      <code>{finding.code}</code>
      <p>{finding.message}</p>
      {finding.category && (
        <span className={styles.findingCategory}>{CATEGORY_LABELS[finding.category]}</span>
      )}
    </div>
  </li>
);

function runOptionLabel(execution: DurableExecution): string {
  const id =
    execution.execution_id.length > 28
      ? `${execution.execution_id.slice(0, 28)}…`
      : execution.execution_id;
  return `${id} · ${formatLabel(execution.state)}`;
}

function formatTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'Unavailable';
  return date.toLocaleString();
}

function formatLabel(value: string): string {
  return value.replace(/_/g, ' ');
}

function compatibilityLabel(status: ResearchComparisonData['compatibility']['status']): string {
  switch (status) {
    case 'comparable':
      return 'Comparable records';
    case 'partial':
      return 'Partial comparison';
    default:
      return 'Incompatible records';
  }
}

function compatibilityCopy(status: ResearchComparisonData['compatibility']['status']): string {
  switch (status) {
    case 'comparable':
      return 'Every category could be compared. This does not establish factual equivalence, source support or correctness.';
    case 'partial':
      return 'Some recorded facts were unavailable, legacy or incompatible. Missing facts are not treated as empty or clean.';
    default:
      return 'No category could be meaningfully compared. The records are not evidence of equivalence.';
  }
}

function compatibilityVariant(
  status: ResearchComparisonData['compatibility']['status']
): BadgeVariant {
  switch (status) {
    case 'comparable':
      return 'success';
    case 'partial':
      return 'warning';
    default:
      return 'error';
  }
}

function categoryVariant(status: ResearchComparisonCategoryStatus): BadgeVariant {
  switch (status) {
    case 'comparable':
      return 'success';
    case 'partial':
      return 'warning';
    case 'incompatible':
      return 'error';
    default:
      return 'info';
  }
}

function stateVariant(state: DurableExecutionState): BadgeVariant {
  switch (state) {
    case 'completed':
      return 'success';
    case 'failed':
      return 'error';
    case 'cancelled':
    case 'timed_out':
      return 'warning';
    case 'unknown':
      return 'info';
    default:
      return 'default';
  }
}

function differenceVariant(kind: ResearchComparisonKind): BadgeVariant {
  switch (kind) {
    case 'added':
      return 'info';
    case 'removed':
      return 'warning';
    case 'incompatible':
      return 'error';
    case 'legacy':
    case 'unavailable':
      return 'default';
    default:
      return 'warning';
  }
}

function findingVariant(severity: ResearchComparisonSeverity): BadgeVariant {
  switch (severity) {
    case 'error':
      return 'error';
    case 'warning':
      return 'warning';
    default:
      return 'info';
  }
}

function comparisonErrorMessage(error: unknown): string {
  if (error && typeof error === 'object' && 'status' in error) {
    const status = (error as { status?: unknown }).status;
    if (status === 409) {
      return 'One or both selected runs are not settled comparison snapshots.';
    }
    if (status === 404) {
      return 'One or both selected runs are no longer available in this scope.';
    }
    if (status === 403) {
      return 'You are not authorized to compare these runs.';
    }
    if (status === 401) {
      return 'Sign in again to compare recorded runs.';
    }
  }
  return 'The comparison could not be loaded. The recorded runs were not changed.';
}
