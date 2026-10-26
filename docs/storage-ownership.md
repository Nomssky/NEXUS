# Storage ownership and boot-time path safety

This document is the operational contract for NEXUS's file-backed record store
(`storage.data_dir`). It covers the single-writer guarantee, the boot-time
storage path rules, ownership lifecycle, and the limits of both. It describes
the current implementation; it is not a deployment platform.

Code: `internal/foundation/store/{ownership.go,ownership_unix.go,ownership_unsupported.go,storage_path.go,filestore.go}`.

---

## 1. Single-writer guarantee

A `FileStore` root is owned by **at most one live process**. Ownership is
acquired before any record is read or written and held until the process closes
the store or exits.

The primitive is an OS advisory lock (`flock(LOCK_EX|LOCK_NB)`) on:

```
<canonical storage.data_dir>/.nexus-store.lock
```

Properties:

- **Non-blocking.** A second process never waits or retries; construction fails
  closed with an error wrapping `store.ErrStorageOwned`.
- **Kernel-scoped.** The lock belongs to the open file description, not to a
  PID stored in the file. The kernel releases it when the process exits,
  including `SIGKILL` and crashes.
- **No stale-PID heuristic.** The lock file is intentionally **never deleted**.
  A leftover `.nexus-store.lock` after a crash is normal and harmless: only the
  kernel lock state is authoritative. Do not delete the file as a recovery
  step, and never "clear" it to force a second writer.
- **Symlink-resistant.** The lock file is opened with `O_NOFOLLOW` and
  `O_CLOEXEC`, so a planted link cannot redirect the lock and an `exec`'d child
  cannot inherit it.
- **Owner-only.** The lock file is created/restored as `0600`.
- **Not durable data.** The marker is ignored by record loading, excluded from
  backup manifests, and never treated as a record.

Exactly one process is the intended topology for a data directory. Do not run
two NEXUS processes against the same directory for "read-only" access either:
reads and writes share the same store and the second process is refused.

### 1.1 Failure behavior

- Startup (and `nexus-retention` / restore validation) fails before the HTTP
  surface serves requests.
- A refused contender performs no writes and does not disturb the owner.
- The error names the directory so an operator can identify the live owner.
- Once `FileStore.Close()` has run, all well-formed record operations fail with
  `STORE_CLOSED`; `CountAll()` returns 0. Close is idempotent.

### 1.2 Platform and filesystem limits

The build tags cover Linux, Darwin, FreeBSD, NetBSD, OpenBSD and DragonFly BSD
(the package is build-tested for those plus Windows; the lock primitive itself
is only functionally exercised on Linux in CI). On every other platform, store
construction fails closed with "exclusive storage ownership is not supported"
rather than silently dropping the guarantee.

Advisory locks are not reliable on general network filesystems (NFS/SMB) or on
filesystems that do not propagate `flock` semantics across hosts. **A shared
network filesystem is not a supported location for `storage.data_dir`.** If the
directory is on a network volume, the single-writer guarantee cannot be
enforced regardless of what this process checks; use local durable storage or a
single-writer block volume. Container platforms must also guarantee that only
one replica mounts the data directory read-write (e.g. a ReadWriteOnce volume
with one replica).

The directory must be writable by the process user: the lock file must be
creatable/opened and the directory must permit creating record files and type
directories.

---

## 2. Boot-time storage path validation

`storage.data_dir` is validated at the real construction boundary, before any
record access. The effective path must be:

- non-empty, absolute and clean;
- free of `..` traversal segments and NUL bytes;
- not the filesystem root;
- not a well-known sensitive system location: `/etc`, `/proc`, `/sys`, `/dev`,
  `/boot`, `/root`, `/run`, `/usr`, `/bin`, `/sbin`, `/lib*`, `/var/run`,
  `/var/log`, `/var/spool`, `~/.ssh`, `~/.gnupg`;
- an existing directory, or a final component whose immediate parent already
  exists as a real directory.

Additional rules:

- **Only the final missing component is created.** Missing intermediate parents
  are an error; NEXUS does not recursively create an unchecked subtree.
