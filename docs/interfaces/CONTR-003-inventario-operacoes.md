<!-- A-BIOMA-DOC-OBSOLETO -->
<!-- A-BIOMA-DOC-MIGRADO -->

> **Conteúdo lido e migrado para:** [REF-001-inventario-operacoes.md](../referencia/REF-001-inventario-operacoes.md). Este arquivo é mantido para rastreabilidade histórica; use o destino como fonte canônica.

# CONTR-003 — Inventário de operações e redução de redundâncias

**Estado:** revisão de interface em 2026-11-08; consolidação progressiva, sem alteração de comportamento das APIs Go existentes.
**Fonte normativa:** [CONTR-001 — Operações canônicas](CONTR-001-operacoes-canonicas.md) e [CONTR-002 — STDIO](CONTR-002-transporte-stdio.md).
**Implementação atual:** [main.go](../../main.go), [vm_cli.go](../../vm_cli.go), [insulavm/engine.go](../../insulavm/engine.go).

## Comandos atualmente executáveis

| Intenção | CLI | Biblioteca Go | Situação |
| --- | --- | --- | --- |
| Validar configuração | `insula config check` | `New`, `Configuration` | implementado |
| Listar perfis | `insula profile list` | `ListProfiles` | implementado |
| Consultar perfil | `insula profile show` | `GetProfile` | implementado |
| Registrar QCOW2 pronto | `insula profile register` | `RegisterProfile` | implementado |
| Iniciar VM de perfil | `insula vm start` | `StartVMWithOptions` | implementado para perfil, RAM, vCPUs; disco por instância pendente |
| Listar VMs | `insula vm list` | `ListVMStatus` | implementado |
| Consultar VM/estado | `insula vm status` | `GetVMStatus` | implementado; alias redundante `vm show` retirado da CLI nesta revisão |
| Terminal PTY interativo | `insula vm shell` | sem fachada Go dedicada | somente CLI |
| Executar comando | `insula vm exec` | `ExecVM` | implementado; STDIO bruto pendente |
| Importar para VM gerenciada | `insula vm import` | `ImportToVM` | implementado; extrai conteúdo |
| Importar para host SSH externo | `insula import` | `ImportPath` | implementado; não é a mesma operação que importar por UUID |
| Enviar arquivo e executar script | `insula vm deploy` | `DeployToVM` | implementado; inclui upload de arquivo + script |
| Executar jobs por manifesto | `insula run` | `Run` | implementado; orquestra pipeline |
| Congelar VM em perfil | `insula vm freeze` | `FreezeVM` | implementado; remove origem depois do sucesso |
| Parar VM | `insula vm stop` | `StopVM` | implementado |
| Parar todas | `insula vm stop-all` | `StopAllVMs` | implementado |

Além destes, `insula help`, `insula -h` e `insula --help` exibem a ajuda. O atalho implícito de pipeline (`insula` sem o subcomando `run`) foi removido nesta revisão: pipelines exigem `insula run` explícito. A biblioteca mantém `Run` normalmente.

## Operações canônicas alvo ainda ausentes ou parciais

| Contrato alvo | CLI alvo | Estado e relação com legado |
| --- | --- | --- |
| `CreateProfileFromISO` | `insula profile create-from-iso` | implementado na develop em modo automático; boot real não verificado |
| `CreateVM` | `insula vm create` | parcial por `vm start`; clonar origem e alterar disco pendentes |
| `UploadArchive` | `insula vm upload` | parcial por `vm import`; salvar TAR.GZ sem extrair ainda pendente |
| `ExecuteScript` | `insula vm run-script` | parcial por `vm deploy`; STDIO fiel pendente |
| `ExecuteCommand` | `insula vm command` | parcial por `vm exec`; stdin + streams independentes + exit code pendentes |
| `StopVM` | `insula vm stop` | implementado |
| `StopAllVMs` | `insula vm stop-all` | implementado |
| `FreezeVM` | `insula vm freeze` | implementado |

**Não anunciar como executáveis** comandos alvo que ainda não constam do código.

## Duplicidades reais e não duplicidades

