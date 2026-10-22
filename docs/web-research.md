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

## Real providers

`WikipediaSearchProvider` and `BraveSearchProvider` are the shipped real
external backends. Each implements the same `ResearchProvider` seam and is
selected explicitly at boot; provider-specific parsing never leaks into the
capability platform, evidence schema or synthesis code.

### Brave general web search

`BraveSearchProvider` calls the documented Brave Search Web Search API
(`GET https://api.search.brave.com/res/v1/web/search`, authenticated with the
`X-Subscription-Token` header). It maps only the provider's web-result `title`,
`url` and `description` into `Source`, derives the source label from the result
URL host, and stops there. It never fetches result pages, follows redirects,
uses Brave's rich/local enrichment endpoints, or maps `page_age` (a relative
freshness label) onto the publication-date field.

```sh
NEXUS_CAPABILITY_WEB=brave
BRAVE_API_KEY=...                                                 # required
NEXUS_CAPABILITY_WEB_USER_AGENT='NEXUS-research/1.0'             # optional
```

The API key is read only from `BRAVE_API_KEY`. It is sent only in the provider
request header; it is never logged, persisted, included in errors or exposed in
result metadata. Startup fails with a credential error when the key is missing
or malformed. The endpoint defaults to the fixed documented Brave endpoint; an
endpoint override is accepted only as loopback HTTP with
`NEXUS_CAPABILITY_WEB_ALLOW_INSECURE=true` for deterministic tests, so a
credential-bearing request cannot be redirected to an arbitrary host.

### Wikipedia

`WikipediaSearchProvider` calls the documented MediaWiki Action API
(`action=query&list=search`) and maps only the search-result title, canonical
Wikipedia page URL, host label and search excerpt into `Source`. It does not
fetch result pages, follow redirects, accept model-supplied endpoints, or
invent publication dates (the provider timestamp is a last-edit time, not a
publication date).

Boot-time configuration:

```sh
NEXUS_CAPABILITY_WEB=wikipedia
NEXUS_CAPABILITY_WEB_ENDPOINT=https://en.wikipedia.org/w/api.php  # optional
NEXUS_CAPABILITY_WEB_LANGUAGE=en                                  # optional
NEXUS_CAPABILITY_WEB_USER_AGENT='NEXUS-research/1.0'             # optional
```

An empty `NEXUS_CAPABILITY_WEB` disables `web.search`; `scripted` selects the
test provider; `wikipedia` and `brave` select the real external providers. Any
other non-empty value fails boot. The Wikipedia endpoint must be HTTPS unless
it is loopback HTTP with `NEXUS_CAPABILITY_WEB_ALLOW_INSECURE=true` (test-only).
Provider failures are returned as classified errors and never fall back to
another provider, the scripted provider, or synthetic evidence. The safe
`tool.registered` diagnostic names the selected provider for `web.search`
without exposing credentials or endpoints.

The adapters bound queries to 512 bytes and accept at most 10 provider
records. They reject control characters and URL-shaped queries, validate every
result URL as absolute HTTP(S) with a public host and no embedded credentials,
drop unusable records individually, and bound title, URL, source and snippet
fields plus the complete envelope. HTTP 401/403 is reported as a credential
failure, 429 as a rate limit, 5xx as provider unavailability, and malformed
JSON or record envelopes as external errors. Errors omit the provider response
body and transport details, so credentials or internal endpoints cannot leak
through a failure.

The deterministic Research E2E boots the compiled binary with
`NEXUS_CAPABILITY_WEB=wikipedia` against a local MediaWiki-shaped HTTP fixture
and with `NEXUS_CAPABILITY_WEB=brave` against a local Brave-shaped HTTP
fixture, so both real adapters, launcher wiring, normalization, durable
evidence and UI are exercised without external network access. Live retrieval
is covered by the opt-in smoke tests described below.

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

The provider-only smoke verifies the Brave adapter against the real documented
endpoint without invoking a model:

```sh
NEXUS_LIVE_WEB_SMOKE=1 \
NEXUS_LIVE_WEB_PROVIDER=brave \
BRAVE_API_KEY=... \
go test ./internal/capability -run TestLiveBraveProviderSmoke -count=1 -v
```

It reports provider identity and test mode, distinguishes a reached-but-empty
result from returned evidence, and sends records through the production
`WebSearchTool` normalization path. It never substitutes a local fixture for
live validation.

The full live smoke runs a real OpenAI-compatible model and the explicitly
selected real provider through the same mediated platform and intelligence
loop:

```sh
NEXUS_LIVE_PROVIDER_SMOKE=1 \
TOKENHARBOR_API_KEY=... \
NEXUS_LIVE_WEB_PROVIDER=brave \
BRAVE_API_KEY=... \
go test ./internal/agentintel -run TestLiveProviderEvidenceGroundedSynthesis -count=1 -v
```

With `NEXUS_LIVE_WEB_PROVIDER` empty or set to `wikipedia` (the
backward-compatible default), the test uses Wikipedia and the optional
`NEXUS_LIVE_WEB_ENDPOINT` override. With `brave`, it requires `BRAVE_API_KEY`
and uses the fixed documented Brave endpoint; there is no provider fallback.
The test requires a two-step dependent plan, two real provider searches, v2
evidence, and a typed synthesis citing sources from both steps. Both live
tests are skipped unless explicitly enabled and make bounded external requests.
