#!/usr/bin/env bash
#
# Build the loack core and every provider module for ONE platform, named for
# release publishing. The download half resolves a provider by composing this
# same name from (svc, version, os, arch):
#
#   loack-provider-<svc>_<version>_<os>_<arch>[.exe]
#
# The core (loack) and the all-in-one (loack-aio) are published the same way.
# Pure-Go (CGO disabled), so every target cross-compiles from any host.
#
# Env:
#   VERSION   required, e.g. v0.1.0 (the release tag)
#   GOOS      required, e.g. linux | darwin
#   GOARCH    required, e.g. amd64 | arm64
#   OUT       output dir (default: dist)
#
# Controllers must be vendored first (`make vendor`); each provider module
# replaces them to ../../<ctrl>.
set -euo pipefail

: "${VERSION:?set VERSION (e.g. v0.1.0)}"
: "${GOOS:?set GOOS}"
: "${GOARCH:?set GOARCH}"
GO="${GO:-go}"

# Resolve OUT to an absolute path so the per-provider builds (which run from the
# provider module dir) write to the right place regardless of cwd.
mkdir -p "${OUT:-dist}"
OUT="$(cd "${OUT:-dist}" && pwd)"
suffix="_${VERSION}_${GOOS}_${GOARCH}"
ext=""; [ "$GOOS" = "windows" ] && ext=".exe"
export CGO_ENABLED=0 GOOS GOARCH

echo "building loack (core, split) ${GOOS}/${GOARCH}"
$GO build -tags split -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${OUT}/loack${suffix}${ext}" ./cmd/loack

echo "building loack-aio (all-in-one) ${GOOS}/${GOARCH}"
$GO build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${OUT}/loack-aio${suffix}${ext}" ./cmd/loack

for d in providers/*/; do
  svc="$(basename "$d")"
  echo "building loack-provider-${svc} ${GOOS}/${GOARCH}"
  ( cd "$d" && CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
      $GO build -trimpath -ldflags "-s -w" \
      -o "${OUT}/loack-provider-${svc}${suffix}${ext}" . )
done

echo "--- ${OUT} (${GOOS}/${GOARCH}) ---"
ls -1 "${OUT}" | grep -- "${suffix}${ext}\$"
