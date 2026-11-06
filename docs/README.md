<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# Documentação — InsulaVM

**Estado geral:** configuração global, catálogo de perfis QCOW2, criação de overlay, inicialização de VM transitória via Libvirt e execução SSH sequencial implementados; instalação a partir da ISO, Cloud-Init automático e transferência de repositório ainda pendentes.

O InsulaVM aceita `config.yaml` global e `profile.yaml` por máquina, lê o perfil selecionado em `insula.yaml`, inicia uma VM descartável usando overlay QCOW2 ou usa uma VM SSH existente, e grava logs por job. A instalação inicial a partir de ISO e a preparação do repositório dentro do guest ainda dependem de desenvolvimento.

## Mapa de leitura

| Documento | Categoria | Conteúdo |
| --- | --- | --- |
| [ARQ-001 — Arquitetura do motor](arquitetura/ARQ-001-motor-insulavm.md) | Explicação/arquitetura | Componentes, dependências Go futuras, provisionamento, autenticação, segurança e decisões pendentes |
| [ARQ-002 — Perfis de VM](arquitetura/ARQ-002-perfis-vm.md) | Arquitetura | Instalação inicial proposta, metadados QCOW2 e overlays descartáveis |
| [ARQ-003 — Configuração e catálogo](arquitetura/ARQ-003-configuracao-perfis.md) | Arquitetura implementada | Config global, perfis por diretório, seleção e execução por Libvirt |
| [FLUX-001 — Execução de manifesto](fluxos/FLUX-001-execucao-manifesto.md) | Fluxo parcialmente implementado | Executar Actions a partir de perfil, preparar repositório, setup, jobs, logs e limpeza |
| [FLUX-002 — Criar perfil de VM](fluxos/FLUX-002-criacao-perfil-vm.md) | Fluxo proposto | Perguntas de recursos e ISO, instalação Debian, terminal e salvamento |
| [FLUX-003 — Execução sequencial e logs](fluxos/FLUX-003-execucao-sequencial-logs.md) | Fluxo implementado | Uma sessão por comando, fluxo de stdout/stderr e persistência física por job |
| [INTF-TERM-001 — Criar perfil pelo terminal](interfaces/terminal/INTF-TERM-001-criar-perfil-vm.md) | CLI proposta | Interação prevista com operador, confirmações, entradas e saídas |
| [INTF-TERM-002 — Executar manifesto](interfaces/terminal/INTF-TERM-002-executar-manifesto.md) | CLI implementada | Flags, SSH, logs, manifesto e limitações atuais |
| [INTF-TERM-003 — Configuração e perfis](interfaces/terminal/INTF-TERM-003-configuracao-perfis.md) | CLI implementada | Validar config, listar/inspecionar/registrar perfis e iniciar instância |
| [Exemplo config.yaml](../config.yaml) | Configuração | Padrões de diretórios, virtualização e SSH |
| [Exemplo de insula.yaml](../insula.yaml.example) | Exemplo de entrada | Manifesto com setup, pipelines, jobs e allow_failure |
| [README da raiz](../README.md) | Visão geral | Propósito do projeto e entrada para a documentação |

## Prefixos documentais

| Prefixo | Finalidade | Documentos atuais |
| --- | --- | --- |
| `ARQ` | Arquitetura | [ARQ-001](arquitetura/ARQ-001-motor-insulavm.md), [ARQ-002](arquitetura/ARQ-002-perfis-vm.md), [ARQ-003](arquitetura/ARQ-003-configuracao-perfis.md) |
| `FLUX` | Fluxos de execução | [FLUX-001](fluxos/FLUX-001-execucao-manifesto.md), [FLUX-002](fluxos/FLUX-002-criacao-perfil-vm.md), [FLUX-003](fluxos/FLUX-003-execucao-sequencial-logs.md) |
| `INTF-TERM` | Interface de terminal | [INTF-TERM-001](interfaces/terminal/INTF-TERM-001-criar-perfil-vm.md), [INTF-TERM-002](interfaces/terminal/INTF-TERM-002-executar-manifesto.md), [INTF-TERM-003](interfaces/terminal/INTF-TERM-003-configuracao-perfis.md) |

## Convenções e evidências

- Aplicamos a convenção **A-Bioma Doc Standard**, inspirada em Diátaxis, arc42 simplificado e C4; isso **não** equivale a certificação formal.
- **Proposto:** comportamento ainda não existente no código.
- **Implementado:** comportamento verificado no código, teste ou contrato executável.
- **Não verificado:** hipótese ou decisão ainda sem comprovação.
- Documentos de arquitetura são fonte canônica dos componentes; documentos de fluxo, da ordem de execução e tratamento de exceções.

## Próximos documentos, quando houver decisões aprovadas

- Contrato completo do manifesto `insula.yaml` para instalar ISO, transferir projetos e selecionar versões de perfis (o parser atual seleciona nome do perfil e jobs).
- Provisionamento ISO interativo, Cloud-Init com identidade própria por VM e gerenciamento de perfis versionados.
- Transferência de repositórios e inicialização do workspace remoto.
- Sanitização de segredos de logs, operação de limpeza e testes de ponta a ponta com uma VM real.
