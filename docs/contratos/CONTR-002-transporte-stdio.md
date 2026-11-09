<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# CONTR-002 — Transporte STDIO fiel para comandos e scripts

**Estado:** contrato canônico aprovado; implementação de transporte fiel **pendente** (o executor legado processa linhas e adiciona prefixos).
**Origem:** [CONTR-001](CONTR-001-operacoes-canonicas.md).
**Código legado:** [pkg/executor/ssh.go](../../pkg/executor/ssh.go), [insulavm/exec.go](../../insulavm/exec.go) e [insulavm/deploy.go](../../insulavm/deploy.go).

## Objetivo

A aplicação consumidora deve enxergar a execução do processo convidado como processo local: escrevendo stdin, recebendo stdout e stderr em tempo real, aguardando a conclusão e consultando o exit code. O InsulaVM gerencia somente transporte autenticado, ciclo de vida, cancelamento e registro em arquivos. Não interpreta, converte nem reescreve a carga útil dos streams.

## Contrato entre aplicação, InsulaVM e convidado

```mermaid
sequenceDiagram
    participant A as Aplicação Go ou CLI
    participant I as InsulaVM Engine
    participant S as Sessão SSH isolada
    participant P as Processo no guest
    A->>I: CommandRequest(VMID, comando, args, stdin, stdout, stderr)
    I->>I: Valida VM em SQLite e Libvirt; autentica SSH
    I->>S: Abre sessão SSH para esta chamada
    S->>P: Executa comando com argv seguro
    A->>I: stdin (bytes)
    I->>P: stdin (bytes, sem conversão)
    P-->>I: stdout (bytes)
    I-->>A: stdout (mesmos bytes)
    P-->>I: stderr (bytes)
    I-->>A: stderr (mesmos bytes)
    P-->>I: exit status
    I-->>A: CommandResult(ExitCode, início/fim, log) + erro de transporte eventual
```

## Regras

1. `Stdin io.Reader`, `Stdout io.Writer`, `Stderr io.Writer` são canais **independentes e opcionais**; `nil` significa EOF para stdin ou descarte do respectivo fluxo de saída, sem prejudicar a execução do processo.
2. Os bytes precisam ser preservados. Não acrescentar nome de job, timestamps, rótulos, newline ou mensagens próprias no stream da aplicação.
3. Nunca exigir separação por linhas para transmitir dados; `io.Copy`/stream binário ou equivalente. Fluxos podem conter bytes NUL, UTF-8 parcial e arquivos binários.
4. Ler stdout e stderr concorrentemente para evitar deadlock; preservação de ordem **por stream**, sem prometer ordenação total entre os dois.
5. Fechar stdin remoto no EOF do leitor e aguardar confirmação de término do processo; cancelamento/timeout deve encerrar a sessão e retornar erro de cancelamento, sem transformar isso em exit code de comando finalizado.
6. A execução remota deve usar executável e argumentos separados, escapando corretamente cada componente para a semântica do SSH remoto; para interpretar uma expressão de shell o consumidor passa explicitamente `/bin/sh -c '...'`.
7. Executar por padrão como usuário SSH não-root da VM. `Sudo` exige permissão já concedida e `sudo -n`, sem prompt de senha; não imprimir segredos.
8. Entregar o `ExitCode` real quando o SSH reportar término. `exit != 0` é **resultado normal de processo**, não erro de transporte. Se a sessão não fornece status confiável, retornar erro próprio, sem inventar `0`.
9. Escrever logs separados de stdout e stderr (bytes íntegros) e metadados de execução em espaço próprio do host; não injetar logs nos streams.
10. Progresso de upload/criação e mensagens de diagnóstico devem seguir callbacks/eventos separados. Em CLI canônica `vm command`/`vm run-script`, reservar fd 0/1/2 ao convidado e disponibilizar metadados por resultado/arquivo de controle.
11. Para `ExecuteScript`, primeiro transferir o script local confiável à pasta `guest.destination`, depois executá-lo no guest com argumentos. A transferência não pode aparecer em stdout/stderr do comando; seus eventos são independentes.
12. A cada chamada, iniciar nova sessão/processo. Estado do disco e arquivos persistem enquanto a mesma VM permanecer ativa, mas não assumir que `cd`, variáveis exportadas ou descritores permaneçam entre comandos.

## Formato Go desejado (ainda não implementado)

```go
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
    ExitCode int
    StartedAt time.Time
    EndedAt time.Time
    LogDirectory string
}

func (e *Engine) ExecuteCommand(
    ctx context.Context,
    req CommandRequest,
) (*CommandResult, error)
```

A versão final precisa documentar erro específico quando não for possível obter exit code, sem confundi-lo com um término legítimo com `ExitCode=0`.

## Diferença em relação ao executor atual

O executor atual [pkg/executor/ssh.go](../../pkg/executor/ssh.go) abre sessões SSH por comando e lê stdout/stderr, mas **converte em linhas**, coloca marcadores como `[job]`/`[stderr]` na saída configurada e registra `<<` nos logs. O `ExecResult` atual não carrega `ExitCode`, e `ExecVM` tem apenas `Output io.Writer` para os dois canais. Esses são comportamentos legados que devem permanecer para chamadas antigas enquanto se implementam `ExecuteCommand` e `ExecuteScript` com STDIO fiel.

A CLI `vm shell` possui PTY próprio. Uma sessão PTY **não** é equivalente ao transporte STDIO de três canais, pois frequentemente combina stdout/stderr e aplica semântica de terminal. Manter PTY separado.

## Testes necessários

- Comparar bytes de `stdin`, `stdout` e `stderr`, incluindo bytes não textuais, sem linhas e sem truncamento.
- Verificar código de saída `0`, `1`, `127` e não zero arbitrário.
- Cancelamento de processo e timeout; fechamento de stdin/SSH sem goroutines bloqueadas.
- Comando sem stdin, stream com alto volume, consumidor que fornece escritor com erro e processos concorrentes.
- Confirmar que warnings internos e logs de progresso **não contaminam** os canais.
- Comparar execução pelo pacote Go e CLI com o mesmo processo e dados.

## Referências

- [CONTR-001 — Operações canônicas](CONTR-001-operacoes-canonicas.md)
- [FLUX-003 — Logs atuais](../fluxos/FLUX-003-execucao-sequencial-logs.md)
- [pkg/executor/ssh.go](../../pkg/executor/ssh.go)


**Origem documental preservada:** [CONTR-002-transporte-stdio.md](../interfaces/CONTR-002-transporte-stdio.md).
