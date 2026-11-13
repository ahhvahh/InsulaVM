<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-017 — Executar comandos com QEMU Guest Agent (QGA)

**Estado:** integração CLI/biblioteca implementada; boot e execução em KVM real pendentes de validação.
**Escopo:** Linux QEMU/KVM com Libvirt, inclusive Alpine Linux instalado a partir de ISO.

## Visão geral

A CLI oferece `insula vm exec -qga -id UUID_DA_VM "comando"`.
Equivalente Go: `Engine.ExecVM(ctx, ExecRequest{VMID: id, Command: "whoami", QGA: true})`.

O InsulaVM valida a VM no SQLite e o estado `running` do domínio Libvirt; não usa IP, DHCP ou SSH. Envia `guest-exec` pelo canal Virtio `org.qemu.guest_agent.0`, recebe um PID e consulta `guest-exec-status` até conclusão ou timeout. A resposta preserva stdout/stderr como `[]byte` e oferece `ExitCode *int`. Na CLI, stdout e stderr remotos saem em seus respectivos canais. Por padrão **nenhum comando `vm exec` grava `command.log`**. A opção `-log` habilita a gravação por execução e imprime o endereço relativo de `command.log` em stderr. Sem `-log`, não há linha `Logs:`. Erros de infraestrutura continuam no canal de diagnóstico da CLI.

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
- `ExecRequest.Output` e `ExecRequest.ErrorOutput` aceitam `io.Writer` para receber bytes nativos após o processo terminar. Mesmo quando ambos são `nil`, `ExecResult.Stdout`, `ExecResult.Stderr` e `ExecResult.ExitCode` permanecem acessíveis.
- `ExecRequest.Log` (padrão `false`) habilita um `command.log` no host tanto para SSH quanto QGA. `ExecResult.LogDirectory` fica vazio sem essa opção. O log registra comando e stdout/stderr (no QGA, cargas não textuais são armazenadas em Base64). O caminho completo é devolvido pela biblioteca; a CLI imprime o caminho relativo ao diretório atual no **stderr**, apenas com `-log`. O bloqueio de concorrência e o histórico das VMs não mudam.

## Consumo do stdout pela biblioteca Go

Os campos `Stdout` e `Stderr` são bytes nativos; o consumidor decide se imprime, armazena ou interpreta. A implementação é síncrona, sem streaming em tempo real do QGA.

```go
engine, err := insulavm.New("config.yaml")
if err != nil { return err }

result, err := engine.ExecVM(ctx, insulavm.ExecRequest{
    VMID: "UUID_DA_VM",
    Command: "uname -a",
    QGA: true,
    Output: os.Stdout,      // opcional: copiar stdout após a execução
    ErrorOutput: os.Stderr, // opcional: copiar stderr após a execução
})
if result != nil {
    // Também disponível: result.Stdout, result.Stderr, result.ExitCode.
    // Se Output=io.Writer foi fornecido, evite imprimir Stdout novamente.
}
return err
```

Na CLI, `./insula vm exec -qga -id UUID_DA_VM "uname -a"` imprime apenas os bytes stdout do processo em stdout. Redirecionamento funciona normalmente: `./insula vm exec -qga -id UUID_DA_VM "cat /etc/os-release" > os-release.txt`. O InsulaVM não acrescenta rótulos, mensagens ou quebras de linha à saída bruta.

## Logs opcionais e diretório de execução

Por padrão, `vm exec` envia a resposta ao console, sem criar log de comando. Para persistir o comando e a resposta no host, use `-log`:

```bash
./insula vm exec -qga -id UUID_DA_VM "pwd"
./insula vm exec -qga -id UUID_DA_VM -workspace /tmp/workspace "pwd && ls -la"
./insula vm exec -qga -id UUID_DA_VM -log "uname -a"
```

`-log` é uma **flag booleana**, não recebe um caminho como argumento. O InsulaVM cria automaticamente `storage.logs/vm-UUID/exec/<execução>/command.log` e imprime `Logs: data/logs/.../command.log` em stderr, com caminho relativo ao diretório atual do host quando possível. O usuário não precisa definir esse caminho. Os dados da VM continuam em stdout/stderr, sem rótulos acrescentados.

O diretório remoto depende do transporte:

- **QGA:** sem `-workspace`, o processo `/bin/sh -c` herda o diretório de trabalho do serviço `qemu-ga`; o InsulaVM não garante um diretório fixo. O protocolo `guest-exec` não fornece parâmetro `cwd`. Para diretório controlado, usar `-workspace /diretorio/absoluto`, que faz `cd` antes do comando e exige que a pasta exista. `pwd` mostra o diretório efetivo.
- **SSH:** sem `-workspace`, utiliza `profile.yaml workspace.destination`, depois `guest.destination` ou `config.yaml guest.destination`. O motor cria/assegura o workspace configurado quando necessário. Com `-workspace`, executa `cd` para a pasta informada; o transporte SSH legado ainda formata linhas no console e não fornece stdout/stderr binários separados.
- **Log:** o caminho do log pertence ao host, não ao guest; não altera o diretório de execução dentro da VM.

Na biblioteca Go, `ExecRequest{Log: true}` habilita o mesmo registro. Mesmo sem `Log`, `ExecResult.Stdout/Stderr` do QGA ficam disponíveis; a aplicação escolhe se deseja persistir o resultado.

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
