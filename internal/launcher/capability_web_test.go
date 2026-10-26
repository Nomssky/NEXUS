package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

const launcherBraveKey = "launcher-brave-test-key-0123456789"

func envMap(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestResearchProviderFromEnv(t *testing.T) {
	provider, err := researchProviderFromEnv(envMap(nil))
	if err != nil || provider != nil {
		t.Fatalf("empty selection must disable web.search: provider=%v err=%v", provider, err)
	}

	provider, err = researchProviderFromEnv(envMap(map[string]string{"NEXUS_CAPABILITY_WEB": "scripted"}))
	if err != nil || provider == nil || provider.Identify() != "scripted-research" {
		t.Fatalf("scripted selection: provider=%v err=%v", provider, err)
	}

	provider, err = researchProviderFromEnv(envMap(map[string]string{
		"NEXUS_CAPABILITY_WEB":          "wikipedia",
		"NEXUS_CAPABILITY_WEB_LANGUAGE": "en",
	}))
	if err != nil || provider == nil || provider.Identify() != "wikipedia" {
		t.Fatalf("wikipedia selection: provider=%v err=%v", provider, err)
	}

	provider, err = researchProviderFromEnv(envMap(map[string]string{
		"NEXUS_CAPABILITY_WEB":                "wikipedia",
		"NEXUS_CAPABILITY_WEB_ENDPOINT":       "http://127.0.0.1:1234/w/api.php",
		"NEXUS_CAPABILITY_WEB_ALLOW_INSECURE": "true",
	}))
	if err != nil || provider == nil {
		t.Fatalf("loopback fixture endpoint: provider=%v err=%v", provider, err)
	}

	provider, err = researchProviderFromEnv(envMap(map[string]string{
		"NEXUS_CAPABILITY_WEB": "brave",
		"BRAVE_API_KEY":        launcherBraveKey,
	}))
	if err != nil || provider == nil || provider.Identify() != "brave" {
		t.Fatalf("brave selection: provider=%v err=%v", provider, err)
	}

	provider, err = researchProviderFromEnv(envMap(map[string]string{
		"NEXUS_CAPABILITY_WEB":                "brave",
		"BRAVE_API_KEY":                       launcherBraveKey,
		"NEXUS_CAPABILITY_WEB_ENDPOINT":       "http://127.0.0.1:1234/res/v1/web/search",
		"NEXUS_CAPABILITY_WEB_ALLOW_INSECURE": "true",
	}))
	if err != nil || provider == nil || provider.Identify() != "brave" {
		t.Fatalf("brave loopback fixture endpoint: provider=%v err=%v", provider, err)
	}
}

func TestResearchProviderFromEnvFailsClosedOnBadConfiguration(t *testing.T) {
	cases := []map[string]string{
		{"NEXUS_CAPABILITY_WEB": "typo"},
		{"NEXUS_CAPABILITY_WEB": "wikipedia", "NEXUS_CAPABILITY_WEB_ENDPOINT": "ftp://example.com/w/api.php"},
		{"NEXUS_CAPABILITY_WEB": "wikipedia", "NEXUS_CAPABILITY_WEB_ENDPOINT": "http://example.com/w/api.php"},
		{"NEXUS_CAPABILITY_WEB": "brave"},
		{"NEXUS_CAPABILITY_WEB": "brave", "BRAVE_API_KEY": "   "},
		{"NEXUS_CAPABILITY_WEB": "brave", "BRAVE_API_KEY": "bad\nkey"},
		{"NEXUS_CAPABILITY_WEB": "brave", "BRAVE_API_KEY": launcherBraveKey,
			"NEXUS_CAPABILITY_WEB_ENDPOINT": "https://example.com/res/v1/web/search"},
	}
	for i, env := range cases {
		if _, err := researchProviderFromEnv(envMap(env)); err == nil {
			t.Fatalf("case %d must fail closed", i)
		} else if strings.Contains(err.Error(), launcherBraveKey) {
			t.Fatalf("case %d leaked the credential: %v", i, err)
		}
	}
	if _, err := researchProviderFromEnv(envMap(map[string]string{"NEXUS_CAPABILITY_WEB": "typo"})); !errors.Is(err, capability.ErrValidation) {
		t.Fatalf("unknown selection must be a validation error: %v", err)
	}
	if _, err := researchProviderFromEnv(envMap(map[string]string{"NEXUS_CAPABILITY_WEB": "brave"})); !errors.Is(err, capability.ErrCredential) {
		t.Fatalf("missing Brave credential must be a credential error: %v", err)
	}
}

func TestCapabilityPlatformMarksProviderConfigAsStartupFatal(t *testing.T) {
	for _, env := range []map[string]string{
		{"NEXUS_CAPABILITY_WEB": "typo"},
		{"NEXUS_CAPABILITY_WEB": "brave"},
		{"NEXUS_CAPABILITY_WEB": "brave", "BRAVE_API_KEY": "bad\nkey"},
	} {
		_, _, err := capabilityPlatform(envMap(env), "", tool.NewToolRegistry(),
			identity.NewMembershipSet(), nil, CapabilityOptions{}, nil)
		if err == nil || !errors.Is(err, errResearchProviderConfig) {
			t.Fatalf("provider config error must be startup-fatal: %v", err)
		}
	}
}

func TestCapabilityPlatformCapturesBootDescriptor(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		_, desc, err := capabilityPlatform(envMap(nil), "", tool.NewToolRegistry(),
			identity.NewMembershipSet(), nil, CapabilityOptions{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if desc.Kind != capability.ResearchKindDisabled || desc.Provider != "disabled" || desc.Registered {
			t.Fatalf("disabled descriptor = %+v", desc)
		}
		if desc.SelectedBy != "NEXUS_CAPABILITY_WEB" {
			t.Fatalf("disabled selected_by = %q", desc.SelectedBy)
		}
	})

	t.Run("scripted from env", func(t *testing.T) {
		_, desc, err := capabilityPlatform(envMap(map[string]string{"NEXUS_CAPABILITY_WEB": "scripted"}),
			"", tool.NewToolRegistry(), identity.NewMembershipSet(), nil, CapabilityOptions{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if desc.Provider != "scripted-research" || desc.Kind != capability.ResearchKindScripted ||
			!desc.Registered || desc.Live {
			t.Fatalf("scripted descriptor = %+v", desc)
		}
		if desc.SelectedBy != "NEXUS_CAPABILITY_WEB" {
			t.Fatalf("scripted selected_by = %q", desc.SelectedBy)
		}
	})

	t.Run("brave from env", func(t *testing.T) {
		_, desc, err := capabilityPlatform(envMap(map[string]string{
			"NEXUS_CAPABILITY_WEB": "brave",
			"BRAVE_API_KEY":        launcherBraveKey,
		}), "", tool.NewToolRegistry(), identity.NewMembershipSet(), nil, CapabilityOptions{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if desc.Provider != "brave" || desc.Kind != capability.ResearchKindLive ||
			!desc.Registered || !desc.Live || desc.Keyless {
			t.Fatalf("brave descriptor = %+v", desc)
		}
		if desc.CredentialEnv != "BRAVE_API_KEY" || strings.Contains(desc.Endpoint, launcherBraveKey) {
			t.Fatalf("brave descriptor must expose only the credential name: %+v", desc)
		}
	})

	t.Run("injected", func(t *testing.T) {
		_, desc, err := capabilityPlatform(envMap(nil), "", tool.NewToolRegistry(),
			identity.NewMembershipSet(), nil,
			CapabilityOptions{ResearchProvider: launcherInjectedProvider{}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if desc.Kind != capability.ResearchKindInjected || desc.Provider != "injected-test" ||
			!desc.Registered || desc.Live {
			t.Fatalf("injected descriptor = %+v", desc)
		}
		if desc.SelectedBy != "launcher_option" || desc.CredentialEnv != "" || desc.Endpoint != "" {
			t.Fatalf("injected provenance must not be guessed: %+v", desc)
		}
	})
}

// launcherInjectedProvider is intentionally not a shipped adapter: it has no
// ResearchDescriptor method, so the platform must report unknown provenance.
type launcherInjectedProvider struct{}

func (launcherInjectedProvider) Identify() string { return "injected-test" }

func (launcherInjectedProvider) Search(context.Context, string, int) ([]capability.Source, error) {
	return nil, nil
}

func TestCapabilityPlatformDiagnosticsExposeSelectedProviderWithoutSecret(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      map[string]string
		provider string
	}{
		{
			name:     "scripted",
			env:      map[string]string{"NEXUS_CAPABILITY_WEB": "scripted"},
			provider: "scripted-research",
		},
		{
			name:     "brave",
			env:      map[string]string{"NEXUS_CAPABILITY_WEB": "brave", "BRAVE_API_KEY": launcherBraveKey},
			provider: "brave",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var registered []*event.Event
			bus := event.NewMemBusWithTap(func(e *event.Event) {
				if e.Type == event.EventTypeToolRegistered {
					registered = append(registered, e)
				}
			})
			tools := tool.NewToolRegistry()
			if _, _, err := capabilityPlatform(envMap(tc.env), "", tools,
				identity.NewMembershipSet(), bus, CapabilityOptions{}, nil); err != nil {
				t.Fatalf("capability platform: %v", err)
			}
			found := false
			for _, e := range registered {
				var diagnostic map[string]string
				if err := json.Unmarshal(e.Data, &diagnostic); err != nil {
					t.Fatalf("tool.registered data is not JSON: %v", err)
				}
				if diagnostic["tool_id"] != "web.search" {
					continue
				}
				found = true
				if diagnostic["provider"] != tc.provider {
					t.Fatalf("provider diagnostic = %q, want %q", diagnostic["provider"], tc.provider)
				}
				if strings.Contains(string(e.Data), launcherBraveKey) {
					t.Fatalf("diagnostic exposed the credential: %s", e.Data)
				}
			}
			if !found {
				t.Fatal("web.search registration event not published")
			}
		})
	}
}
