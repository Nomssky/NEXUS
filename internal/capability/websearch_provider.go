package capability

// Real external web-search provider behind the existing ResearchProvider seam
// (contracts/CAPABILITY_TOOL_CONTRACTS.md §12). The provider uses the public
// MediaWiki Action API (list=search) — a documented external search interface.
// It never fetches a result page: the adapter maps the provider's search
// response into the bounded Source records and stops there. A snippet is a
// search-result excerpt, not page content.
//
// Provider-specific code lives here; the capability contract and the mediated
// invocation path in platform.go are unchanged. The endpoint and language are
// runtime configuration, never model input. The API is keyless, so no
// credential can leak from this adapter; errors still deliberately omit the
// provider response body in case an operator points it at an authenticated
// mirror.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultWikipediaEndpoint is the documented MediaWiki Action API endpoint.
	DefaultWikipediaEndpoint = "https://en.wikipedia.org/w/api.php"
	// DefaultWikipediaLanguage selects the result URL host.
	DefaultWikipediaLanguage = "en"
	// DefaultWikipediaUserAgent identifies NEXUS to the provider. MediaWiki
	// asks clients for a descriptive User-Agent.
	DefaultWikipediaUserAgent = "NEXUS-research/1.0 (mediated web.search capability)"

	// Search input/output bounds, aligned with the manifest and the durable
	// evidence schema. They are deliberately enforced here as well so a
	// provider response can never grow the envelope past what the journal
	// accepts.
	MaxSearchQueryBytes      = 512
	MaxSearchResultTitle     = 512
	MaxSearchResultURL       = 2048
	MaxSearchResultSource    = 256
	MaxSearchResultSnippet   = 1024
	MaxSearchResultsPerQuery = 10
	maxWikipediaResponseByte = 512 * 1024
	maxSearchTimeout         = 15 * time.Second
)

// WikipediaProviderConfig is the runtime configuration for the real provider.
// Every field is boot-time configuration; no model output can influence it.
type WikipediaProviderConfig struct {
	// Endpoint is the full Action API URL. Empty uses Language to build the
	// canonical endpoint.
	Endpoint string
	// Language is the Wikipedia language code used for result URLs and the
	// default endpoint (default "en").
	Language string
	// UserAgent is sent on every request. Empty uses the shipped default.
	UserAgent string
	// Timeout bounds the whole provider request. It is clamped to the
	// capability's own limit.
	Timeout time.Duration
	// MaxResponseBytes bounds the provider response body.
	MaxResponseBytes int64
	// AllowInsecureHTTP permits a plain-HTTP endpoint for loopback fixture
	// tests only. NewWikipediaSearchProvider refuses non-loopback HTTP even
	// when this is set.
	AllowInsecureHTTP bool
}

// WikipediaSearchProvider is a real, external, keyless search provider. It
// implements ResearchProvider.
type WikipediaSearchProvider struct {
	endpoint *url.URL
	language string
	agent    string
	client   *http.Client
	maxBytes int64
}

var wikipediaLanguagePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,15}$`)
var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

// NewWikipediaSearchProvider validates configuration at the adapter boundary
// and returns an actionable, sanitized error when it is unusable.
func NewWikipediaSearchProvider(cfg WikipediaProviderConfig) (*WikipediaSearchProvider, error) {
	language := strings.ToLower(strings.TrimSpace(cfg.Language))
	if language == "" {
		language = DefaultWikipediaLanguage
	}
	if !wikipediaLanguagePattern.MatchString(language) {
		return nil, fmt.Errorf("%w: web search language must be a short language code", ErrValidation)
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = "https://" + language + ".wikipedia.org/w/api.php"
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%w: web search endpoint is not a valid URL", ErrValidation)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: web search endpoint must not embed credentials", ErrValidation)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !cfg.AllowInsecureHTTP || !loopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("%w: web search endpoint must be https (http is test-only for loopback)", ErrValidation)
		}
	default:
		return nil, fmt.Errorf("%w: web search endpoint scheme is not permitted", ErrValidation)
	}
	agent := strings.TrimSpace(cfg.UserAgent)
	if agent == "" {
		agent = DefaultWikipediaUserAgent
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
		maxBytes = maxWikipediaResponseByte
	}
	if maxBytes > 2*maxWikipediaResponseByte {
		maxBytes = 2 * maxWikipediaResponseByte
	}
	p := &WikipediaSearchProvider{
		endpoint: u, language: language, agent: agent, maxBytes: maxBytes,
	}
	p.client = &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			// The provider endpoint is operator-configured. A redirect is not
			// part of the documented API interaction, so refusing it keeps the
			// egress destination fixed.
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

// Identify implements ResearchProvider.
func (p *WikipediaSearchProvider) Identify() string { return "wikipedia" }

func loopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// wikiSearchResponse is the subset of the documented MediaWiki response the
// adapter consumes. Unknown fields are ignored.
type wikiSearchResponse struct {
	BatchComplete bool `json:"batchcomplete"`
	Error         *struct {
		Code string `json:"code"`
	} `json:"error"`
	Query *struct {
		Search *[]struct {
			Title   string `json:"title"`
			PageID  int64  `json:"pageid"`
			Snippet string `json:"snippet"`
		} `json:"search"`
	} `json:"query"`
}

// Search implements ResearchProvider. It performs one bounded HTTPS request
// and maps the provider's records into the existing Source contract.
func (p *WikipediaSearchProvider) Search(ctx context.Context, query string, limit int) ([]Source, error) {
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
	q.Set("action", "query")
	q.Set("list", "search")
	q.Set("srsearch", query)
	q.Set("srlimit", strconv.Itoa(limit))
	q.Set("srnamespace", "0")
	q.Set("srprop", "snippet|pageid|title")
	q.Set("format", "json")
	q.Set("formatversion", "2")
	q.Set("utf8", "1")
	requestURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot build web search request", ErrExternal)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", p.agent)

	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
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
	var parsed wikiSearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("%w: web search provider returned malformed JSON", ErrExternal)
	}
	if parsed.Error != nil {
		code := sanitizeProviderCode(parsed.Error.Code)
		if code == "" {
			code = "provider_error"
		}
		return nil, fmt.Errorf("%w: web search provider reported an error (%s)", ErrExternal, code)
	}
	if parsed.Query == nil || parsed.Query.Search == nil {
		// A complete response with no query/search member is malformed. It is
		// not an empty result set; silently treating it as one would fabricate
		// a successful empty search.
		return nil, fmt.Errorf("%w: web search provider response is missing the search result", ErrExternal)
	}

	out := make([]Source, 0, len(*parsed.Query.Search))
	for _, item := range *parsed.Query.Search {
		if len(out) >= limit {
			break
		}
		title := truncateUTF8Bytes(cleanProviderText(item.Title), MaxSearchResultTitle)
		if title == "" {
			continue // a record without identity cannot be presented as a source
		}
		rawURL := canonicalWikipediaURL(p.language, item.Title)
		if _, ok := normalizedSearchURL(rawURL); !ok {
			continue
		}
		snippet := truncateUTF8Bytes(snippetText(item.Snippet), MaxSearchResultSnippet)
		out = append(out, Source{
			Title:   title,
			URL:     rawURL,
			Source:  p.language + ".wikipedia.org",
			Snippet: snippet,
			// PublishedAt is deliberately omitted: the provider timestamp is a
			// last-edit time, not a publication date, and the contract does
			// not support claiming one.
		})
	}
	return out, nil
}

func validateSearchQuery(query string) error {
	if query == "" {
		return fmt.Errorf("%w: query is required", ErrValidation)
	}
	if len(query) > MaxSearchQueryBytes {
		return fmt.Errorf("%w: query exceeds %d bytes", ErrValidation, MaxSearchQueryBytes)
	}
	for _, r := range query {
		if r == '\x00' || r == '\r' || r == '\n' || (r < 0x20 && r != '\t') || r == 0x7f {
			return fmt.Errorf("%w: query contains control characters", ErrValidation)
		}
	}
	// A URL-shaped query is not a search expression. Refusing it keeps the
	// adapter from being usable as a URL-fetching primitive by accident.
	if u, err := url.Parse(query); err == nil && u.Host != "" &&
		(strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
		return fmt.Errorf("%w: query must be a search expression, not a URL", ErrValidation)
	}
	return nil
}

func canonicalWikipediaURL(language, title string) string {
	// The canonical page URL is deterministic from the provider-returned
	// title. It is not a claim that the page was fetched. url.URL.String
	// escapes each path segment while keeping the /wiki/ prefix readable.
	return (&url.URL{
		Scheme: "https",
		Host:   language + ".wikipedia.org",
		Path:   "/wiki/" + strings.ReplaceAll(strings.TrimSpace(title), " ", "_"),
	}).String()
}

func snippetText(raw string) string {
	// MediaWiki wraps matches in <span class="searchmatch">. Strip markup,
	// decode entities and collapse whitespace: the result stays the provider's
	// search excerpt, never page content.
	text := html.UnescapeString(htmlTagPattern.ReplaceAllString(raw, " "))
	return collapseWhitespace(text)
}

func cleanProviderText(value string) string {
	return collapseWhitespace(html.UnescapeString(htmlTagPattern.ReplaceAllString(value, " ")))
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateUTF8Bytes(value string, max int) string {
	if len(value) <= max {
		return value
	}
	// Cut at a rune boundary so a truncated provider field is still valid
	// UTF-8. The evidence schema bounds bytes, not runes.
	cut := max
	for cut > 0 && value[cut]&0xc0 == 0x80 {
		cut--
	}
	return value[:cut]
}

func searchTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func sanitizeProviderCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	var b strings.Builder
	for _, r := range code {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
		if b.Len() >= 64 {
			break
		}
	}
	return b.String()
}

// normalizedSearchURL validates a provider-supplied result URL for the
// presentation-facing contract. Only absolute HTTP(S) URLs with a public host
// and no embedded credentials are accepted. The adapter never fetches these
// URLs; this is link safety plus SSRF hygiene, not a fetch primitive.
func normalizedSearchURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > MaxSearchResultURL {
		return "", false
	}
	if strings.ContainsAny(raw, "\x00\r\n") || strings.Contains(raw, " | ") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Opaque != "" {
		return "", false
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") {
		return "", false
	}
	if ip := net.ParseIP(host); ip != nil && !publicSearchAddress(ip) {
		return "", false
	}
	return u.String(), true
}

func publicSearchAddress(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0, v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127,
			v4[0] == 198 && (v4[1] == 18 || v4[1] == 19), v4[0] >= 224:
			return false
		}
	}
	return true
}
