# InsulaVM

CLI open-source em **Go** para executar jobs sequenciais por SSH dentro de máquinas virtuais Linux, com logs persistentes no hospedeiro.

**Implementado:** instâncias por UUIDv7 registradas em SQLite, SSH individual automatizado por Cloud-Init NoCloud, elevação controlada com sudo, biblioteca Go importável em `github.com/ahhvahh/InsulaVM/insulavm`, CLI usando a mesma API, `config.yaml` global, catálogo de `profile.yaml`, registro de imagens QCOW2 já instaladas, seleção por `insula.yaml`, overlay QCOW2 descartável com QEMU/Libvirt e executor SSH sequencial com logs `>>`/`<<`, além de importação TAR.GZ de arquivos e diretórios do hospedeiro para a VM, com progresso no console ou callback Go.

**Ainda não implementado:** instalação de Debian/Arch a partir de ISO instaladora, criação interativa de perfil por console e checkout automático de repositórios Git (a importação manual de arquivos locais já funciona). A imagem registrada precisa ter Cloud-Init, SSH e DHCP. O destino configurado para imports/jobs é criado automaticamente pelo usuário SSH, desde que tenha permissão.

## Arquivos de configuração

- [`config.yaml`](config.yaml): diretórios dos perfis, execução, logs e banco SQLite; Libvirt, política SSH/sudo e **um destino único em `guest.destination`**.
- `data/profiles/<nome>/profile.yaml`: recursos da VM, versão e caminho para `versions/v001/base.qcow2`.
- [`insula.yaml.example`](insula.yaml.example): escolhe `profile`, usuário SSH opcional (`ssh.user`), fontes `imports`, `setup` e `pipelines`.

Exemplos de perfis (somente metadados; **sem discos instalados**):
[Debian](examples/profiles/debian-minimal/profile.yaml),
[Arch Linux](examples/profiles/arch-minimal/profile.yaml),
[Alpine](examples/profiles/alpine-minimal/profile.yaml),
[Void](examples/profiles/void-minimal/profile.yaml).

## Reutilizar perfis e configurar cada VM

Um perfil instalado (`profile.yaml` + imagem-base QCOW2) pode ser reutilizado por quantas instâncias independentes forem necessárias. Cada inicialização gera um **overlay QCOW2 próprio**, sem modificar o perfil ou a imagem-base. RAM/vCPUs podem ser ajustadas por instância, sem alterar as configurações gravadas.

```bash
./insula vm start -profile debian13
./insula vm start -profile debian13 -memory-mib 2048 -vcpus 2
./insula vm start -profile debian13 -memory-mib 4096 -vcpus 4
```

Na biblioteca Go, `StartVMWithOptions` aceita `VMStartOptions` com `Profile`, `MemoryMiB` e `VCPUs` (zero = usar os valores do perfil). Para gerenciamento do ciclo de vida, `StopOnContextDone` provoca limpeza **assíncrona e de melhor esforço** quando o contexto é cancelado, e `OnStopError` recebe erros dessa limpeza. Para aguardar e validar o encerramento, prefira chamar `StopVM` explicitamente.

```go
engine, err := insulavm.New("config.yaml")
if err != nil { return err }
ctx := context.Background()
instance, err := engine.StartVMWithOptions(ctx, insulavm.VMStartOptions{
    Profile: "debian13",
    MemoryMiB: 2048,
    VCPUs: 2,
}, nil)
if err != nil { return err }
fmt.Println(instance.ID, instance.IPAddress)
// A VM continua ativa; chame engine.StopVM(ctx, instance.ID) quando terminar.
```

## Congelar uma VM preparada e reutilizar como modelo

Após instalar dependências e configurar uma VM gerenciada, crie um **perfil congelado por nome**:

```bash
./insula vm list
./insula vm freeze -id UUID_DA_VM -name debian13-go-testes
./insula profile list
./insula profile show -name debian13-go-testes
./insula vm start -profile debian13-go-testes
./insula vm start -profile debian13-go-testes -memory-mib 4096 -vcpus 4
```

