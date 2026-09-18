# NEXUS — Development (M1 Identity & Security Primitives)

This document covers **only** M1: the configuration, identity, and security
primitives layered on top of the M0 foundation. It does not duplicate
architecture or contract docs.

> M1 extends M0. It adds **identity, authentication, authorization boundaries,
> secret *references*, and a fail-closed security posture**. It does **not**
> implement governance policy, the decision engine, agents, tools, providers, or
> any autonomous behavior (those are M2+).

---

## Scope (C01 + C02 + C04)

| Component | What M1 adds |
|---|---|
| **C01 Configuration** | immutable startup snapshot, deterministic fingerprint, per-section source metadata, `security` config section with fail-closed validation |
| **C02 Identity** | identity types/records, scopes (global/business/division/…), memberships, capabilities, permissions, authentication primitives, authorization boundary, per-request security context |
| **C04 Security primitives** | cryptographically-random IDs, constant-time comparison, credential hashing boundary, `SecretRef`, minimally-exposed `Secret`, `Resolver` + dev-only resolver, log redaction, deny-by-default egress policy |

### Invariants preserved by M1

These are structural, tested properties — not conventions:

- `IDENTITY ≠ AUTHORITY ≠ CAPABILITY ≠ PERMISSION ≠ TRUST` — separate types;
  possessing any of these never implies another.
- `AUTHENTICATION ≠ AUTHORIZATION` — `Authenticate` only establishes *who*;
  authorization is a separate evaluation.
- `AUTHORIZATION ≠ GOVERNANCE` — internal results `AUTHORIZED`/`NOT_AUTHORIZED`
  are distinct from the five canonical governance outcomes and must never be
  presented as them.
- `CAPABILITY ≠ PERMISSION` — the default evaluator consults permissions only.
- `SECRET_REF ≠ SECRET_VALUE` — a `SecretRef` has no field capable of holding a
  value; `Secret` redacts itself in `String`/`MarshalJSON` and exposes bytes only
  via explicit `Reveal`.
- `BUSINESS_SCOPE ≠ GLOBAL_ACCESS` — grants/credentials scoped to Business A
  never apply to Business B.
- `CONFIGURATION ≠ AUTHORITY` — the `security` config section can only *narrow*
  behavior (enforcement switches, restrictions); no field grants privilege.
- `RESOURCE ≠ PERMISSION` — the requested resource is not itself a grant.
- `EXTERNAL_INPUT ≠ TRUSTED_INPUT` — malformed input fails closed.

---

## Layout

```
internal/foundation/
  config/
    config.go        C01: typed config + security section, precedence, fail-closed Validate
    snapshot.go      C01: immutable Snapshot, source metadata, sha256 fingerprint
  identity/
    identity.go      C02: Type/Status, Scope, Identity, New/Validate
    membership.go    C02: Role, Membership, MembershipSet (multi-business, explicit context)
    capability.go    C02: CapabilityRef/CapabilitySet, Permission/PermissionSet (distinct)
    authenticate.go  C02/C04: AuthResult, Authenticator, LocalAuthenticator, RequireAuthenticated
    authorize.go     C02/C03 boundary: Request, Decision, Evaluator (+ internal results)
    context.go       C02: SecurityContext (actor, authn, scope, perms, caps, cred refs)
  security/
    security.go      C04: IDs, constant-time compare, credential hash, SecretRef,
                     Secret, Resolver/DevResolver, redaction, EgressPolicy
```

This maps to the blueprint's layer L1 identity/security primitives.

---

## Configuration

### Security section

The `security` section (file JSON or `NEXUS_SECURITY_*` environment variables)
describes the **defensive posture only**. Values are validated fail-closed.

| Field | Env | Meaning | Default |
|---|---|---|---|
| `audit_enabled` | `NEXUS_SECURITY_AUDIT_ENABLED` | emit security posture/audit | `true` |
| `require_authentication` | `NEXUS_SECURITY_REQUIRE_AUTHENTICATION` | require an authenticated identity | `true` |
| `enforce_business_scope` | `NEXUS_SECURITY_ENFORCE_BUSINESS_SCOPE` | require explicit business context for scoped ops | `true` |
| `dev_allow_unsafe_overrides` | `NEXUS_SECURITY_DEV_ALLOW_UNSAFE_OVERRIDES` | development-only affordance | `false` |
| `egress_allow_list` | `NEXUS_SECURITY_EGRESS_ALLOW_LIST` | allowed outbound `host[:port]` (CSV); empty = deny all | `[]` |
| `sandbox_enabled` | `NEXUS_SECURITY_SANDBOX_ENABLED` | untrusted execution must be sandboxed | `true` |

**Fail-closed rules:** in `production`, `audit_enabled`,
`require_authentication`, `enforce_business_scope`, and `sandbox_enabled` must be
`true`, and `dev_allow_unsafe_overrides` must be `false`. A weakened production
posture is rejected at startup with a `VALIDATION`-category error. Booleans are
parsed strictly (`true`/`false`/`1`/`0`); ambiguous values fail closed.

### Immutable snapshot & fingerprint

Configuration is frozen into an immutable `Snapshot` at startup:

- `Snapshot.Settings()` returns the effective (validated) `Config`.
- `Snapshot.Fingerprint()` returns a deterministic `sha256:` id over non-secret
  settings — useful for audit/drift reasoning without exposing secrets.
