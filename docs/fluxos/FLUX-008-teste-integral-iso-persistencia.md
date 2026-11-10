<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# FLUX-008 — Integral 3: congelamento QCOW2 e recuperação de arquivos

**Estado:** implementado no código; validação de boot real da segunda VM pendente em CI de promoção.
**Motor Go:** [CreateProfileFromISO](../../insulavm/create_profile_iso.go), [StartVMWithOptions](../../insulavm/start_options.go), [ExecVM](../../insulavm/exec.go), [FreezeVM](../../insulavm/freeze.go).
**Execução:** [teste Integral 3](../../tests/integration/integral3_iso_persistence_test.go) e [runner CI](../../scripts/tests/integral-3-iso-persistence.sh).
**Guia:** [GUIA-007](../guias/GUIA-007-teste-integral-3-iso-persistencia.md).

## Objetivo

Comprovar o ciclo real **ISO instaladora Debian 13 → VM preparada → congelamento comprimido QCOW2 → nova VM → mesmo conteúdo SHA-256**. A geração ou inicialização de ISO Live não faz parte deste teste. Os nomes históricos dos arquivos e do check foram mantidos por compatibilidade com os requisitos do CI.

## Recursos

- ISO **instaladora** oficial Debian 13 (mini.iso), verificada por SHA-256; ela só serve para criar o primeiro sistema instalado.
- Instalação desassistida com 1024 MiB/1 vCPU, mínimo atual da API.
- Disco QCOW2 instalado com 10 GiB virtuais.
- Primeira e segunda VMs gerenciadas: 500 MiB/1 vCPU, boot KVM real, rede DHCP, SSH, Cloud-Init.
- Workspace persistente do convidado: `/home/insula/insulavm/workspace/created/state.txt`, independente de `tmpfs`.

## Sequência e aceite

1. Instalar Debian pela mini.iso com `CreateProfileFromISO`; validar QCOW2 de 10 GiB, ausência de backing e perfil registrado.
2. Iniciar a primeira VM por `StartVMWithOptions` com 500 MiB/1 vCPU. Confirmar domínio real, IP e SSH.
3. Criar arquivo com marcador aleatório **somente dentro da primeira VM**, via `ExecVM`. Em sessão SSH independente, confirmar conteúdo e SHA-256. Exigir `findmnt` em filesystem não volátil (rejeitar `tmpfs`, `ramfs` e `overlay`).
4. Chamar `FreezeVM` com compressão nativa padrão `qemu-img convert -c` e `ExportISO=false`. Exigir desligamento gracioso, `ForcedShutdown=false`, QCOW2 independente, ausência de backing, `qemu-img check` sem erros, origem removida **somente após salvar** o novo perfil.
5. Iniciar segunda VM pelo **perfil QCOW2 congelado**, com overlay gravável próprio, novamente 500 MiB/1 vCPU. Exigir novo UUID, nome Libvirt, IP e SSH. Iniciar uma terceira VM derivada **enquanto a segunda está ativa**; comprovar identidades distintas, dois boots reais simultâneos com o mesmo disco-base, SHA-256 preservado em ambas e escrita em uma instância invisível na outra.
6. Sem enviar ou escrever marcador na segunda VM, confirmar existência da pasta e o mesmo SHA-256 do arquivo. Repetir leitura via CLI `vm exec`, que compartilha o motor Go.
7. Desligar as duas VMs derivadas graciosamente com `ForceAfterTimeout=false`; exigir ausência dos domínios Libvirt e remoção dos respectivos overlays/chaves transitórias. Confirmar que a VM remanescente continua funcional depois da parada da outra.

O teste só emite `INSULA_INTEGRAL_3_OK` e `INSULA_INTEGRAL_3_CI_OK` depois de todos os critérios. Erro de KVM, DHCP, SSH, SHA, integridade QCOW2 ou shutdown reprova. Não há fallback mock para o teste real.

## Armazenamento e segurança

A compressão é **nativa do QCOW2**, sem ZIP ou descompactação antes do boot; a base é imutável e cada VM derivada possui seu próprio overlay. O tamanho virtual de 10 GiB não representa ocupação real, que depende da quantidade e compressibilidade dos dados. O volume `storage.profiles` precisa de espaço suficiente durante a conversão e deve permanecer montado para VMs futuras.

O congelamento não captura memória RAM, conteúdo `tmpfs` nem discos externos. A verificação do filesystem faz parte do critério de aceite, não é dispensada. Dados são criados exclusivamente no convidado; não há cópia do marcador pelo host, bind-mount externo nem reinjeção após freeze.

A limpeza de Cloud-Init e chaves SSH não remove segredos de aplicações. O teste usa diretórios e nomes aleatórios, preserva logs de falha e só limpa VMs que foram criadas nessa execução.

## CI e pendências

O workflow [integral-3-iso-workspace.yml](../../.github/workflows/integral-3-iso-workspace.yml) continua limitado a PR com origem `develop` e destino `homolog`/`main`; eventos `opened`, `synchronize`, `reopened`. Não dispara em push, schedule ou manualmente. O nome do check `integral-3-iso-workspace-persistence` foi preservado para compatibilidade, mesmo agora verificando QCOW2. O workflow falha sem KVM e publica logs de diagnóstico como artefato.

O boot real do novo QCOW2 precisa ser executado e aprovado no runner KVM para homologação. O código experimental de ISO Live permanece disponível apenas fora deste fluxo e terá sua própria validação futura.

**Referência oficial:** [QEMU qemu-img](https://www.qemu.org/docs/master/tools/qemu-img.html).
