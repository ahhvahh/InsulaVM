<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-002 — Executar manifesto via SSH

**Estado:** Implementado como ponto de entrada SSH para VM existente; provisionamento e clonagem ainda propostos.
**Fluxo efetivo:** [FLUX-003 — Execução sequencial e logs](../../fluxos/FLUX-003-execucao-sequencial-logs.md).
**Fluxo futuro de Action completa:** [FLUX-001](../../fluxos/FLUX-001-execucao-manifesto.md).

## Objetivo

Ler um manifesto `insula.yaml`, conectar à VM Debian já inicializada, executar `setup` e jobs em ordem, imprimir respostas em tempo real e gravá-las em arquivos físicos no hospedeiro.

## Comando implementado

```bash
go run . \
  -manifest insula.yaml \
  -logs-root /var/lib/insulavm/logs \
  -host 192.0.2.10:22 \
  -user insula \
  -key /home/usuario/.ssh/id_ed25519 \
  -known-hosts /home/usuario/.ssh/known_hosts \
  -command-timeout 30m
```

O endereço IP acima é ilustrativo. `INSULA_LOG_ROOT` também define o caminho de logs na ausência de `-logs-root`.

| Opção | Obrigatória? | Significado |
| --- | --- | --- |
| `-manifest` | Não; padrão `insula.yaml` | Arquivo de manifesto YAML |
| `-logs-root` | Sim, ou `INSULA_LOG_ROOT` | Pasta raiz dos logs no hospedeiro |
| `-host` | Sim | Endereço TCP da VM Debian, incluindo porta |
| `-user` | Sim | Usuário SSH da VM |
| `-key` | Sim | Chave privada SSH não criptografada usada pela CLI |
| `-known-hosts` | Sim | Lista confiável de chaves públicas de hosts SSH |
| `-command-timeout` | Não; padrão `30m` | Tempo máximo por comando remoto |

## Manifesto suportado

O arquivo [insula.yaml.example](../../../insula.yaml.example) apresenta um exemplo completo. Os campos suportados neste primeiro ciclo são `repository.name`, `action` (opcional quando há uma única pipeline), `workspace` (caminho absoluto dentro da VM), `setup` (lista de comandos) e `pipelines[].name/jobs[].name/commands/allow_failure`. Campos não reconhecidos são rejeitados.

## Entradas, saídas e códigos

- **stdin:** não solicita perguntas na CLI; scripts seguem pelo `StdinPipe` da sessão remota, não pelo terminal local.
- **stdout:** diretório de logs, mensagens de jobs, linhas recebidas e conclusão, com `[nome-do-job]`.
- **stderr:** erros da CLI (manifesto, SSH, log, falha de job); mensagens `stderr` remotas aparecem identificadas no fluxo do terminal.
- **Código de saída da CLI:** `0` se todos os jobs terminarem segundo a política `allow_failure`; `1` em erro fatal.
- **Cancelamento:** `Ctrl+C` cancela o contexto, fecha sessão remota em curso e encerra a CLI com erro.
- **Limitações:** não cria VM, não transfere o projeto e não modifica a imagem-base; `workspace` precisa existir.

## Exemplo de arquivo de log

```text
>> cd '/app/repo-workspace' && python3 -m pytest
<< ========================= test session starts =========================
<< collected 3 items
<< tests/test_main.py ... [100%]
<< 3 passed
```

Os dados de exemplo não foram obtidos de uma VM real.
