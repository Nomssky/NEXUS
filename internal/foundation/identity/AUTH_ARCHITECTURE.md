# Authentication module — architecture

`internal/foundation/identity` implements authentication (C02, layer L1). This
document records the internal pattern introduced by the authentication refactor
and the rules that keep it intact.

The public API (`LocalAuthenticator`, `Authenticator`, `AuthResult`,
`RequireAuthenticated`) is unchanged: existing callers did not need a migration.

## Pattern: Ports & Adapters (hexagonal)

```
                 ┌──────────────────────────────────────┐
                 │        authenticationService         │
   driving ─────▶│  (auth_service.go, use-case core)    │
   adapter       │  validation · uniform failure · TTL  │
   (facade)      └──────────────┬───────────────────────┘
                                │ depends only on
                 ┌──────────────▼───────────────────────┐
                 │            ports                     │
                 │  CredentialRepository (repository)   │
                 │  IdentityDirectory                   │
                 │  CredentialVerifier                  │
                 └──────────────▲───────────────────────┘
                                │ implemented by
                 ┌──────────────┴───────────────────────┐
                 │            adapters                  │
                 │  memory / store repositories         │
                 │  registry directory                  │
                 │  SHA-256 verifier                    │
                 └──────────────────────────────────────┘
```

Two complementary patterns sit alongside it:

- **Facade** — `LocalAuthenticator` wires adapters and delegates every call to
  the core. It contains no authentication rules.
- **Repository** — `CredentialRepository` hides persistence. `Find` / `LoadAll`
  / `Save` are the only persistence vocabulary the core knows.

## File map

| File | Responsibility |
| --- | --- |
| `auth_ports.go` | Port interfaces and the `Credential` value. No imports of store, registry or security. |
| `auth_service.go` | The use-case core: validation, uniform failure (F11), status/expiry enforcement, TTL. |
| `auth_adapters.go` | Memory + store repositories, registry directory, SHA-256 verifier, record codec. |
| `authenticate.go` | Domain types (`AuthMethod`, `AuthResult`), the driving `Authenticator` interface, and the `LocalAuthenticator` facade. |
| `authorize.go`, `capability.go`, `membership.go`, `registry.go`, `business.go`, `division.go`, `context.go` | Unrelated identity/authorization concerns, untouched by this refactor. |

## Dependency rule

Dependencies point **inward**. The core may import only the standard library
and `nerrors`. It must never import `store`, `security`, or reference
`*Registry` directly. If a new capability is needed, add a port; do not link the
core to a technology.

## Behavioural invariants (must not regress)

1. **AUTHENTICATION != AUTHORIZATION.** An `AuthResult` grants nothing.
2. **Uniform failure (F11).** Unknown identity, wrong credential, empty
   credential, missing/suspended/expired record all return the same
   `AuthResult` shape (modulo the echoed id), the same `auth.failed` error, and
   cost the same work. The unknown-id path hashes and compares against a
   precomputed `unknownHash`.
3. **Fail closed.** Any uncertainty — missing record, non-active status,
   expired identity, corrupt stored record — denies.
4. **Persist → memory.** Write-through repositories persist first and only then
   expose the credential; a store failure leaves no in-memory-only credential.
5. **Hydration is atomic.** A single malformed record aborts `OpenLocalAuthenticator`;
   the authenticator is never partially restored.
6. **No raw credentials at rest.** Only verification hashes are stored.

## Adding an adapter

1. Implement the relevant port in `auth_adapters.go` (or a new file).
2. Add the compile-time assertion (`var _ Port = (*adapter)(nil)`).
3. Add a case to `runCredentialRepositoryContract` in `auth_adapters_test.go`
   when the adapter is a `CredentialRepository`.
4. Wire it from the facade. The core does not change.

## Test strategy

| Layer | File | What it proves |
| --- | --- | --- |
| Core | `auth_service_test.go` | Rules work through fake ports only — no store, registry or crypto. |
| Adapters | `auth_adapters_test.go` | One shared repository contract across memory/MemStore/FileStore, registry directory, verifier, hydration fail-closed, concurrency. |
| Facade | `auth_facade_test.go` | Public API wiring: nil store fallback, registry toggling, `RequireAuthenticated`. |
| Robustness | `auth_fuzz_test.go` | No input authenticates without a credential; register/authenticate round-trip; hydration never panics. |
| Performance | `auth_bench_test.go` | Hot-path baselines; known-success, unknown, wrong-credential, parallel, register. |

## CI gates

`.github/workflows/auth.yml` runs on every change under the module:

- `gofmt`, `go vet`;
- `go test -race -covermode=atomic`;
- **statement coverage ≥ 90%** across `auth_adapters.go`, `auth_service.go`
  and `authenticate.go` (currently ~97%);
- 10-second fuzz smoke for each fuzz target;
- benchmark smoke (`-benchtime=20x`).

Repository-wide CI (`.github/workflows/ci.yml`) still runs the full suite,
`-race`, release build and E2E.

## Deferred (unchanged by this refactor)

Session stores, token issuance/rotation, password KDF policy, MFA/passkeys,
device binding, federation/OIDC, and audience enforcement at real boundaries.
