package agentintel

// The loop's memory operations (contracts/AGENT_MEMORY_CONTEXT_CONTRACTS.md §7,
// §11, §13, §37). Every one of them goes through the memory platform's
// authorized path; the loop never reaches the storage implementation and never
// decides scope, provenance or trust itself.

import (
	"fmt"
	"strings"

	"github.com/Nomssky/NEXUS/internal/memory"
)

// observationsFor returns the execution's observations (newest first).
func observationsFor(w *WorkingMemory) []Observation {
	if w == nil {
		return nil
	}
	return w.Snapshot().Observations
}

// memoryWrite validates a model-proposed memory write and hands it to the
// platform.
//
// The model may propose a key, a value, a type, a scope and — as a promotion
// request — an observation id. It may never choose provenance or trust, may not
// widen its scope, and a promoted observation takes its CONTENT from the
// runtime's observation rather than from the model.
// memoryCandidate builds the candidate a write WOULD use, including the
// promotion rules. It is the single place that candidate is constructed, so the
// target resolution and the mutation can never drift apart (contract §6).
func (r *Runtime) memoryCandidate(id memory.Identity, a Action, observations []Observation) (memory.Candidate, memory.WriterKind, error) {
	cand := memory.Candidate{
		Key:   a.Key,
		Value: a.Value,
		Type:  memory.Type(a.MemoryType),
		Scope: memory.Scope(a.MemoryScope),
	}
	kind := memory.WriterAgent
	if obsID := strings.TrimSpace(a.ObservationID); obsID != "" {
		obs, ok := findObservation(observations, obsID)
		if !ok {
			return memory.Candidate{}, "", fmt.Errorf("memory: observation %q is not part of this execution", obsID)
		}
		// Promotion: content and provenance come from the runtime's own
		// observation. The reliability outcome travels with it and is never
		// rewritten (contract §13).
		cand.Value = obs.Text
		cand.Type = memory.TypeObservation
		cand.Scope = memory.ScopeAgent
		cand.ObservationID = obs.ObservationID
		cand.Outcome = obs.Outcome
		cand.Attempts = obs.Attempts
		cand.RetryRecommended = obs.RetryRecommended
		cand.ReconciliationRequired = obs.ReconciliationRequired
		if cand.Value == "" {
			return memory.Candidate{}, "", fmt.Errorf("memory: observation %q carries no content to promote", obsID)
		}
		kind = memory.WriterObservation
	}
	return cand, kind, nil
}

// memoryWriteTarget resolves the RUNTIME-ESTABLISHED destination of a write
// through the memory platform's own authorization and scope clamping, without
// mutating anything. Governance then decides about that target (contract §6).
func (r *Runtime) memoryWriteTarget(rn *run, agentID string, a Action, observations []Observation) (memory.WriteTarget, error) {
	if r.Memory == nil {
		return memory.WriteTarget{}, fmt.Errorf("memory: no memory platform configured")
	}
	id := r.memoryIdentity(rn, agentID)
	cand, kind, err := r.memoryCandidate(id, a, observations)
	if err != nil {
		return memory.WriteTarget{}, err
	}
	return r.Memory.ResolveWriteTarget(id, kind, cand)
}

// memoryDeleteTargets resolves the set of records a delete would ACTUALLY remove:
// every record the caller is authorized to see under that key, across scopes.
// It uses the same authorized query the delete then performs.
func (r *Runtime) memoryDeleteTargets(rn *run, agentID, key string) ([]string, error) {
	if r.Memory == nil || strings.TrimSpace(key) == "" {
		return nil, nil
	}
	id := r.memoryIdentity(rn, agentID)
	res, err := r.Memory.Query(id, memory.Query{BusinessID: id.BusinessID, Key: key})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(res.Records))
	for _, rec := range res.Records {
		ids = append(ids, rec.ID)
	}
	return ids, nil
}

