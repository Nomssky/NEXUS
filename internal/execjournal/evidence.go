package execjournal

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Research source evidence is an optional, additive part of an execution
// record. It stores only the source records the web.search capability returned
// in a complete, validated result envelope, plus the runtime-owned plan/step
// provenance that explains which mediated search produced each source. It never
// stores the model output, the search query, a publication date, an author, an
// intent, or a claim relationship: those cannot be derived from the documented
// tool result without inventing them.
//
// Presence means complete. A partial, malformed, truncated or oversized
// envelope is rejected wholesale by ParseWebSearchEvidence, so a durable
// record can never advertise a source list that was only partly captured.
//
// v2 adds optional plan/step/operation provenance. v3 adds the runtime-owned
// objective reference, completion-step relationship, step intent/plan revision
// and explicit provenance markers. v1 and v2 records remain readable and are
// presented as legacy/unavailable provenance: no intent, objective, completion
// relationship or authoritative marker is ever invented for them.
const (
	// ResearchEvidenceVersion is the durable evidence schema version written
	// today. It is independent of SchemaVersion so the execution record can add
	// optional evidence without making older records unreadable.
	ResearchEvidenceVersion = 3
	// ResearchEvidenceProvenanceVersion is the v54/v59 schema: plan, step and
	// operation provenance without step intent or explicit provenance markers.
	// It is accepted on read but never written.
	ResearchEvidenceProvenanceVersion = 2
	// ResearchEvidenceLegacyVersion is the v52/v53 schema. It is accepted on
	// read but never written.
	ResearchEvidenceLegacyVersion = 1
	// ResearchProvenanceAuthoritative marks fields the runtime recorded at the
	// moment the fact occurred. It is never assigned on read or inferred.
	ResearchProvenanceAuthoritative = "authoritative"
	// ResearchAttributionDerived marks source-to-step grouping derived from
	// runtime-written operation records. It is a deterministic join, not an
	// entailment or truth claim about the source content.
	ResearchAttributionDerived = "derived"
	// ResearchAttributionUnavailable marks a snapshot whose sources could not be
	// attributed to a recorded step. The sources stay honest but unattributed.
	ResearchAttributionUnavailable = "unavailable"
	// ResearchEvidenceCapability is the only capability whose result is
	// recognized today.
	ResearchEvidenceCapability = "web.search"
	// ResearchEvidenceOperation is the documented operation for that capability.
	ResearchEvidenceOperation = "search"

	// MaxResearchEvidenceSources bounds the durable source list. The shipped
	// tool budget allows at most 8 web.search calls of at most 10 results each,
	// so this leaves headroom without making a journal record unbounded.
	MaxResearchEvidenceSources = 50
	// Provenance bounds. A plan is a snapshot of the active plan version at the
	// terminal transition, not an unbounded history. Operations are the
	// mediated searches the runtime actually performed.
	MaxResearchPlanSteps             = 64
	MaxResearchStepDependencies      = 32
	MaxResearchOperations            = 64
	MaxResearchOperationSourceRefs   = MaxResearchEvidenceSources
	MaxResearchPlanID                = 128
	MaxResearchStepID                = 128
	MaxResearchObservationID         = 128
	MaxResearchOperationID           = 128
	MaxResearchSourceID              = 64
	MaxResearchStepIntent            = 2000 // mirrors maxPlanIntentLen in internal/agentintel/plan.go
	MaxResearchObjectiveID           = 128  // mirrors maxPlanIdentifierLen; objective references are execution-scoped ids
	MaxResearchProvenancePlanVersion = 1_000_000
	// Per-field bounds. They are deliberately generous for normal provider
	// output but small enough to keep one record bounded. An over-limit value
	// rejects the whole snapshot rather than silently truncating provenance.
	MaxResearchSourceTitle   = 512
	MaxResearchSourceURL     = 2048
	MaxResearchSourceName    = 256
	MaxResearchSourceSnippet = 1024
)

