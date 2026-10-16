#!/usr/bin/env bash
#
# NEXUS release artifact builder (Production Hardening v4).
#
# Builds the NEXUS application and the nexus-backup operator tool for the host
# GOOS/GOARCH into a fresh, tool-owned output directory under dist/, writes a
# release manifest, and generates dist/SHA256SUMS.
#
# It never touches the live data directory, never publishes anything, and never
# runs `git clean`. Only the dedicated dist/ output is (re)created.
#
# Usage:
#   scripts/release.sh [version]
#
# Environment:
#   VERSION   overrides the version string (default: 0.0.0-dev)
#   DIST      overrides the output root (default: <repo>/dist)
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

VERSION="${1:-${VERSION:-0.0.0-dev}}"
DIST="${DIST:-$REPO_ROOT/dist}"

# Source revision and tree state (honest about uncommitted work).
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
  if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
    TREE_STATE="dirty"
  else
    TREE_STATE="clean"
  fi
else
  COMMIT="unknown"
  TREE_STATE="unknown"
fi

GOOS_T="$(go env GOOS)"
GOARCH_T="$(go env GOARCH)"
GO_VERSION="$(go version | awk '{print $3}')"
BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

ART_DIR="$DIST/nexus-${VERSION}-${GOOS_T}-${GOARCH_T}"
LD_VERSION="github.com/Nomssky/NEXUS/internal/foundation/version.Version"
LD_COMMIT="github.com/Nomssky/NEXUS/internal/foundation/version.Commit"
LDFLAGS="-s -w -X ${LD_VERSION}=${VERSION} -X ${LD_COMMIT}=${COMMIT}"

echo "release: version=${VERSION} commit=${COMMIT} tree=${TREE_STATE} target=${GOOS_T}/${GOARCH_T}"

# Recreate ONLY the dedicated output root, and only after verifying it is a
# tool-owned release path. dist/ is git-ignored; a custom DIST must either be
# empty or carry a prior release's SHA256SUMS. This is never the live data dir.
if [ -e "$DIST" ]; then
  if [ ! -d "$DIST" ]; then
    echo "release: refusing — DIST=$DIST exists and is not a directory" >&2
    exit 1
  fi
  if [ -n "$(ls -A "$DIST" 2>/dev/null)" ] && [ ! -f "$DIST/SHA256SUMS" ]; then
    echo "release: refusing to clean non-empty DIST without SHA256SUMS (not a tool-owned release path): $DIST" >&2
    exit 1
  fi
fi
rm -rf "$DIST"
mkdir -p "$ART_DIR"

# Build both binaries from the CURRENT source with -trimpath (no local absolute
# paths embedded) and version/commit metadata. A build error fails the run — no
# stale binary is ever packaged.
CGO_ENABLED=0 GOOS="$GOOS_T" GOARCH="$GOARCH_T" \
  go build -trimpath -ldflags "$LDFLAGS" -o "$ART_DIR/nexus" ./cmd/nexus
CGO_ENABLED=0 GOOS="$GOOS_T" GOARCH="$GOARCH_T" \
  go build -trimpath -ldflags "$LDFLAGS" -o "$ART_DIR/nexus-backup" ./cmd/nexus-backup

# Minimum required documentation.
cp "$REPO_ROOT/docs/operations-runbook.md" "$ART_DIR/operations-runbook.md"

# Release manifest (relative names only; no absolute paths, no secrets).
MANIFEST="$ART_DIR/release-manifest.txt"
{
  echo "nexus-release-manifest"
  echo "format: 1"
  echo "name: nexus"
  echo "version: ${VERSION}"
  echo "commit: ${COMMIT}"
  echo "tree_state: ${TREE_STATE}"
  echo "go_version: ${GO_VERSION}"
  echo "goos: ${GOOS_T}"
  echo "goarch: ${GOARCH_T}"
  echo "built_at: ${BUILT_AT}"
  echo "artifacts:"
  echo "  nexus"
  echo "  nexus-backup"
  echo "  operations-runbook.md"
  echo "  release-manifest.txt"
} > "$MANIFEST"

# Checksums over every produced file (relative paths, stable order).
(
  cd "$DIST"
  find "nexus-${VERSION}-${GOOS_T}-${GOARCH_T}" -type f -print0 \
    | sort -z \
    | xargs -0 sha256sum > SHA256SUMS
)

echo "release: artifacts in ${ART_DIR#$REPO_ROOT/}"
echo "release: checksums in ${DIST#$REPO_ROOT/}/SHA256SUMS"
if [ "$TREE_STATE" != "clean" ]; then
  echo "release: WARNING — built from a ${TREE_STATE} tree; not an official release" >&2
fi