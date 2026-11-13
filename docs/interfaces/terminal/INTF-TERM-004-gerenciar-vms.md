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
| `insula vm start` | Exatamente um de `-profile` (nova VM) ou `-id` (reabrir VM preservada); `-memory-mib`, `-vcpus` opcionais para perfil | Cria nova VM ou inicia/retoma uma VM existente com mesmo UUID/QCOW2; [ARQ-003](../../arquitetura/ARQ-003-configuracao-perfis.md) |
| `insula vm list` | Nenhum além de `-config` | Lista Nome, Perfil, UUID, SQLite, Libvirt e IP, nessa ordem; [PERS-001](../../persistencia/PERS-001-registro-instancias.md) |
| `insula vm status` | `-id` obrigatório | Exibe identidade/SSH e estados; [PERS-001](../../persistencia/PERS-001-registro-instancias.md) |
| `insula vm shell` | `-id` obrigatório; `-directory` opcional | Abre SSH na VM. Tenta `profile.yaml workspace.destination` (ou `config.yaml guest.destination`) e, se inacessível, mantém a shell no diretório inicial do usuário, com aviso; [ARQ-006](../../arquitetura/ARQ-006-identidade-registro-sudo.md) |
| `insula vm exec` | `-id` e `-command` (SSH); ou `-qga -id UUID "comando"`; `-workspace` opcional | SSH legado ou execução pelo QEMU Guest Agent (sem IP, com stdout/stderr e exit code); [GUIA-017](../../guias/GUIA-017-qga-exec.md) |
| `insula vm import` | `-id`, `-source` obrigatórios | Copia arquivo/diretório para o destino do perfil (`workspace.destination` ou `guest.destination`); [ARQ-005](../../arquitetura/ARQ-005-importacao-arquivos.md) |
| `insula vm freeze` | `-id` e `-name` obrigatórios; `-export-iso=false`, `-compress=true` por padrão; `-shutdown-timeout`, `-recover-stopped`, `-skip-guest-cleanup` opcionais | Para `install-iso`, solicita shutdown por QGA, fallback ACPI; publica QCOW2 independente. Recuperação de `shut off` exige `-recover-stopped`; Cloud-Init exige também `-skip-guest-cleanup`. [FLUX-004](../../fluxos/FLUX-004-congelar-vm.md) |
| `insula vm stop` | `-id` obrigatório | Encerra e remove disco temporário, mantendo o histórico SQLite; [ARQ-006](../../arquitetura/ARQ-006-identidade-registro-sudo.md) |
| `insula vm delete` | `-id` obrigatório | Confirma ausência no Libvirt, exclui disco/credenciais temporários e elimina registro e histórico de shell do SQLite; preserva logs operacionais |
| `insula profile delete` | `-name` obrigatório | Exclui perfil e sua imagem-base somente se nenhuma VM registrada ainda referenciá-lo |
| `insula vm stop-all` | `-dry-run` opcional; não aceita `-id` | Processa somente domínios registrados neste InsulaVM; [PERS-001](../../persistencia/PERS-001-registro-instancias.md) |

A resolução do diretório de trabalho das VMs gerenciadas usa `profile.yaml workspace.destination` quando preenchido, depois `profile.yaml guest.destination` e, por fim, `config.yaml guest.destination` nos perfis legados. `vm exec`, `vm import` e Actions gerenciadas adotam esse destino como padrão. **Somente `vm shell` ignora um erro de `cd` no guest**, preservando a sessão no `$HOME` (ou `/` se o home não puder ser acessado). Comandos batch e importações continuam sinalizando falhas do destino, para não executar ações ou escrever arquivos em um local inesperado. A validação SSH continua exigindo chave conhecida e identidade correta.\n\nTodos esses subcomandos aceitam `-config` opcional, cujo padrão é `config.yaml` relativo ao diretório atual. O YAML é necessário mesmo quando `-config` não foi informado. O comando `deploy` está documentado em [INTF-TERM-005](INTF-TERM-005-deploy-vm.md).


## Reiniciar a mesma VM pelo UUID

O mesmo comando controla a instância preservada sem criar outro UUID, disco ou overlay, tanto na CLI quanto na biblioteca Go (`Engine.ResumeVM(ctx, id)`):

```bash
./insula vm start -id UUID_DA_VM
./insula vm status -id UUID_DA_VM
./insula vm console -id UUID_DA_VM
```

O motor compara o registro SQLite, o `domain.xml` preservado e o disco `vda` gerenciado. Para domínios existentes, também valida o XML real retornado pelo Libvirt antes de qualquer ação.

| Estado real Libvirt | Comportamento de `vm start -id` |
| --- | --- |
| `shut off` | Executa `virsh start UUID` sobre o domínio definido existente |
| `paused` | Executa `virsh resume UUID` |
| `running` / `blocked` | Retorna sucesso sem iniciar novamente e conserva o IP registrado se o SQLite já estiver `running` |
| `absent` | Recria o domínio transitório a partir de `domain.xml`, preservando UUID e QCOW2 |
| `unknown`, `crashed`, `in shutdown` ou outro | Retorna erro sem forçar transição insegura |

As retomadas efetivas exigem confirmação do estado `running` ou `blocked` antes de atualizar o SQLite. O IP anterior é invalidado quando um domínio é iniciado novamente: a obtenção do endereço atual deve ser confirmada pelo Libvirt; `status_sqlite=running` não garante boot do convidado. VMs marcadas como `destroyed`, com diretório/disco ausente ou XML incompatível, não são retomadas.

