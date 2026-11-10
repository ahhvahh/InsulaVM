<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-007 — Integral 3: freezing QCOW2 comprimido e boot de nova VM

**Estado:** teste/CI migrados; execução real KVM da revisão atual pendente.
**Fluxo:** [FLUX-008](../fluxos/FLUX-008-teste-integral-iso-persistencia.md).

## Pré-requisitos

Host amd64 com KVM/Libvirt, rede Libvirt `default` com DHCP, Go, virt-install, qemu-img, libguestfs/guestmount, OpenSSL, genisoimage e permissões QEMU. O instalador Debian precisa de rede e repositórios acessíveis. **Não** são exigidos grub-mkrescue, mksquashfs ou xorriso para exportação de ISO Live.

## Executar

```bash
bash scripts/tests/integral-3-iso-persistence.sh
```

Para reutilizar uma ISO instaladora confiável e uma configuração isolada:

```bash
bash scripts/tests/integral-3-iso-persistence.sh \
  --config /etc/insulavm/test-config.yaml \
  --installer-iso /isos/debian-13-mini.iso
```

O script usa por padrão `data/integral-3-ci/state`, baixa a mini.iso Debian 13 amd64 (~64 MB), compara o SHA-256 com o manifesto da distribuição e instala o sistema Debian. Os nomes antigos de arquivos do teste e do workflow foram preservados para não modificar desnecessariamente os contratos do CI.

## Critérios obrigatórios

| Etapa | Aprovação exigida |
| --- | --- |
| Instalação inicial | Debian 13 pela ISO instaladora oficial; 1024 MiB/1 vCPU na instalação |
| Disco instalado | Perfil QCOW2 de 10 GiB virtuais, sem backing |
| Primeira VM | Boot KVM com 500 MiB/1 vCPU, DHCP, SSH |
| Escrita | Criar `/home/insula/insulavm/workspace/created/state.txt` dentro da VM |
| Fonte de dados | `findmnt` mostra filesystem persistente (não `tmpfs`) |
| Persistência anterior | Confirmação de bytes e SHA-256 em sessão SSH independente |
| Freeze | `FreezeVM`, QCOW2 comprimido independente, sem ISO, shutdown gracioso |
| Integridade | `qemu-img info` sem backing; `qemu-img check` bem-sucedido |
| Segunda VM | Novo UUID e boot real KVM do perfil congelado, 500 MiB/1 vCPU |
| Persistência posterior | Arquivo e mesmo SHA-256, sem reenvio de dados |
| CLI/Go | Leitura do arquivo também por `vm exec` |
| Parada | Shutdown gracioso sem força, domínio ausente e overlay limpo |

Sucesso exige `INSULA_INTEGRAL_3_OK` e `INSULA_INTEGRAL_3_CI_OK` com código zero. Não considerar produção pronta apenas porque o QCOW2 existe.

## Integração CI

Workflow: [integral-3-iso-workspace.yml](../../.github/workflows/integral-3-iso-workspace.yml).
O workflow **só** executa em PRs originadas em `develop` destinadas a `homolog` ou `main`. Não executa em pushes, schedules, disparos manuais ou PRs de outras branches. O check mantém o identificador legado `integral-3-iso-workspace-persistence` para não exigir migração imediata das regras de branch. O teste unitário de contrato valida esses gatilhos.

A suíte normal `Go tests` executa em push e compila os testes com tag `integration`, mas não substitui a verificação com hipervisor real.

## Diagnóstico

- Preservar os logs do instalador, Libvirt, do guest e do `qemu-img` nos artefatos do job; nunca publicar chaves privadas nem imagens completas.
- Verificar a última etapa comprovada por logs, sem atribuir automaticamente erro de boot a DHCP.
- Se `findmnt` indicar `tmpfs`, corrigir o caminho de armazenamento do convidado; um QCOW2 nunca contém arquivos existentes apenas em RAM.
- Se `qemu-img check` falhar, não iniciar a segunda VM e preservar o overlay quando ainda disponível.
- A imagem comprimida continua diretamente bootável via QEMU; nenhuma extração é necessária. O recurso ISO Live fica separado para implementação/homologação futura.
