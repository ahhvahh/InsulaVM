#!/usr/bin/env bash
# Baixa a ISO netinst oficial do Debian 13 amd64 e valida sua integridade.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
OUTPUT_DIR="$ROOT/data/isos"
BASE_URL='https://cdimage.debian.org/debian-cd/current/amd64/iso-cd'
FORCE=0

usage() {
  cat <<'USAGE'
Uso: bash scripts/baixar-debian.sh [--output-dir CAMINHO] [--force]

Baixa a ISO Debian 13 amd64 netinst a partir do repositório oficial,
valida SHA256 e imprime o caminho absoluto da ISO no stdout.

  --output-dir DIR  Pasta de destino (padrão: data/isos no repositório)
  --force           Substitui uma cópia existente com SHA256 incorreto
  -h, --help        Exibe esta ajuda
USAGE
}

fail() { printf 'ERRO: %s\n' "$*" >&2; exit 1; }

while (( $# )); do
  case "$1" in
    --output-dir)
      (( $# >= 2 )) || fail 'Informe a pasta após --output-dir'
      OUTPUT_DIR="$2"; shift 2 ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "Argumento desconhecido: $1" ;;
  esac
done

for command in curl awk sha256sum mktemp; do
  command -v "$command" >/dev/null 2>&1 || fail "Comando não encontrado: $command"
done

mkdir -p -- "$OUTPUT_DIR"
OUTPUT_DIR="$(cd -- "$OUTPUT_DIR" && pwd -P)"
TEMP_DIR="$(mktemp -d)"
PARTIAL=''
cleanup() {
  [[ -z "$PARTIAL" ]] || rm -f -- "$PARTIAL"
  rm -rf -- "$TEMP_DIR"
}
trap cleanup EXIT

printf 'Consultando SHA256SUMS oficial do Debian...\n' >&2
curl --fail --location --silent --show-error --retry 3 --connect-timeout 20 \
  --output "$TEMP_DIR/SHA256SUMS" "$BASE_URL/SHA256SUMS"

mapfile -t matches < <(awk '$2 ~ /^debian-13\.[0-9]+\.[0-9]+-amd64-netinst\.iso$/ { print $2 }' "$TEMP_DIR/SHA256SUMS")
(( ${#matches[@]} == 1 )) || fail 'SHA256SUMS não contém uma única ISO Debian 13 amd64 netinst'
ISO_NAME="${matches[0]}"
EXPECTED_SHA="$(awk -v name="$ISO_NAME" '$2 == name { print $1 }' "$TEMP_DIR/SHA256SUMS")"
[[ "$EXPECTED_SHA" =~ ^[a-fA-F0-9]{64}$ ]] || fail 'Checksum SHA256 inválido no manifesto'
ISO_PATH="$OUTPUT_DIR/$ISO_NAME"

if [[ -e "$ISO_PATH" ]]; then
  if printf '%s  %s\n' "$EXPECTED_SHA" "$ISO_PATH" | sha256sum --check --status -; then
    printf 'ISO já existe e está íntegra.\n' >&2
    printf '%s\n' "$ISO_PATH"
    exit 0
  fi
  (( FORCE == 1 )) || fail "ISO existente falhou no SHA256. Use --force para substituí-la: $ISO_PATH"
fi

PARTIAL="$(mktemp "$OUTPUT_DIR/.${ISO_NAME}.partial.XXXXXX")"
printf 'Baixando %s...\n' "$ISO_NAME" >&2
curl --fail --location --silent --show-error --retry 3 --connect-timeout 20 \
  --output "$PARTIAL" "$BASE_URL/$ISO_NAME"
printf '%s  %s\n' "$EXPECTED_SHA" "$PARTIAL" | sha256sum --check --status - \
  || fail 'Falha na validação SHA256 da ISO baixada'
mv -f -- "$PARTIAL" "$ISO_PATH"
PARTIAL=''
printf 'Download e SHA256 OK.\n' >&2
printf '%s\n' "$ISO_PATH"
