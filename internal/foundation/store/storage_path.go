package store

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// prepareStorageDir validates the effective data directory at the boot
// boundary and returns its canonical, existing directory path.
//
// The contract is intentionally strict:
//   - the path must be absolute, clean, and free of traversal segments;
//   - the filesystem root and well-known sensitive system locations are
//     rejected, after symlink resolution;
//   - a configured symlink is resolved once and the canonical target is used
//     for every later operation, so a later symlink swap cannot redirect this
//     store through the configured path;
//   - the final component may be created only when its parent already exists
//     as a real directory;
//   - existing non-directory paths, dangling links and unexpected file types
//     fail closed.
func prepareStorageDir(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("store: data directory is required")
	}
	if strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("store: data directory contains a NUL byte")
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("store: data directory must be absolute: %q", raw)
	}
	for _, part := range strings.Split(raw, string(filepath.Separator)) {
		if part == ".." {
			return "", fmt.Errorf("store: data directory must not contain a traversal segment: %q", raw)
		}
	}

	clean := filepath.Clean(raw)
	if clean == string(filepath.Separator) {
		return "", fmt.Errorf("store: the filesystem root cannot be a data directory")
	}
	if sensitiveStoragePath(clean) {
		return "", fmt.Errorf("store: sensitive system path cannot be a data directory: %s", clean)
	}

	info, err := os.Lstat(clean)
	switch {
	case err == nil:
		// Resolve every symlink component once. The store then operates only
		// on the canonical target, not on the configured link path.
		resolved, rerr := filepath.EvalSymlinks(clean)
		if rerr != nil {
			return "", fmt.Errorf("store: resolve data directory %s: %w", clean, rerr)
		}
		clean = resolved
	case os.IsNotExist(err):
		parent := filepath.Dir(clean)
		resolvedParent, rerr := filepath.EvalSymlinks(parent)
		if rerr != nil {
			return "", fmt.Errorf("store: data directory parent %s must exist: %w", parent, rerr)
		}
		pinfo, perr := os.Stat(resolvedParent)
		if perr != nil {
			return "", fmt.Errorf("store: inspect data directory parent %s: %w", parent, perr)
		}
		if !pinfo.IsDir() {
			return "", fmt.Errorf("store: data directory parent %s is not a directory", parent)
		}
		clean = filepath.Join(resolvedParent, filepath.Base(clean))
	default:
		return "", fmt.Errorf("store: inspect data directory %s: %w", clean, err)
	}

	if clean == string(filepath.Separator) {
		return "", fmt.Errorf("store: the filesystem root cannot be a data directory")
	}
	if sensitiveStoragePath(clean) {
		return "", fmt.Errorf("store: sensitive system path cannot be a data directory: %s", clean)
	}

	// Create only the final missing component. The parent is already known to
	// exist and to be a real directory, so there is no recursive walk into an
	// unchecked ancestor.
	info, err = os.Lstat(clean)
	if os.IsNotExist(err) {
		if merr := os.Mkdir(clean, 0o700); merr != nil && !os.IsExist(merr) {
			return "", fmt.Errorf("store: create data directory %s: %w", clean, merr)
		}
	} else if err != nil {
		return "", fmt.Errorf("store: inspect data directory %s: %w", clean, err)
	}

	info, err = os.Lstat(clean)
	if err != nil {
		return "", fmt.Errorf("store: inspect data directory %s: %w", clean, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("store: data directory %s became a symlink during startup", clean)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("store: data directory %s is not a directory", clean)
	}
	return clean, nil
}

// tightenStorageDir removes group/other permission bits from an existing
// storage directory. Owner bits are preserved: a directory that is not
// writable by its owner is reported by the normal I/O path rather than being
// silently granted more access. Existing regular files are never rewritten by
// open; newly created files use 0600 in the store itself.
func tightenStorageDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("store: inspect directory %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("store: directory %s is a symlink", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("store: path %s is not a directory", path)
	}
	mode := info.Mode().Perm()
	tightened := mode &^ 0o077
	if tightened == mode {
		return nil
	}
	if err := os.Chmod(path, tightened); err != nil {
		return fmt.Errorf("store: restrict directory %s: %w", path, err)
	}
	return nil
}

// sensitiveStoragePath rejects well-known system locations after
// canonicalization. /tmp is intentionally allowed because the test suite and
// disposable installs use it, but it is never treated as durable storage.
func sensitiveStoragePath(path string) bool {
	roots := []string{
		"/etc", "/proc", "/sys", "/dev", "/boot", "/root", "/run", "/usr",
		"/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32",
		"/var/run", "/var/log", "/var/spool",
	}
	// On Darwin, /etc, /var, /tmp are symlinks into /private. Reject the
	// canonical spellings too so /private/etc and /private/var/{run,log,spool}
	// cannot be used to bypass the guard.
	if runtime.GOOS == "darwin" {
		roots = append(roots, "/private/etc", "/private/var/run", "/private/var/log", "/private/var/spool")
	}
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		// Resolve a symlinked home so a configured ~/.ssh under a linked home
		// is still rejected after canonicalization.
		if resolved, rerr := filepath.EvalSymlinks(home); rerr == nil && resolved != home {
			home = resolved
		}
		for _, name := range []string{".ssh", ".gnupg"} {
			root := filepath.Join(home, name)
			if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// validStoreComponent reports whether one path component is safe to join
// beneath the store root. Record IDs become file names; separators, NUL and
// directory-entry names would let a caller escape the type directory.
func validStoreComponent(component string) bool {
	if component == "" || component == "." || component == ".." {
		return false
	}
	return !strings.ContainsAny(component, "/\\\x00")
}
