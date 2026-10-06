# Tool Credentials

Secrets for capabilities with
`credential_requirement.required=true` are resolved by **`ScopedCredentialResolver`**
only; adapters receive an opaque handle. The intelligence layer, the model,
and the result/event pipeline never see raw value material.

## Resolution flow

```
TOOL manifest: credential_requirement {reference, business_id, division_id}
         │
         ▼
Platform.Invoke → ScopedCredentialResolver.Resolve(reference, business, division)
         │
         ▼ wraps security.DevResolver.Resolve(ref, businessID, divisionID)
         │  enforces ref.business_id == request business and
         │  ref.division_id == "" or == request division
         ▼
tool.NewCredentialHandle(reference, resolveFn)
         │  handle.Secret() produces the verbatim value — used only
         │  inside the call that needed it (e.g. the Authorization header
         │  on the outbound HTTPS request)
         ▼
adapter execution with real external service
```

## Rules

1. **Fail closed.** Any unavailable / invalid / denied reference short-circuits
   the invocation with `credential unavailable`. No adapter call happens —
   verified by the audit `tool.credential.denied` + `adapter.called == 0`.
2. **Never cross scope.** A credential is resolved against the *invocation's*
   business/division. A ref stored for a foreign business cannot produce a
   secret for this call (Go `DevResolver.Resolve` refuses with
   `security.secret_out_of_scope`).
3. **Division scoping.** A division-scoped credential cannot serve callers at
   the business scope off the division it lives in.
4. **The handle is the only carrier.** `tool.CredentialHandle` is a pure
   transport object. It is deliberately not JSON-visible: serializing an
   `Invocation` writes the reference name, never the secret.
5. **Not automatic from tool name.** The runtime does not "guess" that a
   given host needs a token.
6. **Mandatory redaction.** The launcher persists `NEXUS_TOOL_CREDENTIAL_VALUE`
   into `Platform.RegisterSecret`, so even if an adapter result erroneously
   contained the literal string, every downstream payload strips it.

## Environment knobs

| Variable | Role |
|---|---|
| `NEXUS_TOOL_CREDENTIAL_VALUE` | dev secret value (loopback tests); never logged |
| `NEXUS_TOOL_CREDENTIAL_REF` | named reference |
| `NEXUS_TOOL_CREDENTIAL_BUSINESS` / `_DIVISION` | scoping fields |
| `NEXUS_CAPABILITY_GITHUB_REF` | the GitHub capability manifests are only registered when this is explicit |
| `NEXUS_CAPABILITY_GITHUB_BUSINESS` | GitHub business scope (defaults to NEXUS_TOOL_CREDENTIAL_BUSINESS or NEXUS_BOOTSTRAP_BUSINESS) |

When practice and development differ: in production a real `security.Resolver`
(vault) must be wired, and `DevResolver.DevelopmentOnly() == true` must be
checked at composition roots to refuse dev credentials. Production mode
(`NEXUS_ENVIRONMENT=production`) refuses loopback HTTP and tool-level
policies that bypass the allowlist-as-guard invariant.
