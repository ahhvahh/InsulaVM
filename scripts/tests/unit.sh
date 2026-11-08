#!/usr/bin/env bash
# Unit tests do not require KVM/Libvirt.
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/../.." && pwd)"
cd "$ROOT"
bash -n scripts/tests/unit.sh scripts/tests/integration.sh scripts/tests/guest/verify.sh scripts/tests/guest/loop.sh scripts/tests/guest/prepare-live.sh
go vet ./...
go test -race ./...
TMP="$(mktemp -d)"
trap 'rm -rf -- "$TMP"' EXIT
go build -o "$TMP/insula" .
"$TMP/insula" help >/dev/null
echo "INSULA_UNIT_OK"
