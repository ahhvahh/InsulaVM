#!/usr/bin/env bash
# Preparar Debian 13 minimo via InsulaVM; execute do diretorio com ./insula e config.yaml.
set -Eeuo pipefail
umask 077
PROFILE="deb13-abioma-tests"
FROZEN="deb13-abioma-tests-preparado"
ROOT="$(pwd -P)"
BIN="$ROOT/insula"
CONFIG="$ROOT/config.yaml"
ISO_DIR="$ROOT/isos/$PROFILE"
ISO="$ISO_DIR/mini.iso"
STATE="$ROOT/.insula-testes"
IDFILE="$STATE/$PROFILE.id"
ACTION="all"
if (( $# )); then ACTION="$1"; shift; fi
if (( $# )); then echo "Argumentos inesperados" >&2; exit 2; fi
usage() {
  echo "Uso: bash scripts/preparar-deb13-abioma-tests.sh [all|download|install|boot|open|export|status]"
  echo "all baixa ISO, instala o perfil 1 CPU/1 GiB/5 GiB, liga VM, abre shell e pergunta sobre a exportacao."
  echo "open reconecta a VM; export congela o disco em novo perfil e gera uma ISO Live."
}
case "$ACTION" in
  help|-h|--help) usage; exit 0 ;;
  all|download|install|boot|open|export|status) ;;
  *) usage >&2; exit 2 ;;
esac
[[ -x "$BIN" ]] || { echo "Execute no diretorio que contem ./insula" >&2; exit 1; }
[[ -f "$CONFIG" ]] || { echo "config.yaml nao encontrado: $CONFIG" >&2; exit 1; }
mkdir -p "$ISO_DIR" "$STATE"
chmod 700 "$ISO_DIR" "$STATE"
insula() {
  local group="$1" verb="$2"
  shift 2
  "$BIN" "$group" "$verb" -config "$CONFIG" "$@"
}
vmid() {
  [[ -s "$IDFILE" ]] || { echo "UUID ausente; use boot primeiro" >&2; return 1; }
  ID="$(cat "$IDFILE")"
  [[ "$ID" =~ ^[a-f0-9-]{36}$ ]] || { echo "UUID invalido" >&2; return 1; }
}
download() {
  echo "Baixando/verificando a ultima Debian 13 mini.iso oficial em $ISO_DIR"
  local output backup
  if output="$(insula test download -output-dir "$ISO_DIR" 2>&1)"; then
    echo "$output"
  elif [[ "$output" == *"mini.iso in cache has unexpected checksum"* ]] &&
       [[ -f "$ISO" && ! -L "$ISO" ]]; then
    backup="$ISO_DIR/mini.iso.anterior.$(date +%Y%m%d%H%M%S).$"
    mv -n -- "$ISO" "$backup"
    echo "Versao anterior preservada em $backup"
    insula test download -output-dir "$ISO_DIR"
  else
    echo "$output" >&2
    return 1
  fi
}
install() {
  if details="$(insula profile show -name "$PROFILE" 2>/dev/null)"; then
    [[ " $details " == *" memory_mib=1024 vcpus=1 disk_gb=5 "* ]] || {
      echo "Perfil existente com parametros diferentes: $details" >&2; return 1;
    }
    echo "Perfil $PROFILE ja instalado"
    return
  fi
  [[ -s "$ISO" ]] || { echo "Execute download antes" >&2; return 1; }
  insula test install -profile "$PROFILE" -iso "$ISO" -memory-mib 1024 -vcpus 1 -disk-gb 5
}
boot() {
  local line status
  if [[ -s "$IDFILE" ]]; then
    vmid
    if status="$(insula vm status -id "$ID" 2>/dev/null)" &&
       grep -Eq '^status_libvirt=(running|blocked)$' <<<"$status"; then
      echo "VM $ID ja esta executando"
      return
    fi
    echo "UUID anterior $ID: verifique com 'insula vm list' antes de criar outra VM" >&2
    return 1
  fi
  line="$(insula test boot -profile "$PROFILE" -memory-mib 1024 -vcpus 1)"
  echo "$line"
  ID="$(sed -nE 's/^TEST BOOT OK id=([a-f0-9-]{36}) .*/\1/p' <<<"$line" | head -n 1)"
  [[ "$ID" =~ ^[a-f0-9-]{36}$ ]] || { echo "UUID nao detectado; consulte vm list" >&2; return 1; }
  printf '%s\n' "$ID" > "$IDFILE"
}
open_vm() {
  vmid
  echo "Dentro da VM, prepare o Debian; digite 'exit' para voltar sem desligar."
  insula test shell -id "$ID"
}
export_vm() {
  vmid
  if insula profile show -name "$FROZEN" >/dev/null 2>&1; then
    echo "Perfil congelado ja existe: $FROZEN" >&2; return 1
  fi
  [[ -t 0 ]] || { echo "Exportacao exige confirmacao no terminal" >&2; return 1; }
  local ok
  read -r -p "Salvar disco e ISO Live como $FROZEN, encerrando a VM? [s/N] " ok || return 0
  case "$ok" in s|S|sim|SIM) ;; *) echo "VM mantida: $ID"; return 0 ;; esac
  # test iso cuida do shutdown e do congelamento. Nunca chamar vm stop primeiro.
  insula test iso -id "$ID" -name "$FROZEN" -confirm -verify=false
  rm -f "$IDFILE"
  echo "QCOW2 e ISO Live preparados. Perfil: $FROZEN"
}
status_vm() {
  insula profile show -name "$PROFILE" || true
  if [[ -s "$IDFILE" ]]; then vmid && insula vm status -id "$ID" || true; fi
  insula profile show -name "$FROZEN" || true
  echo "ISO de instalacao: $ISO"
}
case "$ACTION" in
  download) download ;;
  install) install ;;
  boot) boot ;;
  open) open_vm ;;
  export) export_vm ;;
  status) status_vm ;;
  all)
    insula config check
    download
    install
    boot
    open_vm
    export_vm
    ;;
esac
