// Package backup implements the durable-store backup and restore contract
// (docs/operations-runbook.md).
//
// Scope: it backs up ONLY the durable record store root — the FileStore's
// <type>/<id>.json record files and an in-flight .batch-journal.json, exactly
// the files the store's own open path reads. It deliberately EXCLUDES the
// filesystem-sandbox tree (workspaces/) and every piece of process-local
// runtime state (executions, approvals/resume snapshots, cancellations,
// escalations, event buffers and traces): those are not durable and are NOT
// recoverable from a backup. See the package's exclusions in the manifest.
//
// Consistency: the store writes each record atomically (temp file, fsync,
// rename, directory fsync), so a read-only copy taken while no writer is active
// is a consistent committed state. Backup therefore requires QUIESCENCE (the
// application stopped) and never opens or mutates the source store. If a
// crash left a batch journal behind, it is copied verbatim and the restored
// store's normal open-time recovery resolves it — the artifact always
// represents a state the store itself would accept on reopen.
//
// The artifact is a single tar file whose FIRST entry is the manifest
// (nexus-backup-manifest.json) and whose remaining entries are the store files
// at their store-relative paths. There is no second archive format and no
// generic backup framework.
package backup

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// Format is the artifact family identifier.
	Format = "nexus-backup"
	// FormatVersion is the artifact/manifest schema version. An artifact with a
	// different version is rejected rather than guessed at.
	FormatVersion = 1
	// ManifestName is the manifest entry, always the first tar entry.
	ManifestName = "nexus-backup-manifest.json"
	// StoreLayout names the durable layout the artifact captures.
	StoreLayout = "filestore-json/v1"
	// journalName is the store's crash-recovery journal.
	journalName = ".batch-journal.json"
	// sandboxDirName is the filesystem-sandbox root, excluded from backups.
	sandboxDirName = "workspaces"
)

