# InsulaVM

O **InsulaVM** é um projeto open-source de automação CLI em **Go** para executar scripts e pipelines em máquinas virtuais **Debian Linux** isoladas.

**Estado atual:** arquitetura e fluxo documentados; motor ainda não implementado.

## Funcionamento planejado

1. Ler o manifesto `insula.yaml`.
2. Provisionar VM Debian descartável via **KVM/QEMU e Libvirt**, com configuração inicial por **Cloud-Init**.
3. Obter repositório **remoto ou local** dentro da VM.
4. Executar comandos de `setup` e, em seguida, pipelines/jobs encadeados na raiz do projeto.
5. Transmitir logs de stdout/stderr em tempo real via **SSH** ao terminal do hospedeiro.
6. Apresentar o resultado e remover os recursos temporários.

A arquitetura evita agentes próprios e pesados dentro da VM convidada.

## Documentação

- [Índice da documentação](docs/README.md)
- [Arquitetura do motor e decisões técnicas](docs/arquitetura/ARQ-001-motor-insulavm.md)
- [Fluxo de execução do manifesto](docs/fluxos/FLUX-001-execucao-manifesto.md)

> Os documentos descrevem o comportamento **proposto**. Não há, neste ciclo, implementação de `go.mod`, `pkg/vm/kvm.go` ou `pkg/executor/ssh.go`.
