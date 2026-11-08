<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-003 — Executar testes unitários e integrais

**Estado:** scripts criados; boot KVM real não verificado.
**Fluxo:** [FLUX-006](../fluxos/FLUX-006-teste-integral-iso-vm.md).

## Unitários

Executar na raiz do projeto:

```bash
bash scripts/tests/unit.sh
```


O script executa bash -n, go vet ./... e go test -race ./... sem exigir /dev/kvm.

## Integrais

**Executar somente em host dedicado** com Libvirt/KVM, rede DHCP, configuração e imagem Debian preparadas. O teste cria e destrói instâncias reais; os perfis criados ficam no storage configurado para inspeção.

```bash
bash scripts/tests/integration.sh \
  --config /etc/insulavm/test-config.yaml \
  --base-qcow2 /var/lib/insulavm-test/debian13-cloud.qcow2
```


- O perfil precisa ter Debian 13 instalado, SSH, Cloud-Init e sudo sem senha.
- A montagem Live requer guestmount, guestunmount, grub-mkrescue, mksquashfs, lsinitramfs, grub-pc-bin/grub-efi-amd64-bin e xorriso.
- O guest precisa de acesso ao APT para instalar live-boot e gerar initramfs compatível.
- O script valida upload, execução com token, congelamento e clonagem, boot da ISO Live e encerramento com timeout de 30 segundos.
- Para isolar problemas da ISO, use --skip-live-iso; isso **não** passa no critério de aceite integral da ISO.

O marcador de êxito é INSULA_INTEGRATION_OK. Em erro, o script termina com código diferente de zero. Não considerar sucesso apenas porque existe uma imagem .iso.

## Troubleshooting

- /dev/kvm indisponível: executar no host com virtualização ativada.
- Libvirt sem permissão: ajustar acesso ao URI conforme [GUIA-002](GUIA-002-permissoes-libvirt.md).
- DHCP/SSH falha: conferir rede Libvirt, Cloud-Init e usuário configurado no guest.
- Falha na ISO: conferir live-boot no initramfs e dependências do host.
- Falha ao encerrar: analisar estado real com insula vm status -id UUID e virsh; não apagar discos de instâncias ativas.
