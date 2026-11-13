<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# FLUX-004 — Congelar VM gerenciada como perfil reutilizável

**Estado:** congelamento padrão QCOW2 independente com compressão nativa QEMU; teste de boot real da base comprimida pendente.
**Versão aprovada:** nenhuma.
**Evidências:** [FreezeVM](../../insulavm/freeze.go), [ShutdownForFreeze](../../pkg/vm/freeze.go), [FreezeFromOverlay](../../pkg/profile/freeze.go) e [testes de conversão](../../pkg/profile/freeze_test.go).
**Interface CLI:** [INTF-TERM-004](../interfaces/terminal/INTF-TERM-004-gerenciar-vms.md).
**Perfil de origem:** [ARQ-002](../arquitetura/ARQ-002-perfis-vm.md).

## Objetivo

Publicar as modificações realizadas em uma VM temporária como um **novo perfil nomeado e reutilizável**, sem sobrescrever seu perfil de origem. Novas VMs partem do QCOW2 congelado e gravam suas alterações em overlays próprios.

## Entradas e pré-condições

- `VMID`: UUIDv7 de VM gerenciada com SQLite `running` e Libvirt `running`/`blocked`. Na recuperação `-recover-stopped`, também é aceito Libvirt `shut off` ou `absent` com disco intacto; para Cloud-Init, exigir adicionalmente `-skip-guest-cleanup`.
- `Name`: nome novo e válido para perfil; não pode existir em `storage.profiles`.
- `ShutdownTimeout`: prazo positivo para desligamento, padrão de 3 minutos.
- Perfil de origem do tipo QCOW2 gerenciado: sistema instalado, Cloud-Init, SSH e `ssh.sudo_mode: passwordless`. **Alternativamente**, perfil `installer-iso` com sistema já instalado em `installer.qcow2`: não exige SSH/Cloud-Init/sudo, mas não sanitiza a identidade do convidado.
- Disco `overlay.qcow2` (gerenciado) ou `installer.qcow2` (instalador) regular dentro de `storage.runs/insula-vm-*`, e domínio cujo UUID corresponda ao registro.

## Sequência

```mermaid
sequenceDiagram
    autonumber
    participant C as CLI / Go Engine
    participant DB as SQLite
    participant V as VM convidada
    participant L as Libvirt
    participant Q as qemu-img
    participant P as Catálogo de perfis
    C->>DB: Consulta VM e estado persistido
    C->>L: Verifica existência e estado real
    C->>P: Confere nome exclusivo e perfil de origem
    C->>V: ExecVM com sudo (limpa cloud-init e chaves SSH)
    C->>L: instalador: shutdown --mode agent; fallback ACPI / gerenciada: shutdown
    loop Até domínio ficar absent ou shut off ou expirar prazo
        C->>L: list/domstate
    end
    alt Desligamento comprovado
        C->>Q: convert -f qcow2 -O qcow2 -c overlay novo/base.qcow2
        C->>Q: info --output=json
        C->>P: Publica profile.yaml somente após validar disco autônomo
        C->>L: StopVM confirma ausência e limpa origem
        C->>DB: Status destroyed
        C-->>C: Nome do perfil e caminho da imagem
    else Falha
        C-->>C: Erro + estado parcial; preserva overlay quando exportação falhar
    end
```

## Regras de execução e segurança