| Par atual | Avaliação | Ação |
| --- | --- | --- |
| `vm show` e `vm status` | comandos equivalentes, mesma implementação | retirar alias `vm show` da CLI e manter `vm status` |
| `insula` implícito versus `insula run` | mesma execução de manifesto; acesso implícito via parser de argumentos da raiz | eliminar atalho implícito; exigir `insula run` |
| `StartVM`, `StartVMWithProgress`, `StartVMWithOptions` | wrappers de configuração/feedback do mesmo runtime | quando `CreateVM` estiver funcional, tornar os antigos compatíveis/deprecados antes de remoção versionada |
| `vm start` versus futuro `vm create` | sobreposição parcial | implementar `CreateVM` uma vez no Engine; migrar sem quebrar CLI atual |
| `vm exec` versus futuro `vm command` | sobreposição parcial | motor único; não alterar formato de STDIO da CLI legada sem versionamento |
| `vm import` versus futuro `vm upload` | sobreposição parcial; `import` extrai | motor de transferência único com `Extract` explícito |
| `vm deploy` versus futuro `vm run-script` | composição de operações, não equivalência completa | `DeployToVM` como composição de upload e execução; sem segundo motor |
| `vm shell` versus `vm exec` | **não redundante**: PTY interativo versus comando individual | preservar |
| `insula import` versus `vm import` | **não redundante**: host externo versus UUID gerenciado | preservar até existir contrato separado de host externo |
| `insula run` versus `vm exec` | **não redundante**: pipeline/manifesto e VM transitória versus comando numa VM existente | preservar |
| `vm list` versus `vm status` | **não redundante**: coleção versus detalhe de um UUID | preservar |
| `GetVM` versus `GetVMStatus` | **não redundante**: somente registro persistido versus registro + consulta ao estado Libvirt | preservar; documentar distinção |
| `ListVMs` versus `ListVMStatus` | **não redundante**: históricos SQLite versus históricos + estado Libvirt | preservar; documentar distinção |
| `profile show` versus `vm status` | **não redundante**: perfil de imagem versus instância | preservar |
| `vm stop` versus `vm stop-all` | **não redundante**: alvo único versus conjunto | preservar |

## Política de consolidação

1. Remover **somente aliases comprovadamente idênticos**, como `vm show` e o atalho de `run` sem subcomando, nesta revisão.
2. Não remover/exportar API Go existente prematuramente; os nomes canônicos ainda são em grande parte especificações não implementadas.
3. Implementar os contratos canônicos como fachadas sobre o **mesmo** núcleo de VM/SSH/transferência já usado pela CLI.
4. Após a paridade e testes de compatibilidade, marcar wrappers legados como deprecados e removê-los em versão de quebra explicitamente aprovada.
5. Comandos novos e biblioteca devem expor entradas e retornos equivalentes, segundo CONTR-001. STDIO fiel, segundo CONTR-002.

## Validação

- CLI `vm show` deve responder uso inválido; `vm status` continua aceito.
- CLI `insula` e `insula -manifest ...` sem `run` devem informar uso inválido; `insula run -manifest ...` continua roteado para `Run`.
- Verificar que nenhum guia público sugere `vm show`.
- Validar que `vm shell`, `vm exec`, `vm import`, `vm deploy` e `run` seguem distintos e não foram removidos.
- Executar `go test ./...` para verificar regressões; execução real KVM/Libvirt requer host apropriado.

## Extensões experimentais adicionadas

| Ação | CLI | Go | Evidência |
| --- | --- | --- | --- |
| Clonar perfil por nome | `profile clone` | `CloneProfile` | `insulavm/profile_clone.go` |
| Exportar Debian Live | `profile export-iso` | `ExportProfileLiveISO` | `insulavm/export_iso.go` |
| Iniciar guest por ISO Live | `vm start-iso` | `StartVMFromISO` | `insulavm/live_iso.go`, `pkg/vm/live_iso.go` |
| Console serial | `vm console` | `OpenConsole` | `insulavm/console.go` |
| Shutdown 30 s e força após timeout | `vm stop` | `StopVMWithOptions` | `insulavm/stop_policy.go` |

Implementação no código da branch de trabalho; **compilação e teste real KVM ainda não comprovados**. Instalador ISO netinst permanece pendente. [Fluxo de testes integrais](../fluxos/FLUX-006-teste-integral-iso-vm.md).

## Acrescentadas na develop — 2026-11-08

| Intenção | CLI | Go | Situação |
| --- | --- | --- | --- |
| Instalar Debian 13 da ISO e publicar perfil | `profile create-from-iso` | `CreateProfileFromISO` | implementado, KVM real pendente |
| Exportar ISO Live de VM gerenciada em uma operação | `vm export-iso` | `ExportVMToLiveISO` | implementado, KVM real pendente |

Ambas utilizam o mesmo motor da biblioteca. Não foi criada uma segunda implementação do ciclo de VM.
