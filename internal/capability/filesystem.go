package capability

// Filesystem sandbox (contracts/CAPABILITY_TOOL_CONTRACTS.md §10): a controlled
// filesystem capability that operates ONLY inside a configured workspace root.
// Every path is canonicalized (abs + symlink resolution) and authorized AFTER
// canonicalization, so `../`, absolute paths and symlink escapes cannot leave
// the sandbox.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// FilesystemPolicy is the runtime-controlled sandbox configuration.
type FilesystemPolicy struct {
	// Root is the configured workspace root (absolute, canonicalized on use).
	Root string
	// MaxFileBytes bounds a single read or write.
	MaxFileBytes int
	// MaxEntries bounds a directory listing.
	MaxEntries int
}

// DefaultFilesystemPolicy bounds a root that must be supplied by the runtime.
func DefaultFilesystemPolicy(root string) FilesystemPolicy {
	return FilesystemPolicy{Root: root, MaxFileBytes: 256 * 1024, MaxEntries: 512}
}

// FilesystemTool is the sandboxed filesystem adapter.
type FilesystemTool struct{ Policy FilesystemPolicy }

// Operations implements tool.Adapter.
func (t *FilesystemTool) Operations() []string { return []string{"list", "read", "write"} }

// resolve canonicalizes rel inside the sandbox root and returns the absolute
// path plus its relative form. It fails closed on any escape.
func (t *FilesystemTool) resolve(rel string) (abs string, relOut string, err error) {
	if t.Policy.Root == "" {
		return "", "", fmt.Errorf("%w: filesystem sandbox root is not configured", ErrValidation)
	}
	if strings.TrimSpace(rel) == "" {
		return "", "", fmt.Errorf("%w: path is required", ErrValidation)
	}
	if strings.ContainsRune(rel, 0) {
		return "", "", fmt.Errorf("%w: path contains a null byte", ErrValidation)
	}
	root, err := filepath.Abs(t.Policy.Root)
	if err != nil {
		return "", "", fmt.Errorf("%w: sandbox root is invalid", ErrValidation)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", "", fmt.Errorf("%w: sandbox root cannot be resolved", ErrValidation)
		}
		if mkErr := os.MkdirAll(root, 0o750); mkErr != nil {
			return "", "", fmt.Errorf("%w: sandbox root cannot be created", ErrValidation)
		}
		root, _ = filepath.EvalSymlinks(root)
	}
	// Path traversal and absolute inputs are REJECTED, not neutralized: a
	// model asking for "../x" or "/etc/passwd" gets a hard denial.
	cleanRel := filepath.Clean(rel)
	if filepath.IsAbs(rel) || cleanRel == ".." ||
		strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) ||
		strings.Contains(cleanRel, string(os.PathSeparator)+"..") {
		return "", "", fmt.Errorf("%w: path traversal is not permitted", ErrScope)
	}
	joined := filepath.Join(root, cleanRel)
	abs, err = filepath.Abs(joined)
	if err != nil {
		return "", "", fmt.Errorf("%w: path cannot be resolved", ErrValidation)
	}
	// Resolve symlinks on the deepest existing ancestor so a symlinked file
	// cannot smuggle the path out of the sandbox.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", "", fmt.Errorf("%w: path cannot be resolved", ErrValidation)
		}
		// New paths (e.g. a file write) do not exist yet: resolve the deepest
		// EXISTING ancestor and rebuild the leaf. This still catches symlink
		// escapes through any pre-existing directory.
		ancestor := abs
		for {
			if a, aerr := filepath.EvalSymlinks(ancestor); aerr == nil {
				rel, _ := filepath.Rel(ancestor, abs)
				resolved = filepath.Join(a, rel)
				break
			}
			parent := filepath.Dir(ancestor)
			if parent == ancestor {
				return "", "", fmt.Errorf("%w: parent directory cannot be resolved", ErrValidation)
			}
			ancestor = parent
		}
	}
	if !withinRoot(root, resolved) {
		return "", "", fmt.Errorf("%w: path escapes the sandbox", ErrScope)
	}
	relOut, err = filepath.Rel(root, resolved)
	if err != nil {
		return "", "", fmt.Errorf("%w: path is outside the sandbox", ErrScope)
	}
	return resolved, relOut, nil
}

// withinRoot reports whether path is inside root (segment-wise, not prefix).
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}

