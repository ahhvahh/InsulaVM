# InsulaVM

CLI open-source em **Go** para executar jobs sequenciais por SSH dentro de máquinas virtuais Linux, com logs persistentes no hospedeiro.

**Implementado:** `config.yaml` global, catálogo de `profile.yaml`, registro de imagens QCOW2 já instaladas, seleção por `insula.yaml`, overlay QCOW2 descartável com QEMU/Libvirt e executor SSH sequencial com logs `>>`/`<<`.

**Ainda não implementado:** instalação de Debian/Arch a partir de ISO, criação interativa de perfil por console, Cloud-Init automático por instância e transferência do repositório. A imagem registrada precisa iniciar com DHCP e SSH; o diretório `workspace` deve existir no convidado.

## Arquivos de configuração

- [`config.yaml`](config.yaml): diretórios dos perfis, execução e logs, Libvirt e padrões SSH.
- `data/profiles/<nome>/profile.yaml`: recursos da VM, versão e caminho para `versions/v001/base.qcow2`.
- [`insula.yaml.example`](insula.yaml.example): escolhe `profile` e descreve `setup` e `pipelines`.

Exemplos de perfis (somente metadados; **sem discos instalados**):
[Debian](examples/profiles/debian-minimal/profile.yaml),
[Arch Linux](examples/profiles/arch-minimal/profile.yaml),
[Alpine](examples/profiles/alpine-minimal/profile.yaml),
[Void](examples/profiles/void-minimal/profile.yaml).

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
- [FLUX-001 — Execução de Action](docs/fluxos/FLUX-001-execucao-manifesto.md)
- [FLUX-003 — SSH sequencial e logs](docs/fluxos/FLUX-003-execucao-sequencial-logs.md)
- [INTF-TERM-003 — Comandos de configuração e perfis](docs/interfaces/terminal/INTF-TERM-003-configuracao-perfis.md)
