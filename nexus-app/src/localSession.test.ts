/**
 * Local session config validation tests.
 *
 * Verifies `parseLocalSessionConfig` accepts only well-formed launcher-provided
 * session records and rejects anything malformed, so the local desktop bootstrap
 * flow never establishes a false authenticated state.
 */
import { describe, expect, it } from 'vitest';
import { parseLocalSessionConfig, type LocalSessionConfig } from './localSession';

const validConfig: LocalSessionConfig = {
  apiBase: 'http://127.0.0.1:8081/api/v1',
  actorId: 'nx:human:bootstrap',
  credential: 'some-bootstrap-credential',
  businessId: 'default',
};

describe('parseLocalSessionConfig', () => {
  it('accepts a well-formed local session config', () => {
    const result = parseLocalSessionConfig(JSON.stringify(validConfig));
    expect(result).toEqual(validConfig);
  });

  it('rejects malformed JSON', () => {
    expect(parseLocalSessionConfig('{not-json')).toBeNull();
    expect(parseLocalSessionConfig('')).toBeNull();
    expect(parseLocalSessionConfig('[]')).toBeNull();
    expect(parseLocalSessionConfig('null')).toBeNull();
  });

  it('rejects missing apiBase', () => {
    const bad = { ...validConfig } as Record<string, unknown>;
    delete bad.apiBase;
    expect(parseLocalSessionConfig(JSON.stringify(bad))).toBeNull();
  });

  it('rejects empty apiBase', () => {
    expect(
      parseLocalSessionConfig(
        JSON.stringify({ ...validConfig, apiBase: '' })
      )
    ).toBeNull();
  });

  it('rejects missing actorId', () => {
    const bad = { ...validConfig } as Record<string, unknown>;
    delete bad.actorId;
    expect(parseLocalSessionConfig(JSON.stringify(bad))).toBeNull();
  });

  it('rejects missing credential', () => {
    const bad = { ...validConfig } as Record<string, unknown>;
    delete bad.credential;
    expect(parseLocalSessionConfig(JSON.stringify(bad))).toBeNull();
  });

  it('rejects missing businessId', () => {
    const bad = { ...validConfig } as Record<string, unknown>;
    delete bad.businessId;
    expect(parseLocalSessionConfig(JSON.stringify(bad))).toBeNull();
  });

  it('rejects non-object payloads', () => {
    expect(parseLocalSessionConfig(JSON.stringify('string'))).toBeNull();
    expect(parseLocalSessionConfig(JSON.stringify(42))).toBeNull();
    expect(parseLocalSessionConfig(JSON.stringify(true))).toBeNull();
  });

  it('rejects records with non-string fields', () => {
    expect(
      parseLocalSessionConfig(
        JSON.stringify({ ...validConfig, apiBase: 123 })
      )
    ).toBeNull();
    expect(
      parseLocalSessionConfig(
        JSON.stringify({ ...validConfig, actorId: null })
      )
    ).toBeNull();
  });
});
