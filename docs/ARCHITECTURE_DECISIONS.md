# NEXUS — Architecture Decisions (G1–G5)

**Milestone:** gap-disposition decisions closing the five open contract/product
semantic questions left by the Platform Integrity Audit.
**Baseline:** `ceef954` (Platform Integrity Audit complete).
**Status:** complete — all five gaps DECIDED, implemented where required,
covered by regression/E2E tests, and reconciled into locked-contract addenda.

Authority used to decide:
contracts (`SCHEMA_IDENTITIES_ORG`, `SCHEMA_COMMON`, `CORE_INTERFACE_CONTRACTS`,
`RUNTIME_EXECUTION_CONTRACTS`) → prior dispostions (`C-019`, audit §A1–§A6)
→ implementation → tests. No role model, no superuser, no multi-instance
coordination was invented anywhere below.

---

## G1 — Identity lifecycle authority

**Decision.** Membership-boundary authority is the canonical model and is
intentional: any **business-wide** active member of the record's business
may transition that business's identity/business/division records —
including a record of the caller's own id. No role hierarchy is invented;
recorded roles remain descriptive labels.

**Self-mutation.** Allowed and symmetric across statuses: an identity may
suspend/revoke/activate its own record. A lockout it causes is an
operational event, redirected to the recovery paths below.

**Bootstrap.** The bootstrap identity has no permanent privilege:
`NEXUS_BOOTSTRAP_CREDENTIAL` only refreshes its credential hash at boot
(§10.2); the identity record itself is a normal member, its status is
enforced at authentication, and suspending/revoking it is never undone by
a restart. Bootstrap privilege is therefore exactly: a member of its
business.

**Suspended/revoked reactivation.** `suspended → active` is allowed for any
other authorized business-wide member. `revoked` is terminal; recovery is
creating a new identity record, not "un-revoking". `pending → active` is
activating that same path.

**Recovery.** Documented, three equivalent escape hatches: a second active
identity in the business (E2E `13`), deleting the data directory, or the
key-gated control surface. There is deliberately no hidden superuser path.

**Rationale.** INV-04 already fixes `identity ≠ authority`; the contract
defines no role system, so a role hierarchy would be invented architecture.
Membership is the only scope boundary the contract defines for org data,
so transitions ride on it (tightened to *business-wide* membership under
G3). This preserves cross-business isolation: a member of business A can
never transition business B's records — B's records answer `404`.

**Security consequence.** The authority surface for lifecycle mutation is
exactly "active business-wide members of the same business". Suspension
takes effect on the next authentication, so a mutated record loses access
immediately. Self-lockout is reachable only through an authorized member —
never accidentally and never across tenants.

**Product consequence.** Any operator can suspend a member; any operator
can bring a suspended member back. No admin role is required for recovery.

**Implementation.** `internal/gateway/org.go` (`orgScopeVisible` now
requires a business-wide membership), transition handlers unchanged.
No new authority types.

**Tests.** E2E `13-identity-lifecycle.spec.ts` (suspend/revoke remove auth,
pending activation, malformed input rollback, bootstrap self-lockout
recovery through a second identity, and the new self-mutation case);
gateway Go tests pin foreign-record `404` (`org_test.go`).

## G2 — Business/division active-status admission semantics

**Decision.** Lifecycle status gates **admission only**.

| Status (business, or division when set) | New submit | Existing execution | Read | Cancel | Governance evaluation |
|---|---|---|---|---|---|
| `active` | `202` | continues | allowed | allowed | evaluated |
| `suspended` | `409 CONFLICT` (`no new work is admitted`) | continues | allowed | allowed | nothing new admitted |
| `archived` | same as `suspended` | continues | allowed | allowed | nothing new admitted |

* A request is a business-scope request → requires the business `active`.
* A request with `division_id` → additionally requires that division
  `active`. A business-level request is not blocked by a suspended division.
* Status is **not** an authorization status: members keep authenticating,
  reading, and cancelling against a suspended/archived business; record
  transitions keep working (`suspended → active` is the re-enable path).
* Division requests never enter a non-active division; suspended/archived
  divisions' historical records remain readable.
* The registry already enforces `active` parents at *creation*
  (`CreateIdentity`/`CreateDivision`); this decision extends the same rule
  to the request admission edge (`§12.2`).

**Rationale.** §3.1/§4.1 define `active/suspended/archived` but none of the
lifecycle documents states whether suspended organizations accept work.
Splitting admission/execution/read/mutation gives the states honest meaning:
`suspended` is "closed to new work, history remains viewable", never
"disabled" for the people who must still audit or cancel it. Inventing an
error category would violate the contract; `409 CONFLICT` is the existing
"state forbids this transition" code.

