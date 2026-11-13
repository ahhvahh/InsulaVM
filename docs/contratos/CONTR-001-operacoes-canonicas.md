<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-001 — Contrato canônico de operações InsulaVM (CLI e biblioteca Go)

**Estado:** contrato de referência aprovado em 2026-11-08; implementação **parcial** na `develop` consultada em 2026-11-13. A tabela abaixo diferencia recursos disponíveis e assinaturas propostas; não considerar um método existente apenas porque aparece na especificação.
**Escopo:** a mesma operação e semântica acessível por aplicação Go importadora e por linha de comando. Interfaces futuras (UDS/MCP/HTTP) devem adaptar este contrato, sem criar regras divergentes.
**Complemento obrigatório:** [CONTR-002 — Transporte STDIO](CONTR-002-transporte-stdio.md).
**Implementações existentes:** [ARQ-004 — Biblioteca](../arquitetura/ARQ-004-biblioteca-go.md), [INTF-TERM-004](../interfaces/terminal/INTF-TERM-004-gerenciar-vms.md) e [INTF-TERM-005](../interfaces/terminal/INTF-TERM-005-deploy-vm.md).

## Invariantes normativas

1. **Uma só regra de negócio:** CLI chama a biblioteca Go; não implementa o ciclo de VM, upload ou SSH em paralelo.
2. **Nomes canônicos das operações:** `CreateProfileFromISO`, `CreateVM`, `UploadArchive`, `ExecuteScript`, `ExecuteCommand`, `StopVM`, `StopAllVMs` e `FreezeVM`. Os métodos legados podem continuar como aliases para compatibilidade; uma nova integração deve preferir os canônicos assim que implementados.
3. **Mesmos argumentos lógicos e mesmos resultados:** diferenças específicas de interface (flags CLI versus estruturas Go) devem ser apenas adaptação de transporte.
4. **VM é identificada por UUIDv7**, nunca por PID ou ordem de inicialização. A fonte de verdade do estado real é Libvirt; o SQLite guarda cadastro e histórico.
5. **Diretório remoto por perfil:** a implementação atual prioriza `profile.yaml workspace.destination`, depois `profile.yaml guest.destination` e finalmente `config.yaml guest.destination` como fallback legado. Operações canônicas propostas devem respeitar o destino do perfil; chamadas legadas como `ExecVM` ainda admitem `Workspace` explícito.
6. **Arquivo canônico:** TAR.GZ (`.tar.gz`/`.tgz`). Um upload salva o pacote no diretório configurado; `Extract=true` pede extração adicional. A extração nunca deve ser obrigatória.
7. **STDIO é dado do programa remoto:** não prefixar, normalizar, truncar, combinar ou traduzir `stdin`, `stdout` ou `stderr`. Diagnóstico do InsulaVM usa canal independente dos três streams. Ver CONTR-002.
8. **Cada comando é uma execução nova** na mesma VM. Pode-se chamar múltiplos comandos sequencialmente, sem depender da persistência de um shell entre chamadas.
9. **Falhas precisam ser distinguíveis:** erro de validação/transporte/infraestrutura não é o mesmo que processo remoto que termina com exit code diferente de zero; registrar/entregar resultados parciais onde aplicável.
10. **Cancelar não é destruir automaticamente:** a VM criada explicitamente permanece até `StopVM`, `StopAllVMs` ou política de cancelamento expressa. `Run` legado continua com seu comportamento descartável documentado.
11. **Segurança:** validar arquivos e nomes, verificar host key SSH, conferir propriedade da VM antes de remover recursos, nunca executar scripts externos de origem não confiável como root.
12. **Evolução:** alterações incompatíveis às operações canônicas exigem nova versão de contrato e testes de equivalência CLI/Go.

## Superfície canônica