1. Validar registro, identidade Libvirt, diretório temporário e nome de destino **antes** de modificar o convidado.
2. Para perfil Cloud-Init, usar `ExecVM` e `sudo -n` para limpar `cloud-init`, `authorized_keys` e chaves host SSH. Para perfil instalador, **não tentar** essa limpeza: o guest pode não possuir SSH/Cloud-Init. O operador deve sanitizar manualmente identidades e segredos quando necessário.
3. Para instaladores, solicitar `virsh shutdown UUID --mode agent` primeiro, com espera limitada. Se o QEMU Guest Agent não responder, solicitar `virsh shutdown UUID --mode acpi`. Se o agente aceitar mas a VM permanecer ativa por 15 segundos, enviar também ACPI. O fluxo Cloud-Init mantém `virsh shutdown UUID`. **Não destruir** sem `-force-freeze-shutdown`.
4. Só converter depois de comprovar o estado Libvirt `absent` ou `shut off`. Esses estados não têm QEMU escrevendo no disco. Estados `unknown`, `paused`, `running`, `blocked` e `in shutdown` não autorizam conversão. Em recuperação manual, exigir `-recover-stopped`; para Cloud-Init, também `-skip-guest-cleanup`.
5. Converter o disco de origem (`overlay.qcow2` ou `installer.qcow2`) em `profiles/<nome>/versions/v001/base.qcow2` com `qemu-img convert -c`; confirmar QCOW2 autônomo sem backing file e publicar `profile.yaml` ao final. Para instaladores, registrar `boot_mode: direct`. O registro é exclusivo e não substitui nomes existentes.
6. Executar `StopVM` para limpar a VM original **somente após** publicar o perfil. Se Libvirt ainda registrar um domínio `shut off`, verificar UUID, nome e caminho do disco `vda` contra o registro antes de `virsh undefine UUID` (sem flags de remoção de storage); confirmar domínio `absent` antes de apagar arquivos temporários.
7. Reutilizar a base por `vm start -profile <nome>`, inclusive com RAM/vCPUs sobrescritos apenas na instância.

## ISO instaladora e boot direto (Alpine)

```bash
./insula vm freeze -id UUID_DA_INSTALACAO -name abioma-tests
./insula profile show -name abioma-tests
./insula vm start -profile abioma-tests -memory-mib 500 -vcpus 1
./insula vm exec -qga -id UUID_NOVA_VM "uname -a"
```

A instância instalada por `vm install-iso` usa `installer.qcow2` e mantém a ISO original somente como mídia de instalação. A imagem congelada é independente da ISO, usa `boot_mode: direct` e é iniciada por um disco gravável temporário com base QCOW2 comprimida. Não há geração de credenciais SSH, NoCloud nem espera por DHCP/SSH. `vm start` comprova apenas o estado `running` no Libvirt; a validação de boot e agente QGA exige verificar o convidado. O agente QGA, se disponível, pode executar comandos sem IP.

**Risco de identidade clonada:** `machine-id`, host keys SSH, senhas e tokens presentes no disco são preservados. Não compartilhe o perfil sem uma sanitização apropriada feita antes de congelar. Para operação segura, o desligamento normal é obrigatório; `-force-freeze-shutdown` deve ser opção explícita de recuperação, com risco de filesystem inconsistente. O disco original é preservado em falhas de desligamento ou conversão. Não executar `vm stop` antes do freeze.

### Recuperar uma instalação já desligada (Libvirt: shut off)

```bash
./insula vm status -id UUID_DA_INSTALACAO
./insula vm freeze -id UUID_DA_INSTALACAO -name abioma-test -recover-stopped
./insula profile show -name abioma-test
```

Para discos de perfil instalador, `-skip-guest-cleanup` não é necessário: não há identidade Cloud-Init gerenciada. Para perfis Cloud-Init, `-recover-stopped -skip-guest-cleanup` permanece obrigatório, e o operador deve ter evidência de que a limpeza foi feita previamente.

Se o QEMU Guest Agent estiver ausente/inativo e ACPI não encerrar o sistema, o freeze termina com erro e preserva o disco; não tenta injetar comandos de shell/credenciais no console. O operador pode usar `vm console` e `poweroff` dentro do guest, e então recuperar com `-recover-stopped`.

**Atenção ao boot instalador Alpine:** confirma-se a instalação no disco somente após iniciar o sistema a partir do `vda`, não basta visualizar o login do Live ISO. Conteúdo criado em `/tmp` ou sistemas `tmpfs` não faz parte do disco persistente.

## Falhas e recuperação

| Falha | Efeito verificado |
| --- | --- |
| Nome inválido/duplicado, VM sem sudo ou não ativa | Recusa antes do comando de preparação no convidado |
| Limpeza do Cloud-Init falha | Operação interrompida; VM de origem não é desligada pelo fluxo |
| QGA/ACPI não encerram o guest dentro do prazo | Não converte o disco; `installer.qcow2` ou overlay é preservado. Não há force implícito. |
| Conversão ou validação QCOW2 falha | Diretório de novo perfil parcial é removido; overlay original preservado |
| Perfil criado, falha ao limpar VM original | Retorna erro e o `VMFreezeResult.ImagePath` já preenchido; perfil permanece. `shut off` precisa ter identidade e `vda` confirmados antes de `undefine`. |
| Contexto cancelado | Operação interrompida; consultar `vm status` e arquivos restantes antes de recuperar |

