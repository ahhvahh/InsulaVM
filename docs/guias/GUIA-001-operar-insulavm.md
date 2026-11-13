<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-001 — Operar e automatizar VMs InsulaVM

**Estado:** procedimentos baseados na CLI atual.
**Público:** administrador do hospedeiro Linux.
**Referências de comandos:** [INTF-TERM-004](../interfaces/terminal/INTF-TERM-004-gerenciar-vms.md) e [INTF-TERM-005](../interfaces/terminal/INTF-TERM-005-deploy-vm.md).

## Preparar, reutilizar e congelar ambiente de testes

1. Registre um perfil QCOW2 com sistema já instalado e Cloud-Init. A ISO Debian instaladora não é aceita como perfil pronto.
2. Inicie uma VM descartável, faça os ajustes em seu próprio overlay e confirme os comandos/testes necessários.
3. Se a intenção for preservar o ambiente, faça o congelamento por nome **antes** de `vm stop`. Perfis Cloud-Init exigem sudo sem senha; VMs criadas com `vm install-iso` utilizam `installer.qcow2` e são congeladas sem SSH/Cloud-Init (sem limpeza automática da identidade do guest).
4. Crie novas VMs a partir do novo perfil. Os overlays de cada instância permanecem independentes.

```bash
./insula vm start -profile debian13
./insula vm list
./insula vm freeze -id UUID_DA_VM -name debian13-go-testes
./insula profile list
./insula vm start -profile debian13-go-testes -memory-mib 2048 -vcpus 2
```

**Atenção:** `vm freeze` encerra e elimina a instância de origem após publicar o QCOW2 autônomo. Veja [FLUX-004](../fluxos/FLUX-004-congelar-vm.md). Para encerrar outras VMs gerenciadas, simule antes com `./insula vm stop-all -dry-run`.

## Congelar o Alpine instalado por ISO

```bash
./insula vm status -id UUID_DA_VM
./insula vm freeze -id UUID_DA_VM -name abioma-tests
./insula profile show -name abioma-tests
./insula vm start -profile abioma-tests -memory-mib 500 -vcpus 1
./insula vm exec -qga -id UUID_NOVA_VM "uname -a"
```

O perfil de origem `installer-iso` armazena o Linux instalado no arquivo `installer.qcow2`. A conversão registra uma base independente comprimida e `boot_mode: direct`, sem ISO/Cloud-Init e sem validação SSH. A inicialização reporta somente domínio Libvirt `running`; valide boot e disponibilidade do QGA dentro do guest. Prepare manualmente a identidade da imagem antes do freeze quando houver necessidade de host keys ou `machine-id` únicos. Não execute `vm stop` antes do freeze, pois isso remove o disco instalador.

## Deploy direto

```bash
./insula vm deploy -id UUID_DA_VM   -source ./pacote.tar.gz   -script ./instalar.sh   -extract
```

O script recebe o nome-base do pacote em `$1` e só é executado após a transferência e a extração opcional. Consulte [FLUX-005](../fluxos/FLUX-005-deploy-arquivos.md).

## Enviar arquivo para VM ativa e executar comandos em sequência

O script [`scripts/enviar-executar-vm.sh`](../../scripts/enviar-executar-vm.sh) usa `insula vm import`, aguarda o fim do envio, pode extrair `.tar.gz`/`.tgz`, executa cada comando com `insula vm exec` e opcionalmente abre `insula vm shell`. **Não inicia nem interrompe a VM**. O destino é o `guest.destination` do `profile.yaml` da VM (fallback legado: `/tmp/insulavm/workspace`).

```bash
bash scripts/enviar-executar-vm.sh \
  --id UUID_DA_VM \
  --source ./pacote.tar.gz \
  --extract \
  --command 'ls -lah' \
  --command 'bash ./instalar.sh' \
  --shell
```

Para enviar um script Bash simples e executá-lo imediatamente:

```bash
bash scripts/enviar-executar-vm.sh \
  --id UUID_DA_VM --source ./instalar.sh \
  --command 'bash ./instalar.sh'
```

É possível usar `--command` várias vezes; a execução é estritamente sequencial. `--sudo` aplica `sudo -n` aos comandos posteriores (quando permitido pela VM). Se importação, extração ou qualquer comando falhar, os seguintes não são executados e o script retorna erro. O `vm exec` mantém logs de cada execução no hospedeiro. **Extraia apenas arquivos TAR de origem confiável.** Consulte `--help` para todas as opções.

### Executar um script Bash após carregar um arquivo

O mesmo comando aceita `--script /caminho/local/pos-carga.sh`. Primeiro envia o arquivo principal, opcionalmente extrai com `--extract`, **depois envia o script Bash e executa-o automaticamente** no diretório `guest.destination` da VM. O script recebe como primeiro argumento (`$1`) o **nome do arquivo enviado**. Não é necessário marcar o arquivo `.sh` como executável, pois ele roda com `bash -e`.

```bash
bash scripts/enviar-executar-vm.sh \
  --id UUID_DA_VM \
  --source ./pacote.tar.gz \
  --extract \
  --script ./pos-carga.sh \
  --shell
```

