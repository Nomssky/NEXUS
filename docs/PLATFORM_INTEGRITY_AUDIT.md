# NEXUS — Platform Integrity Audit

**Milestone:** platform integrity audit to a production-ready baseline
**Baseline:** `76ca08c` (Governance-to-Production Roadmap complete)
**Status:** working audit record — findings, dispositions and the reconciliation
that follows them.

This document is the Phase 0 audit matrix and the finding register that drives
the implementation commits in this milestone. Every finding carries a
disposition:

| Disposition | Meaning |
|---|---|
| `FIXED` | implementation corrected, contract already required it |
| `ACCEPTED` | current behaviour is correct and contract-consistent; documented |
| `DEFERRED` | out of scope for this milestone, with a stated reason |
| `CONTRACT GAP` | the contract is silent or self-contradictory; recorded, not invented |

Authority order used throughout: locked contracts → implementation → tests →
commit history → design notes. Where two contract sections disagree, the
addendum that restates the behaviour is called out explicitly rather than
either section being rewritten.

---

## 1. Audit matrix

| Subsystem | Contract | Implementation | HTTP exposure | Persistence | E2E | Gap |
|---|---|---|---|---|---|---|
| identities | `SCHEMA_IDENTITIES_ORG` §2, §9 | `foundation/identity/identity.go`, `registry.go` | `GET/POST /api/v1/identities`, `GET {id}` | durable (`storage.data_dir`) | `01`, `03`, `08` | lifecycle **authority model undefined** (§G1) |
| credentials | `SCHEMA_IDENTITIES_ORG` §10 | `identity/authenticate.go`, `persist.go` | never exposed | durable, verifier/hash only | `01`, `08` | — |
| memberships | `SCHEMA_IDENTITIES_ORG` §10 | `identity/membership.go` | never exposed | durable | `03`, `08` | division narrowing only applied where a division is recorded (§G3) |
| businesses | `SCHEMA_IDENTITIES_ORG` §3 | `identity/registry.go` | `GET/POST /api/v1/businesses` + transitions | durable | `03`, `08` | business status not checked at admission (§G2) |
| divisions | `SCHEMA_IDENTITIES_ORG` §4 | `identity/registry.go` | `GET/POST /api/v1/divisions` + transitions | durable | none | **`division_id` absent from submit** (F1) |
| requests | `CORE_INTERFACE_CONTRACTS` §4, `RUNTIME_EXECUTION_CONTRACTS` §3 | `core/engine.go`, `core/chain.go` | submit / result / cancel | ephemeral (C-019) | `02`, `04`, `07`, `08` | division scope dropped at the HTTP boundary (F1) |
| scopes | `RUNTIME_EXECUTION_CONTRACTS` §3.2 | `core/context.go`, `chain.go` `chainAuthorization` | `business_id` query + body | — | `03` | admission is division-aware once a division reaches the context (F1) |
| governance policies | `SCHEMA_GOVERNANCE_ATTENTION` §2, §9 | `governance/engine.go`, `gateway/policies.go` | `GET/PUT/DELETE /api/v1/control/policies*` | process-lifetime (§9.6) | `09` | §2.8 text coarser than §9.5 (F4) |
| governance decisions | `SCHEMA_GOVERNANCE_ATTENTION` §2.8, `CORE` §3 | `core/chain.go` `chainGovernance` | via terminal result | — | `09`, `10` | stale comments (F3) |
| approvals | `SCHEMA_WORK_OBJECTIVES` §6 | `core/approval.go`, `governance/approval.go` | `GET /api/v1/approvals`, approve/deny | in-memory (P1) | `10` | no unauthorized-decision E2E |
| escalations | `SCHEMA_GOVERNANCE_ATTENTION` §4 | `core/escalation.go` | `GET /api/v1/escalations`, ack/resolve | in-memory (P1) | `10` | — |
| executor | `RUNTIME_EXECUTION_CONTRACTS` §6.1 (worker machine) | `executor/executor.go` | via `outcome.metrics` | — | `02`, `11` | worker lifecycle ≠ process lifecycle (ACCEPTED, §A5) |
| providers | `PROVIDER_CONTRACTS` §4, §7.3, §12 | `modelrouter/provider.go`, `router.go` | via result error | config-seeded | `11` | §7.3 vs §12.2 reconciled by addendum (§A6) |
| cancellation | `CORE` §6.4, `core/cancel.go` | `core/cancel.go` | `POST /api/v1/requests/{id}/cancel` | — | `07` | division narrowing added at core boundary (F2) |
| persistence | `store/store.go`, `filestore.go` | `foundation/store` | never exposed | identity/business/division/credential/membership durable | `08` | classification made explicit (§7) |
| SSE / events | `SCHEMA_EVENTS_TRIGGERS` §2 | `foundation/event`, `gateway/server.go` | `GET /events` | — | `06` | — |
| health / readiness | `docs/http-gateway.md` | `gateway/server.go` | `GET /health`, `/ready`, `/status` | — | `05` | — |
| control API | `docs/http-gateway.md` | `gateway/server.go` | `/api/v1/control/*` | — | `05`, `09` | `uptime` documented but never populated (F3) |
| authentication | `SCHEMA_IDENTITIES_ORG` §2.4, §10 | `gateway/identity.go`, `identity/authenticate.go` | `X-Actor-ID` / `Basic` | hash only | `01`, `08` | — |
| authorization | `CORE_INTERFACE_CONTRACTS` §4.2 | `gateway/identity.go` `authorizeMembership` | all identity-scoped paths | — | `03` | membership is the boundary, no role model (ACCEPTED) |
| error envelopes | `CORE_INTERFACE_CONTRACTS` §3 | `gateway/server.go` `writeError` | every error path | — | `04` | — |
| operational controls | `docs/http-gateway.md` | `gateway/server.go` | pause / resume / metrics / components | — | `05`, `09` | — |

