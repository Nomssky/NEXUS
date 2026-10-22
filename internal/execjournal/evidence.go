package execjournal

import (
	"fmt"
	"strconv"
	"strings"
)

// Research source evidence is an optional, additive part of an execution
// record. It stores only the source records the web.search capability returned
// in a complete, validated result envelope. It never stores the model output,
// the search query, a publication date, an author, or a claim relationship:
// those cannot be derived from the documented tool result without inventing
// them.
//
// Presence means complete. A partial, malformed, truncated or oversized
// envelope is rejected wholesale by ParseWebSearchEvidence, so a durable
// record can never advertise a source list that was only partly captured.
const (
	// ResearchEvidenceVersion is the durable evidence schema version. It is
	// independent of SchemaVersion so the execution record can add optional
	// evidence without making older records unreadable.
	ResearchEvidenceVersion = 1
	// ResearchEvidenceCapability is the only capability whose result is
	// recognized today.
	ResearchEvidenceCapability = "web.search"
	// ResearchEvidenceOperation is the documented operation for that capability.
	ResearchEvidenceOperation = "search"

	// MaxResearchEvidenceSources bounds the durable source list. The shipped
	// tool budget allows at most 8 web.search calls of at most 10 results each,
	// so this leaves headroom without making a journal record unbounded.
	MaxResearchEvidenceSources = 50
	// Per-field bounds. They are deliberately generous for normal provider
	// output but small enough to keep one record bounded. An over-limit value
	// rejects the whole snapshot rather than silently truncating provenance.
	MaxResearchSourceTitle   = 512
	MaxResearchSourceURL     = 2048
	MaxResearchSourceName    = 256
	MaxResearchSourceSnippet = 1024
)

// ResearchSource is one normalized source record from a web.search result.
// URL is the provider's original value with surrounding whitespace removed;
// the journal never rewrites it. The frontend applies the existing http(s)
// link-safety rule before rendering an anchor.
type ResearchSource struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Source  string `json:"source"`
	Snippet string `json:"snippet"`
}

// ResearchEvidence is the bounded, versioned source snapshot for one execution.
// Count is the count declared by the provider envelope; it must equal
// len(Sources).
type ResearchEvidence struct {
	Version    int              `json:"version"`
	Capability string           `json:"capability"`
	Operation  string           `json:"operation"`
	Count      int              `json:"count"`
	Sources    []ResearchSource `json:"sources"`
}

// ParseWebSearchEvidence recognizes the documented web.search result envelope
// {sources: "1. Title | URL | Source | Snippet\n...", count: "N"} and returns a
// complete evidence snapshot, or ok=false. It is intentionally all-or-nothing:
// missing keys, a count mismatch, a malformed line, a non-sequential index, an
// oversized field, or a truncated platform result rejects the entire snapshot.
//
// The separator is the existing contract's literal " | ". A title that itself
// contains that separator is ambiguous in the provider's own output; the
// parser follows the same position-based rule as the frontend, and therefore
// never invents extra structure. Snippets may contain the separator and are
// preserved after the third field.
func ParseWebSearchEvidence(toolID, operation string, result map[string]string, truncated bool) (*ResearchEvidence, bool) {
	if truncated || toolID != ResearchEvidenceCapability {
		return nil, false
	}
	if operation != "" && operation != ResearchEvidenceOperation {
		return nil, false
	}
	rawSources, ok := result["sources"]
	if !ok {
		return nil, false
	}
	rawCount, ok := result["count"]
	if !ok {
		return nil, false
	}
	count, err := strconv.Atoi(strings.TrimSpace(rawCount))
	if err != nil || count < 0 || count > MaxResearchEvidenceSources {
		return nil, false
	}

	if strings.TrimSpace(rawSources) == "" {
		if count != 0 {
			return nil, false
		}
		return &ResearchEvidence{
			Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: 0, Sources: []ResearchSource{},
		}, true
	}

	sources := make([]ResearchSource, 0, count)
	for _, rawLine := range strings.Split(rawSources, "\n") {
		line := strings.TrimSuffix(rawLine, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, " | ")
		if len(parts) < 2 {
			return nil, false
		}
		index, title, ok := parseSourceIndex(parts[0])
		if !ok || index != len(sources)+1 {
			return nil, false
		}
		url := strings.TrimSpace(parts[1])
		source, snippet := "", ""
		if len(parts) > 2 {
			source = strings.TrimSpace(parts[2])
		}
		if len(parts) > 3 {
			snippet = strings.TrimSpace(strings.Join(parts[3:], " | "))
		}
		if !validSourceField(title, MaxResearchSourceTitle) ||
			!validSourceField(url, MaxResearchSourceURL) ||
			!validSourceField(source, MaxResearchSourceName) ||
			!validSourceField(snippet, MaxResearchSourceSnippet) {
			return nil, false
		}
		sources = append(sources, ResearchSource{Title: title, URL: url, Source: source, Snippet: snippet})
	}
	if len(sources) != count {
		return nil, false
	}
	return &ResearchEvidence{
		Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
		Operation: ResearchEvidenceOperation, Count: count, Sources: sources,
	}, true
}

// parseSourceIndex parses the "N. Title" prefix emitted by the capability
// adapter. A missing or malformed prefix is not guessed at.
func parseSourceIndex(value string) (int, string, bool) {
	value = strings.TrimSpace(value)
	dot := strings.IndexByte(value, '.')
	if dot <= 0 || dot > 6 {
		return 0, "", false
	}
	n, err := strconv.Atoi(value[:dot])
	if err != nil || n < 1 {
		return 0, "", false
	}
	rest := value[dot+1:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	return n, strings.TrimSpace(rest), true
}

func validSourceField(value string, max int) bool {
	return len(value) <= max && !strings.ContainsAny(value, "\x00\n\r")
}

// ValidateResearchEvidence checks an evidence snapshot at the durable write and
// decode boundaries. It is intentionally strict; a record that cannot be
// proven complete is not exposed.
func ValidateResearchEvidence(ev *ResearchEvidence) error {
	if ev == nil {
		return nil
	}
	if ev.Version != ResearchEvidenceVersion {
		return fmt.Errorf("execjournal: unsupported research evidence version %d", ev.Version)
	}
	if ev.Capability != ResearchEvidenceCapability {
		return fmt.Errorf("execjournal: unsupported research evidence capability %q", ev.Capability)
	}
	if ev.Operation != ResearchEvidenceOperation {
		return fmt.Errorf("execjournal: unsupported research evidence operation %q", ev.Operation)
	}
	if ev.Count < 0 || ev.Count > MaxResearchEvidenceSources {
		return fmt.Errorf("execjournal: research evidence count %d out of range", ev.Count)
	}
	if ev.Count != len(ev.Sources) {
		return fmt.Errorf("execjournal: research evidence count %d does not match %d sources",
			ev.Count, len(ev.Sources))
	}
	for i, source := range ev.Sources {
		if !validSourceField(source.Title, MaxResearchSourceTitle) ||
			!validSourceField(source.URL, MaxResearchSourceURL) ||
			!validSourceField(source.Source, MaxResearchSourceName) ||
			!validSourceField(source.Snippet, MaxResearchSourceSnippet) {
			return fmt.Errorf("execjournal: research evidence source %d exceeds its bounds", i)
		}
	}
	return nil
}
