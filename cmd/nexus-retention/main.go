// Command nexus-retention is the operator tool for durable execution-journal
// retention (contracts/EXECUTION_JOURNAL_RETENTION_CONTRACTS.md).
//
// Usage:
//
//	nexus-retention plan  -data-dir <dir> [-min-age <dur>] [-batch N]
//	nexus-retention apply -data-dir <dir>  -min-age <dur>  [-batch N]
//
// Like nexus-backup it is a narrow operator utility, not a command framework,
// and it runs OFFLINE: the store must be quiesced (no other process using the
// data directory). `plan` is a read-only dry-run that reports the decision;
// `apply` is destructive and requires an explicit -min-age.
//
// The report body carries counts and status only: never record contents, ids,
// scope or credentials. A FAILURE additionally echoes the underlying store
// diagnostic, which may name a record id or a file path — that is intentional
// for an offline tool whose operator owns the data directory, and it is not a
// network surface. No secrets are ever read or printed.
//
// Every run reports its own accounting (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md
// §6): the run timestamp, the age/batch parameters the decision actually used,
// records examined, eligible and rejected by reason, physically deleted records,
// the operation outcome and its duration, and whether the output is a dry-run or
// an applied deletion. Byte reclamation is deliberately NOT reported: the store
// exposes no per-record size, so any byte figure would be an estimate and this
// tool reports record counts only. A failure never prints a deletion count.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
	"github.com/Nomssky/NEXUS/internal/foundation/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "plan":
		return cmdRun(args[1:], false)
	case "apply":
		return cmdRun(args[1:], true)
	case "-h", "--help", "help":
		usage()
		return 0
	case "-version", "--version", "version":
		fmt.Printf("nexus-retention %s (%s)\n", version.Version, version.Commit)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "nexus-retention: unknown mode %q\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `nexus-retention — NEXUS execution-journal retention

Modes:
  plan  -data-dir <dir> [-min-age <dur>] [-batch N]   dry-run: report, mutate nothing
  apply -data-dir <dir>  -min-age <dur>  [-batch N]   remove eligible records (destructive)

Flags:
  -min-age  minimum age of a terminal record to be eligible (e.g. 168h); the
            age basis is the record's last durable transition. Required for
            apply. For plan it defaults to 168h (7 days).
  -batch    maximum records removed by one run (default 256, cap 4096).

Notes:
  * Retention is opt-in; it never runs at startup, on backup or on restore.
  * Only terminal executions whose invocations are all known terminal outcomes
    and that are at least -min-age old are removed. Nonterminal, unknown and
    ambiguous records are always retained.
  * Stop the application before running (quiescence); this tool is not safe
    against a live writer.
