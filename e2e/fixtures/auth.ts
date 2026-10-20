/**
 * Authentication abstractions for the NEXUS E2E harness.
 *
 * This module provides the dependency-injection backbone for all auth
 * concerns. Instead of free functions that reach into a NexusHandle for
 * credentials (which couples auth to process lifecycle), we define
 *
 *   - AuthStrategy: an interface for anything that can produce HTTP headers
 *     for a given auth scheme (actor identity, control-plane API key).
 *   - CredentialProvider: an interface for anything that can mint fresh
 *     credentials. The harness injects this into startNexus() so tests can
 *     control credential generation (random, deterministic, fixture-loaded)
 *     without changing the process-spawning logic.
 *
 * Both interfaces are intentionally minimal (single method) so they are
 * trivial to implement, mock, or stub in unit tests.
 */

// ---------------------------------------------------------------------------
// AuthStrategy
// ---------------------------------------------------------------------------

/**
 * An AuthStrategy produces a set of HTTP headers that authenticate a
 * request. Strategies are stateless once constructed — they hold their
 * credential material and return the same headers on every call.
 */
export interface AuthStrategy {
  /** HTTP headers to attach to a request. */
  headers(): Record<string, string>;
}

// ---------------------------------------------------------------------------
// ActorAuthStrategy  (X-Actor-ID + X-Actor-Credential)
// ---------------------------------------------------------------------------

/**
 * Header-based actor authentication: `X-Actor-ID` and `X-Actor-Credential`.
 *
 * This is the primary auth scheme for identity-scoped endpoints
 * (/api/v1/requests, /api/v1/identities, /events, …).
 */
export class ActorAuthStrategy implements AuthStrategy {
  readonly actorID: string;
  readonly credential: string;

  constructor(actorID: string, credential: string) {
    this.actorID = actorID;
    this.credential = credential;
  }

  headers(): Record<string, string> {
    return {
      'X-Actor-ID': this.actorID,
      'X-Actor-Credential': this.credential,
    };
  }

  /**
   * Return equivalent HTTP Basic credentials
   * (`Authorization: Basic base64(id:cred)`). The gateway treats both
   * forms as identical (see 01-auth.spec.ts "HTTP Basic equivalence").
   */
  toBasicAuth(): Record<string, string> {
    const encoded = Buffer.from(`${this.actorID}:${this.credential}`).toString('base64');
    return { Authorization: `Basic ${encoded}` };
  }
}

// ---------------------------------------------------------------------------
// ControlAuthStrategy  (X-API-Key)
// ---------------------------------------------------------------------------

/**
 * Control-plane authentication: `X-API-Key`.
 *
 * Used for policy CRUD and other control endpoints
 * (/api/v1/control/policies, /api/v1/control/status, …). These endpoints
 * are NOT identity-scoped — they require the bootstrap API key instead.
 */
export class ControlAuthStrategy implements AuthStrategy {
  readonly apiKey: string;

  constructor(apiKey: string) {
    this.apiKey = apiKey;
  }

  headers(): Record<string, string> {
    return { 'X-API-Key': this.apiKey };
  }
}

// ---------------------------------------------------------------------------
// CredentialProvider
// ---------------------------------------------------------------------------

/**
 * Provides fresh credentials for a newly-spawned gateway process.
 *
 * The harness calls these methods exactly once per startNexus() invocation
 * and passes the results to the child process via environment variables
 * (NEXUS_BOOTSTRAP_CREDENTIAL, NEXUS_CONTROL_API_KEY).
 */
export interface CredentialProvider {
  /** Generate a bootstrap actor credential (passed as NEXUS_BOOTSTRAP_CREDENTIAL). */
  generateActorCredential(): string;
  /** Generate a control-plane API key (passed as NEXUS_CONTROL_API_KEY). */
  generateControlKey(): string;
}

/**
 * Default CredentialProvider: generates cryptographically-random,
 * human-readable credential strings with a timestamp suffix.
 *
 * Format:
 *   actor credential → e2e-<random-base36>-<timestamp-base36>
 *   control key      → e2e-control-<random-base36>-<timestamp-base36>
 */
export class RandomCredentialProvider implements CredentialProvider {
  generateActorCredential(): string {
    return `e2e-${Math.random().toString(36).slice(2)}-${Date.now().toString(36)}`;
  }

  generateControlKey(): string {
    return `e2e-control-${Math.random().toString(36).slice(2)}-${Date.now().toString(36)}`;
  }
}
