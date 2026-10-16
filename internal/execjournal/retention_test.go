package execjournal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// Retention (contracts/EXECUTION_JOURNAL_RETENTION_CONTRACTS.md). Every case is
// deterministic: an injected clock drives age, on-disk state is asserted
// directly, and no test sleeps.

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func newRetentionJournal(t *testing.T) (*Journal, store.Store, *testClock, string) {
	t.Helper()
	dir := t.TempDir()
	clk := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	st, err := store.NewFileStoreWithClock(dir, clk.now)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return New(st, clk.now), st, clk, dir
}

func seedTerminalExec(t *testing.T, j *Journal, id string, state State) {
	t.Helper()
	if err := j.Accepted(id, "biz-1", "", "actor"); err != nil {
		t.Fatalf("accepted %s: %v", id, err)
	}
	if err := j.Terminal(id, state, ""); err != nil {
		t.Fatalf("terminal %s: %v", id, err)
	}
}

func seedRawExecution(t *testing.T, st store.Store, rec ExecutionRecord) {
	t.Helper()
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Put(&store.Record{ID: execKey(rec.ID), Type: recordTypeExecution,
		Status: store.RecordStatusActive, BusinessID: rec.BusinessID, Data: data}); err != nil {
		t.Fatalf("seed raw execution: %v", err)
	}
}

func execFile(dir, id string) string {
	return filepath.Join(dir, string(recordTypeExecution), execKey(id)+".json")
}

