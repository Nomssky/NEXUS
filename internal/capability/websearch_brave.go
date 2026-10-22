package capability

// General web-search provider behind the existing ResearchProvider seam
// (contracts/CAPABILITY_TOOL_CONTRACTS.md §12). The provider uses the
// documented Brave Search Web Search API:
//
//	GET https://api.search.brave.com/res/v1/web/search
//	X-Subscription-Token: <API key>
//
// The adapter consumes only the web result records (title, URL and
// description) and stops there. It never fetches a result page, follows a
// redirect, or turns a search excerpt into page content. Provider-specific
// response parsing stays here; the public web.search contract, evidence
// capture and synthesis paths are unchanged.
//
// The API key is boot-time configuration and is only ever written to the
// provider request header. It is not accepted from model output, is not
// included in errors, and the provider's String method is redacted so that
// diagnostic formatting cannot serialize the credential.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBraveEndpoint is the documented Brave Search Web Search API
	// endpoint. The credential-bearing endpoint is fixed in production; the
	// only override is loopback HTTP for deterministic adapter tests.
	DefaultBraveEndpoint = "https://api.search.brave.com/res/v1/web/search"
	// DefaultBraveUserAgent identifies NEXUS to the provider.
	DefaultBraveUserAgent = "NEXUS-research/1.0 (mediated web.search capability)"

	maxBraveResponseBytes = 512 * 1024
	maxBraveAPIKeyBytes   = 1024
)

// BraveProviderConfig is the runtime configuration for the Brave adapter.
// Every field is boot-time configuration; no model output can influence it.
type BraveProviderConfig struct {
	// APIKey is the Brave Search subscription token. It is required.
	APIKey string
	// Endpoint is optional. Empty uses DefaultBraveEndpoint. A non-default
	// endpoint is accepted only as loopback HTTP when AllowInsecureHTTP is set,
	// so a credential cannot be redirected to an arbitrary host.
	Endpoint string
	// UserAgent is sent on every request. Empty uses the shipped default.
	UserAgent string
	// Timeout bounds the whole provider request. It is clamped to the
	// capability's own limit.
	Timeout time.Duration
	// MaxResponseBytes bounds the provider response body.
	MaxResponseBytes int64
	// AllowInsecureHTTP permits a loopback HTTP fixture endpoint. It never
	// permits a non-loopback or production override.
	AllowInsecureHTTP bool
}

// braveCredential prevents accidental formatting of the API key. The only way
// to recover the secret is an explicit string conversion at the request header.
type braveCredential string

func (braveCredential) String() string { return "[redacted]" }

// BraveSearchProvider is a real external general web-search provider. It
// implements ResearchProvider.
type BraveSearchProvider struct {
	endpoint *url.URL
	apiKey   braveCredential
	agent    string
	client   *http.Client
	maxBytes int64
}

// NewBraveSearchProvider validates configuration at the adapter boundary and
// returns an actionable, sanitized error when it is unusable.
func NewBraveSearchProvider(cfg BraveProviderConfig) (*BraveSearchProvider, error) {
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, fmt.Errorf("%w: Brave Search API key is required (set BRAVE_API_KEY)", ErrCredential)
	}
	if len(key) > maxBraveAPIKeyBytes {
		return nil, fmt.Errorf("%w: Brave Search API key is invalid", ErrCredential)
	}
	for _, r := range key {
		if r <= 0x20 || r >= 0x7f {
			return nil, fmt.Errorf("%w: Brave Search API key is invalid", ErrCredential)
		}
	}

	endpoint, err := braveEndpoint(cfg.Endpoint, cfg.AllowInsecureHTTP)
	if err != nil {
		return nil, err
	}
	agent := strings.TrimSpace(cfg.UserAgent)
	if agent == "" {
		agent = DefaultBraveUserAgent
	}
	if len(agent) > MaxSearchResultSource || strings.ContainsAny(agent, "\r\n") {
		return nil, fmt.Errorf("%w: web search user agent is invalid", ErrValidation)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if timeout > maxSearchTimeout {
		timeout = maxSearchTimeout
	}
	maxBytes := cfg.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = maxBraveResponseBytes
	}
	if maxBytes > 2*maxBraveResponseBytes {
		maxBytes = 2 * maxBraveResponseBytes
	}

	p := &BraveSearchProvider{
		endpoint: endpoint, apiKey: braveCredential(key), agent: agent, maxBytes: maxBytes,
	}
	p.client = &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			// A redirect is not part of the documented API interaction. It
			// could also move the credential-bearing request to another host.
			return fmt.Errorf("%w: web search redirects are not permitted", ErrNetworkBlocked)
		},
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
			TLSHandshakeTimeout:   timeout,
			ResponseHeaderTimeout: timeout,
			DisableKeepAlives:     true,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	return p, nil
}

// braveEndpoint enforces the credential-bearing destination policy: the fixed
// documented Brave endpoint, or loopback HTTP for deterministic tests.
func braveEndpoint(raw string, allowInsecureHTTP bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultBraveEndpoint
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%w: web search endpoint is not a valid URL", ErrValidation)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: web search endpoint must not embed credentials", ErrValidation)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%w: web search endpoint must not include a query or fragment", ErrValidation)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		defaultURL, _ := url.Parse(DefaultBraveEndpoint)
		if !strings.EqualFold(u.Host, defaultURL.Host) || u.EscapedPath() != defaultURL.EscapedPath() {
			return nil, fmt.Errorf("%w: Brave endpoint override is test-only (loopback HTTP)", ErrValidation)
		}
	case "http":
		if !allowInsecureHTTP || !loopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("%w: Brave endpoint override must be loopback HTTP for tests", ErrValidation)
		}
	default:
		return nil, fmt.Errorf("%w: web search endpoint scheme is not permitted", ErrValidation)
	}
	return u, nil
}

