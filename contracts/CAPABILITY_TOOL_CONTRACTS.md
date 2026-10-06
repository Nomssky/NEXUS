# Capability & Tool Platform — Contract (v1)

**Status:** v1, additive. It extends the existing tool runtime
(`internal/foundation/tool`), the Agent Execution Layer
(`contracts/AGENT_EXECUTION_CONTRACTS.md`) and the Agent Intelligence Layer
(`contracts/AGENT_INTELLIGENCE_CONTRACTS.md`). **No locked contract section is
modified, and the following remain authoritative unchanged:**

* G1–G5 (`docs/ARCHITECTURE_DECISIONS.md`, `SCHEMA_IDENTITIES_ORG` §12,
  `CORE_INTERFACE_CONTRACTS` §11) — identity, admission, division scope,
  durability (Level 1), visibility;
* Agent Execution v1 — selection, tool allowlists, delegation, workflows,
  cancellation;
* Agent Intelligence v1 — the closed action vocabulary (`tool_call` stays the
  only tool-invoking verb), plan validation, budgets, observation boundary.

The single invariant of this milestone:

> **The model can request capabilities. It cannot grant capabilities.**

---

## 1. Terminology

| Term | Meaning |
|---|---|
| **Tool** | A registered capability adapter exposing controlled operations. |
| **Tool ID** | Stable machine identifier (`http.request`, `filesystem.read`, `git.status`, `web.search`, `data.json.parse`, `github.issue.create`, …). |
| **Tool manifest** | Machine-readable declaration: id, version, name, description, category, input/output schema, side-effect class, network requirement, credential requirement, resource limits, supported operations, scope requirements, security class. |
| **Capability** | Declarative description of what an agent may use. **Not** a permission. |
| **Permission** | Runtime authorization for one invocation (identity + scope + allowlist + manifest + governance + credential + budgets). |
| **Credential** | Secret material resolved at the tool boundary through `security.Resolver`; never enters model context, observations, events, logs or responses. |
| **Invocation** | One validated runtime operation through §5's single path. |
| **Observation** | The redacted, bounded, untrusted-data-tagged result handed to the intelligence loop. |

## 2. Tool manifest

```
ToolManifest {
  id, version, name, description, category,
  input_schema, output_schema,
  side_effect_class,          // read | write | external_mutation | credentialed_external_mutation
  network_requirement,        // none | outbound_http
  credential_requirement,     // none | named reference (business/division scoped)
  resource_limits,            // duration, output bytes, request bytes, response bytes, max items …
  supported_operations[],     // explicit; an unnamed operation is invalid
  scope_requirements,         // business | division | workspace
  security_class              // sandboxed | network | credentialed
}
```

Validation is fail-closed and happens **before** registration: blank id,
blank/invalid version, unsupported category, malformed or missing schema,
unknown side-effect/security/network/credential class, invalid resource limits,
duplicate or blank operation names are all rejected. Dangerous declarations are
never silently normalized. Duplicate tool ids are rejected. Existing tool ids
(`echo`, `calculator`, `transform`) keep working: they are registered with
manifests (`read`, `sandboxed`, `none` network/credential).

## 3. Side-effect classes

`read`, `write`, `external_mutation`, `credentialed_external_mutation`.

The class is **security metadata**; the runtime never infers it from a tool
name, and it decides retry and audit policy:

| Class | Automatic retry | Default |
|---|---|---|
| `read` | permitted when the adapter marks the operation retryable | `read` |
| `write` | not automatic | `write` |
| `external_mutation` | never automatic | `external_mutation` |
| `credentialed_external_mutation` | never automatic | `credentialed_external_mutation` |

## 4. Capability discovery (informational only)

An identity-scoped `GET /api/v1/tools` returns the catalog of tools visible in
the caller's scope: id, name, description, input/output schema, supported
operations, side-effect class, constraints. It never contains credentials,
secret values, internal filesystem roots, network topology or authorization
internals. The same bounded catalog is rendered into the intelligence decision
prompt. Discovery grants nothing: every invocation is validated again.

## 5. The single invocation path

