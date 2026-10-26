//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// storageOwnership is an OS advisory lock held for the lifetime of a FileStore.
// flock ownership belongs to the open file description, so it is released by
// the kernel on process exit (including SIGKILL) and cannot be mistaken for a
// stale PID inside a leftover file.
type storageOwnership struct {
	file *os.File
}

func acquireStorageOwnership(dir string) (*storageOwnership, error) {
	path := dir + string(os.PathSeparator) + storageLockName
	// O_NOFOLLOW prevents a planted symlink from redirecting the lock file.
	// O_CLOEXEC keeps an exec'd child from inheriting the lock.
	f, err := os.OpenFile(path,
		os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("store: open ownership lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("%w: %s", ErrStorageOwned, dir)
		}
		return nil, fmt.Errorf("store: acquire ownership lock %s: %w", path, err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("store: inspect ownership lock %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("store: ownership lock %s is not a regular file", path)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("store: restrict ownership lock %s: %w", path, err)
	}
	return &storageOwnership{file: f}, nil
}

func (o *storageOwnership) Close() error {
	if o == nil || o.file == nil {
		return nil
	}
	f := o.file
	o.file = nil
	unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	closeErr := f.Close()
	if unlockErr != nil {
		return errors.Join(unlockErr, closeErr)
	}
	return closeErr
}
