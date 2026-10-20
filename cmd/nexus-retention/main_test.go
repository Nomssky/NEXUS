package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

func seedOldTerminal(t *testing.T, dir, id string) string {
	t.Helper()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour).UTC()
	rec := map[string]any{
		"version": 1, "id": id, "business_id": "biz-1", "state": "completed",
		"created_at": old.Format(time.RFC3339Nano), "updated_at": old.Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Put(&store.Record{ID: "nexus.execution:" + id, Type: "execution",
		Status: store.RecordStatusActive, BusinessID: "biz-1", Data: data}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "execution", "nexus.execution:"+id+".json")
}

// plan is a read-only dry-run; apply requires an explicit age and removes only
// then.
func TestToolPlanIsReadOnlyAndApplyRemoves(t *testing.T) {
	dir := t.TempDir()
	path := seedOldTerminal(t, dir, "exec-old")

	if code := run([]string{"plan", "-data-dir", dir, "-min-age", "24h"}); code != 0 {
		t.Fatalf("plan exit code = %d, want 0", code)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("plan must not remove anything")
	}

	if code := run([]string{"apply", "-data-dir", dir}); code != 2 {
		t.Fatalf("apply without -min-age must exit 2, got %d", code)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("a refused apply must not remove anything")
	}

	if code := run([]string{"apply", "-data-dir", dir, "-min-age", "24h"}); code != 0 {
		t.Fatalf("apply exit code = %d, want 0", code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("apply must remove the eligible record")
	}
}

func TestToolRejectsUnknownModeAndMissingDataDir(t *testing.T) {
	if code := run([]string{"frobnicate"}); code != 2 {
		t.Fatalf("unknown mode exit = %d, want 2", code)
	}
	if code := run([]string{"plan"}); code != 2 {
		t.Fatalf("missing -data-dir exit = %d, want 2", code)
	}
}

// ---------- operational accounting (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS §7) ----------

// capture runs the tool with stdout/stderr redirected and returns both streams
// plus the exit code.
func capture(t *testing.T, args ...string) (stdout, stderrOut string, code int) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW

	done := make(chan [2]string, 1)
	go func() {
		o, _ := readAll(outR)
		e, _ := readAll(errR)
		done <- [2]string{o, e}
	}()

	code = run(args)

	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = origOut, origErr
	pair := <-done
	return pair[0], pair[1], code
}

func readAll(f *os.File) (string, error) {
	b, err := io.ReadAll(f)
	return string(b), err
}

func countOnDiskRecords(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	for _, sub := range []string{"execution", "invocation"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		n += len(entries)
	}
	return n
}

// Obligation 17: the dry-run reports correct counts and leaves the on-disk state
// completely unchanged.
func TestPlanReportsAccountingAndChangesNothing(t *testing.T) {
	dir := t.TempDir()
	oldPath := seedOldTerminal(t, dir, "exec-old")
	youngPath := seedTerminalAt(t, dir, "exec-young", time.Now().UTC())

	before := treeSnapshot(t, dir)

	stdout, _, code := capture(t, "plan", "-data-dir", dir, "-min-age", "24h")
	if code != 0 {
		t.Fatalf("plan exit = %d, want 0 (stderr follows)", code)
	}

	// Everything the operator needs to account for the run must be present.
	for _, want := range []string{
		"plan (dry-run)",                        // dry-run marker
		"parameters: min-age=24h0m0s batch=256", // the parameters actually used
		"examined: executions=2",                // records examined
		"eligible: executions=1",                // one old terminal record
		"rejected:",                             // rejection accounting
		"too-young=1",                           // the young record is rejected by reason
		"physically deleted: 0 records",         // nothing was deleted
		"dry-run",                               // dry-run is stated explicitly
		"outcome: ok",
		"duration:",
		"bytes reclaimed: not reported", // byte claims are explicitly omitted
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("plan output missing %q:\n%s", want, stdout)
		}
	}

	// The on-disk state is byte-for-byte unchanged.
	after := treeSnapshot(t, dir)
	if len(before) != len(after) {
		t.Fatalf("dry-run changed the file count: %d -> %d", len(before), len(after))
	}
	for name, content := range before {
		if after[name] != content {
			t.Fatalf("dry-run mutated %s", name)
		}
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("dry-run must not remove the eligible record: %v", err)
	}
	if _, err := os.Stat(youngPath); err != nil {
		t.Fatalf("dry-run must not remove the young record: %v", err)
	}
}

