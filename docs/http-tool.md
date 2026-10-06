# `http.request` Capability

Manifest-declared operations: `get`, `post`, `put`, `patch`, `delete`.

## Configuration

Runtime flags (never model input):

```go
type HTTPPolicy struct {
    AllowedHosts []string // suffix-match: host == entry || ends_with("."+entry)
    BlockedHosts []string // wins over the allowlist
    AllowInsecureHTTP bool // permits "http"; otherwise only "https"
    AllowedPorts []int  // gateway whitelist; happens per hop
    AllowLoopback bool  // dev. blocked entirely in production
    AllowAnyPublic bool // dangerous: with empty allowlist, permits any public IP
    Production bool
    Timeout time.Duration
    MaxRedirects int
    MaxResponseByte int
    MaxHeaderCount int
    MaxBodyByte int
}
```

Empty allowlist + `AllowAnyPublic=false` (default) = no URL passes.
`AllowAnyPublic=true` is refused in `NEXUS_ENVIRONMENT=production`.

## Per-request flow

1. `validateURL`: scheme must be https/http-with-opt-in; embedded credentials
   (`user:pass@`) blocked; blocked-hosts compared first; allowlist required
   unless `AllowAnyPublic`; port check.
2. `MaxBodyByte` gates oversized signed payloads.
3. `dialGuarded`: blocks non-tcp networks (no `unix`, `file`, `gopher`) and
   `net.DefaultResolver.LookupIPAddr`s the hostname; every address class
   checks negative (loopback unless allowed, IPv4 private/link-local/CGNAT/
   multicast/unspecified, IPv6 ULA) before dialing the first non-invalid
   address.
4. redirects re-validate every target through `CheckRedirect` (bounded by
   `MaxRedirects`).
5. response is bounded by `MaxResponseByte` (tail-marked `truncated=true`),
   `MaxHeaderCount`, context-aware on the parent deadline and cancellation.
6. Sensitive response headers (Authorization, Proxy-Authorization, Cookie,
   Set-Cookie, *token keys) are `redactedMarker`'d; surviving body and
   headers pass through the platform's `redactString` in `Platform.normalize`.

## Adapter-level rule

The model never supplies an `Authorization` value: normally the request uses
an explicit `CredentialHandle` supplied by the platform (through GitHub), and
any ad-hoc `Authorization` header supplied via `input` is ignored for the
general path. The GitHub adapter installs the token from `handle.Secret()`.

## Testing matrix (Go)

- localhost / loopback / private tested by default; loopback opt-in behavior
  (`NEXUS_TOOL_HTTP_ALLOW_LOOPBACK=true/false`) covered.
- redirect-to-private-metadata (`http.StatusFound`) returns ErrNetworkBlocked.
- redirect count ceiling honored.
- oversized body → external error (never truncates the timeout).
- cancellation error surface: `context.Canceled`.
- UDP/UNIX/SOCKS schemes reject before network access.
- `url.Error` from rebinding attacker surface is preserved in the error
  chain for forensics, but never contains the target word.