// FileEntry is one captured file.
type FileEntry struct {
	// Path is store-relative, forward-slash separated.
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest describes a backup artifact. It contains no secrets.
type Manifest struct {
	Format        string      `json:"format"`
	FormatVersion int         `json:"format_version"`
	CreatedAt     string      `json:"created_at"`
	StoreLayout   string      `json:"store_layout"`
	Scope         string      `json:"scope"`
	Exclusions    []string    `json:"exclusions"`
	Files         []FileEntry `json:"files"`
}

// Exclusions is the fixed, documented set of things the artifact never
// contains. It is written into every manifest so the artifact is self-describing.
var Exclusions = []string{
	"process-local runtime state: executions and their in-memory results",
	"process-local runtime state: approval and resume snapshots",
	"process-local runtime state: cancellation and escalation state",
	"process-local runtime state: event buffers and execution traces",
	"filesystem sandbox tree (workspaces/)",
	"provider API keys and secrets (environment-only, never stored)",
	"host environment files and unrelated host data",
}

// Create copies the durable store under dataDir into a new, self-contained
// artifact at artifactPath. It never mutates dataDir and refuses to overwrite
// an existing artifact.
func Create(dataDir, artifactPath string) (*Manifest, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("backup: data-dir is required")
	}
	if strings.TrimSpace(artifactPath) == "" {
		return nil, errors.New("backup: artifact path is required")
	}
	info, err := os.Stat(dataDir)
	if err != nil {
		return nil, fmt.Errorf("backup: data-dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("backup: data-dir %q is not a directory", dataDir)
	}
	if _, err := os.Stat(artifactPath); err == nil {
		return nil, fmt.Errorf("backup: artifact %q already exists; refusing to overwrite", artifactPath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("backup: artifact path: %w", err)
	}

	files, err := collectFiles(dataDir)
	if err != nil {
		return nil, err
	}

	man := &Manifest{
		Format:        Format,
		FormatVersion: FormatVersion,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		StoreLayout:   StoreLayout,
		Scope:         "durable record store (FileStore JSON records and batch journal)",
		Exclusions:    Exclusions,
		Files:         files,
	}
	manBytes, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("backup: manifest: %w", err)
	}

	// Write to a temp file in the destination directory, then rename atomically.
	dir := filepath.Dir(artifactPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("backup: artifact dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".nexus-backup-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("backup: temp artifact: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	tw := tar.NewWriter(tmp)
	if err := writeTarEntry(tw, ManifestName, manBytes); err != nil {
		_ = tw.Close()
		_ = tmp.Close()
		return nil, err
	}
	for _, fe := range files {
		data, err := os.ReadFile(filepath.Join(dataDir, filepath.FromSlash(fe.Path)))
		if err != nil {
			_ = tw.Close()
			_ = tmp.Close()
			return nil, fmt.Errorf("backup: read %s: %w", fe.Path, err)
		}
		if err := writeTarEntry(tw, fe.Path, data); err != nil {
			_ = tw.Close()
			_ = tmp.Close()
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("backup: finalize tar: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("backup: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("backup: close: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return nil, fmt.Errorf("backup: chmod: %w", err)
	}
	if err := os.Rename(tmpName, artifactPath); err != nil {
		return nil, fmt.Errorf("backup: publish: %w", err)
	}
	return man, nil
}

// collectFiles walks the durable store shape: the root crash journal plus every
// <type>/<name>.json record file. It excludes the filesystem sandbox and never
// follows a symlink.
func collectFiles(dataDir string) ([]FileEntry, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("backup: read data-dir: %w", err)
	}

	var out []FileEntry
	// Root-level crash journal (if a crash left one).
	if fi, err := os.Lstat(filepath.Join(dataDir, journalName)); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("backup: refusing symlink %s", journalName)
		}
		if fi.Mode().IsRegular() {
			fe, err := hashFile(dataDir, journalName)
			if err != nil {
				return nil, err
			}
			out = append(out, fe)
		}
	}

	for _, e := range entries {
		name := e.Name()
		if name == sandboxDirName {
			continue // filesystem sandbox is explicitly excluded
		}
		if e.Type()&os.ModeSymlink != 0 {
			// A symlink in the store root (including one that points at a
			// directory) is unexpected and could escape the root if followed.
			return nil, fmt.Errorf("backup: refusing symlink %q in data root", name)
		}
		if !e.IsDir() {
			continue
		}
		sub, err := os.ReadDir(filepath.Join(dataDir, name))
		if err != nil {
			return nil, fmt.Errorf("backup: read type dir %q: %w", name, err)
		}
		for _, f := range sub {
			if f.IsDir() || f.Type()&os.ModeSymlink != 0 {
				continue
			}
			if strings.ToLower(filepath.Ext(f.Name())) != ".json" {
				continue // temp files (*.tmp) and anything else are not durable records
			}
			rel := name + "/" + f.Name()
			fe, err := hashFile(dataDir, rel)
			if err != nil {
				return nil, err
			}
			out = append(out, fe)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// hashFile returns the {size, sha256} entry for a store-relative file.
func hashFile(dataDir, rel string) (FileEntry, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, filepath.FromSlash(rel)))
	if err != nil {
		return FileEntry{}, fmt.Errorf("backup: read %s: %w", rel, err)
	}
	sum := sha256.Sum256(data)
	return FileEntry{Path: rel, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}, nil
}

func writeTarEntry(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:     name,
		Mode:     0o644,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
		ModTime:  time.Unix(0, 0).UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: tar header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("backup: tar write %s: %w", name, err)
	}
	return nil
}

// safeRelPath rejects absolute, traversal, backslash, or otherwise unsafe
// store-relative paths. It returns the path unchanged when safe.
func safeRelPath(p string) error {
	if p == "" {
		return errors.New("empty path")
	}
	if strings.Contains(p, "\\") {
		return fmt.Errorf("backslash in path %q", p)
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("absolute path %q", p)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("path %q is not clean", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("unsafe path segment in %q", p)
		}
	}
	return nil
}

// Verify checks an artifact's manifest, inventory and checksums without
// mutating anything, and returns the manifest.
func Verify(artifactPath string) (*Manifest, error) {
	f, err := os.Open(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("verify: open: %w", err)
	}
	defer f.Close()

	tr := tar.NewReader(f)
	man, err := readManifest(tr)
	if err != nil {
		return nil, err
	}
	if err := verifyFiles(tr, man, ""); err != nil {
		return nil, err
	}
	return man, nil
}

// readManifest reads the first tar entry, which must be the manifest.
func readManifest(tr *tar.Reader) (*Manifest, error) {
	hdr, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("verify: empty or unreadable artifact: %w", err)
	}
	if hdr.Name != ManifestName {
		return nil, fmt.Errorf("verify: first entry is %q, expected the manifest", hdr.Name)
	}
	if hdr.Typeflag != tar.TypeReg {
		return nil, errors.New("verify: manifest entry is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(tr, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("verify: read manifest: %w", err)
	}
	var man Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, fmt.Errorf("verify: malformed manifest: %w", err)
	}
	if man.Format != Format {
		return nil, fmt.Errorf("verify: unsupported format %q", man.Format)
	}
	if man.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("verify: unsupported format version %d", man.FormatVersion)
	}
	// The manifest inventory itself must be safe and duplicate-free.
	seen := map[string]struct{}{}
	for _, fe := range man.Files {
		if err := safeRelPath(fe.Path); err != nil {
			return nil, fmt.Errorf("verify: unsafe manifest path: %w", err)
		}
		if fe.Path == ManifestName {
			return nil, errors.New("verify: manifest lists itself")
		}
		if _, dup := seen[fe.Path]; dup {
			return nil, fmt.Errorf("verify: duplicate path %q in manifest", fe.Path)
		}
		seen[fe.Path] = struct{}{}
	}
	return &man, nil
}

// verifyFiles streams the remaining entries, checking each against the manifest
// and (when outDir != "") writing it under outDir after path validation.
func verifyFiles(tr *tar.Reader, man *Manifest, outDir string) error {
	want := map[string]FileEntry{}
	for _, fe := range man.Files {
		want[fe.Path] = fe
	}
	seen := map[string]struct{}{}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("verify: read entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("verify: entry %q is not a regular file (symlinks/links are rejected)", hdr.Name)
		}
		if err := safeRelPath(hdr.Name); err != nil {
			return fmt.Errorf("verify: unsafe entry: %w", err)
		}
		fe, ok := want[hdr.Name]
		if !ok {
			return fmt.Errorf("verify: entry %q is not listed in the manifest", hdr.Name)
		}
		if _, dup := seen[hdr.Name]; dup {
			return fmt.Errorf("verify: duplicate entry %q in artifact", hdr.Name)
		}
		seen[hdr.Name] = struct{}{}

		data, err := io.ReadAll(tr)
		if err != nil {
			return fmt.Errorf("verify: read %s: %w", hdr.Name, err)
		}
		if int64(len(data)) != fe.Size {
			return fmt.Errorf("verify: size mismatch for %s (manifest %d, artifact %d)", hdr.Name, fe.Size, len(data))
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != fe.SHA256 {
			return fmt.Errorf("verify: checksum mismatch for %s", hdr.Name)
		}
		if outDir != "" {
			if err := writeRestored(outDir, hdr.Name, data); err != nil {
				return err
			}
		}
	}

	for _, fe := range man.Files {
		if _, ok := seen[fe.Path]; !ok {
			return fmt.Errorf("verify: manifest lists %q but the artifact omits it", fe.Path)
		}
	}
	return nil
}

// writeRestored writes one verified entry under outDir, creating parent dirs.
func writeRestored(outDir, rel string, data []byte) error {
	dst := filepath.Join(outDir, filepath.FromSlash(rel))
	if !withinDir(outDir, dst) {
		return fmt.Errorf("verify: entry %q escapes the destination", rel)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("restore: mkdir for %s: %w", rel, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("restore: write %s: %w", rel, err)
	}
	return nil
}

// withinDir reports whether target is inside root (after cleaning).
func withinDir(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// FileExists reports whether a path exists (used by callers for preconditions).
func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
