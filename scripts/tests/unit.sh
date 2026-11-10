#!/usr/bin/env bash
# Suite sem KVM/Libvirt: o teste de VMs reais e feito nos workflows integrais de PR.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "$0")/../.." && pwd)"
cd "$ROOT"

bash -n scripts/baixar-debian.sh scripts/tests/test-baixar-debian.sh \
  scripts/preparar-deb13-abioma-tests.sh \
  scripts/tests/manual-debian-mini-iso.sh \
  scripts/tests/integration.sh \
  scripts/tests/alpine-iso.sh \
  scripts/tests/integral-2-zip.sh \
  scripts/tests/integral-3-iso-persistence.sh \
  scripts/tests/guest/verify.sh \
  scripts/tests/guest/loop.sh \
  scripts/tests/guest/prepare-live.sh \
  scripts/tests/guest/ignore-shutdown.sh

bash scripts/tests/test-baixar-debian.sh
bash scripts/tests/manual-debian-mini-iso.sh help >/dev/null
bash scripts/preparar-deb13-abioma-tests.sh help >/dev/null

go vet ./...
TMP="$(mktemp -d)"
trap 'rm -rf -- "$TMP"' EXIT

go test -count=1 ./...
go test -race -count=1 -coverprofile="$TMP/coverage.out" ./...
go tool cover -func="$TMP/coverage.out" | tail -n 1
# Show zero-coverage functions so new tests target unexercised behavior.
go tool cover -func="$TMP/coverage.out" | grep -E "[[:space:]]0[.]0%" | head -n 120 || true

# Executar apenas os contratos, e compilar todos os testes marcados 'integration'
# sem criar VMs, instalar ISO ou transferir arquivos no runner unitario.
go test -tags integration -count=1 -run '^(TestAlpineISO(XMLContract|TranscriptContract)|TestIntegral2ZipFixtureContract|TestIntegral3MarkerContract)$' ./tests/integration
go test -tags integration -run '^$' ./tests/integration

go build -o "$TMP/insula" .
"$TMP/insula" help >/dev/null
echo "INSULA_UNIT_OK"
