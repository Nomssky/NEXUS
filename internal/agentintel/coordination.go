package agentintel

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
)

// Coordination execution (contracts/AGENT_COORDINATION_CONTRACTS.md): one
// typed coordinate action becomes one concurrent wave of independent
// children, settled deterministically in member declaration order. The loop
// stays sequential — parallelism lives exactly one level down, inside the
// wave — so the step ledger, evidence rule, replan and duplicate machinery
// all keep working unchanged.

// coordinateMemberFingerprints returns one single-compatible fingerprint per
// member, in declaration order. A member fingerprint equals the fingerprint
// the same delegation would carry as a single action, so cross single↔group
// duplication is caught in both directions.
func coordinateMemberFingerprints(a Action) []string {
	out := make([]string, 0, len(a.Children))
	for _, m := range a.Children {
		out = append(out, delegateEffectFingerprint(m.AgentID, m.Objective, m.DelegateTools))
	}
	return out
}

// coordinateFullyExecuted reports whether every member fingerprint is already
// recorded: the identical group settled before, so nothing new would run.
func coordinateFullyExecuted(fps []string, has func(string) bool) bool {
	if len(fps) == 0 {
		return false
	}
	for _, fp := range fps {
		if !has(fp) {
			return false
		}
	}
	return true
}

// performCoordinate executes one validated coordinate action: prune
// already-completed members, intersect tools per member, admit every
// remaining member (fail closed, create none), run the wave, and settle it
// into one deterministic observation. Settlement is always non-terminal:
// the ledger step completes on the settlement evidence and the loop
// continues, so the parent can replan from partial results. Group-level
// faults (admission refusal, budget, cancellation, deadline) are terminal.
func (r *Runtime) performCoordinate(rn *run, ctx context.Context, a Action, agentID string, working *WorkingMemory, usage BudgetUsage, budget Budget, planVersion int) (*Observation, BudgetUsage, State, string) {
	obs := &Observation{ObservationID: newID("obs"), Source: "delegate", ActionType: ActionCoordinate,
		Status: "ok", Timestamp: r.now()}

	// Prune members whose effect already completed: their original evidence
	// stands, and only the remainder runs. Recovery never duplicates.
	type pending struct {
		index  int
		member CoordinationChild
		fp     string
		tools  []agentexec.ToolCallSpec
	}
	fps := coordinateMemberFingerprints(a)
	var todo []pending
	for i, m := range a.Children {
		fp := fps[i]
		if working.HasEffect(fp) {
			continue
		}
		tools, terr := r.resolveDelegateToolsFor(rn, agentID, m.AgentID, m.DelegateTools)
		if terr != nil {
			obs.Status, obs.Text = "failed", terr.Error()
			return obs, usage, StateFailed, "coordination failed: " + terr.Error()
		}
		todo = append(todo, pending{index: i, member: m, fp: fp, tools: tools})
	}
	if len(todo) == 0 {
		// The pre-perform duplicate check normally catches this; the guard
		// here closes the race between the check and the effect.
		msg := "action rejected: duplicate of an already-completed effect (not re-executed)"
		obs.Status, obs.Text = "failed", msg
		return obs, usage, StateFailed, msg
	}

	// Admit every remaining member BEFORE creating any: one refusal fails the
	// whole group closed with zero children started. A group is never a
	// blanket authorization (contract §5).
	type admitted struct {
		pending    pending
		proposalID string
	}
	var ready []admitted
	for _, p := range todo {
		admObs, st, msg, done, proposalID := r.admitNonTool(rn, agentID, control.ActionDelegate,
			control.ResourceTypeAgent, control.DelegateTarget(p.member.AgentID))
		if done {
			// Name the refusing member: the parent sees exactly which child
			// stopped the group and why. The admission observation carries
			// the approval/escalation references when present, so resume
			// and attention surfaces keep working.
			obs.Status = memberRefusedStatus(st)
			obs.Text = fmt.Sprintf("coordination refused: child %d (%s): %s", p.index, p.member.AgentID, msg)
			obs.Result = map[string]string{
				"group_size": fmt.Sprint(len(a.Children)), "refused_index": fmt.Sprint(p.index),
				"refused_agent": p.member.AgentID,
			}
			if admObs != nil && admObs.Result != nil {
				for k, v := range admObs.Result {
					obs.Result[k] = v
				}
			}
			if st == StatePendingApproval {
				// The standard resume path replays cleanly: nothing ran, so
				// re-admission consumes the approval and the group proceeds.
				return obs, usage, st, msg
			}
			if st == StateEscalated {
				return obs, usage, st, msg
			}
			return obs, usage, StateDenied, msg
		}
		ready = append(ready, admitted{pending: p, proposalID: proposalID})
	}

	usage.Delegations += len(ready)
	groupID := newID("coord")
	rn.emit(event.EventTypeCoordinationStarted, map[string]string{
		"group_id": groupID, "members": fmt.Sprint(len(ready)),
		"plan_version": fmt.Sprint(planVersion),
	})
	members := make([]agentexec.CoordinationMember, 0, len(ready))
	for _, ad := range ready {
		members = append(members, agentexec.CoordinationMember{
			Index: ad.pending.index,
			Spec: agentexec.DelegateSpec{
				ID: newID("child"), Intent: ad.pending.member.Objective,
				AgentID: ad.pending.member.AgentID, Tools: ad.pending.tools,
				ParentAgentID: agentID, DelegationActionID: ad.proposalID,
			},
		})
	}
	outcome := r.Exec.RunCoordinationGroup(ctx, rn.req, rn.ag, members, rn.depth)

	// Wave-level interruption is terminal; settlement never is.
	if outcome.Cancelled {
		for _, res := range outcome.Results {
			r.recordCoordinationMember(working, a, res)
		}
		obs.Status, obs.Text = "failed", fmt.Sprintf("coordination %s cancelled after %d of %d started",
			groupID, outcome.Started, len(members))
		return obs, usage, StateCancelled, "coordination cancelled"
	}
	if outcome.DeadlineExceeded {
		for _, res := range outcome.Results {
			r.recordCoordinationMember(working, a, res)
		}
		obs.Status, obs.Text = "failed", fmt.Sprintf("coordination %s exceeded its deadline after %d of %d started",
			groupID, outcome.Started, len(members))
		return obs, usage, StateDeadlineExceeded, "deadline exceeded"
	}

	// Settle deterministically in member declaration order — never completion
	// race order (contract §7). Completed members are fingerprinted; anything
	// else is recorded as what it is, never collapsed, never retried here.
	for _, res := range outcome.Results {
		r.recordCoordinationMember(working, a, res)
	}
	agg := aggregateCoordination(groupID, a, outcome)
	obs.Text = agg.text
	obs.Result = agg.fields
	obs.Outcome = agg.outcome
	obs.Attempts = outcome.Started
	obs.ReconciliationRequired = agg.reconciliation
	rn.emit(event.EventTypeCoordinationCompleted, map[string]string{
		"group_id": groupID, "members": fmt.Sprint(len(members)),
		"completed": fmt.Sprint(agg.completed), "failed": fmt.Sprint(agg.failed),
		"unknown":      fmt.Sprint(agg.unknown),
		"plan_version": fmt.Sprint(planVersion),
	})
	return obs, usage, "", ""
}

