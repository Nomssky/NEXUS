import { describe, it, expect } from 'vitest';
import {
  ActorAuthStrategy,
  ControlAuthStrategy,
  RandomCredentialProvider,
  type AuthStrategy,
  type CredentialProvider,
} from './auth';

// ---------------------------------------------------------------------------
// ActorAuthStrategy
// ---------------------------------------------------------------------------

describe('ActorAuthStrategy', () => {
  it('produces X-Actor-ID and X-Actor-Credential headers', () => {
    const strategy = new ActorAuthStrategy('nx:human:alice', 'secret-123');
    const headers = strategy.headers();

    expect(headers['X-Actor-ID']).toBe('nx:human:alice');
    expect(headers['X-Actor-Credential']).toBe('secret-123');
  });

  it('exposes actorID and credential as readonly properties', () => {
    const strategy = new ActorAuthStrategy('nx:human:bob', 'hunter2');
    expect(strategy.actorID).toBe('nx:human:bob');
    expect(strategy.credential).toBe('hunter2');
  });

  it('implements the AuthStrategy interface', () => {
    const strategy: AuthStrategy = new ActorAuthStrategy('id', 'cred');
    expect(strategy.headers()).toBeDefined();
  });

  it('produces identical headers on every call (stateless)', () => {
    const strategy = new ActorAuthStrategy('nx:human:carol', 's3cr3t');
    expect(strategy.headers()).toEqual(strategy.headers());
  });

  it('toBasicAuth returns Authorization: Basic base64(id:cred)', () => {
    const strategy = new ActorAuthStrategy('nx:human:dave', 'pass');
    const basic = strategy.toBasicAuth();

    const expected = Buffer.from('nx:human:dave:pass').toString('base64');
    expect(basic['Authorization']).toBe(`Basic ${expected}`);
  });

  it('toBasicAuth output matches headers() when decoded', () => {
    const strategy = new ActorAuthStrategy('nx:human:eve', 'pw');
    const basic = strategy.toBasicAuth();
    const encoded = basic['Authorization'].replace('Basic ', '');
    const decoded = Buffer.from(encoded, 'base64').toString('utf-8');

    // actorID contains colons, so split on the LAST colon only
    const lastColon = decoded.lastIndexOf(':');
    const id = decoded.slice(0, lastColon);
    const cred = decoded.slice(lastColon + 1);
    expect(id).toBe(strategy.actorID);
    expect(cred).toBe(strategy.credential);
  });
});

// ---------------------------------------------------------------------------
// ControlAuthStrategy
// ---------------------------------------------------------------------------

describe('ControlAuthStrategy', () => {
  it('produces X-API-Key header', () => {
    const strategy = new ControlAuthStrategy('my-control-key');
    const headers = strategy.headers();

    expect(headers['X-API-Key']).toBe('my-control-key');
  });

  it('exposes apiKey as a readonly property', () => {
    const strategy = new ControlAuthStrategy('ctrl-key-42');
    expect(strategy.apiKey).toBe('ctrl-key-42');
  });

  it('implements the AuthStrategy interface', () => {
    const strategy: AuthStrategy = new ControlAuthStrategy('key');
    expect(strategy.headers()).toBeDefined();
  });

  it('produces identical headers on every call (stateless)', () => {
    const strategy = new ControlAuthStrategy('stable-key');
    expect(strategy.headers()).toEqual(strategy.headers());
  });
});

// ---------------------------------------------------------------------------
// RandomCredentialProvider
// ---------------------------------------------------------------------------

describe('RandomCredentialProvider', () => {
  it('implements the CredentialProvider interface', () => {
    const provider: CredentialProvider = new RandomCredentialProvider();
    expect(typeof provider.generateActorCredential).toBe('function');
    expect(typeof provider.generateControlKey).toBe('function');
  });

  it('generates actor credentials with the expected prefix', () => {
    const provider = new RandomCredentialProvider();
    const cred = provider.generateActorCredential();

    expect(cred).toMatch(/^e2e-/);
  });

  it('generates control keys with the expected prefix', () => {
    const provider = new RandomCredentialProvider();
    const key = provider.generateControlKey();

    expect(key).toMatch(/^e2e-control-/);
  });

  it('generates unique values across calls', () => {
    const provider = new RandomCredentialProvider();
    const creds = new Set(
      Array.from({ length: 100 }, () => provider.generateActorCredential()),
    );
    expect(creds.size).toBe(100);
  });

  it('generates unique control keys across calls', () => {
    const provider = new RandomCredentialProvider();
    const keys = new Set(
      Array.from({ length: 100 }, () => provider.generateControlKey()),
    );
    expect(keys.size).toBe(100);
  });

  it('actor credential and control key are different values', () => {
    const provider = new RandomCredentialProvider();
    expect(provider.generateActorCredential()).not.toBe(provider.generateControlKey());
  });
});

// ---------------------------------------------------------------------------
// Cross-strategy invariants
// ---------------------------------------------------------------------------

describe('cross-strategy invariants', () => {
  it('ActorAuthStrategy and ControlAuthStrategy produce different header sets', () => {
    const actor = new ActorAuthStrategy('nx:human:test', 'cred');
    const control = new ControlAuthStrategy('cred');

    const actorHeaders = actor.headers();
    const controlHeaders = control.headers();

    expect(Object.keys(actorHeaders)).not.toEqual(Object.keys(controlHeaders));
  });

  it('a custom CredentialProvider can be injected (DI pattern)', () => {
    // Simulates what startNexus() does with opts.credentialProvider
    const fixed: CredentialProvider = {
      generateActorCredential: () => 'fixed-actor-cred',
      generateControlKey: () => 'fixed-control-key',
    };

    const actorCred = fixed.generateActorCredential();
    const controlKey = fixed.generateControlKey();

    const actorStrategy = new ActorAuthStrategy('nx:human:test', actorCred);
    const controlStrategy = new ControlAuthStrategy(controlKey);

    expect(actorStrategy.headers()['X-Actor-Credential']).toBe('fixed-actor-cred');
    expect(controlStrategy.headers()['X-API-Key']).toBe('fixed-control-key');
  });
});