```
intelligence action tool_call
  ↓ tool id + input
  1 manifest lookup                (registry; unknown → rejected)
  2 input schema validation        (unknown field / wrong type → rejected)
  3 operation resolution           (must be in supported_operations)
  4 agent allowlist                (agent.AllowedTools ⊇ tool id)
  5 scope validation               (identity membership; G3 division rules)
  6 permission evaluation          (CanInvoke: identity+scope+allowlist+manifest+governance+credential scope+limits)
  7 budget validation              (capability budgets; server caps)
  8 credential resolution          (security.Resolver → opaque handle)
  9 adapter invocation             (ctx, validated input, handle)
 10 result normalization           (status, result, metadata, error, duration, usage, truncated)
 11 secret redaction               (mandatory, runtime-side)
 12 observation                    (untrusted-data tagged)
```

No tool may bypass this path; there is no second mechanism. Adapter-level
authorization does not exist: adapters receive already-authorized, bounded
input and an opaque credential handle, and never see identity or policy.

## 6. Credentials

`CapabilityCredentialResolver` wraps the existing `security.Resolver`
(`security.DevResolver` today) and returns an opaque handle. Rules:

* references are named and **business-scoped** (optionally division-scoped);
* a foreign business or division fails closed before any adapter runs;
* unavailable/invalid reference ⇒ `credential unavailable` error, no adapter
  call, `tool.credential.denied` event;
* the raw secret is passed only to the adapter through the handle and is never
  returned to the intelligence layer, never rendered into events, observations,
  logs or HTTP responses;
* tools that declare no credential requirement never receive a handle.

## 7. Result contract and redaction

```
Result { status, result, metadata, error, duration, usage, truncated }
```

Bounds: max output bytes, max metadata bytes, max result depth, max duration,
max request bytes (per manifest, clamped by runtime caps). Exceeding a bound
sets `truncated = true` (never silent), except when the bound is a hard safety
limit, in which case the invocation fails with `resource limit`.

Redaction is mandatory and runtime-side. It removes `Authorization`,
`Set-Cookie`, `Proxy-Authorization`, bearer tokens, API keys, passwords,
client secrets, private keys and any registered secret value from headers,
results, metadata and error text — before they can reach a model prompt,
observation, event, log, response or durable memory.

## 8. Capability budgets

Runtime-enforced, additive to intelligence budgets:
`max_tool_duration`, `max_output_bytes`, `max_request_bytes`,
`max_response_bytes`, `max_headers`, `max_redirects`, `max_concurrent_network`,
`max_external_mutations`, `max_files_read`, `max_files_written`,
`max_items`. Manifests may only request *smaller* limits than the runtime caps;
callers may only tighten further. The model cannot change any of them.

## 9. HTTP capability (`http.request`)

Operations: `get`, `post`, `put`, `patch`, `delete` (each explicit; method is
an operation name, never a free-form string).

SSRF defence, in order, before any connection: scheme allowlist
(`https`, plus `http` only when explicitly configured); reject file/ftp/gopher/
unix schemes, URLs with embedded credentials, and non-standard ports unless
allowlisted; resolve the hostname and reject loopback, private, link-local,
multicast, unspecified, CGNAT and cloud-metadata addresses; re-check every
resolved address before connecting (DNS-rebinding defence via a custom
`DialContext` that validates the address it is asked to dial); validate each
redirect target with the same rules; bound redirects, timeout, request body,
response size and header count; strip sensitive response headers. `localhost`
/ loopback is blocked **unless** the non-production environment variable
`NEXUS_TOOL_HTTP_ALLOW_LOOPBACK=true` is set (hard-refused when
`NEXUS_ENVIRONMENT=production`), which exists solely for local development and
the black-box test suite. Cloud metadata addresses stay blocked always.

Response normalization: `status_code`, sanitized `headers`, `content_type`,
bounded `body`, `truncated`, `duration`, `final_url`.

## 10. Filesystem sandbox (`filesystem.read` / `.write` / `.list`)

