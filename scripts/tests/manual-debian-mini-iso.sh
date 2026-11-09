#!/usr/bin/env bash
# Manual KVM test: download Debian 13 mini.iso -> install -> open SSH shell
# -> freeze the prepared VM -> export Live ISO -> boot and verify.
set -Eeuo pipefail
umask 077

ROOT="$(cd "$(dirname "$0")/../.." && pwd -P)"
CONFIG="$ROOT/config.yaml"
WORKDIR="$HOME/.local/state/insulavm/debian-mini"
PROFILE="debian13-mini-base"
FROZEN="debian13-mini-preparado"
YES=0
ACTION="help"
if (($#)); then ACTION="$1"; shift; fi

while (($#)); do
  case "$1" in
    --config) CONFIG="$2"; shift 2 ;;
    --workdir) WORKDIR="$2"; shift 2 ;;
    --profile) PROFILE="$2"; shift 2 ;;
    --frozen) FROZEN="$2"; shift 2 ;;
    --yes) YES=1; shift ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done
case "$ACTION" in
  help|download|install|start|open|status|export|verify|all) ;;
  *) echo "Unknown action: $ACTION" >&2; exit 2 ;;
esac
if [[ "$ACTION" == help ]]; then
  cat <<'USAGE'
Manual Debian mini.iso integration test:
  bash scripts/tests/manual-debian-mini-iso.sh all [options]

Or run one phase at a time:
  download  Download official Debian 13 amd64 mini.iso and verify SHA256
  install   Install Debian via Preseed into a reusable QCOW2 profile
  start     Start the VM from that profile and save its UUID
  open      Enter the interactive SSH shell; exit keeps VM running
  status    Inspect the VM, profiles and exported ISO
  export    Confirm, freeze the VM, and generate a bootable Live ISO
  verify    Boot the exported ISO, execute a command, stop the test VM
Options: --config FILE --workdir DIR --profile NAME --frozen NAME --yes

WARNING: export stops and REMOVES the source VM after successfully
saving its changes into a new QCOW2 profile. --yes bypasses confirmation.
Use a dedicated Linux KVM host with Internet access.
USAGE
  exit 0
fi

for name in "$PROFILE" "$FROZEN"; do
  [[ "$name" =~ ^[A-Za-z0-9][A-Za-z0-9_-]{0,60}$ ]] || { echo "Invalid profile name: $name" >&2; exit 2; }
