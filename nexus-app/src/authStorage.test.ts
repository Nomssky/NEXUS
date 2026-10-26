import { describe, expect, it } from 'vitest';
import { stripPersistedControlKey } from './authStorage';

describe('legacy auth storage', () => {
  it('strips a persisted control key even when actor credentials are empty', () => {
    const raw = JSON.stringify({
      actorId: 'nx:human:bootstrap',
      credential: '',
      businessId: 'default',
      controlKey: 'legacy-operator-secret',
    });

    const result = stripPersistedControlKey(raw);

    expect(result.parsed).toEqual({
      actorId: 'nx:human:bootstrap',
      credential: '',
      businessId: 'default',
    });
    expect(result.serialized).not.toBeNull();
    expect(result.serialized).not.toContain('legacy-operator-secret');
    expect(result.serialized).toContain('nx:human:bootstrap');
  });

  it('strips a persisted control key when the record is not authenticated', () => {
    const result = stripPersistedControlKey(
      JSON.stringify({ controlKey: 'orphaned-operator-secret' })
    );

    expect(result.parsed).toEqual({});
    expect(result.serialized).not.toContain('orphaned-operator-secret');
  });

  it('does not rewrite records that never contained a control key', () => {
    const result = stripPersistedControlKey(
      JSON.stringify({ actorId: 'nx:human:a', credential: 'c', businessId: 'default' })
    );

    expect(result.serialized).toBeNull();
    expect(result.parsed).toMatchObject({ actorId: 'nx:human:a' });
  });

  it('ignores malformed storage instead of throwing', () => {
    expect(stripPersistedControlKey('{not-json')).toEqual({
      parsed: null,
      serialized: null,
    });
    expect(stripPersistedControlKey('[]')).toEqual({ parsed: null, serialized: null });
  });
});
