#!/usr/bin/env bash
# Unit tests do not require KVM/Libvirt.
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/../.." && pwd)"
cd "$ROOT"
bash -n scripts/tests/unit.sh scripts/tests/integration.sh scripts/tests/guest/verify.sh scripts/tests/guest/loop.sh scripts/tests/guest/prepare-live.sh scripts/tests/guest/ignore-shutdown.sh
go vet ./...
TMP="$(mktemp -d)"
trap 'rm -rf -- "$TMP"' EXIT
go test -race -coverprofile="$TMP/coverage.out" ./...
go tool cover -func="$TMP/coverage.out" | tail -n 1
# Compile KVM-only integration tests, do not execute on ordinary CI runners.
go test -tags integration -run '^
go build -o "$TMP/insula" .
"$TMP/insula" help >/dev/null
echo "INSULA_UNIT_OK"
 ./tests/integration
TMP="$(mktemp -d)"
trap 'rm -rf -- "$TMP"' EXIT
go build -o "$TMP/insula" .
"$TMP/insula" help >/dev/null
echo "INSULA_UNIT_OK"
