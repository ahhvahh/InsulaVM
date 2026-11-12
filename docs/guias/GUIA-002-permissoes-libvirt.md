<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-002 — Corrigir permissões de armazenamento Libvirt

**Estado:** procedimento para o hospedeiro Linux. Verifique o UID efetivo do QEMU antes de qualquer ajuste; valores exibidos são ilustrativos.
**Arquitetura relacionada:** [ARQ-003](../arquitetura/ARQ-003-configuracao-perfis.md).

## Diagnosticar o acesso do QEMU aos discos

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

Execute `go build -o insula ./cmd/insula` e tente novamente. O InsulaVM usa `setfacl` para permitir **somente travessia** dos diretórios ancestrais (`--x`), leitura da imagem QCOW2-base e seed ISO (`r--`) e leitura/gravação do overlay (`rw-`). **Não** concede acesso à chave SSH privada do cliente ou ao conteúdo de `cloud-init/user-data`. Se um diretório ancestral não pertencer ao usuário atual e estiver fechado, a mensagem informa como criar a ACL com `sudo setfacl` uma única vez.

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
