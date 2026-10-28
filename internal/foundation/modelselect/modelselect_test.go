package modelselect

import (
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/hardware"
)

func TestResolvePrecedence(t *testing.T) {
	global, _ := Explicit("global-model")
	div, _ := Explicit("division-model")
	ag, _ := Explicit("agent-model")
	settings := Settings{
		Global:    global,
		Divisions: map[string]Preference{"d1": div},
		Agents:    map[string]Preference{"a1": ag},
	}
	cases := []struct {
		name               string
		division, agent    string
		wantModel, wantSrc string
	}{
		{"agent wins", "d1", "a1", "agent-model", "agent"},
		{"division when no agent", "d1", "a2", "division-model", "division"},
		{"global when neither", "d2", "a2", "global-model", "global"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := Resolve(settings, c.division, c.agent)
			if err != nil {
				t.Fatal(err)
			}
			if r.Preference.ModelID != c.wantModel || string(r.Source) != c.wantSrc {
				t.Fatalf("got %+v want model=%s source=%s", r, c.wantModel, c.wantSrc)
			}
		})
	}
}

func TestResolveInheritAndAutomaticDefault(t *testing.T) {
	// An inherit override defers to the next scope; an all-inherit set is automatic.
	global, _ := Explicit("g")
	r, err := Resolve(Settings{Global: global, Agents: map[string]Preference{"a": Inherit()}}, "d", "a")
	if err != nil || r.Preference.ModelID != "g" || r.Source != SourceGlobal {
		t.Fatalf("inherit must defer: %+v err=%v", r, err)
	}
	r, err = Resolve(Settings{Global: Inherit()}, "", "")
	if err != nil || r.Source != SourceAutomatic || r.Preference.Mode != ModeAutomatic {
		t.Fatalf("all-inherit must resolve to automatic: %+v err=%v", r, err)
	}
}

func TestResolveRejectsInvalidPreferences(t *testing.T) {
	if _, err := Resolve(Settings{Global: Preference{Mode: ""}}, "", ""); err == nil {
		t.Fatal("empty mode must be rejected, not treated as inherit")
	}
	if _, err := Resolve(Settings{Global: Preference{Mode: ModeExplicit}}, "", ""); err == nil {
		t.Fatal("explicit without a model id must be rejected")
	}
	if _, err := Resolve(Settings{Global: Preference{Mode: ModeAutomatic, ModelID: "x"}}, "", ""); err == nil {
		t.Fatal("automatic with a model id must be rejected")
	}
}

func baseCandidates() []Candidate {
	return []Candidate{
		{ModelID: "small", ProviderID: "ollama", Installed: true, Capabilities: []string{"reasoning"},
			ContextWindow: 8192, MemoryEstimateBytes: 4 << 30, MemoryKnown: true, QualityTier: 1},
		{ModelID: "big-tools", ProviderID: "ollama", Installed: true, Capabilities: []string{"reasoning", "tool_calling"},
			ContextWindow: 32768, MemoryEstimateBytes: 12 << 30, MemoryKnown: true, QualityTier: 3},
		{ModelID: "remote", ProviderID: "cloud", Installed: false, Capabilities: []string{"reasoning"},
			ContextWindow: 128000, MemoryEstimateBytes: 0, MemoryKnown: false, QualityTier: 5},
	}
}

