#!/usr/bin/env bash
# Real KVM integration suite; run on a dedicated Debian host.
# Requires KVM and either a prepared Debian 13 QCOW2 or a Debian 13 amd64 installer ISO.
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/../.." && pwd)"
CONFIG="$ROOT/config.yaml"
BASE_IMAGE=""
INSTALLER_ISO=""
LIVE_TEST=1
GO_API_TEST=1
while (($#)); do
  case "$1" in
    --config) CONFIG="$2"; shift 2 ;;
    --base-qcow2) BASE_IMAGE="$2"; shift 2 ;;
    --installer-iso) INSTALLER_ISO="$2"; shift 2 ;;
    --skip-live-iso) LIVE_TEST=0; shift ;;
    --skip-go-api) GO_API_TEST=0; shift ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
if [[ ! -f "$CONFIG" || "$CONFIG" != /* ||
      ( -z "$BASE_IMAGE" && -z "$INSTALLER_ISO" ) ||
      ( -n "$BASE_IMAGE" && ( "$BASE_IMAGE" != /* || ! -f "$BASE_IMAGE" ) ) ||
      ( -n "$INSTALLER_ISO" && ( "$INSTALLER_ISO" != /* || ! -f "$INSTALLER_ISO" ) ) ]]; then
  echo "Usage: bash scripts/tests/integration.sh --config /absolute/config.yaml (--base-qcow2 /absolute/debian13.qcow2 | --installer-iso /absolute/debian13.iso) [--skip-live-iso] [--skip-go-api]" >&2
  exit 2
fi
for tool in virsh qemu-img ssh; do
  command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 2; }
done
if [[ -n "$INSTALLER_ISO" ]]; then
  for tool in virt-install guestmount guestunmount openssl; do command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 2; }; done
fi
[[ -c /dev/kvm ]] || { echo "/dev/kvm unavailable" >&2; exit 2; }
cd "$ROOT"
TMP="$(mktemp -d)"
VM_IDS=""
cleanup() {
  local rc=$?
  trap - EXIT
  for id in $VM_IDS; do
    "$TMP/insula" vm stop -config "$CONFIG" -id "$id" -grace-timeout 5s -force-after-timeout=true >/dev/null 2>&1 || true
  done
  rm -rf -- "$TMP"
  exit "$rc"
}
trap cleanup EXIT
go build -o "$TMP/insula" .
insula() {
  local group="$1" sub="$2"
  shift 2
  "$TMP/insula" "$group" "$sub" -config "$CONFIG" "$@"
}
extract_id() {
  local id
  id="$(printf '%s\n' "$1" | grep -oE 'id=[0-9a-f-]{36}' | head -n 1 | cut -d= -f2)"
  [[ -n "$id" ]] || { echo "missing UUID from VM start output" >&2; exit 1; }
  printf '%s\n' "$id"
}
tag="it-$(date +%s)-$RANDOM"
base="$tag-base"
frozen="$tag-frozen"
clone="$tag-clone"
if [[ -z "$BASE_IMAGE" ]]; then
  echo "[integration] install Debian 13 from ISO and create the reusable base profile"
  output="$(insula profile create-from-iso -name "$base" -iso "$INSTALLER_ISO" -memory-mib 2048 -vcpus 2 -disk-gb 12 -install-timeout 90m)"
  echo "$output"
  grep -q 'PROFILE ISO OK' <<<"$output"
  BASE_IMAGE="$(sed -nE 's/^PROFILE ISO OK: name=[^ ]+ image=(.*) sha256=[0-9a-f]{64}$/\1/p' <<<"$output")"
  [[ -s "$BASE_IMAGE" ]] || { echo "[integration] no reusable QCOW2 from installer ISO" >&2; exit 1; }
else
  echo "[integration] register prepared Debian QCOW2"
  insula profile register -name "$base" -image "$BASE_IMAGE" -os debian -memory-mib 2048 -vcpus 2 -disk-gb 8
fi
insula profile clone -source "$base" -name "$clone"
echo "[integration] boot clone and upload guest verification script"
output="$(insula vm start -profile "$clone")"
vmid="$(extract_id "$output")"
VM_IDS="$VM_IDS $vmid"
mkdir -p "$TMP/payload"
echo integration > "$TMP/payload/payload.txt"
tar -czf "$TMP/payload.tar.gz" -C "$TMP/payload" .
output="$(insula vm deploy -id "$vmid" -source "$TMP/payload.tar.gz" -script "$ROOT/scripts/tests/guest/verify.sh" -extract)"
echo "$output"
grep -q INSULA_TEST_OK <<<"$output"
echo "[integration] infinite loop with 5-second sleeps and shutdown escalation"
insula vm import -id "$vmid" -source "$ROOT/scripts/tests/guest/loop.sh"
output="$(insula vm exec -id "$vmid" -command "nohup bash loop.sh >/tmp/insula-test-loop.log 2>&1 </dev/null & echo INSULA_LOOP_LAUNCHED")"
grep -q INSULA_LOOP_LAUNCHED <<<"$output"
output="$(insula vm exec -id "$vmid" -command "test -s /tmp/insula-test-loop.log && grep -q INSULA_LOOP_STARTED /tmp/insula-test-loop.log && echo INSULA_LOOP_READY")"
grep -q INSULA_LOOP_READY <<<"$output"
insula vm stop -id "$vmid" -grace-timeout 30s -force-after-timeout=true
insula vm status -id "$vmid" | grep -q 'status_libvirt=absent'
echo "[integration] force shutdown: guest ignores ACPI and runs an endless loop"
output="$(insula vm start -profile "$clone")"
forceid="$(extract_id "$output")"
VM_IDS="$VM_IDS $forceid"
output="$(insula vm deploy -id "$forceid" -source "$TMP/payload.tar.gz" -script "$ROOT/scripts/tests/guest/ignore-shutdown.sh" -extract -sudo)"
echo "$output"
grep -q INSULA_IGNORE_SHUTDOWN_OK <<<"$output"
insula vm import -id "$forceid" -source "$ROOT/scripts/tests/guest/loop.sh"
output="$(insula vm exec -id "$forceid" -command "nohup bash loop.sh >/tmp/insula-test-loop.log 2>&1 </dev/null & echo INSULA_LOOP_LAUNCHED")"
grep -q INSULA_LOOP_LAUNCHED <<<"$output"
output="$(insula vm exec -id "$forceid" -command "test -s /tmp/insula-test-loop.log && grep -q INSULA_LOOP_STARTED /tmp/insula-test-loop.log && echo INSULA_LOOP_READY")"
grep -q INSULA_LOOP_READY <<<"$output"
output="$(insula vm exec -id "$forceid" -command "echo INSULA_FORCE_GUEST_READY")"
grep -q INSULA_FORCE_GUEST_READY <<<"$output"
force_begin="$SECONDS"
output="$(insula vm stop -id "$forceid" -grace-timeout 30s -force-after-timeout=true)"
echo "$output"
force_elapsed="$((SECONDS-force_begin))"
if ((force_elapsed < 29)); then
  echo "[integration] hard destroy occurred before the 30s grace period: elapsed=$force_elapsed" >&2
  exit 1
fi
grep -q 'forced=true' <<<"$output"
grep -q 'timed_out=true' <<<"$output"
insula vm status -id "$forceid" | grep -q 'status_libvirt=absent'
echo INSULA_FORCE_SHUTDOWN_OK
echo "[integration] freeze a second VM into a named profile"
output="$(insula vm start -profile "$base")"
sourceid="$(extract_id "$output")"
VM_IDS="$VM_IDS $sourceid"
if [[ "$LIVE_TEST" == 1 ]]; then
  echo "[integration] install live-boot into disposable Debian guest"
  output="$(insula vm deploy -id "$sourceid" -source "$TMP/payload.tar.gz" -script "$ROOT/scripts/tests/guest/prepare-live.sh" -extract -sudo -command-timeout 30m)"
  echo "$output"
  grep -q INSULA_LIVE_BOOT_READY <<<"$output"
fi
insula vm freeze -id "$sourceid" -name "$frozen"
echo "[integration] clone frozen profile and verify remote commands"
output="$(insula vm start -profile "$frozen")"
cloneid="$(extract_id "$output")"
VM_IDS="$VM_IDS $cloneid"
output="$(insula vm exec -id "$cloneid" -command "echo INSULA_CLONE_OK")"
grep -q INSULA_CLONE_OK <<<"$output"
insula vm stop -id "$cloneid" -grace-timeout 30s -force-after-timeout=true
if [[ "$LIVE_TEST" == 1 ]]; then
  echo "[integration] export ISO and boot it as an SSH-ready guest"
  output="$(insula profile export-iso -name "$frozen")"
  iso_path="$(printf '%s\n' "$output" | sed -n 's/^LIVE ISO OK: //p')"
  [[ -s "$iso_path" ]] || { echo "ISO missing" >&2; exit 1; }
  output="$(insula vm start-iso -profile "$frozen" -iso "$iso_path")"
  isoid="$(extract_id "$output")"
  VM_IDS="$VM_IDS $isoid"
  output="$(insula vm deploy -id "$isoid" -source "$TMP/payload.tar.gz" -script "$ROOT/scripts/tests/guest/verify.sh" -extract)"
  grep -q INSULA_TEST_OK <<<"$output"
  insula vm stop -id "$isoid" -grace-timeout 30s -force-after-timeout=true
fi
if [[ "$GO_API_TEST" == 1 ]]; then
  echo "[integration] public Go Engine API end-to-end"
  INSULA_TEST_CONFIG="$CONFIG" INSULA_TEST_QCOW2="$BASE_IMAGE" INSULA_TEST_LIVE_ISO="$LIVE_TEST" INSULA_TEST_INSTALLER_ISO="$INSTALLER_ISO" \
    go test -tags integration -count=1 -timeout 150m -v ./tests/integration
fi
echo INSULA_INTEGRATION_OK
echo "Profiles kept for inspection: $base $clone $frozen"
