package backup

import (
	"archive/tar"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

// Restore validates an artifact and materializes it as a durable store in
// destDir, then proves the result by reopening it through the store's real
// initialization/recovery path.
//
// Safety contract:
//   - destDir is required; it must not exist or must be an empty directory.
//   - A non-empty existing directory is refused (no silent overwrite).
//   - The store is built in a hidden staging directory beside destDir and
//     published with an atomic rename only after the WHOLE artifact verifies
//     and the store reopens cleanly. A failure removes the staging directory
//     and leaves destDir and the source artifact untouched.
//   - The caller must ensure destDir is not the currently active store root and
//     is not inside one; Restore never opens or mutates the source store.
func Restore(artifactPath, destDir string) (*Manifest, error) {
	if artifactPath == "" {
		return nil, fmt.Errorf("restore: artifact path is required")
	}
	if destDir == "" {
		return nil, fmt.Errorf("restore: destination is required")
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return nil, fmt.Errorf("restore: destination: %w", err)
	}

	switch info, statErr := os.Stat(absDest); {
	case statErr == nil && !info.IsDir():
		return nil, fmt.Errorf("restore: destination %q exists and is not a directory", destDir)
	case statErr == nil && info.IsDir():
		empty, err := dirIsEmpty(absDest)
		if err != nil {
			return nil, err
		}
		if !empty {
			return nil, fmt.Errorf("restore: destination %q is not empty; refusing to overwrite", destDir)
		}
	case !os.IsNotExist(statErr):
		return nil, fmt.Errorf("restore: destination: %w", statErr)
	}

	parent := filepath.Dir(absDest)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("restore: destination parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(absDest)+".restoring-*")
	if err != nil {
		return nil, fmt.Errorf("restore: staging dir: %w", err)
	}
	// Any failure path drops the staging directory entirely.
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()

	f, err := os.Open(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("restore: open artifact: %w", err)
	}
	defer f.Close()

	tr := tar.NewReader(f)
	man, err := readManifest(tr)
	if err != nil {
		return nil, err
	}
	if err := verifyFiles(tr, man, staging); err != nil {
		return nil, err
	}

	// Prove the restored store is openable through the real store path. This
	// also performs any batch-journal recovery the artifact carried.
	fs, err := store.NewFileStore(staging)
	if err != nil {
		return nil, fmt.Errorf("restore: restored store failed to reopen: %w", err)
	}
	// Release the validation lock BEFORE publishing: the destination must be
	// owned by no process when it appears, and a lock held across the rename
	// would otherwise pin the staging inode rather than the published root.
	if err := fs.Close(); err != nil {
		return nil, fmt.Errorf("restore: release validation store: %w", err)
	}

	// Publish: an existing empty destination directory is replaced.
	if _, statErr := os.Stat(absDest); statErr == nil {
		if err := os.Remove(absDest); err != nil {
			return nil, fmt.Errorf("restore: replace empty destination: %w", err)
		}
	}
	if err := os.Rename(staging, absDest); err != nil {
		return nil, fmt.Errorf("restore: publish: %w", err)
	}
	published = true
	return man, nil
}

func dirIsEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("restore: read destination: %w", err)
	}
	return len(entries) == 0, nil
}
