<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-017 — Executar comandos com QEMU Guest Agent (QGA)

**Estado:** integração CLI/biblioteca implementada; boot e execução em KVM real pendentes de validação.
**Escopo:** Linux QEMU/KVM com Libvirt, inclusive Alpine Linux instalado a partir de ISO.

## Visão geral

A CLI oferece `insula vm exec -qga -id UUID_DA_VM "comando"`.
Equivalente Go: `Engine.ExecVM(ctx, ExecRequest{VMID: id, Command: "whoami", QGA: true})`.

O InsulaVM valida a VM no SQLite e o estado `running` do domínio Libvirt; não usa IP, DHCP ou SSH. Envia `guest-exec` pelo canal Virtio `org.qemu.guest_agent.0`, recebe um PID e consulta `guest-exec-status` até conclusão ou timeout. A resposta preserva stdout/stderr como `[]byte` e oferece `ExitCode *int`. Na CLI, stdout e stderr remotos saem em seus respectivos canais, enquanto caminhos de logs e falhas vão a stderr.

## Preparar a VM Alpine

O domínio gerado por `vm install-iso` e por `vm start` já inclui:

```xml
<channel type='unix'>
  <target type='virtio' name='org.qemu.guest_agent.0'/>
</channel>
```

A imagem convidada precisa do QEMU Guest Agent; no Alpine 3.24, depois de instalar o sistema operacional no disco, executar como root:

```sh
setup-apkrepos -c -1
apk update
apk add qemu-guest-agent qemu-guest-agent-openrc
rc-update add qemu-guest-agent default
rc-service qemu-guest-agent start
ls -l /dev/virtio-ports/org.qemu.guest_agent.0
```

Uma VM já inicializada sem esse canal não recebe o dispositivo retroativamente apenas por atualizar o InsulaVM. Desligue o guest e recrie ou retome o domínio com XML atualizado, **preservando `installer.qcow2`**. Para VMs criadas com `install-iso`, não execute `vm stop` nem `vm delete` antes de preservar a imagem instalada: essas operações removem o disco temporário. Uma opção segura é adicionar o canal ao `domain.xml` preservado, com backup, e usar `vm start -id UUID` quando o domínio estiver ausente. O InsulaVM não modifica XML de VMs já em execução.

## Validar a comunicação

No host (requer permissão para acessar `qemu:///system`):

```sh
sudo virsh -c qemu:///system qemu-agent-command UUID_DA_VM --timeout 5 '{"execute":"guest-ping"}'
./insula vm exec -qga -id UUID_DA_VM "whoami"
./insula vm exec -qga -id UUID_DA_VM "uname -a"
./insula vm exec -qga -id UUID_DA_VM -command "printf test"
```

Para a CLI, os argumentos `-qga` e `-id` devem aparecer antes do comando posicional. `-command` continua aceito. `-command-timeout` limita o tempo total de espera.

## Contrato, permissões e falhas

- `-qga` é opcional; sem ele, `vm exec` mantém o transporte SSH anterior.
- O QGA executa com a identidade do serviço `qemu-ga`, normalmente **root**. `-sudo` em conjunto com `-qga` é rejeitado para evitar uma falsa promessa de elevação seletiva; restrinja rigorosamente quem pode acionar QGA no host.
- Diferentemente do SSH, QGA não cria automaticamente o workspace. Use `-workspace /caminho/absoluto` somente se ele já existir na VM; por padrão o comando roda no diretório padrão do daemon.
- O resultado contém `Stdout`, `Stderr`, `ExitCode` e sinalizadores de truncamento. Erros de saída diferente de zero são distintos dos erros de transporte.
- `guest-exec` captura a saída **após** o processo terminar; não é streaming contínuo. Output truncado é tratado como erro e os bytes disponíveis são preservados.
- Timeout ou cancelamento interrompe a espera no host, **não garante encerramento do processo remoto**. A VM e o disco são preservados.
- **Concorrência sem fila:** enquanto `Engine.ExecVM` executa um comando (SSH ou QGA) para um UUID, uma segunda chamada `vm exec` para a mesma VM retorna `insulavm.ErrVMExecBusy` (`errors.Is` na biblioteca; código `1` e mensagem legível na CLI). Para outras VMs, a execução continua concorrente. O lock Linux `flock` é imediato e compartilhado entre CLI/biblioteca no mesmo host que utiliza o mesmo `storage.database`.
- O lock é liberado no retorno da chamada ou no encerramento do processo host, **não** prova que um guest-exec remoto foi interrompido após timeout. Reconfirme o estado no convidado antes de reenviar comandos não idempotentes. Não existe espera automática nem fila; operações externas e outros métodos não compartilham esse lock.
- O canal Virtio não expõe TCP/IP, mas seu acesso depende das permissões do Libvirt e da integridade do guest. Não disponibilize a execução do agente a usuários não confiáveis.
- `ExecResult.LogDirectory` identifica o histórico; o arquivo `command.log` é diagnóstico textual. Saídas binárias permanecem sem transformação em `ExecResult.Stdout/Stderr`.

## Testes

```sh
go test ./...
go test -race ./...
go vet ./...
```

Esses comandos validam o código; o teste integral exige host com KVM, Alpine instalado, canal Virtio ativo e QEMU Guest Agent em execução. Confirmar também duas VMs distintas com resultados associados aos seus próprios UUIDs.

## Fontes

- QEMU Guest Agent Protocol Reference: https://www.qemu.org/docs/master/interop/qemu-ga-ref.html
- Libvirt QEMU Guest Agent: https://wiki.libvirt.org/Qemu_guest_agent.html
- Libvirt domain XML: https://libvirt.org/formatdomain.html
