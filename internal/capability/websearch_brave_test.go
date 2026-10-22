package capability

// Deterministic adapter tests for the Brave general web-search provider.
// These use a local HTTP fixture and verify request construction, provider
// mapping, bounds, credential handling and failure classification. They do not
// claim live external retrieval; that is the opt-in smoke test.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testBraveKey = "brave-test-key-0123456789"

func braveProviderFor(t *testing.T, handler http.Handler, maxBytes int64) (*BraveSearchProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p, err := NewBraveSearchProvider(BraveProviderConfig{
		APIKey: testBraveKey, Endpoint: srv.URL + "/res/v1/web/search", Timeout: 2 * time.Second,
		MaxResponseBytes: maxBytes, AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("provider config: %v", err)
	}
	return p, srv
}

func TestBraveProviderMapsProviderResponse(t *testing.T) {
	var gotQuery map[string]string
	var gotKey, gotAgent string
	p, _ := braveProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Subscription-Token")
		gotAgent = r.Header.Get("User-Agent")
		gotQuery = map[string]string{
			"q":          r.URL.Query().Get("q"),
			"count":      r.URL.Query().Get("count"),
			"safesearch": r.URL.Query().Get("safesearch"),
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"type":"search","query":{"original":"nexus runtime"},"web":{"type":"search","results":[`+
			`{"title":"Nexus &amp; runtime","url":"https://WWW.Example.com/nexus?x=1","description":"A <b>general</b> web excerpt","page_age":"2 days ago","meta_url":{"hostname":"example.com"}},`+
			`{"title":"Second result","url":"https://docs.example.org/second","description":null}]}}`)
	}), 0)

	sources, err := p.Search(context.Background(), "nexus runtime", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if gotKey != testBraveKey {
		t.Fatalf("subscription header not sent")
	}
	if !strings.Contains(gotAgent, "NEXUS-research") {
		t.Fatalf("user agent not sent: %q", gotAgent)
	}
	if gotQuery["q"] != "nexus runtime" || gotQuery["count"] != "2" || gotQuery["safesearch"] != "moderate" {
		t.Fatalf("unexpected provider query: %v", gotQuery)
	}
	if len(sources) != 2 {
		t.Fatalf("want 2 sources, got %d", len(sources))
	}
	first := sources[0]
	if first.Title != "Nexus & runtime" ||
		first.URL != "https://WWW.Example.com/nexus?x=1" ||
		first.Source != "www.example.com" ||
		first.Snippet != "A general web excerpt" {
		t.Fatalf("provider mapping wrong: %+v", first)
	}
	if first.PublishedAt != "" || sources[1].PublishedAt != "" {
		t.Fatalf("publication date must not be invented: %q %q", first.PublishedAt, sources[1].PublishedAt)
	}
	if sources[1].Snippet != "" {
		t.Fatalf("missing description must stay empty: %q", sources[1].Snippet)
	}
}

func TestBraveProviderEmptyResultIsExplicit(t *testing.T) {
	for _, body := range []string{
		`{"type":"search","query":{"original":"no match"},"web":{"type":"search","results":[]}}`,
		`{"type":"search","query":{"original":"no match"}}`,
		`{"type":"search","web":null}`,
	} {
		p, _ := braveProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, body)
		}), 0)
		sources, err := p.Search(context.Background(), "no match", 5)
		if err != nil {
			t.Fatalf("empty result %s must be valid: %v", body, err)
		}
		if len(sources) != 0 {
			t.Fatalf("empty result fabricated %d sources", len(sources))
		}
	}
}

func TestBraveProviderSkipsUnusableRecords(t *testing.T) {
	p, _ := braveProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"type":"search","web":{"results":[`+
			`{},`+
			`{"title":"","url":"https://example.com/no-title","description":"x"},`+
			`{"title":"No URL","description":"x"},`+
			`{"title":"Bad scheme","url":"javascript:alert(1)","description":"x"},`+
			`{"title":"Private URL","url":"http://127.0.0.1/private","description":"x"},`+
			`{"title":"Userinfo URL","url":"https://user:pass@example.com/x","description":"x"},`+
			`{"title":"Relative URL","url":"/relative","description":"x"},`+
			`{"title":42,"url":"https://example.com/numeric-title","description":"x"},`+
			`{"title":"Good","url":"https://example.com/good","description":"kept"}]}}`)
	}), 0)
	sources, err := p.Search(context.Background(), "q", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(sources) != 1 || sources[0].Title != "Good" || sources[0].URL != "https://example.com/good" {
		t.Fatalf("unusable record handling wrong: %+v", sources)
	}
}

