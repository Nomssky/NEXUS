package agentintel

import (
	"fmt"
	"sort"
)

// Step state machine (contracts/AGENT_EXECUTION_DEPTH_CONTRACTS.md §3).
//
// The ledger is runtime-owned scratch state for ONE plan version of ONE
// execution: the model can neither see it as writable nor modify it — plans
// and decisions have no status fields, and unknown fields fail closed at the
// protocol boundary. Completed records are historical fact: replans carry
// them forward by step id, never rewrite them.

// StepStatus is the runtime-owned lifecycle of one plan step.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepReady     StepStatus = "ready"
	StepExecuting StepStatus = "executing"
	StepObserving StepStatus = "observing"
	StepCompleted StepStatus = "completed"
	StepFailed    StepStatus = "failed"
	StepBlocked   StepStatus = "blocked"
	StepCancelled StepStatus = "cancelled"
)

// StepTerminal reports whether no further transition is legal.
func (s StepStatus) Terminal() bool {
	switch s {
	case StepCompleted, StepFailed, StepBlocked, StepCancelled:
		return true
	default:
		return false
	}
}

// StepRecord is the authoritative fact about one step: what happened, under
// which plan version, evidenced by which observation. Result carries the
// human-readable outcome; Outcome carries the normalized reliability outcome
// (completed/failed/timed_out/cancelled/unknown) when the step ran an effect.
type StepRecord struct {
	StepID        string
	Status        StepStatus
	PlanVersion   int
	ObservationID string
	Result        string
	Outcome       string
	Attempts      int
}

// planLedger tracks every step of one active plan version in plan order.
type planLedger struct {
	planID  string
	version int
	order   []string
	steps   map[string]*Step
	records map[string]*StepRecord
}

// newPlanLedger starts every step as pending. The plan must already be
// structurally validated: unknown dependencies and cycles never reach here.
func newPlanLedger(plan *Plan) *planLedger {
	l := &planLedger{
		planID: plan.PlanID, version: plan.Version,
		steps:   make(map[string]*Step, len(plan.Steps)),
		records: make(map[string]*StepRecord, len(plan.Steps)),
	}
	for i := range plan.Steps {
		s := &plan.Steps[i]
		l.order = append(l.order, s.StepID)
		cp := *s
		l.steps[s.StepID] = &cp
		l.records[s.StepID] = &StepRecord{StepID: s.StepID, Status: StepPending, PlanVersion: plan.Version}
	}
	return l
}

// refresh advances pending steps whose dependencies resolved: all completed
// → ready; any failed/blocked/cancelled → blocked (transitively, one call
// handles chains because it loops to a fixed point). It returns the ids that
// changed, in plan order, so the caller can emit one event per transition.
func (l *planLedger) refresh() []string {
	var changed []string
	for {
		progress := false
		for _, id := range l.order {
			rec := l.records[id]
			if rec.Status != StepPending {
				continue
			}
			blocked := false
			ready := true
			for _, d := range l.steps[id].Dependencies {
				dep, ok := l.records[d]
				if !ok || dep.Status != StepCompleted {
					ready = false
				}
				if ok && (dep.Status == StepFailed || dep.Status == StepBlocked || dep.Status == StepCancelled) {
					blocked = true
				}
			}
			if blocked {
				rec.Status = StepBlocked
				changed = append(changed, id)
				progress = true
			} else if ready {
				rec.Status = StepReady
				changed = append(changed, id)
				progress = true
			}
		}
		if !progress {
			return changed
		}
	}
}

// nextReady returns the first ready step in plan order, or nil when no step
// may execute. Deterministic: the model cannot reorder execution by array
// position when dependencies say otherwise.
func (l *planLedger) nextReady() *Step {
	for _, id := range l.order {
		if l.records[id].Status == StepReady {
			return l.steps[id]
		}
	}
	return nil
}

// get returns the record for a step id, or nil for an unknown id.
func (l *planLedger) get(stepID string) *StepRecord {
	return l.records[stepID]
}

// begin marks a ready step executing. Anything else is a runtime bug, failed
// closed — the loop never starts a step the ledger did not offer.
func (l *planLedger) begin(stepID string) error {
	rec := l.records[stepID]
	if rec == nil {
		return fmt.Errorf("ledger: unknown step %q", stepID)
	}
	if rec.Status != StepReady {
		return fmt.Errorf("ledger: step %q is %s, not ready", stepID, rec.Status)
	}
	rec.Status = StepExecuting
	return nil
}

