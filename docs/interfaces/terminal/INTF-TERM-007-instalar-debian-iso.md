<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-007 — Instalar Debian 13 a partir de ISO e publicar perfil

**Estado:** código implementado na `develop`; instalação/boot real em KVM não verificados.
**Contratos:** [CONTR-001](../CONTR-001-operacoes-canonicas.md).
**Fluxo:** [FLUX-002](../../fluxos/FLUX-002-criacao-perfil-vm.md).
**Testes:** [GUIA-003](../../guias/GUIA-003-testes.md).

## CLI

Baixe a ISO oficial Debian 13 netinst e valide o SHA256 antes de criar o perfil:

```bash
ISO="$(bash scripts/baixar-debian.sh)"
# Opções: --output-dir /isos  e  --force (substituir arquivo com checksum inválido)
```

O script grava por padrão em `data/isos` (ignorado pelo Git), reutiliza arquivos válidos e escreve somente o caminho absoluto no stdout. Isso não instala o SO; a instalação é realizada pelo comando seguinte.

```bash
insula profile create-from-iso \
  -config /etc/insulavm/config.yaml \
  -name debian13-build \
  -iso "$ISO" \
  -memory-mib 2048 -vcpus 2 -disk-gb 12 \
  -install-timeout 90m

insula vm start -config /etc/insulavm/config.yaml -profile debian13-build
```

**Entradas:** `-name` (perfil exclusivo), `-iso` (ISO local absoluta e regular com ISO9660), `-memory-mib` (1.024 a 1.048.576), `-vcpus` (1 a 256), `-disk-gb` (8 a 4.096) e `-install-timeout` (padrão 90 min; máximo 6 h). Sem valores de recurso na CLI, RAM=2048, vCPUs=2 e disco=`virtualization.default_disk_gb`.

**Saída:** `PROFILE ISO OK: name=<perfil> image=<qcow2> sha256=<sha256-iso>` e mensagens de etapas em `stderr`. Erro retorna código de saída 1. Não solicita senha ou responde perguntas do instalador; usa Debian Installer Preseed.

## Biblioteca Go

```go
engine, err := insulavm.New("/etc/insulavm/config.yaml")
if err != nil { return err }
created, err := engine.CreateProfileFromISO(ctx, insulavm.CreateProfileFromISORequest{
    Name:"debian13-build",
    ISOPath:"/isos/debian-13-amd64-netinst.iso",
    MemoryMiB:2048, VCPUs:2, DiskGB:12,
    Timeout:90*time.Minute,
})
if err != nil { return err }
_ = created.ImagePath
```

O outro projeto deve importar `github.com/ahhvahh/InsulaVM/insulavm` como módulo Go. O método emite progresso por `OnProgress`; não precisa executar a CLI nem trafegar por HTTP.

## ISO Live diretamente da VM

```bash
insula vm export-iso -config /etc/insulavm/config.yaml \
  -id UUIDv7 -name debian13-live -prepare-live-boot
```

A biblioteca usa `ExportVMToLiveISO(ctx, VMExportISORequest{VMID, Name, PrepareLiveBoot:true})`. Essa operação **encerra e remove a VM original** como parte do congelamento; se a montagem Live falhar, o perfil congelado é preservado para novo `profile export-iso -name`.

## Pré-requisitos e limites

- Debian 13 `amd64` instalador ISO; BIOS, rede Libvirt com DHCP, acesso ao mirror Debian; não é um instalador genérico de todas as distribuições.
- No host: KVM, Libvirt, `virsh`, `qemu-img`, `virt-install`, `openssl`, `guestmount` e `guestunmount`; ACLs em `virtualization.qemu_uid` quando necessário.
- O instalador cria usuário `ssh.user` com senha aleatória desconhecida e hash SHA-512 crypt temporário, root sem login e pacotes Cloud-Init, SSH, sudo e live-boot. `ssh.sudo_mode: passwordless` configura acesso administrativo sem senha somente quando solicitado na configuração.
- O disco de instalação é temporário e somente publicado após desligamento confirmado e inspeção offline do sistema; ISO original preservada. Sem garantias de compatibilidade de todas as versões de `virt-install` antes de teste real.
- O wizard com console/edição manual e opção Salvar/Descartar permanece proposto em [INTF-TERM-001](INTF-TERM-001-criar-perfil-vm.md).

Referências técnicas: [Debian 13 — Preseed](https://www.debian.org/releases/trixie/amd64/apb) e [virt-install](https://manpages.debian.org/testing/virt-install/virt-install.1.en.html).