Exemplo de `pos-carga.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
pacote="${1:?nome do arquivo ausente}"
echo "Arquivo recebido: $pacote"
test -f "$pacote"
ls -lah
# Escreva aqui os comandos de instalacao/teste necessarios.
```

**Ordem garantida:** upload principal → extração opcional → upload do script → execução do script → `--command` adicionais → `--shell`. Falhas interrompem a sequência e produzem código de saída diferente de zero. `--sudo` aplica-se ao script e aos comandos. A VM existente não é desligada.

## Enviar um repositório Git para uma VM e abrir o terminal

O script [scripts/repositorio-vm.sh](../../scripts/repositorio-vm.sh) usa **bash, git, tar e gzip** para criar `<nome-do-repositório>.tar.gz` a partir de `HEAD`. Ele empacota **somente arquivos versionados**, sem `.git`, alterações locais não commitadas, arquivos ignorados ou discos temporários. O pacote contém uma pasta raiz com o nome do repositório.

Para gerar somente o TAR.GZ no diretório atual:

```bash
bash scripts/repositorio-vm.sh empacotar .
# Resultado: ./InsulaVM.tar.gz
```

Para gerar o arquivo, enviá-lo à VM **já iniciada**, enviar o próprio script, extrair em `guest.destination` e abrir uma sessão SSH na pasta do projeto:

```bash
git pull --ff-only origin main
go build -o insula ./cmd/insula
bash scripts/repositorio-vm.sh enviar 01a1b24e-88cb-7464-9acc-06a08cf727fd .
```

Para outro repositório local, substitua `.` pelo caminho do projeto. A VM é mantida ligada quando você digita `exit` no terminal SSH. A transferência usa o comando existente `insula vm import` e não precisa de um serviço adicional na VM. A extração usa `insula vm exec` no destino global configurado.

É possível abrir a mesma VM sem enviar arquivos:

```bash
./insula vm shell -config config.yaml -id UUID_DA_VM
./insula vm shell -config config.yaml -id UUID_DA_VM -directory InsulaVM
```

**Importante:** o pacote é um snapshot do commit atual, não um clone Git completo. Para realizar commits/pushes dentro da VM, será necessário um clone Git separado ou implementar exportação de histórico. Não inclua segredos ou chaves privadas em arquivos versionados.

## Criar instâncias com SSH automático e sudo

Uma **imagem QCOW2 com Linux já instalado e preparado com Cloud-Init** é inicializada com uma ISO **NoCloud/CIDATA gerada pelo InsulaVM**, contendo um usuário `insula` e chaves Ed25519 individuais. A ISO de configuração não instala Linux; o instalador Debian/Arch por ISO é uma funcionalidade diferente e ainda pendente.

```bash
go run ./cmd/insula vm start -config config.yaml -profile debian-minimal
go run ./cmd/insula vm list -config config.yaml
go run ./cmd/insula vm status -config config.yaml -id UUIDv7
go run ./cmd/insula vm exec -config config.yaml -id UUIDv7 -command "id -un"
go run ./cmd/insula vm exec -config config.yaml -id UUIDv7 -command "id -u" -sudo
go run ./cmd/insula vm import -config config.yaml -id UUIDv7 -source /srv/projeto
go run ./cmd/insula vm stop -config config.yaml -id UUIDv7
```

**Caminhos exibidos no terminal:** os logs, o comando SSH, `vm status`, `config check` e a localização dos relatórios mostram caminhos **relativos ao diretório atual** (`data/profiles/...`, `data/runs/...`), sem repetir `/home/usuario/...`. O armazenamento, as chaves e os contratos da biblioteca Go continuam usando caminhos absolutos internamente. Copie e execute o comando SSH **a partir do mesmo diretório** em que executou `insula`; se mudar de diretório, obtenha um novo comando pelo `vm status` ou use os caminhos completos manualmente.

Ao executar `vm start`, o InsulaVM mostra no terminal as etapas de validação do perfil, criação do overlay QCOW2, geração Cloud-Init, permissões, registro SQLite, inicialização Libvirt, obtenção do IP, SSH e validação de sudo. Uma falha inclui a etapa e o diagnóstico. Um segundo `vm start` **cria outra VM**: a CLI avisa quando já existe alguma instância ativa do mesmo perfil.

**Estado real versus histórico:** `vm list` exibe duas colunas (`SQLite` e `Libvirt`). `vm status` consultam o domínio no Libvirt e avisam sobre registros `creating`/`running` sem VM correspondente. A consulta não altera o banco automaticamente. `vm exec`, `vm import` e `vm stop` também validam o estado real antes de interagir com a instância ou remover arquivos. Se não for possível consultar o Libvirt, o programa apresenta erro em vez de supor que a VM está desligada.

O SQLite é compartilhado entre CLI e biblioteca no caminho `storage.database` (padrão: `<storage.root>/insula.db`). Armazena UUIDv7, UUID Libvirt, MAC, IP, usuário, impressão digital da chave do servidor, referências a chaves e estado da VM. A chave privada permanece protegida em arquivo exclusivo da instância e é removida junto com o overlay ao encerrar.