// observe marks an executing step observing.
func (l *planLedger) observe(stepID string) error {
	rec := l.records[stepID]
	if rec == nil {
		return fmt.Errorf("ledger: unknown step %q", stepID)
	}
	if rec.Status != StepExecuting {
		return fmt.Errorf("ledger: step %q is %s, not executing", stepID, rec.Status)
	}
	rec.Status = StepObserving
	return nil
}

// finish marks an observing step completed or failed. Completed requires an
// observation id: a step completes only on runtime evidence, never on model
// assertion. Terminal records are immutable — any second finish is refused.
func (l *planLedger) finish(stepID, observationID, result, outcome string, attempts int, failed bool) error {
	rec := l.records[stepID]
	if rec == nil {
		return fmt.Errorf("ledger: unknown step %q", stepID)
	}
	if rec.Status.Terminal() {
		return fmt.Errorf("ledger: step %q is already %s: completed results are immutable", stepID, rec.Status)
	}
	if rec.Status != StepObserving {
		return fmt.Errorf("ledger: step %q is %s, not observing", stepID, rec.Status)
	}
	if !failed && observationID == "" {
		return fmt.Errorf("ledger: step %q cannot complete without an observation", stepID)
	}
	rec.ObservationID = observationID
	rec.Result = result
	rec.Outcome = outcome
	rec.Attempts = attempts
	if failed {
		rec.Status = StepFailed
	} else {
		rec.Status = StepCompleted
	}
	return nil
}

// cancel marks every non-terminal step cancelled.
func (l *planLedger) cancel() []string {
	var changed []string
	for _, id := range l.order {
		if !l.records[id].Status.Terminal() {
			l.records[id].Status = StepCancelled
			changed = append(changed, id)
		}
	}
	return changed
}

// counts summarizes the ledger for context rendering and telemetry.
func (l *planLedger) counts() map[StepStatus]int {
	out := map[StepStatus]int{}
	for _, id := range l.order {
		out[l.records[id].Status]++
	}
	return out
}

// allTerminal reports whether every step reached a terminal status.
func (l *planLedger) allTerminal() bool {
	for _, id := range l.order {
		if !l.records[id].Status.Terminal() {
			return false
		}
	}
	return true
}

// settledIDs returns the ids in a status class, in plan order.
func (l *planLedger) settledIDs(status StepStatus) []string {
	var out []string
	for _, id := range l.order {
		if l.records[id].Status == status {
			out = append(out, id)
		}
	}
	return out
}

// carryCompleted builds the ledger for a replan, carrying completed steps
// forward by step id with their original evidence and version. Everything
// else starts pending under the new version: failed steps may be retried
// only through the newly validated plan, and completed effects are never
// re-executed automatically. A completed step the new plan drops stays a
// historical fact in the old ledger object; the new ledger only knows steps
// the new plan declares.
func (l *planLedger) carryCompleted(next *Plan) *planLedger {
	nl := newPlanLedger(next)
	for _, id := range nl.order {
		old, ok := l.records[id]
		if !ok || old.Status != StepCompleted {
			continue
		}
		nl.records[id] = &StepRecord{
			StepID: id, Status: StepCompleted, PlanVersion: old.PlanVersion,
			ObservationID: old.ObservationID, Result: old.Result,
			Outcome: old.Outcome, Attempts: old.Attempts,
		}
	}
	return nl
}

// describe renders the authoritative step state for the next model decision.
// It is data about runtime state, carried in the user message — never an
// instruction, and never writable by the model.
func (l *planLedger) describe() string {
	parts := make([]string, 0, len(l.order))
	for _, id := range l.order {
		rec := l.records[id]
		part := id + "=" + string(rec.Status)
		if rec.ObservationID != "" {
			part += "(" + rec.ObservationID + ")"
		}
		parts = append(parts, part)
	}
	return "[" + joinStrings(parts, ", ") + "]"
}

func joinStrings(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// sortedKeys returns map keys in deterministic order (telemetry only).
func sortedKeys(m map[StepStatus]int) []StepStatus {
	out := make([]StepStatus, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
