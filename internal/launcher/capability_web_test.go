package launcher

import (
	"errors"
	"testing"

	"github.com/Nomssky/NEXUS/internal/capability"
)

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
}

func TestResearchProviderFromEnvFailsClosedOnBadConfiguration(t *testing.T) {
	cases := []map[string]string{
		{"NEXUS_CAPABILITY_WEB": "typo"},
		{"NEXUS_CAPABILITY_WEB": "wikipedia", "NEXUS_CAPABILITY_WEB_ENDPOINT": "ftp://example.com/w/api.php"},
		{"NEXUS_CAPABILITY_WEB": "wikipedia", "NEXUS_CAPABILITY_WEB_ENDPOINT": "http://example.com/w/api.php"},
	}
	for i, env := range cases {
		if _, err := researchProviderFromEnv(envMap(env)); err == nil {
			t.Fatalf("case %d must fail closed", i)
		}
	}
	if _, err := researchProviderFromEnv(envMap(map[string]string{"NEXUS_CAPABILITY_WEB": "typo"})); !errors.Is(err, capability.ErrValidation) {
		t.Fatalf("unknown selection must be a validation error: %v", err)
	}
}