// Invoke implements tool.Adapter.
func (t *FilesystemTool) Invoke(ctx context.Context, inv tool.Invocation) (tool.RawResult, error) {
	if err := ctx.Err(); err != nil {
		return tool.RawResult{}, err
	}
	abs, rel, err := t.resolve(inv.Input["path"])
	if err != nil {
		return tool.RawResult{}, err
	}
	switch inv.Operation {
	case "read":
		info, err := os.Lstat(abs)
		if err != nil {
			return tool.RawResult{}, fmt.Errorf("%w: file not found", ErrExternal)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return tool.RawResult{}, fmt.Errorf("%w: symlinked files are not read directly", ErrScope)
		}
		if !info.Mode().IsRegular() {
			return tool.RawResult{}, fmt.Errorf("%w: only regular files are readable", ErrValidation)
		}
		limit := t.Policy.MaxFileBytes
		if inv.Limits.MaxOutputByte > 0 && inv.Limits.MaxOutputByte < limit {
			limit = inv.Limits.MaxOutputByte
		}
		if int(info.Size()) > limit {
			return tool.RawResult{}, fmt.Errorf("%w: file exceeds the size limit", ErrResourceLimit)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return tool.RawResult{}, fmt.Errorf("%w: read failed: %v", ErrExternal, err)
		}
		return tool.RawResult{Result: map[string]string{"path": rel, "content": string(data), "bytes": fmt.Sprint(len(data))}}, nil
	case "write":
		content := inv.Input["content"]
		if len(content) > t.Policy.MaxFileBytes {
			return tool.RawResult{}, fmt.Errorf("%w: write exceeds the size limit", ErrResourceLimit)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			return tool.RawResult{}, fmt.Errorf("%w: cannot create directory", ErrExternal)
		}
		if err := os.WriteFile(abs, []byte(content), 0o640); err != nil {
			return tool.RawResult{}, fmt.Errorf("%w: write failed: %v", ErrExternal, err)
		}
		return tool.RawResult{Result: map[string]string{"path": rel, "bytes": fmt.Sprint(len(content))}}, nil
	case "list":
		entries, err := os.ReadDir(abs)
		if err != nil {
			return tool.RawResult{}, fmt.Errorf("%w: directory not readable", ErrExternal)
		}
		if len(entries) > t.Policy.MaxEntries {
			return tool.RawResult{}, fmt.Errorf("%w: directory has too many entries", ErrResourceLimit)
		}
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		return tool.RawResult{Result: map[string]string{"path": rel, "entries": strings.Join(names, "\n"), "count": fmt.Sprint(len(names))}}, nil
	default:
		return tool.RawResult{}, fmt.Errorf("%w: unsupported operation %q", ErrValidation, inv.Operation)
	}
}

// FilesystemManifest is the manifest for the filesystem capability.
func FilesystemManifest(id string, ops []string) tool.ToolManifest {
	return tool.ToolManifest{
		ID: id, Version: "1.0.0", Name: "Filesystem (sandboxed)",
		Description: "Sandboxed filesystem access inside the configured workspace root (operations: read, write, list). Path traversal and symlink escapes are rejected.",
		Category:    tool.ToolCategoryRead,
		InputSchema: tool.Schema{Fields: []tool.SchemaField{
			{Name: "path", Type: tool.FieldString, Required: true, MaxLength: 512},
			{Name: "content", Type: tool.FieldString, Required: false, MaxLength: 262144},
		}},
		OutputSchema: tool.Schema{Fields: []tool.SchemaField{
			{Name: "path", Type: tool.FieldString},
			{Name: "content", Type: tool.FieldString, MaxLength: 262144},
			{Name: "bytes", Type: tool.FieldString},
			{Name: "entries", Type: tool.FieldString, MaxLength: 65536},
			{Name: "count", Type: tool.FieldString},
		}},
		SideEffectClass:    tool.SideEffectWrite, // write is a side effect; read is a sub-operation
		NetworkRequirement: tool.NetworkNone,
		ScopeRequirement:   tool.ScopeWorkspace,
		SecurityClass:      tool.SecuritySandboxed,
		Operations:         ops,
		ResourceLimits:     tool.ResourceLimits{MaxDuration: 5 * time.Second, MaxOutputByte: 262144, MaxRequestByt: 262144},
	}
}
