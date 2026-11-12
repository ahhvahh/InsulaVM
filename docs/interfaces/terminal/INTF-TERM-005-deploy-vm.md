<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-005 — Enviar arquivo e executar script em uma VM

**Estado:** implementado.
**Código:** [vm_cli.go](../../../cmd/insula/vm_cli.go) e [DeployToVM](../../../insulavm/deploy.go).
**Fluxo:** [FLUX-005 — Deploy](../../fluxos/FLUX-005-deploy-arquivos.md).

## Sintaxe

```bash
./insula vm deploy \
  -id UUIDv7 \
  -source ./projeto.tar.gz \
  -script ./instalar.sh \
  -extract
```

| Flag | Obrigatória | Significado |
| --- | --- | --- |
| `-id` | Sim | UUIDv7 de uma VM ativa gerenciada |
| `-source` | Sim | Arquivo regular local a enviar |
| `-script` | Sim | Arquivo Bash regular local |
| `-extract` | Não | Extrair `.tar.gz`/`.tgz` no convidado antes do script |
| `-sudo` | Não | Executar comandos remotos via `sudo -n` quando permitido |
| `-command-timeout` | Não | Tempo por comando remoto, padrão `30m` |
| `-config` | Não | Caminho de `config.yaml`, padrão no diretório atual |

`-source` e `-script` não aceitam diretórios nem symlinks. Seus nomes-base devem ser diferentes. Para enviar um diretório sem script, utilize [`vm import`](INTF-TERM-004-gerenciar-vms.md).

## Entrada, saída e execução

- **stdin/TTY:** não solicita interação; arquivos são lidos do hospedeiro.
- **stdout:** progresso de transferência e dos comandos executados, caminho de logs quando disponível e linha `DEPLOY SUCCESS: vm=... arquivo=... script=... status=success` no sucesso.
- **stderr:** erros apresentados pela CLI, com contexto da etapa; o executável retorna código `1` quando ocorre falha.
- **Código `0`:** os dois arquivos foram importados, a extração solicitada terminou e o script saiu com êxito.
- **Cancelamento:** `Ctrl+C` cancela o contexto, mas não desfaz arquivos já recebidos.

O script executa no diretório `guest.destination` do perfil e recebe em `$1` o nome-base do arquivo original. Exemplo de script local:

```bash
#!/usr/bin/env bash
set -euo pipefail
arquivo="$1"
test -f "$arquivo"
printf 'Recebido: %s\n' "$arquivo"
```

Quando `-extract` é informado, a extração ocorre **antes** de enviar/executar o script, mas o pacote original permanece no convidado. A expansão de `tar` e o script podem sobrescrever arquivos do workspace; forneça apenas arquivos confiáveis.

## Estados, erros e rastreabilidade

| Condição | Saída/resultado |
| --- | --- |
| Arquivo ausente ou formato incompatível | Falha de validação sem upload |
| VM fora do ar ou acesso SSH negado | Erro de importação/execução |
| Falha de `tar` ou Bash | Não imprime `DEPLOY SUCCESS`; retorna não zero |
| Sucesso | Resultado da operação e `DEPLOY SUCCESS` |
| Logs disponíveis | Diretório informado pelo `ExecResult.LogDirectory` |

O [FLUX-005](../../fluxos/FLUX-005-deploy-arquivos.md) contém a sequência e os estados de resultado parcial. O mecanismo de logs é explicado em [FLUX-003](../../fluxos/FLUX-003-execucao-sequencial-logs.md), e o destino físico está definido em [ARQ-005](../../arquitetura/ARQ-005-importacao-arquivos.md).
