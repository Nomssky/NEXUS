/**
 * Legacy auth-storage migration.
 *
 * The control-plane key is memory-only. Older builds persisted it inside the
 * `nexus-auth` record, sometimes before actor credentials existed. This helper
 * removes that field regardless of whether the record is otherwise usable, so
 * a legacy key cannot remain in browser storage just because the actor
 * credential is empty or absent.
 */

export interface StrippedAuthStorage {
  /** The parsed record after removing `controlKey`, or null if unparseable. */
  parsed: Record<string, unknown> | null;
  /** Replacement JSON when a legacy key was present; null when no rewrite is needed. */
  serialized: string | null;
}

export function stripPersistedControlKey(raw: string): StrippedAuthStorage {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return { parsed: null, serialized: null };
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return { parsed: null, serialized: null };
  }
  const record = parsed as Record<string, unknown>;
  if (!Object.prototype.hasOwnProperty.call(record, 'controlKey')) {
    return { parsed: record, serialized: null };
  }
  const { controlKey: _legacyControlKey, ...rest } = record;
  return { parsed: rest, serialized: JSON.stringify(rest) };
}
