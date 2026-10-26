package capability

// Boot-time description of the web-search provider that was actually
// constructed. This is deliberately separate from the adapter itself: the
// configuration surface must report the provider the process selected at boot,
// not re-read NEXUS_CAPABILITY_WEB at request time. It never contains a
// credential value, and describing a provider performs no network I/O.

import (
	"net/url"
	"strings"
)

// Research provider kinds. "injected" means an embedding process supplied an
// adapter directly; its liveness and credential requirements are unknown and
// must not be guessed.
const (
	ResearchKindDisabled = "disabled"
	ResearchKindScripted = "scripted"
	ResearchKindLive     = "live"
	ResearchKindInjected = "injected"
)

// ResearchProviderDescriptor describes one selected web-search adapter.
//
// Provider is the adapter's canonical Identify() value (for example
// "scripted-research", "wikipedia" or "brave"). Endpoint, when present, is
// already stripped of userinfo, query and fragment.
type ResearchProviderDescriptor struct {
	Provider      string `json:"provider"`
	Kind          string `json:"kind"`
	Registered    bool   `json:"registered"`
	Live          bool   `json:"live"`
	Keyless       bool   `json:"keyless"`
	CredentialEnv string `json:"credential_env,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
	Language      string `json:"language,omitempty"`
	SelectedBy    string `json:"selected_by,omitempty"`
}

// researchDescriber is implemented by the shipped adapters. New adapters
// should implement it; an adapter that does not is reported as injected rather
// than guessed to be live.
type researchDescriber interface {
	ResearchDescriptor() ResearchProviderDescriptor
}

// DescribeResearchProvider captures the descriptor of an already-constructed
// provider. A nil provider is the explicit disabled state. No network probe and
// no environment lookup happens here.
func DescribeResearchProvider(p ResearchProvider) ResearchProviderDescriptor {
	if p == nil {
		return ResearchProviderDescriptor{
			Provider: "disabled",
			Kind:     ResearchKindDisabled,
			Keyless:  true,
		}
	}
	if d, ok := p.(researchDescriber); ok {
		desc := d.ResearchDescriptor()
		desc.Provider = sanitizeResearchProviderID(desc.Provider)
		desc.Endpoint = sanitizeResearchEndpoint(desc.Endpoint)
		return desc
	}
	return ResearchProviderDescriptor{
		Provider: sanitizeResearchProviderID(p.Identify()),
		Kind:     ResearchKindInjected,
		// Live, Keyless and CredentialEnv stay zero: an injected adapter has
		// unknown provenance, and claiming otherwise would be fabrication.
	}
}

// ResearchDescriptor implements researchDescriber.
func (p *ScriptedResearchProvider) ResearchDescriptor() ResearchProviderDescriptor {
	return ResearchProviderDescriptor{
		Provider: "scripted-research",
		Kind:     ResearchKindScripted,
		Keyless:  true,
	}
}

// ResearchDescriptor implements researchDescriber.
func (p *WikipediaSearchProvider) ResearchDescriptor() ResearchProviderDescriptor {
	return ResearchProviderDescriptor{
		Provider: "wikipedia",
		Kind:     ResearchKindLive,
		Live:     true,
		Keyless:  true,
		Endpoint: p.endpoint.String(),
		Language: p.language,
	}
}

// ResearchDescriptor implements researchDescriber.
func (p *BraveSearchProvider) ResearchDescriptor() ResearchProviderDescriptor {
	return ResearchProviderDescriptor{
		Provider:      "brave",
		Kind:          ResearchKindLive,
		Live:          true,
		Keyless:       false,
		CredentialEnv: "BRAVE_API_KEY",
		Endpoint:      p.endpoint.String(),
	}
}

// sanitizeResearchProviderID keeps the canonical identifier safe for a
// read-only API: lower-case, bounded, and restricted to identifier characters.
func sanitizeResearchProviderID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == ':':
			b.WriteRune(r)
		}
		if b.Len() >= 64 {
			break
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

// sanitizeResearchEndpoint removes userinfo, query and fragment. An adapter
// endpoint is validated before construction, so an unparseable value can only
// come from a malformed injected descriptor; report it as absent rather than
// echoing it.
func sanitizeResearchEndpoint(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