O modo `ssh.sudo_mode: passwordless` no `config.yaml` habilita `sudo -n`, sem senha ou prompt. Use somente em VMs confiáveis; omitir a opção desabilita sudo por padrão. A biblioteca oferece `Engine.StartVM`, `GetVM`, `ListVMs`, `ExecVM` e `StopVM`. Veja [ARQ-006 — Identidade, SQLite e sudo](../../docs/arquitetura/ARQ-006-identidade-registro-sudo.md).

## Destino por perfil e usuário da Action

Defina uma pasta Linux **para cada perfil** em `data/profiles/<nome>/profile.yaml`. As importações locais e os jobs sem `workspace` explícito usarão esse caminho nas VMs criadas a partir do perfil:

```yaml
# profile.yaml
guest:
  destination: /tmp/insulavm/workspace
```

O usuário SSH padrão continua em `config.yaml` (`ssh.user: insula`). Perfis antigos que não declaram `guest.destination` herdam o valor legado de `config.yaml`, ou `/tmp/insulavm/workspace` se não existir.

O destino é criado no convidado pelo usuário SSH; não é uma pasta do hospedeiro. Se escolher um local protegido como `/opt`, prepare as permissões no perfil QCOW2 antes de executar.

O usuário é opcional na Action (`insula.yaml`). Se não for informado, prevalece `ssh.user` do `config.yaml`, cujo padrão é `insula`:

```yaml
profile: debian-minimal
repository:
  name: meu-projeto
action: testar
# ssh:
#   user: buildagent
imports:
  - source: /srv/meu-projeto
  - source: /srv/configuracoes.ini
pipelines:
  - name: build
    jobs:
      - name: listar
        commands:
          - ls -la
```

Não existe `destination`, `key` ou senha nesse manifesto. Em VMs gerenciadas, o Cloud-Init cria o usuário escolhido e o InsulaVM gera, guarda e verifica as chaves automaticamente.

Para uma VM já criada e registrada:

```bash
go run ./cmd/insula vm import -config config.yaml -id UUIDv7 -source /srv/meu-projeto
go run ./cmd/insula vm import -config config.yaml -id UUIDv7 -source /srv/extra.ini
```

**Ambos os uploads usam a mesma pasta.** A biblioteca expõe `Engine.ImportToVM(ctx, VMImportRequest{VMID: ..., SourcePath: ...})`, sem destino nem chave na chamada. Os estágios podem ser acompanhados por `OnProgress`; uma Action usa `RunRequest.OnImportProgress`.

Para hosts SSH externos não administrados pelo InsulaVM, o comando `insula import -host ... -source ...` utiliza `ssh.private_key` e `ssh.known_hosts` definidos no arquivo global. Nesse cenário não é possível criar automaticamente credenciais em uma máquina preexistente sem acesso de provisionamento.

Veja [ARQ-005 — Importação de arquivos](../../docs/arquitetura/ARQ-005-importacao-arquivos.md).

## Uso

Requisitos de desenvolvimento: Go 1.23+. Para criar VMs: Linux com KVM/QEMU, `virsh`, `qemu-img`, rede Libvirt com DHCP, acesso ao hipervisor e permissões apropriadas para as imagens. Para VMs gerenciadas: Cloud-Init e OpenSSH já instalados no convidado. Chaves são geradas automaticamente; em hosts SSH externos, é necessário cadastrar chave privada e `known_hosts` na configuração global.

```bash
# Validar config.yaml
go run ./cmd/insula config check -config config.yaml

# Uma vez, após instalar o Debian em um QCOW2 por outro procedimento:
go run ./cmd/insula profile register -config config.yaml \
  -name debian-minimal \
  -image /tmp/debian-instalado.qcow2 \
  -memory-mib 1024 -vcpus 1 -disk-gb 8 -os debian

go run ./cmd/insula profile list -config config.yaml
go run ./cmd/insula profile show -config config.yaml -name debian-minimal

# Configurar os comandos do manifesto, sem esquecer "profile: debian-minimal"
cp insula.yaml.example insula.yaml

# A imagem já precisa possuir Linux + Cloud-Init + SSH.
# Não precisa informar usuário, chave SSH ou pasta de destino.
go run ./cmd/insula run -config config.yaml -manifest insula.yaml
```

No modo de VM automática, o InsulaVM gerencia as chaves SSH e verifica a identidade do servidor por `known_hosts` específico da instância. O processo Libvirt deve conseguir acessar os diretórios QCOW2; ACLs/grupos são responsabilidade da instalação.

## Logs e testes

```text
data/logs/<repositorio>/<action-ou-pipeline>/<YYYYMMDD_HHMMSS>/<job>.log
```

Cada comando enviado é registrado com `>>`; stdout e stderr do convidado são salvos por linha com `<<`. O próximo comando só executa após a conclusão real do anterior. Os logs permanecem mesmo quando a instância temporária é descartada.

```bash
go test -race ./...
go vet ./...
```

## Solução de problemas

Consulte [GUIA-002 — Acesso ao armazenamento Libvirt](GUIA-002-permissoes-libvirt.md).
