package guard_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TEST-M0-015: no locked contract or architecture files are modified.
//
// This guard asserts that the working tree does not modify any file under
// contracts/ or Core/ relative to the blueprint baseline commit. It is a
// safety net for the locked layers; M0 must not touch them.
func TestLockedLayersUnmodified(t *testing.T) {
	base := "12a3eb3" // planning/implementation-blueprint HEAD (blueprint base)

	out, err := exec.Command("git", "diff", "--name-only", base, "--", "contracts", "Core").Output()
	if err != nil {
		// Retry with the full commit and a merge-base fallback; if history is
		// unavailable (e.g. shallow clone), skip rather than fail.
		alt := exec.Command("git", "diff", "--name-only", "planning/implementation-blueprint", "--", "contracts", "Core")
		out, err = alt.Output()
		if err != nil {
			t.Skipf("cannot diff locked layers (no history): %v", err)
		}
	}
	changed := strings.TrimSpace(string(out))
	if changed != "" {
		t.Fatalf("locked contract/architecture files were modified:\n%s", changed)
	}
}
