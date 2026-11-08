# InsulaVM

Ferramenta em **Go** para criar e gerenciar VMs Linux descartáveis com **QEMU/KVM + Libvirt**, executar comandos por SSH, importar arquivos e registrar logs no hospedeiro. Funciona como CLI e biblioteca Go importável.

**Implementado:** perfis QCOW2 reutilizáveis, identidade por VM com Cloud-Init NoCloud, registro SQLite, criação e encerramento de instâncias, importação, deploy, execução de pipelines, congelamento de uma VM preparada em novo perfil nomeado e encerramento em lote de VMs gerenciadas.

**Ainda não implementado:** instalar Linux automaticamente a partir de ISO instaladora, exportar ISO Live e checkout automático de repositórios Git. A imagem-base QCOW2 deve ter um sistema Linux previamente instalado e preparado com Cloud-Init, SSH, rede DHCP e, para congelamento, sudo sem senha.

## Começar

Requisitos: **Go 1.23+**, Linux com KVM/QEMU/Libvirt, `virsh`, `qemu-img`, rede Libvirt com DHCP, permissões para operar o hipervisor e geração de ISO NoCloud (`genisoimage` ou `xorriso`). O `config.yaml` é opcional na linha de comando, mas o arquivo precisa existir no local esperado.

```bash
go build -o insula .
./insula config check
./insula profile list

# Registrar um disco com Linux já instalado
./insula profile register -name debian13 -image ./debian13.qcow2 \
  -memory-mib 2048 -vcpus 2 -disk-gb 8

# Criar uma VM e consultar seu UUID
./insula vm start -profile debian13
./insula vm list
```

O registro de perfil é necessário apenas se ele ainda não existir. Consulte [GUIA-001](docs/guias/GUIA-001-operar-insulavm.md) para instalação de dependências, acesso SSH, importações e execução.

## Preparar e congelar um ambiente

```bash
# Depois de configurar a VM de origem:
./insula vm freeze -id UUID_DA_VM -name debian13-go-testes
./insula vm start -profile debian13-go-testes -memory-mib 4096 -vcpus 4
```

Para instalar dependências administrativas durante a primeira inicialização, a API Go oferece `VMStartOptions.StartupScriptPath`: caminho absoluto de script confiável, incluído no Cloud-Init e executado como root antes de a VM ficar pronta. Veja [ARQ-004](docs/arquitetura/ARQ-004-biblioteca-go.md).

O congelamento **encerra e remove a VM original** após consolidar seu overlay em um QCOW2 autônomo. Não execute `vm stop` antes de congelar, pois isso apaga o overlay temporário. Leia [FLUX-004](docs/fluxos/FLUX-004-congelar-vm.md).

## Deploy, execução e encerramento

```bash
./insula vm deploy -id UUID_DA_VM -source ./pacote.tar.gz \
  -script ./instalar.sh -extract

./insula vm stop-all -dry-run  # apenas mostra os alvos gerenciados
./insula vm stop-all           # encerra os domínios registrados
```

`stop-all` consulta o SQLite e o estado real no Libvirt, sem encerrar VMs externas não registradas. Consulte [INTF-TERM-004](docs/interfaces/terminal/INTF-TERM-004-gerenciar-vms.md) e [INTF-TERM-005](docs/interfaces/terminal/INTF-TERM-005-deploy-vm.md).

## Configuração, API Go e documentação

- [config.yaml](config.yaml) — diretórios, rede Libvirt, conta SSH, sudo e destino de importação.
- [insula.yaml.example](insula.yaml.example) — perfil, setup, imports e jobs de pipeline.
- [Exemplos de perfis](examples/profiles/debian-minimal/profile.yaml) — metadados ilustrativos; não incluem discos instalados.
- [API Go e integração](docs/arquitetura/ARQ-004-biblioteca-go.md) — `github.com/ahhvahh/InsulaVM/insulavm`.
- [GUIA-001 — Operação](docs/guias/GUIA-001-operar-insulavm.md) e [GUIA-002 — Permissões Libvirt](docs/guias/GUIA-002-permissoes-libvirt.md).
- [Índice completo da documentação](docs/README.md).

A API ainda é experimental, sem compromisso de compatibilidade de versão estável. O projeto utiliza a convenção [A-Bioma Doc Standard](docs/README.md) para organizar a documentação, sem alegar certificação formal.

## Testes

```bash
go test -race ./...
go vet ./...
```

Testes automatizados não substituem validação de ciclo completo em um hipervisor KVM/Libvirt real.
