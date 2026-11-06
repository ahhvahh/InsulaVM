# InsulaVM

CLI open-source em **Go** para executar jobs sequenciais por SSH dentro de máquinas virtuais Linux, com logs persistentes no hospedeiro.

**Implementado:** instâncias por UUIDv7 registradas em SQLite, SSH individual automatizado por Cloud-Init NoCloud, elevação controlada com sudo, biblioteca Go importável em `github.com/ahhvahh/InsulaVM/insulavm`, CLI usando a mesma API, `config.yaml` global, catálogo de `profile.yaml`, registro de imagens QCOW2 já instaladas, seleção por `insula.yaml`, overlay QCOW2 descartável com QEMU/Libvirt e executor SSH sequencial com logs `>>`/`<<`, além de importação TAR.GZ de arquivos e diretórios do hospedeiro para a VM, com progresso no console ou callback Go.

**Ainda não implementado:** instalação de Debian/Arch a partir de ISO instaladora, criação interativa de perfil por console e checkout automático de repositórios Git (a importação manual de arquivos locais já funciona). A imagem registrada precisa ter Cloud-Init, SSH e DHCP; o diretório `workspace` deve existir no convidado.

## Arquivos de configuração

- [`config.yaml`](config.yaml): diretórios dos perfis, execução, logs e banco SQLite; Libvirt, SSH e política sudo.
- `data/profiles/<nome>/profile.yaml`: recursos da VM, versão e caminho para `versions/v001/base.qcow2`.
- [`insula.yaml.example`](insula.yaml.example): escolhe `profile` e descreve `setup` e `pipelines`.

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

// O motor pode ser compartilhado por múltiplas chamadas no mesmo processo.
engine, err := insulavm.New("/etc/insulavm/config.yaml")
if err != nil { return err }
result, err := engine.Run(ctx, insulavm.RunRequest{
    ManifestPath: "/srv/projeto/insula.yaml",
    Host: "192.0.2.10:22", // Exemplo: VM já inicializada
    User: "insula",
    PrivateKey: "/etc/insulavm/id_ed25519",
    KnownHosts: "/etc/insulavm/known_hosts",
    Output: os.Stdout,
})
if result != nil { fmt.Println(result.LogDirectory) }
return err
```

**API experimental:** não há tag de versão estável. Use uma revisão controlada no projeto consumidor. Consulte [ARQ-004 — Biblioteca Go](docs/arquitetura/ARQ-004-biblioteca-go.md) e [exemplo de aplicação integrada](examples/go-library/main.go). O `Run` aceita manifesto em memória (`Manifest`) além de um caminho YAML.

## Criar instâncias com SSH automático e sudo

Uma **imagem QCOW2 com Linux já instalado e preparado com Cloud-Init** é inicializada com uma ISO **NoCloud/CIDATA gerada pelo InsulaVM**, contendo um usuário `insula` e chaves Ed25519 individuais. A ISO de configuração não instala Linux; o instalador Debian/Arch por ISO é uma funcionalidade diferente e ainda pendente.

```bash
go run . vm start -config config.yaml -profile debian-minimal
go run . vm list -config config.yaml
go run . vm show -config config.yaml -id UUIDv7
go run . vm exec -config config.yaml -id UUIDv7 -command "id -un"
go run . vm exec -config config.yaml -id UUIDv7 -command "id -u" -sudo
go run . vm stop -config config.yaml -id UUIDv7
```

O SQLite é compartilhado entre CLI e biblioteca no caminho `storage.database` (padrão: `<storage.root>/insula.db`). Armazena UUIDv7, UUID Libvirt, MAC, IP, usuário, impressão digital da chave do servidor, referências a chaves e estado da VM. A chave privada permanece protegida em arquivo exclusivo da instância e é removida junto com o overlay ao encerrar.

O modo `ssh.sudo_mode: passwordless` no `config.yaml` habilita `sudo -n`, sem senha ou prompt. Use somente em VMs confiáveis; omitir a opção desabilita sudo por padrão. A biblioteca oferece `Engine.StartVM`, `GetVM`, `ListVMs`, `ExecVM` e `StopVM`. Veja [ARQ-006 — Identidade, SQLite e sudo](docs/arquitetura/ARQ-006-identidade-registro-sudo.md).

## Importar arquivos ou pastas para uma VM já iniciada

O comando recebe **um arquivo ou uma pasta** no hospedeiro, compacta o conteúdo em TAR.GZ, envia pelo SSH e extrai no diretório informado dentro da VM. Uma pasta transfere seu conteúdo; um arquivo transfere seu nome e conteúdo. A confirmação é emitida após o `tar` remoto concluir com sucesso.

```bash
go run . import -config config.yaml \
  -host 192.168.122.10:22 \
  -source /home/usuario/meu-projeto \
  -dest /tmp/meu-projeto \
  -user insula \
  -key /home/usuario/.ssh/id_ed25519 \
  -known-hosts /home/usuario/.ssh/known_hosts
```

**Biblioteca:** `engine.ImportPath(ctx, insulavm.ImportRequest{...})` aceita `OnProgress func(insulavm.ImportProgress)` e `Output io.Writer`. Cada estágio (`connecting`, `validating`, `compressing`, `uploading`, `verifying`, `extracting`, `completed` ou `failed`) pode ser exibido pela aplicação consumidora em tempo real.

Também pode usar `imports:` no `insula.yaml` para transferir arquivos **antes dos jobs**, inclusive quando o InsulaVM cria uma VM temporária a partir do perfil:

```yaml
imports:
  - source: /home/usuario/meu-projeto
    destination: /tmp/meu-projeto
```

Em `Engine.Run`, os eventos são recebidos por `RunRequest.OnImportProgress` e os resultados por `RunResult.Imports`. Consulte [ARQ-005 — Importação de arquivos](docs/arquitetura/ARQ-005-importacao-arquivos.md) para detalhes e limitações.

## Uso

Requisitos de desenvolvimento: Go 1.23+. Para criar VMs: Linux com KVM/QEMU, `virsh`, `qemu-img`, rede Libvirt com DHCP, acesso ao hipervisor e permissões apropriadas para as imagens. Para jobs SSH: conta remota, chave privada e arquivo `known_hosts` confiável.

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

# Execução a partir de perfil: inicia VM temporária e remove o overlay ao final
go run . run -config config.yaml -manifest insula.yaml \
  -user insula \
  -key /home/usuario/.ssh/id_ed25519 \
  -known-hosts /home/usuario/.ssh/known_hosts

# Teste contra VM já iniciada (não cria VM/overlay):
go run . run -config config.yaml -manifest insula.yaml \
  -host 192.0.2.10:22 -user insula \
  -key /home/usuario/.ssh/id_ed25519 \
  -known-hosts /home/usuario/.ssh/known_hosts
```

Os caminhos SSH e IP são ilustrativos e precisam ser substituídos. No modo de VM automática, a imagem precisa ter SSH configurado e o host deve verificar a identidade do servidor por `known_hosts`. O processo Libvirt deve conseguir acessar os diretórios QCOW2; ACLs/grupos são responsabilidade da instalação.

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
