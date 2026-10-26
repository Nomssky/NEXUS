//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package store

import (
	"fmt"
	"runtime"
)

// storageOwnership is deliberately unavailable on platforms without the
// project's tested advisory-lock primitive. Startup fails closed rather than
// silently dropping the single-writer guarantee.
type storageOwnership struct{}

func acquireStorageOwnership(string) (*storageOwnership, error) {
	return nil, fmt.Errorf("store: exclusive storage ownership is not supported on %s", runtime.GOOS)
}

func (*storageOwnership) Close() error { return nil }
