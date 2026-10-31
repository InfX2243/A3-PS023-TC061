#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GOOS=linux
GOARCH="${GOARCH:-amd64}"
export GOOS GOARCH CGO_ENABLED=0
mkdir -p dist
go build -trimpath -ldflags="-s -w" -o dist/vm-controller ./cmd/controller
go build -trimpath -ldflags="-s -w" -o dist/vm-worker-agent ./cmd/worker-agent
(
  cd dist
  sha256sum vm-controller vm-worker-agent > SHA256SUMS
)
printf 'Built Linux/%s artifacts in %s/dist\n' "$GOARCH" "$ROOT"