### 1.1 Route reconciliation

`internal/gateway/server.go` registers **39** routes; `docs/http-gateway.md`
§Endpoints lists the same **39**, in the same two authentication groups
(identity-scoped vs. key-gated control). No registered route is undocumented and
no documented route is unregistered, so the gateway exposes no undocumented
public behaviour.

---

## 2. Findings

### F1 — division scope is unreachable through the public submit API

| | |
|---|---|
| **Severity** | High (architecture coherence / scope propagation) |
| **Subsystem** | requests → scopes → governance |
| **Classification** | `FIXED` |

**Contract.** `CORE_INTERFACE_CONTRACTS` §4.2 `CTR-AUTH-001` takes
`division_id: string (optional)` as authority-resolution input.
`RUNTIME_EXECUTION_CONTRACTS` §3.2 requires that "Admission respects
business/division scope" and §7.3 requires `division_id` to be preserved on
*all* workflow nodes. `SCHEMA_IDENTITIES_ORG` §8 requires `division_id` to be
within `business_id`.

**Implementation.** The scope already exists at every layer below HTTP:
`RequestContext.DivisionID` with `WithDivision`, `chainAuthorization` calls
`IsMember(actor, business, req.Context.DivisionID)`, `chainGovernance` forwards
the division into `governance.Request`, and `governance.matchesScope` matches
division-pinned policies. Divisions are full public entities (create, read,
list, transitions).

**Gap.** `submitRequest` had no `division_id` field, so the value was always
empty end to end. A division-scoped policy could never match a request and the
admission gate never narrowed by division.

**Resolution.** Accept optional `division_id` on submit, validate it against
§8, require the membership to cover it, and propagate it into the request
context.

### F2 — a recorded division was not enforced on cancel

| | |
|---|---|
| **Severity** | Medium |
| **Subsystem** | cancellation |
| **Classification** | `FIXED` |

**Contract.** `SCHEMA_IDENTITIES_ORG` §4.3: "Agents scoped to a Division cannot
access other Divisions' data."

**Implementation.** `CancelRequest` compared `businessID` only; the in-flight
record carried no division.

**Resolution.** Record the division with the in-flight entry and enforce it at
the cancel boundary under the same `identityEnforceScope` flag that guards
`chainAuthorization`. Divisionless requests are untouched.

### F3 — documented control field never populated; stale comments

| | |
|---|---|
| **Severity** | Low |
| **Subsystem** | control API, governance/core comments |
| **Classification** | `FIXED` |

* `docs/http-gateway.md` documents `GET /api/v1/control/status` as
  `200 {status, uptime, components, request_count}` and `ControlStatusResponse`
  carries an `uptime` tag, but `handleControlStatus` never set it, so the wire
  always carried `""`. **Fixed:** populated from the server's start instant
  (Go duration string, documented).
* `ESALATION_REQUIRED` appears in comments while the wire code and every test
  use the correct `ESCALATION_REQUIRED`. Comment-only; no wire change.
* Comments and `docs/m2-governance-engine.md` claim a `SYSTEM_SAFETY` precedence
  level above `GLOBAL`. No such level exists in `ScopeLevel`, in `Policy`, or in
  any contract. **Fixed:** comments and the milestone doc reconciled to the
  implemented hierarchy.

### F4 — governance §2.8 describes a coarser algorithm than §9.5

