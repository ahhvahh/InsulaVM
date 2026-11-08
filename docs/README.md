<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# Documentação — InsulaVM

**Estado geral:** configuração, perfis QCOW2, criação de VMs transitórias, Cloud-Init NoCloud, SQLite, importação, deploy, congelamento de VM e execução SSH com logs estão implementados. Instalação a partir de ISO instaladora, exportação de ISO Live e checkout Git automático continuam não implementados.

O InsulaVM aceita `config.yaml` global e `profile.yaml` por máquina, lê o perfil selecionado em `insula.yaml`, pode importar arquivos/pastas locais em TAR.GZ antes dos jobs, inicia uma VM descartável usando overlay QCOW2 ou usa uma VM SSH existente, e grava logs por job. A CLI e outros projetos Go podem usar o mesmo motor através do pacote importável `insulavm`. O Linux deve estar instalado em uma imagem com Cloud-Init; a ISO NoCloud de configuração já é gerada, mas a instalação do sistema a partir de ISO instaladora permanece pendente. O InsulaVM registra as VMs em SQLite.

## Mapa de leitura

| Documento | Categoria | Conteúdo |
| --- | --- | --- |
| [CONTR-001 — Operações canônicas](interfaces/CONTR-001-operacoes-canonicas.md) | Contrato normativo | Nomes e semântica compartilhados entre CLI e biblioteca Go, parâmetros, retornos, mapeamento legado e estados de implementação |
| [CONTR-002 — STDIO fiel](interfaces/CONTR-002-transporte-stdio.md) | Contrato normativo | Transporte binário de stdin/stdout/stderr, exit code, callbacks, erros e testes de equivalência |
| [CONTR-003 — Inventário de operações](interfaces/CONTR-003-inventario-operacoes.md) | Inventário | Mapa CLI/Go, redundâncias reais e plano de consolidação |
| [GUIA-001 — Operar VMs](guias/GUIA-001-operar-insulavm.md) | Guia operacional | Como iniciar, configurar, congelar, importar e executar rotinas em VMs |\n| [GUIA-002 — Permissões Libvirt](guias/GUIA-002-permissoes-libvirt.md) | Diagnóstico | UID do QEMU, ACLs e segurança dos discos QCOW2 |\n| [ARQ-001 — Arquitetura do motor](arquitetura/ARQ-001-motor-insulavm.md) | Explicação/arquitetura | Componentes, dependências Go futuras, provisionamento, autenticação, segurança e decisões pendentes |
| [ARQ-002 — Perfis de VM](arquitetura/ARQ-002-perfis-vm.md) | Arquitetura | Instalação inicial proposta, metadados QCOW2 e overlays descartáveis |
| [ARQ-003 — Configuração e catálogo](arquitetura/ARQ-003-configuracao-perfis.md) | Arquitetura implementada | Config global, `guest.destination` compartilhado, perfis e Libvirt |
| [ARQ-004 — Biblioteca Go](arquitetura/ARQ-004-biblioteca-go.md) | Integração implementada | API Go para orquestradores externos, `Run(ctx, request)`, retorno estruturado e ciclo de vida |
| [ARQ-005 — Importação TAR.GZ](arquitetura/ARQ-005-importacao-arquivos.md) | Transferência implementada | Destino configurado globalmente, TAR.GZ via SSH, extração e progresso por callback |
| [ARQ-006 — SSH, SQLite e sudo](arquitetura/ARQ-006-identidade-registro-sudo.md) | Instâncias registradas | UUIDv7, NoCloud seed ISO, SSH Ed25519, IP/MAC e escalonamento sudo sem senha |
| [FLUX-001 — Execução de manifesto](fluxos/FLUX-001-execucao-manifesto.md) | Fluxo parcialmente implementado | Executar Actions a partir de perfil, preparar repositório, setup, jobs, logs e limpeza |
| [FLUX-002 — Criar perfil de VM](fluxos/FLUX-002-criacao-perfil-vm.md) | Fluxo proposto | Perguntas de recursos e ISO, instalação Debian, terminal e salvamento |
| [FLUX-003 — Execução sequencial e logs](fluxos/FLUX-003-execucao-sequencial-logs.md) | Fluxo implementado | Uma sessão por comando, fluxo de stdout/stderr e persistência física por job |
| [FLUX-004 — Congelar VM](fluxos/FLUX-004-congelar-vm.md) | Fluxo implementado | Preparar identidade, desligar origem, consolidar QCOW2 e publicar perfil reutilizável |
| [FLUX-005 — Deploy](fluxos/FLUX-005-deploy-arquivos.md) | Fluxo implementado | Transferir arquivo e script, extrair opcionalmente e executar com erro propagado |
| [INTF-TERM-001 — Criar perfil pelo terminal](interfaces/terminal/INTF-TERM-001-criar-perfil-vm.md) | CLI proposta | Interação prevista com operador, confirmações, entradas e saídas |
| [INTF-TERM-002 — Executar manifesto](interfaces/terminal/INTF-TERM-002-executar-manifesto.md) | CLI implementada | Flags, SSH, logs, manifesto e limitações atuais |
| [INTF-TERM-003 — Configuração e perfis](interfaces/terminal/INTF-TERM-003-configuracao-perfis.md) | CLI implementada | Validar config, listar/inspecionar/registrar perfis e iniciar instância |
| [INTF-TERM-004 — Gerenciamento de VMs](interfaces/terminal/INTF-TERM-004-gerenciar-vms.md) | CLI implementada | Criar, listar, consultar, congelar e encerrar VMs individuais ou em lote |
| [INTF-TERM-005 — Deploy](interfaces/terminal/INTF-TERM-005-deploy-vm.md) | CLI implementada | Flags, saídas e erros de upload + script por UUID |
| [PERS-001 — Instâncias SQLite](persistencia/PERS-001-registro-instancias.md) | Persistência | Schema vm_instances, estados e relação lógica com perfis |\n| [Exemplo config.yaml](../config.yaml) | Configuração | Pasta de destino comum, virtualização e SSH |
| [Exemplo de consumo da biblioteca](../examples/go-library/main.go) | Exemplo Go | Outro processo controla execução in-processo, sem sockets |
| [Exemplo de insula.yaml](../insula.yaml.example) | Exemplo de entrada | Manifesto com usuário SSH opcional, imports sem destino, setup e jobs |
| [README da raiz](../README.md) | Visão geral | Propósito do projeto e entrada para a documentação |