// memberRefusedStatus keeps the admission axis on group refusal: a denial is
// a denial, while approval/escalation travel under their own states only
// when the loop returns them directly (here they are reported, then mapped
// by the caller paths above).
func memberRefusedStatus(st State) string {
	switch st {
	case StatePendingApproval:
		return "pending_approval"
	case StateEscalated:
		return "escalated"
	default:
		return "denied"
	}
}

// recordCoordinationMember fingerprints a member effect that actually
// completed, so regroups prune it instead of re-running it. Anything else is
// left unrecorded: a failed member may be proposed again, a completed one
// may not.
func (r *Runtime) recordCoordinationMember(working *WorkingMemory, a Action, res agentexec.CoordinationChildResult) {
	if !res.Started || res.Outcome == nil || res.Outcome.Status != "completed" {
		return
	}
	if res.Index < 0 || res.Index >= len(a.Children) {
		return
	}
	m := a.Children[res.Index]
	fp := delegateEffectFingerprint(m.AgentID, m.Objective, m.DelegateTools)
	_, _ = working.NoteEffect(fp)
}

// coordinationAggregate is the deterministic settlement record.
type coordinationAggregate struct {
	text           string
	fields         map[string]string
	outcome        string
	completed      int
	failed         int
	unknown        int
	reconciliation bool
}