| | |
|---|---|
| **Severity** | Low (contract self-consistency) |
| **Subsystem** | governance |
| **Classification** | `ACCEPTED` (documented, locked §2.8 untouched) |

§2.8 step 4–6 reads "First `DENY` wins … first explicit effect wins"; the
implementation sorts by restrictiveness, then precedence, then scope
narrowness. §9.5 (addendum) restates exactly the implemented rule. §9.5 is the
operative restatement; §2.8 is a coarser summary, not a contradicting
requirement for any reachable input (a lone `DENY` wins under both). Locked §2.8
is **not** rewritten; the relationship is documented in
`docs/m2-governance-engine.md`.

---

## 3. Accepted — current behaviour is contract-consistent

### §A1 — identity lifecycle transitions carry no authority model

`SCHEMA_IDENTITIES_ORG` §2.4 states "An identity does NOT carry authority … All
four are resolved separately through Governance", and the contract defines no
authority model for lifecycle transitions. The implementation's rule is
membership of the record's business scope — any member, no role check — and
`404` for foreign scope. No role system is invented. The rule is documented
precisely in `docs/http-gateway.md` and covered by E2E (member may transition,
non-member gets `404`, bootstrap self-lockout and recovery).

### §A2 — `403` on foreign-scope request reads vs `404` on org records

`GET /api/v1/requests/{id}` answers `403 AUTHORIZATION` for a known id outside
the caller's scope, while the org endpoints answer `404` for foreign records.
Both are documented in `docs/http-gateway.md` and both are pinned by tests.
Request reads require an explicit `business_id` the caller must be a member of,
so the two surfaces are not shaped the same way. Accepted as documented;
recorded under §G5 so a future decision can unify it if wanted.

### §A3 — durable vs ephemeral state

See §7 below. Approvals, escalations, policies and request/result records are
process-lifetime by explicit prior disposition (`C-019`, `SCHEMA_GOVERNANCE`
§9.6, approval P1). Identity, business, division, credential and membership
records are durable.

### §A4 — constraint enforcement

`ALLOW_WITH_CONSTRAINTS` reports constraints and does not enforce them.
`SCHEMA_GOVERNANCE_ATTENTION` §9.5 states "reported not enforced"; enforcement
is a governance-execution milestone. Not introduced in this milestone.

### §A5 — worker lifecycle vs process lifecycle

`RUNTIME_EXECUTION_CONTRACTS` §6.1 defines a *worker* state machine
(`REGISTERING`, `BUSY`, `DEGRADED`, `RECOVERING`). `foundation/lifecycle`
implements the *process* state machine. Different subjects, no conflict.

### §A6 — provider §7.3 retryability and health vocabulary

`PROVIDER_CONTRACTS` §7.3 marks "Provider unavailable" as retryable with
backoff; the shipped behaviour is `retryable: false`. §12.2 (addendum) pins the
observable result and explains the resolution: `CORE` §3 permits "Maybe" and the
chain resolves it to `false`. Health vocabulary is internal: §12.1 allows only
`healthy`/`offline` for the seeded simulator and explicitly does not repurpose
§4 health states. Covered by `e2e/tests/11-provider-failure.spec.ts`.

---

## 4. Contract gaps (recorded, not invented)

| Ref | Gap | Consequence | Status |
|---|---|---|---|
| **G1** | `SCHEMA_IDENTITIES_ORG` defines no authority model for identity lifecycle transitions | membership of the record's business is the only boundary; any member may suspend/revoke | documented in `docs/http-gateway.md`; no authority invented |
| **G2** | No contract says whether a request is rejected when its business (or division) is not `active` | a suspended/archived business with active memberships still admits work | documented; not invented |
| **G3** | No contract states whether a division-scoped *membership* grants business-level access when the caller presents no division | reads/cancels of divisionless requests stay business-scoped | narrowing applies wherever a division is recorded |
| **G4** | Request durability is not specified; `C-019` makes `Store()` external-only | requests/results do not survive restart | documented in §7 |
| **G5** | Foreign-scope status codes differ between request reads (`403`) and org reads (`404`) | mild cross-scope existence difference, both documented | accepted per §A2 |

---

## 5. Security posture after the audit

