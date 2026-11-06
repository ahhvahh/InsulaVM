<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# Documentação — InsulaVM

**Estado geral:** arquitetura e fluxo propostos; implementação ainda não iniciada.

O InsulaVM planeja executar `setup` e pipelines descritos em `insula.yaml` dentro de VMs Debian temporárias usando KVM/QEMU, Libvirt, Cloud-Init e SSH.

## Mapa de leitura

| Documento | Categoria | Conteúdo |
| --- | --- | --- |
| [ARQ-001 — Arquitetura do motor](arquitetura/ARQ-001-motor-insulavm.md) | Explicação/arquitetura | Componentes, dependências Go futuras, provisionamento, autenticação, segurança e decisões pendentes |
| [FLUX-001 — Execução de manifesto](fluxos/FLUX-001-execucao-manifesto.md) | Fluxo | Diagrama de sequência, preparação da VM, origem local/remota, setup, jobs, logs, erros e limpeza |
| [README da raiz](../README.md) | Visão geral | Propósito do projeto e entrada para a documentação |

## Prefixos documentais

| Prefixo | Finalidade | Documentos atuais |
| --- | --- | --- |
| `ARQ` | Arquitetura | [ARQ-001](arquitetura/ARQ-001-motor-insulavm.md) |
| `FLUX` | Fluxos de execução | [FLUX-001](fluxos/FLUX-001-execucao-manifesto.md) |

## Convenções e evidências

- Aplicamos a convenção **A-Bioma Doc Standard**, inspirada em Diátaxis, arc42 simplificado e C4; isso **não** equivale a certificação formal.
- **Proposto:** comportamento ainda não existente no código.
- **Implementado:** comportamento verificado no código, teste ou contrato executável.
- **Não verificado:** hipótese ou decisão ainda sem comprovação.
- Documentos de arquitetura são fonte canônica dos componentes; documentos de fluxo, da ordem de execução e tratamento de exceções.

## Próximos documentos, quando houver decisões aprovadas

- Referência do manifesto `insula.yaml` (schema, campos e validação).
- Interface CLI (comandos, argumentos, saída e códigos de retorno).
- Procedimentos de instalação, execução e testes, após existir implementação verificável.
