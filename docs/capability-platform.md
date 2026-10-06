# Capability & Tool Platform v1

NEXUS does not give models direct authority over the host. A model says
`tool_call {tool: "..."}`; the **Capability & Tool Platform** (`internal/capability`)
decides, and only then does a bounded adapter run.

```
OBJECTIVE → INTELLIGENCE → tool_call{tool, input} → CAPABILITY PLATFORM
    → TOOL REGISTRY → PERMISSION/SCOPE/POLICY → CREDENTIAL BOUNDARY
    → ADAPTER → NORMALIZED RESULT → REDACTION → OBSERVATION → INTELLIGENCE
```

## Components

| piece | where | role |
|---|---|---|
| Manifest | `internal/foundation/tool/manifest.go` | machine-readable contract per capability |
| Registry | `internal/foundation/tool/registry_v2.go` | one registry, validated on registration |
| Platform | `internal/capability/platform.go` | the single invocation path |
| Permission | `Platform.CanInvoke` | membership + scope + allowlist + manifest + governance facts |
| Credential boundary | `ScopedCredentialResolver` | wraps `security.Resolver`; opaque handles only |
| Redaction | `redact.go` | runtime-side, mandatory |
| HTTP adapter | `http.go` | SSRF defense at the dial point (changed address checks incl. loopback/metadata/private) |
| Web adapter | `adapters.go` | pluggable `ResearchProvider`; scripted provider for tests |
| Filesystem | `filesystem.go` | sandbox root, canonicalization after `filepath.EvalSymlinks` |
| Git | `git.go` | fixed argv, hooks disabled, bounded output |
| GitHub | `adapters.go` | mediated over the HTTP boundary with scoped credential |
| Data | `adapters.go` | bounded JSON/CSV parse/transform |

## The single invocation path

`Platform.Invoke` performs, in order (contract §5):

1. manifest lookup (unknown → rejected)
2. input schema validation
3. operation resolution (`operations` declared by the manifest)
4. agent allowlist check
5. scope validation (`Platform.CanInvoke`: actor membership, business, division; `ErrScope`)
6. permission-emitted audit (`tool.permission.denied`)
7. budget clamp (`Caps` / manifest runtime limits — manifest may only tighten)
8. credential resolution (manifest declared requirements only; handle resolved for adapters only)
9. adapter invocation with `context.Context` (cancellation must propagate)
10. normalization → bounds/truncation (`truncated=true`, never silent)
11. secret redaction on results *and* errors *and* headers (metadata + audit data)
12. observation path back to the loop — no raw adapter payload ever leaves the boundary

There is **no second invocation path**. `agentintel`, workflows, and delegated
children all reuse `InvokeToolScoped` on `agentexec.Runtime`, which uses the
same platform when one is wired, and the same allowlist/manifest gates otherwise.

## Side-effect classes

Manifest-declared; the runtime never infers them from the name:

* `read` — may be automatically retried by AGENTS (adapter-marked)
* `write` — no automatic retry
* `external_mutation` — never automatic
* `credentialed_external_mutation` — never automatic

## Events on the existing bus

`tool.registered`, `tool.invocation.started`, `tool.invocation.completed`,
`tool.invocation.failed`, `tool.invocation.rejected`, `tool.permission.denied`,
`tool.credential.denied`, `tool.result.truncated`. Every payload carries
`correlation_id`, `business_id`, and fields (tool_id, operation, agent_id,
status, duration_ms, credential *reference* if required, result bytes,
truncated), never secret material.

## Credential boundary

`ScopedCredentialResolver` returns an **opaque handle** (`tool.CredentialHandle`)
to the adapter. The raw secret is reachable only via handle.Secret() inside the
adapter implementation; every token written into the platform is also registered
with `Platform.RegisterSecret` so redaction removes its value from *any* result,
error message, header, or event payload. Business/division scoping is enforced
by the existing `security.DevResolver` semantics — Credentials never cross
business/division boundaries.

## Workspace root resolution

Order: `NEXUS_TOOL_FS_ROOT` env var → `<data_dir>/workspaces/<business_id>`.
`<business_id>` comes from the GitHub business id or `NEXUS_BOOTSTRAP_BUSINESS`
(empty at bootstrap = fail-closed: no sandboxed FS/git tools registered).

## Failure semantics

Tool errors are distinguishable and map to the existing error model; the loop
turns them into `state=failed: <message>` observations. Adapter failures never
become success. Permission (allowlist/scope) is evaluated *before* the adapter
runs, so no data flows from a denied invocation — the failure is audit-only.

## G1–G5 inheritance

* **G1**: bootstrap has no override on this path; all constraints hold from the
  platform registry as much as from handlers.
* **G2**: an inactive business or division still admits no new *execution* —
  this is unchanged gateway admission (capability checks layer *onto* an
  admitted request).
* **G3**: business-scope tools are denied to division-only members through the
  same `MembershipSet.AllowsScope` as upstream; division-scoped tools need the
  acting caller's division to be populated.
* **G4**: executions remain process-local; agent *definitions* are durable;
  manifests are re-registered at boot from configuration, so the catalog is
  stable across restarts while any old execution id returns 404.
* **G5**: a tool that does not exist or is not enabled in the caller's scope
  answers exactly like any other not-found surface (G5 invariants unchanged).

## Deferred / out of scope (written down, not implemented)

Unrestricted shell, arbitrary code/WASM execution, browser automation, host
process management, arbitrary network egress, autonomous account creation,
autonomous credential acquisition, distributed tool execution, durable tool
recovery beyond G4, tool self-installation or modification, RBAC,
owner/owner-bypass escape hatches.