func TestBraveProviderBoundsResultsAndFields(t *testing.T) {
	longTitle := strings.Repeat("T", MaxSearchResultTitle+100)
	longSnippet := strings.Repeat("界", MaxSearchResultSnippet)
	records := make([]string, 0, 12)
	records = append(records, fmt.Sprintf(`{"title":%q,"url":"https://example.com/one","description":%q}`,
		longTitle, longSnippet))
	for i := 1; i < 12; i++ {
		records = append(records, fmt.Sprintf(`{"title":"result %d","url":"https://example.com/%d","description":"s"}`, i, i))
	}
	p, _ := braveProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"type":"search","web":{"results":[%s]}}`, strings.Join(records, ","))
	}), 0)
	sources, err := p.Search(context.Background(), "q", 99)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(sources) != MaxSearchResultsPerQuery {
		t.Fatalf("result count not bounded: %d", len(sources))
	}
	if len(sources[0].Title) > MaxSearchResultTitle || len(sources[0].Snippet) > MaxSearchResultSnippet {
		t.Fatalf("fields not bounded: title=%d snippet=%d", len(sources[0].Title), len(sources[0].Snippet))
	}
	if !strings.HasPrefix(sources[0].Snippet, "界") || strings.Contains(sources[0].Snippet, "\ufffd") {
		t.Fatal("truncation split a UTF-8 rune")
	}
}

func TestBraveProviderUnicodeQueryAndBounds(t *testing.T) {
	var gotQuery string
	p, _ := braveProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		fmt.Fprint(w, `{"type":"search","web":{"results":[{"title":"結果","url":"https://example.com/界","description":"説明"}]}}`)
	}), 0)
	sources, err := p.Search(context.Background(), "検索 クエリ", 1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if gotQuery != "検索 クエリ" || len(sources) != 1 || sources[0].Snippet != "説明" {
		t.Fatalf("unicode handling wrong: query=%q sources=%+v", gotQuery, sources)
	}
}

func TestBraveProviderHTTPFailures(t *testing.T) {
	cases := []struct {
		status int
		kind   error
		want   string
	}{
		{http.StatusUnauthorized, ErrCredential, "credential"},
		{http.StatusForbidden, ErrCredential, "credential"},
		{http.StatusTooManyRequests, ErrExternal, "rate limited"},
		{http.StatusInternalServerError, ErrExternal, "unavailable"},
		{http.StatusServiceUnavailable, ErrExternal, "unavailable"},
		{http.StatusBadRequest, ErrExternal, "rejected"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			p, _ := braveProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `{"detail":%q}`, testBraveKey)
			}), 0)
			_, err := p.Search(context.Background(), "q", 5)
			if err == nil || !errors.Is(err, tc.kind) {
				t.Fatalf("want %v, got %v", tc.kind, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not explain %q", err, tc.want)
			}
			if strings.Contains(err.Error(), testBraveKey) || strings.Contains(err.Error(), "detail") {
				t.Fatalf("credential or response body leaked: %v", err)
			}
		})
	}
}

func TestBraveProviderRejectsMalformedResponses(t *testing.T) {
	cases := map[string]string{
		"malformed json":       `{`,
		"wrong top-level type": `[]`,
		"null top-level":       `null`,
		"empty object":         `{}`,
		"missing web results":  `{"foo":"bar"}`,
		"invalid web block":    `{"web":[]}`,
		"invalid records":      `{"web":{"results":"nope"}}`,
		"provider error":       `{"error":{"code":"SUBSCRIPTION_TOKEN_INVALID","detail":"secret detail"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			p, _ := braveProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			}), 0)
			_, err := p.Search(context.Background(), "q", 5)
			if err == nil || !errors.Is(err, ErrExternal) {
				t.Fatalf("want external error, got %v", err)
			}
			if strings.Contains(err.Error(), "secret detail") || strings.Contains(err.Error(), testBraveKey) {
				t.Fatalf("provider detail leaked: %v", err)
			}
		})
	}
}

func TestBraveProviderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, `{"type":"search","web":{"results":[]}}`)
	}))
	defer srv.Close()
	p, err := NewBraveSearchProvider(BraveProviderConfig{
		APIKey: testBraveKey, Endpoint: srv.URL, Timeout: 30 * time.Millisecond,
		AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Search(context.Background(), "q", 5)
	if err == nil || !errors.Is(err, ErrTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
}

func TestBraveProviderCancellationAndDeadline(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)
	p, err := NewBraveSearchProvider(BraveProviderConfig{
		APIKey: testBraveKey, Endpoint: srv.URL, Timeout: 2 * time.Second, AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := p.Search(ctx, "q", 5); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}

	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if _, err := p.Search(deadline, "q", 5); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context deadline, got %v", err)
	}
}

func TestBraveProviderResponseSizeLimit(t *testing.T) {
	p, _ := braveProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 1024))
	}), 64)
	_, err := p.Search(context.Background(), "q", 5)
	if err == nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("want resource limit, got %v", err)
	}
}

