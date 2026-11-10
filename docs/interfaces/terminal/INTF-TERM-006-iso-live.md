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
| insula vm freeze -config CFG -id UUID ./destino/nome.iso [-name PERFIL] [-force-freeze-shutdown] | FreezeVM / ExportVMToLiveISO | VM gerenciada com overlay intacto; confirmação expressa para forçar | Perfil QCOW2 e ISO Live apenas com -export-iso=true |\n| insula profile export-iso -name PERFIL | ExportProfileLiveISO | Perfil QCOW2 Debian com live-boot | Caminho ISO em exports/ |
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
insula vm freeze -id UUID -name deb13-abioma-tests-final -export-iso=true -prepare-live-boot=true \
  -skip-guest-cleanup -prepare-live-boot=false \
  -force-freeze-shutdown -shutdown-timeout 20s
# Se já não existir domínio ativo, adicionar -recover-stopped.
```

O recurso de força requer aceite consciente: `destroy` não garante limpeza de cache do convidado. Se não houver `live-boot` no QCOW2, a ISO Live falhará, **mas o perfil QCOW2 permanece salvo** para recuperação e nova preparação. O padrão é somente QCOW2 comprimido; ISO Live requer `-export-iso=true -prepare-live-boot=true` e ainda é experimental.

## Destino de ISO em `vm freeze`

```bash
./insula vm freeze -config ./config.yaml -id UUID_DA_VM -export-iso=true -prepare-live-boot=true ./imagens/minha-vm.iso
# ou:
./insula vm freeze -config ./config.yaml -id UUID_DA_VM -export-iso=true -prepare-live-boot=true -output /tmp/minha-vm.iso
# compatibilidade:
./insula vm freeze -config ./config.yaml -id UUID_DA_VM -name minha-vm -export-iso=true -prepare-live-boot=true
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

## Diagnóstico persistente e recuperação sem apagar a VM

Na inicialização por `start-iso`, o InsulaVM cria em `storage.logs`:
`live-iso/<UUID>/events.log` (estágios, DHCP, SSH e falhas).
Se a VM falhar, grava também `live-iso/<UUID>/diagnostic.log` com
`virsh domstate`, `dominfo`, `domiflist`, `domifaddr --source lease`,
estado da rede e concessão DHCP referente ao **MAC desta VM**.
Os arquivos têm acesso privado (`0600` para logs, diretório `0700`)
e sobrevivem à remoção do overlay, sem armazenar credenciais.

O `start-iso` **agora preserva uma VM que já chegou a iniciar no Libvirt
quando falha DHCP/SSH**, sem exigir `-preserve-on-failure`.
Para restaurar a política anterior, use `-preserve-on-failure=false`.
Falhas anteriores ao registro da VM continuam sujeitas a limpeza.
A opção `-serial-log` inclui `<log file=...>` no console serial PTY do
Libvirt e grava mensagens do kernel/GRUB em `serial.log`, caso o host tenha
permissão para o arquivo e suporte a `virtlogd`. É **opt-in**, pois alguns
hospedeiros têm política QEMU/SELinux/AppArmor que pode rejeitar gravação
nessa localização. O campo `virtualization.qemu_uid` permite conceder ACL
restrita ao usuário QEMU quando necessário.

Exemplo completo, sem chamar o virsh diretamente:

```bash
./insula vm start-iso -config ./config.yaml \
  -profile deb13-abioma-tests-final \
  -iso ./deb13-abioma-tests-final.iso \
  -memory-mib 1024 -vcpus 1 \
  -ssh-ready-timeout 8m -serial-log

# Se falhar, a mensagem inclui o UUID preservado.
./insula vm diagnose -config ./config.yaml -id UUID_DA_VM
./insula vm console -config ./config.yaml -id UUID_DA_VM
```

`vm diagnose` é uma consulta somente-leitura da VM e produz novo snapshot
de Libvirt/rede; exibe os últimos 16 KiB de cada log e caminhos completos
para os arquivos originais. Funciona também para um UUID histórico sem domínio,
desde que ainda exista no SQLite. `vm console` só funciona se o domínio
estiver rodando. Para sair do console do Libvirt use Ctrl+]; o comando não
desliga a VM. Use `vm stop` **somente depois de concluir o diagnóstico**;
`stop` remove o disco temporário. Logs de boot do QEMU/Libvirt também podem
estar no journal do Linux:

```bash
journalctl -u virtqemud -u libvirtd -u virtlogd --since "30 minutes ago"
# Registro específico de QEMU: /var/log/libvirt/qemu/<NOME_DA_VM>.log
```

**Recuperação da imagem preparada:** o `freeze` gera um QCOW2
independente para recriar VMs persistindo as configurações do Debian.
Se a ISO Live não iniciar, `vm start -profile deb13-abioma-tests-final`
usa esse QCOW2 sem a cadeia Debian Live/GRUB/SquashFS. Não é um boot
pela ISO; é a forma recomendada de reutilizar ambiente configurado.
A causa de um erro de boot de ISO só é determinada após examinar
`serial.log` ou `vm console`, não apenas pelo timeout de DHCP.

## Serial de GRUB (diagnóstico antes do kernel)

A ISO Live gerada anteriormente passava `console=ttyS0,115200n8`
somente ao kernel, mas não habilitava saída serial no GRUB. Logo,
`vm console` e `-serial-log` podiam ficar vazios se o boot parasse
no carregador, mesmo com o domínio Libvirt em estado `running`.

A exportação nova configura, **antes** da entrada do menu:
`serial --unit=0 --speed=115200`,
`terminal_input serial console` e `terminal_output serial console`.
Essas diretivas são documentadas pelo manual do GNU GRUB. São válidas
para o boot da próxima ISO exportada; não corrigem retroativamente
arquivos ISO já existentes.

Para recriar a ISO a partir do QCOW2 congelado, sem destruir ou
sobrescrever a original:

```bash
./insula profile export-iso -config ./config.yaml \
  -name deb13-abioma-tests-final \
  -output ./deb13-abioma-tests-final-serial.iso

./insula vm start-iso -config ./config.yaml \
  -profile deb13-abioma-tests-final \
  -iso ./deb13-abioma-tests-final-serial.iso \
  -memory-mib 2048 -vcpus 2 -serial-log
```

O log de GRUB pode revelar travamento no carregador e mensagens
anteriores ao kernel. Se o domínio não apresentar saída serial,
ainda será preciso analisar firmware/VGA e os arquivos persistidos
em `storage.logs/live-iso/<UUID>/`. A configuração serial melhora a
observabilidade, mas não comprova que a ISO inicializa com sucesso.
