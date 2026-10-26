package store

import "errors"

// storageLockName is the advisory-lock file at the store root. It is not a
// record and is deliberately ignored by backup, restore and the record loader.
// The file may remain on disk after a process exits; only the OS lock state is
// authoritative, so a stale file never blocks a later boot.
const storageLockName = ".nexus-store.lock"

// sandboxDirName is the filesystem-sandbox root beneath the store. It is not
// a record type directory and must not be scanned by loadAll (it is also
// excluded from backups). It lives in the store package so the loader can skip
// it without importing the backup package (which would cycle).
const sandboxDirName = "workspaces"

// ErrStorageOwned reports that another live process already owns the storage
// directory. It is a startup error, never a transient retry signal: the caller
// must not proceed as an independent writer.
var ErrStorageOwned = errors.New("storage directory is already owned by another process")
