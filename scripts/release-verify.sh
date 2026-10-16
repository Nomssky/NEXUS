#!/usr/bin/env bash
#
# NEXUS release artifact verifier (Production Hardening v4).
#
# Verifies a locally built release under dist/: the SHA-256 checksums, the
# manifest's required fields, and that the expected files are present. It does
# NOT execute the binaries and does NOT claim cryptographic authenticity —
# checksums detect accidental corruption, they do not authenticate a publisher.
#
# Usage:
#   scripts/release-verify.sh [version]
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

DIST="${DIST:-$REPO_ROOT/dist}"
VERSION="${1:-${VERSION:-}}"

if [ ! -d "$DIST" ]; then
  echo "verify: no dist/ directory at $DIST (run scripts/release.sh first)" >&2
  exit 1
fi

# Locate the artifact directory.
if [ -n "$VERSION" ]; then
  ART_DIR="$(find "$DIST" -maxdepth 1 -type d -name "nexus-${VERSION}-*" | head -1)"
else
  ART_DIR="$(find "$DIST" -maxdepth 1 -type d -name 'nexus-*' | sort | head -1)"
fi
if [ -z "$ART_DIR" ] || [ ! -d "$ART_DIR" ]; then
  echo "verify: no release artifact directory found in $DIST" >&2
  exit 1
fi
ART_NAME="$(basename "$ART_DIR")"
echo "verify: checking $ART_NAME"

# 1. Checksums.
if [ ! -f "$DIST/SHA256SUMS" ]; then
  echo "verify: missing SHA256SUMS" >&2
  exit 1
fi
if ! (cd "$DIST" && sha256sum -c SHA256SUMS >/dev/null); then
  echo "verify: checksum verification FAILED" >&2
  (cd "$DIST" && sha256sum -c SHA256SUMS) >&2 || true
  exit 1
fi
echo "verify: checksums OK"

# 2. Manifest required fields.
MANIFEST="$ART_DIR/release-manifest.txt"
if [ ! -f "$MANIFEST" ]; then
  echo "verify: missing release-manifest.txt" >&2
  exit 1
fi
for field in format name version commit tree_state goos goarch; do
  if ! grep -q "^${field}: " "$MANIFEST"; then
    echo "verify: manifest missing required field '${field}'" >&2
    exit 1
  fi
done
if ! grep -q "^format: 1$" "$MANIFEST"; then
  echo "verify: unsupported manifest format" >&2
  exit 1
fi
echo "verify: manifest OK"

# 3. Expected files present.
for f in nexus nexus-backup operations-runbook.md release-manifest.txt; do
  if [ ! -f "$ART_DIR/$f" ]; then
    echo "verify: expected artifact missing: $f" >&2
    exit 1
  fi
done
echo "verify: all expected files present"

echo "verify: OK — $ART_NAME verified (checksums + manifest + inventory)"
echo "verify: note — checksums detect corruption only; they do not authenticate the publisher"