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