**Security consequence.** An archived business cannot be turned into a
covert work intake channel; admission is fail-closed at the boundary, in
both enforcement-on and enforcement-off gateway modes.

**Implementation.** `internal/gateway/server.go` `handleSubmitRequest`:
gate on `registry.GetBusiness`/`GetDivision` status before admissing.
`internal/gateway/g2_admission_test.go` unit coverage; probing on the wire.

**Tests.** E2E `14-admission.spec.ts` (suspended business blocks submit but
reads/cancel keep working; suspended division gates only its own
admissions; archive terminal for admissions); manual probes assert the
complete round trip on the live binary.

## G3 — Division-scoped membership semantics

**Decision (Model B — narrow membership).** A division-scoped membership is
a strict sub-scope of business authority, never a superset:

| Membership | May use |
|---|---|
| Business-wide (`division_id` empty) | every scope of the business: business-level work, every division, approvals, escalations, identity lifecycle, SSE, org lists |
| `division_id == D` | only D-recorded resources: submit with `division_id == D`, read/cancel D-recorded requests |

A division member **never** reaches divisionless business-level resources,
never submits business-scope work, never lists/transitions org records,
never decides approvals, and never subscribes to the business event stream.
Cross-division access is impossible by construction.

**Rationale.** `SCHEMA_IDENTITIES_ORG` §4.3 ("Agents scoped to a Division
cannot access other Divisions' data") and the locked `Scope.Covers` rule
("a source narrowed to a division must not cover a whole-business target")
only function if division membership cannot stand in for business
membership. Model C (business-level reads for division members) would
require a second, divisional definition of "business-level record visible
to division members" everywhere — an invented concept. Model A contradicts
§4.3 as implemented today.

**Hierarchy:**

```
Business          ← business-wide membership required
 ├── Division A   ← business-wide OR membership of A
 ├── Division B   ← business-wide OR membership of B
 └── divisionless resources ← business-wide only
```

**Security consequence.** The bug class "division lead reaches business
billing channel, approvals, or the identity directory" is closed at every
edge. The rule is enforced at the gateway admission path, the core chain's
IDENTITY/AUTHORIZATION stages, cancellation, and every org/approval/
escalation/SSE surface through one predicate (`MembershipSet.AllowsScope`).

**Implementation.** `identity.AllowsScope` added to
`internal/foundation/identity/membership.go`; consumed by
`gateway/identity.go` (`authorizeMembershipScope`,
`authorizeDivisionRead`, `requireBusinessWideMembership`),
`gateway/server.go` (submit gate, result/cancel reads, approvals,
escalations, SSE), `gateway/org.go` (`orgScopeVisible`), `core/chain.go`
(`chainAuthorization`), and `core/cancel.go` (`divisionCancelDenied`).

**Tests.** E2E `12-topology.spec.ts` (own division / sibling 404 /
business-level invisible / governance surfaces 403 matrix); gateway Go
tests `TestG3AllowsScopeSemantics`, updated division-scope matrix in
`division_scope_test.go`; chain-authorization Go tests; manual probes
(narrow-submit 403, narrow business-level read 404, sibling 404).

## G4 — Request durability

**Decision.** Canonical durability level is **Level 1 — durable record**
(§11.1 of `CORE_INTERFACE_CONTRACTS`):

* **Durable:** identities, credentials, memberships, businesses, divisions
  (`SCHEMA_IDENTITIES_ORG` §10), provider configuration.
* **Process-local:** requests (admitted/running/terminal), results,
  approvals, escalations, governance policies, runtime state.

Record durability is *not* execution durability: a persisted request/result
record would not make the in-flight executor resumable, and neither exists
for Level 1. Execution durability is process-local by design and the API
says so.

**Restart semantics (externally observable).**

* Graceful restart, crash, machine reboot: all request state is lost.
* After restart, a previously admitted or completed request id answers
  `404 VALIDATION` from `GET /api/v1/requests/{id}` — identical to an
  unknown id. Clients that want continuity re-submit.
* The `202 accepted` on submit means acceptance by the *current process*;
  nothing in the API implies crash recovery, resume, or multi-instance
  coordination. Level 3 (multi-instance orchestration) is deliberately
  not a requirement of this product (single-process personal AI OS).
* Mid-run cancellation state and pending approvals do not survive;
  nothing is half-replayed on boot.

**Rationale.** `C-019` fixed the Engine `Store()` as an external-only
surface; contract-level persistence is defined only for the identity plane
(§10). Request state is derived data; fabricating a recovery claim would be
dishonest semantics. This avoids inventing a write-ahead execution log the
architecture does not require.

**Security consequence.** No restart-based privilege restoration: all
authorization state (memberships) is re-hydrated from the durable records,
never reconstructed from request state; request data cannot shadow a
membership.

**Implementation.** No code change — behavior already matches Level 1;
what changed is that it is now the written contract (`CORE … §11.1`,
`docs/PLATFORM_INTEGRITY_AUDIT.md` §7 classification refreshed under §4),
plus an explicit regression test.

**Tests.** E2E `08-restart.spec.ts` asserts durable hydration and the new
explicit `404` for a completed pre-restart request id.

## G5 — Visibility semantics (403 vs 404)

**Decision.** One visibility model across all resource families:

* `404 VALIDATION` — *not found OR not visible to this actor*,
  deliberately indistinguishable. An unknown id, a record in a foreign
  business, and a record outside the caller's division scope all answer
  `404`. Existence of cross-tenant resources is never confirmed.
* `403 AUTHORIZATION` — *the scope is visible but the action is not
  permitted*: querying a `business_id` the caller is not a member of at
  all (unknown businesses answer identically, so nothing is disclosed),
  an actor with only a division-scoped membership hitting a business-level
  surface, or an authority exclusion such as `self_approval_prohibited`.
* `409 CONFLICT` — the target's *state* forbids the transition (terminal
  cancel, approval-not-pending, non-active lifecycle admission).

