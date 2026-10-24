package execjournal

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func webSearchEnvelope(sources, count string) map[string]string {
	return map[string]string{"sources": sources, "count": count}
}

// A complete documented envelope becomes one ordered snapshot. The snippet may
// contain the separator and is preserved after the three structural fields.
func TestParseWebSearchEvidenceCompleteSnapshot(t *testing.T) {
	ev, ok := ParseWebSearchEvidence(ResearchEvidenceCapability, ResearchEvidenceOperation, webSearchEnvelope(
		"1. Nexus capability boundary | https://docs.example/nexus | docs.example | The runtime decides; the model proposes.\n"+
			"2. External content is data | https://example.org/safety | example.org | Inert | data",
		"2",
	), false)
	if !ok || ev == nil {
		t.Fatal("a complete envelope must be captured")
	}
	if ev.Version != ResearchEvidenceVersion || ev.Capability != ResearchEvidenceCapability ||
		ev.Operation != ResearchEvidenceOperation || ev.Count != 2 {
		t.Fatalf("unexpected evidence header: %+v", ev)
	}
	if len(ev.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(ev.Sources))
	}
	if ev.Sources[0].Title != "Nexus capability boundary" ||
		ev.Sources[0].ID != "src-1" ||
		ev.Sources[0].URL != "https://docs.example/nexus" ||
		ev.Sources[0].Source != "docs.example" ||
		ev.Sources[0].Snippet != "The runtime decides; the model proposes." {
		t.Fatalf("first source mangled: %+v", ev.Sources[0])
	}
	if ev.Sources[1].ID != "src-2" || ev.Sources[1].Snippet != "Inert | data" {
		t.Fatalf("second source mangled: %+v", ev.Sources[1])
	}
}

// A zero-result search is evidence of an empty complete snapshot, distinct from
// no evidence at all.
func TestParseWebSearchEvidenceEmptySnapshot(t *testing.T) {
	ev, ok := ParseWebSearchEvidence(ResearchEvidenceCapability, "", webSearchEnvelope("", "0"), false)
	if !ok || ev == nil {
		t.Fatal("an empty complete result must be captured")
	}
	if ev.Count != 0 || ev.Sources == nil || len(ev.Sources) != 0 {
		t.Fatalf("empty snapshot must be present and empty, got %+v", ev)
	}
}

func TestParseWebSearchEvidenceRejectsIncompleteOrMalformed(t *testing.T) {
	cases := map[string]struct {
		toolID    string
		operation string
		result    map[string]string
		truncated bool
	}{
		"wrong tool":       {toolID: "http.request", result: webSearchEnvelope("1. A | https://a.example | a | s", "1")},
		"wrong operation":  {toolID: ResearchEvidenceCapability, operation: "fetch", result: webSearchEnvelope("1. A | https://a.example | a | s", "1")},
		"truncated":        {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("1. A | https://a.example | a | s", "1"), truncated: true},
		"missing sources":  {toolID: ResearchEvidenceCapability, result: map[string]string{"count": "0"}},
		"missing count":    {toolID: ResearchEvidenceCapability, result: map[string]string{"sources": ""}},
		"bad count":        {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("", "many")},
		"negative count":   {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("", "-1")},
		"count too large":  {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("", "51")},
		"count mismatch":   {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("1. A | https://a.example | a | s", "2")},
		"empty with count": {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("   ", "1")},
		"no separator":     {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("1. A https://a.example", "1")},
		"no index":         {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("A | https://a.example | a | s", "1")},
		"bad index":        {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("2. A | https://a.example | a | s", "1")},
		"gap index":        {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("1. A | https://a.example | a | s\n3. B | https://b.example | b | t", "2")},
		"newline field":    {toolID: ResearchEvidenceCapability, result: webSearchEnvelope("1. A\nB | https://a.example | a | s", "1")},
		"oversized URL": {toolID: ResearchEvidenceCapability, result: webSearchEnvelope(
			"1. A | https://a.example/"+strings.Repeat("x", MaxResearchSourceURL)+" | a | s", "1")},
		"oversized snippet": {toolID: ResearchEvidenceCapability, result: webSearchEnvelope(
			"1. A | https://a.example | a | "+strings.Repeat("x", MaxResearchSourceSnippet+1), "1")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if ev, ok := ParseWebSearchEvidence(tc.toolID, tc.operation, tc.result, tc.truncated); ok || ev != nil {
				t.Fatalf("malformed/partial evidence must be rejected, got %+v", ev)
			}
		})
	}
}

