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

## Real provider

`WikipediaSearchProvider` is the shipped real external backend. It calls the
documented MediaWiki Action API (`action=query&list=search`) and maps only the
search-result title, canonical Wikipedia page URL, host label and search
excerpt into `Source`. It does not fetch result pages, follow redirects, accept
model-supplied endpoints, or invent publication dates (the provider timestamp
is a last-edit time, not a publication date).

Boot-time configuration:

```sh
NEXUS_CAPABILITY_WEB=wikipedia
NEXUS_CAPABILITY_WEB_ENDPOINT=https://en.wikipedia.org/w/api.php  # optional
NEXUS_CAPABILITY_WEB_LANGUAGE=en                                  # optional
NEXUS_CAPABILITY_WEB_USER_AGENT='NEXUS-research/1.0'             # optional
```

An empty `NEXUS_CAPABILITY_WEB` disables `web.search`; `scripted` selects the
test provider; any other value fails boot. The endpoint must be HTTPS unless it
is loopback HTTP with `NEXUS_CAPABILITY_WEB_ALLOW_INSECURE=true` (test-only).
Provider failures are returned as classified errors and never fall back to the
scripted provider or synthetic evidence.

The adapter bounds queries to 512 bytes and accepts at most 10 provider
records. It rejects control characters and URL-shaped queries, validates every
result URL as absolute HTTP(S) with a public host and no embedded credentials,
drops unusable records individually, and bounds title, URL, source and snippet
fields plus the complete envelope. Errors omit the provider response body and
transport details, so credentials or internal endpoints cannot leak through a
failure.

The deterministic Research E2E boots the compiled binary with
`NEXUS_CAPABILITY_WEB=wikipedia` against a local MediaWiki-shaped HTTP fixture,
so the real provider, launcher wiring, normalization, durable evidence and UI
are exercised without external network access. Live retrieval is covered by
the opt-in smoke test described below.

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

## Live smoke (opt-in)

The live smoke runs a real OpenAI-compatible model and the real Wikipedia
provider through the same mediated platform and intelligence loop:

```sh
NEXUS_LIVE_PROVIDER_SMOKE=1 \
TOKENHARBOR_API_KEY=... \
go test ./internal/agentintel -run TestLiveProviderEvidenceGroundedSynthesis -count=1 -v
```

Optional overrides are `NEXUS_LIVE_PROVIDER_URL`, `NEXUS_LIVE_MODEL` and
`NEXUS_LIVE_WEB_ENDPOINT`. The test requires a two-step dependent plan, two
real provider searches, v2 evidence from canonical Wikipedia URLs, and a typed
synthesis citing sources from both steps. It is skipped unless explicitly
enabled.
