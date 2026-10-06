# Filesystem Sandbox

`filesystem.read`, `filesystem.write`, `filesystem.list` are the only
filesystem tools exposed. They are **never** a host-wide view: every path is
resolved against one fixed sandbox root.

## Sandbox resolution

1. Root comes from `NEXUS_TOOL_FS_ROOT` or the default
   `<data_dir>/workspaces/<business_id>` — set by launcher option, never
   by model output.
2. `FilesystemTool.resolve` joins the input with the root using
   `filepath.Join(root, cleanRel)` after rejecting absolute inputs and any
   `..` segment in the cleaned relative path; this makes `..`, `..\\`,
   null bytes, and absolute paths fail closed.
3. `filepath.EvalSymlinks` on the deepest existing ancestor lets us detect
   attempts to smuggle the path through a symlinked directory even for
   not-yet-existing targets.
4. `withinRoot` asserts `Rel(root, resolved)` stays non-`..` after
   canonicalization — the contract's rule is that authorization must be
   based on the **resolved path**, not on symlink-infected input.
5. Read refuses symlinked files and non-regular files directly.

## Limits

- `MaxFileBytes`: the hard cap (default 256KiB), with
  `inv.Limits.MaxOutputByte` taking the smaller of the two.
- `MaxEntries`: directory listing cap.
- traversal outside root → `ErrScope`.

## Tests

`../secret.txt`, `notes/../../secret.txt`, `/etc/passwd`, symlinked file
read, symlinked directory escape, null byte, oversized file, oversized
directory, unconfigured root — all reject with proper classifications.