// String implements fmt.Stringer so diagnostic formatting cannot expose the
// credential or an endpoint override.
func (p *BraveSearchProvider) String() string { return "BraveSearchProvider{provider=brave}" }

// GoString implements fmt.GoStringer for the same reason; %#v otherwise prints
// every struct field, including the credential.
func (p *BraveSearchProvider) GoString() string {
	return "capability.BraveSearchProvider{provider=brave}"
}

// Identify implements ResearchProvider.
func (p *BraveSearchProvider) Identify() string { return "brave" }

// braveSearchEnvelope is the documented response envelope subset the adapter
// needs. Raw messages keep a malformed record from discarding valid siblings.
type braveSearchEnvelope struct {
	Type  json.RawMessage `json:"type"`
	Query json.RawMessage `json:"query"`
	Web   json.RawMessage `json:"web"`
}

type braveWebResults struct {
	Results json.RawMessage `json:"results"`
}

type braveWebResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// Search implements ResearchProvider. It performs one bounded GET request and
// maps the provider's web results into the existing Source contract.
func (p *BraveSearchProvider) Search(ctx context.Context, query string, limit int) ([]Source, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if err := validateSearchQuery(query); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > MaxSearchResultsPerQuery {
		limit = MaxSearchResultsPerQuery
	}

	requestURL := *p.endpoint
	q := requestURL.Query()
	q.Set("q", query)
	q.Set("count", strconv.Itoa(limit))
	q.Set("safesearch", "moderate")
	requestURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot build web search request", ErrExternal)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", p.agent)
	req.Header.Set("X-Subscription-Token", string(p.apiKey))

	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, ErrNetworkBlocked) {
			return nil, fmt.Errorf("%w: web search redirects are not permitted", ErrNetworkBlocked)
		}
		if errors.Is(err, context.DeadlineExceeded) || searchTimeout(err) {
			return nil, fmt.Errorf("%w: web search provider timed out", ErrTimeout)
		}
		// Never include the transport error verbatim: it can contain the full
		// endpoint or resolver details.
		return nil, fmt.Errorf("%w: web search provider is unavailable", ErrExternal)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// handled below
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: web search provider rejected the credential (HTTP %d)",
			ErrCredential, resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w: web search provider rate limited the request (HTTP 429)", ErrExternal)
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: web search provider is unavailable (HTTP %d)", ErrExternal, resp.StatusCode)
	default:
		return nil, fmt.Errorf("%w: web search provider rejected the request (HTTP %d)", ErrExternal, resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, p.maxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: web search provider response could not be read", ErrExternal)
	}
	if int64(len(raw)) > p.maxBytes {
		return nil, fmt.Errorf("%w: web search provider response exceeds the size limit", ErrResourceLimit)
	}
	var envelope braveSearchEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("%w: web search provider returned malformed JSON", ErrExternal)
	}
	// json.Unmarshal accepts a JSON null into a struct without error; an empty
	// object is likewise not a Brave response.
	if len(envelope.Web) == 0 && len(envelope.Query) == 0 && len(envelope.Type) == 0 {
		return nil, fmt.Errorf("%w: web search provider response is missing the result envelope", ErrExternal)
	}
	if len(envelope.Web) == 0 || isJSONNull(envelope.Web) {
		// A successful, identified Brave response with no web block is an
		// explicit empty result. An unidentified object is malformed, not an
		// empty search.
		if len(envelope.Query) > 0 || braveEnvelopeTypeIsSearch(envelope.Type) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: web search provider response is missing the web results", ErrExternal)
	}
	var web braveWebResults
	if err := json.Unmarshal(envelope.Web, &web); err != nil {
		return nil, fmt.Errorf("%w: web search provider response has an invalid web result block", ErrExternal)
	}
	if isJSONNull(web.Results) || len(web.Results) == 0 {
		return nil, nil
	}
	var records []json.RawMessage
	if err := json.Unmarshal(web.Results, &records); err != nil {
		return nil, fmt.Errorf("%w: web search provider response has invalid result records", ErrExternal)
	}

	out := make([]Source, 0, min(limit, len(records)))
	for _, rawRecord := range records {
		if len(out) >= limit {
			break
		}
		var record braveWebResult
		if err := json.Unmarshal(rawRecord, &record); err != nil {
			continue // malformed record: drop it rather than the whole result set
		}
		title := truncateUTF8Bytes(cleanProviderText(record.Title), MaxSearchResultTitle)
		if title == "" {
			continue // a record without identity cannot be presented as a source
		}
		resultURL, ok := normalizedSearchURL(record.URL)
		if !ok {
			continue // unsafe or malformed result URL
		}
		parsedURL, err := url.Parse(resultURL)
		if err != nil {
			continue
		}
		source := truncateUTF8Bytes(strings.ToLower(parsedURL.Hostname()), MaxSearchResultSource)
		if source == "" {
			continue
		}
		snippet := truncateUTF8Bytes(cleanProviderText(record.Description), MaxSearchResultSnippet)
		out = append(out, Source{
			Title:   title,
			URL:     resultURL,
			Source:  source,
			Snippet: snippet,
			// PublishedAt is deliberately omitted. Brave may return page_age,
			// which is a relative freshness label, not a publication date; the
			// contract does not support claiming one.
		})
	}
	return out, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.EqualFold(strings.TrimSpace(string(raw)), "null")
}

func braveEnvelopeTypeIsSearch(raw json.RawMessage) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil && strings.EqualFold(strings.TrimSpace(value), "search")
}
