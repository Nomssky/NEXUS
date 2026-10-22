package capability

// Tests for the mediated web.search adapter boundary: provider records are
// untrusted, unsafe URLs never become part of a result, and one bad record
// cannot poison safe records. These use the ScriptedResearchProvider (or a
// tiny local stub) and never claim to be external retrieval.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

type sourceProvider struct {
	sources []Source
	err     error
	calls   int
}

func (p *sourceProvider) Identify() string { return "test-source" }
func (p *sourceProvider) Search(context.Context, string, int) ([]Source, error) {
	p.calls++
	return p.sources, p.err
}

func TestWebSearchFiltersUnsafeRecordsWithoutPoisoningSafeOnes(t *testing.T) {
	provider := &sourceProvider{sources: []Source{
		{Title: "Unsafe JS", URL: "javascript:alert(1)", Source: "bad", Snippet: "drop me"},
		{Title: "Unsafe data", URL: "data:text/html,<script>", Source: "bad", Snippet: "drop me"},
		{Title: "Credential URL", URL: "https://user:pass@example.com/x", Source: "bad", Snippet: "drop me"},
		{Title: "Good | title", URL: "https://example.com/good", Source: "example|com", Snippet: "safe\nsnippet"},
	}}
	web := &WebSearchTool{Provider: provider, MaxSnippet: 512, MaxResults: 10}
	result, err := web.Invoke(context.Background(), tool.Invocation{
		Operation: "search", Input: map[string]string{"query": "q"},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if got := result.Result["count"]; got != "1" {
		t.Fatalf("unsafe records were not dropped: count=%s sources=%q", got, result.Result["sources"])
	}
	line := result.Result["sources"]
	if !strings.Contains(line, "https://example.com/good") ||
		!strings.Contains(line, "Good / title") || !strings.Contains(line, "example/com") {
		t.Fatalf("safe record was mangled: %q", line)
	}
	if strings.Contains(line, "javascript:") || strings.Contains(line, "data:text") ||
		strings.Contains(line, "user:pass") || strings.Contains(line, "\n") && strings.Contains(line, "drop me") {
		t.Fatalf("unsafe record leaked: %q", line)
	}
}

func TestWebSearchAllInvalidRecordsIsExplicitEmpty(t *testing.T) {
	web := &WebSearchTool{Provider: &sourceProvider{sources: []Source{
		{Title: "Bad", URL: "javascript:alert(1)"},
		{Title: "", URL: "https://example.com/no-title"},
	}}, MaxResults: 10}
	result, err := web.Invoke(context.Background(), tool.Invocation{
		Operation: "search", Input: map[string]string{"query": "q"},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result.Result["count"] != "0" || result.Result["sources"] != "" {
		t.Fatalf("invalid records must yield an explicit empty result: %+v", result.Result)
	}
}

func TestWebSearchInputValidation(t *testing.T) {
	provider := &sourceProvider{}
	web := &WebSearchTool{Provider: provider, MaxResults: 10}
	cases := []map[string]string{
		{"query": ""},
		{"query": "   "},
		{"query": strings.Repeat("q", MaxSearchQueryBytes+1)},
		{"query": "line\nbreak"},
		{"query": "https://example.com/fetch"},
	}
	for _, input := range cases {
		_, err := web.Invoke(context.Background(), tool.Invocation{Operation: "search", Input: input})
		if err == nil || !errors.Is(err, ErrValidation) {
			t.Fatalf("input %v must fail validation, got %v", input, err)
		}
	}
	if provider.calls != 0 {
		t.Fatal("invalid input must not reach the provider")
	}
}

func TestWebSearchNoScriptedFallbackOnProviderFailure(t *testing.T) {
	web := &WebSearchTool{Provider: &sourceProvider{err: errors.New("backend down")}, MaxResults: 10}
	_, err := web.Invoke(context.Background(), tool.Invocation{
		Operation: "search", Input: map[string]string{"query": "q"},
	})
	if err == nil || !errors.Is(err, ErrExternal) {
		t.Fatalf("provider failure must fail closed as external, got %v", err)
	}
	if strings.Contains(err.Error(), "docs.example") || strings.Contains(err.Error(), "Nexus capability") {
		t.Fatalf("scripted data must never substitute for a failed live provider: %v", err)
	}
}

func TestWebSearchPreservesProviderErrorKind(t *testing.T) {
	web := &WebSearchTool{Provider: &sourceProvider{err: fmt.Errorf("%w: rate limited", ErrExternal)}}
	if _, err := web.Invoke(context.Background(), tool.Invocation{
		Operation: "search", Input: map[string]string{"query": "q"},
	}); !errors.Is(err, ErrExternal) {
		t.Fatalf("external kind lost: %v", err)
	}
	web.Provider = &sourceProvider{err: fmt.Errorf("%w: slow", ErrTimeout)}
	if _, err := web.Invoke(context.Background(), tool.Invocation{
		Operation: "search", Input: map[string]string{"query": "q"},
	}); !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout kind lost: %v", err)
	}
}

func TestWebSearchBoundsEnvelopeAndCount(t *testing.T) {
	sources := make([]Source, 0, MaxSearchResultsPerQuery)
	for i := 0; i < MaxSearchResultsPerQuery; i++ {
		sources = append(sources, Source{
			Title:   fmt.Sprintf("Result %d", i),
			URL:     fmt.Sprintf("https://example.com/%d", i),
			Source:  "example.com",
			Snippet: strings.Repeat("x", MaxSearchResultSnippet),
		})
	}
	web := &WebSearchTool{Provider: &sourceProvider{sources: sources}, MaxSnippet: 512, MaxResults: 10}
	result, err := web.Invoke(context.Background(), tool.Invocation{
		Operation: "search", Input: map[string]string{"query": "q"},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(result.Result["sources"]) > 30*1024 {
		t.Fatalf("envelope exceeds bound: %d", len(result.Result["sources"]))
	}
	lines := strings.Count(result.Result["sources"], "\n") + 1
	if result.Result["count"] != fmt.Sprint(lines) {
		t.Fatalf("count must match the returned lines: count=%s lines=%d", result.Result["count"], lines)
	}
}

func TestWebSearchHonoursProviderDeadline(t *testing.T) {
	provider := &blockingProvider{}
	web := &WebSearchTool{Provider: provider, Timeout: 20 * time.Millisecond}
	start := time.Now()
	_, err := web.Invoke(context.Background(), tool.Invocation{
		Operation: "search", Input: map[string]string{"query": "q"},
	})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("provider deadline was not propagated")
	}
}

type blockingProvider struct{}

func (p *blockingProvider) Identify() string { return "blocking" }
func (p *blockingProvider) Search(ctx context.Context, _ string, _ int) ([]Source, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