`vm freeze` exige uma VM em execução, provisionada pelo InsulaVM, com `cloud-init`, SSH e `sudo -n` habilitado. A operação **não é um pause/snapshot de RAM**: prepara a imagem para clonagem (`cloud-init clean --logs --machine-id --seed`, remove chaves SSH antigas), solicita desligamento ACPI, aguarda o desaparecimento do domínio transiente e executa `qemu-img convert` para produzir um QCOW2 autônomo, sem backing file. O novo perfil é salvo em `data/profiles/<nome>/versions/v001/base.qcow2`. Ao concluir, elimina a VM original e seus arquivos temporários. O nome não pode sobrescrever um perfil existente.

**Atenção:** o congelamento **encerra a VM de origem**. Não execute `vm stop` antes dele: isso apagaria o overlay que precisa ser capturado. Se a conversão falhar, o overlay é mantido para recuperação; quando a limpeza da VM original falhar após o perfil ser salvo, o comando informa ambos os estados. Na nova inicialização, o Cloud-Init cria novas credenciais e identidade de máquina. Remova também segredos de aplicativos, tokens e dados privados antes de congelar.

```go
result, err := engine.FreezeVM(ctx, insulavm.VMFreezeRequest{
    VMID: vm.ID,
    Name: "debian13-go-testes",
    ShutdownTimeout: 3 * time.Minute,
})
if err != nil { return err }
fmt.Println(result.ProfileName, result.ImagePath)
```

**ISO x perfil congelado:** esse comando gera um **QCOW2**, não uma ISO Live ou instaladora. O InsulaVM inicia instâncias diretamente de perfis QCOW2 com Cloud-Init. Iniciar de uma ISO instaladora ou exportar uma ISO Live personalizada continua sendo um fluxo separado, ainda não implementado nativamente.

## Encerrar todas as VMs gerenciadas

```bash
./insula vm stop-all -dry-run  # Simula e lista os alvos sem encerrar
./insula vm stop-all           # Desliga as instancias gerenciadas restantes
```

O `stop-all` lê **somente as VMs cadastradas no SQLite do InsulaVM** associado a `config.yaml` e confere cada domínio no Libvirt. Não encerra VMs externas nem varre todos os domínios do host para desligá-los. Domínios ausentes são ignorados; o comando também remove overlay, ISO e chaves temporárias das instâncias efetivamente encerradas. Domínios ativos, pausados ou ainda presentes são candidatos à limpeza. Erros ou estado Libvirt desconhecido são reportados sem presumir que o domínio desapareceu; os outros alvos continuam sendo processados. O resultado exibe quantas instâncias foram encontradas, encerradas, ignoradas ou apresentaram falhas.

Em Go, use `engine.StopAllVMs(ctx, insulavm.VMStopAllOptions{DryRun: true})` para simular e `DryRun: false` para executar. A função retorna `VMStopAllResult` e possível erro agregado.

## Usar como biblioteca Go

O InsulaVM também funciona **dentro de outro processo Go**, sem iniciar binário auxiliar, API HTTP ou socket. A API pública está no pacote `insulavm`; o `package main` da raiz é somente a CLI.

```bash
# No projeto consumidor
go get github.com/ahhvahh/InsulaVM/insulavm@main
```

```go
import "github.com/ahhvahh/InsulaVM/insulavm"

// A CLI e a biblioteca utilizam exatamente as mesmas configurações.
engine, err := insulavm.New("/etc/insulavm/config.yaml")
if err != nil { return err }
result, err := engine.Run(ctx, insulavm.RunRequest{
    ManifestPath: "/srv/projeto/insula.yaml",
    Output: os.Stdout,
})
if result != nil { fmt.Println(result.LogDirectory) }
return err
```

**API experimental:** não há tag de versão estável. Use uma revisão controlada no projeto consumidor. Consulte [ARQ-004 — Biblioteca Go](docs/arquitetura/ARQ-004-biblioteca-go.md) e [exemplo de aplicação integrada](examples/go-library/main.go). O `Run` aceita manifesto em memória (`Manifest`) além de um caminho YAML.

## Solução de problemas: Libvirt não consegue ler o disco