// URL normalization is deliberately conservative: surrounding whitespace is
// removed but the provider's original value is not rewritten. Safety is a
// presentation decision, not a durability decision.
func TestParseWebSearchEvidencePreservesOriginalURL(t *testing.T) {
	const url = "javascript:alert(1)"
	ev, ok := ParseWebSearchEvidence(ResearchEvidenceCapability, ResearchEvidenceOperation,
		webSearchEnvelope("1. Unsafe |  "+url+"  | provider | snippet", "1"), false)
	if !ok || len(ev.Sources) != 1 {
		t.Fatalf("expected a complete source record, got %+v", ev)
	}
	if ev.Sources[0].URL != url {
		t.Fatalf("URL changed: %q", ev.Sources[0].URL)
	}
}

func TestParseWebSearchEvidenceHandlesCRLFAndBlankLines(t *testing.T) {
	ev, ok := ParseWebSearchEvidence(ResearchEvidenceCapability, ResearchEvidenceOperation,
		webSearchEnvelope("\r\n1. A | https://a.example | a | s\r\n\r\n", "1"), false)
	if !ok || len(ev.Sources) != 1 || ev.Sources[0].URL != "https://a.example" {
		t.Fatalf("CRLF/blank lines must not corrupt the record: %+v ok=%v", ev, ok)
	}
}

