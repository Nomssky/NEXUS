/**
 * Local session config parsing for the single-user desktop bootstrap flow.
 *
 * The launcher writes a short-lived local-session file (served only on
 * localhost by the frontend static server) carrying the API base URL and
 * bootstrap credentials. This module parses and validates that record. Any
 * malformed or incomplete record resolves to null so the caller falls back to
 * stored auth or the login screen — never to a false authenticated state.
 */

export interface LocalSessionConfig {
  apiBase: string;
  actorId: string;
  credential: string;
  businessId: string;
}

// Parse and validate a launcher-provided local-session config. Returns null for
// any malformed or incomplete record.
export function parseLocalSessionConfig(raw: string): LocalSessionConfig | null {
  try {
    const config = JSON.parse(raw);
    if (
      config &&
      typeof config.apiBase === 'string' &&
      config.apiBase &&
      typeof config.actorId === 'string' &&
      config.actorId &&
      typeof config.credential === 'string' &&
      config.credential &&
      typeof config.businessId === 'string' &&
      config.businessId
    ) {
      return config as LocalSessionConfig;
    }
  } catch {
    // Malformed JSON: fall through to stored auth or login.
  }
  return null;
}
