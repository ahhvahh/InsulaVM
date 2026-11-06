<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# Documentação — InsulaVM

**Estado geral:** arquitetura, fluxos e interface CLI propostos; implementação ainda não iniciada.

O InsulaVM planeja criar perfis persistentes de VMs Debian a partir de ISO local e executar `setup` e pipelines descritos em `insula.yaml` dentro de instâncias descartáveis desses perfis, usando KVM/QEMU, Libvirt, QCOW2 e SSH.

## Mapa de leitura

| Documento | Categoria | Conteúdo |
| --- | --- | --- |
| [ARQ-001 — Arquitetura do motor](arquitetura/ARQ-001-motor-insulavm.md) | Explicação/arquitetura | Componentes, dependências Go futuras, provisionamento, autenticação, segurança e decisões pendentes |
| [ARQ-002 — Perfis de VM](arquitetura/ARQ-002-perfis-vm.md) | Arquitetura | ISO local, imagem QCOW2 persistente, metadados e overlays descartáveis |
| [FLUX-001 — Execução de manifesto](fluxos/FLUX-001-execucao-manifesto.md) | Fluxo | Executar Actions a partir de perfil, preparar repositório, setup, jobs, logs e limpeza |
| [FLUX-002 — Criar perfil de VM](fluxos/FLUX-002-criacao-perfil-vm.md) | Fluxo | Perguntas de recursos e ISO, instalação Debian, terminal e salvamento |
| [INTF-TERM-001 — Criar perfil pelo terminal](interfaces/terminal/INTF-TERM-001-criar-perfil-vm.md) | CLI | Interação prevista com operador, confirmações, entradas e saídas |
| [README da raiz](../README.md) | Visão geral | Propósito do projeto e entrada para a documentação |

## Prefixos documentais

| Prefixo | Finalidade | Documentos atuais |
| --- | --- | --- |
| `ARQ` | Arquitetura | [ARQ-001](arquitetura/ARQ-001-motor-insulavm.md), [ARQ-002](arquitetura/ARQ-002-perfis-vm.md) |
| `FLUX` | Fluxos de execução | [FLUX-001](fluxos/FLUX-001-execucao-manifesto.md), [FLUX-002](fluxos/FLUX-002-criacao-perfil-vm.md) |
| `INTF-TERM` | Interface de terminal | [INTF-TERM-001](interfaces/terminal/INTF-TERM-001-criar-perfil-vm.md) |

## Convenções e evidências

- Aplicamos a convenção **A-Bioma Doc Standard**, inspirada em Diátaxis, arc42 simplificado e C4; isso **não** equivale a certificação formal.
- **Proposto:** comportamento ainda não existente no código.
- **Implementado:** comportamento verificado no código, teste ou contrato executável.
- **Não verificado:** hipótese ou decisão ainda sem comprovação.
- Documentos de arquitetura são fonte canônica dos componentes; documentos de fluxo, da ordem de execução e tratamento de exceções.

## Próximos documentos, quando houver decisões aprovadas

- Referência do manifesto `insula.yaml` (schema, campos e validação).
- Esquema de configuração global (raiz de dados, disco padrão, imagem, recursos) e comandos/flags CLI definitivos.
- Contrato de seleção/versão do perfil e política de clonagem para Actions.
- Procedimentos de instalação, execução e testes, após existir implementação verificável.
