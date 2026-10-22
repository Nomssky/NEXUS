package agentintel

import (
	"fmt"
	"slices"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
)

// Resume continuation (contracts/APPROVAL_LIFECYCLE_CONTRACTS.md §6).
//
// When a run pauses on REQUIRE_APPROVAL, the engine re-submits the SAME
// request on approval, which re-enters Run from the top. Without memory of
// the paused run that re-entry would start blank: completed effects could
// run again, budgets would reset, and the deadline would extend. The resume
// registry below makes the re-entry a CONTINUATION instead: the paused run
// snapshots its authoritative state, and the resumed run restores it after
// verifying it is resuming the same execution (id plus objective, business,
// division and actor must all match).
//
// Everything here is process-local by explicit decision (G4): a restart
// drops approvals and snapshots together, so no half-state can authorize an
// effect. Snapshots are consumed on load, verified before use, and bounded.

// maxResumeSnapshots bounds waiting executions held for resume. Eviction is
// FIFO; an evicted execution resumes blank (historical behavior), never
// wrongly — verification would reject a mismatched snapshot anyway.
const maxResumeSnapshots = 256

// objectiveIdentity is what a re-submitted objective must still say for a
// re-entry to CONTINUE the waiting execution instead of silently replacing it
// (APPROVAL_LIFECYCLE_CONTRACTS §6: "Approving re-submits the SAME request").
//
// The description alone is not that identity. An objective also carries its
// success criteria and its effective budget, and a resume inherits the ORIGINAL
// plan, ledger, observations, completed-effect fingerprints and consumed
// counters. Matching on the description alone would let a re-submission swap in
// weaker criteria or a wider budget while keeping the original run's authority
// and its protection against re-executing completed effects. Both are bound
// here so the restore is verified against what actually governs the execution.
//
// The budget bound is the CLAMPED budget — the autonomy the run actually got —
// not the raw request. Callers may only tighten (§9), so an unset field and an
// explicitly-cap value are the same grant and must not read as a change.
type objectiveIdentity struct {
	description string
	criteria    []criterionKey
	budget      Budget
}

// newObjectiveIdentity captures an objective's resume identity from its parsed
// description, its parsed criteria and its already-clamped budget. Both inputs
// are the normalized forms the loop itself uses, so nothing here re-parses or
// re-normalizes anything that could drift from the running execution.
func newObjectiveIdentity(obj Objective, budget Budget, crits []criterion) objectiveIdentity {
	return objectiveIdentity{
		description: obj.Description,
		criteria:    criteriaIdentity(crits),
		budget:      budget,
	}
}

// matches reports whether a re-submitted objective is the same objective.
// Comparison is by value over immutable data, and the criteria slice is
// compared element-wise, so a snapshot and a live request share no mutable
// state.
func (id objectiveIdentity) matches(other objectiveIdentity) bool {
	return id.description == other.description &&
		id.budget == other.budget &&
		slices.Equal(id.criteria, other.criteria)
}

// runSnapshot is the authoritative scratch state of one paused execution:
// everything the loop needs to continue instead of restarting.
type runSnapshot struct {
	// identity pins the snapshot to one execution of one objective.
	identity   objectiveIdentity
	businessID string
	divisionID string
	actorID    string
	// plan is the validated active plan; ledger carries per-step facts.
	plan   *Plan
	ledger *planLedger
	// observations in loop (chronological) order.
	observations []Observation
	// usage continues every counter: budgets never reset across resume.
	usage BudgetUsage
	// effects fingerprints every completed mutating effect, so resumed
	// runs prune instead of re-executing.
	effects map[string]struct{}
	// entries are the bounded working-memory scratch entries.
	entries map[string]string
	// deadline is the ORIGINAL caller deadline: waiting never extends it.
	deadline time.Time
	// depth and planVersion continue the execution's position.
	depth       int
	planVersion int
	// researchEvidence carries source records captured before the pause, so a
	// resumed run neither loses them nor re-runs the search. Overflow is
	// latched too: once a capture was incomplete, it must stay absent.
	researchEvidence  *execjournal.ResearchEvidence
	evidenceOverflow  bool
	researchAttempted bool
}

// snapshotRun stores the paused run's state for its resume. Only the
// pending_approval path calls this, right before returning.
func (r *Runtime) snapshotRun(rn *run, id objectiveIdentity, plan *Plan, ledger *planLedger,
	observations []Observation, usage BudgetUsage, working *WorkingMemory, deadline time.Time) {
	rn.finalizeResearchEvidence(plan, ledger)
	snap := &runSnapshot{
		identity:          id,
		businessID:        rn.businessID,
		divisionID:        rn.divisionID,
		actorID:           rn.req.ActorID,
		plan:              copyPlan(plan),
		observations:      copyObservations(observations),
		usage:             usage,
		effects:           working.Effects(),
		entries:           map[string]string{},
		deadline:          deadline,
		depth:             rn.depth,
		planVersion:       rn.planVersion,
		researchEvidence:  copyResearchEvidence(rn.researchEvidence),
		evidenceOverflow:  rn.evidenceOverflow,
		researchAttempted: rn.researchAttempted,
	}
	for k, v := range working.Snapshot().Entries {
		snap.entries[k] = v
	}
	snap.ledger = copyLedger(plan, ledger)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resumes == nil {
		r.resumes = map[string]*runSnapshot{}
	}
	if _, ok := r.resumes[rn.execID]; !ok {
		r.resumeOrder = append(r.resumeOrder, rn.execID)
	}
	r.resumes[rn.execID] = snap
	for len(r.resumeOrder) > maxResumeSnapshots {
		delete(r.resumes, r.resumeOrder[0])
		r.resumeOrder = r.resumeOrder[1:]
	}
}