func TestSelectExplicitEligible(t *testing.T) {
	pref, _ := Explicit("small")
	dec, err := Select(Resolved{Preference: pref, Source: SourceAgent}, baseCandidates(),
		Task{RequiredCapabilities: []string{"reasoning"}}, Limits{MemoryKnown: true, AvailableMemoryBytes: 16 << 30}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if dec.ModelID != "small" || dec.Source != SourceAgent || dec.FallbackUsed {
		t.Fatalf("unexpected decision: %+v", dec)
	}
}

func TestSelectExplicitMissingFailsOrFallsBack(t *testing.T) {
	pref, _ := Explicit("ghost")
	_, err := Select(Resolved{Preference: pref, Source: SourceGlobal}, baseCandidates(),
		Task{}, Limits{MemoryKnown: true, AvailableMemoryBytes: 16 << 30}, Policy{})
	if err == nil {
		t.Fatal("missing explicit model with no fallback must fail")
	}
	dec, err := Select(Resolved{Preference: pref, Source: SourceGlobal}, baseCandidates(),
		Task{RequiredCapabilities: []string{"reasoning"}}, Limits{MemoryKnown: true, AvailableMemoryBytes: 16 << 30},
		Policy{AllowFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.FallbackUsed || dec.ModelID == "ghost" {
		t.Fatalf("expected observable fallback: %+v", dec)
	}
}

func TestSelectAutomaticPrefersQualityThenSmallerMemory(t *testing.T) {
	dec, err := Select(Resolved{Preference: Automatic(), Source: SourceAutomatic}, baseCandidates(),
		Task{RequiredCapabilities: []string{"reasoning", "tool_calling"}},
		Limits{MemoryKnown: true, AvailableMemoryBytes: 16 << 30}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if dec.ModelID != "big-tools" {
		t.Fatalf("only tool-capable eligible model should win: %+v", dec)
	}
}

func TestSelectRejectionsAreExplainable(t *testing.T) {
	dec, err := Select(Resolved{Preference: Automatic(), Source: SourceAutomatic}, baseCandidates(),
		Task{RequiredCapabilities: []string{"reasoning"}, MinContextTokens: 16000},
		Limits{MemoryKnown: true, AvailableMemoryBytes: 16 << 30}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	// big-tools has context and fits; small is rejected on context; remote is not installed.
	if dec.ModelID != "big-tools" {
		t.Fatalf("got %s", dec.ModelID)
	}
	if len(dec.Rejections) == 0 {
		t.Fatal("expected recorded rejections")
	}
}

func TestSelectNoEligibleCandidateFails(t *testing.T) {
	_, err := Select(Resolved{Preference: Automatic(), Source: SourceAutomatic}, baseCandidates(),
		Task{RequiredCapabilities: []string{"vision"}}, Limits{MemoryKnown: true, AvailableMemoryBytes: 64 << 30}, Policy{})
	if err == nil || !strings.Contains(err.Error(), "no eligible model") {
		t.Fatalf("expected actionable diagnostic, got %v", err)
	}
}

func TestSelectUnknownMemoryPolicy(t *testing.T) {
	cands := []Candidate{{ModelID: "m", ProviderID: "ollama", Installed: true, Capabilities: []string{"reasoning"}}}
	_, err := Select(Resolved{Preference: Automatic(), Source: SourceAutomatic}, cands,
		Task{RequiredCapabilities: []string{"reasoning"}}, Limits{MemoryKnown: true, AvailableMemoryBytes: 8 << 30}, Policy{})
	if err == nil {
		t.Fatal("unknown memory must be refused when AllowUnknownMemory is false")
	}
	dec, err := Select(Resolved{Preference: Automatic(), Source: SourceAutomatic}, cands,
		Task{}, Limits{MemoryKnown: true, AvailableMemoryBytes: 8 << 30}, Policy{AllowUnknownMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.MissingHardwareData {
		t.Fatalf("uncertain selection must be flagged: %+v", dec)
	}
}

func TestSelectResourceInsufficiency(t *testing.T) {
	pref, _ := Explicit("big-tools")
	_, err := Select(Resolved{Preference: pref, Source: SourceAgent}, baseCandidates(),
		Task{}, Limits{MemoryKnown: true, AvailableMemoryBytes: 4 << 30}, Policy{})
	if err == nil || !strings.Contains(err.Error(), "exceeds available") {
		t.Fatalf("expected memory rejection, got %v", err)
	}
}

func TestLimitsFromInventory(t *testing.T) {
	if got := LimitsFromInventory(hardware.Inventory{RAMKnown: false}, 0); got.MemoryKnown {
		t.Fatal("unknown RAM must propagate as unknown")
	}
	got := LimitsFromInventory(hardware.Inventory{RAMKnown: true, AvailableRAMBytes: 8 << 30}, 2<<30)
	if !got.MemoryKnown || got.AvailableMemoryBytes != 6<<30 {
		t.Fatalf("reserve not applied: %+v", got)
	}
}

func TestSelectCostAwareChoosesCheapest(t *testing.T) {
	cands := []Candidate{
		{ModelID: "expensive", ProviderID: "cloud", Installed: true, Capabilities: []string{"reasoning"},
			ContextWindow: 8192, PricingInput: 0.010, PricingOutput: 0.030, Source: "paid",
			MemoryEstimateBytes: 4 << 30, MemoryKnown: true},
		{ModelID: "cheap", ProviderID: "ollama", Installed: true, Capabilities: []string{"reasoning"},
			ContextWindow: 8192, PricingInput: 0.001, PricingOutput: 0.002, Source: "local",
			MemoryEstimateBytes: 4 << 30, MemoryKnown: true},
	}
	dec, err := Select(Resolved{Preference: Automatic(), Source: SourceAutomatic}, cands,
		Task{RequiredCapabilities: []string{"reasoning"}, EstimatedInputTokens: 1000, EstimatedOutputTokens: 500},
		Limits{MemoryKnown: true, AvailableMemoryBytes: 16 << 30}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if dec.ModelID != "cheap" {
		t.Fatalf("expected cheapest model, got %s", dec.ModelID)
	}
	if dec.Source != SourceAutomatic {
		t.Fatalf("expected automatic source, got %s", dec.Source)
	}
}

func TestSelectCostAwareSkipsUnaffordablePaid(t *testing.T) {
	cands := []Candidate{
		{ModelID: "expensive", ProviderID: "cloud", Installed: true, Capabilities: []string{"reasoning"},
			ContextWindow: 8192, PricingInput: 10.0, PricingOutput: 30.0, Source: "paid",
			MemoryEstimateBytes: 4 << 30, MemoryKnown: true},
		{ModelID: "cheap", ProviderID: "ollama", Installed: true, Capabilities: []string{"reasoning"},
			ContextWindow: 8192, PricingInput: 0.001, PricingOutput: 0.002, Source: "local",
			MemoryEstimateBytes: 4 << 30, MemoryKnown: true},
	}
	dec, err := Select(Resolved{Preference: Automatic(), Source: SourceAutomatic}, cands,
		Task{RequiredCapabilities: []string{"reasoning"}, EstimatedInputTokens: 1000, EstimatedOutputTokens: 500},
		Limits{MemoryKnown: true, AvailableMemoryBytes: 16 << 30}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if dec.ModelID != "cheap" {
		t.Fatalf("expected cheap model when expensive exceeds budget, got %s", dec.ModelID)
	}
}

func TestSelectUnknownPricingNotRankedFirst(t *testing.T) {
	cands := []Candidate{
		{ModelID: "unknown-price", ProviderID: "cloud", Installed: true, Capabilities: []string{"reasoning"},
			ContextWindow: 8192, Source: "paid", MemoryEstimateBytes: 4 << 30, MemoryKnown: true},
		{ModelID: "cheap", ProviderID: "ollama", Installed: true, Capabilities: []string{"reasoning"},
			ContextWindow: 8192, PricingInput: 0.001, PricingOutput: 0.002, Source: "local",
			MemoryEstimateBytes: 4 << 30, MemoryKnown: true},
	}
	dec, err := Select(Resolved{Preference: Automatic(), Source: SourceAutomatic}, cands,
		Task{RequiredCapabilities: []string{"reasoning"}, EstimatedInputTokens: 1000, EstimatedOutputTokens: 500},
		Limits{MemoryKnown: true, AvailableMemoryBytes: 16 << 30}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if dec.ModelID != "cheap" {
		t.Fatalf("expected cheap model when pricing unknown for other, got %s", dec.ModelID)
	}
}

func TestSelectThinkingDisabledNotInferred(t *testing.T) {
	// A model with thinking capability declared should not be assumed to
	// support structured output unless explicitly listed.
	cands := []Candidate{
		{ModelID: "thinking-model", ProviderID: "ollama", Installed: true, Capabilities: []string{"reasoning"},
			ContextWindow: 8192, PricingInput: 0.001, PricingOutput: 0.002, Source: "local"},
	}
	_, err := Select(Resolved{Preference: Automatic(), Source: SourceAutomatic}, cands,
		Task{RequiredCapabilities: []string{"structured_output"}},
		Limits{}, Policy{})
	if err == nil {
		t.Fatal("thinking-only model should not satisfy structured_output requirement")
	}
}
