package capability

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type descriptorTestProvider struct {
	id   string
	desc ResearchProviderDescriptor
}

func (p descriptorTestProvider) Identify() string { return p.id }

func (p descriptorTestProvider) Search(context.Context, string, int) ([]Source, error) {
	return nil, nil
}

func (p descriptorTestProvider) ResearchDescriptor() ResearchProviderDescriptor { return p.desc }

// opaqueTestProvider deliberately does not implement researchDescriber.
type opaqueTestProvider struct{ id string }

func (p opaqueTestProvider) Identify() string { return p.id }

func (p opaqueTestProvider) Search(context.Context, string, int) ([]Source, error) {
	return nil, nil
}

func TestDescribeResearchProviderDisabled(t *testing.T) {
	desc := DescribeResearchProvider(nil)
	if desc.Provider != "disabled" || desc.Kind != ResearchKindDisabled {
		t.Fatalf("nil provider must be disabled: %+v", desc)
	}
	if desc.Live || !desc.Keyless || desc.Registered {
		t.Fatalf("disabled descriptor must not claim liveness or registration: %+v", desc)
	}
}

func TestDescribeResearchProviderScripted(t *testing.T) {
	desc := DescribeResearchProvider(ScriptedResearch())
	if desc.Provider != "scripted-research" || desc.Kind != ResearchKindScripted {
		t.Fatalf("scripted descriptor = %+v", desc)
	}
	if desc.Live || !desc.Keyless || desc.CredentialEnv != "" || desc.Endpoint != "" {
		t.Fatalf("scripted descriptor must be keyless and non-live: %+v", desc)
	}
}

func TestDescribeResearchProviderLiveAdapters(t *testing.T) {
	const key = "descriptor-test-brave-key"
	brave, err := NewBraveSearchProvider(BraveProviderConfig{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	desc := DescribeResearchProvider(brave)
	if desc.Provider != "brave" || desc.Kind != ResearchKindLive || !desc.Live || desc.Keyless {
		t.Fatalf("brave descriptor = %+v", desc)
	}
	if desc.CredentialEnv != "BRAVE_API_KEY" {
		t.Fatalf("brave credential env = %q", desc.CredentialEnv)
	}
	if desc.Endpoint != DefaultBraveEndpoint {
		t.Fatalf("brave endpoint = %q", desc.Endpoint)
	}
	raw, err := json.Marshal(desc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), key) {
		t.Fatalf("descriptor exposed the credential: %s", raw)
	}

	wiki, err := NewWikipediaSearchProvider(WikipediaProviderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	wikiDesc := DescribeResearchProvider(wiki)
	if wikiDesc.Provider != "wikipedia" || wikiDesc.Kind != ResearchKindLive ||
		!wikiDesc.Live || !wikiDesc.Keyless || wikiDesc.CredentialEnv != "" {
		t.Fatalf("wikipedia descriptor = %+v", wikiDesc)
	}
	if wikiDesc.Language != DefaultWikipediaLanguage {
		t.Fatalf("wikipedia language = %q", wikiDesc.Language)
	}
}

func TestDescribeResearchProviderInjectedIsNotGuessed(t *testing.T) {
	p := opaqueTestProvider{id: "My Custom Provider!"}
	desc := DescribeResearchProvider(p)
	if desc.Provider != "mycustomprovider" {
		t.Fatalf("injected provider id must be canonicalized, got %q", desc.Provider)
	}
	// A provider without a descriptor has unknown provenance. The descriptor
	// must not claim liveness, keylessness, a credential or an endpoint.
	if desc.Kind != ResearchKindInjected || desc.Live || desc.Keyless ||
		desc.CredentialEnv != "" || desc.Endpoint != "" {
		t.Fatalf("injected descriptor must not guess provenance: %+v", desc)
	}
}

func TestDescribeResearchProviderSanitizesEndpoint(t *testing.T) {
	p := descriptorTestProvider{
		id: "custom",
		desc: ResearchProviderDescriptor{
			Provider: "custom",
			Kind:     ResearchKindLive,
			Endpoint: "https://user:secret@api.example/v1?token=secret#fragment",
		},
	}
	desc := DescribeResearchProvider(p)
	if strings.Contains(desc.Endpoint, "secret") || strings.Contains(desc.Endpoint, "user") ||
		strings.Contains(desc.Endpoint, "?") || strings.Contains(desc.Endpoint, "#") {
		t.Fatalf("endpoint was not sanitized: %q", desc.Endpoint)
	}
	if desc.Endpoint != "https://api.example/v1" {
		t.Fatalf("sanitized endpoint = %q", desc.Endpoint)
	}
}