Se `vm start` apresentar `Cannot access storage file ... overlay.qcow2 (as uid:NNNN): Permission denied`, o QEMU não tem permissão para acessar os discos/ISO gerados sob o diretório privado `storage.runs`. O libvirt executa as VMs como usuário Linux diferente do processo da CLI.

Em Debian:

```bash
sudo apt install -y acl
# Confirme o UID exato mostrado em "as uid:NNNN" no erro do seu host.
```

Informe o UID correspondente ao QEMU **do seu servidor** na seção `virtualization` de `config.yaml` (exemplo ilustrativo, substitua pelo UID real):

```yaml
virtualization:
  provider: kvm
  libvirt_uri: qemu:///system
  qemu_uid: 64055
  network: default
  default_disk_gb: 8
```

Execute `go build -o insula .` e tente novamente. O InsulaVM usa `setfacl` para permitir **somente travessia** dos diretórios ancestrais (`--x`), leitura da imagem QCOW2-base e seed ISO (`r--`) e leitura/gravação do overlay (`rw-`). **Não** concede acesso à chave SSH privada do cliente ou ao conteúdo de `cloud-init/user-data`. Se um diretório ancestral não pertencer ao usuário atual e estiver fechado, a mensagem informa como criar a ACL com `sudo setfacl` uma única vez.

