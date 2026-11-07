#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "$0")/../.." && pwd -P)"
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT
uuid=01a1b24e-88cb-7464-9acc-06a08cf727fd
touch "$tmp/config.yaml"
printf 'data\n' > "$tmp/pacote.tar.gz"
printf 'script\n' > "$tmp/app.sh"
printf '#!/usr/bin/env bash\nset -e\necho post-upload\n' > "$tmp/pos-carga.sh"
printf 'error\n' > "$tmp/FAIL_IN_VM.sh"
printf 'error\n' > "$tmp/FAIL_SECOND_UPLOAD.sh"
cat > "$tmp/mock-insula" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
case "$2" in
  import)
    echo import >> "$MOCK_EVENTS"
    for arg in "$@"; do [[ "$arg" != *FAIL_SECOND_UPLOAD.sh* ]] || exit 18; done ;;
  exec)
    echo exec >> "$MOCK_EVENTS"
    for arg in "$@"; do
      [[ "$arg" != *FAIL_IN_VM* ]] || exit 23
      [[ "$arg" != '-sudo' ]] || echo sudo >> "$MOCK_SUDO"
      [[ "$arg" != *'tar -xzf'* ]] || printf '%s\n' "$arg" >> "$MOCK_EXTRACT"
      [[ "$arg" != *'bash -e --'* ]] || printf '%s\n' "$arg" >> "$MOCK_SCRIPT"
    done ;;
  shell) echo shell >> "$MOCK_EVENTS" ;;
  *) exit 70 ;;
esac
MOCK
chmod +x "$tmp/mock-insula"
export MOCK_EVENTS="$tmp/events" MOCK_SUDO="$tmp/sudo" MOCK_EXTRACT="$tmp/extract" MOCK_SCRIPT="$tmp/script"
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
# Envia pacote, extrai e depois envia/roda script de pos-carga.
: > "$MOCK_EVENTS"
: > "$MOCK_SUDO"
"${cmd[@]}" --source "$tmp/pacote.tar.gz" --extract --script "$tmp/pos-carga.sh" --command 'echo done' --sudo --shell
[[ "$(paste -sd, "$MOCK_EVENTS")" == 'import,exec,import,exec,exec,shell' ]] || { cat "$MOCK_EVENTS" >&2; exit 1; }
[[ "$(wc -l < "$MOCK_SUDO")" == 2 ]] || { echo 'sudo missing for script or command' >&2; exit 1; }
[[ "$(cat "$MOCK_SCRIPT")" == "bash -e -- './pos-carga.sh' 'pacote.tar.gz'" ]] || { cat "$MOCK_SCRIPT" >&2; exit 1; }

# Uma falha ao enviar o script nao executa script, comandos ou terminal.
: > "$MOCK_EVENTS"
if "${cmd[@]}" --source "$tmp/app.sh" --script "$tmp/FAIL_SECOND_UPLOAD.sh" --command 'echo never' --shell; then
  echo 'failed script upload was ignored' >&2; exit 1
fi
[[ "$(paste -sd, "$MOCK_EVENTS")" == 'import,import' ]] || { cat "$MOCK_EVENTS" >&2; exit 1; }

# Uma falha no script interrompe as etapas seguintes.
: > "$MOCK_EVENTS"
if "${cmd[@]}" --source "$tmp/app.sh" --script "$tmp/FAIL_IN_VM.sh" --command 'echo never' --shell; then
  echo 'failed post-upload script was ignored' >&2; exit 1
fi
[[ "$(paste -sd, "$MOCK_EVENTS")" == 'import,import,exec' ]] || { cat "$MOCK_EVENTS" >&2; exit 1; }

# Entrada invalida e nome conflitante falham antes de qualquer envio.
: > "$MOCK_EVENTS"
if "${cmd[@]}" --source "$tmp/app.sh" --script "$tmp/ausente.sh"; then exit 1; fi
if "${cmd[@]}" --source "$tmp/app.sh" --script "$tmp/app.sh"; then exit 1; fi
[[ ! -s "$MOCK_EVENTS" ]]

echo 'OK: upload, extract, script file, order, sudo, and fail-fast handling'

