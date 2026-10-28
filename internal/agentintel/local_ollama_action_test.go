package agentintel

// Local-model compatibility regression tests for the documented local-model text
// variants actually observed against the installed `bonsai-2:9b` model through
// the Ollama provider. Each case below is a structural copy (or the literal
// content) of a real provider response, with fabricated-free values only —
// they exercise normalization boundaries, never the validation that follows.
//
// The two defects pinned here were found by a read-only logging proxy between
// NEXUS and Ollama during Phase 4:
//
//  1. `result` emitted as an OBJECT caused `parseActionResultObjectFallback`
//     to drop the co-present structured `synthesis`, so a valid completion
//     failed the gate afterwards as "evidence was captured but the complete
//     action has no synthesis";
//  2. a native tool call whose `arguments` inlined the tool's declared input
//     fields ({"query":"...","limit":"5"}) was rejected as `unknown field`,
//     which failed the whole decision before any capability could run.

import (
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/executor"
)

// localOllamaCompletionWithObjectResult is the real completion shape: a
// `complete` decision whose `result` is a JSON object (documented local-model
// variant) and whose `synthesis` is fully structured and citation-resolved.
func localOllamaCompletionWithObjectResult(ids ...string) string {
	body := `{
  "type": "complete",
  "prompt": "Search the web for the Kubernetes project and summarize the sources",
  "tool": null,
  "operation": null,
  "input": null,
  "objective": "Summarize Kubernetes project sources",
  "synthesis": {
    "status": "complete",
    "summary": "The Kubernetes project is a cloud-native computing foundation originally inspired by the Greek term for helmsman.",
    "evidence_backed": [
      {
        "statement": "The Kubernetes project is a cloud-native computing foundation.",
        "source_ids": [` + quoteIDs(ids) + `]
      }
    ],
    "inferences": [],
    "conflicts": [],
    "unresolved_questions": []
  },
  "result": {"text": true, "status": "completed"},
  "reason": "Web search completed with validated sources synthesizing the project overview."
}`
	return body
}

// quoteIDs renders ids as a JSON string array without importing encoding/json
// into the fixture body (keeps the captured shape literal and readable in a diff).
func quoteIDs(ids []string) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, `"`+id+`"`)
	}
	return strings.Join(out, ", ")
}

// TestLocalOllamaObjectResultKeepsStructuredSynthesis pins defect 1 at the
// protocol boundary: normalizing result-as-object must not drop the co-present
// structured synthesis, and `result` must still be coerced to a string.
func TestLocalOllamaObjectResultKeepsStructuredSynthesis(t *testing.T) {
	a, err := ParseAction(localOllamaCompletionWithObjectResult("src-1", "src-2"))
	if err != nil {
		t.Fatalf("parse real completion variant: %v", err)
	}
	if a.Type != ActionComplete {
		t.Fatalf("type = %q, want %q", a.Type, ActionComplete)
	}
	if a.Result == "" {
		t.Fatal("result-as-object must be coerced to a string, not dropped")
	}
	if a.Synthesis == nil {
		t.Fatal("the structured synthesis was dropped by result-as-object normalization")
	}
	if a.Synthesis.Status != executor.ResearchSynthesisComplete {
		t.Fatalf("synthesis status = %q", a.Synthesis.Status)
	}
	if len(a.Synthesis.EvidenceBacked) != 1 || len(a.Synthesis.EvidenceBacked[0].SourceIDs) != 2 {
		t.Fatalf("citation-resolved statements lost: %+v", a.Synthesis.EvidenceBacked)
	}
	if !strings.Contains(strings.Join(a.Synthesis.EvidenceBacked[0].SourceIDs, ","), "src-2") {
		t.Fatalf("source ids lost: %+v", a.Synthesis.EvidenceBacked[0].SourceIDs)
	}
	if a.Reason == "" {
		t.Fatal("reason must survive normalization")
	}
}

// TestLocalOllamaCompletionReachesTypedSynthesis drives the full loop with the
// real completion shape after a (text) tool call and asserts the run reaches a
// completed research outcome with a typed synthesis bound to the captured
// evidence — i.e. the defect caused exactly the observed "no synthesis" failure.
func TestLocalOllamaCompletionReachesTypedSynthesis(t *testing.T) {
	f := newEvidenceFixture(t)
	f.provider.queue(envelopeOf(
		"1. Kubernetes | https://en.wikipedia.org/wiki/Kubernetes | en.wikipedia.org | The cloud-native orchestrator.",
		"2. Helm package manager | https://en.wikipedia.org/wiki/Helm_(package_manager) | en.wikipedia.org | Package manager for Kubernetes.",
	))

	out := runEvidenceObjective(t, f,
		`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"kubernetes"}}`,
		localOllamaCompletionWithObjectResult("src-1", "src-2"))

	if out.Status != "completed" {
		t.Fatalf("expected completed research run, got status=%s error=%q", out.Status, out.Error)
	}
	if out.ResearchEvidence == nil || len(out.ResearchEvidence.Sources) != 2 {
		t.Fatalf("evidence must be captured: %+v", out.ResearchEvidence)
	}
	if out.ResearchSynthesis == nil {
		t.Fatalf("typed synthesis must survive the normalization boundary: %+v", out)
	}
	if out.ResearchSynthesis.Status != executor.ResearchSynthesisComplete {
		t.Fatalf("synthesis status = %q, want %q", out.ResearchSynthesis.Status, executor.ResearchSynthesisComplete)
	}
	// Every cited id must resolve to a captured source (the runtime owns this).
	cited := map[string]bool{}
	for _, s := range out.ResearchSynthesis.EvidenceBacked {
		for _, id := range s.SourceIDs {
			cited[id] = true
		}
	}
	for _, src := range out.ResearchEvidence.Sources {
		if !cited[src.ID] {
			t.Fatalf("captured source %q is not cited by the synthesis", src.ID)
		}
	}
	if len(cited) == 0 {
		t.Fatalf("synthesis cites nothing: %+v", out.ResearchSynthesis)
	}
}

