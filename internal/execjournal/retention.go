package execjournal

import (
	"fmt"
	"sort"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// Execution-journal retention (contracts/EXECUTION_JOURNAL_RETENTION_CONTRACTS.md).
//
// Retention physically removes a verified subset of durable records to reclaim
// disk. It never removes recovery evidence that could still be needed: only a
// terminal execution that is old enough and whose every invocation is a durable,
// known terminal outcome is eligible, and only when a policy explicitly enables
// it. A plan/dry-run reports the decision without mutating anything. The removal
// itself is one atomic Store.DeleteBatchHard.

// RetentionPolicy is the explicit, opt-in retention configuration. The zero
// value disables retention: a plan or apply with it removes nothing.
type RetentionPolicy struct {
	// Enabled is the master switch. False makes every retention call refuse
	// (an error), so a misconfiguration is never a silent no-op.
	Enabled bool
	// MinAge is the minimum age a terminal record must have to be eligible. The
	// age basis is the record's UpdatedAt (its last durable transition); the
	// boundary is inclusive: age >= MinAge is eligible.
	MinAge time.Duration
	// MaxBatch bounds how many records (executions plus their invocations) one
	// run removes. Below 1 or above MaxRetentionBatch it is clamped to
	// MaxRetentionBatch. The bound keeps the store lock and the batch journal
	// small.
	MaxBatch int
}

// DefaultRetentionMinAge is the conservative horizon tooling uses when none is
// given. It is never applied automatically.
const DefaultRetentionMinAge = 7 * 24 * time.Hour

// MaxRetentionBatch bounds a single retention run's removals (records).
const MaxRetentionBatch = 4096

// RetentionReport separates what was examined from what was removed and why
// records were retained. It carries counts only — never record content, ids,
// scope or paths — so it is safe to log.
type RetentionReport struct {
	DryRun              bool
	ScannedExecutions   int
	ScannedInvocations  int
	EligibleExecutions  int
	EligibleInvocations int
	RemovedRecords      int
	RetainedNonterminal int
	RetainedUnknown     int
	RetainedYoung       int
	RetainedInvocation  int
}

// invRef is the store id and durable status of one invocation, indexed by its
// owning execution.
type invRef struct {
	storeID string
	status  string
}

// invocationTerminalKnown reports whether an invocation status is a durable,
// known terminal outcome — the only statuses that permit reclaiming the
// execution (and the invocation) it belongs to. `dispatched` and `unknown` are
// ambiguous and forbid removal.
func invocationTerminalKnown(status string) bool {
	return status == InvCompleted || status == InvFailed
}

// planRetention computes the retention decision without mutating anything. It
// fails closed: on ANY corrupt or unsupported record it returns an error and
// removes nothing, never skipping a bad record. The removal set is ordered
// deterministically (oldest execution first, tie-broken by execution id) and
// bounded by the policy.
func (j *Journal) planRetention(p RetentionPolicy) (RetentionReport, []string, error) {
	var rep RetentionReport
	rep.DryRun = true
	if !p.Enabled {
		return rep, nil, fmt.Errorf("execjournal: retention is disabled")
	}
	if p.MinAge <= 0 {
		return rep, nil, fmt.Errorf("execjournal: retention requires a positive min age")
	}
	minAge := p.MinAge
	maxBatch := p.MaxBatch
	if maxBatch < 1 || maxBatch > MaxRetentionBatch {
		maxBatch = MaxRetentionBatch
	}

	execRecs, err := j.st.List(store.Filter{Type: recordTypeExecution})
	if err != nil {
		return rep, nil, fmt.Errorf("execjournal: retention list executions: %w", err)
	}
	invRecs, err := j.st.List(store.Filter{Type: recordTypeInvocation})
	if err != nil {
		return rep, nil, fmt.Errorf("execjournal: retention list invocations: %w", err)
	}

	type candidate struct {
		storeID string
		rec     ExecutionRecord
	}
	cands := make([]candidate, 0, len(execRecs))
	for _, r := range execRecs {
		er, err := unmarshalExecution(r)
		if err != nil {
			return rep, nil, err // fail closed: remove nothing
		}
		cands = append(cands, candidate{storeID: r.ID, rec: er})
	}

	// Group every invocation by its owning execution, validating each one
	// (fail closed). A record we cannot validate forbids the whole run.
	invsByExec := make(map[string][]invRef, len(execRecs))
	for _, r := range invRecs {
		iv, err := unmarshalInvocation(r)
		if err != nil {
			return rep, nil, err // fail closed
		}
		invsByExec[iv.ExecutionID] = append(invsByExec[iv.ExecutionID], invRef{storeID: r.ID, status: iv.Status})
	}

	// Deterministic order: oldest UpdatedAt first, tie-broken by execution id.
	sort.Slice(cands, func(a, b int) bool {
		if !cands[a].rec.UpdatedAt.Equal(cands[b].rec.UpdatedAt) {
			return cands[a].rec.UpdatedAt.Before(cands[b].rec.UpdatedAt)
		}
		return cands[a].rec.ID < cands[b].rec.ID
	})

	rep.ScannedExecutions = len(cands)
	rep.ScannedInvocations = len(invRecs)
	now := j.now()

	var ids []string
	for _, c := range cands {
		if !c.rec.State.Terminal() {
			if c.rec.State == StateUnknown {
				rep.RetainedUnknown++
			} else {
				rep.RetainedNonterminal++
			}
			continue
		}
		if now.Sub(c.rec.UpdatedAt) < minAge {
			rep.RetainedYoung++
			continue
		}
		invs := invsByExec[c.rec.ID]
		ambiguous := false
		for _, iv := range invs {
			if !invocationTerminalKnown(iv.status) {
				ambiguous = true
				break
			}
		}
		if ambiguous {
			rep.RetainedInvocation++
			continue
		}
		if len(ids)+1+len(invs) > maxBatch {
			break // bounded batch, deterministic (oldest first)
		}
		rep.EligibleExecutions++
		rep.EligibleInvocations += len(invs)
		ids = append(ids, c.storeID)
		for _, iv := range invs {
			ids = append(ids, iv.storeID)
		}
	}
	return rep, ids, nil
}

// PlanRetention reports the retention decision without removing anything. It is
// the dry-run: it never changes record bytes, filenames, journal metadata or
// disk usage. A disabled policy or a corrupt record removes nothing and returns
// an error.
func (j *Journal) PlanRetention(p RetentionPolicy) (RetentionReport, error) {
	if j == nil {
		return RetentionReport{DryRun: true}, nil
	}
	rep, _, err := j.planRetention(p)
	rep.DryRun = true
	return rep, err
}

// ApplyRetention plans and then physically removes the eligible records in ONE
// atomic batch (Store.DeleteBatchHard). A disabled policy, an invalid age, or a
// corrupt/unsupported record removes nothing. It is idempotent: a repeated run
// finds the already-removed records gone and removes nothing more.
func (j *Journal) ApplyRetention(p RetentionPolicy) (RetentionReport, error) {
	if j == nil {
		return RetentionReport{}, nil
	}
	rep, ids, err := j.planRetention(p)
	if err != nil {
		return rep, err
	}
	rep.DryRun = false
	if len(ids) == 0 {
		return rep, nil
	}
	if err := j.st.DeleteBatchHard(ids); err != nil {
		return rep, fmt.Errorf("execjournal: retention apply: %w", err)
	}
	rep.RemovedRecords = len(ids)
	return rep, nil
}