func invFile(dir string, key string) string {
	return filepath.Join(dir, string(recordTypeInvocation), key+".json")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Retention is disabled with the zero policy: it refuses and removes nothing.
func TestRetentionDisabledRemovesNothing(t *testing.T) {
	j, _, clk, dir := newRetentionJournal(t)
	seedTerminalExec(t, j, "exec-done", StateCompleted)
	clk.set(clk.now().Add(30 * 24 * time.Hour))

	if _, err := j.ApplyRetention(RetentionPolicy{}); err == nil {
		t.Fatal("a disabled policy must refuse, never silently no-op")
	}
	if !exists(execFile(dir, "exec-done")) {
		t.Fatal("a disabled policy must not remove a record")
	}
}

// A plan is a dry-run: it reports eligibility and changes nothing (bytes,
// filenames, journal, disk content).
func TestRetentionPlanIsReadOnly(t *testing.T) {
	j, _, clk, dir := newRetentionJournal(t)
	seedTerminalExec(t, j, "exec-old", StateCompleted)
	clk.set(clk.now().Add(30 * 24 * time.Hour))

	before, err := os.ReadFile(execFile(dir, "exec-old"))
	if err != nil {
		t.Fatal(err)
	}

	rep, err := j.PlanRetention(RetentionPolicy{Enabled: true, MinAge: time.Hour})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if rep.EligibleExecutions != 1 || rep.RemovedRecords != 0 || !rep.DryRun {
		t.Fatalf("unexpected plan report: %+v", rep)
	}

	after, err := os.ReadFile(execFile(dir, "exec-old"))
	if err != nil {
		t.Fatalf("plan must not remove the file: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("plan must not change record bytes")
	}
	if exists(filepath.Join(dir, ".batch-journal.json")) {
		t.Fatal("plan must not create a batch journal")
	}
}

// Only terminal records older than MinAge are removed, and only when enabled;
// the removal reclaims the on-disk file.
func TestRetentionRemovesEligibleTerminalRecords(t *testing.T) {
	j, _, clk, dir := newRetentionJournal(t)
	seedTerminalExec(t, j, "exec-done", StateCompleted)
	invID, err := j.RecordDispatchIntent("exec-done", "call-1", "http.request", "post", "mutation")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordOutcome(invID, InvCompleted, ""); err != nil {
		t.Fatal(err)
	}
	clk.set(clk.now().Add(30 * 24 * time.Hour))

	rep, err := j.ApplyRetention(RetentionPolicy{Enabled: true, MinAge: 24 * time.Hour})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rep.RemovedRecords != 2 || rep.EligibleExecutions != 1 || rep.EligibleInvocations != 1 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	if exists(execFile(dir, "exec-done")) {
		t.Fatal("the eligible execution file must be reclaimed from disk")
	}
	if exists(invFile(dir, invID)) {
		t.Fatal("the eligible invocation file must be reclaimed from disk")
	}
	if _, ok, _ := j.Get("exec-done"); ok {
		t.Fatal("the eligible record must be gone from the store")
	}
}

// Nonterminal, unknown, dispatched-invocation and too-young records are retained.
func TestRetentionRetainsEvidence(t *testing.T) {
	j, st, clk, _ := newRetentionJournal(t)
	t0 := clk.now()

	// Nonterminal (accepted).
	if err := j.Accepted("exec-open", "biz-1", "", "actor"); err != nil {
		t.Fatal(err)
	}
	// Unknown (as recovery would set it), old.
	seedRawExecution(t, st, ExecutionRecord{Version: SchemaVersion, ID: "exec-unknown", BusinessID: "biz-1",
		State: StateUnknown, CreatedAt: t0, UpdatedAt: t0})
	// Terminal with a dispatched invocation (ambiguous), old.
	seedTerminalExec(t, j, "exec-ambiguous", StateCompleted)
	if _, err := j.RecordDispatchIntent("exec-ambiguous", "call-1", "http.request", "post", "mutation"); err != nil {
		t.Fatal(err)
	}
	// Terminal and terminal-known, but too young.
	clk.set(t0.Add(20 * 24 * time.Hour))
	seedTerminalExec(t, j, "exec-young", StateCompleted)

	clk.set(t0.Add(30 * 24 * time.Hour))
	rep, err := j.ApplyRetention(RetentionPolicy{Enabled: true, MinAge: 25 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rep.RemovedRecords != 0 {
		t.Fatalf("nothing may be removed, got %+v", rep)
	}
	if rep.RetainedNonterminal != 1 || rep.RetainedUnknown != 1 || rep.RetainedInvocation != 1 || rep.RetainedYoung != 1 {
		t.Fatalf("unexpected retention detail: %+v", rep)
	}
	for _, id := range []string{"exec-open", "exec-unknown", "exec-ambiguous", "exec-young"} {
		if _, ok, _ := j.Get(id); !ok {
			t.Fatalf("%s must be retained", id)
		}
	}
}

// The age boundary is inclusive and deterministic.
func TestRetentionAgeBoundary(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	minAge := 10 * time.Hour

	for _, tc := range []struct {
		name       string
		now        time.Time
		wantRemove bool
	}{
		{"exactly at the boundary", base.Add(minAge), true},
		{"one nanosecond before the boundary", base.Add(minAge - time.Nanosecond), false},
		{"well before the boundary", base.Add(minAge / 2), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j, st, _, _ := newRetentionJournal(t)
			seedRawExecution(t, st, ExecutionRecord{Version: SchemaVersion, ID: "exec-b", BusinessID: "biz-1",
				State: StateCompleted, CreatedAt: base, UpdatedAt: base})
			// Point the journal clock at the case's now.
			j.now = func() time.Time { return tc.now }
			rep, err := j.ApplyRetention(RetentionPolicy{Enabled: true, MinAge: minAge})
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			removed := rep.RemovedRecords == 1
			if removed != tc.wantRemove {
				t.Fatalf("removed=%v want=%v (%+v)", removed, tc.wantRemove, rep)
			}
		})
	}
}

// A corrupt or unsupported record fails the whole run closed: nothing is removed.
func TestRetentionFailsClosedOnCorrupt(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{"unsupported version", `{"version":2,"id":"exec-bad","state":"completed"}`},
		{"invalid state", `{"version":1,"id":"exec-bad","state":"sideways"}`},
		{"not json", `{not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j, st, clk, dir := newRetentionJournal(t)
			// One eligible record that must survive because a sibling is corrupt.
			seedTerminalExec(t, j, "exec-good", StateCompleted)
			if err := st.Put(&store.Record{ID: execKey("exec-bad"), Type: recordTypeExecution,
				Status: store.RecordStatusActive, Data: []byte(tc.data)}); err != nil {
				t.Fatal(err)
			}
			clk.set(clk.now().Add(30 * 24 * time.Hour))

			if _, err := j.ApplyRetention(RetentionPolicy{Enabled: true, MinAge: time.Hour}); err == nil {
				t.Fatal("a corrupt record must fail the run closed")
			}
			if !exists(execFile(dir, "exec-good")) {
				t.Fatal("a fail-closed run must remove nothing")
			}
		})
	}
}

// Repeated retention is idempotent.
func TestRetentionIsIdempotent(t *testing.T) {
	j, _, clk, dir := newRetentionJournal(t)
	seedTerminalExec(t, j, "exec-a", StateCompleted)
	seedTerminalExec(t, j, "exec-b", StateFailed)
	clk.set(clk.now().Add(30 * 24 * time.Hour))

	p := RetentionPolicy{Enabled: true, MinAge: time.Hour}
	first, err := j.ApplyRetention(p)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	second, err := j.ApplyRetention(p)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if first.RemovedRecords != 2 || second.RemovedRecords != 0 {
		t.Fatalf("idempotency: first=%d second=%d", first.RemovedRecords, second.RemovedRecords)
	}
	if exists(execFile(dir, "exec-a")) || exists(execFile(dir, "exec-b")) {
		t.Fatal("eligible records must be gone")
	}
}

// A record written after the plan (a new execution) is never removed, even when
// the plan already decided to remove older records.
func TestRetentionDoesNotLoseNewWrites(t *testing.T) {
	j, _, clk, _ := newRetentionJournal(t)
	seedTerminalExec(t, j, "exec-old", StateCompleted)
	clk.set(clk.now().Add(30 * 24 * time.Hour))

	p := RetentionPolicy{Enabled: true, MinAge: time.Hour}
	if _, err := j.PlanRetention(p); err != nil {
		t.Fatalf("plan: %v", err)
	}
	// A write that lands between planning and applying.
	seedTerminalExec(t, j, "exec-new", StateCompleted)

	rep, err := j.ApplyRetention(p)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rep.RemovedRecords != 1 {
		t.Fatalf("only the old record may be removed, got %d", rep.RemovedRecords)
	}
	if _, ok, _ := j.Get("exec-new"); !ok {
		t.Fatal("a record written after the plan must never be removed")
	}

	// A concurrent writer racing the apply: the batch holds the store lock, so
	// no new record can be silently lost.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = j.Accepted("exec-race-"+string(rune('a'+i%26)), "biz-1", "", "actor")
		}
	}()
	_, _ = j.ApplyRetention(p)
	wg.Wait()
	for i := 0; i < 50; i++ {
		id := "exec-race-" + string(rune('a'+i%26))
		if _, ok, _ := j.Get(id); !ok {
			t.Fatalf("a concurrently written record %s was lost", id)
		}
	}
}
