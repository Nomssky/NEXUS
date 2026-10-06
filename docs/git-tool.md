# Git Tool

Manifest operations: `status`, `diff`, `log`. Local-only, inside the same
sandbox root as the filesystem tool. No remote mutations, no hooks, no
unrestricted `git` flags.

## Security boundary

The git binary is invoked with a **fixed argument vector**; there is no
shell, no configuration override, and no user-hook execution. Specifically:

```go
cmd := exec.CommandContext(runCtx, bin, args...)
cmd.Dir = resolved    // sandbox-resolved, canonicalized, must stay inside the root
cmd.Env = []string{
    "PATH=" + os.Getenv("PATH"),
    "GIT_TERMINAL_PROMPT=0",
    "GIT_CONFIG_NOSYSTEM=1",
    "GIT_OPTIONAL_LOCKS=0",
    "HOME=" + os.TempDir(), // no user-level git config, no credentials
}
args = "git -c core.hooksPath=/dev/null -c core.pager=cat --no-pager", per-op-args…
```

- `status --porcelain=v1 --branch`
- `diff --no-color --no-ext-diff [-- <path>]`
- `log --no-color --no-ext-diff -n<limit> --pretty=format:...`
- `GIT_CONFIG_NOSYSTEM=1` prevents injecting malicious system config.
- `GIT_OPTIONAL_LOCKS=0` + fixed prefix avoids the interactive lock.
- Repository resolution: every `repo` input goes through the same
  sandbox-root canonicalization as filesystem paths — path escapes like
  `../../etc/passwd` or absolute paths are rejected with `ErrScope`.

## Limitations (v1)

- **no push/pull/fetch/clone** — remote operations are explicitly out of scope.
- No new args from the model: the model can only pass `repo`, optional `limit`,
  optional `path`, which are all bounded and validated.
- Large outputs are bounded via `MaxOutputBytes` (and `policy.MaxOutputByte`).

## E2E coverage

The E2E spec seeds a standard `git init` + empty commit in the workspace
root. If git happens to be missing from the test host, the workspace test
handles the fail-open failure honestly rather than crashing.