A imagem resultante ainda pode conter tokens, dados privados, arquivos temporários e configurações próprias de aplicações. O operador deve remover esses conteúdos antes de congelar. Não existe sanitização automática abrangente.

## Resultado

`FreezeVM(ctx, VMFreezeRequest)` retorna `VMFreezeResult` com `VMID`, `ProfileName`, `ImagePath`, `SourceStopped` e `SourceCleaned`, além de erro quando aplicável. A CLI usa código de saída `0` no sucesso e `1` em erro. Os testes automatizados empregam Libvirt/QEMU simulados; **boot e congelamento completo em KVM real não foram verificados aqui**.

## Contrato de congelamento nativo

O comando padrão **não gera ISO**; é equivalente a `VMFreezeRequest{VMID: id, Name: "debian-preparado"}` no motor Go.

```bash
./insula vm freeze -config ./config.yaml -id UUID_DA_VM -name debian-preparado
./insula vm start -config ./config.yaml -profile debian-preparado
```

A execução desliga a VM graciosamente, consolida seu overlay com `qemu-img convert -f qcow2 -O qcow2 -c`, valida formato e ausência de backing, e registra `profile.yaml` apenas após a conversão bem-sucedida. A base QCOW2 comprimida é usada diretamente pelo QEMU, sem etapa de descompactação; as novas instâncias escrevem overlays independentes.

**Configurações:** compressão habilitada por padrão. `-compress=false` na CLI e `VMFreezeRequest.DisableQCOW2Compression=true` na biblioteca desabilitam a compressão de forma explícita. `VMFreezeRequest.ExportISO` permanece `false` por padrão; a geração de ISO Live é experimental, fora do teste de aceite atual. Para solicitar ISO Live explicitamente, existem `vm export-iso` e `vm freeze -export-iso=true -prepare-live-boot=true`, ainda sem homologação de boot.

**Armazenamento:** `storage.profiles` em `config.yaml` aceita caminho absoluto para um volume dedicado. Preservar juntos o `profile.yaml` e `versions/v001/base.qcow2`; o volume deve estar montado e acessível por Libvirt antes de iniciar VMs. Não mover/apagar imagem-base referenciada por overlays. Armazenamento virtual de 10 GiB não corresponde ao consumo físico, que depende da ocupação e compressibilidade dos blocos. A conversão pode requerer espaço para origem e destino simultaneamente.

**Persistência:** `FreezeVM` captura o armazenamento de disco, **não** RAM, `tmpfs` nem montagens externas. Antes de congelar, assegurar que dados necessários estejam num filesystem persistente do convidado. O Integral 3 verifica o tipo de filesystem e o SHA-256 após iniciar outra VM real.

**Segurança e recuperação:** a conversão não modifica a base/overlay originais. Erros de shutdown, conversão ou publicação preservam o overlay e não aceitam `destroy` sem consentimento explícito. A VM original é removida apenas após salvar o novo perfil. O congelamento limpa identidade Cloud-Init e SSH, mas não apaga segredos das aplicações.

**Testes e evidências:** testes Go verificam compressão, ausência de backing, falhas, CLI/Go e recuperação. O teste KVM atualizado precisa aprovar, na PR de promoção, o boot de outra VM pelo perfil comprimido. Sem esse resultado, não declarar homologação integral.

**Referência QEMU:** [qemu-img convert -c](https://www.qemu.org/docs/master/tools/qemu-img.html).

## Referências

- [INTF-TERM-004 — Gerenciamento de VMs](../interfaces/terminal/INTF-TERM-004-gerenciar-vms.md)
- [ARQ-002 — Perfis e overlays](../arquitetura/ARQ-002-perfis-vm.md)
- [PERS-001 — Registro SQLite](../persistencia/PERS-001-registro-instancias.md)
