#!/usr/bin/env bash
# Integral 3: Debian mini ISO -> 10GiB install -> 500MiB VM ->
# /tmp/workspace -> freeze ISO -> second 500MiB VM -> same bytes.
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/../.." && pwd)"
STATE="$ROOT/data/integral-3-ci"
CONFIG=""
INSTALLER_ISO=""
DEBIAN_IMAGES_URL="https://deb.debian.org/debian/dists/trixie/main/installer-amd64/current/images"

usage() {
  cat <<'USAGE'
Usage: bash scripts/tests/integral-3-iso-persistence.sh [--config /absolute/config.yaml] [--installer-iso /absolute/mini.iso]
The installer requires 1024 MiB under InsulaVM's existing API. Both managed
VMs run at 500 MiB, 1 vCPU; the installed disk is exactly 10 GiB.
USAGE
}
while (($#)); do
  case "$1" in
    --config) (($# >= 2)) || { usage >&2; exit 2; }; CONFIG="$2"; shift 2 ;;
    --installer-iso) (($# >= 2)) || { usage >&2; exit 2; }; INSTALLER_ISO="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
done
[[ -c /dev/kvm ]] || { echo "[integral-3] KVM unavailable" >&2; exit 1; }
for tool in go virsh virt-install qemu-img guestmount guestunmount mksquashfs \
            grub-mkrescue lsinitramfs openssl genisoimage curl sha256sum awk; do
  command -v "$tool" >/dev/null || { echo "[integral-3] missing $tool" >&2; exit 2; }
done
mkdir -p "$STATE"
if [[ -z "$CONFIG" ]]; then
  uid="$(id -u libvirt-qemu 2>/dev/null || true)"
  [[ "$uid" =~ ^[0-9]+$ && "$uid" -gt 0 ]] || { echo "[integral-3] missing QEMU UID" >&2; exit 2; }
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
  destination: /tmp/workspace
virtualization:
  provider: kvm
  libvirt_uri: qemu:///system
  network: default
  default_disk_gb: 10
  qemu_uid: $uid
ssh:
  user: insula
  connect_timeout_seconds: 60
  sudo_mode: passwordless
YAML
  chmod 0600 "$CONFIG"
fi
[[ "$CONFIG" == /* && -f "$CONFIG" ]] || { echo "[integral-3] --config must be absolute" >&2; exit 2; }
if [[ -z "$INSTALLER_ISO" ]]; then
  download="$STATE/debian-installer"
  mkdir -p "$download/netboot"
  INSTALLER_ISO="$download/netboot/mini.iso"
  if [[ ! -s "$INSTALLER_ISO" ]]; then
    echo "[integral-3] downloading official Debian 13 mini.iso (about 64 MB)"
    curl -fSL --retry 3 --connect-timeout 25 \
      "$DEBIAN_IMAGES_URL/netboot/mini.iso" -o "$INSTALLER_ISO"
  fi
  curl -fSL --retry 3 --connect-timeout 25 \
    "$DEBIAN_IMAGES_URL/SHA256SUMS" -o "$download/SHA256SUMS"
  (
    cd "$download"
    awk '$2 == "./netboot/mini.iso" && length($1) == 64 &&
      $1 !~ /[^[:xdigit:]]/ {print}' SHA256SUMS > verified-entry
    [[ "$(wc -l < verified-entry)" == 1 ]] || { echo "[integral-3] missing unique ISO SHA" >&2; exit 1; }
    sha256sum --check --strict verified-entry
  )
else
  [[ "$INSTALLER_ISO" == /* && -f "$INSTALLER_ISO" && -s "$INSTALLER_ISO" ]] || {
    echo "[integral-3] --installer-iso must be absolute and nonempty" >&2; exit 2;
  }
fi
echo "[integral-3] inspecting ISO boot kernel/initrd paths (read-only)"
xorriso -indev "$INSTALLER_ISO" -find / -type f -print 2>&1 |
  grep -E 'linux|initrd|vmlinuz|install' | tail -n 45 || true
virsh -c qemu:///system net-info default | grep -Eq 'Active:[[:space:]]+yes' || {
  echo "[integral-3] Libvirt default network inactive" >&2; exit 1;
}
cd "$ROOT"
BIN="$STATE/insula-integral-3"
go build -o "$BIN" .
mkdir -p "$STATE/diagnostic"
echo "[integral-3] executing real ISO install/freeze/second ISO boot"
set +e
INSULA_IT3_CONFIG="$CONFIG" INSULA_IT3_INSTALLER_ISO="$INSTALLER_ISO" \
INSULA_IT3_CLI="$BIN" go test -tags integration -count=1 \
  -timeout 150m -run '^TestIntegral3ISOWorkspacePersistence$' \
  -v ./tests/integration 2>&1 | tee "$STATE/diagnostic/test-output.log"
rc=$?
set -e
if ((rc != 0)) || ! grep -q 'INSULA_INTEGRAL_3_OK:' "$STATE/diagnostic/test-output.log"; then
  echo "[integral-3] FAILED: ISO install, /tmp preservation, ISO boot or graceful shutdown" >&2
  exit 1
fi
echo INSULA_INTEGRAL_3_CI_OK