- **Existing non-directories and dangling links fail closed.** An existing
  directory that becomes a symlink between validation and use is rejected.
- **Configured symlinks are resolved once.** The canonical target is used for
  every later operation, so a later swap of the configured path cannot redirect
  an already-running store. The target is revalidated against the sensitive-root
  rules. Safe mounted-volume links (for example `/data -> /mnt/volume`) are
  permitted, but all I/O uses the resolved path.
- **Type directories are checked at open.** A symlinked type directory, or one
  that would escape the owned root, fails closed instead of being followed.
- **Record identifiers are single path components.** IDs or types containing
  separators, NUL, `.` or `..` are rejected, as are batch-journal records that
  carry them.

### 2.1 Permissions

- Newly created store roots and type directories: `0700`.
- New record files, the batch journal, the ownership marker, restored files and
  backup artifacts: `0600`.
- Existing storage directories have group/other permission bits removed while
  preserving owner bits. A directory with no owner write permission is left
  alone and fails naturally on I/O rather than being silently widened.
- Existing regular-file modes are **not** rewritten by open. New writes use
  temp file → fsync → close → chmod `0600` → rename → directory fsync.

These modes are deliberate: the store contains credential verification hashes
and organizational records. Backups inherit the same classification.

---

## 3. Lifecycle and release

Ownership is released on every lifecycle exit:

1. **Orderly shutdown.** The app registers a `record-store` lifecycle close
   hook. Hooks run in reverse registration order: core runtime → record store →
   health server. The store therefore closes only after the runtime that uses it
   has stopped, and before the health surface goes away.
2. **`App.Close()`** is idempotent and releases the store's lock. It is called
   from the lifecycle hook and again from a deferred close in `App.Run` (safe
   because idempotent).
3. **Startup failure.** If construction fails after the store opened (for
   example membership or registry hydration), the store is closed before the
   error is returned. The primary startup error is preserved; a cleanup failure
   is appended as diagnostic context.
4. **Early `cmd/nexus` exit.** After a successful `app.New`, a deferred
   `a.Close()` covers failures that occur before launcher hooks exist.
5. **Offline tools.** `nexus-retention` acquires the same lock and closes the
   store on every exit, including failures. Its non-zero outcome is preserved
   even if releasing the lock also fails.
6. **Restore validation.** `nexus-backup restore` opens the staged store,
   proves it reopens (including journal recovery), then **closes it before the
   atomic publish**. The published directory is owned by no process.

A crash (including `SIGKILL`) is safe: the kernel releases the lock with the
process, so the next boot can acquire the same directory immediately even
though the marker file remains.

---

## 4. Backup, restore and retention interaction

- **Backup is offline/quiescent.** `nexus-backup backup` does not open the
  source store and does not acquire the lock; the operator must stop NEXUS
  first. The `.nexus-store.lock` marker is excluded from the manifest.
- **Restore is offline and staged.** It refuses a non-empty destination, builds
  in a hidden staging directory, validates by reopening through the real store
  path, closes that validation store, and only then publishes by atomic rename.
- **Retention is offline.** `nexus-retention` now truly enforces quiescence: a
  live NEXUS process holding the directory makes the tool fail closed at open
  rather than relying on the operator to remember.
- **Layout is unchanged.** The only new entry is `.nexus-store.lock`; record
  paths (`<type>/<id>.json`) and `.batch-journal.json` semantics are unchanged.
  No migration is required.

---

## 5. Operator checklist

1. Stop the application before backup, restore or retention.
2. If startup reports `storage directory is already owned by another process`,
   find and stop the live owner. Do **not** delete `.nexus-store.lock`.
3. Use a local, durable, single-writer filesystem (or a volume with the
   equivalent guarantee); do not place `storage.data_dir` on NFS/SMB.
4. Keep `storage.data_dir` and backup artifacts owner-only (`0700` directories,
   `0600` files).
5. After an unclean host crash, simply restart. A surviving lock marker does not
   block recovery; a lock that cannot be acquired means another live process
   still owns the directory.
