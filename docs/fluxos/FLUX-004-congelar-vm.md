<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# FLUX-004 — Congelar VM gerenciada como perfil reutilizável

**Estado:** implementado para gerar um QCOW2 independente de uma VM já instalada; não produz ISO de instalação ou ISO Live.
**Versão aprovada:** nenhuma.
**Evidências:** [FreezeVM](../../insulavm/freeze.go), [ShutdownForFreeze](../../pkg/vm/freeze.go), [FreezeFromOverlay](../../pkg/profile/freeze.go) e [testes de conversão](../../pkg/profile/freeze_test.go).
**Interface CLI:** [INTF-TERM-004](../interfaces/terminal/INTF-TERM-004-gerenciar-vms.md).
**Perfil de origem:** [ARQ-002](../arquitetura/ARQ-002-perfis-vm.md).

## Objetivo

Publicar as modificações realizadas em uma VM temporária como um **novo perfil nomeado e reutilizável**, sem sobrescrever seu perfil de origem. Novas VMs partem do QCOW2 congelado e gravam suas alterações em overlays próprios.

## Entradas e pré-condições

- `VMID`: UUIDv7 de VM gerenciada que consta como `running` no SQLite **e** está `running` ou `blocked` no Libvirt.
- `Name`: nome novo e válido para perfil; não pode existir em `storage.profiles`.
- `ShutdownTimeout`: prazo positivo para desligamento, padrão de 3 minutos.
- Perfil original com sistema instalado, `cloud-init` funcional e acesso SSH. A VM original precisa estar provisionada com `ssh.sudo_mode: passwordless`.
- Disco `overlay.qcow2` regular dentro de `storage.runs/insula-vm-*` e domínio cujo UUID corresponda ao registro.

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
    C->>L: shutdown UUID
    loop Até domínio ficar ausente ou expirar prazo
        C->>L: list/domstate
    end
    alt Desligamento comprovado
        C->>Q: convert -f qcow2 -O qcow2 overlay novo/base.qcow2
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
2. Via `ExecVM` e `sudo -n`, executar `cloud-init clean --logs --machine-id --seed`, remover `authorized_keys` da conta SSH configurada e remover `/etc/ssh/ssh_host_*`. Isso prepara a clonagem, **mas não limpa segredos de aplicativos**.
3. Solicitar desligamento normal do convidado com `virsh shutdown`; **não destruir** o domínio antecipadamente.
4. Só converter após Libvirt informar `absent` para o domínio transitório. Não converter overlay em uso.
5. Gerar `profiles/<nome>/versions/v001/base.qcow2` por `qemu-img convert`; confirmar formato QCOW2 e ausência de backing file; publicar `profile.yaml` ao final. O registro é exclusivo e não substitui nomes existentes.
6. Executar `StopVM` para remover arquivos temporários da VM original **depois** de confirmar que o perfil foi salvo.
7. Reutilizar a base por `vm start -profile <nome>`, inclusive com RAM/vCPUs sobrescritos apenas na instância.

## Falhas e recuperação

| Falha | Efeito verificado |
| --- | --- |
| Nome inválido/duplicado, VM sem sudo ou não ativa | Recusa antes do comando de preparação no convidado |
| Limpeza do Cloud-Init falha | Operação interrompida; VM de origem não é desligada pelo fluxo |
| Desligamento falha ou excede prazo | Não converte o disco; overlay é preservado |
| Conversão ou validação QCOW2 falha | Diretório de novo perfil parcial é removido; overlay original preservado |
| Perfil criado, falha ao limpar VM original | Retorna erro e o `VMFreezeResult.ImagePath` já preenchido; perfil permanece |
| Contexto cancelado | Operação interrompida; consultar `vm status` e arquivos restantes antes de recuperar |

A imagem resultante ainda pode conter tokens, dados privados, arquivos temporários e configurações próprias de aplicações. O operador deve remover esses conteúdos antes de congelar. Não existe sanitização automática abrangente.

## Resultado

`FreezeVM(ctx, VMFreezeRequest)` retorna `VMFreezeResult` com `VMID`, `ProfileName`, `ImagePath`, `SourceStopped` e `SourceCleaned`, além de erro quando aplicável. A CLI usa código de saída `0` no sucesso e `1` em erro. Os testes automatizados empregam Libvirt/QEMU simulados; **boot e congelamento completo em KVM real não foram verificados aqui**.

## Referências

- [INTF-TERM-004 — Gerenciamento de VMs](../interfaces/terminal/INTF-TERM-004-gerenciar-vms.md)
- [ARQ-002 — Perfis e overlays](../arquitetura/ARQ-002-perfis-vm.md)
- [PERS-001 — Registro SQLite](../persistencia/PERS-001-registro-instancias.md)