- `Snapshot.Source(section)` returns where a section came from (`default`,
  `file`, or `environment`).

The snapshot is **not** hot-reloadable (deferred; see below). Business/division
context is **not** process-global — it belongs to the request/session context
(`identity.SecurityContext`), never to mutable global state.

---

## Identity, authentication, authorization

### Identity (`identity.go`)

`Identity` describes **who/what** something is: `ID`, `NexusID`, `Type`
(human/agent/service/device/system/business/division/workflow/task/provider/model/
tool/connector), `Status`, `Scope`, and metadata. It intentionally has **no**
authority, capability, permission, or trust fields. Metadata keys that look like
secrets are rejected so secrets cannot hide inside identities.

### Scope (`identity.go`)

`Scope` expresses *where*: `GLOBAL`, `BUSINESS`, `DIVISION`, `AGENT`, `WORKFLOW`,
`TASK`, `TEMPORARY`. `Scope.Covers` answers a **visibility** question only — it is
not an authorization check. Business isolation and division isolation are
enforced: sibling businesses/divisions never cover each other, and a division
never covers its whole business.

### Membership (`membership.go`)

`MembershipSet` maps identities to businesses (and optional divisions). One NEXUS
serves many businesses; the active business is supplied **explicitly** at the call
site and verified against memberships. `CurrentBusiness` fails closed
(`AUTHORIZATION`) when the identity is not a member.

### Capability vs Permission (`capability.go`)

- `CapabilityRef`/`CapabilitySet` — *technical ability* (e.g. a tool/model
  exists and can be used). Possessing a capability grants nothing.
- `Permission`/`PermissionSet` — *explicit grants* (action + resource + scope).
  `PermissionSet.Has` requires a covering scope.

These are deliberately separate types so capability cannot be mistaken for (or
silently promoted to) permission.

### Authentication (`authenticate.go`)

`Authenticator.Authenticate` establishes *who* the actor is and returns an
`AuthResult` (authenticated?, method, issue/expiry). `LocalAuthenticator` stores
only credential **hashes** (`security.HashCredential`), compares in constant time
with a uniform failure response, and supports injectable clock/TTL for testing.
`RequireAuthenticated` enforces that an identity is authenticated (and not
expired) — it grants **no** permission.

### Authorization boundary (`authorize.go`)

`PermissionEvaluator.Evaluate` produces an internal `Decision` with result
`AUTHORIZED` or `NOT_AUTHORIZED`, based only on explicit permission grants within
the request scope. It fails closed on malformed requests. These internal results
are **distinct** from the canonical governance outcomes
(`ALLOW`, `DENY`, `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS`, `ESCALATE`); M1
provides the boundary, not the governance engine (M2). `Enforce` turns a
`NOT_AUTHORIZED` decision into an `AUTHORIZATION`-category error.

### Security context (`context.go`)

`SecurityContext` is the validated, per-request/session bag that flows through the
foundation: actor identity, authentication state, active business/division, the
permission and capability sets, credential **references** (never values),
`correlation_id` (tracing only, INV-19), and a data classification. It carries
**no authority**. `TrySwitchBusiness` re-scopes the context only when the actor is
a verified member, dropping credential references that no longer apply — so a
context can never be silently re-pointed at another business.

---

## Secrets (`security.go`)

- `SecretRef` describes *where* a secret lives and how it is scoped. It can be
  logged and serialized safely. Malformed refs (missing store/name, path
  traversal, separators, empty scopes) fail closed with `VALIDATION`.
- `Secret` is a minimally-exposed value: `String()`/`MarshalJSON()` redact it;
  `Reveal()` is the single explicit exposure point.
- `Resolver` is the swappable resolution boundary (its `Resolve` takes an explicit
  business/division scope). `DevResolver` is **development-only**
  (`DevelopmentOnly() == true`) and enforces scope; it is *not* production secret
  management.
- `RedactMap`/`IsSecretKey`/`RedactMarker` provide recursive log redaction.
- `NewID`/`NewCorrelationID` use `crypto/rand`; `ConstantTimeEqual` uses
  `crypto/subtle`; `HashCredential` is a boundary helper (SHA-256), **not** a
  password KDF.

### Egress policy

`EgressPolicy` is **deny-by-default**: constructed from
`security.egress_allow_list`, an empty list denies all egress. It is a
restriction, never an authority grant.

---

## Testing

M1 adds tests `TEST-M1-001..036` (identity, scope isolation, authn/authz
separation, capability-vs-permission, secret handling/redaction, egress
deny-by-default, configuration snapshot/fingerprint/source metadata, and
fail-closed production posture). M0 tests (`TEST-M0-001..015`) remain in place.

```sh
make check       # fmt-check + vet + build + test
make test-race   # race detector
make cover       # coverage summary
```

The **locked-layer guard** (`internal/foundation/guard`) still asserts that no
contract or `Core/` module changed relative to the frozen baseline.

---

## Deferred (NOT implemented at M1)

Explicitly out of scope, per the milestone plan (do not implement):

- Full configuration control plane: distributed config, hot reload, approval
  flows, staged rollout/canary, drift reconciliation, feature-flag
  orchestration, rollback.
- Governance policy engine / decision (M2): the five governance outcomes are
  **not** produced by M1 authorization.
- Persistence, audit stores, event substrate (M3).
- Cognitive and execution components (M4+).