| Método Go (alvo) | CLI correspondente | Entradas lógicas | Retorno obrigatório | Estado na `develop` em 2026-11-13 |
| --- | --- | --- | --- | --- |
| `CreateProfileFromISO` | `insula profile create-from-iso` | Nome, ISO local, RAM MiB, vCPUs, disco GiB; modo atual não interativo | Perfil, imagem QCOW2, progresso, erro | **Implementado:** Debian 13 via ISO instaladora com Preseed; teste KVM real da revisão atual **não verificado**. Modo interativo pendente |
| `CreateVM` | `insula vm create` | Perfil registrado **ou** origem VM existente, RAM, vCPUs, disco opcional, script inicial opcional | UUIDv7, nome, estado, IP/SSH, progresso, erro | **Parcial:** `StartVMWithOptions` cria por perfil, RAM/vCPUs; disco/clone direto pendentes |
| `UploadArchive` | `insula vm upload` | UUIDv7, caminho TAR.GZ local, `Extract` | Pasta, nome do pacote, bytes, hash/verificação, resultado/erro | **Parcial:** `ImportToVM` compacta e **extrai sempre**; `DeployToVM` envia fonte e executa script |
| `ExecuteScript` | `insula vm run-script` | UUIDv7, script local, argumentos, stdin, stdout, stderr, timeout, sudo | Exit code, início/fim, logs separados, erro de transporte | **Parcial:** `DeployToVM` executa script, mas mistura progresso com saída |
| `ExecuteCommand` | `insula vm command` | UUIDv7, executável/argv ou shell explícito, stdin, stdout, stderr, timeout, sudo | Exit code, início/fim, logs separados, erro de transporte | **Parcial:** `ExecVM` SSH continua legado; `ExecRequest.QGA=true` preserva stdout/stderr binários e exit code após o término, sem persistir logs de saída, mas não fornece streaming/stdin do contrato-alvo. A API `ExecuteCommand` continua pendente |
| `StopVM` | `insula vm stop` | UUIDv7 | Confirmação ou erro; estado registrado | **Implementado** (`StopVM`) |
| `StopAllVMs` | `insula vm stop-all` | DryRun, progresso | Checked, Targets, Stopped, Skipped, Failed, erro agregado | **Implementado** (`StopAllVMs`) |
| `FreezeVM` | `insula vm freeze` | UUIDv7, nome do perfil, timeout, progresso | Perfil QCOW2 criado, estado de shutdown/limpeza, erro parcial | **Implementado** (`FreezeVM`); a origem é removida após sucesso |

As grafias CLI canônicas **não são todas executáveis hoje**. Uma CLI não deve alegar suporte enquanto o respectivo método não existir; aliases legados devem permanecer funcionando.

### Fonte de VM em `CreateVM`

- `ProfileName`: criar overlay isolado de perfil QCOW2 existente — fluxo já disponível por `StartVMWithOptions`.
- `SourceVMID`: criar cópia independente consistente de uma instância existente **sem destruí-la**; exige captura/snapshot seguro, **pendente**. Não substituir esse comportamento pelo `FreezeVM` atual, pois ele remove a origem.
- Exatamente uma origem deve ser informada. Uma ISO instaladora é entrada de `CreateProfileFromISO`, não uma imagem diretamente inicializável por `CreateVM`.
- `MemoryMiB`, `VCPUs`: zero significa padrão do perfil. `DiskGiB`: zero mantém tamanho original; expansão requer validação do QCOW2 e eventual crescimento de partições/filesystem no guest. Redução destrutiva não é permitida.

**API ISO já publicada:** [`CreateProfileFromISORequest`](../../insulavm/create_profile_iso.go) contém `Name`, `ISOPath`, `MemoryMiB`, `VCPUs`, `DiskGB`, `GuestDestination`, `Timeout` e `OnProgress`. Não contém `InstallMode`; a operação atual é não interativa.

### Contratos de dados ainda propostos (ilustrativos, não publicados na API Go)

```go
type CreateVMRequest struct {
    ProfileName string
    SourceVMID string // mutuamente exclusivo de ProfileName
    MemoryMiB int
    VCPUs int
    DiskGiB int
    StartupScriptPath string
    StopOnContextDone bool
}

type UploadArchiveRequest struct {
    VMID string
    ArchivePath string // .tar.gz / .tgz
    Extract bool
}

type CommandRequest struct {
    VMID string
    Command string
    Args []string
    Stdin io.Reader
    Stdout io.Writer
    Stderr io.Writer
    Timeout time.Duration
    Sudo bool
}

type CommandResult struct {
    VMID string
    ExitCode int
    StartedAt time.Time
    EndedAt time.Time
    LogDirectory string
}
```

O `ExecuteScript` compartilha os mesmos streams e retorno de `ExecuteCommand`; adiciona `ScriptPath` e argumentos. Para os demais métodos, preservar os tipos/resultados existentes como `VMStopAllResult` e `VMFreezeResult` enquanto for possível sem quebrar consumidores.

