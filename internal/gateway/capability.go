package gateway

// Capability discovery surface (contracts/CAPABILITY_TOOL_CONTRACTS.md §4).
//
// GET /api/v1/tools returns the catalog of tools visible in the caller's scope:
// id, version, description, category, operations, side-effect class, security
// class and constraints. It never returns credentials, secret values, internal
// filesystem roots, network topology or authorization internals, and it grants
// nothing — every invocation is validated again by the capability platform.
//
// This endpoint adds no authorization of its own: it reuses the same identity
// and business-membership gates as every other scoped surface (G3/G5), and a
// missing platform answers 503 rather than fabricating a catalogue.

import (
	"encoding/json"
	"net/http"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// WithCapabilityPlatform wires the capability platform for discovery. Nil makes
// the endpoint fail closed (503).
func WithCapabilityPlatform(p *capability.Platform) ServerOption {
	return func(s *Server) { s.capability = p }
}

// toolCatalogEntry is the bounded, non-secret view of one manifest.
type toolCatalogEntry struct {
	ID               string   `json:"tool_id"`
	Version          string   `json:"version"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Category         string   `json:"category"`
	Operations       []string `json:"supported_operations"`
	SideEffectClass  string   `json:"side_effect_class"`
	SecurityClass    string   `json:"security_class"`
	NetworkNeeds     string   `json:"network_requirement"`
	CredentialNeeded bool     `json:"credential_required"`
	ScopeRequirement string   `json:"scope_requirement"`
	MaxDurationMS    int64    `json:"max_duration_ms,omitempty"`
	MaxOutputBytes   int      `json:"max_output_bytes,omitempty"`
}

// handleListTools serves the capability catalog for one business scope. A
// division-scoped caller sees the same business catalog (discovery is not a
// permission); G3 membership is still enforced.
func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	if s.capability == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_FAILURE",
			"capability platform not configured")
		return
	}
	businessID := r.URL.Query().Get("business_id")
	if businessID == "" {
		s.writeError(w, r, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}
	divisionID := r.URL.Query().Get("division_id")
	if _, stopped := s.requireActorMembership(w, r, businessID); stopped {
		return
	}
	manifests := s.capability.Catalog(businessID, divisionID)
	entries := make([]toolCatalogEntry, 0, len(manifests))
	for _, m := range manifests {
		entries = append(entries, catalogEntry(m))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"business_id": businessID,
		"tools":       entries,
		"count":       len(entries),
		"note":        "discovery is informational; the runtime validates every invocation",
	})
}

func catalogEntry(m tool.ToolManifest) toolCatalogEntry {
	return toolCatalogEntry{
		ID: m.ID, Version: m.Version, Name: m.Name, Description: m.Description,
		Category: string(m.Category), Operations: m.SortedOperations(),
		SideEffectClass: string(m.SideEffectClass), SecurityClass: string(m.SecurityClass),
		NetworkNeeds:     string(m.NetworkRequirement),
		CredentialNeeded: m.CredentialRequirement.Required,
		ScopeRequirement: string(m.ScopeRequirement),
		MaxDurationMS:    m.ResourceLimits.MaxDuration.Milliseconds(),
		MaxOutputBytes:   m.ResourceLimits.MaxOutputByte,
	}
}
