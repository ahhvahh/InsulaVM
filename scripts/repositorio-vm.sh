#!/usr/bin/env bash
# Empacota somente os arquivos versionados (HEAD) e, opcionalmente,
# transfere/abre uma VM ja iniciada pelo InsulaVM.
set -euo pipefail

usage() {
  cat <<'USAGE'
Uso:
  bash scripts/repositorio-vm.sh empacotar [PASTA_GIT] [DESTINO]
  bash scripts/repositorio-vm.sh enviar UUID_DA_VM [PASTA_GIT] [CONFIG_YAML]
  bash repositorio-vm.sh extrair REPOSITORIO.tar.gz

empacotar  Gera DESTINO/<repositorio>.tar.gz (padrao: pasta atual).
enviar     Empacota HEAD, importa arquivo e script para a VM,
           extrai no guest.destination e abre terminal no repositorio.
extrair    Descompacta o TAR.GZ na pasta atual dentro da VM.

O pacote NAO inclui .git, arquivos ignorados nem mudancas sem commit.
USAGE
}

die() { printf '[repositorio-vm] ERRO: %s\n' "$*" >&2; exit 1; }
log() { printf '[repositorio-vm] %s\n' "$*" >&2; }

script_path="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/$(basename -- "${BASH_SOURCE[0]}")"

repository_info() {
  local source="${1:-.}"
  [[ -d "$source" ]] || die "Diretorio nao encontrado: $source"
  repo_root="$(git -C "$source" rev-parse --show-toplevel)" ||
    die "A pasta nao pertence a um repositorio Git: $source"
  repo_name="$(basename -- "$repo_root")"
  [[ "$repo_name" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ ]] ||
    die "Nome de repositorio nao suportado: $repo_name"
  git -C "$repo_root" rev-parse --verify HEAD >/dev/null ||
    die "O repositorio ainda nao tem um commit HEAD"
}

create_archive() {
  local destination="$1" outfile="$2"
  mkdir -p -- "$destination"
  log "Compactando $repo_name (HEAD) em $(basename -- "$outfile")"
  git -C "$repo_root" archive --format=tar --prefix="$repo_name/" HEAD |
    gzip -n > "$outfile"
  [[ -s "$outfile" ]] || die "O arquivo compactado esta vazio"
  gzip -t "$outfile" || die "Falha ao validar o TAR.GZ"
}

extract_archive() {
  local archive="${1:-}"
  [[ "$archive" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*\.tar\.gz$ ]] ||
    die "Informe somente o nome REPOSITORIO.tar.gz"
  [[ -f "$archive" ]] || die "Arquivo nao encontrado: $archive"
  local name="${archive%.tar.gz}" member
  while IFS= read -r member; do
    case "$member" in
      "$name/"|"$name/"*) ;;
      *) die "Caminho fora do repositorio no pacote: $member" ;;
    esac
  done < <(tar -tzf "$archive")
  log "Extraindo $archive para ./$name/"
  tar -xzf "$archive" -C .
  log "Arquivos disponiveis em ./$name/"
}

send_to_vm() {
  local vm_id="${1:-}" source="${2:-.}" config_arg="${3:-}"
  [[ "$vm_id" =~ ^[0-9a-fA-F-]{36}$ ]] || die "Informe um UUID valido da VM"
  repository_info "$source"
  local insula_root insula_bin config_file tmp archive
  insula_root="$(cd -- "$(dirname -- "$script_path")/.." && pwd -P)"
  insula_bin="$insula_root/insula"
  [[ -x "$insula_bin" ]] || die "Executavel ausente: $insula_bin (compile com go build -o insula ./cmd/insula)"
  config_file="${config_arg:-$insula_root/config.yaml}"
  [[ -f "$config_file" ]] || die "Configuracao nao encontrada: $config_file"
  config_file="$(cd -- "$(dirname -- "$config_file")" && pwd -P)/$(basename -- "$config_file")"
  tmp="$(mktemp -d)"
  cleanup_tmp_dir="$tmp"
  trap 'rm -rf -- "$cleanup_tmp_dir"' EXIT
  archive="$tmp/$repo_name.tar.gz"
  create_archive "$tmp" "$archive"
  cd -- "$insula_root"
  log "Enviando $repo_name.tar.gz para VM $vm_id"
  "$insula_bin" vm import -config "$config_file" -id "$vm_id" -source "$archive"
  log "Enviando este script para a mesma VM"
  "$insula_bin" vm import -config "$config_file" -id "$vm_id" -source "$script_path"
  log "Extraindo na pasta guest.destination da VM"
  "$insula_bin" vm exec -config "$config_file" -id "$vm_id" \
    -command "bash ./repositorio-vm.sh extrair '$repo_name.tar.gz'"
  log "Abrindo terminal dentro de $repo_name (digite exit para sair)"
  "$insula_bin" vm shell -config "$config_file" -id "$vm_id" -directory "$repo_name"
}

case "${1:-}" in
  empacotar)
    repository_info "${2:-.}"
    dest="${3:-$PWD}"
    create_archive "$dest" "$dest/$repo_name.tar.gz"
    log "Gerado: $dest/$repo_name.tar.gz"
    ;;
  enviar) send_to_vm "${2:-}" "${3:-.}" "${4:-}" ;;
  extrair) extract_archive "${2:-}" ;;
  -h|--help|help) usage ;;
  *) usage >&2; exit 2 ;;
esac
