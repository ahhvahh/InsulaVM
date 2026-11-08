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
| insula profile export-iso -name PERFIL | ExportProfileLiveISO | Perfil QCOW2 Debian com live-boot | Caminho ISO em exports/ |
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
