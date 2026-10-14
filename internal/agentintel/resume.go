package agentintel

import (
	"fmt"
	"time"

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

// runSnapshot is the authoritative scratch state of one paused execution:
// everything the loop needs to continue instead of restarting.
type runSnapshot struct {
	// identity pins the snapshot to one execution of one objective.
	objective  string
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
}

// snapshotRun stores the paused run's state for its resume. Only the
// pending_approval path calls this, right before returning.
func (r *Runtime) snapshotRun(rn *run, obj Objective, plan *Plan, ledger *planLedger,
	observations []Observation, usage BudgetUsage, working *WorkingMemory, deadline time.Time) {
	snap := &runSnapshot{
		objective:    obj.Description,
		businessID:   rn.businessID,
		divisionID:   rn.divisionID,
		actorID:      rn.req.ActorID,
		plan:         copyPlan(plan),
		observations: copyObservations(observations),
		usage:        usage,
		effects:      working.Effects(),
		entries:      map[string]string{},
		deadline:     deadline,
		depth:        rn.depth,
		planVersion:  rn.planVersion,
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

// takeResume loads and consumes the snapshot for an execution id, verifying
// it belongs to this exact request. A mismatch (or no snapshot) means a
// fresh start: the stale entry is dropped, never applied.
func (r *Runtime) takeResume(execID string, obj Objective, req *executor.WorkRequest) (*runSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := r.resumes[execID]
	if snap == nil {
		return nil, false
	}
	delete(r.resumes, execID)
	for i, id := range r.resumeOrder {
		if id == execID {
			r.resumeOrder = append(r.resumeOrder[:i], r.resumeOrder[i+1:]...)
			break
		}
	}
	if snap.objective != obj.Description || snap.businessID != req.BusinessID ||
		snap.divisionID != req.DivisionID || snap.actorID != req.ActorID {
		return nil, false
	}
	return snap, true
}

// restoreWorking rebuilds scratch state from a snapshot: fingerprints,
// entries, and observations in chronological order (AddObservation prepends,
// so replaying oldest-first restores the newest-first invariant).
func restoreWorking(snap *runSnapshot) *WorkingMemory {
	working := NewWorkingMemory()
	working.Bind(snap.objective)
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

// obsSummary renders one line per observation for failure messages. It is
// metadata only (ids, types, statuses), never content.
func obsSummary(obs []Observation) string {
	parts := make([]string, 0, len(obs))
	for _, o := range obs {
		parts = append(parts, fmt.Sprintf("%s:%s:%s", o.ObservationID, o.ActionType, o.Status))
	}
	return "[" + joinStrings(parts, ", ") + "]"
}
