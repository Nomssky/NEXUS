// Package agentexec implements Agent Execution Layer v1: a durable agent
// registry, deterministic selection, model-floating execution that runs
// *inside* the canonical request pipeline (so identity, scope, governance,
// approvals, escalations, events, cancellation and visibility keep the exact
// semantics the rest of NEXUS enforces), bounded tool use, delegation,
// workflows (sequential and parallel), cancellation and retry/fallback
// semantics.
//
// What v1 deliberately is NOT: a parallel authorization system, a UI, a
// distributed orchestrator, or a pseudo-AI simulator baked into the runtime.
// Production execution ends at modelrouter.ModelProvider; the simulator is
// a seeded provider, never runtime logic.
package agentexec

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/schema"
)

// Status is the agent definition lifecycle (§12.2 philosophy: active,
// suspended, archived; archived terminal).
type Status string

const (
	AgentActive    Status = "active"
	AgentSuspended Status = "suspended"
	AgentArchived  Status = "archived"
)

var validStatuses = map[Status]struct{}{
	AgentActive: {}, AgentSuspended: {}, AgentArchived: {},
}

// Validate reports whether s is a canonical lifecycle status.
func (s Status) Valid() bool { _, ok := validStatuses[s]; return ok }

// transitionMatrix mirrors org transitions: archived is terminal.
var transitionMatrix = map[Status][]Status{
	AgentActive:    {AgentSuspended, AgentArchived},
	AgentSuspended: {AgentActive, AgentArchived},
	AgentArchived:  nil,
}

// MemoryConfig describes an agent's memory boundary in v1. It does not open
// memory stores; memory injection is recorded for later execution wiring.
type MemoryConfig struct {
	// Mode is the declared scope of the agent's memory retrieval:
	// "none" (default), "business" or "division".
	Mode string `json:"mode,omitempty"`
}

// ModelRequirements express what the agent needs from the router. The agent
// never names a concrete provider/model that it cannot control;
// PreferredProvider/PreferredModel are hints the router works from.
type ModelRequirements struct {
	PreferLocal       bool     `json:"prefer_local,omitempty"`
	ToolCalling       bool     `json:"tool_calling,omitempty"`
	StructuredOutput  bool     `json:"structured_output,omitempty"`
	PreferredProvider string   `json:"preferred_provider,omitempty"`
	PreferredModel    string   `json:"preferred_model,omitempty"`
	AllowedProviders  []string `json:"allowed_providers,omitempty"`
}

// Definition is the durable agent definition record — the agent analog of the
// identity/business/division record family. `AllowedTools` is the agent's
// allowlist (registration-time); actual use of a tool is still mediated by
// the tool boundary at invocation time (see tools.go).
type Definition struct {
	SchemaVersion string               `json:"schema_version"`
	EntityType    string               `json:"entity_type"`
	NexusID       string               `json:"nexus_id"`
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Description   string               `json:"description"`
	BusinessID    string               `json:"business_id"`
	DivisionID    string               `json:"division_id,omitempty"`
	Capabilities  []string             `json:"capabilities"`
	AllowedTools  []string             `json:"allowed_tools,omitempty"`
	Model         ModelRequirements    `json:"model"`
	Memory        MemoryConfig         `json:"memory"`
	Parameters    map[string]string    `json:"parameters,omitempty"`
	Status        Status               `json:"status"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
	Provenance    schema.ProvenanceRef `json:"provenance"`
}

// Validate enforces §4 record validity at registration or update.
func (d *Definition) Validate() error {
	if strings.TrimSpace(d.BusinessID) == "" {
		return fmt.Errorf("business_id is required")
	}
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(d.Capabilities) == 0 {
		return fmt.Errorf("at least one capability is required")
	}
	if !d.Status.Valid() {
		return fmt.Errorf("status %q is not canonical (active|suspended|archived)", d.Status)
	}
	if d.Memory.Mode != "" && d.Memory.Mode != "none" && d.Memory.Mode != "business" && d.Memory.Mode != "division" {
		return fmt.Errorf("memory.mode %q is not canonical (none|business|division)", d.Memory.Mode)
	}
	return nil
}

// canTransition reports whether from→to is a legal lifecycle edge.
func canTransition(from, to Status) bool {
	if from == to {
		return false
	}
	for _, n := range transitionMatrix[from] {
		if n == to {
			return true
		}
	}
	return false
}