| Check | Result |
|---|---|
| authentication bypass | none — missing/invalid credentials both `401 AUTH`, identity status enforced on every call |
| authorization bypass | none — `actor_id` must equal the authenticated identity; membership checked on every scoped path |
| actor spoofing | blocked (`403 AUTHORIZATION` on `actor_id` mismatch); with enforcement off the client actor is discarded, not trusted |
| scope widening via parameters | `business_id` must be a membership; `division_id` must exist, belong to that business, and be covered by the membership |
| credential leakage | verifier/hash only on disk; never in responses, errors or logs (E2E asserts the raw value is absent from the record file) |
| cross-business access | `403` on mismatch, `404` for foreign org records, membership-filtered business directory |
| cross-division access | enforced wherever a division is recorded (F1, F2) |
| governance bypass | none — every request evaluates at least one policy; `default-allow` is read-only through the API |
| approval bypass | none — resume requires an explicit approved record (INV-10) |
| escalation bypass | none — escalation is a distinct `ESCALATION_REQUIRED` code, terminal |
| persistence poisoning | fail-closed hydration: corrupt/invalid/mismatched records abort boot |
| information leakage | error envelopes carry no internals; `retryable` follows the category default; control surface is key-gated |
| race conditions | `go test -race` clean; cancellation arbitration is atomic with slot reservation (E-005) |

---

## 6. Route → contract → test map

Every registered route appears in `docs/http-gateway.md`, and every route with
externally observable contract behaviour has black-box coverage:

| Route family | Contract | E2E |
|---|---|---|
| `/health`, `/ready`, `/status` | `docs/http-gateway.md` | `05` |
| submit / result / cancel | `CORE` §4, `RUNTIME` §3 | `02`, `04`, `07`, `12` (division) |
| approvals | `SCHEMA_WORK` §6 | `10` |
| escalations | `SCHEMA_GOVERNANCE` §4 | `10` |
| identities / businesses / divisions | `SCHEMA_IDENTITIES_ORG` §2–§4 | `01`, `03`, `08`, `13` (lifecycle), `12` (division) |
| control surface | `docs/http-gateway.md` | `05` |
| policy control | `SCHEMA_GOVERNANCE` §9 | `09`, `14` (lifecycle) |
| `/events` (SSE) | `SCHEMA_EVENTS` §2.2 | `06` |

---

## 7. Persistence classification

Derived from contracts and prior dispositions, not from convenience.

| State | Class | Where | Contract basis |
|---|---|---|---|
| identities | **Durable** | `store` type `identity` | `SCHEMA_IDENTITIES_ORG` §10 |
| credentials | **Durable** | `store` type `credential`, hash only | §10.1/§10.2 |
| memberships | **Durable** | `store` type `membership` | §10.1/§10.2 |
| businesses | **Durable** | `store` type `business` | §10.1 |
| divisions | **Durable** | `store` type `division` | §10.1 |
| provider configuration | **Durable** | config file / environment | `PROVIDER_CONTRACTS` §12.1 |
| requests | **Ephemeral** | in-memory admission index | `C-019` (§G4) |
| results | **Ephemeral** | in-memory result map | `C-019` (§G4) |
| approvals | **Ephemeral** | in-memory approval index | approval P1 disposition |
| escalations | **Ephemeral** | in-memory escalation queue | escalation P1 disposition |
| governance policies | **Ephemeral** | engine policy set | `SCHEMA_GOVERNANCE` §9.6 |
| runtime / process state | **Ephemeral** | `foundation/lifecycle` | "runtime state is not durable state" |
| events | **Reconstructable** | re-emitted from lifecycle transitions | `SCHEMA_EVENTS_TRIGGERS` |

Hydration is fail-closed: a record that cannot be decoded, that disagrees with
its record id, that fails validation, or that carries a non-canonical `method`
aborts boot. Record ids are namespaced by type (`credential:<id>`,
`membership:<id>`) so no type can shadow another in the id-keyed index.

---

## 8. Operational hardening classification

| Item | Classification | Basis |
|---|---|---|
| TLS / mTLS | **INTENTIONALLY OUT OF SCOPE** | gateway binds `127.0.0.1` only; no contract requires transport security on a loopback listener |
| multi-instance | **INTENTIONALLY OUT OF SCOPE** | single-process architecture; no contract describes clustering |
| gateway rate limiting | **DEFERRED** | contracts define rate limiting for *provider* traffic (`EXTERNAL_FAILURE_RECONCILIATION`), not for the loopback gateway; body and header limits are enforced |
| 413 / body limits | **REQUIRED — present** | `MaxBytesReader` 1 MB, response cap, SSE frame cap |
| SSE capacity | **REQUIRED — present** | 10 concurrent clients → `503 RESOURCE_LIMIT` |
| SSE failure behaviour | **REQUIRED — present** | write failure cancels the stream (G-011) |
| control metrics / components | **REQUIRED — present** | documented and E2E covered |
| readiness / health / status | **REQUIRED — present** | documented and E2E covered |
| graceful shutdown | **REQUIRED — present** | `SIGTERM` drains close hooks under budget, exit 0 |
| configuration validation | **REQUIRED — present** | blank/invalid config rejected at startup, e.g. seeded provider status |
