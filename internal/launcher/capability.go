package launcher

// Capability & Tool Platform wiring (contracts/CAPABILITY_TOOL_CONTRACTS.md).
//
// The platform is built from runtime configuration only: the workspace root,
// the HTTP destination policy, the research provider and the GitHub credential
// reference come from launcher options and the process environment. No model
// output and no request body can change any of them. An unusable configuration
// fails closed: the affected capability is not registered at all rather than
// registered with a permissive default.

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/agentintel"
	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/metrics"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// CapabilityOptions configures the shipped capability catalogue.
type CapabilityOptions struct {
	// ResearchProvider is the web research backend. Nil disables web.search.
	ResearchProvider capability.ResearchProvider
	// GitHubCredentialRef is the named, business-scoped credential reference
	// for the GitHub capability. Empty disables the GitHub tools.
	GitHubCredentialRef string
	// GitHubBusinessID scopes that credential (and the workspace root
	// default) to a business.
	GitHubBusinessID string
}

// capabilityPlatform builds the capability platform over the SAME tool registry
// the agent execution layer uses (one registry, one invocation boundary) and
// registers the shipped catalogue.
//
// env is the environment reader (os.Getenv in production, a map in tests).
func capabilityPlatform(
	env func(string) string,
	dataDir string,
	tools *tool.ToolRegistry,
	members *identity.MembershipSet,
	bus *event.MemBus,
	opts CapabilityOptions,
	m *metrics.Metrics,
) (*capability.Platform, error) {
	resolver := capability.NewScopedCredentialResolver(security.NewDevResolver())
	// The operator-provided credential value is registered under its named
	// reference and registered for redaction, so it can never escape into a
	// result, event, observation, log or response.
	if value := env("NEXUS_TOOL_CREDENTIAL_VALUE"); value != "" {
		ref := env("NEXUS_TOOL_CREDENTIAL_REF")
		if ref != "" {
			business := env("NEXUS_TOOL_CREDENTIAL_BUSINESS")
			division := env("NEXUS_TOOL_CREDENTIAL_DIVISION")
			_ = resolver.Put(ref, business, division, []byte(value))
		}
	}
	platform := capability.New(tools, members, resolver, bus)
	if m != nil {
		platform.Metrics = m
	}
	if dataDir == "" {
		dataDir = env("NEXUS_DATA_DIR")
	}
	production := strings.EqualFold(env("NEXUS_ENVIRONMENT"), "production")

	// Capability catalogue knobs (boot-time configuration, never model input):
	//   NEXUS_CAPABILITY_WEB=scripted  -> deterministic test provider
	//   NEXUS_CAPABILITY_WEB=wikipedia -> real external MediaWiki search
	//   NEXUS_CAPABILITY_WEB_ENDPOINT / _LANGUAGE / _USER_AGENT -> provider config
	//   NEXUS_CAPABILITY_GITHUB_REF=x  -> register the GitHub capability
	//   NEXUS_CAPABILITY_GITHUB_BUSINESS=b -> its business scope
	//
	// An unknown non-empty provider selection fails boot: a typo must never
	// silently disable search or fall back to scripted retrieval.
	if opts.ResearchProvider == nil {
		provider, err := researchProviderFromEnv(env)
		if err != nil {
			return nil, fmt.Errorf("capability platform: %w", err)
		}
		opts.ResearchProvider = provider
	}
	if ref := env("NEXUS_CAPABILITY_GITHUB_REF"); ref != "" && opts.GitHubCredentialRef == "" {
		opts.GitHubCredentialRef = ref
		opts.GitHubBusinessID = firstNonEmpty(opts.GitHubBusinessID, env("NEXUS_CAPABILITY_GITHUB_BUSINESS"),
			env("NEXUS_TOOL_CREDENTIAL_BUSINESS"), env("NEXUS_BOOTSTRAP_BUSINESS"))
	}
	httpPolicy, policyErr := capability.LoadHTTPPolicyFromEnv(env, production)
	if policyErr != nil {
		// Fail closed on an unusable policy: a malformed port list or a
		// loopback opt-in in production must not silently widen access.
		return nil, fmt.Errorf("capability platform: http policy: %w", policyErr)
	}
	root := workspaceRoot(env, dataDir, firstNonEmpty(opts.GitHubBusinessID, env("NEXUS_BOOTSTRAP_BUSINESS")))
	if root != "" {
		if err := capability.EnsureWorkspaceRoot(root); err != nil {
			return nil, err
		}
	}
	capOpts := capability.Options{
		WorkspaceRoot:       root,
		HTTPPolicy:          httpPolicy,
		ResearchProvider:    opts.ResearchProvider,
		GitHubCredentialRef: opts.GitHubCredentialRef,
		GitHubBusinessID:    opts.GitHubBusinessID,
	}
	if err := capability.RegisterAll(tools, capOpts); err != nil {
		return nil, fmt.Errorf("capability platform: %w", err)
	}
	if bus != nil {
		for _, m := range tools.ListManifests() {
			_ = bus.Publish(&event.Event{
				Type:       event.EventTypeToolRegistered,
				Source:     "capability",
				Timestamp:  time.Now(),
				BusinessID: opts.GitHubBusinessID,
				Data: []byte(fmt.Sprintf(`{"tool_id":%q,"version":%q,"side_effect_class":%q,"security_class":%q}`,
					m.ID, m.Version, m.SideEffectClass, m.SecurityClass)),
			})
		}
	}
	return platform, nil
}

