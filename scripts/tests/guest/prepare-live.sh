#!/usr/bin/env bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends live-boot
update-initramfs -u
test -d /usr/share/initramfs-tools/scripts/live
echo INSULA_LIVE_BOOT_READY