func (r *Runtime) memoryWrite(rn *run, agentID string, a Action, observations []Observation, admitted memory.WriteTarget) (memory.Record, error) {
	if r.Memory == nil {
		return memory.Record{}, fmt.Errorf("memory: no memory platform configured")
	}
	id := r.memoryIdentity(rn, agentID)
	cand, kind, err := r.memoryCandidate(id, a, observations)
	if err != nil {
		return memory.Record{}, err
	}
	// The effect is executed against the admitted target and nothing else. The
	// platform re-derives the target under the SAME lock that guards the
	// mutation and refuses if it differs, so there is no window in which a
	// changed agent definition, membership or memory mode could redirect the
	// write to a record governance never saw (contract §6/§7). This is a
	// platform capability, not a second authorization pass here.
	rec, err := r.Memory.WriteBound(id, kind, cand, admitted)
	if err != nil {
		return memory.Record{}, err
	}
	return rec, nil
}

// memoryRead resolves one key through the authorized query path.
func (r *Runtime) memoryRead(rn *run, agentID, key string) (memory.Record, bool) {
	if r.Memory == nil || strings.TrimSpace(key) == "" {
		return memory.Record{}, false
	}
	id := r.memoryIdentity(rn, agentID)
	res, err := r.Memory.Query(id, memory.Query{BusinessID: id.BusinessID, Key: key, Limit: 1})
	if err != nil || len(res.Records) == 0 {
		return memory.Record{}, false
	}
	return res.Records[0], true
}

// memoryDeleteExactly removes EXACTLY the records already admitted by
// governance: the set resolved by memoryDeleteTargets and enforced by
// ConstrainEffect. It never re-queries: a broad authorization-scope re-read
// between admission and mutation could include a record that governance never
// saw (contract §6). The whole batch is atomic from the caller's point of view:
// if ANY record cannot be loaded or delete-authorized, none are removed and the
// caller fails closed rather than mutating a target different from the admitted
// set.
func (r *Runtime) memoryDeleteExactly(rn *run, agentID string, targets []string) (int, error) {
	if r.Memory == nil {
		return 0, nil
	}
	if len(targets) == 0 {
		return 0, nil
	}
	id := r.memoryIdentity(rn, agentID)
	if err := r.Memory.DeleteExact(id, targets); err != nil {
		return 0, err
	}
	return len(targets), nil
}

// findObservation looks up an observation produced by THIS execution.
func findObservation(observations []Observation, id string) (Observation, bool) {
	for _, o := range observations {
		if o.ObservationID == id {
			return o, true
		}
	}
	return Observation{}, false
}

// contextInputs builds the assembler's input for one decision: the objective and
// step, the authorized memory, the observations so far and the capability hints.
func (r *Runtime) contextInputs(rn *run, id memory.Identity, obj Objective, step Step,
	observations []Observation, tools []ToolHint) memory.Input {
	assembler := r.assembler()
	budget := assembler.Budget()
	memRecs := r.retrieveForContext(id, budget.MaxMemoryRecords)
	obsIn := make([]memory.ObservationInput, 0, len(observations))
	// Newest observations are the most relevant; the assembler keeps that order.
	for i := len(observations) - 1; i >= 0; i-- {
		o := observations[i]
		obsIn = append(obsIn, memory.ObservationInput{
			ID: o.ObservationID, Source: o.Source, Status: o.Status, Text: o.Text,
			Outcome: o.Outcome, Attempts: o.Attempts,
			RetryRecommended: o.RetryRecommended, ReconciliationRequired: o.ReconciliationRequired,
		})
	}
	toolHints := make([]memory.ToolHint, 0, len(tools))
	for _, t := range tools {
		toolHints = append(toolHints, memory.ToolHint{
			ID: t.ID, Operations: t.Operations, SideEffect: t.SideEffect, Summary: t.Summary,
		})
	}
	return memory.Input{
		Objective: obj.Description, StepID: step.StepID, StepIntent: step.Intent,
		Memories: memRecs, Observations: obsIn, Tools: toolHints, Budget: budget,
	}
}

// assembledMemoryRecords returns the memory records the assembler actually
// admitted, so the DecisionPrompt carries exactly what the model will see.
func assembledMemoryRecords(ctx memory.Context) []memory.Record {
	return ctx.Assembled
}

// firstNonEmpty returns the first non-empty string (small helper for telemetry).
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
