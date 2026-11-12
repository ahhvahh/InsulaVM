<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-004 — Gerenciar e congelar instâncias de VM

**Estado:** implementado na CLI Go.
**Plataforma:** hospedeiro Linux com Libvirt/QEMU/KVM.
**Código:** [vm_cli.go](../../../cmd/insula/vm_cli.go), [start_options.go](../../../insulavm/start_options.go), [stop.go](../../../insulavm/stop.go) e [freeze.go](../../../insulavm/freeze.go).

## Correspondência com contratos canônicos

Os comandos abaixo são a **CLI legada efetivamente implementada**. A nomenclatura de referência de operações e a exigência de paridade entre CLI e biblioteca constam em [CONTR-001](../../contratos/CONTR-001-operacoes-canonicas.md). Os futuros `vm command` e `vm run-script` devem obedecer a [CONTR-002](../../contratos/CONTR-002-transporte-stdio.md), sem alterar silenciosamente os formatos legados de `vm exec`/`vm deploy`.

## Comandos e parâmetros

| Comando | Parâmetros específicos | Efeito / vínculo ao fluxo |
| --- | --- | --- |
| `insula vm start` | Exatamente um de `-profile` (nova VM) ou `-id` (reabrir VM preservada); `-memory-mib`, `-vcpus` opcionais para perfil | Cria nova VM ou reinicia domínio transitório com mesmo UUID/QCOW2; [ARQ-003](../../arquitetura/ARQ-003-configuracao-perfis.md) |
| `insula vm list` | Nenhum além de `-config` | Lista Nome, Perfil, UUID, SQLite, Libvirt e IP, nessa ordem; [PERS-001](../../persistencia/PERS-001-registro-instancias.md) |
| `insula vm status` | `-id` obrigatório | Exibe identidade/SSH e estados; [PERS-001](../../persistencia/PERS-001-registro-instancias.md) |
| `insula vm shell` | `-id` obrigatório; `-directory` opcional | Abre SSH na VM. Tenta `profile.yaml workspace.destination` (ou `config.yaml guest.destination`) e, se inacessível, mantém a shell no diretório inicial do usuário, com aviso; [ARQ-006](../../arquitetura/ARQ-006-identidade-registro-sudo.md) |
| `insula vm exec` | `-id`, `-command` obrigatórios; `-workspace`, `-sudo` opcionais | Executa comando e retorna logs; [FLUX-003](../../fluxos/FLUX-003-execucao-sequencial-logs.md) |
| `insula vm import` | `-id`, `-source` obrigatórios | Copia arquivo/diretório para o destino do perfil (`workspace.destination` ou `guest.destination`); [ARQ-005](../../arquitetura/ARQ-005-importacao-arquivos.md) |
| `insula vm freeze` | `-id` e `-name` obrigatórios; `-export-iso=false` e `-compress=true` por padrão; `-shutdown-timeout` opcional | Publica QCOW2 independente, opcionalmente comprimido com QEMU; [FLUX-004](../../fluxos/FLUX-004-congelar-vm.md) |
| `insula vm stop` | `-id` obrigatório | Encerra e remove disco temporário, mantendo o histórico SQLite; [ARQ-006](../../arquitetura/ARQ-006-identidade-registro-sudo.md) |
| `insula vm delete` | `-id` obrigatório | Confirma ausência no Libvirt, exclui disco/credenciais temporários e elimina registro e histórico de shell do SQLite; preserva logs operacionais |
| `insula profile delete` | `-name` obrigatório | Exclui perfil e sua imagem-base somente se nenhuma VM registrada ainda referenciá-lo |
| `insula vm stop-all` | `-dry-run` opcional; não aceita `-id` | Processa somente domínios registrados neste InsulaVM; [PERS-001](../../persistencia/PERS-001-registro-instancias.md) |

A resolução do diretório de trabalho das VMs gerenciadas usa `profile.yaml workspace.destination` quando preenchido, depois `profile.yaml guest.destination` e, por fim, `config.yaml guest.destination` nos perfis legados. `vm exec`, `vm import` e Actions gerenciadas adotam esse destino como padrão. **Somente `vm shell` ignora um erro de `cd` no guest**, preservando a sessão no `$HOME` (ou `/` se o home não puder ser acessado). Comandos batch e importações continuam sinalizando falhas do destino, para não executar ações ou escrever arquivos em um local inesperado. A validação SSH continua exigindo chave conhecida e identidade correta.\n\nTodos esses subcomandos aceitam `-config` opcional, cujo padrão é `config.yaml` relativo ao diretório atual. O YAML é necessário mesmo quando `-config` não foi informado. O comando `deploy` está documentado em [INTF-TERM-005](INTF-TERM-005-deploy-vm.md).


