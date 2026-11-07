#!/usr/bin/env bash
# Envia um arquivo/pasta para uma VM InsulaVM ativa e executa comandos
# sequencialmente SOMENTE depois que o upload/extracao terminar com sucesso.
set -euo pipefail

usage() {
  cat <<'USAGE'
Uso:
  bash scripts/enviar-executar-vm.sh --id UUID --source ARQUIVO [opcoes]

Opcoes:
  --id UUID             UUID da VM ja iniciada (obrigatorio)
  --source CAMINHO       Arquivo ou pasta local (obrigatorio)
  --extract              Extrai .tar.gz/.tgz recebido em guest.destination
  --script CAMINHO       Envia um script Bash local e executa apos a carga
  --command 'COMANDO'    Executa comando remoto apos upload (repetivel)
  --sudo                 Executa --script e --command usando sudo -n
  --shell                Abre terminal SSH apos todas as etapas (opcional)
  --config CAMINHO       Configuracao InsulaVM (padrao: ../config.yaml)
  --insula CAMINHO       Executavel InsulaVM (padrao: ../insula)
  --help                 Exibe esta ajuda

Exemplo:
  bash scripts/enviar-executar-vm.sh \
    --id UUID_DA_VM --source ./app.tar.gz --extract \
    --script ./instalar.sh --command 'ls -lah' --shell

O destino e sempre guest.destination do config.yaml.
Ordem: enviar arquivo, extrair (--extract), enviar/executar script (--script),
executar --command em sequencia e abrir terminal (--shell).
O script recebe como primeiro argumento o nome do arquivo/pasta enviado.
Se uma etapa falhar, as etapas seguintes NAO serao executadas.
Somente descompacte pacotes TAR.GZ de origem confiavel.
USAGE
}

log() { printf '[InsulaVM][envio] %s\n' "$*" >&2; }
die() { printf '[InsulaVM][envio][erro] %s\n' "$*" >&2; exit 1; }
need_value() {
  (($# >= 2)) && [[ -n "$2" ]] && [[ "$2" != --* ]] ||
    die "Informe o valor de $1"
}

# Recebe um caminho como argumento e retorna sua forma absoluta.
absolute_source() {
  local source="$1" parent
  [[ -f "$source" || -d "$source" ]] || die "Arquivo ou pasta inexistente: $source"
  parent="$(cd -- "$(dirname -- "$source")" && pwd -P)" ||
    die "Nao foi possivel acessar a pasta da origem"
  printf '%s/%s' "$parent" "$(basename -- "$source")"
}

# Aspas seguras para um unico argumento em POSIX sh na VM.
remote_quote() {
  local v="$1"
  v=${v//\'/\'\"\'\"\'}
  printf "'%s'" "$v"
}

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_dir="$(cd -- "$script_dir/.." && pwd -P)"
vm_id=""
source_file=""
execution_script=""
extract=false
shell=false
sudo=false
config="$repo_dir/config.yaml"
insula="$repo_dir/insula"
commands=()

while (($#)); do
  case "$1" in
    --id|-id)
      need_value "$@"; vm_id="$2"; shift 2 ;;
    --source|-source|--file)
      need_value "$@"; source_file="$2"; shift 2 ;;
    --extract)
      extract=true; shift ;;
    --script)
      need_value "$@"; execution_script="$2"; shift 2 ;;
    --command)
      need_value "$@"; commands+=("$2"); shift 2 ;;
    --sudo)
      sudo=true; shift ;;
    --shell)
      shell=true; shift ;;
    --config)
      need_value "$@"; config="$2"; shift 2 ;;
    --insula)
      need_value "$@"; insula="$2"; shift 2 ;;
    --help|-h)
      usage; exit 0 ;;
    *)
      die "Opcao desconhecida: $1 (consulte --help)" ;;
  esac
done

[[ "$vm_id" =~ ^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}$ ]] ||
  die "Informe --id com um UUID valido"
[[ -n "$source_file" ]] || die "Informe --source com o arquivo/pasta para enviar"
source_file="$(absolute_source "$source_file")"
if [[ -n "$execution_script" ]]; then
  [[ -f "$execution_script" && ! -L "$execution_script" ]] || die "--script exige um arquivo Bash comum"
  execution_script="$(absolute_source "$execution_script")"
  [[ -r "$execution_script" ]] || die "Sem permissao para ler script: $execution_script"
  [[ "$(basename -- "$source_file")" != "$(basename -- "$execution_script")" ]] ||
    die "Arquivo principal e script nao podem ter o mesmo nome no guest.destination"
fi
[[ -f "$config" ]] || die "Arquivo de configuracao ausente: $config"
[[ -x "$insula" ]] || die "Executavel InsulaVM ausente: $insula (compile com go build -o insula .)"

if "$extract"; then
  [[ -f "$source_file" ]] || die "--extract exige um arquivo TAR.GZ, nao uma pasta"
  case "${source_file,,}" in
    *.tar.gz|*.tgz) ;;
    *) die "--extract aceita somente arquivos .tar.gz ou .tgz" ;;
  esac
fi

log "Enviando $(basename -- "$source_file") para a VM $vm_id"
"$insula" vm import -config "$config" -id "$vm_id" -source "$source_file"
log "Transferencia concluida"

# O script e enviado apenas depois que a carga principal foi confirmada.
# Para --extract, ele sera enviado depois da descompactacao.
if "$extract"; then
  archive_name="$(basename -- "$source_file")"
  log "Extraindo $archive_name dentro de guest.destination"
  "$insula" vm exec -config "$config" -id "$vm_id" \
    -command "tar -xzf $(remote_quote "$archive_name")"
  log "Extracao concluida"
fi

if [[ -n "$execution_script" ]]; then
  script_name="$(basename -- "$execution_script")"
  log "Enviando script de pos-carga: $script_name"
  "$insula" vm import -config "$config" -id "$vm_id" -source "$execution_script"
  log "Executando script de pos-carga: $script_name"
  script_exec_args=(vm exec -config "$config" -id "$vm_id" \
    -command "bash -e -- $(remote_quote "./$script_name") $(remote_quote "$(basename -- "$source_file")")")
  if "$sudo"; then script_exec_args+=(-sudo); fi
  "$insula" "${script_exec_args[@]}"
  log "Script de pos-carga concluido"
fi

if (("${#commands[@]}")); then
  for index in "${!commands[@]}"; do
    log "Executando comando $((index + 1))/${#commands[@]}: ${commands[index]}"
    exec_args=(vm exec -config "$config" -id "$vm_id" -command "${commands[index]}")
    if "$sudo"; then exec_args+=(-sudo); fi
    "$insula" "${exec_args[@]}"
    log "Comando $((index + 1)) concluido"
  done
fi

if "$shell"; then
  log "Abrindo terminal da VM $vm_id"
  "$insula" vm shell -config "$config" -id "$vm_id"
fi

log "Operacao concluida"
