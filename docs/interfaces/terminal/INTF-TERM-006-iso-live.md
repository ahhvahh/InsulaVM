<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-006 — Exportar e iniciar ISO Live

**Estado:** implementado no código da branch de trabalho; não validado em KVM real.
**Contrato de referência:** [CONTR-001](../CONTR-001-operacoes-canonicas.md).
**Fluxo:** [FLUX-006](../../fluxos/FLUX-006-teste-integral-iso-vm.md).

## Comandos

| CLI | Biblioteca Go | Entrada | Saída |
| --- | --- | --- | --- |
| insula profile clone -source ORIGEM -name NOVO | CloneProfile | Dois nomes de perfil | Perfil clonado |
| insula vm freeze -config CFG -id UUID ./destino/nome.iso [-name PERFIL] [-force-freeze-shutdown] | FreezeVM / ExportVMToLiveISO | VM gerenciada com overlay intacto; confirmação expressa para forçar | Perfil QCOW2 e, por padrão, ISO Live em exports/ |\n| insula profile export-iso -name PERFIL | ExportProfileLiveISO | Perfil QCOW2 Debian com live-boot | Caminho ISO em exports/ |
| insula vm start-iso -profile PERFIL -iso ISO | StartVMFromISO | Perfil registrado e ISO Live com Cloud-Init; script inicial opcional | UUID, IP, SSH |
| insula vm console -id UUID | OpenConsole | UUID e terminal de origem | Console serial no stdin/stdout/stderr |
| insula vm stop -id UUID -grace-timeout 30s -force-after-timeout=true | StopVMWithOptions | UUID, timeout e escalada | graceful/forced/timed_out |

Todos os comandos aceitam -config CAMINHO após o subcomando.

## Entradas e saídas

- clone e export-iso: sem stdin; retornam texto em stdout e código 0/1.
- start-iso: aguarda DHCP e autenticação SSH, aceita -startup-script apenas de arquivo administrativo confiável; retorna UUID para chamadas subsequentes.
- console: transfere stdin, stdout e stderr para virsh console e termina quando o terminal é desconectado; depende de console serial do guest.
- stop: retorna 0 apenas após limpeza e atualização do registro; escalada ocorre somente quando o prazo gracioso expira. Não confundir -force-after-timeout com destruição imediata.
- Installador Debian netinst não é Live ISO; a criação de perfil desde ISO instaladora ainda está **proposta**, em [FLUX-002](../../fluxos/FLUX-002-criacao-perfil-vm.md).

## Exemplo (após preparar live-boot dentro do guest)

```bash
insula profile export-iso -name debian-test
insula vm start-iso -profile debian-test
insula vm exec -id UUID -command "echo INSULA_TEST_OK"
insula vm stop -id UUID -grace-timeout 30s -force-after-timeout=true
```


O boot real ainda depende da infraestrutura do usuário e de teste integral aprovado. Consulte [GUIA-003](../../guias/GUIA-003-testes.md).

## Recuperar falha de desligamento anterior

```bash
insula vm status -id UUID
# A tentativa anterior já executou cloud-init clean e apagou authorized_keys.
insula vm freeze -id UUID -name deb13-abioma-tests-final \
  -skip-guest-cleanup -prepare-live-boot=false \
  -force-freeze-shutdown -shutdown-timeout 20s
# Se já não existir domínio ativo, adicionar -recover-stopped.
```

O recurso de força requer aceite consciente: `destroy` não garante limpeza de cache do convidado. Se não houver `live-boot` no QCOW2, a ISO Live falhará, **mas o perfil QCOW2 permanece salvo** para recuperação e nova preparação. Para formato somente QCOW2, usar `-export-iso=false`.

## Destino de ISO em `vm freeze`

```bash
./insula vm freeze -config ./config.yaml -id UUID_DA_VM ./imagens/minha-vm.iso
# ou:
./insula vm freeze -config ./config.yaml -id UUID_DA_VM -output /tmp/minha-vm.iso
# compatibilidade:
./insula vm freeze -config ./config.yaml -id UUID_DA_VM -name minha-vm
```

O argumento posicional final é o **caminho do arquivo ISO**, não o IP da VM. O UUID identifica a VM. Destinos `./...` são relativos ao diretório de execução e caminhos absolutos são aceitos. Quando o nome do perfil não é informado, é derivado do nome do arquivo. A ISO publicada não substitui arquivo existente. Somente após o `shutdown` ser confirmado no Libvirt, a VM é mapeada em modo leitura para montar a ISO. O QCOW2 e o overlay da origem permanecem em falhas de exportação.

## Diagnóstico do boot da ISO sem DHCP ou SSH

O `vm start-iso` aguarda o Debian Live obter um endereço pela rede Libvirt e
subir o SSH. O limite antigo de três minutos era fixo e podia aparecer como
`signal: killed`, pois o contexto do comando `virsh domstate` era cancelado.

```bash
./insula vm start-iso -config ./config.yaml \
  -profile deb13-abioma-tests-final \
  -iso ./deb13-abioma-tests-final.iso \
  -memory-mib 2048 -vcpus 2 \
  -ssh-ready-timeout 8m \
  -preserve-on-failure

# Em caso de falha, copie o UUID exibido na mensagem:
./insula vm status -config ./config.yaml -id UUID_DA_VM
./insula vm console -config ./config.yaml -id UUID_DA_VM

# Após diagnosticar, se não for mais necessária:
./insula vm stop -config ./config.yaml -id UUID_DA_VM
```

A biblioteca Go `LiveISOStartOptions` expõe `SSHReadyTimeout` e
`PreserveOnFailure`, encaminhados para `vm.StartLiveISOWithOptions`.
A função anterior `vm.StartLiveISO` continua aceita. O timeout padrão
é 5 minutos para a prontidão conjunta DHCP/SSH, substituindo os 3 minutos
fixos da implementação anterior. Um timeout agora informa a etapa
(`DHCP` ou `SSH`) e mantém `context.DeadlineExceeded` como causa.

A VM é preservada somente quando ela já foi criada e registrada pelo
InsulaVM. Isso mantém o disco `scratch.qcow2`, o console e as chaves
de diagnóstico. É necessário executar `vm stop` após a investigação,
pois `-preserve-on-failure` deixa a VM ativa, consumindo memória.

## Rede do Debian Live: DHCP explícito pela interface MAC

O instalador Debian pode salvar uma configuração de rede associada a um
nome de interface que muda quando a mesma imagem é inicializada como ISO Live.
Para não depender do nome da placa, `start-iso` cria um arquivo
`network-config` NoCloud versão 2 (somente na seed temporária da VM Live),
com `match.macaddress` e `dhcp4: true`. A configuração é lida pelo
Cloud-Init durante o primeiro boot e não altera o perfil QCOW2 congelado
nem modifica a ISO Live original.

Se o erro continuar, a ausência de lease não comprova que o DHCP falhou:
a ISO pode sequer estar completando o boot (kernel, initramfs ou GRUB).
Use `-preserve-on-failure` e `vm console -id UUID` para observar o guest.
O `start-iso` utiliza `virsh domifaddr --source lease`, que só devolve
IP quando existe uma concessão DHCP observável na rede Libvirt.

```bash
./insula vm start-iso -config ./config.yaml \
  -profile deb13-abioma-tests-final \
  -iso ./deb13-abioma-tests-final.iso \
  -memory-mib 2048 -vcpus 2 \
  -ssh-ready-timeout 8m -preserve-on-failure
```

**Teste KVM real:** pendente. O seed de rede melhora a determinação da
interface de DHCP, mas não substitui a verificação de console/boot.
