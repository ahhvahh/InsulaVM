<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# FLUX-009 — Shell persistente por VM e histórico SQLite

**Estado:** implementado no motor Go; testes em KVM/SSH real ainda pendentes.
**Código:** [shell_sessions.go](../../insulavm/shell_sessions.go), [transport.go](../../internal/shell/transport.go), [shell_history.go](../../pkg/registry/shell_history.go).
**Interfaces:** Go `Engine.ExecuteShellCommand`; CLI `vm shell-command`, `vm shell-session` e `vm shell-history`.

**Origem preservada:** [fluxo anterior](FLUX-008-sessoes-shell.md).

## Objetivo

Executar comandos em uma VM gerenciada por UUIDv7, preservando o estado do
mesmo shell remoto entre chamadas (`cd`, `export`, variáveis e ambiente),
com histórico completo dos comandos submetidos e das respostas recebidas.

## Contrato Go

```go
engine, err := insulavm.New("/etc/insulavm/config.yaml")
if err != nil { return err }
defer engine.Close() // encerra SSH, não para VMs

first, err := engine.ExecuteShellCommand(ctx, insulavm.ShellCommandRequest{
    VMID: vmID,
    Command: "cd /tmp && pwd",
})
if err != nil { return err }
fmt.Println(first.ShellID, first.CommandID, string(first.Output), first.ExitCode)

next, err := engine.ExecuteShellCommand(ctx, insulavm.ShellCommandRequest{
    VMID: vmID,
    ShellID: first.ShellID,
    Command: "pwd",
})
if err != nil { return err }
fmt.Println(string(next.Output))
sessions, err := engine.ShellSessionsForVM(ctx, vmID)
if err != nil { return err }
_ = sessions
history, err := engine.ShellCommandHistory(ctx, first.ShellID)
if err != nil { return err }
_ = history
return engine.CloseShellSession(ctx, first.ShellID)
```

`ShellCommandRequest` exige `VMID` e `Command`, aceita `ShellID` opcional e
`CommandTimeout` (padrão 30 minutos). Na primeira chamada, o motor resolve
SSH e `guest.destination` do perfil, autentica por chave e known_hosts e
inicia `sh -s`. Cada comando recebe um `CommandID` UUIDv7. Um código de
saída diferente de zero é retornado em `ExitCode`, com `Completed=true`,
sem ser confundido com falha de transporte.

### Persistência

`shell_sessions`: `id`, `vm_id`, `status`, `created_at`, `updated_at`,
`closed_at`.

`shell_commands`: `id`, `shell_id`, `vm_id`, `command`, `output` (BLOB),
`status`, `exit_code` (nulo se desconhecido), `error`, `started_at`,
`finished_at`.

A linha de comando é gravada como `running` antes do envio ao SSH. Ao receber
resposta ou erro, o motor grava a saída e o resultado em SQLite. Uma falha
de rede preserva a saída parcial e torna a sessão `disconnected`. Os dados
sobrevivem à parada da VM e ao encerramento do processo.

### Segurança e limites

- Chave privada e conteúdo de credenciais SSH nunca são salvos na tabela.
- **Comandos e suas saídas podem conter dados sensíveis**: não enviar senhas,
  tokens ou chaves privadas em comandos auditados. Proteger e rotacionar o
  arquivo SQLite conforme a política de retenção do ambiente; o arquivo é
  criado com permissões 0600.
- Cada shell pertence a **uma única VM**, e o UUID do registro é conferido
  com o Libvirt antes da execução.
- Saída combinada stdout+stderr limitada a **4 MiB por comando**; ao exceder,
  a sessão é desconectada e o resultado parcial fica no banco.
- Comando limitado a 64 KiB. São aceitas expressões confiáveis de shell.
  Este modo **não** substitui o contrato de STDIO binário independente de
  `ExecuteCommand` previsto em CONTR-002.
- Comandos por sessão são serializados; sessões diferentes podem executar
  em paralelo.
- `CloseShellSession` e `Engine.Close` liberam conexões; `StopVM` fecha
  sessões locais da VM. `Engine.Close` **não** desliga a VM.
- Se um comando executar `exit`, quebrar o protocolo, bloquear o stdin ou
  perder SSH, o shell é descartado e deve ser criado novamente.
- Sessão em memória **não sobrevive ao encerramento do processo**. O banco
  guarda **histórico**, não o socket SSH ou o estado de `sh`. Após reiniciar
  o processo, criar outra sessão (sem tentar reutilizar o UUID anterior).
  Um status `active` deixado por encerramento abrupto indica último estado
  persistido, **não prova conexão ativa**.

## CLI

Comando avulso, que abre e fecha seu próprio shell:

```sh
insula vm shell-command -id UUID_VM -command 'ls -la'
```

Processo CLI de longa duração para reutilizar o shell entre várias linhas
do mesmo stdin:

```sh
printf 'pwd\ncd /tmp\npwd\n' | insula vm shell-session -id UUID_VM
```

Consultar o histórico depois de encerrada a sessão:

```sh
insula vm shell-history -shell-id UUID_SHELL
insula vm shell-history -id UUID_VM  # listar sessoes de uma VM
```

`vm shell-history` emite objetos JSON Lines (com saída UTF-8 textual).
O método Go preserva bytes binários integralmente no `[]byte` e no BLOB.
A CLI avulsa não pode reutilizar um `shell_id` de outra invocação,
porque não há daemon/serviço InsulaVM para manter o socket aberto.
Para sessões por ID entre processos distintos, será preciso um serviço
gerenciador de sessões, fora do escopo atual.

## Testes e evidências

- Unitários: enquadramento da resposta, código de saída, NUL/bytes binários,
  limite de saída, validação do comando, duas execuções em um transporte.
- SQLite: comandos e respostas persistidos entre aberturas da base; estados
  `completed` e `failed`; rejeição de IDs duplicados.
- CLI: validação de argumentos e consulta de histórico inexistente.
- Pendentes: teste com guest KVM real, SSH autenticado, repetição de `cd`,
  cancelamento, perda de rede, parada e limpeza da VM, concorrência e
  `go test -race ./...`.