// aggregateCoordination renders the settlement in member declaration order.
// Counts per status class travel as fields; per-member identity, status,
// outcome and truncated output travel namespaced by index. Completion race
// order never appears.
func aggregateCoordination(groupID string, a Action, outcome agentexec.CoordinationOutcome) coordinationAggregate {
	agg := coordinationAggregate{fields: map[string]string{
		"group_id": groupID, "group_size": fmt.Sprint(len(outcome.Results)),
	}}
	var lines []string
	byStatus := map[string]int{}
	for _, res := range outcome.Results {
		status, result, attempts := memberSettlement(res)
		byStatus[status]++
		switch status {
		case "completed":
			agg.completed++
		case "unknown":
			agg.unknown++
			agg.reconciliation = true
		default:
			agg.failed++
		}
		key := fmt.Sprintf("child.%d", res.Index)
		agg.fields[key+".agent"] = res.AgentID
		agg.fields[key+".status"] = status
		if res.TaskID != "" {
			agg.fields[key+".task"] = res.TaskID
		}
		if result != "" {
			agg.fields[key+".result"] = truncate(result, 512)
		}
		_ = attempts
		lines = append(lines, fmt.Sprintf("[%d:%s %s]", res.Index, shortID(res.AgentID), status))
	}
	for status, n := range byStatus {
		agg.fields[status] = fmt.Sprint(n)
	}
	// Deterministic one-line summary first, then the per-member record.
	summary := fmt.Sprintf("group %s settled %d (%s)", groupID, len(outcome.Results), joinStatusCounts(byStatus))
	agg.text = summary + "\n" + strings.Join(lines, "\n")
	switch {
	case agg.unknown > 0:
		agg.outcome = "unknown"
	case agg.failed > 0:
		agg.outcome = "failed"
	default:
		agg.outcome = "completed"
	}
	return agg
}

// memberSettlement normalizes one member result to (status, result, attempts)
// without rewriting anything: unknown stays unknown, pending approval and
// escalation keep their names and references.
func memberSettlement(res agentexec.CoordinationChildResult) (string, string, int) {
	if !res.Started {
		return "cancelled", "member never started", 0
	}
	if res.Err != nil {
		return "failed", res.Err.Error(), 0
	}
	out := res.Outcome
	if out == nil {
		return "failed", "member returned no outcome", 0
	}
	switch out.Status {
	case "completed":
		return "completed", out.Output, 0
	case "unknown":
		return "unknown", out.Error, 0
	case "pending_approval":
		return "pending_approval", "approval_id=" + out.ApprovalID, 0
	case "escalated":
		return "escalated", "escalation_id=" + out.EscalationRef, 0
	case "denied":
		return "denied", out.Error, 0
	case "cancelled":
		return "cancelled", out.Error, 0
	case "timed_out":
		return "timed_out", out.Error, 0
	default:
		return "failed", out.Error, 0
	}
}

// shortID keeps aggregated lines readable without leaking internals.
func shortID(id string) string {
	if len(id) > 24 {
		return id[:24]
	}
	return id
}

// joinStatusCounts renders "2 completed, 1 failed" deterministically.

// joinStatusCounts renders "2 completed, 1 failed" deterministically.
func joinStatusCounts(byStatus map[string]int) string {
	keys := make([]string, 0, len(byStatus))
	for k := range byStatus {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", byStatus[k], k))
	}
	return strings.Join(parts, ", ")
}