`)
}

func cmdRun(args []string, apply bool) (code int) {
	mode := "plan"
	if apply {
		mode = "apply"
	}
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "durable store data directory (quiesced)")
	minAge := fs.Duration("min-age", 0, "minimum terminal-record age (e.g. 168h)")
	batch := fs.Int("batch", 256, "maximum records removed by one run")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dataDir == "" {
		fmt.Fprintf(os.Stderr, "nexus-retention %s: -data-dir is required\n", mode)
		return 2
	}
	age := *minAge
	if apply {
		if age <= 0 {
			fmt.Fprintf(os.Stderr, "nexus-retention apply: -min-age is required and must be positive\n")
			return 2
		}
	} else if age <= 0 {
		age = execjournal.DefaultRetentionMinAge
	}

	st, err := store.NewFileStore(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus-retention %s: open store: %v\n", mode, err)
		return 1
	}
	journal := execjournal.New(st, nil)
	policy := execjournal.RetentionPolicy{Enabled: true, MinAge: age, MaxBatch: *batch}

	// Operational accounting (v15): the run is timestamped and timed so an
	// operator can correlate it with the durable evidence it reports. The clock
	// is read once, here, and is never a metric claim.
	//
	// The store is closed before the report is printed so a failure to release
	// ownership is reflected in the printed outcome and the exit code. Opening
	// the store (creating the lock marker, tightening dirs, replaying a stale
	// journal) touches the filesystem, so "plan" means "no records are deleted"
	// rather than "no filesystem state is touched at all".
	runAt := time.Now().UTC()
	start := time.Now()

	var (
		rep     execjournal.RetentionReport
		outcome = "ok"
		deleted bool
	)

	if !apply {
		rep, err = journal.PlanRetention(policy)
		if err != nil {
			_ = st.Close()
			fmt.Fprintf(os.Stderr, "nexus-retention plan: %v\n", err)
			fmt.Fprintf(os.Stderr, "  outcome: FAILED — nothing was changed (plan deletes no records)\n")
			fmt.Fprintf(os.Stderr, "  duration: %s\n", time.Since(start).Round(time.Millisecond))
			return 1
		}
		deleted = false
	} else {
		fmt.Printf("nexus-retention apply: removing terminal records older than %s (batch ≤ %d) — DESTRUCTIVE\n", age, *batch)
		rep, err = journal.ApplyRetention(policy)
		if err != nil {
			_ = st.Close()
			fmt.Fprintf(os.Stderr, "nexus-retention apply: %v\n", err)
			// A failed batch is never reported as a deletion. DeleteBatchHard
			// either rolls the batch back inline or leaves the batch journal for
			// the next store open, which replays it as a rollback — so a failure
			// means the removal is NOT durably applied.
			fmt.Fprintf(os.Stderr, "  outcome: FAILED — no deletion is durably applied; an interrupted batch is rolled back when the store is next opened\n")
			fmt.Fprintf(os.Stderr, "  duration: %s\n", time.Since(start).Round(time.Millisecond))
			return 1
		}
		deleted = true
	}

	// Release ownership before reporting. If release fails, the outcome is
	// "failed" and the exit code is non-zero even though the durable work
	// itself succeeded.
	if cerr := st.Close(); cerr != nil {
		outcome = "failed"
		code = 1
		fmt.Fprintf(os.Stderr, "nexus-retention %s: release store ownership: %v\n", mode, cerr)
	}

	printReport(reportArgs{
		mode:   mode,
		kind:   map[bool]string{true: "applied", false: "dry-run"}[deleted],
		policy: policy,
		rep:    rep, at: runAt, dur: time.Since(start), outcome: outcome,
		deleted: deleted,
	})
	return code
}

// reportArgs carries everything the operator output needs. It exists so the
// printed accounting always names the exact parameters the decision used.
type reportArgs struct {
	mode    string
	kind    string // "dry-run" or "applied"
	policy  execjournal.RetentionPolicy
	rep     execjournal.RetentionReport
	at      time.Time
	dur     time.Duration
	outcome string
	deleted bool
}

func printReport(a reportArgs) {
	rep := a.rep
	fmt.Printf("retention %s (%s) at %s\n", a.mode, a.kind, a.at.Format(time.RFC3339))
	fmt.Printf("  parameters: min-age=%s batch=%d\n", a.policy.MinAge, a.policy.MaxBatch)
	fmt.Printf("  examined: executions=%d invocations=%d\n", rep.ScannedExecutions, rep.ScannedInvocations)
	fmt.Printf("  eligible: executions=%d invocations=%d\n", rep.EligibleExecutions, rep.EligibleInvocations)
	fmt.Printf("  rejected: nonterminal=%d unknown=%d too-young=%d ambiguous-invocation=%d\n",
		rep.RetainedNonterminal, rep.RetainedUnknown, rep.RetainedYoung, rep.RetainedInvocation)
	if a.deleted {
		// Only reported when the storage operation returned success.
		fmt.Printf("  physically deleted: %d records (one atomic batch)\n", rep.RemovedRecords)
	} else {
		fmt.Printf("  physically deleted: 0 records (dry-run — nothing was changed)\n")
	}
	fmt.Printf("  bytes reclaimed: not reported (the store exposes no per-record size; record counts only)\n")
	fmt.Printf("  outcome: %s\n", a.outcome)
	fmt.Printf("  duration: %s\n", a.dur.Round(time.Millisecond))
}