func researchProviderFromEnv(env func(string) string) (capability.ResearchProvider, error) {
	switch strings.ToLower(strings.TrimSpace(env("NEXUS_CAPABILITY_WEB"))) {
	case "":
		return nil, nil
	case "scripted":
		return capability.ScriptedResearch(), nil
	case "wikipedia":
		provider, err := capability.NewWikipediaSearchProvider(capability.WikipediaProviderConfig{
			Endpoint:          env("NEXUS_CAPABILITY_WEB_ENDPOINT"),
			Language:          env("NEXUS_CAPABILITY_WEB_LANGUAGE"),
			UserAgent:         env("NEXUS_CAPABILITY_WEB_USER_AGENT"),
			AllowInsecureHTTP: strings.EqualFold(env("NEXUS_CAPABILITY_WEB_ALLOW_INSECURE"), "true"),
		})
		if err != nil {
			return nil, fmt.Errorf("web search provider: %w", err)
		}
		return provider, nil
	default:
		return nil, fmt.Errorf("%w: NEXUS_CAPABILITY_WEB must be empty, %q, or %q",
			capability.ErrValidation, "scripted", "wikipedia")
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// workspaceRoot resolves the filesystem/git sandbox root from configuration. An
// empty result disables the sandboxed capabilities (fail closed) instead of
// defaulting to "/" or the process working directory.
func workspaceRoot(env func(string) string, dataDir, businessID string) string {
	if v := strings.TrimSpace(env("NEXUS_TOOL_FS_ROOT")); v != "" {
		return v
	}
	if dataDir == "" || businessID == "" {
		return ""
	}
	return dataDir + "/workspaces/" + businessID
}

// capabilityCatalog adapts the capability platform to the intelligence layer's
// CatalogProvider (contract §4/§31). It lives here rather than in the
// capability package so the dependency direction stays one-way: the platform
// knows nothing about the intelligence layer.
type capabilityCatalog struct{ platform *capability.Platform }

// Catalog returns the scope-visible catalog as bounded prompt hints. The
// input shape is derived from each manifest's declared input schema (field
// names, types, required markers) — descriptive context only, never a grant.
func (c capabilityCatalog) Catalog(businessID, divisionID string) []agentintel.ToolHint {
	if c.platform == nil {
		return nil
	}
	manifests := c.platform.Catalog(businessID, divisionID)
	out := make([]agentintel.ToolHint, 0, len(manifests))
	for _, m := range manifests {
		out = append(out, agentintel.ToolHint{
			ID:         m.ID,
			Summary:    m.Description,
			Operations: m.SortedOperations(),
			SideEffect: string(m.SideEffectClass),
			InputShape: inputShapeOf(m),
		})
	}
	return out
}

// inputShapeOf renders "name:type" per declared input field, "*" = required.
// It exposes the shape the model needs for a valid proposal and nothing
// about credentials, paths, membership or governance.
func inputShapeOf(m tool.ToolManifest) string {
	parts := make([]string, 0, len(m.InputSchema.Fields))
	for _, f := range m.InputSchema.Fields {
		name := strings.TrimSpace(string(f.Name))
		if name == "" {
			continue
		}
		part := name + ":" + strings.TrimSpace(string(f.Type))
		if f.Required {
			part += "*"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ",")
}