All paths are resolved against a configured workspace root
(`NEXUS_TOOL_FS_ROOT`, default `<data_dir>/workspaces/<business>`), canonicalized
(`filepath.Abs` + `filepath.EvalSymlinks`), and authorized **after**
canonicalization: the resolved path must remain inside the root (prefix check
on path segments), so `../`, absolute paths, symlink escapes, device paths and
special files are rejected. Reads/writes are byte-bounded; no permissions,
chmod/chown, no process execution.

## 11. Git capability (`git.status` / `git.diff` / `git.log`)

Local, read-only repository operations inside a configured workspace. The
repository path is resolved by the same sandbox canonicalization; the git
binary is invoked with a fixed argument vector (no shell, no `-c`, no
`--exec-path`, no hooks, scrubbed environment, fixed timeout, bounded output).
Remote mutation (push/pull/fetch over the network) is **not** part of v1.

## 12. Web research (`web.search`)

`ResearchProvider` interface with one operation (`search`) returning normalized
sources `{title, url, snippet, source, published_at}` with bounded lengths. A
deterministic scripted provider ships for tests and for local exercise; a real
provider plugs in behind the same interface. Research data is **external,
untrusted data**: it is redacted, bounded and delivered as an observation, never
as instructions.

## 13. GitHub capability (`github.*`)

`github.repository.read`, `github.issue.list`, `github.issue.read`,
`github.issue.create`, `github.issue.comment`. Implemented as a mediated adapter
over §9's HTTP boundary with a fixed host policy (`api.github.com`), a
business/division-scoped credential reference for the token, and explicit side
effect classes (`read` for reads, `credentialed_external_mutation` for
creates/comments — never automatically retried). The token never reaches the
model.

## 14. Structured data capability (`data.*`)

`data.json.parse`, `data.json.transform`, `data.csv.parse` — deterministic,
side-effect-free, with byte, depth, item-count and field-count bounds. No
expression language: `transform` supports a fixed operation set only.

## 15. Events

Added to the existing envelope/bus (no parallel bus): `tool.registered`,
`tool.invocation.started`, `tool.invocation.completed`, `tool.invocation.failed`,
`tool.invocation.rejected`, `tool.permission.denied`, `tool.credential.denied`,
`tool.result.truncated`. Each carries request/correlation, business, division
and actor; none carries secrets or raw payloads; audit fields (tool, operation,
agent, status, duration, retry count, credential *reference*, result size,
truncated) are the event data.

## 16. Error semantics

Tool errors are distinguishable and map onto the existing NEXUS error model:
`validation`, `permission denied`, `scope denied`, `credential unavailable`,
`network blocked`, `timeout`, `cancelled`, `external error`, `resource limit`,
`internal adapter failure`. G5 visibility semantics are unchanged (a tool that
does not exist or is not visible answers like any other invisible resource).

## 17. Cancellation, durability, governance

* Adapters receive `context.Context`; cancellation propagates from the
  intelligence loop through the execution into network/filesystem/git work.
* Tool **definitions and manifests** are process configuration in v1 (code +
  policy), not durable records; **capability audit** rides the existing event
  model. Agent definitions and agent memory stay durable; execution state stays
  process-local (G4 Level 1). A pre-restart execution id still answers `404`.
* Governance stays upstream and unchanged; the capability platform consumes the
  runtime authority already granted to the execution and performs **no** policy
  evaluation of its own. Per-node governance re-evaluation stays out of scope.

## 18. Intelligence integration

* `tool_call` remains the only tool-invoking action verb; there is no
  `http_call`/`github_call`.
* The decision prompt carries a bounded capability catalog (ids, descriptions,
  operations, side-effect classes) — no secrets, no internal paths.
* Tool results reach the model only as redacted, bounded observations.

## 19. Out of scope for v1 (documented, not silently absent)

Unrestricted shell, arbitrary code/WASM execution, browser automation (and
stealth variants), host process management, unrestricted network access,
autonomous credential acquisition, cross-business tools or delegation,
distributed tool execution, durable tool-execution recovery, tool
self-installation, tool self-modification, autonomous permission escalation,
model-controlled permissions/credentials/registration/policy.