## Importação na API legada implementada

Os métodos **atuais** `ImportToVM(ctx, VMImportRequest)` e `ImportPath(ctx, ImportRequest)` não são sinônimos do futuro `UploadArchive`. O primeiro recebe `VMID` e `SourcePath` local e recupera a identidade da VM registrada; o segundo usa host e credenciais SSH configurados para uma máquina externa.

- O destino remoto é resolvido a partir de `workspace.destination` do perfil, depois `guest.destination` do perfil, usando o valor global apenas como fallback legado. `ImportToVM` usa o destino do perfil. `ImportPath` para hosts externos e `ExecVM` no modo SSH permitem destino explícito nas suas APIs legadas; por padrão, `ExecVM` e jobs usam o destino do perfil.
- `SourcePath` pode ser um arquivo regular ou um diretório local; a implementação rejeita symlinks/dispositivos na origem. Uma pasta envia seu conteúdo, sem acrescentar uma pasta externa; arquivos preservam o próprio nome.
- O InsulaVM cria um TAR.GZ temporário, transmite via SSH, compara o **tamanho** recebido, extrai no workspace e descarta o arquivo temporário. Os dados extraídos podem permanecer se houver erro posterior. Não existe SHA-256 remoto, extração atômica ou retomada.
- `ImportResult` confirma `Imported` e fornece `DestinationPath` e contagens quando disponíveis. `OnProgress func(ImportProgress)` entrega `Stage`, `Message`, `SourcePath`, `DestinationPath` (informativo), `BytesSent`, `TotalBytes`, `Files` e `At`. Os estágios incluem `connecting`, `validating`, `compressing`, `compressed`, `uploading`, `verifying`, `extracting` e `completed`; falhas usam `failed`.
- O progresso é separado da semântica pretendida para STDIO bruto de comandos. Atualmente a CLI apresenta mensagens de importação no terminal, não um stream de processo remoto transparente.

**Situação:** o método canônico `UploadArchive` continua **pendente**; deverá aceitar um TAR.GZ já compactado, preservá-lo na pasta de destino e apenas extrair quando `Extract=true`. Consulte [ARQ-005 — Arquitetura da importação](../arquitetura/ARQ-005-importacao-arquivos.md) e [INTF-TERM-004 — CLI implementada](../interfaces/terminal/INTF-TERM-004-gerenciar-vms.md).

## Semântica da CLI

- Para **métodos de controle** (criação, upload, stop, freeze), CLI pode apresentar mensagens humanas por padrão e modo estruturado `--output json` quando implementado. A estrutura deve representar o mesmo resultado exposto em Go.
- Para `command` e `run-script`, os três descritores são reservados exclusivamente ao processo remoto (`stdin` 0, `stdout` 1, `stderr` 2). Mensagens operacionais da CLI devem ir a arquivo de log separado ou saída de controle explicitamente solicitada, nunca misturadas aos bytes do convidado.
- O **exit status da CLI** para comando/script deve espelhar o código de saída remoto sempre que for conhecido e representável pelo SO; falha de infraestrutura deve usar código próprio reservado/documentado, sem mascarar o estado remoto. Código de saída na biblioteca é dado estruturado.
- `command`: flags próprias de VM e execução; argumentos do executável após `--`, sem concatenação insegura de shell. Se o usuário quiser shell, deve passar `/bin/sh -c` explicitamente.
- `run-script`: transfere o script confiável e o executa no workspace com argumentos fornecidos.
- Preservar comandos existentes `vm start`, `vm exec`, `vm import` e `vm deploy` até migração explicitamente versionada. Esses comandos não passam a ser STDIO bruto retroativamente sem compatibilidade.

## Retornos, progresso e observabilidade

As regras a seguir descrevem a **API canônica pretendida**. O transporte QGA implementado recebe stdout/stderr após o término do processo no guest, enquanto o SSH legado mantém formato próprio; nenhum deles implementa todo o STDIO canônico descrito em [CONTR-002](CONTR-002-transporte-stdio.md).

