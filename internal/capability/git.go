package capability

// Git capability (contracts/CAPABILITY_TOOL_CONTRACTS.md §11): local, read-only
// repository operations inside the same workspace sandbox as the filesystem
// tool.
//
// Boundary: the git binary is invoked with a FIXED argument vector — never a
// shell, never `git -c`, no hooks (`-c core.hooksPath=/dev/null`), a scrubbed
// environment, a timeout, and bounded output. Remote mutation (fetch/pull/push)
// is not implemented in v1. The repository path always comes from the sandbox
// resolution, never from model input outside the sandbox.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// GitPolicy is the runtime-controlled git boundary.
type GitPolicy struct {
	// Roots are the workspace roots inside which git may operate (business
	// scoped by the caller; the sandbox resolution decides the exact repo).
	Roots []string
	// MaxOutputBytes bounds any single command's output.
	MaxOutputBytes int
	Timeout        time.Duration
	// Binary is the git executable path; empty resolves `git` from PATH.
	Binary string
}

// DefaultGitPolicy bounds the shipped configuration.
func DefaultGitPolicy(roots []string) GitPolicy {
	return GitPolicy{Roots: roots, MaxOutputBytes: 64 * 1024, Timeout: 10 * time.Second}
}

// GitTool is the mediated git adapter.
type GitTool struct{ Policy GitPolicy }

// Operations implements tool.Adapter. Remote mutation is deliberately absent.
func (t *GitTool) Operations() []string {
	return []string{"diff", "log", "status"}
}

// Invoke implements tool.Adapter.
func (t *GitTool) Invoke(ctx context.Context, inv tool.Invocation) (tool.RawResult, error) {
	if err := ctx.Err(); err != nil {
		return tool.RawResult{}, err
	}
	repo := strings.TrimSpace(inv.Input["repo"])
	if repo == "" {
		repo = "."
	}
	resolved, _, err := resolveInRoots(repo, t.Policy.Roots)
	if err != nil {
		return tool.RawResult{}, err
	}
	args, err := gitArgs(inv.Operation, inv.Input)
	if err != nil {
		return tool.RawResult{}, err
	}
	bin := t.Policy.Binary
	if bin == "" {
		bin = "git"
	}
	timeout := t.Policy.Timeout
	if inv.Limits.MaxDuration > 0 && inv.Limits.MaxDuration < timeout {
		timeout = inv.Limits.MaxDuration
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Fixed argv: -c core.hooksPath=/dev/null disables hooks; --no-pager and
	// -c core.pager=cat keep output deterministic and non-interactive.
	full := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.pager=cat", "--no-pager"}, args...)
	cmd := exec.CommandContext(runCtx, bin, full...)
	cmd.Dir = resolved
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_OPTIONAL_LOCKS=0",
		"HOME=" + os.TempDir(), // no user-level git config, no credentials
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if runCtx.Err() != nil {
			return tool.RawResult{}, runCtx.Err()
		}
		return tool.RawResult{}, fmt.Errorf("%w: git failed: %s", ErrExternal, strings.TrimSpace(errBuf.String()))
	}
	data := out.String()
	max := t.Policy.MaxOutputBytes
	if inv.Limits.MaxOutputByte > 0 && inv.Limits.MaxOutputByte < max {
		max = inv.Limits.MaxOutputByte
	}
	truncated := false
	if len(data) > max {
		data = data[:max]
		truncated = true
	}
	return tool.RawResult{Result: map[string]string{
		"output":    data,
		"truncated": fmt.Sprint(truncated),
		"repo":      filepath.Base(resolved),
	}}, nil
}

// gitArgs maps an operation to a FIXED argument vector. Model input can only
// select bounded numeric/format options, never arbitrary flags.
func gitArgs(op string, input map[string]string) ([]string, error) {
	switch op {
	case "status":
		return []string{"status", "--porcelain=v1", "--branch"}, nil
	case "diff":
		args := []string{"diff", "--no-color", "--no-ext-diff"}
		if p := strings.TrimSpace(input["path"]); p != "" {
			args = append(args, "--", p)
		}
		return args, nil
	case "log":
		n := boundedInt(input["limit"], 20, 1, 200)
		return []string{"log", "--no-color", "--no-ext-diff", fmt.Sprintf("-n%d", n), "--pretty=format:%h %an %s"}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported git operation %q", ErrValidation, op)
	}
}

// resolveInRoots canonicalizes rel inside one of the allowed roots.
func resolveInRoots(rel string, roots []string) (string, string, error) {
	if len(roots) == 0 {
		return "", "", fmt.Errorf("%w: no git workspace root configured", ErrValidation)
	}
	for _, root := range roots {
		tool := &FilesystemTool{Policy: DefaultFilesystemPolicy(root)}
		if abs, relOut, err := tool.resolve(rel); err == nil {
			return abs, relOut, nil
		}
	}
	return "", "", fmt.Errorf("%w: repository is outside every configured workspace", ErrScope)
}

func boundedInt(v string, def, min, max int) int {
	n := def
	if v != "" {
		if parsed, err := parsePositiveInt(v); err == nil {
			n = parsed
		}
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func parsePositiveInt(v string) (int, error) {
	n := 0
	for _, c := range strings.TrimSpace(v) {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// GitManifest is the manifest for the git capability.
func GitManifest() tool.ToolManifest {
	return tool.ToolManifest{
		ID: "git", Version: "1.0.0", Name: "Git (read-only, sandboxed)",
		Description: "Read-only local git operations inside the workspace sandbox (operations: status, diff, log). Remote mutation is not available.",
		Category:    tool.ToolCategoryRead,
		InputSchema: tool.Schema{Fields: []tool.SchemaField{
			{Name: "repo", Type: tool.FieldString, Required: false, MaxLength: 256},
			{Name: "path", Type: tool.FieldString, Required: false, MaxLength: 256},
			{Name: "limit", Type: tool.FieldInt, Required: false, Max: 200, Min: 1},
		}},
		OutputSchema: tool.Schema{Fields: []tool.SchemaField{
			{Name: "output", Type: tool.FieldString, MaxLength: 65536},
			{Name: "truncated", Type: tool.FieldString},
			{Name: "repo", Type: tool.FieldString},
		}},
		SideEffectClass:    tool.SideEffectRead,
		NetworkRequirement: tool.NetworkNone,
		ScopeRequirement:   tool.ScopeWorkspace,
		SecurityClass:      tool.SecuritySandboxed,
		Operations:         []string{"diff", "log", "status"},
		ResourceLimits:     tool.ResourceLimits{MaxDuration: 10 * time.Second, MaxOutputByte: 65536},
	}
}
