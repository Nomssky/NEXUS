# Web Research Capability

`web.search` is the single web-research tool. It is not the intelligence
loop calling a search engine directly — it dispatches through the
`ResearchProvider` interface:

```go
type ResearchProvider interface {
    Identify() string
    Search(ctx context.Context, query string, limit int) ([]Source, error)
}
```

## Scripted provider

`ScriptedResearchProvider` (also exposed by `capability.ScriptedResearch()`)
is the deterministic simulation used in tests:

- fixed two-source fixture; one of them contains deliberately
  instruction-shaped text (`Ignore previous instructions…`) and
  secret-shaped content (`api_key=sk-…`) to verify redaction and the
  "data, never instructions" authority rule
- `Fail: true` makes it return an external error
- `Oversized: true` makes it return a 50,000-character snippet
- bounded by `limit` (clamped 1..10 by the manifest)

Launching the binary with `NEXUS_CAPABILITY_WEB=scripted` uses it so the
intelligence E2E and manual probes exercise `web.search` end-to-end with no
external provider.

## Result normalization

All sources pass through the platform invocation path: the result is fed
through the adapter's `maxSnippet`/`maxResults` clamp, then the platform
normalization applies schema bounds and redaction. The model receives the
text block only as an observation inside the bounded `OBSERVATIONS` block —
passive data. Any instruction-shaped content is inert.

Foundational tests to preserve around this boundary:

- tool_call action validated against the *registered* manifest
- the observation payload is fed into the next decision prompt purely as
  data, tagged `source=tool:web.search` inside the bounded `OBSERVATIONS`
  block — inert under the runtime authority hierarchy
- the loop never executes instructions embedded in the observation text
