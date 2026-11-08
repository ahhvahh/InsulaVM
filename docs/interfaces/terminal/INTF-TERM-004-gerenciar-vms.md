<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-004 — Gerenciar e congelar instâncias de VM

**Estado:** implementado na CLI Go.
**Plataforma:** hospedeiro Linux com Libvirt/QEMU/KVM.
**Código:** [vm_cli.go](../../../vm_cli.go), [start_options.go](../../../insulavm/start_options.go), [stop_all.go](../../../insulavm/stop_all.go) e [freeze.go](../../../insulavm/freeze.go).

## Correspondência com contratos canônicos

Os comandos abaixo são a **CLI legada efetivamente implementada**. A nomenclatura de referência de operações e a exigência de paridade entre CLI e biblioteca constam em [CONTR-001](../CONTR-001-operacoes-canonicas.md). Os futuros `vm command` e `vm run-script` devem obedecer a [CONTR-002](../CONTR-002-transporte-stdio.md), sem alterar silenciosamente os formatos legados de `vm exec`/`vm deploy`.

## Comandos e parâmetros

| Comando | Parâmetros específicos | Efeito / vínculo ao fluxo |
| --- | --- | --- |
| `insula vm start` | `-profile` obrigatório; `-memory-mib`, `-vcpus` opcionais | Cria uma VM de perfil já instalado; [ARQ-003](../../arquitetura/ARQ-003-configuracao-perfis.md) |
| `insula vm list` | Nenhum além de `-config` | Lista os registros com estados SQLite/Libvirt; [PERS-001](../../persistencia/PERS-001-registro-instancias.md) |
| `insula vm status` | `-id` obrigatório | Exibe identidade/SSH e estados; [PERS-001](../../persistencia/PERS-001-registro-instancias.md) |
| `insula vm shell` | `-id` obrigatório; `-directory` opcional | Abre shell SSH na VM gerenciada; [ARQ-006](../../arquitetura/ARQ-006-identidade-registro-sudo.md) |
| `insula vm exec` | `-id`, `-command` obrigatórios; `-workspace`, `-sudo` opcionais | Executa comando e retorna logs; [FLUX-003](../../fluxos/FLUX-003-execucao-sequencial-logs.md) |
| `insula vm import` | `-id`, `-source` obrigatórios | Copia arquivo/diretório para `guest.destination`; [ARQ-005](../../arquitetura/ARQ-005-importacao-arquivos.md) |
| `insula vm freeze` | `-id` e `-name` obrigatórios; `-shutdown-timeout` opcional | Congela a VM em perfil QCOW2, encerrando a origem; [FLUX-004](../../fluxos/FLUX-004-congelar-vm.md) |
| `insula vm stop` | `-id` obrigatório | Encerra e remove disco temporário de uma VM gerenciada; [ARQ-006](../../arquitetura/ARQ-006-identidade-registro-sudo.md) |
| `insula vm stop-all` | `-dry-run` opcional; não aceita `-id` | Processa somente domínios registrados neste InsulaVM; [PERS-001](../../persistencia/PERS-001-registro-instancias.md) |

Todos esses subcomandos aceitam `-config` opcional, cujo padrão é `config.yaml` relativo ao diretório atual. O YAML é necessário mesmo quando `-config` não foi informado. O comando `deploy` está documentado em [INTF-TERM-005](INTF-TERM-005-deploy-vm.md).

## Exemplo: reutilização do mesmo perfil

```bash
./insula vm start -profile debian13 -memory-mib 2048 -vcpus 2
./insula vm start -profile debian13 -memory-mib 4096 -vcpus 4
./insula vm list
```

Os parâmetros de RAM e vCPUs sobrescrevem apenas a instância iniciada. Com valor `0`, são usados os recursos originais do perfil. `vm start` **sempre cria uma instância nova**, mesmo que já exista outra em execução com o mesmo perfil.

## Exemplo: congelar o ambiente

Pré-requisito: VM inicializada com `ssh.sudo_mode: passwordless`, Cloud-Init operacional e perfil-base válido.

```bash
./insula vm freeze -id UUIDv7 -name debian13-go-testes -shutdown-timeout 3m
./insula profile show -name debian13-go-testes
./insula vm start -profile debian13-go-testes
```

O sucesso de `freeze` imprime `FREEZE SUCCESS: name=... source=...`. A imagem é QCOW2 autônoma, **não ISO Live**. A VM original é removida após registro do novo perfil. Em falha posterior ao registro, a CLI informa o caminho já criado e o erro de limpeza. Consulte [FLUX-004](../../fluxos/FLUX-004-congelar-vm.md) antes de usar esse comando em ambientes com dados importantes.

## Exemplo: encerrar VMs gerenciadas

```bash
./insula vm stop-all -dry-run
./insula vm stop-all
```

A execução percorre **somente** os UUIDs do banco SQLite associado ao `config.yaml`. Um domínio externo não registrado nunca é alvo. `-dry-run` mostra o plano sem executar `StopVM`. Em execução real, os domínios existentes são encerrados, inclusive estados não operacionais ainda presentes; ausentes são ignorados. Um erro de estado ou remoção é registrado e não transforma os demais alvos em VMs externas.

## Entradas, saídas e erros

| Aspecto | Contrato efetivo |
| --- | --- |
| stdin | Nenhuma entrada interativa em `start`, `list`, `show`, `status`, `freeze`, `stop` ou `stop-all`; `shell` transfere o terminal para SSH |
| stdout | Dados de VM, sugestões de comando SSH, resumos `STOP-ALL` e `FREEZE SUCCESS` quando aplicável |
| stderr | Mensagens de progresso, avisos de perfil duplicado em execução, falhas de validação e Libvirt |
| Código de saída | `0` quando `vmCommand` retorna `nil`; `1` em erro retornado pelo executável |
| Cancelamento | `Ctrl+C` cancela o contexto; não garante rollback de uma exportação já concluída |

**Falhas relevantes:** perfil ausente, valores de RAM/vCPUs inválidos, falta de permissões Libvirt/SSH, UUID não cadastrado, diretório inseguro, estado real indeterminado ou falha de `qemu-img`. `stop-all` retorna código `1` quando ao menos uma falha foi agregada; as VMs processadas com êxito permanecem encerradas.

## Dados e fluxos relacionados

| Ação | Origem dos dados | Destino exibido |
| --- | --- | --- |
| `list`/`status` | [PERS-001](../../persistencia/PERS-001-registro-instancias.md) + Libvirt | UUID, IP, estados, perfil e SSH |
| `start` | [Perfil QCOW2](../../arquitetura/ARQ-002-perfis-vm.md) + config global | UUIDv7, IP, MAC, usuário |
| `freeze` | Registro SQLite + VM real + [FLUX-004](../../fluxos/FLUX-004-congelar-vm.md) | Nome e caminho do perfil, status da limpeza |
| `stop-all` | Registros SQLite + estados Libvirt | Quantidade verificada, encerrada, ignorada e falhas |

**Limite de validação:** exemplos são correspondentes ao parser Go; a execução completa em hipervisor KVM real ainda não está comprovada nesta revisão.
