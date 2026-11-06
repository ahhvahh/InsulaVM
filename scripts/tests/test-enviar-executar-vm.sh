#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "$0")/../.." && pwd -P)"
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT
uuid=01a1b24e-88cb-7464-9acc-06a08cf727fd
touch "$tmp/config.yaml"
printf 'data\n' > "$tmp/pacote.tar.gz"
printf 'script\n' > "$tmp/app.sh"
cat > "$tmp/mock-insula" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
case "$2" in
  import) echo import >> "$MOCK_EVENTS" ;;
  exec)
    echo exec >> "$MOCK_EVENTS"
    for arg in "$@"; do
      [[ "$arg" != *FAIL_IN_VM* ]] || exit 23
      [[ "$arg" != '-sudo' ]] || echo sudo >> "$MOCK_SUDO"
      [[ "$arg" != *'tar -xzf'* ]] || printf '%s\n' "$arg" >> "$MOCK_EXTRACT"
    done ;;
  shell) echo shell >> "$MOCK_EVENTS" ;;
  *) exit 70 ;;
esac
MOCK
chmod +x "$tmp/mock-insula"
export MOCK_EVENTS="$tmp/events" MOCK_SUDO="$tmp/sudo" MOCK_EXTRACT="$tmp/extract"
bash -n "$root/scripts/enviar-executar-vm.sh"
cmd=(bash "$root/scripts/enviar-executar-vm.sh" --insula "$tmp/mock-insula" --config "$tmp/config.yaml" --id "$uuid")
"${cmd[@]}" --source "$tmp/pacote.tar.gz" --extract --command 'ls -lah' --command 'echo OK' --sudo --shell
actual="$(paste -sd, "$MOCK_EVENTS")"
[[ "$actual" == 'import,exec,exec,exec,shell' ]] || { echo "bad sequence: $actual" >&2; exit 1; }
[[ "$(wc -l < "$MOCK_SUDO")" == 2 ]]
[[ "$(cat "$MOCK_EXTRACT")" == "tar -xzf 'pacote.tar.gz'" ]]
: > "$MOCK_EVENTS"
if "${cmd[@]}" --source "$tmp/app.sh" --command FAIL_IN_VM --command 'echo unreachable' --shell; then echo 'error ignored' >&2; exit 1; fi
[[ "$(paste -sd, "$MOCK_EVENTS")" == 'import,exec' ]]
: > "$MOCK_EVENTS"
if "${cmd[@]}" --source "$tmp/missing.tar.gz"; then exit 1; fi
if "${cmd[@]}" --source "$tmp/app.sh" --extract; then exit 1; fi
[[ ! -s "$MOCK_EVENTS" ]]
echo 'OK: upload, extract, sequence, sudo, early exit'
