# NEXUS — Model Routing for Agent Execution (v1)

Implementation: `internal/agentexec/runtime.go` (`invokeModel`),
`internal/foundation/modelrouter` (contract surface), seeding in
`internal/launcher/launcher.go`.

## Requirement-driven, never hard-coded

An agent declares *requirements*; the router decides the concrete model:

```json
"model": { "prefer_local": true, "tool_calling": true, "structured_output": false }
```

These become a `modelrouter.RoutingRequest{RequiredCaps, PreferLocal,
AllowedProviders, FallbackEnabled: true}`. The runtime never selects a model by
string matching on intent and never embeds a vendor name in business logic.

## Provider abstraction

```
ModelProvider  (foundation/modelrouter.Provider)
  Identify()        stable provider id
  HealthCheck()     healthy | offline
  ListModels()      models offered
  Invoke(ctx, req)  GenerateRequest → GenerateResponse | error
```

Implemented providers: `LocalProvider` and `RemoteProvider` (Ollama-shaped and
OpenRouter-shaped shells), plus the launcher-seeded `simulated` provider used by
the shipped binary and the test suite. Production providers plug in at the same
interface; nothing in the agent layer depends on a specific one.

## Selection and fallback

`ModelRouter.Route` applies a documented strategy (manual allow-list, privacy,
cost, latency, local-first, or capability match) and then health demotion.
`Invoke` owns the bounded failover chain: the primary attempt fails → health is
recorded → the next chain entry is tried. The runtime reports:

| Field | Meaning |
|---|---|
| `outcome.metrics.provider` | provider that actually answered |
| `outcome.metrics.model` | model that actually answered |
| `outcome.metrics.routing_reason` | router's own reason string (a failover reason starts with `failover`) |
| `outcome.metrics.fallback` | `true` when a failover produced the result |
| `outcome.metrics.retries` | extra runtime-level attempts (bounded, transient-only) |

### Deterministic tie-break contract

When multiple candidates have equal policy rank, the tie is broken by
**ascending lexicographic model ID**. This is deterministic across runs,
across processes, and regardless of model registration order.

- `FindByCapabilities` enumerates candidates in ascending model-ID order, so
  candidate enumeration is deterministic before policy sorting (the previous
  Go map iteration order was unspecified).
- `sortByPolicy` uses stable sorts/partitions, so equal-rank ties preserve the
  ID order: `cheapest` ties by price keep ID order; `local_first` ties within
  each locality class keep ID order; `nearest`/`random` keep ID order.
- `random` is not genuine randomness — it preserves the deterministic
  enumeration order. This is intentional: routing is deterministic, not
  load-balanced (PROVIDER_CONTRACTS §7.3).
- Policy precedence is unchanged: cost/latency/local-first still dominate;
  model ID only breaks ties among equal-rank candidates.

This tie-break mirrors the agent-selection contract in
`docs/agent-contract.md` ("tie-break lexicographic id").

## Failure semantics

A provider failure is a *deterministic failed execution*, never a degraded
success:

```
offline provider → Invoke error → runtime: failed outcome → chain: status failed
                    error.message = "all providers failed: provider <id> is offline"
                    no output, no fabricated completion, correlation id preserved
```

Retry policy (conservative): one extra attempt, only when the provider error
looks transient (`deadline`, `timeout`, and never `offline`). Authorization,
policy, tool-permission and validation failures are never retried — they are
outcomes, not noise.

Offline determinism for tests and demos: `NEXUS_SEEDED_PROVIDER_STATUS=offline`
(PROVIDER_CONTRACTS §12) makes the seeded provider start offline; the gateway
and control plane stay healthy, and every execution fails honestly
(`e2e/tests/15-agent-execution.spec.ts`, `internal/agentexec/retry_test.go`).
## Production provider: `models.providers`

The shipped binary keeps booting its deterministic simulation by default. A
production inference adapter is registered only when `models.providers` is set
in the JSON config passed with `--config` — never as a silent default:

```json
{
  "models": {
    "providers": [{
      "id": "openai-prod",
      "endpoint": "https://api.openai.com/v1",
      "model_id": "gpt-4o-mini",
      "api_key_env": "NEXUS_OPENAI_API_KEY",
      "timeout_ms": 30000,
      "allow_insecure_http": false
    }]
  }
}
```

Registration behavior:

- one OpenAI-compatible Chat Completions adapter per entry, behind the same
  `modelrouter.Provider` interface as every other provider;
- the API key comes from `api_key_env`, read at invoke time — it is never
  serialized into config snapshots, logs, accounting records or errors;
- `endpoint` must be `https`, or `http` **only** with
  `allow_insecure_http: true` and a loopback host (a dev/fixture posture, not
  a production default). It must not embed URL userinfo, a query string or a
  fragment: credentials belong in `api_key_env`, never in the endpoint;
- model invocation honors the request context (deadline + cancellation) and
  the configured `timeout_ms`; hang/length responses are bounded;
- token usage is taken from the provider's `usage` block as-is; nothing is
  fabricated when the provider omits it;
- routing, failover, health demotion, local-first preference and accounting
  stay inside `ModelRouter` — the model itself can never say *"use provider X"*;
- with `models.providers` set, the seeded simulator is NOT registered, so
  production config is an explicit, auditable boundary rather than a
  default-off switch.

When no providers are configured the launcher seeds exactly the deterministic
simulation provider (`simulated:default` or the scripted decision-table
provider behind `NEXUS_SEEDED_PROVIDER_MODE=scripted`), which is what the
shipped E2E suite and offline CI exercise — **never an external API key**.