## Reiniciar a mesma VM pelo UUID

Se uma VM transitória foi desligada pelo sistema convidado e seu disco ainda existe, o InsulaVM recria o domínio usando a configuração persistida sem gerar novo UUID nem disco:

```bash
./insula vm start -id UUID_DA_VM
./insula vm console -id UUID_DA_VM
```

Para uma VM criada por `install-iso`, a retomada tenta `vda` (HD instalado) antes do CD-ROM, mantendo a ISO disponível para fallback. Esta ordem só é alterada no XML temporário de retomada; o XML de instalação original permanece intacto. A retomada exige Libvirt `absent`, diretório gerenciado intacto e arquivos `domain.xml` e `installer.qcow2` ou `overlay.qcow2` regulares e associados ao UUID cadastrado. VMs encerradas com `vm stop` ou `vm delete` não podem ser retomadas. A presença do domínio não comprova boot do sistema convidado: valide pelo console e, quando aplicável, por SSH.

## Exclusão permanente e limpeza da lista

```bash
./insula vm list
./insula vm delete -id UUID_DA_VM
./insula vm list
./insula profile delete -name NOME_DO_PERFIL
```

`vm delete` utiliza o mesmo motor de parada segura de `vm stop`, recusa apagamento se não conseguir verificar ausência do domínio Libvirt ou remoção dos arquivos temporários e então remove o registro e histórico de shell SQLite numa transação. Preserva os logs operacionais. O ID fica reservado em tombstone no banco para nunca ser atribuído a outra VM, mas **não aparece mais** em `vm list`.

O perfil é independente da instância: `profile delete` é uma ação **separada e explícita**. O comando recusa remover perfis com qualquer VM registrada como dependente (inclusive desligada). Exclua primeiro todas as instâncias daquele perfil, depois exclua o perfil. Não use esse comando para limpar apenas registros antigos se outras VMs ainda precisam da imagem-base.

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

### Congelamento sem ISO e armazenamento externo

```bash
./insula vm freeze -id UUIDv7 -name debian-comprimido
./insula vm start -profile debian-comprimido
```

A compressão QCOW2 é **padrão** e corresponde a `VMFreezeRequest{...}` na biblioteca. Para desabilitá-la, usar `-compress=false` ou `VMFreezeRequest.DisableQCOW2Compression=true`. O arquivo permanece em formato QCOW2 e pode ser reutilizado imediatamente por novas VMs. O perfil é salvo no caminho `storage.profiles` do YAML; para usar um volume externo, configure ali o caminho absoluto do volume **antes de congelar**. Armazenar o perfil em volume desconectado impede iniciar novas VMs que dependam dele.

## Exemplo: encerrar VMs gerenciadas

```bash
./insula vm stop-all -dry-run
./insula vm stop-all
```

A execução percorre **somente** os UUIDs do banco SQLite associado ao `config.yaml`. Um domínio externo não registrado nunca é alvo. `-dry-run` mostra o plano sem executar `StopVM`. Em execução real, os domínios existentes são encerrados, inclusive estados não operacionais ainda presentes; ausentes são ignorados. Um erro de estado ou remoção é registrado e não transforma os demais alvos em VMs externas.

## Entradas, saídas e erros

| Aspecto | Contrato efetivo |
| --- | --- |
| stdin | Nenhuma entrada interativa em `start`, `list`, `status`, `freeze`, `stop` ou `stop-all`; `shell` transfere o terminal para SSH |
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

## Desligamento com timeout (novo)

A CLI `insula vm stop -id UUID -grace-timeout 30s -force-after-timeout=true` tenta `virsh shutdown` e espera até 30 segundos; apenas ao expirar o prazo usa destruição imediata e limpa o overlay. `-force-after-timeout=false` mantém a VM quando o prazo expira. Falhas de Libvirt diferentes de timeout não autorizam forçar. A biblioteca legada `StopVM` permanece com destruição imediata; usar `StopVMWithOptions` para a nova política. Consulte [INTF-TERM-006](INTF-TERM-006-iso-live.md) e [FLUX-006](../../fluxos/FLUX-006-teste-integral-iso-vm.md).

**Estado:** código novo não validado em hipervisor real.
