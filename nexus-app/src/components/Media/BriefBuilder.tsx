/**
 * BriefBuilder - a structured creative-brief editor.
 *
 * The brief lives only in client state. It is never persisted to durable
 * memory, never sent to the backend as a structured record, and never
 * presented as a generated asset. The user may convert the brief into an
 * execution objective (which runs the governed research/brief path) with
 * an explicit action; otherwise the brief only lives in this session.
 *
 * This is an honest creative-preparation tool: it helps the user organize
 * their thinking and optionally run the backend-supported brief generation
 * or research task. It does not imply that NEXUS can generate media.
 */

import React, { useState, useCallback } from 'react';
import { Card, Button, Badge } from '../primitives';
import { Input, Textarea } from '../primitives';
import { useSubmitIntelligence } from '../../api/hooks';
import styles from './BriefBuilder.module.css';

interface BriefBuilderProps {
  businessId: string;
  actorId: string;
  onRunBrief?: (objective: string, executionId: string) => void;
}

interface BriefFields {
  objective: string;
  audience: string;
  visualDirection: string;
  format: string;
  references: string;
  constraints: string;
}

const initialBrief: BriefFields = {
  objective: '',
  audience: '',
  visualDirection: '',
  format: '',
  references: '',
  constraints: '',
};

function briefToObjective(brief: BriefFields): string {
  const parts: string[] = [];
  if (brief.objective) parts.push(`Objective: ${brief.objective}`);
  if (brief.audience) parts.push(`Audience: ${brief.audience}`);
  if (brief.visualDirection) parts.push(`Visual direction: ${brief.visualDirection}`);
  if (brief.format) parts.push(`Target format: ${brief.format}`);
  if (brief.references) parts.push(`References: ${brief.references}`);
  if (brief.constraints) parts.push(`Constraints: ${brief.constraints}`);
  return `Creative brief — ${parts.join('. ')}`;
}

const FIELDS: Array<{ key: keyof BriefFields; label: string; placeholder: string; multiline: boolean }> = [
  { key: 'objective', label: 'Objective', placeholder: 'What should this creative work achieve?', multiline: false },
  { key: 'audience', label: 'Audience', placeholder: 'Who should it speak to?', multiline: false },
  { key: 'visualDirection', label: 'Visual direction', placeholder: 'Style, mood, tone — what should it feel like?', multiline: true },
  { key: 'format', label: 'Format', placeholder: 'Poster, short video, social tile, deck…', multiline: false },
  { key: 'references', label: 'References', placeholder: 'Links or descriptions of reference works (optional)', multiline: true },
  { key: 'constraints', label: 'Constraints', placeholder: 'Budget, time, brand, technical…', multiline: true },
];

export const BriefBuilder: React.FC<BriefBuilderProps> = ({ businessId, actorId, onRunBrief }) => {
  const [brief, setBrief] = useState<BriefFields>(initialBrief);
  const [edited, setEdited] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const submitMutation = useSubmitIntelligence({
    onSuccess: (response) => {
      setError(undefined);
      onRunBrief?.(briefToObjective(brief), response.execution_id);
    },
    onError: (err: Error) => setError(err.message),
  });

  const setField = useCallback((key: keyof BriefFields, value: string) => {
    setBrief((prev) => ({ ...prev, [key]: value }));
    setEdited(true);
  }, []);

  const isEmpty = isBriefEmpty(brief);
  const filledCount = FIELDS.filter((f) => brief[f.key].trim().length > 0).length;

  const handleRun = useCallback(() => {
    if (isEmpty || submitMutation.isPending) return;
    submitMutation.mutate({
      business_id: businessId,
      actor_id: actorId,
      objective: { description: briefToObjective(brief) },
    });
  }, [isEmpty, submitMutation.isPending, brief, businessId, actorId, submitMutation]);

  return (
    <Card variant="default" padding="lg" className={styles.card}>
      <div className={styles.header}>
        <h3 className={styles.title}>Creative Brief</h3>
        <Badge variant="default" size="sm">
          {filledCount}/{FIELDS.length} fields
        </Badge>
      </div>
      <p className={styles.intro}>
        A brief is a working document — not a generated asset. Fill in the fields
        that matter to you; NEXUS can run the result as a task with your consent.
      </p>
      <div className={styles.fields}>
        {FIELDS.map((f) =>
          f.multiline ? (
            <Textarea
              key={f.key}
              label={f.label}
              placeholder={f.placeholder}
              value={brief[f.key]}
              onChange={(e) => setField(f.key, e.target.value)}
              rows={2}
              fullWidth
            />
          ) : (
            <Input
              key={f.key}
              label={f.label}
              placeholder={f.placeholder}
              value={brief[f.key]}
              onChange={(e) => setField(f.key, e.target.value)}
              fullWidth
            />
          )
        )}
      </div>
      <div className={styles.actions}>
        <span className={styles.hint}>
          {edited ? 'Unsaved local changes' : 'Nothing yet'}
        </span>
        <Button
          variant="primary"
          size="md"
          onClick={handleRun}
          disabled={isEmpty || submitMutation.isPending}
          isLoading={submitMutation.isPending}
          aria-busy={submitMutation.isPending}
        >
          Run as research task
        </Button>
      </div>
      {error && (
        <p className={styles.errorText} role="alert">{error}</p>
      )}
    </Card>
  );
};

function isBriefEmpty(brief: BriefFields): boolean {
  return !FIELDS.some((f) => brief[f.key].trim().length > 0);
}