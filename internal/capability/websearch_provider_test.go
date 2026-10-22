package capability

// Adapter tests for the real MediaWiki search provider. These use a local HTTP
// test server: they verify provider mapping, bounds and failure handling, not
// live external retrieval. Live retrieval is the separate opt-in smoke test.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func wikiProviderFor(t *testing.T, handler http.Handler, maxBytes int64) (*WikipediaSearchProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p, err := NewWikipediaSearchProvider(WikipediaProviderConfig{
		Endpoint: srv.URL + "/w/api.php", Language: "en", Timeout: 2 * time.Second,
		MaxResponseBytes: maxBytes, AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("provider config: %v", err)
	}
	return p, srv
}

func TestWikipediaProviderMapsProviderResponse(t *testing.T) {
	var gotQuery url.Values
	p, _ := wikiProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"batchcomplete":true,"query":{"searchinfo":{"totalhits":2},"search":[`+
			`{"title":"Nexus (software)","pageid":41,"snippet":"The <span class=\"searchmatch\">Nexus</span> runtime &amp; tools"},`+
			`{"title":"Second result","pageid":42,"snippet":"plain excerpt"}]}}`)
	}), 0)

	sources, err := p.Search(context.Background(), "nexus runtime", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("want 2 sources, got %d", len(sources))
	}
	if gotQuery.Get("action") != "query" || gotQuery.Get("list") != "search" ||
		gotQuery.Get("srsearch") != "nexus runtime" || gotQuery.Get("srlimit") != "2" ||
		gotQuery.Get("srnamespace") != "0" || gotQuery.Get("format") != "json" {
		t.Fatalf("unexpected provider query: %v", gotQuery)
	}
	first := sources[0]
	if first.Title != "Nexus (software)" ||
		first.URL != "https://en.wikipedia.org/wiki/Nexus_%28software%29" ||
		first.Source != "en.wikipedia.org" ||
		first.Snippet != "The Nexus runtime & tools" {
		t.Fatalf("provider mapping wrong: %+v", first)
	}
	if first.PublishedAt != "" {
		t.Fatalf("publication date must not be invented: %q", first.PublishedAt)
	}
}

func TestWikipediaProviderEmptyResultIsExplicit(t *testing.T) {
	p, _ := wikiProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"batchcomplete":true,"query":{"searchinfo":{"totalhits":0},"search":[]}}`)
	}), 0)
	sources, err := p.Search(context.Background(), "no such thing", 5)
	if err != nil {
		t.Fatalf("empty result must be valid: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("empty result fabricated %d sources", len(sources))
	}
}

func TestWikipediaProviderSkipsUnusableRecords(t *testing.T) {
	p, _ := wikiProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"batchcomplete":true,"query":{"search":[`+
			`{"title":"","pageid":1,"snippet":"no identity"},`+
			`{"title":"Good","pageid":2,"snippet":"kept"},`+
			`{"title":"No snippet","pageid":3}]}}`)
	}), 0)
	sources, err := p.Search(context.Background(), "q", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(sources) != 2 || sources[0].Title != "Good" || sources[1].Title != "No snippet" {
		t.Fatalf("unusable record handling wrong: %+v", sources)
	}
	if sources[1].Snippet != "" {
		t.Fatalf("missing snippet must stay empty: %q", sources[1].Snippet)
	}
}

func TestWikipediaProviderBoundsResultsAndFields(t *testing.T) {
	longTitle := strings.Repeat("T", MaxSearchResultTitle+100)
	longSnippet := strings.Repeat("S", MaxSearchResultSnippet+100)
	p, _ := wikiProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"batchcomplete":true,"query":{"search":[`+
			`{"title":%q,"pageid":1,"snippet":%q},`+
			`{"title":"two","pageid":2,"snippet":"s"},`+
			`{"title":"three","pageid":3,"snippet":"s"}]}}`, longTitle, longSnippet)
	}), 0)
	sources, err := p.Search(context.Background(), "q", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("result count not bounded: %d", len(sources))
	}
	if len(sources[0].Title) > MaxSearchResultTitle || len(sources[0].Snippet) > MaxSearchResultSnippet {
		t.Fatalf("fields not bounded: title=%d snippet=%d", len(sources[0].Title), len(sources[0].Snippet))
	}
	if strings.Contains(sources[0].Title, "\ufffd") || strings.Contains(sources[0].Snippet, "\ufffd") {
		t.Fatal("truncation split a UTF-8 rune")
	}
}

