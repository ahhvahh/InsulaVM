<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-016 — Inicializar VM pela ISO instaladora Alpine

**Estado:** implementação disponível no código; validação de boot gráfico em KVM real pendente.
**Fluxo:** CLI `insula vm install-iso` e Go `Engine.StartVMFromInstallerISO`.
**Escopo:** ISO Alpine Virtual 3.24.4, 1 vCPU, 500 MiB de RAM, disco QCOW2 isolado de 8 GiB.

## Pré-requisitos

- Linux x86_64 com QEMU/KVM, Libvirt e `virsh`, `qemu-img`, `/dev/kvm`
- `config.yaml` válido; acesso autorizado ao `qemu:///system`
- ISO `./iso/alpine-virt-3.24.4-x86_64.iso` acessível ao QEMU
- Para diretórios privados, configurar `virtualization.qemu_uid` correto e instalar `acl`, conforme GUIA-002
- Rede Libvirt `default` iniciada se `network.enabled=true`

## Preparação

Na raiz do projeto, usando a implementação atual:

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o insula ./cmd/insula
test -c /dev/kvm
test -s ./iso/alpine-virt-3.24.4-x86_64.iso
./insula config check -config config.yaml
mkdir -p data/profiles/alpine-virt
cp examples/profiles/alpine-virt/profile.yaml data/profiles/alpine-virt/profile.yaml
./insula profile show -config config.yaml -name alpine-virt
```

`disk.start_iso` relativo sempre resolve contra o diretório do `config.yaml`, não contra `data/profiles`.
`network.ipv4.mode: dhcp` solicita uma NIC Libvirt; a distribuição convidada é responsável pela rede.
`mode: static` é rejeitado por enquanto: não é possível garantir IP fixo em ISO Live original sem configuração dentro do guest.

## Iniciar e acessar a VM

```bash
./insula vm install-iso -config config.yaml -profile alpine-virt
# Copie o id=UUID retornado:
./insula vm list -config config.yaml
./insula vm status -config config.yaml -id UUID
./insula vm console -config config.yaml -id UUID

# Se o Alpine não apresentar login no console serial, utilize VNC no host:
sudo virsh -c qemu:///system domdisplay UUID --type vnc
remote-viewer "$(sudo virsh -c qemu:///system domdisplay UUID --type vnc)"
```

No console Alpine Virtual, `root` inicia sem senha na mídia Live de testes. Para sair de `virsh console`, pressione `Ctrl+]`. Para instalar manualmente em `/dev/vda`, use `setup-alpine` e selecione o modo `sys`.

**Importante:** `vm install-iso` confirma apenas que o domínio Libvirt está em estado running, não que o Alpine completou o boot. Consulte o console serial ou a tela VNC para verificar o sistema convidado. Não exige DHCP, Cloud-Init ou SSH. O VNC usa porta automática, escuta somente em `127.0.0.1` no host e mantém a porta serial; não fornece autenticação adicional, portanto é destinado a ambientes locais confiáveis. `remote-viewer` é fornecido pelo pacote `virt-viewer` (quando instalado).

## Limites da versão inicial

O domínio é **transitório**. Cada execução cria seu próprio UUIDv7 e QCOW2 em `storage.runs/insula-vm-*`, registrado em SQLite. O comando `./insula vm stop -id UUID` **apaga o disco temporário**; não equivale a salvar a instalação. O domínio desaparece ao desligar o sistema convidado e ainda não há comando de religamento a partir desse QCOW2. Preserve/exporte o disco por procedimento explícito antes de remover a instância. Não use este fluxo como armazenamento definitivo sem operação posterior de publicação do disco.

A ISO original é somente leitura. `vm shell` e `vm exec` exigem SSH configurado no convidado e não são meios de acesso inicial à ISO Alpine. Para inspeção, use `vm console` (serial) ou `virsh domdisplay UUID --type vnc` com um visualizador VNC no host. A inclusão de VNC não modifica domínios já em execução: é preciso criar uma nova VM para receber a nova configuração. Preserve o QCOW2 existente antes de descartar a VM antiga.

## Referências

- [Libvirt domain XML](https://libvirt.org/formatdomain.html)
- [Libvirt virsh create/console](https://www.libvirt.org/manpages/virsh.html)
- [GUIA-002 — Permissões](GUIA-002-permissoes-libvirt.md)
- [GUIA-005 — Teste integral Alpine](GUIA-005-alpine-iso.md)