// Required collection fields are arrays in the wire contract. Producers that
// leave them nil must not turn a present empty capture into null, and the
// normalization must not mutate the stored evidence.
func TestResearchEvidenceMarshalUsesRequiredArrays(t *testing.T) {
	ev := &ResearchEvidence{
		Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
		Operation: ResearchEvidenceOperation, Count: 0,
		Operations: []ResearchOperationEvidence{{
			OperationID: "obs-1", StepID: "s1",
			ToolID: ResearchEvidenceCapability, Operation: ResearchEvidenceOperation,
		}},
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"sources":[]`) {
		t.Fatalf("missing sources array: %s", raw)
	}
	if !strings.Contains(string(raw), `"source_ids":[]`) {
		t.Fatalf("missing source_ids array: %s", raw)
	}
	if ev.Sources != nil || ev.Operations[0].SourceIDs != nil {
		t.Fatal("marshaling must not mutate the stored evidence")
	}
}

func TestValidateResearchEvidence(t *testing.T) {
	valid := &ResearchEvidence{
		Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
		Operation: ResearchEvidenceOperation, Count: 1,
		Sources:     []ResearchSource{{ID: "src-1", Title: "A", URL: "https://a.example"}},
		Attribution: ResearchAttributionUnavailable,
	}
	if err := ValidateResearchEvidence(valid); err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}
	if err := ValidateResearchEvidence(nil); err != nil {
		t.Fatalf("nil evidence must be allowed: %v", err)
	}
	cases := map[string]*ResearchEvidence{
		"version":    {Version: 99, Capability: ResearchEvidenceCapability, Operation: ResearchEvidenceOperation},
		"capability": {Version: ResearchEvidenceVersion, Capability: "other", Operation: ResearchEvidenceOperation, Attribution: ResearchAttributionUnavailable},
		"operation":  {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability, Operation: "fetch", Attribution: ResearchAttributionUnavailable},
		"count mismatch": {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: 2, Sources: []ResearchSource{{ID: "src-1"}},
			Attribution: ResearchAttributionUnavailable},
		"oversized": {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: 1,
			Sources:     []ResearchSource{{ID: "src-1", Snippet: strings.Repeat("x", MaxResearchSourceSnippet+1)}},
			Attribution: ResearchAttributionUnavailable},
		"control": {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: 1,
			Sources:     []ResearchSource{{ID: "src-1", Title: "a\nb"}},
			Attribution: ResearchAttributionUnavailable},
		"missing source id": {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: 1,
			Sources:     []ResearchSource{{Title: "A"}},
			Attribution: ResearchAttributionUnavailable},
		"duplicate source id": {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: 2,
			Sources:     []ResearchSource{{ID: "src-1"}, {ID: "src-1"}},
			Attribution: ResearchAttributionUnavailable},
		"too many sources": {Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: MaxResearchEvidenceSources + 1,
			Sources:     make([]ResearchSource, MaxResearchEvidenceSources+1),
			Attribution: ResearchAttributionUnavailable},
	}
	for name, ev := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateResearchEvidence(ev); err == nil {
				t.Fatalf("invalid evidence accepted: %+v", ev)
			}
		})
	}
}

func provenanceEvidence(executionID string) *ResearchEvidence {
	return &ResearchEvidence{
		Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
		Operation: ResearchEvidenceOperation, Count: 3,
		Sources: []ResearchSource{
			{ID: "src-1", Title: "One", URL: "https://one.example"},
			{ID: "src-2", Title: "Two", URL: "https://two.example"},
			{ID: "src-3", Title: "Three", URL: "https://three.example"},
		},
		Plan: &ResearchPlanEvidence{
			PlanID: executionID, Version: 2, ObjectiveID: "objective-" + executionID,
			CompletionStepID: "s2", Provenance: ResearchProvenanceAuthoritative,
		},
		Steps: []ResearchStepEvidence{
			{StepID: "s1", Intent: "gather sources", PlanVersion: 1, Status: "completed",
				ObservationID: "obs-1", Provenance: ResearchProvenanceAuthoritative},
			{StepID: "s2", Intent: "synthesize findings", PlanVersion: 2, Status: "completed",
				ObservationID: "obs-2", Dependencies: []string{"s1"}, Provenance: ResearchProvenanceAuthoritative},
		},
		Operations: []ResearchOperationEvidence{
			{OperationID: "obs-1", StepID: "s1", ToolID: ResearchEvidenceCapability,
				Operation: ResearchEvidenceOperation, SourceIDs: []string{"src-1", "src-2"},
				Provenance: ResearchProvenanceAuthoritative},
			{OperationID: "obs-2", StepID: "s2", ToolID: ResearchEvidenceCapability,
				Operation: ResearchEvidenceOperation, SourceIDs: []string{"src-3"},
				Provenance: ResearchProvenanceAuthoritative},
		},
		Attribution: ResearchAttributionDerived,
	}
}

func TestLegacyResearchEvidenceV2IsReadableWithoutInventingV3Fields(t *testing.T) {
	legacy := &ResearchEvidence{
		Version: ResearchEvidenceProvenanceVersion, Capability: ResearchEvidenceCapability,
		Operation: ResearchEvidenceOperation, Count: 1,
		Sources: []ResearchSource{{ID: "src-1", Title: "Legacy v2", URL: "https://legacy.example"}},
		Plan:    &ResearchPlanEvidence{PlanID: "exec-1", Version: 2},
		Steps: []ResearchStepEvidence{
			{StepID: "s1", Status: "completed", Dependencies: []string{"s2"}},
			{StepID: "s2", Status: "completed"},
		},
		Operations: []ResearchOperationEvidence{{
			OperationID: "obs-1", StepID: "s1", ToolID: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, SourceIDs: []string{"src-1"},
		}},
	}
	if err := ValidateResearchEvidenceForExecution("exec-1", legacy); err != nil {
		t.Fatalf("v2 evidence must remain readable: %v", err)
	}
	if legacy.Attribution != "" || legacy.Steps[0].Intent != "" ||
		legacy.Steps[0].PlanVersion != 0 || legacy.Steps[0].Provenance != "" ||
		legacy.Plan.ObjectiveID != "" || legacy.Plan.CompletionStepID != "" ||
		legacy.Plan.Provenance != "" || legacy.Operations[0].Provenance != "" {
		t.Fatalf("v2 evidence must not acquire v3 fields on read: %+v", legacy)
	}

	cases := map[string]func(*ResearchEvidence){
		"attribution": func(ev *ResearchEvidence) {
			ev.Attribution = ResearchAttributionDerived
		},
		"plan objective": func(ev *ResearchEvidence) {
			ev.Plan.ObjectiveID = "obj-1"
		},
		"plan completion": func(ev *ResearchEvidence) {
			ev.Plan.CompletionStepID = "s1"
		},
		"plan provenance": func(ev *ResearchEvidence) {
			ev.Plan.Provenance = ResearchProvenanceAuthoritative
		},
		"step intent": func(ev *ResearchEvidence) {
			ev.Steps[0].Intent = "invented"
		},
		"step plan version": func(ev *ResearchEvidence) {
			ev.Steps[0].PlanVersion = 1
		},
		"step provenance": func(ev *ResearchEvidence) {
			ev.Steps[0].Provenance = ResearchProvenanceAuthoritative
		},
		"operation provenance": func(ev *ResearchEvidence) {
			ev.Operations[0].Provenance = ResearchProvenanceAuthoritative
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			ev := *legacy
			plan := *legacy.Plan
			ev.Plan = &plan
			ev.Steps = append([]ResearchStepEvidence(nil), legacy.Steps...)
			ev.Operations = append([]ResearchOperationEvidence(nil), legacy.Operations...)
			ev.Operations[0].SourceIDs = append([]string(nil), legacy.Operations[0].SourceIDs...)
			mutate(&ev)
			if err := ValidateResearchEvidence(&ev); err == nil {
				t.Fatalf("v2 evidence accepted a v3-only field: %+v", ev)
			}
		})
	}
}

func TestValidateResearchEvidenceProvenance(t *testing.T) {
	ev := provenanceEvidence("exec-1")
	if err := ValidateResearchEvidenceForExecution("exec-1", ev); err != nil {
		t.Fatalf("valid provenance rejected: %v", err)
	}
	// Plan identity is execution-scoped; a snapshot cannot be re-attributed to
	// another execution on a write or read boundary.
	if err := ValidateResearchEvidenceForExecution("exec-2", ev); err == nil {
		t.Fatal("a plan from another execution must be rejected")
	}
}

func TestValidateResearchEvidenceRejectsInvalidProvenance(t *testing.T) {
	cases := map[string]func(*ResearchEvidence){
		"steps without plan": func(ev *ResearchEvidence) {
			ev.Plan = nil
		},
		"operations without plan": func(ev *ResearchEvidence) {
			ev.Plan = nil
			ev.Steps = nil
		},
		"plan with no steps": func(ev *ResearchEvidence) {
			ev.Steps = nil
		},
		"duplicate step": func(ev *ResearchEvidence) {
			ev.Steps[1].StepID = ev.Steps[0].StepID
		},
		"invalid status": func(ev *ResearchEvidence) {
			ev.Steps[0].Status = "succeeded"
		},
		"unknown dependency": func(ev *ResearchEvidence) {
			ev.Steps[1].Dependencies = []string{"s9"}
		},
		"self dependency": func(ev *ResearchEvidence) {
			ev.Steps[0].Dependencies = []string{"s1"}
		},
		"dependency cycle": func(ev *ResearchEvidence) {
			ev.Steps[0].Dependencies = []string{"s2"}
		},
		"unsupported plan version": func(ev *ResearchEvidence) {
			ev.Plan.Version = 0
		},
		"unknown operation step": func(ev *ResearchEvidence) {
			ev.Operations[0].StepID = "s9"
		},
		"unknown operation source": func(ev *ResearchEvidence) {
			ev.Operations[0].SourceIDs[0] = "src-9"
		},
		"duplicate operation ref": func(ev *ResearchEvidence) {
			ev.Operations[0].SourceIDs = []string{"src-1", "src-1"}
		},
		"source attributed twice": func(ev *ResearchEvidence) {
			ev.Operations[1].SourceIDs = []string{"src-1"}
		},
		"unsupported operation": func(ev *ResearchEvidence) {
			ev.Operations[0].Operation = "fetch"
		},
		"duplicate operation id": func(ev *ResearchEvidence) {
			ev.Operations[1].OperationID = ev.Operations[0].OperationID
		},
		"empty operation id": func(ev *ResearchEvidence) {
			ev.Operations[0].OperationID = ""
		},
		"too many steps": func(ev *ResearchEvidence) {
			ev.Steps = make([]ResearchStepEvidence, MaxResearchPlanSteps+1)
			for i := range ev.Steps {
				ev.Steps[i] = ResearchStepEvidence{StepID: "s" + strconv.Itoa(i), Status: "pending"}
			}
		},
		"too many dependencies": func(ev *ResearchEvidence) {
			ev.Steps[0].Dependencies = make([]string, MaxResearchStepDependencies+1)
		},
		"too many operations": func(ev *ResearchEvidence) {
			ev.Operations = make([]ResearchOperationEvidence, MaxResearchOperations+1)
		},
		"too many source refs": func(ev *ResearchEvidence) {
			ev.Operations[0].SourceIDs = make([]string, MaxResearchOperationSourceRefs+1)
		},
		"missing plan provenance": func(ev *ResearchEvidence) {
			ev.Plan.Provenance = ""
		},
		"wrong plan provenance": func(ev *ResearchEvidence) {
			ev.Plan.Provenance = ResearchAttributionDerived
		},
		"missing step intent": func(ev *ResearchEvidence) {
			ev.Steps[0].Intent = ""
		},
		"missing step plan version": func(ev *ResearchEvidence) {
			ev.Steps[0].PlanVersion = 0
		},
		"step plan version after plan": func(ev *ResearchEvidence) {
			ev.Steps[0].PlanVersion = ev.Plan.Version + 1
		},
		"missing step provenance": func(ev *ResearchEvidence) {
			ev.Steps[0].Provenance = ""
		},
		"missing operation provenance": func(ev *ResearchEvidence) {
			ev.Operations[0].Provenance = ""
		},
		"missing attribution": func(ev *ResearchEvidence) {
			ev.Attribution = ""
		},
		"attribution does not match operations": func(ev *ResearchEvidence) {
			ev.Attribution = ResearchAttributionUnavailable
		},
		"unknown completion step": func(ev *ResearchEvidence) {
			ev.Plan.CompletionStepID = "s9"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			ev := provenanceEvidence("exec-1")
			mutate(ev)
			if err := ValidateResearchEvidence(ev); err == nil {
				t.Fatalf("invalid provenance accepted: %+v", ev)
			}
		})
	}
}

func TestLegacyResearchEvidenceIsReadableWithoutProvenance(t *testing.T) {
	legacy := &ResearchEvidence{
		Version: ResearchEvidenceLegacyVersion, Capability: ResearchEvidenceCapability,
		Operation: ResearchEvidenceOperation, Count: 1,
		Sources: []ResearchSource{{Title: "Legacy", URL: "https://legacy.example"}},
	}
	if err := ValidateResearchEvidenceForExecution("legacy", legacy); err != nil {
		t.Fatalf("legacy evidence must remain readable: %v", err)
	}
	legacy.Sources[0].ID = "src-1"
	if err := ValidateResearchEvidence(legacy); err == nil {
		t.Fatal("legacy evidence must not acquire invented source ids")
	}
	legacy.Sources[0].ID = ""
	legacy.Plan = &ResearchPlanEvidence{PlanID: "legacy", Version: 1}
	legacy.Steps = []ResearchStepEvidence{{StepID: "s1", Status: "completed"}}
	if err := ValidateResearchEvidence(legacy); err == nil {
		t.Fatal("legacy evidence must not acquire invented provenance")
	}
}
