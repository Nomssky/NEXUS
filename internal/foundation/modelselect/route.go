package modelselect

import (
	"fmt"

	"github.com/Nomssky/NEXUS/internal/foundation/hardware"
)

// Scope is the runtime-established business scope of one routing attempt
// (division/agent only; business identity is enforced upstream by the gateway
// and never derived from model output).
type Scope struct {
	DivisionID string
	AgentID    string
}

// PrefSource supplies LIVE hierarchical preference lookups backed by the
// existing durable stores (config file for global, agent registry for agent
// overrides). Callbacks are required for the levels that have durable state;
// a nil callback means that level has no persistence today (the override
// defers exactly like an unstored value — never like a silent explicit pin).
type PrefSource struct {
	Global   Preference
	Division func(divisionID string) (Preference, error)
	Agent    func(agentID string) (Preference, error)
}

// Resolve applies `agent > division > global > automatic`. A malformed stored
// preference fails closed with its scope named.
func (p PrefSource) Resolve(scope Scope) (Resolved, error) {
	if err := p.Global.Validate(); err != nil {
		return Resolved{}, fmt.Errorf("global model preference: %w", err)
	}
	if scope.AgentID != "" && p.Agent != nil {
		pref, err := p.Agent(scope.AgentID)
		if err != nil {
			return Resolved{}, err
		}
		if err := pref.Validate(); err != nil {
			return Resolved{}, fmt.Errorf("agent preference: %w", err)
		}
		if pref.Mode != ModeInherit {
			return Resolved{Preference: pref, Source: SourceAgent}, nil
		}
	}
	if scope.DivisionID != "" && p.Division != nil {
		pref, err := p.Division(scope.DivisionID)
		if err != nil {
			return Resolved{}, err
		}
		if err := pref.Validate(); err != nil {
			return Resolved{}, fmt.Errorf("division preference: %w", err)
		}
		if pref.Mode != ModeInherit {
			return Resolved{Preference: pref, Source: SourceDivision}, nil
		}
	}
	if p.Global.Mode != ModeInherit {
		return Resolved{Preference: p.Global, Source: SourceGlobal}, nil
	}
	return Resolved{Preference: Automatic(), Source: SourceAutomatic}, nil
}

// RoutingFacts are runtime-supplied observations for ONE routing attempt. No
// fact here comes from model output: candidates come from the registry,
// installation state from the registered provider's local discovery, limits
// from a bounded hardware snapshot, and preferences from the durable stores.
type RoutingFacts struct {
	Prefs  PrefSource
	Scope  Scope
	Task   Task
	Limits Limits
	Policy Policy

	// Candidates are the registry's models already capability-filtered for the
	// request (i.e. registry models that satisfy Task.RequiredCapabilities).
	Candidates []Candidate

	// Installed is model-ID → locally installed, as observed through the
	// registered discovery-capable provider (kind=ollama).
	Installed      map[string]bool
	InstalledKnown bool
	Inventory      hardware.Inventory
	ReserveBytes   int64
}

// Choose resolves the effective preference and runs the deterministic selector.
//
// When installation state is OBSERVED, every candidate is re-marked against
// it: a registry model that is configured but not installed on the local
// runtime becomes ineligible (Installed=false) instead of being assumed to
// exist. When installation state is UNKNOWN (no discoverer configured — e.g.
// remote-only setups), the configured candidates stand, and uncertainty flows
// through the decision instead of silently fabricating availability.
func Choose(f RoutingFacts) (Decision, error) {
	resolved, err := f.Prefs.Resolve(f.Scope)
	if err != nil {
		return Decision{}, err
	}
	cands := f.Candidates
	if f.InstalledKnown {
		marked := make([]Candidate, len(cands))
		for i, c := range cands {
			c.Installed = f.Installed[c.ModelID]
			marked[i] = c
		}
		cands = marked
	}
	limits := f.Limits
	// The caller may pass explicit limits; when its memory is unknown, a
	// fresh hardware snapshot still upgrades them conservatively.
	if !limits.MemoryKnown && f.Inventory.RAMKnown {
		limits = LimitsFromInventory(f.Inventory, f.ReserveBytes)
		limits.MaxConcurrency = f.Limits.MaxConcurrency
	}
	return Select(resolved, cands, f.Task, limits, f.Policy)
}
