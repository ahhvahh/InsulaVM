<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# Documentação — InsulaVM

**Estado geral:** executor SSH, parser do manifesto e gravação de logs implementados; criação de VMs, perfis e transferência do repositório ainda propostos.

O InsulaVM já executa comandos SSH sequenciais descritos em `insula.yaml` contra um Debian existente e grava logs por job. O provisionamento KVM/QEMU/Libvirt, os perfis QCOW2 e a gestão de repositórios são partes planejadas da arquitetura.

## Mapa de leitura

| Documento | Categoria | Conteúdo |
| --- | --- | --- |
| [ARQ-001 — Arquitetura do motor](arquitetura/ARQ-001-motor-insulavm.md) | Explicação/arquitetura | Componentes, dependências Go futuras, provisionamento, autenticação, segurança e decisões pendentes |
| [ARQ-002 — Perfis de VM](arquitetura/ARQ-002-perfis-vm.md) | Arquitetura | ISO local, imagem QCOW2 persistente, metadados e overlays descartáveis |
| [FLUX-001 — Execução de manifesto](fluxos/FLUX-001-execucao-manifesto.md) | Fluxo | Executar Actions a partir de perfil, preparar repositório, setup, jobs, logs e limpeza |
| [FLUX-002 — Criar perfil de VM](fluxos/FLUX-002-criacao-perfil-vm.md) | Fluxo proposto | Perguntas de recursos e ISO, instalação Debian, terminal e salvamento |
| [FLUX-003 — Execução sequencial e logs](fluxos/FLUX-003-execucao-sequencial-logs.md) | Fluxo implementado | Uma sessão por comando, fluxo de stdout/stderr e persistência física por job |
| [INTF-TERM-001 — Criar perfil pelo terminal](interfaces/terminal/INTF-TERM-001-criar-perfil-vm.md) | CLI proposta | Interação prevista com operador, confirmações, entradas e saídas |
| [INTF-TERM-002 — Executar manifesto](interfaces/terminal/INTF-TERM-002-executar-manifesto.md) | CLI implementada | Flags, SSH, logs, manifesto e limitações atuais |
| [Exemplo de insula.yaml](../insula.yaml.example) | Exemplo de entrada | Manifesto com setup, pipelines, jobs e allow_failure |
| [README da raiz](../README.md) | Visão geral | Propósito do projeto e entrada para a documentação |

## Prefixos documentais

| Prefixo | Finalidade | Documentos atuais |
| --- | --- | --- |
| `ARQ` | Arquitetura | [ARQ-001](arquitetura/ARQ-001-motor-insulavm.md), [ARQ-002](arquitetura/ARQ-002-perfis-vm.md) |
| `FLUX` | Fluxos de execução | [FLUX-001](fluxos/FLUX-001-execucao-manifesto.md), [FLUX-002](fluxos/FLUX-002-criacao-perfil-vm.md), [FLUX-003](fluxos/FLUX-003-execucao-sequencial-logs.md) |
| `INTF-TERM` | Interface de terminal | [INTF-TERM-001](interfaces/terminal/INTF-TERM-001-criar-perfil-vm.md), [INTF-TERM-002](interfaces/terminal/INTF-TERM-002-executar-manifesto.md) |

## Convenções e evidências

- Aplicamos a convenção **A-Bioma Doc Standard**, inspirada em Diátaxis, arc42 simplificado e C4; isso **não** equivale a certificação formal.
- **Proposto:** comportamento ainda não existente no código.
- **Implementado:** comportamento verificado no código, teste ou contrato executável.
- **Não verificado:** hipótese ou decisão ainda sem comprovação.
- Documentos de arquitetura são fonte canônica dos componentes; documentos de fluxo, da ordem de execução e tratamento de exceções.

## Próximos documentos, quando houver decisões aprovadas

- Contrato completo do manifesto `insula.yaml` para criação de VM, clone e referência/versionamento de perfis (o parser atual abrange apenas o subconjunto de execução SSH).
- Esquema global para imagens, CPU, RAM, rede, diretórios de dados e política de retenção de logs.
- Integração CLI com provisionamento KVM/QEMU/Libvirt e transferência de projeto para a VM.
- Sanitização de segredos de logs, operação de limpeza e testes de ponta a ponta com uma VM real.
