#!/usr/bin/env bash
# Mandatory real KVM smoke test: unmodified Alpine Virtual ISO -> ttyS0 -> guest command -> poweroff.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "$0")/../.." && pwd)"
CONFIG="$ROOT/config.yaml"
ISO=""
VERSION="3.24.4"
ARCH="x86_64"
BASE_URL="https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/$ARCH"
FILENAME="alpine-virt-$VERSION-$ARCH.iso"

usage() {
  echo "Usage: bash scripts/tests/alpine-iso.sh [--config /absolute/config.yaml] [--iso /absolute/alpine-virt-3.24.4-x86_64.iso]"
}

while (($#)); do
  case "$1" in
    --config) [[ $# -ge 2 ]] || { usage >&2; exit 2; }; CONFIG="$2"; shift 2 ;;
    --iso) [[ $# -ge 2 ]] || { usage >&2; exit 2; }; ISO="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done

[[ "$CONFIG" == /* && -f "$CONFIG" ]] || {
  echo "[alpine] --config must be an absolute existing file" >&2
  exit 2
}
[[ -c /dev/kvm ]] || { echo "[alpine] /dev/kvm unavailable: real KVM test cannot pass" >&2; exit 1; }
for tool in go virsh; do
  command -v "$tool" >/dev/null || { echo "[alpine] missing $tool" >&2; exit 2; }
done

TEMP=""
cleanup() {
  if [[ -n "$TEMP" && -d "$TEMP" ]]; then rm -rf -- "$TEMP"; fi
}
trap cleanup EXIT

if [[ -z "$ISO" ]]; then
  command -v curl >/dev/null || { echo "[alpine] curl is required to download the ISO" >&2; exit 2; }
  command -v sha256sum >/dev/null || { echo "[alpine] sha256sum is required" >&2; exit 2; }
  # The QEMU service account must be able to traverse this directory and
  # read the ISO. Keep the serial transcript and configuration elsewhere.
  TEMP="$(mktemp -d "/var/tmp/insula-alpine-iso.XXXXXXXX")"
  chmod 0755 "$TEMP"
  ISO="$TEMP/$FILENAME"
  echo "[alpine] downloading official Alpine Virtual $VERSION ISO"
  curl --fail --location --silent --show-error --retry 3 \
    "$BASE_URL/$FILENAME" --output "$ISO"
  curl --fail --location --silent --show-error --retry 3 \
    "$BASE_URL/$FILENAME.sha256" --output "$TEMP/$FILENAME.sha256"
  expected="$(awk 'NR == 1 { print $1 }' "$TEMP/$FILENAME.sha256")"
  [[ "$expected" =~ ^[[:xdigit:]]{64}$ ]] || { echo "[alpine] invalid official SHA256 manifest" >&2; exit 1; }
  actual="$(sha256sum "$ISO" | awk '{ print $1 }')"
  [[ "$actual" == "$expected" ]] || { echo "[alpine] ISO SHA256 mismatch" >&2; exit 1; }
  chmod 0644 "$ISO"
  echo "[alpine] ISO checksum verified: $actual"
else
  [[ "$ISO" == /* && -s "$ISO" ]] || { echo "[alpine] --iso must be an absolute nonempty ISO path" >&2; exit 2; }
  echo "[alpine] using supplied local ISO: $ISO (trusted operator input)"
fi

cd "$ROOT"
echo "[alpine] real boot: 1 vCPU, 500 MiB RAM, serial guest command, guest poweroff"
# Capture output so a skipped or non-executed Go test cannot appear successful.
set +e
output="$(INSULA_TEST_CONFIG="$CONFIG" INSULA_TEST_ALPINE_ISO="$ISO" \
  INSULA_TEST_ALPINE_VERSION="$VERSION" \
  go test -tags integration -count=1 -timeout 6m -run '^TestAlpineISOConsoleLifecycle$' -v ./tests/integration 2>&1)"
rc=$?
set -e
printf '%s\n' "$output"
if ((rc != 0)) || ! grep -q 'INSULA_ALPINE_ISO_OK:' <<<"$output"; then
  echo "[alpine] mandatory boot/command/shutdown acceptance FAILED" >&2
  exit 1
fi
echo INSULA_ALPINE_INTEGRATION_OK