**Ordem de boot:** ao recriar domínio `absent` de `install-iso`, a inicialização prioriza `vda` antes do CD-ROM por meio de um XML temporário, sem modificar `domain.xml` original. Se o domínio já estiver definido como `shut off`, `virsh start` mantém a ordem de boot configurada no Libvirt; uma definição persistente que ainda priorize o CD-ROM poderá inicializar a ISO instaladora novamente. A alteração de boot em definições existentes não faz parte desta correção.


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

## Perfil instalado por ISO, sem SSH e sem Cloud-Init

`vm install-iso` registra uma VM cujo disco é `installer.qcow2`. Após instalar o sistema operacional no disco virtual, o `freeze` suporta esse formato:

```bash
./insula vm status -id UUID_DA_VM
./insula vm freeze -id UUID_DA_VM -name abioma-tests
./insula profile show -name abioma-tests
./insula vm start -profile abioma-tests -memory-mib 500 -vcpus 1
./insula vm exec -qga -id UUID_NOVA_VM "uname -a"
```

O `freeze` utiliza a imagem **instalada**, não a ISO, e publica `boot_mode: direct` em `profile.yaml`. Nesse modo, `vm start` somente confirma o estado do domínio no Libvirt, sem provisionar usuário SSH, Cloud-Init, chaves, sudo ou exigir IP. É necessário verificar separadamente o boot do convidado e a disponibilidade do QEMU Guest Agent. O canal Virtio QGA é incluído no XML; o daemon QGA deve estar instalado e ativo dentro da VM.

No fluxo instalador não há limpeza automática de identificadores do convidado nem segredos. Sanitizar `machine-id`/chaves SSH, se necessário, antes do freeze. A conversão QCOW2 é comprimida por padrão; `vm stop` na VM de origem **antes** do freeze exclui `installer.qcow2`. Em falhas de shutdown ou conversão, o disco é preservado; não force parada sem aceitar o risco de inconsistência.

### Alpine desligado manualmente: recuperar freeze

```bash
./insula vm status -id 01a1d2c9-d114-74ee-90c5-a2ea26c87a17
./insula vm freeze -id 01a1d2c9-d114-74ee-90c5-a2ea26c87a17 -name abioma-test -recover-stopped
./insula profile show -name abioma-test
./insula vm start -profile abioma-test -memory-mib 500 -vcpus 1
```

`status_libvirt=shut off` significa que a VM está definida, mas desligada; `SQLite=running` é o histórico não atualizado. A recuperação não liga a VM novamente. A conversão só prossegue se o disco gerenciado e o estado offline forem comprovados. A limpeza posterior usa `virsh undefine` **sem opções para excluir storage**, somente quando UUID, nome e disco da definição conferem com o registro.

Para VM instaladora ainda ativa, `freeze` tenta primeiro o desligamento pelo QEMU Guest Agent (`virsh shutdown --mode agent`), depois ACPI caso necessário. A inexistência do agente **não autoriza force automático**. Em timeout, preservar o disco e orientar a instalação do agente/OpenRC ou `poweroff` no console, seguido de `-recover-stopped`.

## Exemplo: encerrar VMs gerenciadas

```bash
./insula vm stop-all -dry-run
./insula vm stop-all
```

A execução percorre **somente** os UUIDs do banco SQLite associado ao `config.yaml`. Um domínio externo não registrado nunca é alvo. `-dry-run` mostra o plano sem executar `StopVM`. Em execução real, os domínios existentes são encerrados, inclusive estados não operacionais ainda presentes; ausentes são ignorados. Um erro de estado ou remoção é registrado e não transforma os demais alvos em VMs externas.

## Duplicidade de cadastro e chamadas simultâneas de `vm exec`

- A criação de uma VM com `id`, `libvirt_uuid` ou `name` já cadastrado é **rejeitada**, sem substituir a instância. O erro identifica o campo em conflito: `VM identity already registered: UUID "..." already exists` ou `VM identity already registered: name "..." already exists`. Um UUID excluído também não pode ser reutilizado.
- `insula vm exec -id UUID -command "comando"` e `insula vm exec -qga -id UUID "comando"` usam a mesma exclusão imediata por UUID. Se outra chamada de `ExecVM` já estiver ativa para esse UUID, a CLI encerra com código `1`, informando `another vm exec is already running; retry after the current command finishes`. Nenhuma execução concorrente é enfileirada ou iniciada nessa situação.
- `exec` em VMs com UUIDs diferentes pode ocorrer simultaneamente. A proteção cobre chamadas CLI e `Engine.ExecVM` que compartilham `storage.database` no **mesmo host Linux**, por meio de um lock advisory `flock` não bloqueante. Arquivos `.vm-exec-*.lock` ficam ao lado do SQLite e são mantidos para evitar corridas de remoção; não são QCOW2 nem locks do banco.
- O lock é liberado quando a chamada termina ou o processo encerra. Em timeout/cancelamento de QGA, o **processo dentro do guest pode continuar** mesmo depois que o lock do host for liberado. A proteção não coordena ferramentas externas que não usam o motor, nem `vm shell-command`, `import`, `deploy`, `stop` ou `freeze`.
- Não existe fila, espera configurável ou migração para PostgreSQL. A biblioteca Go expõe `insulavm.ErrVMExecBusy` para identificação via `errors.Is`, mantendo o mesmo comportamento da CLI.

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

## QEMU Guest Agent em `vm exec`

A opção `-qga` utiliza o canal Virtio do Libvirt, com `guest-exec` e `guest-exec-status` em vez de SSH. A CLI aceita `vm exec -qga -id UUID "comando"` ou `-command "comando"`. A biblioteca oferece `ExecRequest.QGA=true`, mantendo o mesmo método `ExecVM`. A instalação do serviço dentro do guest é responsabilidade da imagem. Consulte [GUIA-017](../../guias/GUIA-017-qga-exec.md) para os procedimentos, limitações de segurança e diagnósticos.