// Obligation 18: apply reports deletions only when the storage operation
// established them, and never claims byte reclamation.
func TestApplyReportsAccountingForWhatItDeleted(t *testing.T) {
	dir := t.TempDir()
	oldPath := seedOldTerminal(t, dir, "exec-old")
	youngPath := seedTerminalAt(t, dir, "exec-young", time.Now().UTC())

	stdout, _, code := capture(t, "apply", "-data-dir", dir, "-min-age", "24h")
	if code != 0 {
		t.Fatalf("apply exit = %d, want 0", code)
	}
	for _, want := range []string{
		"apply (applied)",
		"parameters: min-age=24h0m0s batch=256",
		"examined: executions=2",
		"eligible: executions=1",
		"physically deleted: 1 records",
		"one atomic batch",
		"outcome: ok",
		"duration:",
		"bytes reclaimed: not reported",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("apply output missing %q:\n%s", want, stdout)
		}
	}

	// The claim must match reality exactly.
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("apply must physically remove the eligible record (stat err=%v)", err)
	}
	if _, err := os.Stat(youngPath); err != nil {
		t.Fatalf("apply must retain the too-young record: %v", err)
	}
	if got := countOnDiskRecords(t, dir); got != 1 {
		t.Fatalf("expected exactly the young record to survive, found %d", got)
	}
}

// A refused apply prints no deletion count at all.
func TestRefusedApplyReportsNoDeletion(t *testing.T) {
	dir := t.TempDir()
	seedOldTerminal(t, dir, "exec-old")

	stdout, stderrOut, code := capture(t, "apply", "-data-dir", dir) // no -min-age
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if strings.Contains(stdout, "physically deleted") {
		t.Fatalf("a refused run must not print a deletion report:\n%s", stdout)
	}
	_ = stderrOut
}

// A failed run must never imply that a deletion succeeded.
func TestFailedRunNeverClaimsADeletion(t *testing.T) {
	dir := t.TempDir()
	// A healthy eligible record, seeded first so the store can be opened...
	seedOldTerminal(t, dir, "exec-eligible")
	healthy := treeSnapshot(t, dir)

	// ...and then a corrupt record, which must make the run fail closed.
	if err := os.MkdirAll(filepath.Join(dir, "execution"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "execution", "bogus.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderrOut, code := capture(t, "apply", "-data-dir", dir, "-min-age", "24h")
	if code == 0 {
		t.Fatalf("a corrupt record must fail the run closed, got exit 0:\n%s", stdout)
	}
	combined := stdout + stderrOut
	if strings.Contains(combined, "physically deleted: 1") ||
		strings.Contains(combined, "physically deleted: 2") {
		t.Fatalf("a failed run must not claim deletions:\n%s", combined)
	}
	// The healthy record must still be there: a failed run deletes nothing.
	for name, content := range healthy {
		if name == "execution/bogus.json" {
			continue
		}
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("a failed run must not remove %s: %v", name, err)
		}
		if string(got) != content {
			t.Fatalf("a failed run mutated %s", name)
		}
	}
}

// The operator output stays bounded and never discloses record contents, ids,
// paths or secrets.
func TestOperatorOutputIsBoundedAndDisclosureFree(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 25; i++ {
		seedOldTerminal(t, dir, "exec-secret-id-"+itoa(i))
	}
	stdout, _, code := capture(t, "plan", "-data-dir", dir, "-min-age", "24h")
	if code != 0 {
		t.Fatalf("plan exit = %d, want 0", code)
	}
	for _, forbidden := range []string{"exec-secret-id", dir, ".json", "nexus.execution:"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("operator output leaked %q:\n%s", forbidden, stdout)
		}
	}
	// Bounded: the output is a fixed number of lines regardless of record count.
	lines := strings.Count(strings.TrimSpace(stdout), "\n") + 1
	if lines > 20 {
		t.Fatalf("operator output is not bounded: %d lines\n%s", lines, stdout)
	}
}

// seedTerminalAt seeds a terminal execution record last transitioned at ts.
func seedTerminalAt(t *testing.T, dir, id string, ts time.Time) string {
	t.Helper()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := map[string]any{
		"version": 1, "id": id, "business_id": "biz-1", "state": "completed",
		"created_at": ts.Format(time.RFC3339Nano), "updated_at": ts.Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Put(&store.Record{ID: "nexus.execution:" + id, Type: "execution",
		Status: store.RecordStatusActive, BusinessID: "biz-1", Data: data}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "execution", "nexus.execution:"+id+".json")
}

// treeSnapshot captures the relative path and content of every file in dir.
func treeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func itoa(i int) string {
	return strconv.Itoa(i)
}
