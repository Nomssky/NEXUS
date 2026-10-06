package capability

// Registration of the shipped capability catalogue (contract §5 step 1): one
// registry, manifest-validated, with the deterministic builtin tools retained.
// Policies (filesystem root, HTTP destinations, git roots, research provider,
// GitHub credential reference) are runtime configuration — never model input.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// Options is the runtime configuration for the shipped catalogue.
type Options struct {
	// WorkspaceRoot is the filesystem/git sandbox root (required for the
	// filesystem and git tools to be registered at all).
	WorkspaceRoot string
	// HTTPPolicy is the SSRF policy for the http tool.
	HTTPPolicy HTTPPolicy
	// ResearchProvider is the web-search backend.
	ResearchProvider ResearchProvider
	// GitHubCredentialRef is the business-scoped credential reference for the
	// GitHub tools; when empty, the GitHub tools are not registered.
	GitHubCredentialRef string
	// GitBinary overrides the git executable (tests).
	GitBinary string
	// GitHubBusinessID is the business that owns the GitHub credential scope.
	GitHubBusinessID string
}

// LoadWorkspaceRoot derives the sandbox root from the environment
// (NEXUS_TOOL_FS_ROOT, default <data_dir>/workspaces/<business>). Fail closed:
// an unusable root disables the filesystem/git tools instead of defaulting to
// "/" or the current directory.
func LoadWorkspaceRoot(env func(string) string, dataDir, businessID string) (string, error) {
	if root := strings.TrimSpace(env("NEXUS_TOOL_FS_ROOT")); root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", fmt.Errorf("%w: NEXUS_TOOL_FS_ROOT is not a usable path", ErrValidation)
		}
		return abs, nil
	}
	if dataDir == "" || businessID == "" {
		return "", fmt.Errorf("%w: no data dir or business for the workspace root", ErrValidation)
	}
	return filepath.Join(dataDir, "workspaces", businessID), nil
}

// RegisterAll registers the shipped capability catalogue into reg. Existing ids
// are skipped so it is safe to call after the builtins are registered.
func RegisterAll(reg *tool.ToolRegistry, opts Options) error {
	// Filesystem: one tool per operation id (filesystem.read/write/list share
	// the same sandboxed adapter, declared as separate tools so the manifest
	// side-effect class is explicit per operation).
	if opts.WorkspaceRoot != "" {
		fs := &FilesystemTool{Policy: DefaultFilesystemPolicy(opts.WorkspaceRoot)}
		ids := map[string][]string{
			"filesystem.read":  {"read"},
			"filesystem.write": {"write"},
			"filesystem.list":  {"list"},
		}
		for id, ops := range ids {
			m := FilesystemManifest(id, ops)
			if err := reg.Register(m, &opScopedAdapter{ops: ops, inner: fs}); err != nil {
				return fmt.Errorf("%w: %s: %v", ErrValidation, id, err)
			}
		}
		git := &GitTool{Policy: GitPolicy{
			Roots: []string{opts.WorkspaceRoot}, MaxOutputBytes: 64 * 1024,
			Timeout: 10 * time.Second, Binary: opts.GitBinary,
		}}
		if err := reg.Register(GitManifest(), git); err != nil {
			return fmt.Errorf("%w: git: %v", ErrValidation, err)
		}
	}
	if opts.ResearchProvider != nil {
		web := &WebSearchTool{Provider: opts.ResearchProvider, MaxSnippet: 512, MaxResults: 10}
		if err := reg.Register(WebSearchManifest(), web); err != nil {
			return fmt.Errorf("%w: web.search: %v", ErrValidation, err)
		}
	}
	if err := reg.Register(DataManifest(), &DataTool{}); err != nil {
		return fmt.Errorf("%w: data: %v", ErrValidation, err)
	}
	httpTool := NewHTTPTool(opts.HTTPPolicy)
	if err := reg.Register(HTTPToolManifest(), httpTool); err != nil {
		return fmt.Errorf("%w: http.request: %v", ErrValidation, err)
	}
	if opts.GitHubCredentialRef != "" {
		gh := NewGitHubTool(httpTool)
		gh.UseCredential = true
		for _, m := range GitHubManifests(opts.GitHubBusinessID, opts.GitHubCredentialRef) {
			if err := reg.Register(m, &opScopedAdapter{ops: []string{"execute"}, inner: gh}); err != nil {
				return fmt.Errorf("%w: %s: %v", ErrValidation, m.ID, err)
			}
		}
	}
	return nil
}

// opScopedAdapter exposes a multi-operation adapter under a single-operation
// manifest (filesystem.read/write/list, github.*): the manifest declares the
// only operation this tool id may run, so a model cannot reach the other
// operations through this id.
type opScopedAdapter struct {
	ops   []string
	inner tool.Adapter
}

func (a *opScopedAdapter) Operations() []string { return a.ops }

func (a *opScopedAdapter) Invoke(ctx context.Context, inv tool.Invocation) (tool.RawResult, error) {
	if len(a.ops) > 0 {
		inv.Operation = a.ops[0]
	}
	return a.inner.Invoke(ctx, inv)
}

// EnsureWorkspaceRoot creates the sandbox root directory (0700) if missing.
func EnsureWorkspaceRoot(root string) error {
	if root == "" {
		return fmt.Errorf("%w: workspace root is empty", ErrValidation)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("%w: cannot create workspace root: %v", ErrValidation, err)
	}
	return nil
}
