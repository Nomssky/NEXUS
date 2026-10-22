/**
 * ResearchOutput rendering tests.
 *
 * Rendered with react-dom/server so the assertions cover real text escaping,
 * list formatting, and the absence of linkification without a DOM test stack.
 */

import React from 'react';
import { describe, it, expect } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ResearchOutput } from './ResearchOutput';

const COMPLETED_SUMMARY =
  'state=completed | objective=search the web for nexus runtime capabilities | agent=probe-analyst | ' +
  'iterations=2 tool_calls=1 delegations=0 model_calls=2 replans=0 observations=1 | ' +
  'result=objective pursued deterministically';

function render(props: React.ComponentProps<typeof ResearchOutput>): string {
  return renderToStaticMarkup(<ResearchOutput {...props} />);
}

describe('ResearchOutput', () => {
  it('renders the parsed result, grounded metrics, and raw summary', () => {
    const markup = render({ output: COMPLETED_SUMMARY, status: 'completed', durationMs: 50 });

    expect(markup).toContain('Research result');
    expect(markup).toContain('objective pursued deterministically');
    expect(markup).toContain('search the web for nexus runtime capabilities');
    expect(markup).toContain('probe-analyst');
    expect(markup).toContain('50 ms');
    expect(markup).toContain('Tool calls');
    expect(markup).toContain('<dd>1</dd>');
    expect(markup).toContain('Execution summary');
    expect(markup).toContain('state=completed');
    expect(markup).toContain('Completed');
  });

  it('renders failed executions from authoritative status and error data', () => {
    const markup = render({
      output: 'state=failed',
      status: 'failed',
      error: { code: 'TOOL_FAILURE', category: 'DEPENDENCY_FAILURE', message: 'search backend unavailable' },
    });

    expect(markup).toContain('Research failed');
    expect(markup).toContain('TOOL_FAILURE');
    expect(markup).toContain('search backend unavailable');
    expect(markup).not.toContain('Completed');
  });

  it('renders the empty state without inventing a result', () => {
    expect(render({})).toContain('No research result yet');
    expect(render({ status: 'completed' })).toContain('No research output');
    expect(render({ status: 'timed_out' })).toContain('Research timed out');
  });

  it('renders malformed JSON as text rather than structured output', () => {
    const malformed = '{"sources": "1. A | https://example.com", "count":';
    const markup = render({ output: malformed, status: 'completed' });

    expect(markup).toContain('sources');
    expect(markup).toContain('count');
    expect(markup).not.toContain('Research Sources');
  });

  it('renders the documented structured web.search envelope as a summary only', () => {
    const output = JSON.stringify({
      sources: '1. Example | https://example.com/a | Example | A snippet',
      count: '1',
    });
    const markup = render({ output, status: 'completed' });

    expect(markup).toContain('Structured research result');
    expect(markup).toContain('1 source record');
    expect(markup).toContain('Sources &amp; Evidence');
    expect(markup).not.toContain('<a ');
  });

  it('escapes untrusted result text and never renders it as HTML', () => {
    const markup = render({
      output:
        'state=completed | objective=test | agent=analyst | iterations=1 tool_calls=0 | ' +
        'result=<script>alert(1)</script> <img src=x onerror=alert(2)>',
      status: 'completed',
    });

    expect(markup).not.toContain('<script>');
    expect(markup).not.toContain('<img');
    expect(markup).toContain('&lt;script&gt;');
  });

  it('does not linkify URLs found in model-generated result prose', () => {
    const markup = render({
      output:
        'state=completed | objective=test | agent=analyst | iterations=1 tool_calls=0 | ' +
        'result=See https://unverified.example/claim for details',
      status: 'completed',
    });

    expect(markup).toContain('https://unverified.example/claim');
    expect(markup).not.toContain('<a href="https://unverified.example/claim"');
  });

  it('preserves list formatting from result text', () => {
    const markup = render({
      output:
        'state=completed | objective=test | agent=analyst | iterations=1 tool_calls=0 | ' +
        'result=Findings:\n\n- first item\n- second item',
      status: 'completed',
    });

    expect(markup).toContain('<ul');
    expect(markup).toContain('first item');
    expect(markup).toContain('second item');
  });

  it('renders planned steps and keeps the synthesis distinct from source records', () => {
    const synthesis = 'objective pursued deterministically';
    const markup = render({
      output: synthesis,
      status: 'completed',
      steps: [
        { stepId: 's1', status: 'completed', actionType: 'tool_call' },
        { stepId: 's2', status: 'failed' },
      ],
      findings: {
        synthesis,
        sources: [
          {
            index: 1,
            title: 'A',
            url: 'https://a.example',
            source: 'a.example',
            snippet: 's',
            linkable: true,
          },
        ],
        evidencePresent: true,
        partial: true,
      },
    });

    expect(markup).toContain('Research steps');
    expect(markup).toContain('s1');
    expect(markup).toContain('tool_call');
    expect(markup).toContain('Failed');
    expect(markup).toContain('Synthesis');
    // The agent result prose is rendered once, not once as synthesis and
    // again as the legacy result block.
    expect(markup.match(/objective pursued deterministically/g)).toHaveLength(1);
    expect(markup).toContain('Findings are partial');
    expect(markup).toContain('1 source record');
  });

  it('renders typed evidence-backed synthesis with authoritative citation titles and URLs', () => {
    const markup = render({
      output: COMPLETED_SUMMARY,
      status: 'completed',
      findings: {
        typedSynthesis: {
          status: 'complete',
          summary: 'The captured sources support the answer.',
          evidenceBacked: [
            {
              statement: 'The runtime decides and the model proposes.',
              citations: [
                {
                  sourceId: 'src-1',
                  source: {
                    index: 1,
                    id: 'src-1',
                    title: 'Authoritative title',
                    url: 'https://docs.example/nexus',
                    source: 'docs.example',
                    snippet: 'captured',
                    linkable: true,
                  },
                },
              ],
            },
          ],
          inferences: ['An inference that is not evidence.'],
          conflicts: [],
          unresolvedQuestions: [],
        },
        sources: [
          {
            index: 1,
            id: 'src-1',
            title: 'Authoritative title',
            url: 'https://docs.example/nexus',
            source: 'docs.example',
            snippet: 'captured',
            linkable: true,
          },
        ],
        evidencePresent: true,
        partial: false,
      },
    });

    expect(markup).toContain('Typed research synthesis');
    expect(markup).toContain('data-synthesis-status="complete"');
    expect(markup).toContain('Evidence-backed findings');
    expect(markup).toContain('data-source-id="src-1"');
    expect(markup).toContain('href="https://docs.example/nexus"');
    expect(markup).toContain('Authoritative title');
    expect(markup).toContain('https://docs.example/nexus');
    expect(markup).toContain('not evidence');
    // The typed answer suppresses the legacy result prose.
    expect(markup).not.toContain('objective pursued deterministically');
  });

  it('renders partial and inconclusive typed synthesis honestly', () => {
    const partial = render({
      output: COMPLETED_SUMMARY,
      status: 'completed',
      findings: {
        typedSynthesis: {
          status: 'partial',
          summary: 'Only one source was captured.',
          evidenceBacked: [
            {
              statement: 'A grounded statement.',
              citations: [
                {
                  sourceId: 'src-1',
                  source: {
                    index: 1,
                    id: 'src-1',
                    title: 'One',
                    url: 'https://one.example',
                    source: 'one',
                    snippet: 's',
                    linkable: true,
                  },
                },
              ],
            },
          ],
          inferences: [],
          conflicts: [],
          unresolvedQuestions: ['Which step did not complete?'],
        },
        sources: [],
        evidencePresent: true,
        partial: true,
      },
    });
    expect(partial).toContain('data-synthesis-status="partial"');
    expect(partial).toContain('This answer is partial');
    expect(partial).toContain('Which step did not complete?');

    const inconclusive = render({
      output: COMPLETED_SUMMARY,
      status: 'completed',
      findings: {
        typedSynthesis: {
          status: 'inconclusive',
          summary: 'No source records were captured.',
          evidenceBacked: [],
          inferences: [],
          conflicts: [],
          unresolvedQuestions: ['What evidence is missing?'],
        },
        sources: [],
        evidencePresent: true,
        partial: false,
      },
    });
    expect(inconclusive).toContain('data-synthesis-status="inconclusive"');
    expect(inconclusive).toContain('No evidence-backed conclusion was reached');
    expect(inconclusive).toContain('What evidence is missing?');
    expect(inconclusive).not.toContain('Evidence-backed findings');
  });

  it('withholds a rejected typed synthesis and does not fall back to prose', () => {
    const markup = render({
      output: COMPLETED_SUMMARY,
      status: 'completed',
      findings: {
        synthesisRejected: 'invalid',
        sources: [],
        evidencePresent: true,
        partial: false,
      },
    });

    expect(markup).toContain('Synthesis withheld');
    expect(markup).toContain('did not pass this page’s citation checks');
    expect(markup).not.toContain('objective pursued deterministically');
    expect(markup).not.toContain('Evidence-backed findings');
  });

  it('renders an unsafe citation URL as unlinked authoritative text', () => {
    const markup = render({
      output: COMPLETED_SUMMARY,
      status: 'completed',
      findings: {
        typedSynthesis: {
          status: 'complete',
          summary: 'Grounded.',
          evidenceBacked: [
            {
              statement: 'Claim.',
              citations: [
                {
                  sourceId: 'src-1',
                  source: {
                    index: 1,
                    id: 'src-1',
                    title: 'Unsafe',
                    url: 'javascript:alert(1)',
                    source: 'unsafe',
                    snippet: 's',
                    linkable: false,
                  },
                },
              ],
            },
          ],
          inferences: [],
          conflicts: [],
          unresolvedQuestions: [],
        },
        sources: [],
        evidencePresent: true,
        partial: false,
      },
    });

    expect(markup).toContain('javascript:alert(1)');
    expect(markup).not.toContain('href="javascript');
  });

  it('omits the step section when no plan steps were observed', () => {
    const markup = render({ output: COMPLETED_SUMMARY, status: 'completed', steps: [] });

    expect(markup).not.toContain('Research steps');
  });

  it('renders a recovered durable plan distinctly from process-local trace steps', () => {
    const markup = render({
      output: COMPLETED_SUMMARY,
      status: 'completed',
      steps: [{ stepId: 'trace-s1', status: 'running', actionType: 'tool_call' }],
      durablePlan: { plan_id: 'exec-1', version: 2 },
      durableSteps: [
        {
          stepId: 's1',
          status: 'completed',
          dependencies: [],
          observationId: 'obs-1',
          sources: [],
        },
        {
          stepId: 's2',
          status: 'completed',
          dependencies: ['s1'],
          observationId: 'obs-2',
          sources: [],
        },
      ],
    });

    expect(markup).toContain('Research plan');
    expect(markup).toContain('Durable record');
    expect(markup).toContain('exec-1');
    expect(markup).toContain('version 2');
    expect(markup).toContain('data-step-id="s1"');
    expect(markup).toContain('after s1');
    // The live trace is not presented as durable plan state.
    expect(markup).not.toContain('Research steps');
    expect(markup).not.toContain('Live trace');
    expect(markup).not.toContain('trace-s1');
  });

  describe('durable lifecycle timestamps (recovery detail read)', () => {
    const rendered = render({
      output: 'state=completed | objective=Search the web for nexus runtime capabilities | agent=analyst | tool_calls=1',
      status: 'completed',
      durationMs: 3120,
    });

    it('renders Submitted from created_at and Finished from completed_at, never updated_at', () => {
      const markup = render({
        output: 'state=completed | objective=Search the web for nexus runtime capabilities | agent=analyst | tool_calls=1',
        status: 'completed',
        durationMs: 3120,
        createdAt: '2026-10-20T10:00:00Z',
        completedAt: '2026-10-20T10:05:30Z',
      });

      expect(markup).toContain('Submitted');
      expect(markup).toContain('2026-10-20 10:00 UTC');
      expect(markup).toContain('Finished');
      expect(markup).toContain('2026-10-20 10:05 UTC');
      // updated_at must never be passed off as completion time.
      expect(markup).not.toContain('updated_at');
    });

    it('renders Finished only when completed_at is available', () => {
      const markup = render({
        output: 'state=completed | objective=Search the web | agent=analyst',
        status: 'completed',
        createdAt: '2026-10-20T10:00:00Z',
      });

      expect(markup).toContain('Submitted');
      expect(markup).toContain('2026-10-20 10:00 UTC');
      expect(markup).not.toContain('Finished');
    });

    it('omits both lifecycle labels when neither durable timestamp exists', () => {
      const markup = render({
        output: 'state=completed | objective=Search the web | agent=analyst',
        status: 'completed',
      });

      expect(markup).toContain('Research result');
      expect(markup).not.toContain('Submitted');
      expect(markup).not.toContain('Finished');
    });

    it('renders an honest unavailable-result state for a durable terminal record instead of claiming output', () => {
      const markup = render({
        output: undefined,
        status: undefined,
        durableState: 'completed',
        createdAt: '2026-10-20T10:00:00Z',
        completedAt: '2026-10-20T10:05:30Z',
      });

      expect(markup).toContain('Synthesis and result text unavailable');
      expect(markup).toContain('no validated synthesis was persisted for it');
      expect(markup).toContain('Durable execution record.');
      expect(markup).toContain('2026-10-20 10:00 UTC');
      expect(markup).toContain('2026-10-20 10:05 UTC');
      expect(markup).not.toContain('No research output');
      expect(markup).not.toContain('objective pursued deterministically');
    });

    it('shows persisted source evidence through the durable record alone', () => {
      const markup = render({
        output: undefined,
        status: undefined,
        durableState: 'completed',
        createdAt: '2026-10-20T10:00:00Z',
        completedAt: '2026-10-20T10:05:30Z',
      });

      expect(markup).toContain('Persisted source records are listed separately');
    });

    it('renders the recovered durable plan while result text remains unavailable', () => {
      const markup = render({
        output: undefined,
        status: undefined,
        durableState: 'completed',
        createdAt: '2026-10-20T10:00:00Z',
        completedAt: '2026-10-20T10:05:30Z',
        durablePlan: { plan_id: 'exec-1', version: 1 },
        durableSteps: [
          { stepId: 's1', status: 'completed', dependencies: [], sources: [] },
          {
            stepId: 's2',
            status: 'completed',
            dependencies: ['s1'],
            sources: [],
          },
        ],
      });

      expect(markup).toContain('Synthesis and result text unavailable');
      expect(markup).toContain('Research plan');
      expect(markup).toContain('data-step-id="s2"');
      expect(markup).toContain('2026-10-20 10:05 UTC');
      expect(markup).not.toContain('data-synthesis-status');
      expect(markup).not.toContain('Evidence-backed findings');
    });
  });

  describe('research quality evaluation', () => {
    const evaluation = {
      version: 1 as const,
      method: 'deterministic_v1' as const,
      execution_id: 'exec-1',
      evidence_version: 2,
      evidence_count: 1,
      evidence_fingerprint: 'a'.repeat(64),
      synthesis_status: 'complete' as const,
      synthesis_fingerprint: 'b'.repeat(64),
      status: 'warnings' as const,
      dimensions: [
        { id: 'evidence_coverage' as const, status: 'pass' as const },
        { id: 'citation_integrity' as const, status: 'pass' as const },
        { id: 'relevance' as const, status: 'warn' as const },
        { id: 'source_quality' as const, status: 'pass' as const },
        { id: 'source_diversity' as const, status: 'pass' as const },
        { id: 'conflict_handling' as const, status: 'not_measurable' as const },
        { id: 'uncertainty_handling' as const, status: 'pass' as const },
        { id: 'plan_completeness' as const, status: 'pass' as const },
      ],
      findings: [
        {
          code: 'source_no_query_overlap',
          dimension: 'relevance' as const,
          severity: 'warning' as const,
          message: 'Source text has no meaningful lexical overlap with the objective.',
          source_id: 'src-1',
        },
      ],
      limitations: ['Citation integrity proves reference resolution only.'],
    };

    it('renders the deterministic verdict next to a live result', () => {
      const markup = render({
        output: COMPLETED_SUMMARY,
        status: 'completed',
        evaluation,
      });

      expect(markup).toContain('aria-label="Research quality evaluation"');
      expect(markup).toContain('data-evaluation-status="warnings"');
      expect(markup).toContain('Quality checks');
      expect(markup).toContain('data-dimension="relevance"');
      expect(markup).toContain('Source text has no meaningful lexical overlap with the objective.');
      expect(markup).not.toContain('Recorded when this execution completed');
    });
    it('renders the persisted verdict when the synthesis is gone after a restart', () => {
      const markup = render({
        output: undefined,
        status: undefined,
        durableState: 'completed',
        createdAt: '2026-10-20T10:00:00Z',
        completedAt: '2026-10-20T10:05:30Z',
        evaluation,
      });

      expect(markup).toContain('aria-label="Research quality evaluation"');
      expect(markup).toContain('Recorded when this execution completed');
      expect(markup).toContain('Synthesis and result text unavailable');
      expect(markup).not.toContain('Checks passed');
    });
  });

  describe('durable research synthesis', () => {
    const durableSynthesis = {
      status: 'complete' as const,
      summary: 'Synthesized from the captured source.',
      evidenceBacked: [
        {
          statement: 'The runtime decides and the model proposes.',
          citations: [
            {
              sourceId: 'src-1',
              source: {
                index: 1,
                id: 'src-1',
                title: 'Nexus capability boundary',
                url: 'https://docs.example/nexus',
                source: 'docs.example',
                snippet: 'The runtime decides; the model proposes.',
                linkable: true,
              },
            },
          ],
        },
      ],
      inferences: [],
      conflicts: [],
      unresolvedQuestions: [],
    };

    it('renders persisted durable synthesis distinctly from live synthesis', () => {
      const markup = render({
        output: undefined,
        status: undefined,
        durableState: 'completed',
        createdAt: '2026-10-20T10:00:00Z',
        completedAt: '2026-10-20T10:05:30Z',
        durableSynthesis,
      });

      expect(markup).toContain('aria-label="Durable research synthesis"');
      expect(markup).toContain('data-provenance="durable"');
      expect(markup).toContain('Durable synthesis');
      expect(markup).toContain('Durable record');
      expect(markup).toContain('Synthesized from the captured source.');
      expect(markup).toContain('The runtime decides and the model proposes.');
      expect(markup).toContain('docs.example/nexus');
      expect(markup).toContain('Nexus capability boundary');
      expect(markup).toContain('docs.example · src-1');
      expect(markup).not.toContain('objective pursued deterministically');
    });

    it('renders durable synthesis alongside live output', () => {
      const markup = render({
        output: COMPLETED_SUMMARY,
        status: 'completed',
        durableSynthesis,
      });

      expect(markup).toContain('aria-label="Durable research synthesis"');
      expect(markup).toContain('Synthesized from the captured source.');
      expect(markup).toContain('objective pursued deterministically');
    });

    it('does not duplicate the answer when the live typed synthesis is present', () => {
      const markup = render({
        output: COMPLETED_SUMMARY,
        status: 'completed',
        durableSynthesis,
        findings: {
          sources: durableSynthesis.evidenceBacked[0].citations.map((c) => c.source),
          evidencePresent: true,
          partial: false,
          typedSynthesis: {
            status: 'complete',
            summary: 'Live typed synthesis summary.',
            evidenceBacked: [
              {
                statement: 'Live typed statement.',
                citations: [
                  {
                    sourceId: 'src-1',
                    source: durableSynthesis.evidenceBacked[0].citations[0].source,
                  },
                ],
              },
            ],
            inferences: [],
            conflicts: [],
            unresolvedQuestions: [],
          },
        },
      });

      expect(markup).toContain('aria-label="Typed research synthesis"');
      expect(markup).toContain('Live typed statement.');
      // The durable copy is the recovery path, not a second rendering of the
      // same answer while the live one is on screen.
      expect(markup).not.toContain('aria-label="Durable research synthesis"');
      expect(markup).not.toContain('Durable record');
    });

    it('renders partial durable synthesis with unresolved questions', () => {
      const markup = render({
        output: undefined,
        status: undefined,
        durableState: 'completed',
        createdAt: '2026-10-20T10:00:00Z',
        completedAt: '2026-10-20T10:05:30Z',
        durableSynthesis: {
          ...durableSynthesis,
          status: 'partial' as const,
          unresolvedQuestions: ['Why did step s2 fail?'],
        },
      });

      expect(markup).toContain('This answer is partial');
      expect(markup).toContain('Why did step s2 fail?');
    });
  });
});
