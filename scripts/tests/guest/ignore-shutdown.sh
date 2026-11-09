#!/usr/bin/env bash
# Run ONLY in a disposable test guest. Prevent ordinary ACPI power-button
# shutdown so the 30-second hard-destroy escalation is exercised on KVM.
set -euo pipefail
test "$(id -u)" = 0
systemctl is-active --quiet systemd-logind
install -d -m 0755 /etc/systemd/logind.conf.d
cat > /etc/systemd/logind.conf.d/99-insula-test-ignore-power.conf <<'EOF'
[Login]
HandlePowerKey=ignore
HandlePowerKeyLongPress=ignore
EOF
systemctl restart systemd-logind
# Some guest setups route ACPI power events to systemd poweroff.target.
# Runtime masking is not persisted in a reusable profile.
systemctl mask --runtime poweroff.target
systemctl is-active --quiet systemd-logind
echo INSULA_IGNORE_SHUTDOWN_OK