func TestBraveProviderRejectsRedirects(t *testing.T) {
	var reached int32
	p, _ := braveProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&reached, 1) == 1 {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}
		fmt.Fprint(w, `{"type":"search","web":{"results":[]}}`)
	}), 1024)
	_, err := p.Search(context.Background(), "q", 5)
	if err == nil || !errors.Is(err, ErrNetworkBlocked) {
		t.Fatalf("redirect must fail closed, got %v", err)
	}
	if atomic.LoadInt32(&reached) != 1 {
		t.Fatalf("redirect target must not be contacted")
	}
}

func TestBraveProviderQueryValidation(t *testing.T) {
	var calls int32
	p, _ := braveProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		fmt.Fprint(w, `{"type":"search","web":{"results":[]}}`)
	}), 0)
	queries := []string{"", "   ", strings.Repeat("q", MaxSearchQueryBytes+1), "bad\nquery", "https://example.com/private"}
	for _, q := range queries {
		if _, err := p.Search(context.Background(), q, 5); err == nil || !errors.Is(err, ErrValidation) {
			t.Fatalf("query %q must be rejected, got %v", q, err)
		}
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("invalid queries must not reach the provider")
	}
}

func TestBraveProviderConfigValidation(t *testing.T) {
	loopback := "http://127.0.0.1:1234/res/v1/web/search"
	valid := []BraveProviderConfig{
		{APIKey: testBraveKey},
		{APIKey: testBraveKey, Endpoint: DefaultBraveEndpoint},
		{APIKey: testBraveKey, Endpoint: loopback, AllowInsecureHTTP: true},
	}
	for i, cfg := range valid {
		if _, err := NewBraveSearchProvider(cfg); err != nil {
			t.Fatalf("valid config %d rejected: %v", i, err)
		}
	}

	cases := []struct {
		cfg  BraveProviderConfig
		kind error
	}{
		{cfg: BraveProviderConfig{}, kind: ErrCredential},
		{cfg: BraveProviderConfig{APIKey: "   "}, kind: ErrCredential},
		{cfg: BraveProviderConfig{APIKey: "bad\nkey"}, kind: ErrCredential},
		{cfg: BraveProviderConfig{APIKey: strings.Repeat("k", maxBraveAPIKeyBytes+1)}, kind: ErrCredential},
		{cfg: BraveProviderConfig{APIKey: testBraveKey, Endpoint: "ftp://example.com/res/v1/web/search"}, kind: ErrValidation},
		{cfg: BraveProviderConfig{APIKey: testBraveKey, Endpoint: "https://user:pass@api.search.brave.com/res/v1/web/search"}, kind: ErrValidation},
		{cfg: BraveProviderConfig{APIKey: testBraveKey, Endpoint: "https://example.com/res/v1/web/search"}, kind: ErrValidation},
		{cfg: BraveProviderConfig{APIKey: testBraveKey, Endpoint: loopback}, kind: ErrValidation},
		{cfg: BraveProviderConfig{APIKey: testBraveKey, Endpoint: "http://example.com/res/v1/web/search", AllowInsecureHTTP: true}, kind: ErrValidation},
		{cfg: BraveProviderConfig{APIKey: testBraveKey, Endpoint: DefaultBraveEndpoint + "?x=1"}, kind: ErrValidation},
		{cfg: BraveProviderConfig{APIKey: testBraveKey, UserAgent: "bad\r\nagent"}, kind: ErrValidation},
	}
	for i, tc := range cases {
		_, err := NewBraveSearchProvider(tc.cfg)
		if err == nil || !errors.Is(err, tc.kind) {
			t.Fatalf("config %d: want %v, got %v", i, tc.kind, err)
		}
		if strings.Contains(err.Error(), testBraveKey) {
			t.Fatalf("config %d leaked the credential: %v", i, err)
		}
	}
}

func TestBraveProviderRedactsCredentialInDiagnostics(t *testing.T) {
	p, err := NewBraveSearchProvider(BraveProviderConfig{APIKey: testBraveKey})
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{
		fmt.Sprint(p),
		fmt.Sprintf("%v", p),
		fmt.Sprintf("%+v", p),
		fmt.Sprintf("%#v", p),
	} {
		if strings.Contains(rendered, testBraveKey) {
			t.Fatalf("provider formatting exposed the credential: %s", rendered)
		}
	}
}
