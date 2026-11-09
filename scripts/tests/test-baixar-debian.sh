#!/usr/bin/env bash
# Teste offline do downloader, sem acesso à rede nem downloads reais.
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "$0")/../.." && pwd -P)"
TMP="$(mktemp -d)"
trap 'rm -rf -- "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/fixtures" "$TMP/output"
name='debian-13.7.0-amd64-netinst.iso'
printf 'fixture ISO de testes\n' > "$TMP/fixtures/$name"
printf '%s  %s\n' "$(sha256sum "$TMP/fixtures/$name" | cut -d' ' -f1)" "$name" > "$TMP/fixtures/SHA256SUMS"
cat > "$TMP/bin/curl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
output=''
url=''
while (( $# )); do
  case "$1" in
    --output) output="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
[[ -n "$output" && -n "$url" ]] || exit 3
printf 'fetch %s\n' "$url" >> "$MOCK_LOG"
cp -- "$MOCK_FIXTURES/${url##*/}" "$output"
MOCK
chmod +x "$TMP/bin/curl"
export PATH="$TMP/bin:$PATH" MOCK_FIXTURES="$TMP/fixtures" MOCK_LOG="$TMP/fetch.log"
script="$ROOT/scripts/baixar-debian.sh"
path="$(bash "$script" --output-dir "$TMP/output")"
[[ "$path" == "$TMP/output/$name" && -s "$path" ]] || { echo 'download falhou' >&2; exit 1; }
[[ $(wc -l < "$MOCK_LOG") -eq 2 ]] || { echo 'fluxo de download inesperado' >&2; exit 1; }
path2="$(bash "$script" --output-dir "$TMP/output")"
[[ "$path2" == "$path" && $(wc -l < "$MOCK_LOG") -eq 3 ]] || { echo 'cache não funcionou' >&2; exit 1; }
printf 'corrompido\n' > "$path"
if bash "$script" --output-dir "$TMP/output" > /dev/null 2>&1; then
  echo 'deveria recusar arquivo corrompido sem --force' >&2; exit 1
fi
path3="$(bash "$script" --output-dir "$TMP/output" --force)"
[[ "$path3" == "$path" ]] || { echo 're-download falhou' >&2; exit 1; }
cmp -s "$path" "$TMP/fixtures/$name" || { echo 'ISO final não confere' >&2; exit 1; }
echo INSULA_DEBIAN_DOWNLOAD_OK
