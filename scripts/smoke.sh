#!/usr/bin/env bash
#
# NEXUS release smoke test (Production Hardening v4).
#
# Everything runs in an isolated temporary directory with a dynamically chosen
# free port. It NEVER points at a real/developer data directory, never uses a
# hard-coded port, and cleans up only the processes and directories it created.
#
#   1. build nexus + nexus-backup from the current source
#   2. report version/build metadata for both binaries
#   3. boot nexus on isolated config + data
#   4. confirm /health responds
#   5. stop the process
#   6. back up the isolated durable store, verify the artifact
#   7. restore into a fresh destination, reopen the restored store
#   8. verify release checksums if dist/ exists
#   9. clean up
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

NEXUS_PID=""
WORK="$(mktemp -d "${TMPDIR:-/tmp}/nexus-smoke.XXXXXX")"
cleanup() {
  if [ -n "$NEXUS_PID" ] && kill -0 "$NEXUS_PID" 2>/dev/null; then
    kill "$NEXUS_PID" 2>/dev/null || true
    wait "$NEXUS_PID" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}

wait_health() { # wait_health <port>
  local port="$1" i
  for i in $(seq 1 50); do
    if curl -fsS "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  return 1
}

echo "smoke: work dir $WORK"

# 1. Build both binaries from the current source.
mkdir -p "$WORK/bin"
CGO_ENABLED=0 go build -trimpath -o "$WORK/bin/nexus" ./cmd/nexus
CGO_ENABLED=0 go build -trimpath -o "$WORK/bin/nexus-backup" ./cmd/nexus-backup

# 2. Version/build metadata.
echo "smoke: $("$WORK/bin/nexus" -version)"
echo "smoke: $("$WORK/bin/nexus-backup" -version)"

# 3. Boot on isolated config + data.
PORT="$(free_port)"
export NEXUS_DATA_DIR="$WORK/data"
export NEXUS_BOOTSTRAP_CREDENTIAL="smoke-secret-$$"
export NEXUS_BOOTSTRAP_BUSINESS="smoke"
export NEXUS_HEALTH_PORT="$PORT"
export NEXUS_ENVIRONMENT="development"
"$WORK/bin/nexus" -http-addr 127.0.0.1:0 >"$WORK/nexus.log" 2>&1 &
NEXUS_PID=$!

# 4. Health check (bounded).
if ! wait_health "$PORT"; then
  echo "smoke: FAIL — health never responded" >&2
  tail -20 "$WORK/nexus.log" >&2 || true
  exit 1
fi
echo "smoke: health OK on port $PORT"

# 5. Stop the process.
kill "$NEXUS_PID" 2>/dev/null || true
wait "$NEXUS_PID" 2>/dev/null || true
NEXUS_PID=""
echo "smoke: process stopped"

if [ ! -d "$WORK/data" ] || [ -z "$(find "$WORK/data" -name '*.json' -print -quit)" ]; then
  echo "smoke: FAIL — isolated store produced no durable records" >&2
  exit 1
fi

# 6. Backup + verify.
"$WORK/bin/nexus-backup" backup -data-dir "$WORK/data" -out "$WORK/backup.tar"
"$WORK/bin/nexus-backup" verify -in "$WORK/backup.tar"
echo "smoke: backup + verify OK"

# 7. Restore into a fresh destination, then reopen the restored store.
"$WORK/bin/nexus-backup" restore -in "$WORK/backup.tar" -dest "$WORK/restored"
PORT2="$(free_port)"
NEXUS_DATA_DIR="$WORK/restored" NEXUS_BOOTSTRAP_CREDENTIAL="smoke-secret-$$" \
  NEXUS_BOOTSTRAP_BUSINESS="smoke" NEXUS_HEALTH_PORT="$PORT2" NEXUS_ENVIRONMENT="development" \
  "$WORK/bin/nexus" -http-addr 127.0.0.1:0 >"$WORK/restored.log" 2>&1 &
NEXUS_PID=$!
if ! wait_health "$PORT2"; then
  echo "smoke: FAIL — restored store did not boot" >&2
  tail -20 "$WORK/restored.log" >&2 || true
  exit 1
fi
kill "$NEXUS_PID" 2>/dev/null || true
wait "$NEXUS_PID" 2>/dev/null || true
NEXUS_PID=""
echo "smoke: restored store reopened OK"

# 8. Verify release checksums if a release was built.
if [ -f "$REPO_ROOT/dist/SHA256SUMS" ]; then
  "$REPO_ROOT/scripts/release-verify.sh"
  echo "smoke: release checksums OK"
else
  echo "smoke: no dist/SHA256SUMS present (run scripts/release.sh to verify release artifacts)"
fi

echo "smoke: PASS"