import type { DurableExecution, DurableExecutionState } from '../../types/api';

const ELIGIBLE_STATES = new Set<DurableExecutionState>([
  'completed',
  'failed',
  'cancelled',
  'timed_out',
  'unknown',
]);

/** A provisional approval pause is not a settled comparison snapshot. */
export function isComparisonEligible(execution: DurableExecution): boolean {
  if (execution.state === 'failed' && execution.error_class === 'APPROVAL_REQUIRED') {
    return false;
  }
  return ELIGIBLE_STATES.has(execution.state);
}