// TestLocalOllamaProseInferencesAreNormalized pins the third real variant
// observed from the local model: `synthesis.inferences` given as an array of
// prose strings instead of {statement:"..."} entries. The text must pass
// through unchanged (inferences carry no citations) rather than failing the
// whole decision as a protocol error.
func TestLocalOllamaProseInferencesAreNormalized(t *testing.T) {
	const prose = "The project uses operators to manage application lifecycle."
	body := `{
	  "type": "complete",
	  "result": {"text": true, "status": "completed"},
	  "synthesis": {
	    "status": "complete",
	    "summary": "The Kubernetes project is cloud native.",
	    "evidence_backed": [
	      {"statement": "The Kubernetes project is cloud native.", "source_ids": ["src-1"]}
	    ],
	    "inferences": [` + "\"" + prose + `"` + `],
	    "conflicts": [],
	    "unresolved_questions": []
	  },
	  "reason": "Web search completed."
	}`
	a, err := ParseAction(body)
	if err != nil {
		t.Fatalf("prose inferences must not fail the decision: %v", err)
	}
	if a.Synthesis == nil {
		t.Fatal("synthesis missing after normalization")
	}
	if len(a.Synthesis.Inferences) != 1 {
		t.Fatalf("inferences lost: %+v", a.Synthesis.Inferences)
	}
	if a.Synthesis.Inferences[0].Statement != prose {
		t.Fatalf("inference text changed: %q", a.Synthesis.Inferences[0].Statement)
	}
	// The evidence-backed claim and its citations must survive untouched.
	if len(a.Synthesis.EvidenceBacked) != 1 || len(a.Synthesis.EvidenceBacked[0].SourceIDs) != 1 ||
		a.Synthesis.EvidenceBacked[0].SourceIDs[0] != "src-1" {
		t.Fatalf("grounded claims changed: %+v", a.Synthesis.EvidenceBacked)
	}
	if a.Synthesis.Status != executor.ResearchSynthesisComplete || a.Result == "" {
		t.Fatalf("status/result wrong: status=%q result=%q", a.Synthesis.Status, a.Result)
	}
}

// TestLocalOffameVariantGateDoesNotLoosenProtocolFailures keeps the
// normalization seam narrow: only the named field mismatches may be
// reconstructed, while unrelated malformed output still fails closed.
func TestLocalOllamaVariantGateDoesNotLoosenProtocolFailures(t *testing.T) {
	cases := map[string]string{
		"unknown action type":   `{"type":"teleport","synthesis":{"status":"complete","summary":"x"}}`,
		"duplicate keys":        `{"type":"complete","type":"fail","synthesis":{"status":"complete","summary":"x"}}`,
		"surrounding prose":     `analysis: {"type":"complete","result":{"ok":true}} done`,
		"array instead object":  `[{"type":"complete","result":{"ok":true}}]`,
		"unknown top-level key": `{"type":"complete","whoknows":1,"result":{"ok":true}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAction(payload); err == nil {
				t.Fatalf("malformed output must fail closed, got no error for %s", name)
			} else if !strings.Contains(err.Error(), "protocol") && !strings.Contains(err.Error(), "unknown action") {
				t.Fatalf("expected protocol/unknown-action error, got: %v", err)
			}
		})
	}
}

// TestLocalOllamaProseSynthesisStillFallsBackToInconclusive preserves the
// documented second variant behavior: a prose (string) synthesis is carried
// over as an inconclusive synthesis rather than rejected or fabricated into a
// grounded claim.
func TestLocalOllamaProseSynthesisStillFallsBackToInconclusive(t *testing.T) {
	body := `{
	  "type": "complete",
	  "result": {"text": true, "status": "completed"},
	  "synthesis": "The search returned no settleable claim for this objective."
	}`
	a, err := ParseAction(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Synthesis == nil {
		t.Fatal("prose synthesis must not be annihilated")
	}
	if a.Synthesis.Status != executor.ResearchSynthesisInconclusive {
		t.Fatalf("status = %q, want %q", a.Synthesis.Status, executor.ResearchSynthesisInconclusive)
	}
	if len(a.Synthesis.EvidenceBacked) != 0 {
		t.Fatal("prose must never be reified into grounded claims")
	}
	if a.Result == "" {
		t.Fatal("result must still be coerced to a string")
	}
}