- Go: `(resultado, error)`; `error != nil` em falhas de infraestrutura, `ExitCode` em término real do programa, mesmo que não zero.
- `stdout` e `stderr` chegam **durante a execução**, não só ao final. `stdin` é lido do consumidor enquanto o processo remoto o aceitar. O consumidor decide se usa `os.Stdout`, arquivos, pipes ou buffers.
- Progresso de criação/upload/freeze/stop-all usa callback/eventos tipados e canal separado; nunca mistura progressos à saída bruta do comando.
- Logs físicos do host devem preservar os bytes completos por canal e sinalizar os metadados; não reescrever/limitar streams para poupar espaço silenciosamente. Definir limites de armazenamento e retenção explícitos.
- API de execução é síncrona até o processo encerrar; concorrência e cancelamento via `context.Context` por execução. Não alegar suporte a fila/reattach persistente antes de implementar.

## Matriz de compatibilidade: implementação atual versus meta

| Legado atual | Relação com contrato | Conduta de migração |
| --- | --- | --- |
| `RegisterProfile` | Importa QCOW2 já instalado (não ISO) | Preservar; não renomear para `CreateProfileFromISO` |
| `StartVMWithOptions` | Parte de `CreateVM` | Adicionar fachada canônica sem duplicar runtime |
| `ImportToVM`/`ImportPath` | Parte de `UploadArchive` | Separar upload bruto de extração; preservar APIs antigas |
| `ExecVM`/`DeployToVM` | Parte de `ExecuteCommand`/`ExecuteScript` | Introduzir transporte STDIO binário sem alterar legacy |
| `StopVM`/`StopAllVMs`/`FreezeVM` | Já possuem os nomes canônicos | Preservar assinaturas e CLI; ajustar apenas extensões compatíveis |
| `Run` | Orquestra jobs em execução descartável | Preservar como operação de mais alto nível, não substituir executores canônicos |
| `vm shell` | CLI de PTY interativo | Sem correspondente público ainda; opcional `OpenShell` futuramente, fora dos oito métodos |

## Critérios de aceite para implementação

1. Testar cada operação tanto pelo método Go quanto pelo comando CLI correspondente, com o **mesmo cenário, resultado lógico e falhas**.
2. Testar STDIO com binários, bytes NUL, linhas sem newline, stdout/stderr intercalados, entrada de stdin, comando sem saída e retorno diferente de zero.
3. Conferir que progresso/telemetria não altera stdout nem stderr remotos; verificar o código de saída do processo.
4. Testar upload TAR.GZ no `guest.destination`, com/sem extração, sem sobrescrita insegura e sem caminhos escapando do workspace.
5. Testar criação por ISO e por perfil; origem VM existente deve ser clonada sem desligar/destruir a origem.
6. Checar invariantes de isolamento e cleanup quando há erro, cancelamento, múltiplas VMs e `StopAllVMs`.
7. A documentação e os exemplos devem declarar o estado real de cada implementação. Teste unitário com mock não comprova execução KVM real.

## Referências

- [ARQ-004 — API Go existente](../arquitetura/ARQ-004-biblioteca-go.md)
- [CONTR-002 — Transporte STDIO](CONTR-002-transporte-stdio.md)
- [FLUX-002 — Instalação por ISO (proposta)](../fluxos/FLUX-002-criacao-perfil-vm.md)
- [FLUX-004 — Freeze existente](../fluxos/FLUX-004-congelar-vm.md)
- [ARQ-005 — Importação existente](../arquitetura/ARQ-005-importacao-arquivos.md)


**Origem documental preservada:** [CONTR-001-operacoes-canonicas.md](../interfaces/CONTR-001-operacoes-canonicas.md).

## Extensão — shell persistente auditável por VM

Além de `ExecuteCommand` (execução **isolada** a cada chamada), existe
`Engine.ExecuteShellCommand(ctx, ShellCommandRequest{VMID, Command, ShellID})`.
Sem `ShellID`, o motor cria um shell SSH gerenciado e retorna o UUIDv7;
com ele, reutiliza o **mesmo processo de shell**, preservando `cd` e
variáveis de ambiente. Cada chamada recebe `CommandID` próprio e o
SQLite registra comando, saída, retorno e falha. `Engine.CloseShellSession`
e `Engine.Close` encerram conexões, não VMs.

**Diferença essencial:** esta API retorna stdout/stderr **combinados** e
não substitui o contrato STDIO transparente de `ExecuteCommand`.
A sessão SSH existe apenas na instância viva de `Engine`; o SQLite
não transporta conexões entre processos. CLI: `vm shell-command`,
`vm shell-session` e `vm shell-history`.
Detalhes: [FLUX-009](../fluxos/FLUX-009-sessoes-shell.md).