done
[[ "$PROFILE" != "$FROZEN" ]] || { echo "Profile names must differ" >&2; exit 2; }
mkdir -p "$WORKDIR"
WORKDIR="$(cd "$WORKDIR" && pwd -P)"
if [[ "$CONFIG" != /* ]]; then CONFIG="$(pwd -P)/$CONFIG"; fi
ISO="$WORKDIR/mini.iso"
ID_FILE="$WORKDIR/vm-id"
EXPORTED_FILE="$WORKDIR/exported-iso"
BIN="$WORKDIR/insula"
SOURCE="https://deb.debian.org/debian/dists/trixie/main/installer-amd64/current/images"

need() {
  for tool in "$@"; do
    command -v "$tool" >/dev/null || { echo "Missing host tool: $tool" >&2; exit 2; }
  done
}
kvm() {
  [[ -c /dev/kvm ]] || { echo "/dev/kvm missing; run on a KVM host" >&2; exit 2; }
  need virsh qemu-img ssh
}
build() {
  [[ -f "$CONFIG" ]] || { echo "Missing config file: $CONFIG" >&2; exit 2; }
  need go
  (cd "$ROOT" && go build -o "$BIN" .)
  "$BIN" config check -config "$CONFIG" >&2
}
insula() {
  group="$1"; sub="$2"; shift 2
  "$BIN" "$group" "$sub" -config "$CONFIG" "$@"
}
read_id() {
  [[ -s "$ID_FILE" ]] || { echo "No saved VM ID; run start first" >&2; return 1; }
  VM_ID="$(cat "$ID_FILE")"
  [[ "$VM_ID" =~ ^[a-f0-9-]{36}$ ]] || { echo "Invalid saved VM ID" >&2; return 1; }
}
download() {
  need curl awk sha256sum
  echo "[mini] Downloading official checksum information" >&2
  sums="$(curl -fsSL --retry 3 --proto '=https' "$SOURCE/SHA256SUMS")"
  expected="$(awk '$2=="./netboot/mini.iso" {print $1}' <<<"$sums")"
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || { echo "Debian checksum unavailable" >&2; return 1; }
  [[ ! -L "$ISO" ]] || { echo "Refusing symlink cache path" >&2; return 1; }
  if [[ -f "$ISO" ]]; then
    echo "$expected  $ISO" | sha256sum -c -
    echo "[mini] Cached ISO verified: $ISO"
    return
  fi
  tmp="$(mktemp "$WORKDIR/.mini.XXXXXXXX.part")"
  if ! curl -fsSL --retry 3 --proto '=https' -o "$tmp" "$SOURCE/netboot/mini.iso"; then
    rm -f "$tmp"; return 1
  fi
  if ! echo "$expected  $tmp" | sha256sum -c -; then
    rm -f "$tmp"; return 1
  fi
  mv "$tmp" "$ISO"
  echo "[mini] Downloaded and verified: $ISO"
}
install() {
  kvm
  [[ -f "$ISO" && ! -L "$ISO" ]] || { echo "Run download first" >&2; return 1; }
  need virt-install openssl guestmount guestunmount
  build
  if insula profile show -name "$PROFILE" >/dev/null 2>&1; then
    echo "[mini] Profile already exists: $PROFILE"
    return
  fi
  insula profile create-from-iso -name "$PROFILE" -iso "$ISO" \
    -memory-mib 2048 -vcpus 2 -disk-gb 12 -install-timeout 90m
}
start() {
  kvm; build
  if [[ -s "$ID_FILE" ]]; then
    read_id
    if insula vm status -id "$VM_ID" | grep -q '^status_libvirt=running$'; then
      echo "[mini] VM already running: $VM_ID"; return
    fi
    echo "Saved VM is no longer running; inspect status before restarting" >&2; return 1
  fi
  out="$(insula vm start -profile "$PROFILE")"
  echo "$out"
  id="$(sed -nE 's/^VM pronta: id=([a-f0-9-]{36}) .*/\1/p' <<<"$out")"
  [[ "$id" =~ ^[a-f0-9-]{36}$ ]] || { echo "VM started but ID not parsed; inspect vm list" >&2; return 1; }
  printf '%s\n' "$id" > "$ID_FILE"
}
open() {
  kvm; build; read_id
  echo "[mini] Customize VM $VM_ID; exit the SSH shell when finished."
  echo "[mini] Closing the shell does NOT stop or freeze the VM."
  insula vm shell -id "$VM_ID"
}
status() {
  build
  echo "Source ISO: $ISO"
  insula profile show -name "$PROFILE" || true
  if [[ -s "$ID_FILE" ]]; then
    read_id && insula vm status -id "$VM_ID" || true
  fi
  insula profile show -name "$FROZEN" || true
  if [[ -s "$EXPORTED_FILE" ]]; then echo "Exported ISO: $(cat "$EXPORTED_FILE")"; fi
}
confirm() {
  if [[ "$YES" == 1 ]]; then return; fi
  [[ -t 0 ]] || { echo "Interactive confirmation required; use --yes to bypass" >&2; return 1; }
  echo "WARNING: export freezes the guest and REMOVES the source VM."
  read -r -p "Type EXPORTAR to proceed: " answer
  [[ "$answer" == EXPORTAR ]] || { echo "Export canceled; VM left running"; return 1; }
}
export_iso() {
  kvm; build; read_id
  if insula profile show -name "$FROZEN" >/dev/null 2>&1; then
    echo "Frozen profile already exists; choose a different --frozen name" >&2
    return 1
  fi
  confirm || return 1
  out="$(insula vm export-iso -id "$VM_ID" -name "$FROZEN" \
    -prepare-live-boot -command-timeout 30m -shutdown-timeout 3m)"
  echo "$out"
  path="$(sed -nE 's/^VM ISO OK: profile=[^ ]+ iso=(.*) source=[a-f0-9-]{36}$/\1/p' <<<"$out")"
  [[ -s "$path" ]] || { echo "No bootable ISO returned: $path" >&2; return 1; }
  printf '%s\n' "$path" > "$EXPORTED_FILE"
  rm -f "$ID_FILE"
  echo "[mini] Saved permanent profile: $FROZEN"
  echo "[mini] Created Live ISO: $path"
}
verify() {
  kvm; build
  [[ -s "$EXPORTED_FILE" ]] || { echo "Export first" >&2; return 1; }
  path="$(cat "$EXPORTED_FILE")"
  [[ -s "$path" ]] || { echo "ISO file missing: $path" >&2; return 1; }
  out="$(insula vm start-iso -profile "$FROZEN" -iso "$path")"
  echo "$out"
  id="$(sed -nE 's/^VM LIVE pronta: id=([a-f0-9-]{36}) .*/\1/p' <<<"$out")"
  [[ "$id" =~ ^[a-f0-9-]{36}$ ]] || { echo "Live VM ID not parsed" >&2; return 1; }
  rc=0
  if ! output="$(insula vm exec -id "$id" -command 'test -r /etc/debian_version && echo INSULA_MINI_LIVE_OK')"; then
    rc=1
  fi
  printf '%s\n' "$output"
  [[ "$output" == *INSULA_MINI_LIVE_OK* ]] || rc=1
  insula vm stop -id "$id" -grace-timeout 30s -force-after-timeout=true || rc=1
  [[ "$rc" == 0 ]] && echo "INSULA_DEBIAN_MINI_MANUAL_OK"
  return "$rc"
}
case "$ACTION" in
  download) download ;;
  install) install ;;
  start) start ;;
  open) open ;;
  status) status ;;
  export) export_iso ;;
  verify) verify ;;
  all) download; install; start; open; export_iso; verify ;;
esac
