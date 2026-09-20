# NEXUS — Development (M6 Model Router + Providers)

This document covers **only** M6: the model router and provider abstraction.
It does not duplicate architecture or contract docs.

> M6 extends M0–M5. It adds the **Model Router, Provider Interface,
> Capability Registry, Health Registry, and Invocation Accounting** —
> the layer that determines which AI model is used, through which provider,
> and when to switch. It does **not** implement memory, attention, or
> full agent lifecycle.

---

## Scope (C12 Model Router)

| Component | What M6 adds |
|---|---|
| **Model Registry** | model definitions, capabilities, pricing, availability, runtime type |
| **Provider Interface** | abstract provider with identify, health, invoke, capabilities |
| **Model Router** | needs-based routing, local-first policy, capability matching, failover |
| **Health Registry** | provider health monitoring, success/failure tracking |
| **Invocation Accounting** | token/cost tracking per model, per business |

### Invariants preserved by M6

- **Routing ≠ Authorization** — routing selects, governance authorizes
- **Local-first** — prefer local models when they meet needs
- **Failover without unsafe duplicates** — try next provider, don't retry same
- **Privacy-aware routing** — respect data retention and external transfer policies
- **Provider credential isolation** — credentials scoped to providers
- **Business isolation** — accounting per business

---

## Layout

```
internal/foundation/modelrouter/
  registry.go         Model definitions, capabilities, pricing
  provider.go         Provider interface, LocalProvider, RemoteProvider
  router.go           Model routing, failover, local-first, accounting
  health.go           Health monitoring, success/failure tracking
  accounting.go       Token/cost accounting per model, per business
  modelrouter_test.go 22 tests (TEST-M6-001..022)
```

---

## Usage

```go
import "github.com/Nomssky/NEXUS/internal/foundation/modelrouter"

// 1. Register models
reg := modelrouter.NewModelRegistry()
reg.RegisterModel(&modelrouter.ModelDefinition{
    ID:           "ollama:qwen3",
    ProviderID:   "ollama",
    Capabilities: []modelrouter.ModelCapability{modelrouter.CapabilityReasoning},
    Runtime:      modelrouter.RuntimeLocal,
    Status:       modelrouter.ModelStatusActive,
})

// 2. Register providers
router := modelrouter.NewModelRouter(reg, modelrouter.RoutingPolicyLocalFirst)
router.RegisterProvider(modelrouter.NewLocalProvider(modelrouter.ProviderConfig{ID: "ollama"}))

// 3. Route a request
decision, err := router.Route(&modelrouter.RoutingRequest{
    RequestID:    "req-1",
    RequiredCaps: []modelrouter.ModelCapability{modelrouter.CapabilityReasoning},
    PreferLocal:  true,
})

// 4. Invoke with failover
resp, decision, err := router.Invoke(routingReq, generateReq)

// 5. Check accounting
tokens := router.GetAccounting().TotalTokens()
```

---

## Testing

- 22 new tests covering TEST-M6-001..022
- M0–M5 tests unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)