// ResearchSource is one normalized source record from a web.search result.
// URL is the provider's original value with surrounding whitespace removed;
// the journal never rewrites it. The frontend applies the existing http(s)
// link-safety rule before rendering an anchor.
//
// ID is a stable, execution-scoped identifier assigned by the runtime when the
// source is captured. It is empty only in legacy v1 records, where no ID was
// ever recorded.
type ResearchSource struct {
	ID      string `json:"id,omitempty"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Source  string `json:"source"`
	Snippet string `json:"snippet"`
}

// ResearchPlanEvidence is the identity of the active plan version at the
// terminal transition. PlanID is the execution-scoped plan identity assigned by
// the runtime and must match the durable execution record id. ObjectiveID is
// the runtime-owned objective the plan was built for; it is optional because
// legacy captures may not record it, but when present it binds the plan to its
// objective for auditability.
type ResearchPlanEvidence struct {
	PlanID      string `json:"plan_id"`
	Version     int    `json:"version"`
	ObjectiveID string `json:"objective_id,omitempty"`
	// CompletionStepID names the step whose completion action settled the run.
	// It is optional because a non-completed run has no completion step and
	// legacy records never captured one.
	CompletionStepID string `json:"completion_step_id,omitempty"`
	// Provenance is ResearchProvenanceAuthoritative on v3 records. It is empty
	// on legacy v2 records.
	Provenance string `json:"provenance,omitempty"`
}

// ResearchStepEvidence is the runtime-owned lifecycle fact for one step in the
// final plan snapshot. Status is one of the existing ledger states; it is not a
// success claim beyond what the runtime actually recorded. Intent is the
// runtime-owned step declaration carried from the plan; it is present for
// auditability and never invented by the journal.
type ResearchStepEvidence struct {
	StepID        string   `json:"step_id"`
	Intent        string   `json:"intent,omitempty"`
	PlanVersion   int      `json:"plan_version,omitempty"`
	Dependencies  []string `json:"dependencies,omitempty"`
	Status        string   `json:"status"`
	ObservationID string   `json:"observation_id,omitempty"`
	// Provenance is ResearchProvenanceAuthoritative on v3 records. It is empty
	// on legacy v2 records.
	Provenance string `json:"provenance,omitempty"`
}

// ResearchOperationEvidence connects one mediated web.search operation to the
// step that was executing when the runtime invoked it. SourceIDs refer to
// ResearchSource.ID values in the same evidence object.
type ResearchOperationEvidence struct {
	OperationID string   `json:"operation_id"`
	StepID      string   `json:"step_id"`
	ToolID      string   `json:"tool_id"`
	Operation   string   `json:"operation"`
	SourceIDs   []string `json:"source_ids"`
	// Provenance is ResearchProvenanceAuthoritative on v3 records. It is empty
	// on legacy v2 records.
	Provenance string `json:"provenance,omitempty"`
}

// ResearchEvidence is the bounded, versioned source/provenance snapshot for one
// execution. Count is the count declared by the provider envelope; it must equal
// len(Sources).
//
// v3: Plan/Steps/Operations carry explicit authoritative provenance, step
// intent and the step's plan revision; Attribution states whether sources were
// deterministically grouped by operation records. v2: same shape without
// intent, objective, completion relationship or explicit provenance markers.
// v1: legacy evidence with no IDs or provenance.
type ResearchEvidence struct {
	Version    int                         `json:"version"`
	Capability string                      `json:"capability"`
	Operation  string                      `json:"operation"`
	Count      int                         `json:"count"`
	Sources    []ResearchSource            `json:"sources"`
	Plan       *ResearchPlanEvidence       `json:"plan,omitempty"`
	Steps      []ResearchStepEvidence      `json:"steps,omitempty"`
	Operations []ResearchOperationEvidence `json:"operations,omitempty"`
	// Attribution is the evidence-level source-to-step grouping state. v3
	// requires ResearchAttributionDerived when operations exist and
	// ResearchAttributionUnavailable otherwise. It is empty on legacy records.
	Attribution string `json:"attribution,omitempty"`
}

// MarshalJSON makes required collection fields serialize as arrays even when a
// producer left the Go slice nil. A complete empty capture must read as
// sources:[] (and an operation with no results as source_ids:[]), never null;
// the read side treats null as malformed rather than evidence. The receiver is
// a value and the operations are copied, so marshaling never mutates stored
// evidence.
func (ev ResearchEvidence) MarshalJSON() ([]byte, error) {
	type researchEvidenceJSON ResearchEvidence
	out := researchEvidenceJSON(ev)
	if out.Sources == nil {
		out.Sources = []ResearchSource{}
	}
	if out.Operations != nil {
		out.Operations = append([]ResearchOperationEvidence(nil), ev.Operations...)
		for i := range out.Operations {
			if out.Operations[i].SourceIDs == nil {
				out.Operations[i].SourceIDs = []string{}
			}
		}
	}
	return json.Marshal(out)
}

// ParseWebSearchEvidence recognizes the documented web.search result envelope
// {sources: "1. Title | URL | Source | Snippet\n...", count: "N"} and returns a
// complete evidence snapshot, or ok=false. It is intentionally all-or-nothing:
// missing keys, a count mismatch, a malformed line, a non-sequential index, an
// oversized field, or a truncated platform result rejects the entire snapshot.
//
// The separator is the existing contract's literal " | ". A title that itself
// contains that separator is ambiguous in the provider's own output; the
// parser follows the same position-based rule as the frontend, and therefore
// never invents extra structure. Snippets may contain the separator and are
// preserved after the third field.
//
// Source IDs returned here are local to this envelope. The loop reassigns them
// to execution-scoped IDs when it merges the envelope into the run snapshot.
func ParseWebSearchEvidence(toolID, operation string, result map[string]string, truncated bool) (*ResearchEvidence, bool) {
	if truncated || toolID != ResearchEvidenceCapability {
		return nil, false
	}
	if operation != "" && operation != ResearchEvidenceOperation {
		return nil, false
	}
	rawSources, ok := result["sources"]
	if !ok {
		return nil, false
	}
	rawCount, ok := result["count"]
	if !ok {
		return nil, false
	}
	count, err := strconv.Atoi(strings.TrimSpace(rawCount))
	if err != nil || count < 0 || count > MaxResearchEvidenceSources {
		return nil, false
	}

	if strings.TrimSpace(rawSources) == "" {
		if count != 0 {
			return nil, false
		}
		return &ResearchEvidence{
			Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, Count: 0, Sources: []ResearchSource{},
			Attribution: ResearchAttributionUnavailable,
		}, true
	}

	sources := make([]ResearchSource, 0, count)
	for _, rawLine := range strings.Split(rawSources, "\n") {
		line := strings.TrimSuffix(rawLine, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, " | ")
		if len(parts) < 2 {
			return nil, false
		}
		index, title, ok := parseSourceIndex(parts[0])
		if !ok || index != len(sources)+1 {
			return nil, false
		}
		url := strings.TrimSpace(parts[1])
		source, snippet := "", ""
		if len(parts) > 2 {
			source = strings.TrimSpace(parts[2])
		}
		if len(parts) > 3 {
			snippet = strings.TrimSpace(strings.Join(parts[3:], " | "))
		}
		if !validSourceField(title, MaxResearchSourceTitle) ||
			!validSourceField(url, MaxResearchSourceURL) ||
			!validSourceField(source, MaxResearchSourceName) ||
			!validSourceField(snippet, MaxResearchSourceSnippet) {
			return nil, false
		}
		sources = append(sources, ResearchSource{
			ID: sourceID(index), Title: title, URL: url, Source: source, Snippet: snippet,
		})
	}
	if len(sources) != count {
		return nil, false
	}
	return &ResearchEvidence{
		Version: ResearchEvidenceVersion, Capability: ResearchEvidenceCapability,
		Operation: ResearchEvidenceOperation, Count: count, Sources: sources,
		Attribution: ResearchAttributionUnavailable,
	}, true
}

// sourceID is the stable execution-scoped identity assigned to a captured
// source. It is derived only from the runtime's append position, never from
// provider titles, URLs, order within a provider envelope, or timestamps.
func sourceID(index int) string {
	return "src-" + strconv.Itoa(index)
}

// parseSourceIndex parses the "N. Title" prefix emitted by the capability
// adapter. A missing or malformed prefix is not guessed at.
func parseSourceIndex(value string) (int, string, bool) {
	value = strings.TrimSpace(value)
	dot := strings.IndexByte(value, '.')
	if dot <= 0 || dot > 6 {
		return 0, "", false
	}
	n, err := strconv.Atoi(value[:dot])
	if err != nil || n < 1 {
		return 0, "", false
	}
	rest := value[dot+1:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	return n, strings.TrimSpace(rest), true
}

func validSourceField(value string, max int) bool {
	return len(value) <= max && !strings.ContainsAny(value, "\x00\n\r")
}

func validProvenanceField(value string, max int) bool {
	return value != "" && len(value) <= max && !strings.ContainsAny(value, "\x00\n\r")
}

// ValidateResearchEvidence checks an evidence snapshot at the durable write and
// decode boundaries. It is intentionally strict; a record that cannot be
// proven complete is not exposed. Legacy v1 records are accepted exactly as
// they were written: no IDs and no provenance are invented for them. Legacy v2
// records are accepted with plan/step/operation provenance but without v3-only
// fields, so a v2 record can never silently acquire intent or authoritative
// markers.
func ValidateResearchEvidence(ev *ResearchEvidence) error {
	if ev == nil {
		return nil
	}
	switch ev.Version {
	case ResearchEvidenceLegacyVersion:
		if ev.Plan != nil || len(ev.Steps) > 0 || len(ev.Operations) > 0 ||
			ev.Attribution != "" {
			return fmt.Errorf("execjournal: legacy research evidence carries provenance")
		}
		return validateResearchEvidenceBase(ev, false)
	case ResearchEvidenceProvenanceVersion:
		if ev.Attribution != "" {
			return fmt.Errorf("execjournal: legacy research evidence carries v3 attribution")
		}
		if err := validateResearchEvidenceBase(ev, true); err != nil {
			return err
		}
		return validateResearchProvenance(ev, ResearchEvidenceProvenanceVersion)
	case ResearchEvidenceVersion:
		if err := validateResearchEvidenceBase(ev, true); err != nil {
			return err
		}
		return validateResearchProvenance(ev, ResearchEvidenceVersion)
	default:
		return fmt.Errorf("execjournal: unsupported research evidence version %d", ev.Version)
	}
}

// ValidateResearchEvidenceForExecution applies the record identity invariant:
// a plan snapshot belongs to exactly one execution and its PlanID must match
// that execution's durable id. Legacy v1 evidence carries no plan and is
// unaffected.
func ValidateResearchEvidenceForExecution(executionID string, ev *ResearchEvidence) error {
	if err := ValidateResearchEvidence(ev); err != nil {
		return err
	}
	if ev == nil || ev.Version < ResearchEvidenceProvenanceVersion || ev.Plan == nil {
		return nil
	}
	if executionID == "" || ev.Plan.PlanID != executionID {
		return fmt.Errorf("execjournal: research plan %q does not belong to execution %q",
			ev.Plan.PlanID, executionID)
	}
	return nil
}

func validateResearchEvidenceBase(ev *ResearchEvidence, v2 bool) error {
	if ev.Capability != ResearchEvidenceCapability {
		return fmt.Errorf("execjournal: unsupported research evidence capability %q", ev.Capability)
	}
	if ev.Operation != ResearchEvidenceOperation {
		return fmt.Errorf("execjournal: unsupported research evidence operation %q", ev.Operation)
	}
	if ev.Count < 0 || ev.Count > MaxResearchEvidenceSources {
		return fmt.Errorf("execjournal: research evidence count %d out of range", ev.Count)
	}
	if ev.Count != len(ev.Sources) {
		return fmt.Errorf("execjournal: research evidence count %d does not match %d sources",
			ev.Count, len(ev.Sources))
	}
	ids := make(map[string]struct{}, len(ev.Sources))
	for i, source := range ev.Sources {
		if !validSourceField(source.Title, MaxResearchSourceTitle) ||
			!validSourceField(source.URL, MaxResearchSourceURL) ||
			!validSourceField(source.Source, MaxResearchSourceName) ||
			!validSourceField(source.Snippet, MaxResearchSourceSnippet) {
			return fmt.Errorf("execjournal: research evidence source %d exceeds its bounds", i)
		}
		if !v2 {
			if source.ID != "" {
				return fmt.Errorf("execjournal: legacy research evidence source %d carries an id", i)
			}
			continue
		}
		if !validProvenanceField(source.ID, MaxResearchSourceID) {
			return fmt.Errorf("execjournal: research evidence source %d has an invalid id", i)
		}
		if _, dup := ids[source.ID]; dup {
			return fmt.Errorf("execjournal: research evidence source id %q is duplicated", source.ID)
		}
		ids[source.ID] = struct{}{}
	}
	return nil
}

func validateResearchProvenance(ev *ResearchEvidence, version int) error {
	v3 := version == ResearchEvidenceVersion
	if ev.Plan == nil {
		if len(ev.Steps) > 0 || len(ev.Operations) > 0 {
			return fmt.Errorf("execjournal: research steps/operations require a plan")
		}
		if v3 && ev.Attribution != ResearchAttributionUnavailable {
			return fmt.Errorf("execjournal: research evidence without a plan must report unavailable attribution")
		}
		return nil
	}
	if !validProvenanceField(ev.Plan.PlanID, MaxResearchPlanID) {
		return fmt.Errorf("execjournal: research plan has an invalid id")
	}
	if ev.Plan.Version < 1 || ev.Plan.Version > MaxResearchProvenancePlanVersion {
		return fmt.Errorf("execjournal: research plan version %d out of range", ev.Plan.Version)
	}
	if v3 {
		if ev.Plan.Provenance != ResearchProvenanceAuthoritative {
			return fmt.Errorf("execjournal: research plan provenance is not authoritative")
		}
		if ev.Plan.ObjectiveID != "" &&
			!validProvenanceField(ev.Plan.ObjectiveID, MaxResearchObjectiveID) {
			return fmt.Errorf("execjournal: research plan objective id is invalid")
		}
		if ev.Plan.CompletionStepID != "" &&
			!validProvenanceField(ev.Plan.CompletionStepID, MaxResearchStepID) {
			return fmt.Errorf("execjournal: research plan completion step id is invalid")
		}
	} else if ev.Plan.ObjectiveID != "" || ev.Plan.CompletionStepID != "" || ev.Plan.Provenance != "" {
		return fmt.Errorf("execjournal: legacy research plan carries v3-only fields")
	}
	if len(ev.Steps) == 0 || len(ev.Steps) > MaxResearchPlanSteps {
		return fmt.Errorf("execjournal: research plan step count %d out of range", len(ev.Steps))
	}

	stepIndex := make(map[string]int, len(ev.Steps))
	for i, step := range ev.Steps {
		if !validProvenanceField(step.StepID, MaxResearchStepID) {
			return fmt.Errorf("execjournal: research step %d has an invalid id", i)
		}
		if _, dup := stepIndex[step.StepID]; dup {
			return fmt.Errorf("execjournal: research step id %q is duplicated", step.StepID)
		}
		stepIndex[step.StepID] = i
		if !validStepStatus(step.Status) {
			return fmt.Errorf("execjournal: research step %q has invalid status %q", step.StepID, step.Status)
		}
		if len(step.Dependencies) > MaxResearchStepDependencies {
			return fmt.Errorf("execjournal: research step %q has too many dependencies", step.StepID)
		}
		seen := make(map[string]struct{}, len(step.Dependencies))
		for _, dep := range step.Dependencies {
			if !validProvenanceField(dep, MaxResearchStepID) || dep == step.StepID {
				return fmt.Errorf("execjournal: research step %q has an invalid dependency", step.StepID)
			}
			if _, dup := seen[dep]; dup {
				return fmt.Errorf("execjournal: research step %q has a duplicate dependency %q", step.StepID, dep)
			}
			seen[dep] = struct{}{}
		}
		if step.ObservationID != "" && !validProvenanceField(step.ObservationID, MaxResearchObservationID) {
			return fmt.Errorf("execjournal: research step %q has an invalid observation id", step.StepID)
		}
		if v3 {
			if !validProvenanceField(step.Intent, MaxResearchStepIntent) {
				return fmt.Errorf("execjournal: research step %q has an invalid intent", step.StepID)
			}
			if step.PlanVersion < 1 || step.PlanVersion > MaxResearchProvenancePlanVersion ||
				step.PlanVersion > ev.Plan.Version {
				return fmt.Errorf("execjournal: research step %q has plan version %d outside the plan snapshot",
					step.StepID, step.PlanVersion)
			}
			if step.Provenance != ResearchProvenanceAuthoritative {
				return fmt.Errorf("execjournal: research step %q provenance is not authoritative", step.StepID)
			}
		} else if step.Intent != "" || step.PlanVersion != 0 || step.Provenance != "" {
			return fmt.Errorf("execjournal: legacy research step %q carries v3-only fields", step.StepID)
		}
	}
	for _, step := range ev.Steps {
		for _, dep := range step.Dependencies {
			if _, ok := stepIndex[dep]; !ok {
				return fmt.Errorf("execjournal: research step %q depends on unknown step %q", step.StepID, dep)
			}
		}
	}
	if researchStepsHaveCycle(ev.Steps) {
		return fmt.Errorf("execjournal: research step dependencies contain a cycle")
	}
	if v3 && ev.Plan.CompletionStepID != "" {
		if _, ok := stepIndex[ev.Plan.CompletionStepID]; !ok {
			return fmt.Errorf("execjournal: research plan completion step %q is not in the snapshot",
				ev.Plan.CompletionStepID)
		}
	}

	if len(ev.Operations) > MaxResearchOperations {
		return fmt.Errorf("execjournal: research operation count %d out of range", len(ev.Operations))
	}
	sourceIDs := make(map[string]struct{}, len(ev.Sources))
	for _, source := range ev.Sources {
		sourceIDs[source.ID] = struct{}{}
	}
	operationIDs := make(map[string]struct{}, len(ev.Operations))
	referencedSources := make(map[string]struct{}, len(ev.Sources))
	for i, op := range ev.Operations {
		if !validProvenanceField(op.OperationID, MaxResearchOperationID) {
			return fmt.Errorf("execjournal: research operation %d has an invalid id", i)
		}
		if _, dup := operationIDs[op.OperationID]; dup {
			return fmt.Errorf("execjournal: research operation id %q is duplicated", op.OperationID)
		}
		operationIDs[op.OperationID] = struct{}{}
		if _, ok := stepIndex[op.StepID]; !ok {
			return fmt.Errorf("execjournal: research operation %q references unknown step %q",
				op.OperationID, op.StepID)
		}
		if op.ToolID != ResearchEvidenceCapability || op.Operation != ResearchEvidenceOperation {
			return fmt.Errorf("execjournal: research operation %q has an unsupported tool/operation",
				op.OperationID)
		}
		if v3 && op.Provenance != ResearchProvenanceAuthoritative {
			return fmt.Errorf("execjournal: research operation %q provenance is not authoritative",
				op.OperationID)
		}
		if !v3 && op.Provenance != "" {
			return fmt.Errorf("execjournal: legacy research operation %q carries v3-only provenance",
				op.OperationID)
		}
		if len(op.SourceIDs) > MaxResearchOperationSourceRefs {
			return fmt.Errorf("execjournal: research operation %q has too many source references",
				op.OperationID)
		}
		seen := make(map[string]struct{}, len(op.SourceIDs))
		for _, sid := range op.SourceIDs {
			if _, ok := sourceIDs[sid]; !ok {
				return fmt.Errorf("execjournal: research operation %q references unknown source %q",
					op.OperationID, sid)
			}
			if _, dup := seen[sid]; dup {
				return fmt.Errorf("execjournal: research operation %q repeats source %q",
					op.OperationID, sid)
			}
			if _, reused := referencedSources[sid]; reused {
				return fmt.Errorf("execjournal: research source %q is attributed to multiple operations", sid)
			}
			seen[sid] = struct{}{}
			referencedSources[sid] = struct{}{}
		}
	}
	if v3 {
		expected := ResearchAttributionUnavailable
		if len(ev.Operations) > 0 {
			expected = ResearchAttributionDerived
		}
		if ev.Attribution != expected {
			return fmt.Errorf("execjournal: research evidence attribution %q does not match its operations",
				ev.Attribution)
		}
	}
	return nil
}

func validStepStatus(status string) bool {
	switch status {
	case "pending", "ready", "executing", "observing",
		"completed", "failed", "blocked", "cancelled":
		return true
	default:
		return false
	}
}

func researchStepsHaveCycle(steps []ResearchStepEvidence) bool {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := make(map[string]int, len(steps))
	deps := make(map[string][]string, len(steps))
	for _, step := range steps {
		deps[step.StepID] = step.Dependencies
	}
	var visit func(string) bool
	visit = func(id string) bool {
		switch color[id] {
		case grey:
			return true
		case black:
			return false
		}
		color[id] = grey
		for _, dep := range deps[id] {
			if visit(dep) {
				return true
			}
		}
		color[id] = black
		return false
	}
	for _, step := range steps {
		if visit(step.StepID) {
			return true
		}
	}
	return false
}
