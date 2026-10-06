# GitHub Capability

Manifests (promoted when `NEXUS_CAPABILITY_GITHUB_REF` is set):

| Manifest | Operation | Side-effect class |
|---|---|---|
| `github.repository.read` | fetch repository metadata | `read` |
| `github.issue.list` | list issues | `read` |
| `github.issue.read` | read one issue | `read` |
| `github.issue.create` | create an issue | `credentialed_external_mutation` |
| `github.issue.comment` | comment on an issue | `credentialed_external_mutation` |

## Architecture

GitHub is **not** a parallel execution boundary. It is a `tool.Adapter`
that first resolves the token through its `CredentialHandle` and then
invokes the shared, SSRF-hardened `HTTPTool.request` path with a fixed-host
policy:

```
Platform.Invoke → credential resolution (github reference)
                → GitHubTool.Invoke → HTTPTool.request → api.github.com
```

The `HTTPTool` used for GitHub runs with:

- `AllowedHosts = outer ∪ {api.github.com}` — foreign hosts remain blocked
- `AllowLoopback=false` (exported to the fixed policy) regardless of the outer allow
- HTTPS only (`AllowInsecureHTTP` honored from the outer policy default false)
- Fixed timeout/redirect/byte budgets carried into `request{}`

No model input can inject a foreign URL: `repo` is regex-validated
(`owner/name`), issue numbers are digit-only, titles/bodies are JSON
-encoded. The Bearer token header is installed only on this step, from the
resolved `CredentialHandle` — never carried through `input`.

## Cross-business credential handling

The referenced SecretRef is stamped with `business` (and optionally
`division`), and `CredentialResolver.Resolve` refuses if the invocation
targets a different scope. The resolver therefore answers the *calling*
request from the *same* business only.

## Events & audit

Every invocation emits `tool.invocation.*` events carrying `credential_ref`
(the reference, not the value) and neither the Authorization header nor the
issue body. External mutations are `credentialed_external_mutation` and
**never auto-retry**; this is structural: retry classification is the side
-effect class's job (`SideEffectRead` is the only retry-eligible class).