// takeResume loads the snapshot for an execution id and consumes it ONLY when
// the request verifies as the same execution. A mismatch is not a resume: it
// starts fresh (APPROVAL_LIFECYCLE_CONTRACTS §6, "mismatches start fresh") and
// leaves the waiting execution's snapshot untouched, so a caller that
// re-submitted the wrong objective cannot destroy a valid snapshot it was never
// entitled to continue. The original attempt may still resume it afterwards.
//
// Verification and consumption share ONE critical section, so a pair of
// concurrent matching callers cannot both win: the loser's lookup observes the
// map after the winner's delete and finds nothing.
func (r *Runtime) takeResume(execID string, id objectiveIdentity, req *executor.WorkRequest) (*runSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := r.resumes[execID]
	if snap == nil {
		return nil, false
	}
	// Verify before consuming. Nothing below this point has touched the
	// registry, so a rejection here is non-destructive by construction rather
	// than by cleanup after the fact.
	if !snap.identity.matches(id) || snap.businessID != req.BusinessID ||
		snap.divisionID != req.DivisionID || snap.actorID != req.ActorID {
		return nil, false
	}
	delete(r.resumes, execID)
	for i, eid := range r.resumeOrder {
		if eid == execID {
			r.resumeOrder = append(r.resumeOrder[:i], r.resumeOrder[i+1:]...)
			break
		}
	}
	return snap, true
}

// restoreWorking rebuilds scratch state from a snapshot: fingerprints,
// entries, and observations in chronological order (AddObservation prepends,
// so replaying oldest-first restores the newest-first invariant).
func restoreWorking(snap *runSnapshot) *WorkingMemory {
	working := NewWorkingMemory()
	working.Bind(snap.identity.description)
	working.SetDeadline(snap.deadline)
	for fp := range snap.effects {
		_, _ = working.NoteEffect(fp)
	}
	for k, v := range snap.entries {
		_ = working.Put(k, v)
	}
	for _, o := range snap.observations {
		working.AddObservation(o)
	}
	return working
}

// copyPlan deep-copies the step list; scalars are values. Plans are treated
// as immutable after validation, but the copy removes any aliasing between
// the paused run and its resume.
func copyPlan(p *Plan) *Plan {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Steps = make([]Step, len(p.Steps))
	for i, s := range p.Steps {
		cp.Steps[i] = s
		cp.Steps[i].Dependencies = append([]string(nil), s.Dependencies...)
		cp.Steps[i].RequiredCaps = append([]string(nil), s.RequiredCaps...)
		cp.Steps[i].AllowedTools = append([]string(nil), s.AllowedTools...)
		cp.Steps[i].SuccessCriteria = append([]string(nil), s.SuccessCriteria...)
	}
	return &cp
}

// copyLedger rebuilds the ledger over a plan copy and restores every record.
// Statuses restore verbatim: completed steps stay completed with their
// original evidence and version (immutability survives the wait).
func copyLedger(plan *Plan, l *planLedger) *planLedger {
	if plan == nil || l == nil {
		return nil
	}
	nl := newPlanLedger(plan)
	for id, rec := range l.records {
		if dst, ok := nl.records[id]; ok {
			*dst = *rec
		}
	}
	return nl
}

// copyObservations duplicates observations including their result maps, so
// the paused run and its resume share nothing mutable.
func copyObservations(in []Observation) []Observation {
	out := make([]Observation, len(in))
	for i, o := range in {
		out[i] = o
		if o.Result != nil {
			out[i].Result = make(map[string]string, len(o.Result))
			for k, v := range o.Result {
				out[i].Result[k] = v
			}
		}
	}
	return out
}

// copyResearchEvidence removes aliasing between a paused run and its resume.
func copyResearchEvidence(in *execjournal.ResearchEvidence) *execjournal.ResearchEvidence {
	if in == nil {
		return nil
	}
	out := *in
	// Sources is a required JSON array. Copying into a nil slice would turn a
	// present empty capture into null after an approval resume.
	out.Sources = append([]execjournal.ResearchSource{}, in.Sources...)
	if in.Plan != nil {
		plan := *in.Plan
		out.Plan = &plan
	}
	out.Steps = append([]execjournal.ResearchStepEvidence(nil), in.Steps...)
	for i := range out.Steps {
		out.Steps[i].Dependencies = append([]string(nil), in.Steps[i].Dependencies...)
	}
	out.Operations = append([]execjournal.ResearchOperationEvidence(nil), in.Operations...)
	for i := range out.Operations {
		// SourceIDs is a required JSON array even for a complete search that
		// returned zero sources; null would be indistinguishable from a
		// malformed association on the read side.
		out.Operations[i].SourceIDs = append([]string{}, in.Operations[i].SourceIDs...)
	}
	return &out
}

// obsSummary renders one line per observation for failure messages. It is
// metadata only (ids, types, statuses), never content.
func obsSummary(obs []Observation) string {
	parts := make([]string, 0, len(obs))
	for _, o := range obs {
		parts = append(parts, fmt.Sprintf("%s:%s:%s", o.ObservationID, o.ActionType, o.Status))
	}
	return "[" + joinStrings(parts, ", ") + "]"
}
