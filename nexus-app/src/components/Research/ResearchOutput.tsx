/**
 * ResearchOutput - Displays the final research result.
 *
 * Rendering contract:
 * - `result=` prose is agent-generated and rendered as text only.
 * - Runtime metrics come from the execution summary prefix.
 * - Structured output is recognized only when parseResearchResult finds the
 *   documented web.search envelope; sources are rendered by ResearchEvidence.
 * - Malformed or unrecognized output is shown verbatim as text.
 * - Timestamps shown here come only from the durable recovery read's real
 *   lifecycle fields (created_at / completed_at). They are never inferred from
 *   updated_at or client-side time, and are omitted entirely when the durable
 *   record cannot provide them.
 * - When the process-local execution read is unavailable but the durable read
 *   proves a terminal state (e.g. after a restart), the panel reports the
 *   result text as unavailable instead of pretending there is no result.
 * - A recovered plan is rendered with its durable plan identity, ordered steps,
 *   dependencies and ledger statuses. It is visually and textually distinct
 *   from the process-local trace, which is labeled as live, non-durable state.
 */

import React, { useMemo } from 'react';
import { Card, Badge, getStatusVariant } from '../primitives';
import type {
  DurableResearchPlan,
  DurableResearchStepStatus,
  ExecutionStatus,
  ParsedWebSearchSource,
  ResearchEvaluation,
} from '../../types/api';
import {
  parseResearchResult,
  type DurableParsedSynthesis,
  type ParsedDurableStep,
  type ParsedResearchSynthesis,
  type ParsedSynthesisStatement,
  type ResearchStep,
  type ResearchFindings,
} from '../../types/research';
import { ResearchQuality } from './ResearchQuality';
import styles from './ResearchOutput.module.css';

interface ResearchOutputProps {
  output?: string;
  status?: string;
  error?: ExecutionStatus['error'];
  durationMs?: number;
  isLoading?: boolean;
  /** Durable state from the recovery detail read, when that read is available. */
  durableState?: string;
  /** Real lifecycle timestamps from the durable record. Undefined means unavailable. */
  createdAt?: string | null;
  completedAt?: string | null;
  /** Process-local step states from the execution trace. */
  steps?: ResearchStep[];
  /** Findings: synthesis kept distinct from source records. */
  findings?: ResearchFindings;
  /** Durable plan identity; present only when the journal persisted it. */
  durablePlan?: DurableResearchPlan;
  /** Durable evidence schema version; 1/2 are legacy records. */
  durableEvidenceVersion?: number;
  /** Durable source-to-step attribution state, when the record states one. */
  durableAttribution?: 'derived' | 'unavailable';
  /** Recovered durable steps in plan order. Distinct from process-local steps. */
  durableSteps?: ParsedDurableStep[];
  /**
   * Defensively parsed deterministic quality verdict. Persisted with the
   * execution, so it may be present after the process-local synthesis is gone.
   */
  evaluation?: ResearchEvaluation;
  /**
   * Durable, validated synthesis resolved against durable evidence. Persisted
   * alongside evidence and evaluation; survives a restart. Rendered distinctly
   * from the process-local live synthesis.
   */
  durableSynthesis?: DurableParsedSynthesis;
}

const DURABLE_TERMINAL_STATES = new Set([
  'completed',
  'failed',
  'cancelled',
  'timed_out',
  'unknown',
]);