O valor é opcional e **não deve ser definido como UID 0/root**. Sem `qemu_uid`, o InsulaVM não modifica ACLs. Se o hipervisor usar outro UID, ajuste a configuração. Restrições AppArmor/SELinux são independentes das ACLs; veja [documentação oficial do Libvirt](https://www.libvirt.org/drvqemu.html).

### Recuperar a imagem-base após alteração de proprietário pelo Libvirt

O Libvirt pode alterar o proprietário de **imagens QCOW2 de base** (backing images) durante a execução. Em algumas situações não restaura a propriedade de imagens usadas somente para leitura quando uma VM é encerrada. O resultado é `resolve profile image: open ...base.qcow2: permission denied` em execuções seguintes.

**Não execute uma correção de proprietário com VMs que estejam usando a imagem em execução.** Confirme com `sudo virsh -c qemu:///system list` e, com todas as VMs dependentes encerradas, recupere somente o arquivo afetado:

```bash
IMG="$PWD/data/profiles/debian13/versions/v001/base.qcow2"
sudo stat -c '%U:%G %a %n' "$IMG"
sudo chown "$(id -u):$(id -g)" "$IMG"
sudo chmod 600 "$IMG"
sudo setfacl -m u:64055:r-- "$IMG"  # ajuste o UID à configuração do host
qemu-img info "$IMG"
```

A geração do XML agora declara a imagem-base explicitamente como backing store e adiciona `<seclabel model='dac' relabel='no'/>` **somente à imagem-base**. Com `virtualization.qemu_uid` e as ACLs de leitura correspondentes, o Libvirt não precisa modificar seu proprietário. O overlay continua gerenciado normalmente pelo Libvirt. O comportamento é documentado em [cadeias de backing images](https://libvirt.org/kbase/backing_chains.html) e [segurança do XML do domínio](https://libvirt.org/formatdomain.html#security-label).

## Deploy nativo: enviar arquivo e executar script na VM

O comando nativo **`insula vm deploy`** recebe os caminhos locais de um arquivo e de um script Bash, usa a VM **já ativa** no Libvirt e executa as etapas de forma síncrona. Não cria nem desliga instâncias. O destino é `guest.destination` (`/tmp/insulavm/workspace` por padrão).

```bash
./insula vm deploy -config config.yaml \
  -id UUID_DA_VM \
  -source ./pacote.tar.gz \
  -script ./instalar.sh \
  -extract
```

O `-extract` é opcional e deve ser usado apenas com arquivos `.tar.gz` ou `.tgz` confiáveis. Sem ele, o script Bash pode abrir o arquivo diretamente. O script é executado com **`bash -e`** e recebe o nome do arquivo enviado como argumento **`$1`**. Exemplo de `instalar.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
arquivo="$1"
echo "Recebido: $arquivo"
test -f "$arquivo"
ls -lah
```

**Ordem garantida:** enviar arquivo → extrair (se solicitado) → enviar script → executar o script → confirmar sucesso. O binário imprime `DEPLOY SUCCESS: ... status=success` e retorna **código de saída `0`** somente depois de todas as etapas concluídas. Falha em qualquer transferência, extração ou comando aborta o processo e retorna **código diferente de zero**; logs são gerados pelo executor SSH. O `-sudo` opcional executa as etapas de comando via `sudo -n`, quando configurado. Para ver o código de saída, execute `echo $?` imediatamente depois.

Com arquivos arbitrários, use apenas scripts confiáveis. Como o diretório de destino é compartilhado por importações da mesma VM, evite arquivos com nomes repetidos em operações concorrentes.

## Enviar arquivo para VM ativa e executar comandos em sequência

O script [`scripts/enviar-executar-vm.sh`](scripts/enviar-executar-vm.sh) usa `insula vm import`, aguarda o fim do envio, pode extrair `.tar.gz`/`.tgz`, executa cada comando com `insula vm exec` e opcionalmente abre `insula vm shell`. **Não inicia nem interrompe a VM**. O destino é sempre `guest.destination` do `config.yaml` (padrão: `/tmp/insulavm/workspace`).

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

O script [scripts/repositorio-vm.sh](scripts/repositorio-vm.sh) usa **bash, git, tar e gzip** para criar `<nome-do-repositório>.tar.gz` a partir de `HEAD`. Ele empacota **somente arquivos versionados**, sem `.git`, alterações locais não commitadas, arquivos ignorados ou discos temporários. O pacote contém uma pasta raiz com o nome do repositório.

Para gerar somente o TAR.GZ no diretório atual:

```bash
bash scripts/repositorio-vm.sh empacotar .
# Resultado: ./InsulaVM.tar.gz
```

Para gerar o arquivo, enviá-lo à VM **já iniciada**, enviar o próprio script, extrair em `guest.destination` e abrir uma sessão SSH na pasta do projeto:

```bash
git pull --ff-only origin main
go build -o insula .
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
go run . vm start -config config.yaml -profile debian-minimal
go run . vm list -config config.yaml
go run . vm status -config config.yaml -id UUIDv7
go run . vm show -config config.yaml -id UUIDv7
go run . vm exec -config config.yaml -id UUIDv7 -command "id -un"
go run . vm exec -config config.yaml -id UUIDv7 -command "id -u" -sudo
go run . vm import -config config.yaml -id UUIDv7 -source /srv/projeto
go run . vm stop -config config.yaml -id UUIDv7
```

**Caminhos exibidos no terminal:** os logs, o comando SSH, `vm show/status`, `config check` e a localização dos relatórios mostram caminhos **relativos ao diretório atual** (`data/profiles/...`, `data/runs/...`), sem repetir `/home/usuario/...`. O armazenamento, as chaves e os contratos da biblioteca Go continuam usando caminhos absolutos internamente. Copie e execute o comando SSH **a partir do mesmo diretório** em que executou `insula`; se mudar de diretório, obtenha um novo comando pelo `vm show` ou use os caminhos completos manualmente.

Ao executar `vm start`, o InsulaVM mostra no terminal as etapas de validação do perfil, criação do overlay QCOW2, geração Cloud-Init, permissões, registro SQLite, inicialização Libvirt, obtenção do IP, SSH e validação de sudo. Uma falha inclui a etapa e o diagnóstico. Um segundo `vm start` **cria outra VM**: a CLI avisa quando já existe alguma instância ativa do mesmo perfil.

**Estado real versus histórico:** `vm list` exibe duas colunas (`SQLite` e `Libvirt`). `vm show` e `vm status` consultam o domínio no Libvirt e avisam sobre registros `creating`/`running` sem VM correspondente. A consulta não altera o banco automaticamente. `vm exec`, `vm import` e `vm stop` também validam o estado real antes de interagir com a instância ou remover arquivos. Se não for possível consultar o Libvirt, o programa apresenta erro em vez de supor que a VM está desligada.

O SQLite é compartilhado entre CLI e biblioteca no caminho `storage.database` (padrão: `<storage.root>/insula.db`). Armazena UUIDv7, UUID Libvirt, MAC, IP, usuário, impressão digital da chave do servidor, referências a chaves e estado da VM. A chave privada permanece protegida em arquivo exclusivo da instância e é removida junto com o overlay ao encerrar.

O modo `ssh.sudo_mode: passwordless` no `config.yaml` habilita `sudo -n`, sem senha ou prompt. Use somente em VMs confiáveis; omitir a opção desabilita sudo por padrão. A biblioteca oferece `Engine.StartVM`, `GetVM`, `ListVMs`, `ExecVM` e `StopVM`. Veja [ARQ-006 — Identidade, SQLite e sudo](docs/arquitetura/ARQ-006-identidade-registro-sudo.md).

## Destino compartilhado e usuário da Action

Defina **uma única pasta Linux dentro da VM** em [`config.yaml`](config.yaml). Todas as importações locais e os jobs sem `workspace` explícito usarão esse caminho em cada nova VM:

```yaml
guest:
  destination: /tmp/insulavm/workspace
ssh:
  user: insula
```

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
go run . vm import -config config.yaml -id UUIDv7 -source /srv/meu-projeto
go run . vm import -config config.yaml -id UUIDv7 -source /srv/extra.ini
```

**Ambos os uploads usam a mesma pasta.** A biblioteca expõe `Engine.ImportToVM(ctx, VMImportRequest{VMID: ..., SourcePath: ...})`, sem destino nem chave na chamada. Os estágios podem ser acompanhados por `OnProgress`; uma Action usa `RunRequest.OnImportProgress`.

Para hosts SSH externos não administrados pelo InsulaVM, o comando `insula import -host ... -source ...` utiliza `ssh.private_key` e `ssh.known_hosts` definidos no arquivo global. Nesse cenário não é possível criar automaticamente credenciais em uma máquina preexistente sem acesso de provisionamento.

Veja [ARQ-005 — Importação de arquivos](docs/arquitetura/ARQ-005-importacao-arquivos.md).

## Uso

Requisitos de desenvolvimento: Go 1.23+. Para criar VMs: Linux com KVM/QEMU, `virsh`, `qemu-img`, rede Libvirt com DHCP, acesso ao hipervisor e permissões apropriadas para as imagens. Para VMs gerenciadas: Cloud-Init e OpenSSH já instalados no convidado. Chaves são geradas automaticamente; em hosts SSH externos, é necessário cadastrar chave privada e `known_hosts` na configuração global.

```bash
# Validar config.yaml
go run . config check -config config.yaml

# Uma vez, após instalar o Debian em um QCOW2 por outro procedimento:
go run . profile register -config config.yaml \
  -name debian-minimal \
  -image /tmp/debian-instalado.qcow2 \
  -memory-mib 1024 -vcpus 1 -disk-gb 8 -os debian

go run . profile list -config config.yaml
go run . profile show -config config.yaml -name debian-minimal

# Configurar os comandos do manifesto, sem esquecer "profile: debian-minimal"
cp insula.yaml.example insula.yaml

# A imagem já precisa possuir Linux + Cloud-Init + SSH.
# Não precisa informar usuário, chave SSH ou pasta de destino.
go run . run -config config.yaml -manifest insula.yaml
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

## Documentação

- [Índice](docs/README.md)
- [ARQ-002 — Perfis e QCOW2](docs/arquitetura/ARQ-002-perfis-vm.md)
- [ARQ-003 — Configuração e catálogo](docs/arquitetura/ARQ-003-configuracao-perfis.md)
- [ARQ-004 — Biblioteca Go](docs/arquitetura/ARQ-004-biblioteca-go.md)
- [ARQ-005 — Importação de arquivos](docs/arquitetura/ARQ-005-importacao-arquivos.md)
- [ARQ-006 — Identidade, SQLite e sudo](docs/arquitetura/ARQ-006-identidade-registro-sudo.md)
- [FLUX-001 — Execução de Action](docs/fluxos/FLUX-001-execucao-manifesto.md)
- [FLUX-003 — SSH sequencial e logs](docs/fluxos/FLUX-003-execucao-sequencial-logs.md)
- [INTF-TERM-003 — Comandos de configuração e perfis](docs/interfaces/terminal/INTF-TERM-003-configuracao-perfis.md)
