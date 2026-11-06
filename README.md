# InsulaVM

O **InsulaVM** é um projeto open-source de automação CLI em **Go** para executar scripts e pipelines em máquinas virtuais **Debian Linux** isoladas.

**Estado atual:** arquitetura e fluxo documentados; motor ainda não implementado.

## Funcionamento planejado

1. **Criar perfil:** informar nome, RAM (MiB), vCPUs e ISO Debian local; instalar o sistema na VM e abrir terminal para ajustes.
2. **Salvar perfil:** encerrar a sessão e optar por guardar imagem-base QCOW2 persistente no diretório configurado.
3. **Executar Action:** ler `insula.yaml`, criar overlay QCOW2 descartável do perfil e iniciar instância isolada via KVM/QEMU e Libvirt.
4. Obter repositório **remoto ou local**, executar `setup` e pipelines/jobs na VM.
5. Transmitir logs de stdout/stderr em tempo real por **SSH**, apresentar resultado e descartar a instância sem alterar o perfil.

A arquitetura evita agentes próprios e pesados dentro da VM convidada.

## Documentação

- [Índice da documentação](docs/README.md)
- [Arquitetura do motor e decisões técnicas](docs/arquitetura/ARQ-001-motor-insulavm.md)
- [Arquitetura de perfis e overlays QCOW2](docs/arquitetura/ARQ-002-perfis-vm.md)
- [Fluxo de criação de perfil](docs/fluxos/FLUX-002-criacao-perfil-vm.md)
- [Interface CLI de criação de perfil](docs/interfaces/terminal/INTF-TERM-001-criar-perfil-vm.md)
- [Fluxo de execução do manifesto](docs/fluxos/FLUX-001-execucao-manifesto.md)

> Os documentos descrevem o comportamento **proposto**. Não há, neste ciclo, implementação de `go.mod`, `pkg/vm/kvm.go` ou `pkg/executor/ssh.go`.
