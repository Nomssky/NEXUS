// Package modelselect implements hierarchical model preference resolution
// (global < division < agent) and a deterministic, explainable,
// hardware-aware model selection policy.
//
// It is deliberately a plain library: it never admits work, never calls a
// provider, never pulls a model, and never bypasses governance or the existing
// router. It turns (preferences, candidates, task, limits, policy) into an
// explicit Decision that a caller can feed to the existing model router
// (e.g. as RoutingRequest.AllowedModels) or use for diagnostics.
package modelselect

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Nomssky/NEXUS/internal/foundation/hardware"
)

// Mode distinguishes an inherited default, deliberate automatic selection, and
// an explicit model preference. An empty string is never overloaded: use
// ModeInherit explicitly.
type Mode string

const (
	// ModeInherit defers to the next applicable scope.
	ModeInherit Mode = "inherit"
	// ModeAutomatic requests policy-driven selection at this scope.
	ModeAutomatic Mode = "automatic"
	// ModeExplicit pins a specific model at this scope.
	ModeExplicit Mode = "explicit"
)

// Preference is one scope's model preference.
type Preference struct {
	Mode    Mode   `json:"mode"`
	ModelID string `json:"model_id,omitempty"`
}

// Inherit returns the inherit preference.
func Inherit() Preference { return Preference{Mode: ModeInherit} }

// Automatic returns the automatic preference.
func Automatic() Preference { return Preference{Mode: ModeAutomatic} }

// Explicit returns an explicit preference for a non-empty model id.
func Explicit(modelID string) (Preference, error) {
	if strings.TrimSpace(modelID) == "" {
		return Preference{}, fmt.Errorf("explicit preference requires a model id")
	}
	return Preference{Mode: ModeExplicit, ModelID: modelID}, nil
}

// Validate rejects malformed preferences (an unknown mode, an explicit
// preference without a model, or a non-explicit mode carrying a model id).
func (p Preference) Validate() error {
	switch p.Mode {
	case ModeInherit, ModeAutomatic:
		if strings.TrimSpace(p.ModelID) != "" {
			return fmt.Errorf("mode %q must not carry a model_id", p.Mode)
		}
		return nil
	case ModeExplicit:
		if strings.TrimSpace(p.ModelID) == "" {
			return fmt.Errorf("explicit preference requires a model id")
		}
		return nil
	case "":
		return fmt.Errorf("preference mode is required (use %q, %q or %q)", ModeInherit, ModeAutomatic, ModeExplicit)
	default:
		return fmt.Errorf("unknown preference mode %q", p.Mode)
	}
}

// Settings is the full preference set. Absent entries inherit.
type Settings struct {
	Global    Preference            `json:"global"`
	Divisions map[string]Preference `json:"divisions,omitempty"`
	Agents    map[string]Preference `json:"agents,omitempty"`
}

// Source identifies where the resolved preference came from.
type Source string

const (
	SourceAgent     Source = "agent"
	SourceDivision  Source = "division"
	SourceGlobal    Source = "global"
	SourceAutomatic Source = "automatic"
)

// Resolved is the effective preference for a scope plus its provenance.
type Resolved struct {
	Preference Preference `json:"preference"`
	Source     Source     `json:"source"`
}

// Resolve applies precedence agent > division > global > automatic. An absent or
// inherit override defers to the next scope. Validation is fail-closed: a
// malformed stored preference is an error, never silently ignored.
func Resolve(s Settings, divisionID, agentID string) (Resolved, error) {
	if err := s.Global.Validate(); err != nil {
		return Resolved{}, fmt.Errorf("global preference: %w", err)
	}
	if agentID != "" {
		if p, ok := s.Agents[agentID]; ok {
			if err := p.Validate(); err != nil {
				return Resolved{}, fmt.Errorf("agent preference: %w", err)
			}
			if p.Mode != ModeInherit {
				return Resolved{Preference: p, Source: SourceAgent}, nil
			}
		}
	}
	if divisionID != "" {
		if p, ok := s.Divisions[divisionID]; ok {
			if err := p.Validate(); err != nil {
				return Resolved{}, fmt.Errorf("division preference: %w", err)
			}
			if p.Mode != ModeInherit {
				return Resolved{Preference: p, Source: SourceDivision}, nil
			}
		}
	}
	if s.Global.Mode != ModeInherit {
		return Resolved{Preference: s.Global, Source: SourceGlobal}, nil
	}
	return Resolved{Preference: Automatic(), Source: SourceAutomatic}, nil
}

