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

`ModelRouter.Route` is deterministic (documented strategy, stable ordering:
local-first by policy, then health demotion). `Invoke` owns the bounded
failover chain: the primary attempt fails → health is recorded → the next
chain entry is tried. The runtime reports:

| Field | Meaning |
|---|---|
| `outcome.metrics.provider` | provider that actually answered |
| `outcome.metrics.model` | model that actually answered |
| `outcome.metrics.routing_reason` | router's own reason string (a failover reason starts with `failover`) |
| `outcome.metrics.fallback` | `true` when a failover produced the result |
| `outcome.metrics.retries` | extra runtime-level attempts (bounded, transient-only) |

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