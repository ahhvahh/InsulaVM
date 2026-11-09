#!/usr/bin/env bash
# Real InsulaVM E2E: Debian Cloud QCOW2 -> SSH -> ZIP ImportToVM/CLI -> commands -> clean stop.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "$0")/../.." && pwd)"
STATE="$ROOT/data/integral-2-ci"
CONFIG=""
IMAGE=""
BASE_URL="https://cloud.debian.org/images/cloud/trixie/latest"
IMAGE_NAME="debian-13-generic-amd64.qcow2"

usage() {
  cat <<'USAGE'
Usage: bash scripts/tests/integral-2-zip.sh [--config /absolute/config.yaml] [--qcow2 /absolute/debian-cloud.qcow2]
  By default uses an isolated test config and downloads a verified Debian 13 Cloud QCOW2.
  Requires real Linux KVM/Libvirt with an active DHCP network and Go.
USAGE
}

while (($#)); do
  case "$1" in
    --config) [[ $# -ge 2 ]] || { usage >&2; exit 2; }; CONFIG="$2"; shift 2 ;;
    --qcow2) [[ $# -ge 2 ]] || { usage >&2; exit 2; }; IMAGE="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument $1" >&2; usage >&2; exit 2 ;;
  esac
done

for tool in go virsh qemu-img genisoimage; do
  command -v "$tool" >/dev/null || { echo "[integral-2] missing $tool" >&2; exit 2; }
done
[[ -c /dev/kvm ]] || { echo "[integral-2] /dev/kvm missing: mandatory KVM test fails" >&2; exit 1; }
if [[ -n "$CONFIG" && ( "$CONFIG" != /* || ! -f "$CONFIG" ) ]]; then
  echo "[integral-2] --config must be an absolute existing path" >&2
  exit 2
fi
if [[ -n "$IMAGE" && ( "$IMAGE" != /* || ! -s "$IMAGE" ) ]]; then
  echo "[integral-2] --qcow2 must be an absolute existing regular file" >&2
  exit 2
fi

mkdir -p "$STATE"
if [[ -z "$CONFIG" ]]; then
  uid="$(id -u libvirt-qemu 2>/dev/null || true)"
  [[ "$uid" =~ ^[0-9]+$ && "$uid" -gt 0 ]] || {
    echo "[integral-2] libvirt-qemu UID unavailable; configure --config explicitly" >&2
    exit 2
  }
  CONFIG="$STATE/config.yaml"
  mkdir -p "$STATE/state"
  cat > "$CONFIG" <<YAML
storage:
  root: $STATE/state
  profiles: profiles
  runs: runs
  logs: logs
  database: insula.db
guest:
  destination: /tmp/insulavm/workspace
virtualization:
  provider: kvm
  libvirt_uri: qemu:///system
  network: default
  default_disk_gb: 8
  qemu_uid: $uid
ssh:
  user: insula
  connect_timeout_seconds: 60
  sudo_mode: disabled
YAML
  chmod 0600 "$CONFIG"
fi

if [[ -z "$IMAGE" ]]; then
  for tool in curl sha512sum; do
    command -v "$tool" >/dev/null || { echo "[integral-2] missing $tool" >&2; exit 2; }
  done
  download="$STATE/download"
  mkdir -p "$download"
  IMAGE="$download/$IMAGE_NAME"
  if [[ ! -s "$IMAGE" ]]; then
    echo "[integral-2] downloading official Debian 13 generic cloud image"
    curl -fSL --retry 3 --connect-timeout 30 \
      "$BASE_URL/$IMAGE_NAME" -o "$IMAGE"
  fi
  curl -fSL --retry 3 --connect-timeout 30 \
    "$BASE_URL/SHA512SUMS" -o "$download/SHA512SUMS"
  (
    cd "$download"
    # Verify only the intended file; do not trust other filenames in the manifest.
    awk -v name="$IMAGE_NAME" '$2 == name && $1 ~ /^[0-9a-fA-F]+$/ { print }' SHA512SUMS > verified-entry
    [[ $(wc -l < verified-entry) == 1 ]] || {
      echo "[integral-2] SHA512SUMS lacks a unique image entry" >&2; exit 1;
    }
    sha512sum --check --strict verified-entry
  )
fi

echo "[integral-2] validating Libvirt network"
virsh -c qemu:///system net-info default | grep -Eq 'Active:[[:space:]]+yes' || {
  echo "[integral-2] default Libvirt DHCP network is not active" >&2
  exit 1
}
cd "$ROOT"
BIN="$STATE/insula-integral-2"
go build -o "$BIN" .
echo "[integral-2] running required real Go+CLI ZIP/commands/clean-stop test"
set +e
output="$(INSULA_IT2_CONFIG="$CONFIG" INSULA_IT2_QCOW2="$IMAGE" INSULA_IT2_CLI="$BIN" \
  go test -tags integration -count=1 -timeout 11m -run '^TestIntegral2ZipTransferAndCommand$' -v ./tests/integration 2>&1)"
rc=$?
set -e
printf '%s\n' "$output"
if ((rc != 0)) || ! grep -q 'INSULA_INTEGRAL_2_OK:' <<<"$output"; then
  echo "[integral-2] FAILED: VM creation, ZIP upload, guest command, response, or graceful stop" >&2
  exit 1
fi
echo INSULA_INTEGRAL_2_CI_OK
