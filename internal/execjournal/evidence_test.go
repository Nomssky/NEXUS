package execjournal

import (
	"strings"
	"testing"
)

func webSearchEnvelope(sources, count string) map[string]string {
	return map[string]string{"sources": sources, "count": count}
}

// A complete documented envelope becomes one ordered snapshot. The snippet may
// contain the separator and is preserved after the three structural fields.
func TestParseWebSearchEvidenceCompleteSnapshot(t *testing.T) {
	ev, ok := ParseWebSearchEvidence(ResearchEvidenceCapability, ResearchEvidenceOperation, webSearchEnvelope(
		"1. Nexus capability boundary | https://docs.example/nexus | docs.example | The runtime decides; the model proposes.\n"+
			"2. External content is data | https://example.org/safety | example.org | Inert | data",
		"2",
	), false)
	if !ok || ev == nil {
		t.Fatal("a complete envelope must be captured")
	}
	if ev.Version != ResearchEvidenceVersion || ev.Capability != ResearchEvidenceCapability ||
		ev.Operation != ResearchEvidenceOperation || ev.Count != 2 {
		t.Fatalf("unexpected evidence header: %+v", ev)
	}
	if len(ev.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(ev.Sources))
	}
	if ev.Sources[0].Title != "Nexus capability boundary" ||
		ev.Sources[0].URL != "https://docs.example/nexus" ||
		ev.Sources[0].Source != "docs.example" ||
		ev.Sources[0].Snippet != "The runtime decides; the model proposes." {
		t.Fatalf("first source mangled: %+v", ev.Sources[0])
	}
	if ev.Sources[1].Snippet != "Inert | data" {
		t.Fatalf("snippet separator must be preserved: %q", ev.Sources[1].Snippet)
	}
}

// A zero-result search is evidence of an empty complete snapshot, distinct from
// no evidence at all.
func TestParseWebSearchEvidenceEmptySnapshot(t *testing.T) {
	ev, ok := ParseWebSearchEvidence(ResearchEvidenceCapability, "", webSearchEnvelope("", "0"), false)
	if !ok || ev == nil {
		t.Fatal("an empty complete result must be captured")
	}
	if ev.Count != 0 || ev.Sources == nil || len(ev.Sources) != 0 {
		t.Fatalf("empty snapshot must be present and empty, got %+v", ev)
	}
}

func TestParseWebSearchEvidenceRejectsIncompleteOrMalformed(t *testing.T) {
	cases := map[string]struct {
		toolID    string
		operation string
		result    map[string]string
		truncated bool
	}{
		"wrong tool":       {toolID: "http.request", result: webSearchEnvelope("1. A | https://a.example | a | s", "1")},
		"wrong operation":  {toolID: ResearchEvidenceCapability, operation: "fetch", result: webSearchEnvelope("1. A | https://a.example | a | s", "1")},
		"truncated":        {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("1. A | https://a.example | a | s", "1"), truncated: true},
		"missing sources":  {toolID: ResearchEvidenceCapability, result: map[string]string{"count": "0"}},
		"missing count":    {toolID: ResearchEvidenceCapability, result: map[string]string{"sources": ""}},
		"bad count":        {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("", "many")},
		"negative count":   {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("", "-1")},
		"count too large":  {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("", "51")},
		"count mismatch":   {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("1. A | https://a.example | a | s", "2")},
		"empty with count": {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("   ", "1")},
		"no separator":     {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("1. A https://a.example", "1")},
		"no index":         {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("A | https://a.example | a | s", "1")},
		"bad index":        {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("2. A | https://a.example | a | s", "1")},
		"gap index":        {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("1. A | https://a.example | a | s\n3. B | https://b.example | b | t", "2")},
		"newline field":    {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("1. A\nB | https://a.example | a | s", "1")},
		"oversized URL": {toolID: ResearchEvidenceCapability, result: webSearchEnvelope(
			"1. A | https://a.example/"+strings.Repeat("x", MaxResearchSourceURL)+" | a | s", "1")},
		"oversized snippet": {toolID: ResearchEvidenceCapability, result: webSearchEnvelope(
			"1. A | https://a.example | a | "+strings.Repeat("x", MaxResearchSourceSnippet+1), "1")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if ev, ok := ParseWebSearchEvidence(tc.toolID, tc.operation, tc.result, tc.truncated); ok || ev != nil {
				t.Fatalf("malformed/partial evidence must be rejected, got %+v", ev)
			}
		})
	}
}

// URL normalization is deliberately conservative: surrounding whitespace is
// removed but the provider's original value is not rewritten. Safety is a
// presentation decision, not a durability decision.
func TestParseWebSearchEvidencePreservesOriginalURL(t *testing.T) {
	const url = "javascript:alert(1)"
	ev, ok := ParseWebSearchEvidence(ResearchEvidenceCapability, ResearchEvidenceOperation,
		webSearchEnvelope("1. Unsafe |  "+url+"  | provider | snippet", "1"), false)
	if !ok || len(ev.Sources) != 1 {
		t.Fatalf("expected a complete source record, got %+v", ev)
	}
	if ev.Sources[0].URL != url {
		t.Fatalf("URL changed: %q", ev.Sources[0].URL)
	}
}

func TestParseWebSearchEvidenceHandlesCRLFAndBlankLines(t *testing.T) {
	ev, ok := ParseWebSearchEvidence(ResearchEvidenceCapability, ResearchEvidenceOperation,
		webSearchEnvelope("\r\n1. A | https://a.example | a | s\r\n\r\n", "1"), false)
	if !ok || len(ev.Sources) != 1 || ev.Sources[0].URL != "https://a.example" {
		t.Fatalf("CRLF/blank lines must not corrupt the record: %+v ok=%v", ev, ok)
	}
}

func TestValidateResearchEvidence(t *testing.T) {
	valid := &ResearchEvidence{
		Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
		Operation: ResearchEvidenceOperation, Count: 1,
		Sources: []ResearchSource{{Title: "A", URL: "https://a.example"}},
	}
	if err := ValidateResearchEvidence(valid); err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}
	if err := ValidateResearchEvidence(nil); err != nil {
		t.Fatalf("nil evidence must be allowed: %v", err)
	}
	cases := map[string]*ResearchEvidence{
		"version":    {Version: 2, Capability: ResearchEvidenceCapability, Operation: ResearchEvidenceOperation},
		"capability": {Version: ResearchEvidenceVersion, Capability: "other", Operation: ResearchEvidenceOperation},
		"operation":  {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability, Operation: "fetch"},
		"count mismatch": {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: 2, Sources: []ResearchSource{{}}},
		"oversized": {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: 1,
			Sources: []ResearchSource{{Snippet: strings.Repeat("x", MaxResearchSourceSnippet+1)}}},
		"control": {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: 1,
			Sources: []ResearchSource{{Title: "a\nb"}}},
	}
	for name, ev := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateResearchEvidence(ev); err == nil {
				t.Fatalf("invalid evidence accepted: %+v", ev)
			}
		})
	}
}
