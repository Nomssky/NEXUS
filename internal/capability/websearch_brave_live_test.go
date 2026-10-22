package capability

// Opt-in live smoke for the Brave general web-search provider. It is
// deliberately independent of the live model so provider reachability,
// credential handling and normalization can be verified on their own.
//
// Enable with:
//
//	NEXUS_LIVE_WEB_SMOKE=1 \
//	NEXUS_LIVE_WEB_PROVIDER=brave \
//	BRAVE_API_KEY=... \
//	go test ./internal/capability -run TestLiveBraveProviderSmoke -count=1 -v
//
// The test uses the documented production endpoint only. A local fixture is
// never treated as live validation.

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

type braveLiveCapture struct {
	inner   *BraveSearchProvider
	sources []Source
}

func (p *braveLiveCapture) Identify() string { return p.inner.Identify() }

func (p *braveLiveCapture) Search(ctx context.Context, query string, limit int) ([]Source, error) {
	sources, err := p.inner.Search(ctx, query, limit)
	p.sources = sources
	return sources, err
}

func TestLiveBraveProviderSmoke(t *testing.T) {
	if strings.TrimSpace(os.Getenv("NEXUS_LIVE_WEB_SMOKE")) != "1" {
		t.Skip("live web smoke is opt-in: set NEXUS_LIVE_WEB_SMOKE=1")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("NEXUS_LIVE_WEB_PROVIDER")), "brave") {
		t.Fatal("live web smoke requires explicit NEXUS_LIVE_WEB_PROVIDER=brave")
	}
	key := strings.TrimSpace(os.Getenv("BRAVE_API_KEY"))
	if key == "" {
		t.Fatal("live Brave smoke requires BRAVE_API_KEY")
	}

	provider, err := NewBraveSearchProvider(BraveProviderConfig{
		APIKey: key, Timeout: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("live Brave configuration: %v", err)
	}
	capture := &braveLiveCapture{inner: provider}
	web := &WebSearchTool{Provider: capture, MaxSnippet: 512, MaxResults: 3, Timeout: 15 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := web.Invoke(ctx, tool.Invocation{
		ToolID: "web.search", Operation: "search",
		Input: map[string]string{"query": "NEXUS runtime capabilities", "limit": "3"},
		Limits: tool.ResourceLimits{
			MaxDuration: 15 * time.Second, MaxOutputByte: 32768, MaxItems: 3,
		},
	})
	if err != nil {
		// The adapter classifies failures; no response body or credential is
		// included in the error.
		t.Fatalf("live Brave provider request failed: %v", err)
	}
	if strings.Contains(result.Result["sources"], key) {
		t.Fatal("live provider result exposed the credential")
	}
	if len(capture.sources) == 0 {
		t.Logf("live provider %q reached; empty result set (no evidence-backed synthesis claim)",
			provider.Identify())
		return
	}
	for _, source := range capture.sources {
		if strings.TrimSpace(source.Title) == "" || strings.TrimSpace(source.Source) == "" {
			t.Fatalf("live provider returned a record without identity: %+v", source)
		}
		if _, ok := normalizedSearchURL(source.URL); !ok {
			t.Fatalf("live provider returned an unsafe result URL: %q", source.URL)
		}
		if source.PublishedAt != "" {
			t.Fatalf("live provider fabricated publication metadata: %+v", source)
		}
	}
	if result.Result["count"] != strconv.Itoa(len(capture.sources)) {
		t.Fatalf("production adapter accepted %s of %d live records", result.Result["count"], len(capture.sources))
	}
	t.Logf("live provider %q reached; %d bounded records passed production normalization; test mode=provider-only",
		provider.Identify(), len(capture.sources))
}