// Candidate is one selectable model with the facts the policy may rely on. It
// carries no capability inferred from a model name.
type Candidate struct {
	ModelID             string   `json:"model_id"`
	ProviderID          string   `json:"provider_id"`
	Installed           bool     `json:"installed"`
	Capabilities        []string `json:"capabilities,omitempty"`
	ContextWindow       int      `json:"context_window,omitempty"`
	MemoryEstimateBytes int64    `json:"memory_estimate_bytes,omitempty"`
	// MemoryKnown reports whether MemoryEstimateBytes is a real estimate rather
	// than a placeholder.
	MemoryKnown bool `json:"memory_known"`
	// QualityTier is operator-configured (higher is preferred). It is never
	// derived from the model name.
	QualityTier int `json:"quality_tier,omitempty"`
	// PricingInput is the per-input-token price for paid models. Zero for free
	// and local models.
	PricingInput float64 `json:"pricing_input,omitempty"`
	// PricingOutput is the per-output-token price for paid models. Zero for
	// free and local models.
	PricingOutput float64 `json:"pricing_output,omitempty"`
	// Source classifies the model as "free", "paid", or "local". Empty means
	// unknown.
	Source string `json:"source,omitempty"`
}

// Task describes what the execution needs.
type Task struct {
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
	MinContextTokens     int      `json:"min_context_tokens,omitempty"`
	// EstimatedInputTokens is the caller's best estimate of input tokens for
	// this invocation. Used for cost-aware selection when provided.
	EstimatedInputTokens int `json:"estimated_input_tokens,omitempty"`
	// EstimatedOutputTokens is the caller's best estimate of output tokens for
	// this invocation. Used for cost-aware selection when provided.
	EstimatedOutputTokens int `json:"estimated_output_tokens,omitempty"`
}

// Limits is the resource envelope for a selection.
type Limits struct {
	AvailableMemoryBytes int64 `json:"available_memory_bytes,omitempty"`
	MemoryKnown          bool  `json:"memory_known"`
	MaxConcurrency       int   `json:"max_concurrency,omitempty"`
}

// Policy controls fallback and uncertainty handling.
type Policy struct {
	// AllowFallback permits automatic selection when an explicit preference
	// cannot be honored.
	AllowFallback bool `json:"allow_fallback"`
	// StrictPreference forbids falling back away from an explicit preference.
	StrictPreference bool `json:"strict_preference"`
	// AllowUnknownMemory permits selecting a model whose memory need cannot be
	// established. When false, an uncertain candidate is refused.
	AllowUnknownMemory bool `json:"allow_unknown_memory"`
}

// Rejection records why a candidate was not chosen.
type Rejection struct {
	ModelID string `json:"model_id"`
	Reason  string `json:"reason"`
}

// Decision is the explainable result of a selection.
type Decision struct {
	ModelID             string      `json:"model_id"`
	ProviderID          string      `json:"provider_id"`
	Source              Source      `json:"source"`
	ResolvedPreference  Preference  `json:"resolved_preference"`
	FallbackUsed        bool        `json:"fallback_used"`
	Estimated           bool        `json:"estimated"`
	MissingHardwareData bool        `json:"missing_hardware_data"`
	Explanation         string      `json:"explanation"`
	Rejections          []Rejection `json:"rejections,omitempty"`
}

// LimitsFromInventory derives selection limits from a hardware snapshot,
// reserving reserveBytes of RAM for the OS and other processes.
func LimitsFromInventory(inv hardware.Inventory, reserveBytes int64) Limits {
	if !inv.RAMKnown {
		return Limits{MemoryKnown: false}
	}
	avail := inv.AvailableRAMBytes
	if reserveBytes > 0 {
		avail -= reserveBytes
	}
	if avail < 0 {
		avail = 0
	}
	return Limits{AvailableMemoryBytes: avail, MemoryKnown: true}
}

// Select resolves the effective preference against the candidate set and returns
// a deterministic, explainable decision. It fails with an actionable diagnostic
// when no safe candidate exists; it never guesses.
func Select(resolved Resolved, candidates []Candidate, task Task, limits Limits, policy Policy) (Decision, error) {
	if err := resolved.Preference.Validate(); err != nil {
		return Decision{}, fmt.Errorf("resolved preference: %w", err)
	}
	byID := make(map[string]Candidate, len(candidates))
	for _, c := range candidates {
		byID[c.ModelID] = c
	}

	var rejections []Rejection
	if resolved.Preference.Mode == ModeExplicit {
		want := resolved.Preference.ModelID
		c, ok := byID[want]
		if !ok {
			rejections = append(rejections, Rejection{ModelID: want, Reason: "not among the known candidates"})
		} else if reason, _ := ineligible(c, task, limits); reason != "" {
			rejections = append(rejections, Rejection{ModelID: want, Reason: reason})
		} else {
			return decisionFor(c, resolved, false, false, resolved.Preference, rejections), nil
		}
		if policy.StrictPreference || !policy.AllowFallback {
			return Decision{}, fmt.Errorf("explicit model %q is not usable and fallback is not permitted: %s",
				want, reasonList(rejections))
		}
		// Fall through to automatic selection as an observable fallback.
		auto, err := automatic(resolved.Preference, candidates, task, limits, policy, rejections)
		if err != nil {
			return Decision{}, err
		}
		auto.FallbackUsed = true
		auto.Explanation = "explicit preference " + want + " was unusable; " + auto.Explanation
		return auto, nil
	}

	return automatic(resolved.Preference, candidates, task, limits, policy, rejections)
}