**Rationale.** The audit left request reads at `403` while org records
already hid behind `404` (§A2) — a mild cross-tenant existence oracle.
Normalizing on 404-for-invisible closes that oracle. 403 is kept exactly
where the caller explicitly asserts a scope they cannot enter and the
answer reveals no record: "you are not a member of that scope" is about the
caller, not the resource. No new error category is introduced; every error
still carries
`code/category/message/retryable/correlation_id`.

**Resource matrix (now contract-consistent).**

| Resource | Unknown id | Foreign business | Out-of-scope division | Visible but forbidden |
|---|---|---|---|---|
| request / result | 404 | 404 | 404 | 403 only for membership-free scope query |
| cancel | 404 | 404 | 404 | 409 terminal conflict |
| approval decision | 404 | 404 | — (business-level) | 403 self/approver, 409 not-pending |
| escalation decision | 404 | 404 | — | 409 not decidable |
| org record read/transition | 404 | 404 | — (business-wide required, else 403 on the queried scope; non-visible record → 404) | 409 duplicate/transition |
| policy (control) | 404 | n/a (global surface) | — | 409 default-allow protected |

**Implementation.** Mapping changes only at the HTTP edge:
`handleGetResult` (foreign/mismatched/pending-scope mismatch → 404),
`handleCancelRequest` (`ErrScopeMismatch`/`ErrDivisionScopeMismatch` →
404), approval/escalation decisions (`ErrScopeMismatch`/`ErrEscalationScope`
→ 404). Gateway tests updated; E2E updated (11 of the resource-matrix
paths are pinned in `07/10/12` and the probe script).

**Tests.** E2E `12-topology.spec.ts` (sibling read/cancel → 404),
`07-cancel.spec.ts` (`foreign-${…}` cancel → 404 path via scope, plus the
membership-failure 403), `10-approval.spec.ts` (foreign decision → 404 via
Go tests; self/approver → 403), Go tests `TestApprovalScopeMismatch`,
`TestCancelScopeMismatch`, `TestEscalationScopeMismatch`,
`TestGetResultCrossTenantDenied`, `f6_pending_test.go`.

---

## Traceability

| Gap | Contract | Implementation | Tests |
|---|---|---|---|
| G1 | `SCHEMA_IDENTITIES_ORG` §12.1 | `org.go`, `identity/registry` | E2E 13 (+self-mutation), `org_test.go` |
| G2 | `SCHEMA_IDENTITIES_ORG` §12.2 | `server.go` submit gate | E2E 14, `g2_admission_test.go`, probes |
| G3 | `SCHEMA_IDENTITIES_ORG` §12.3, `CORE` §11.2 | `membership.AllowsScope` + all edges | E2E 12, `division_scope_test.go`, probes |
| G4 | `CORE_INTERFACE_CONTRACTS` §11.1 | existing (honest) behavior documented | E2E 08 assertion |
| G5 | `CORE_INTERFACE_CONTRACTS` §11.2, `SCHEMA_IDENTITIES_ORG` §12.4 | gateway error mapping | E2E 07/10/12, Go scope tests, probes |

No locked contract section was modified; §11 (CORE) and §12 (IDENTITIES_ORG)
are explicit ADDENDA.