export const ResearchOutput: React.FC<ResearchOutputProps> = ({
  output,
  status,
  error,
  durationMs,
  isLoading = false,
  durableState,
  createdAt,
  completedAt,
  steps,
  findings,
  durablePlan,
  durableEvidenceVersion,
  durableAttribution,
  durableSteps,
  evaluation,
  durableSynthesis,
}) => {
  const parsed = useMemo(() => parseResearchResult(output), [output]);

  if (isLoading) {
    return (
      <Card variant="default" padding="lg">
        <div className={styles.loading}>
          <div className={styles.spinner} aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeDasharray="31.4 31.4" className={styles.spinnerCircle} />
            </svg>
          </div>
          <p>Research in progress…</p>
          <span className={styles.loadingHint}>The agent is gathering and synthesizing sources</span>
        </div>
      </Card>
    );
  }

  if (parsed.kind === 'empty') {
    // No process-local status means the execution read provided nothing (e.g.
    // the result is gone after a backend restart). The durable read is then
    // the honest source: show the persisted state and say plainly that the
    // result text itself is unavailable.
    const durableTerminal =
      status === undefined && durableState !== undefined && DURABLE_TERMINAL_STATES.has(durableState);
    if (durableTerminal) {
      const copy = durableUnavailableCopy(durableState, Boolean(durableSynthesis));
      const details = lifecycleDetails(createdAt, completedAt);
      return (
        <Card variant="outlined" padding="lg">
          {durablePlan && durableSteps && durableSteps.length > 0 && (
            <DurablePlanSection
              plan={durablePlan}
              steps={durableSteps}
              evidenceVersion={durableEvidenceVersion}
              attribution={durableAttribution}
            />
          )}
          {evaluation && (
            <ResearchQuality
              evaluation={evaluation}
              recorded
              synthesisAvailable={Boolean(durableSynthesis)}
            />
          )}
          {durableSynthesis && (
            <DurableSynthesisBlock
              synthesis={durableSynthesis}
              sources={findings?.sources ?? []}
            />
          )}
          <div className={styles.empty}>
            <div className={styles.resultHeader}>
              <div className={styles.resultHeading}>
                <h3 className={styles.title}>{copy.title}</h3>
                <p className={styles.provenance}>Durable execution record.</p>
              </div>
              <Badge variant={getStatusVariant(durableState)} size="sm">
                {statusLabel(durableState)}
              </Badge>
            </div>
            <p>{copy.description}</p>
            <span>{copy.hint}</span>
            {details.length > 0 && (
              <dl className={styles.metaGrid} aria-label="Execution details">
                {details.map((detail) => (
                  <div key={detail.label} className={styles.metaItem}>
                    <dt>{detail.label}</dt>
                    <dd>{detail.value}</dd>
                  </div>
                ))}
              </dl>
            )}
          </div>
        </Card>
      );
    }

    const empty = emptyState(status, error?.message);
    return (
      <Card variant="outlined" padding="lg">
        <div className={styles.empty}>
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
            <path d="M24 16v16M16 24h16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>{empty.title}</p>
          <span>{empty.description}</span>
        </div>
      </Card>
    );
  }

  const runtimeStatus = status || parsed.runtimeState || 'unknown';
  const isFailure =
    runtimeStatus === 'failed' || runtimeStatus === 'cancelled' || runtimeStatus === 'timed_out';
  const details = buildDetails(parsed.objective, parsed.agent, parsed.metrics, durationMs, createdAt, completedAt);
  // The synthesis block is the single place a conclusion is rendered; the
  // fallback below is only for callers that do not supply findings. A rejected
  // typed synthesis also suppresses result prose rather than silently turning
  // an unverifiable answer into a conclusion.
  const synthesisRendered = Boolean(
    findings?.typedSynthesis || findings?.synthesis || findings?.synthesisRejected
  );

  return (
    <Card variant={isFailure ? 'outlined' : 'default'} padding="lg">
      <div className={styles.output}>
        <div className={styles.resultHeader}>
          <div className={styles.resultHeading}>
            <h3 className={styles.title}>
              {isFailure ? failureTitle(runtimeStatus) : titleForKind(parsed.kind)}
            </h3>
            <p className={styles.provenance}>
              {parsed.kind === 'summary'
                ? 'Agent-generated result. Source records are listed separately only when the execution provides them.'
                : parsed.kind === 'structured'
                  ? 'Structured execution output.'
                  : 'Execution output.'}
            </p>
          </div>
          <Badge variant={getStatusVariant(runtimeStatus)} size="sm">
            {statusLabel(runtimeStatus)}
          </Badge>
        </div>

        {isFailure && (
          <div className={styles.errorBanner} role="alert">
            <span className={styles.errorCode}>{error?.code || runtimeStatus}</span>
            <p>{error?.message || failureDescription(runtimeStatus)}</p>
          </div>
        )}

        {durablePlan && durableSteps && durableSteps.length > 0 ? (
          <DurablePlanSection
            plan={durablePlan}
            steps={durableSteps}
            evidenceVersion={durableEvidenceVersion}
            attribution={durableAttribution}
          />
        ) : (
          steps &&
          steps.length > 0 && (
            <section className={styles.stepStates} aria-label="Process-local research steps">
              <div className={styles.stepStatesHeader}>
                <h4 className={styles.stepStatesTitle}>Research steps</h4>
                <Badge variant="default" size="sm">
                  Live trace
                </Badge>
              </div>
              <p className={styles.stepStatesNote}>
                Process-local lifecycle events from this running instance. Not durably persisted.
              </p>
              <ul className={styles.stepStatesList}>
                {steps.map((step) => (
                  <li
                    key={step.stepId}
                    className={`${styles.stepItem} ${styles['step-' + step.status]}`}
                  >
                    <span className={styles.stepId}>{step.stepId}</span>
                    <Badge variant={stepStatusVariant(step.status)} size="sm">
                      {stepStatusLabel(step.status)}
                    </Badge>
                    {step.actionType && (
                      <span className={styles.stepAction}>{step.actionType}</span>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )
        )}

        {evaluation && <ResearchQuality evaluation={evaluation} />}

        {/*
          The live typed synthesis wins when the process still holds it: it is
          the same validated answer the durable record carries, so rendering
          both would duplicate identical text. The durable synthesis is the
          recovery path — shown when the live one is gone (e.g. after a
          restart) — and is always labeled as a durable record.
        */}
        {durableSynthesis && !findings?.typedSynthesis && (
          <DurableSynthesisBlock
            synthesis={durableSynthesis}
            sources={findings?.sources ?? []}
          />
        )}

        {findings?.typedSynthesis && (
          <TypedSynthesisBlock synthesis={findings.typedSynthesis} sources={findings.sources} />
        )}

        {findings?.synthesisRejected && (
          <div className={styles.synthesisRejected} role="alert" aria-label="Synthesis withheld">
            <h4 className={styles.synthesisTitle}>Synthesis withheld</h4>
            <p>
              {findings.synthesisRejected === 'no_evidence'
                ? 'The synthesized answer could not be checked against a validated evidence snapshot from this execution, so it is not shown.'
                : 'The synthesized answer did not pass this page’s citation checks against the captured evidence, so it is not shown.'}
            </p>
            <span>
              No citation, source title or result prose is inferred from the withheld answer.
            </span>
          </div>
        )}

        {findings?.synthesis && (
          <div className={styles.synthesisBlock} aria-label="Research synthesis">
            <h4 className={styles.synthesisTitle}>Synthesis</h4>
            <p className={styles.synthesisText}>{findings.synthesis}</p>
            {findings.partial && (
              <p className={styles.partialNotice}>
                Some research steps did not complete. Findings are partial and
                reflect only the steps that finished.
              </p>
            )}
            {findings.evidencePresent && findings.sources.length > 0 && (
              <p className={styles.sourcesNote}>
                {findings.sources.length} source record{findings.sources.length !== 1 ? 's' : ''}
                captured from tool output and listed in Sources &amp; Evidence.
              </p>
            )}
          </div>
        )}

        {parsed.webSearch ? (
          <p className={styles.structuredResult}>{structuredSummary(parsed.webSearch.count)}</p>
        ) : synthesisRendered ? null : parsed.resultText ? (
          <div className={styles.outputContent}>{formatOutput(parsed.resultText)}</div>
        ) : parsed.kind === 'text' ? (
          <div className={styles.outputContent}>{formatOutput(parsed.raw)}</div>
        ) : (
          <p className={styles.noResult}>
            {isFailure
              ? failureDescription(runtimeStatus)
              : 'The execution completed without a textual result.'}
          </p>
        )}

        {details.length > 0 && (
          <dl className={styles.metaGrid} aria-label="Execution details">
            {details.map((detail) => (
              <div key={detail.label} className={styles.metaItem}>
                <dt>{detail.label}</dt>
                <dd>{detail.value}</dd>
              </div>
            ))}
          </dl>
        )}

        {(parsed.kind === 'summary' || parsed.kind === 'structured') &&
          !findings?.typedSynthesis &&
          !findings?.synthesisRejected && (
            <details className={styles.rawDetails}>
              <summary>Execution summary</summary>
              <pre className={styles.rawSummary}>{parsed.raw}</pre>
            </details>
          )}
      </div>
    </Card>
  );
};

interface DetailItem {
  label: string;
  value: string;
}

const DurablePlanSection: React.FC<{
  plan: DurableResearchPlan;
  steps: ParsedDurableStep[];
  evidenceVersion?: number;
  attribution?: 'derived' | 'unavailable';
}> = ({ plan, steps, evidenceVersion, attribution }) => {
  const legacy = evidenceVersion !== undefined && evidenceVersion < 3;
  return (
    <section
      className={styles.stepStates}
      aria-label="Durable research plan"
      data-provenance="durable"
      data-evidence-version={evidenceVersion}
    >
      <div className={styles.stepStatesHeader}>
        <h4 className={styles.stepStatesTitle}>Research plan</h4>
        <div className={styles.planBadges}>
          <Badge variant="info" size="sm">
            Durable record
          </Badge>
          {legacy && (
            <Badge variant="default" size="sm">
              Legacy v{evidenceVersion}
            </Badge>
          )}
        </div>
      </div>
      <p className={styles.stepStatesNote}>
        Historical plan snapshot persisted with this execution at its terminal
        transition. It is not the current live plan. Plan{' '}
        <span className={styles.planIdentity}>{plan.plan_id}</span>, revision{' '}
        {plan.version}.
      </p>
      <p className={styles.planMeta}>
        {plan.objective_id && (
          <span>
            Objective <span className={styles.planIdentity}>{plan.objective_id}</span>
          </span>
        )}
        {plan.completion_step_id ? (
          <span>
            Settled by completion step{' '}
            <span className={styles.planIdentity}>{plan.completion_step_id}</span>
          </span>
        ) : plan.provenance === 'authoritative' ? (
          <span>No plan step settled completion.</span>
        ) : null}
        {plan.provenance === 'authoritative' ? (
          <span>Plan facts recorded by the runtime.</span>
        ) : (
          <span>Plan provenance markers unavailable on this legacy record.</span>
        )}
        {attribution === 'derived' ? (
          <span>Source-to-step grouping derived from runtime operation records.</span>
        ) : attribution === 'unavailable' ? (
          <span>Source-to-step attribution unavailable.</span>
        ) : null}
      </p>
      {legacy && (
        <p className={styles.legacyNotice}>
          This record predates v3 plan provenance. Step intent, objective
          reference, completion relationship and authoritative markers were not
          captured, so they are shown as unavailable rather than inferred.
        </p>
      )}
      <ul className={styles.stepStatesList}>
        {steps.map((step) => (
          <li
            key={step.stepId}
            className={`${styles.stepItem} ${styles[durableStepClassName(step.status)]}`}
            data-step-id={step.stepId}
            data-step-status={step.status}
          >
            <span className={styles.stepId}>{step.stepId}</span>
            <Badge variant={durableStepStatusVariant(step.status)} size="sm">
              {durableStepStatusLabel(step.status)}
            </Badge>
            {step.intent && <span className={styles.stepIntent}>{step.intent}</span>}
            {step.dependencies.length > 0 && (
              <span className={styles.stepDependencies}>
                after {step.dependencies.join(', ')}
              </span>
            )}
            {step.planVersion !== undefined && step.planVersion !== plan.version && (
              <span className={styles.stepDependencies}>
                declared in revision {step.planVersion}
              </span>
            )}
            <span className={styles.stepSources}>
              {step.sources.length} source{step.sources.length === 1 ? '' : 's'}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
};

/**
 * Durable, validated synthesis persisted through the execution journal.
 * Rendered distinctly from the process-local live synthesis: every citation
 * link and visible title is looked up in the durable source record by id.
 * Labeled as a durable record so users can tell a persisted conclusion from
 * live, in-memory state.
 */
const DurableSynthesisBlock: React.FC<{
  synthesis: DurableParsedSynthesis;
  sources: ParsedWebSearchSource[];
}> = ({ synthesis, sources }) => (
  <div
    className={styles.synthesisBlock}
    aria-label="Durable research synthesis"
    data-synthesis-status={synthesis.status}
    data-provenance="durable"
  >
    <div className={styles.synthesisHeader}>
      <div>
        <h4 className={styles.synthesisTitle}>Durable synthesis</h4>
        <p className={styles.provenance}>
          Persisted validated answer, recovered from the execution journal.
          Citation titles and URLs come from Sources &amp; Evidence, never from
          the model.
        </p>
        <p className={styles.provenance}>
          {synthesis.planVersion > 0
            ? `Bound to plan revision ${synthesis.planVersion}.`
            : 'Plan revision unavailable on this legacy synthesis.'}
        </p>
      </div>
      <Badge variant="info" size="sm">
        Durable record
      </Badge>
      <Badge variant={synthesisStatusVariant(synthesis.status)} size="sm">
        {synthesisStatusLabel(synthesis.status)}
      </Badge>
    </div>

    <p className={styles.synthesisText}>{synthesis.summary}</p>

    {synthesis.status === 'partial' && (
      <p className={styles.partialNotice} role="status">
        This answer is partial: the run did not fully settle its planned research.
        Unresolved questions are listed below.
      </p>
    )}
    {synthesis.status === 'inconclusive' && (
      <p className={styles.inconclusiveNotice} role="status">
        No evidence-backed conclusion was reached. The run either captured no
        supporting sources or could not settle the objective, so only unresolved
        questions are reported.
      </p>
    )}

    {synthesis.evidenceBacked.length > 0 && (
      <section className={styles.synthesisSection} aria-label="Evidence-backed findings">
        <h5 className={styles.synthesisSectionTitle}>Evidence-backed findings</h5>
        <SynthesisClaims claims={synthesis.evidenceBacked} />
      </section>
    )}

    {synthesis.inferences.length > 0 && (
      <section className={styles.synthesisSection} aria-label="Inferences">
        <h5 className={styles.synthesisSectionTitle}>
          Inference <span className={styles.inferenceLabel}>not evidence</span>
        </h5>
        <ul className={styles.claimList}>
          {synthesis.inferences.map((inference, index) => (
            <li key={`inference-${index}`} className={styles.inferenceItem}>
              {inference}
            </li>
          ))}
        </ul>
      </section>
    )}

    {synthesis.conflicts.length > 0 && (
      <section className={styles.synthesisSection} aria-label="Conflicting evidence">
        <h5 className={styles.synthesisSectionTitle}>Conflicting evidence</h5>
        <SynthesisClaims claims={synthesis.conflicts} conflict />
      </section>
    )}

    {synthesis.unresolvedQuestions.length > 0 && (
      <section className={styles.synthesisSection} aria-label="Unresolved questions">
        <h5 className={styles.synthesisSectionTitle}>Unresolved questions</h5>
        <ul className={styles.questionList}>
          {synthesis.unresolvedQuestions.map((question, index) => (
            <li key={`question-${index}`}>{question}</li>
          ))}
        </ul>
      </section>
    )}

    {sources.length > 0 && (
      <p className={styles.sourcesNote}>
        {sources.length} captured source record{sources.length === 1 ? '' : 's'}.
        Citation titles and URLs come from Sources &amp; Evidence, never from the model.
      </p>
    )}
    {sources.length === 0 && (
      <p className={styles.sourcesNote}>
        No source records were captured for this execution.
      </p>
    )}
  </div>
);

/**
 * Typed, evidence-grounded synthesis. Every citation link and visible title is
 * looked up in the durable source record by id; the model's answer contributes
 * only statement text and ids.
 */
const TypedSynthesisBlock: React.FC<{
  synthesis: ParsedResearchSynthesis;
  sources: ParsedWebSearchSource[];
}> = ({ synthesis, sources }) => (
  <div
    className={styles.synthesisBlock}
    aria-label="Typed research synthesis"
    data-synthesis-status={synthesis.status}
  >
    <div className={styles.synthesisHeader}>
      <div>
        <h4 className={styles.synthesisTitle}>Synthesis</h4>
        <p className={styles.provenance}>
          Evidence-grounded answer with citations resolved against this execution.
        </p>
      </div>
      <Badge variant={synthesisStatusVariant(synthesis.status)} size="sm">
        {synthesisStatusLabel(synthesis.status)}
      </Badge>
    </div>

    <p className={styles.synthesisText}>{synthesis.summary}</p>

    {synthesis.status === 'partial' && (
      <p className={styles.partialNotice} role="status">
        This answer is partial: the run did not fully settle its planned research.
        Unresolved questions are listed below.
      </p>
    )}
    {synthesis.status === 'inconclusive' && (
      <p className={styles.inconclusiveNotice} role="status">
        No evidence-backed conclusion was reached. The run either captured no
        supporting sources or could not settle the objective, so only unresolved
        questions are reported.
      </p>
    )}

    {synthesis.evidenceBacked.length > 0 && (
      <section className={styles.synthesisSection} aria-label="Evidence-backed findings">
        <h5 className={styles.synthesisSectionTitle}>Evidence-backed findings</h5>
        <SynthesisClaims claims={synthesis.evidenceBacked} />
      </section>
    )}

    {synthesis.inferences.length > 0 && (
      <section className={styles.synthesisSection} aria-label="Inferences">
        <h5 className={styles.synthesisSectionTitle}>
          Inference <span className={styles.inferenceLabel}>not evidence</span>
        </h5>
        <ul className={styles.claimList}>
          {synthesis.inferences.map((inference, index) => (
            <li key={`inference-${index}`} className={styles.inferenceItem}>
              {inference}
            </li>
          ))}
        </ul>
      </section>
    )}

    {synthesis.conflicts.length > 0 && (
      <section className={styles.synthesisSection} aria-label="Conflicting evidence">
        <h5 className={styles.synthesisSectionTitle}>Conflicting evidence</h5>
        <SynthesisClaims claims={synthesis.conflicts} conflict />
      </section>
    )}

    {synthesis.unresolvedQuestions.length > 0 && (
      <section className={styles.synthesisSection} aria-label="Unresolved questions">
        <h5 className={styles.synthesisSectionTitle}>Unresolved questions</h5>
        <ul className={styles.questionList}>
          {synthesis.unresolvedQuestions.map((question, index) => (
            <li key={`question-${index}`}>{question}</li>
          ))}
        </ul>
      </section>
    )}

    {sources.length > 0 && (
      <p className={styles.sourcesNote}>
        {sources.length} captured source record{sources.length === 1 ? '' : 's'}.
        Citation titles and URLs come from Sources &amp; Evidence, never from the model.
      </p>
    )}
    {sources.length === 0 && (
      <p className={styles.sourcesNote}>
        No source records were captured for this execution.
      </p>
    )}
  </div>
);

const SynthesisClaims: React.FC<{ claims: ParsedSynthesisStatement[]; conflict?: boolean }> = ({
  claims,
  conflict = false,
}) => (
  <ul className={styles.claimList}>
    {claims.map((claim, index) => (
      <li
        key={`claim-${index}`}
        className={conflict ? styles.conflictItem : styles.claimItem}
      >
        <p>{claim.statement}</p>
        <ul className={styles.citationList} aria-label="Citations">
          {claim.citations.map((citation) => {
            const label = citation.source.title.trim() || citation.source.url;
            return (
              <li key={citation.sourceId} data-source-id={citation.sourceId}>
                {citation.source.linkable ? (
                  <a
                    className={styles.citationTitle}
                    href={citation.source.url}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {label}
                  </a>
                ) : (
                  <span className={styles.citationTitle}>{label}</span>
                )}
                <span className={styles.citationUrl}>{citation.source.url}</span>
                <span className={styles.citationMeta}>
                  {citation.source.source || 'captured source'} · {citation.sourceId}
                </span>
              </li>
            );
          })}
        </ul>
      </li>
    ))}
  </ul>
);

function synthesisStatusVariant(
  status: ParsedResearchSynthesis['status']
): import('../primitives').BadgeVariant {
  switch (status) {
    case 'complete':
      return 'success';
    case 'partial':
      return 'warning';
    default:
      return 'default';
  }
}

function synthesisStatusLabel(status: ParsedResearchSynthesis['status']): string {
  switch (status) {
    case 'complete':
      return 'Complete';
    case 'partial':
      return 'Partial';
    case 'inconclusive':
      return 'Inconclusive';
    default:
      return status;
  }
}

function buildDetails(
  objective: string | undefined,
  agent: string | undefined,
  metrics: ReturnType<typeof parseResearchResult>['metrics'],
  durationMs: number | undefined,
  createdAt?: string | null,
  completedAt?: string | null
): DetailItem[] {
  const details: DetailItem[] = [];
  if (objective) details.push({ label: 'Objective', value: objective });
  if (agent) details.push({ label: 'Agent', value: agent });
  if (durationMs !== undefined) details.push({ label: 'Duration', value: formatDuration(durationMs) });
  if (metrics.iterations !== undefined) {
    details.push({ label: 'Iterations', value: String(metrics.iterations) });
  }
  if (metrics.toolCalls !== undefined) {
    details.push({ label: 'Tool calls', value: String(metrics.toolCalls) });
  }
  if (metrics.observations !== undefined && metrics.observations > 0) {
    details.push({ label: 'Observations', value: String(metrics.observations) });
  }
  if (metrics.modelCalls !== undefined && metrics.modelCalls > 0) {
    details.push({ label: 'Model calls', value: String(metrics.modelCalls) });
  }
  if (metrics.delegations !== undefined && metrics.delegations > 0) {
    details.push({ label: 'Delegations', value: String(metrics.delegations) });
  }
  if (metrics.replans !== undefined && metrics.replans > 0) {
    details.push({ label: 'Replans', value: String(metrics.replans) });
  }
  details.push(...lifecycleDetails(createdAt, completedAt));
  return details;
}

/**
 * Lifecycle timestamps from the durable record only. A record written before
 * v52 — or a non-terminal execution — has no completed_at, and the UI must
 * omit it instead of substituting updated_at or the current clock.
 */
function lifecycleDetails(createdAt?: string | null, completedAt?: string | null): DetailItem[] {
  const details: DetailItem[] = [];
  const submitted = formatTimestamp(createdAt);
  if (submitted) details.push({ label: 'Submitted', value: submitted });
  const finished = formatTimestamp(completedAt);
  if (finished) details.push({ label: 'Finished', value: finished });
  return details;
}

function formatTimestamp(value: string | null | undefined): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return undefined;
  // Deterministic UTC rendering of the stored value; no local timezone shift.
  return `${date.toISOString().replace('T', ' ').slice(0, 16)} UTC`;
}

function durableUnavailableCopy(state: string, hasDurableSynthesis: boolean): {
  title: string;
  description: string;
  hint: string;
} {
  const hint =
    'Persisted source records are listed separately when the execution captured them.';
  if (state === 'completed') {
    if (hasDurableSynthesis) {
      return {
        title: 'Execution result text unavailable',
        description:
          'This execution completed. Its validated synthesis, captured source records and quality verdict are retained in the durable execution record. Only the raw execution result text is process-local and is not retained after a restart.',
        hint,
      };
    }
    return {
      title: 'Synthesis and result text unavailable',
      description:
        'This execution completed, but no validated synthesis was persisted for it. The durable step states and captured source records remain available.',
      hint,
    };
  }
  if (hasDurableSynthesis) {
    return {
      title: 'Execution result text unavailable',
      description:
        'This execution is no longer running. Its validated synthesis, captured source records and quality verdict are retained in the durable execution record; only the raw result text is process-local.',
      hint,
    };
  }
  return {
    title: 'Synthesis and result text unavailable',
    description:
      'This execution is no longer running, and no validated synthesis was persisted for it.',
    hint,
  };
}

function stepStatusVariant(status: ResearchStep['status']): import('../primitives').BadgeVariant {
  switch (status) {
    case 'completed':
      return 'success';
    case 'running':
      return 'info';
    case 'ready':
      return 'warning';
    case 'blocked':
      return 'warning';
    case 'failed':
      return 'error';
    default:
      return 'error';
  }
}

function stepStatusLabel(status: ResearchStep['status']): string {
  switch (status) {
    case 'completed':
      return 'Completed';
    case 'running':
      return 'Running';
    case 'ready':
      return 'Ready';
    case 'blocked':
      return 'Blocked';
    case 'failed':
      return 'Failed';
    default:
      return status;
  }
}

/** CSS class for a durable ledger state, including states the trace omits. */
function durableStepClassName(status: DurableResearchStepStatus): string {
  switch (status) {
    case 'completed':
      return 'step-completed';
    case 'executing':
    case 'observing':
      return 'step-running';
    case 'failed':
    case 'cancelled':
      return 'step-failed';
    default:
      return 'step-ready';
  }
}

function durableStepStatusVariant(
  status: DurableResearchStepStatus
): import('../primitives').BadgeVariant {
  switch (status) {
    case 'completed':
      return 'success';
    case 'executing':
    case 'observing':
      return 'info';
    case 'blocked':
    case 'pending':
    case 'ready':
      return 'warning';
    case 'failed':
    case 'cancelled':
      return 'error';
    default:
      return 'default';
  }
}

function durableStepStatusLabel(status: DurableResearchStepStatus): string {
  switch (status) {
    case 'pending':
      return 'Pending';
    case 'ready':
      return 'Ready';
    case 'executing':
      return 'Executing';
    case 'observing':
      return 'Observing';
    case 'completed':
      return 'Completed';
    case 'failed':
      return 'Failed';
    case 'blocked':
      return 'Blocked';
    case 'cancelled':
      return 'Cancelled';
    default:
      return status;
  }
}

function emptyState(
  status: string | undefined,
  errorMessage: string | undefined
): { title: string; description: string } {
  if (!status) {
    return {
      title: 'No research result yet',
      description: 'Submit a research query to see its result.',
    };
  }

  const messages: Record<string, { title: string; description: string }> = {
    pending: { title: 'Research queued', description: 'Waiting for execution to begin.' },
    running: { title: 'Research in progress', description: 'The agent is gathering sources.' },
    failed: { title: 'Research failed', description: 'An error occurred during execution.' },
    cancelled: { title: 'Research cancelled', description: 'The execution was cancelled.' },
    timed_out: { title: 'Research timed out', description: 'The execution exceeded its time limit.' },
    unknown: { title: 'Outcome unknown', description: 'The execution result could not be determined.' },
    completed: {
      title: 'No research output',
      description: 'The execution completed without producing output.',
    },
  };

  const message = messages[status] ?? {
    title: 'No research output',
    description: 'The execution did not produce output.',
  };

  return {
    title: message.title,
    description: errorMessage || message.description,
  };
}

function titleForKind(kind: string): string {
  if (kind === 'structured') return 'Structured research result';
  if (kind === 'text') return 'Research output';
  return 'Research result';
}

function statusLabel(status: string): string {
  switch (status) {
    case 'pending':
      return 'Queued';
    case 'running':
      return 'Running';
    case 'completed':
      return 'Completed';
    case 'failed':
      return 'Failed';
    case 'cancelled':
      return 'Cancelled';
    case 'timed_out':
      return 'Timed out';
    case 'unknown':
      return 'Unknown';
    default:
      return status;
  }
}

function failureTitle(status: string): string {
  switch (status) {
    case 'failed':
      return 'Research failed';
    case 'cancelled':
      return 'Research cancelled';
    case 'timed_out':
      return 'Research timed out';
    default:
      return 'Research did not complete';
  }
}

function failureDescription(status: string): string {
  switch (status) {
    case 'failed':
      return 'The execution did not complete successfully.';
    case 'cancelled':
      return 'The execution was cancelled before producing a result.';
    case 'timed_out':
      return 'The execution exceeded its time limit.';
    default:
      return 'The execution outcome could not be determined.';
  }
}

function structuredSummary(countValue: string): string {
  const count = Number(countValue);
  if (Number.isFinite(count) && Number.isInteger(count) && count >= 0) {
    if (count === 0) return 'The structured execution output contained no source records.';
    return `The execution returned ${count} source record${count === 1 ? '' : 's'}. They are listed in Sources & Evidence.`;
  }
  return 'The execution returned a structured web search result. Source records are listed in Sources & Evidence.';
}

function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return 'Unknown';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const seconds = ms / 1000;
  return `${seconds >= 10 ? Math.round(seconds) : seconds.toFixed(1)} s`;
}

/**
 * Render plain text without interpreting it as HTML or Markdown. Paragraphs
 * and list items use `white-space: pre-wrap`, so single line breaks survive.
 */
function formatOutput(text: string): React.ReactNode {
  const blocks = text.replace(/\r\n/g, '\n').split(/\n\s*\n/);

  return blocks.map((block, index) => {
    const trimmed = block.trim();
    if (!trimmed) return null;

    const lines = trimmed.split('\n');
    if (lines.every((line) => /^\s*[-*]\s+/.test(line))) {
      return (
        <ul key={index} className={styles.list}>
          {lines.map((line, lineIndex) => (
            <li key={lineIndex}>{line.replace(/^\s*[-*]\s+/, '')}</li>
          ))}
        </ul>
      );
    }

    if (lines.every((line) => /^\s*\d+[.)]\s+/.test(line))) {
      return (
        <ol key={index} className={styles.list}>
          {lines.map((line, lineIndex) => (
            <li key={lineIndex}>{line.replace(/^\s*\d+[.)]\s+/, '')}</li>
          ))}
        </ol>
      );
    }

    return (
      <p key={index} className={styles.paragraph}>
        {trimmed}
      </p>
    );
  });
}