func automatic(pref Preference, candidates []Candidate, task Task, limits Limits, policy Policy, rejections []Rejection) (Decision, error) {
	type scored struct {
		c         Candidate
		uncertain bool
	}
	var ok []scored
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.ModelID] {
			continue
		}
		seen[c.ModelID] = true
		if reason, uncertain := ineligible(c, task, limits); reason != "" {
			rejections = append(rejections, Rejection{ModelID: c.ModelID, Reason: reason})
			continue
		} else {
			ok = append(ok, scored{c: c, uncertain: uncertain})
		}
	}
	if len(ok) == 0 {
		return Decision{}, fmt.Errorf("no eligible model for the task: %s", reasonList(rejections))
	}
	// Deterministic preference: when token estimates are provided, rank by
	// estimated total cost (input * pricing_input + output * pricing_output).
	// Otherwise, higher quality tier, then smaller estimated memory (known
	// before unknown), then provider, then model id.
	sort.SliceStable(ok, func(i, j int) bool {
		a, b := ok[i].c, ok[j].c
		// Cost-aware ranking: cheaper total estimated cost first.
		if task.EstimatedInputTokens > 0 || task.EstimatedOutputTokens > 0 {
			costA := float64(task.EstimatedInputTokens)*a.PricingInput + float64(task.EstimatedOutputTokens)*a.PricingOutput
			costB := float64(task.EstimatedInputTokens)*b.PricingInput + float64(task.EstimatedOutputTokens)*b.PricingOutput
			// Unknown pricing (both prices zero on a paid model) is treated as
			// infinitely expensive — never the cheapest choice.
			aUnknown := a.Source == "paid" && a.PricingInput == 0 && a.PricingOutput == 0
			bUnknown := b.Source == "paid" && b.PricingInput == 0 && b.PricingOutput == 0
			if aUnknown && !bUnknown {
				return false // a is more expensive than b
			}
			if !aUnknown && bUnknown {
				return true // a is cheaper than b
			}
			if costA != costB {
				return costA < costB
			}
		}
		if a.QualityTier != b.QualityTier {
			return a.QualityTier > b.QualityTier
		}
		if a.MemoryKnown != b.MemoryKnown {
			return a.MemoryKnown // known sizes preferred over unknown
		}
		if a.MemoryEstimateBytes != b.MemoryEstimateBytes {
			return a.MemoryEstimateBytes < b.MemoryEstimateBytes
		}
		if a.ProviderID != b.ProviderID {
			return a.ProviderID < b.ProviderID
		}
		return a.ModelID < b.ModelID
	})
	chosen := ok[0]
	if chosen.uncertain && !policy.AllowUnknownMemory {
		return Decision{}, fmt.Errorf("best candidate %q has unknown memory requirements and unknown-memory selection is disabled: %s",
			chosen.c.ModelID, reasonList(rejections))
	}
	return decisionFor(chosen.c, Resolved{Preference: pref, Source: SourceAutomatic}, false, chosen.uncertain, pref, rejections), nil
}

// ineligible returns a non-empty reason when the candidate cannot serve the
// task; uncertain reports that the eligibility rests on missing data.
func ineligible(c Candidate, task Task, limits Limits) (reason string, uncertain bool) {
	if !c.Installed {
		return "not installed locally", false
	}
	for _, want := range task.RequiredCapabilities {
		if !hasCapability(c.Capabilities, want) {
			return "missing capability " + want, false
		}
	}
	if task.MinContextTokens > 0 && c.ContextWindow > 0 && c.ContextWindow < task.MinContextTokens {
		return fmt.Sprintf("context window %d < required %d", c.ContextWindow, task.MinContextTokens), false
	}
	if limits.MemoryKnown {
		if c.MemoryKnown {
			if c.MemoryEstimateBytes > limits.AvailableMemoryBytes {
				return fmt.Sprintf("estimated %d bytes exceeds available %d bytes", c.MemoryEstimateBytes, limits.AvailableMemoryBytes), false
			}
		} else {
			uncertain = true
		}
	} else {
		uncertain = true
	}
	return "", uncertain
}

func decisionFor(c Candidate, resolved Resolved, fallback, uncertain bool, pref Preference, rejections []Rejection) Decision {
	expl := fmt.Sprintf("selected %q (provider %s) via %s", c.ModelID, c.ProviderID, resolved.Source)
	if fallback {
		expl += " (fallback)"
	}
	if uncertain {
		expl += "; memory requirement could not be established (uncertain)"
	}
	return Decision{
		ModelID:             c.ModelID,
		ProviderID:          c.ProviderID,
		Source:              resolved.Source,
		ResolvedPreference:  pref,
		FallbackUsed:        fallback,
		Estimated:           c.MemoryKnown,
		MissingHardwareData: uncertain,
		Explanation:         expl,
		Rejections:          rejections,
	}
}

func hasCapability(list []string, want string) bool {
	for _, c := range list {
		if c == want {
			return true
		}
	}
	return false
}

func reasonList(rs []Rejection) string {
	if len(rs) == 0 {
		return "no candidates supplied"
	}
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		parts = append(parts, r.ModelID+": "+r.Reason)
	}
	return strings.Join(parts, "; ")
}
