<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-004 — Teste manual: Debian 13 mini.iso, VM preparada e ISO Live

**Estado:** script criado; a instalação e o boot real não foram executados em host KVM.
**Contratos:** [INTF-TERM-007](../interfaces/terminal/INTF-TERM-007-instalar-debian-iso.md) e [INTF-TERM-006](../interfaces/terminal/INTF-TERM-006-iso-live.md).
**Referência de testes integrais:** [GUIA-003](GUIA-003-testes.md).

## Objetivo

Baixar a ISO de instalação oficial Debian 13 amd64 menor que a netinst convencional, instalar uma VM do InsulaVM, abrir um terminal para personalização, congelar a VM como perfil persistente QCOW2 e produzir/validar uma ISO Live inicializável.

A ISO escolhida é Debian trixie 'netboot/mini.iso' (aproximadamente 64 MiB). **O instalador depende de acesso à internet**, assim como a preparação de live-boot. A ISO final contém um sistema Live extraído do filesystem do disco; ela **não é uma cópia bit a bit do QCOW2**.

## Pré-requisitos

- Linux x86_64 com '/dev/kvm', Libvirt/QEMU, rede DHCP configurada e acesso à internet dentro das VMs;
- Go 1.23+, 'virsh', 'qemu-img', 'virt-install', 'openssl', 'guestmount', 'guestunmount', 'ssh' e 'curl';
- Para exportação de ISO Live: 'grub-mkrescue', 'mksquashfs', 'lsinitramfs', 'xorriso', GRUB BIOS/UEFI e 'live-boot' no convidado;
- Configuração de teste no 'config.yaml' com 'ssh.sudo_mode: passwordless' e permissões Libvirt adequadas para o usuário que executa o script.

O script executa o build da CLI no armazenamento temporário do teste e usa sempre as operações de produção da CLI, que delegam à biblioteca Go.

## Execução completa

Na raiz do repositório:

~~~bash
bash scripts/tests/manual-debian-mini-iso.sh all --config /caminho/absoluto/config.yaml
~~~

O comando abre o shell SSH da VM. Faça suas configurações, por exemplo:

~~~bash
sudo apt-get update
sudo apt-get install -y git ca-certificates
cat /etc/debian_version
exit
~~~

Após 'exit', o script **não considera o fechamento do shell um desligamento**. Ele exige que você digite 'EXPORTAR' para congelar e remover a VM original, salvar um QCOW2 separado e criar a ISO Live. Em seguida, tenta iniciar uma nova VM com a ISO e executar 'test -r /etc/debian_version'. O marcador de êxito integral é 'INSULA_DEBIAN_MINI_MANUAL_OK'.

## Operação por etapas — recomendada para preparar a VM por vários dias

~~~bash
bash scripts/tests/manual-debian-mini-iso.sh download
bash scripts/tests/manual-debian-mini-iso.sh install
bash scripts/tests/manual-debian-mini-iso.sh start
bash scripts/tests/manual-debian-mini-iso.sh open
# Pode repetir open quantas vezes quiser enquanto a VM estiver rodando.
bash scripts/tests/manual-debian-mini-iso.sh status
bash scripts/tests/manual-debian-mini-iso.sh export
bash scripts/tests/manual-debian-mini-iso.sh verify
~~~

O estado é armazenado em '~/.local/state/insulavm/debian-mini/' (pode ser alterado com '--workdir DIR'); não apague essa pasta enquanto precisar reencontrar a VM. Os nomes padrão são 'debian13-mini-base' e 'debian13-mini-preparado'. Podem ser alterados com '--profile' e '--frozen'; mantenha **os mesmos parâmetros** entre as etapas.

## Resultado e segurança

- Baixa a ISO oficial exclusivamente por HTTPS e compara SHA256 com 'SHA256SUMS' obtido no repositório oficial. A assinatura GPG do checksum não é verificada pelo script.
- O primeiro perfil é uma instalação Debian 13 independente em QCOW2. A VM de manutenção usa um overlay descartável.
- A etapa 'export' só acontece após confirmação; ela **para e remove a VM de manutenção** depois de salvar o novo perfil. O perfil 'debian13-mini-preparado' mantém o disco completo em QCOW2 e a ISO fica em seu subdiretório 'exports'.
- Se a exportação Live falhar **depois** do congelamento, o perfil permanece. Corrija a causa e use 'insula profile export-iso -name debian13-mini-preparado', sem destruir outra VM.
- Os arquivos de cache e de identificação da VM são privados ao operador. Não utilize o perfil com segredos de aplicações que não tenham sido removidos.
- Este fluxo só poderá ser considerado validado de ponta a ponta após execução no seu servidor KVM. O GitHub Actions permanece desativado.

## Fontes técnicas

- Debian: https://deb.debian.org/debian/dists/trixie/main/installer-amd64/current/images/netboot/
- Checksums: https://deb.debian.org/debian/dists/trixie/main/installer-amd64/current/images/SHA256SUMS
- Instalação de mídia netboot: https://wiki.debian.org/DebianInstaller/CreateUSBMedia
- Virt-install: https://manpages.debian.org/trixie/virt-install/virt-install.1.en.html