func TestWikipediaProviderRejectsMalformedResponses(t *testing.T) {
	cases := map[string]string{
		"malformed json":       `{`,
		"missing query":        `{"batchcomplete":true}`,
		"missing search":       `{"batchcomplete":true,"query":{"searchinfo":{}}}`,
		"provider error":       `{"error":{"code":"bad_query","info":"secret detail"}}`,
		"wrong top-level type": `[]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			p, _ := wikiProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			}), 0)
			_, err := p.Search(context.Background(), "q", 5)
			if err == nil || !errors.Is(err, ErrExternal) {
				t.Fatalf("want external error, got %v", err)
			}
			if strings.Contains(err.Error(), "secret detail") {
				t.Fatalf("provider detail leaked: %v", err)
			}
		})
	}
}

func TestWikipediaProviderHTTPFailures(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusTooManyRequests, "rate limited"},
		{http.StatusInternalServerError, "unavailable"},
		{http.StatusServiceUnavailable, "unavailable"},
		{http.StatusBadRequest, "rejected"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			p, _ := wikiProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"api_key":"sk-should-never-leak-123456"}`)
			}), 0)
			_, err := p.Search(context.Background(), "q", 5)
			if err == nil || !errors.Is(err, ErrExternal) {
				t.Fatalf("want external error, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not explain %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "sk-should-never-leak") {
				t.Fatalf("secret-shaped body leaked: %v", err)
			}
		})
	}
}

func TestWikipediaProviderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, `{"batchcomplete":true,"query":{"search":[]}}`)
	}))
	defer srv.Close()
	p, err := NewWikipediaSearchProvider(WikipediaProviderConfig{
		Endpoint: srv.URL, Language: "en", Timeout: 30 * time.Millisecond,
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

func TestWikipediaProviderCancellation(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)
	p, err := NewWikipediaSearchProvider(WikipediaProviderConfig{
		Endpoint: srv.URL, Language: "en", Timeout: 2 * time.Second, AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err = p.Search(ctx, "q", 5)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
}

func TestWikipediaProviderResponseSizeLimit(t *testing.T) {
	p, _ := wikiProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 1024))
	}), 64)
	_, err := p.Search(context.Background(), "q", 5)
	if err == nil || !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("want resource limit, got %v", err)
	}
}

func TestWikipediaProviderRejectsRedirects(t *testing.T) {
	var reached int32
	p, _ := wikiProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&reached, 1) == 1 {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}
		fmt.Fprint(w, `{"batchcomplete":true,"query":{"search":[]}}`)
	}), 1024)
	_, err := p.Search(context.Background(), "q", 5)
	if err == nil || !errors.Is(err, ErrExternal) {
		t.Fatalf("redirect must fail closed, got %v", err)
	}
	if atomic.LoadInt32(&reached) != 1 {
		t.Fatalf("redirect target must not be contacted")
	}
}

func TestWikipediaProviderQueryValidation(t *testing.T) {
	var calls int32
	p, _ := wikiProviderFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		fmt.Fprint(w, `{"batchcomplete":true,"query":{"search":[]}}`)
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

func TestWikipediaProviderConfigValidation(t *testing.T) {
	cases := []WikipediaProviderConfig{
		{Endpoint: "ftp://example.com/w/api.php"},
		{Endpoint: "https://user:pass@example.com/w/api.php"},
		{Endpoint: "http://example.com/w/api.php", AllowInsecureHTTP: true},
		{Endpoint: "http://example.com/w/api.php"},
		{Language: "not a language"},
		{UserAgent: "bad\r\nagent"},
	}
	for i, cfg := range cases {
		if _, err := NewWikipediaSearchProvider(cfg); err == nil || !errors.Is(err, ErrValidation) {
			t.Fatalf("config %d must fail closed, got %v", i, err)
		}
	}
}