## Prefixos documentais

| Prefixo | Finalidade | Documentos atuais |
| --- | --- | --- |
| `CONTR` | Contrato público canônico da CLI e biblioteca | [CONTR-001](interfaces/CONTR-001-operacoes-canonicas.md), [CONTR-002](interfaces/CONTR-002-transporte-stdio.md), [CONTR-003](interfaces/CONTR-003-inventario-operacoes.md) |
| `GUIA` | Procedimentos e resolução de problemas | [GUIA-001](guias/GUIA-001-operar-insulavm.md), [GUIA-002](guias/GUIA-002-permissoes-libvirt.md) |\n| `ARQ` | Arquitetura | [ARQ-001](arquitetura/ARQ-001-motor-insulavm.md), [ARQ-002](arquitetura/ARQ-002-perfis-vm.md), [ARQ-003](arquitetura/ARQ-003-configuracao-perfis.md), [ARQ-004](arquitetura/ARQ-004-biblioteca-go.md), [ARQ-005](arquitetura/ARQ-005-importacao-arquivos.md), [ARQ-006](arquitetura/ARQ-006-identidade-registro-sudo.md) |
| `FLUX` | Fluxos de execução | [FLUX-001](fluxos/FLUX-001-execucao-manifesto.md), [FLUX-002](fluxos/FLUX-002-criacao-perfil-vm.md), [FLUX-003](fluxos/FLUX-003-execucao-sequencial-logs.md), [FLUX-004](fluxos/FLUX-004-congelar-vm.md), [FLUX-005](fluxos/FLUX-005-deploy-arquivos.md) |
| `INTF-TERM` | Interface de terminal | [INTF-TERM-001](interfaces/terminal/INTF-TERM-001-criar-perfil-vm.md), [INTF-TERM-002](interfaces/terminal/INTF-TERM-002-executar-manifesto.md), [INTF-TERM-003](interfaces/terminal/INTF-TERM-003-configuracao-perfis.md), [INTF-TERM-004](interfaces/terminal/INTF-TERM-004-gerenciar-vms.md), [INTF-TERM-005](interfaces/terminal/INTF-TERM-005-deploy-vm.md) |

| `PERS` | Persistência | [PERS-001](persistencia/PERS-001-registro-instancias.md) |\n\n## Convenções e evidências

- Aplicamos a convenção **A-Bioma Doc Standard**, inspirada em Diátaxis, arc42 simplificado e C4; isso **não** equivale a certificação formal.
- **Proposto:** comportamento ainda não existente no código.
- **Implementado:** comportamento verificado no código, teste ou contrato executável.
- **Não verificado:** hipótese ou decisão ainda sem comprovação.
- Documentos de contrato `CONTR` são a **fonte normativa da API pública**; documentos de arquitetura são a fonte dos componentes; documentos de fluxo, da ordem de execução e tratamento de exceções. Em divergência, atualizar as demais interfaces para respeitar os contratos canônicos sem declarar implementado o que ainda não está no código.

## Próximos documentos, quando houver decisões aprovadas

- Versionamento semântico e estabilização da API pública Go após testes de compatibilidade com um projeto consumidor.
- Contrato completo do manifesto `insula.yaml` para instalar ISO, transferir projetos e selecionar versões de perfis (o parser atual seleciona nome do perfil e jobs).
- Provisionamento ISO instaladora interativo e gerenciamento de perfis versionados. Cloud-Init com identidade própria já foi implementado.
- Checkout automático Git; transferência de arquivos locais e inicialização do diretório global já são suportadas.
- Sanitização de segredos de logs, operação de limpeza e testes de ponta a ponta com uma VM real.
