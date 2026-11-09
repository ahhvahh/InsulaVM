<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-007 — Teste Integral 3: ISO Live com /tmp/workspace preservado

**Estado:** script/CI implementados; boot real deve ser verificado na PR.
**Fluxo:** [FLUX-008](../fluxos/FLUX-008-teste-integral-iso-persistencia.md).

## Pré-requisitos

Host amd64 com KVM, Libvirt, rede default DHCP, Go, virt-install,
qemu-img, libguestfs, guestmount/guestunmount, mksquashfs, grub-mkrescue,
xorriso, lsinitramfs, openssl, genisoimage e ACL do UID QEMU.

## Execução

~~~bash
bash scripts/tests/integral-3-iso-persistence.sh
~~~

Para usar ISO e configuração privadas de um host de teste:

~~~bash
bash scripts/tests/integral-3-iso-persistence.sh \
  --config /etc/insulavm/test-config.yaml \
  --installer-iso /isos/debian-13-mini.iso
~~~

O modo padrão utiliza diretório isolado data/integral-3-ci/state, sem
acessar armazenamento de produção. Obtém mini.iso Debian 13 amd64 oficial
(~64 MB), verifica SHA-256 com o manifesto da distribuição e instala
pacotes Debian pela rede.

## Verificações

| Etapa | Aprovação exigida |
| --- | --- |
| ISO inicial | Instalação real via mini.iso |
| Disco | QCOW2 de 10 GiB |
| Instalação | 1024 MiB no instalador, mínimo da API atual |
| Primeira VM | 500 MiB/1 vCPU no Libvirt |
| Criação | mkdir /tmp/workspace/created + arquivo state.txt via ExecVM |
| Persistência pré-freeze | SHA-256 do arquivo e conteúdo pela conexão SSH |
| Freeze | Desligamento gracioso, QCOW2 plano e ISO Live criada |
| Segunda VM | Novo UUID, 500 MiB/1 vCPU e boot real pela ISO gerada |
| Persistência pós-boot | Pasta/arquivo existem com o mesmo SHA-256, sem reupload |
| CLI | vm exec confirma arquivo e pasta da VM restaurada |
| Shutdown final | Sem forced stop, domínio ausente e scratch removido |

Sucesso exige saída INSULA_INTEGRAL_3_OK e
INSULA_INTEGRAL_3_CI_OK, com exit code zero.

## Integração CI

Workflow: [.github/workflows/integral-3-iso-workspace.yml](../../.github/workflows/integral-3-iso-workspace.yml).
Somente PRs para **develop, homolog e main**: opened, synchronize (novo commit)
e reopened. Não executa em push direto, merge, alteração de título, agendamento
ou disparo manual por workflow_dispatch. Ao entrar outro commit na mesma PR,
execuções antigas são canceladas para reduzir custo. Status estável:
**integral-3-iso-workspace-persistence**.

Para bloquear merge quando falhar, habilitar esse check como obrigatório
nas regras das branches develop, homolog e main. O workflow precisa estar
incorporado à branch-base; homolog ainda não existe e não foi criada.

## Diagnóstico e limites

- Verificar boot pelo console serial e logs do convidado antes de culpar DHCP.
- O /tmp não possui garantia de persistência em sistemas Linux; o teste deve
  falhar se os dados desaparecerem, sem recolocá-los via host.
- Em erro de freeze, preservar evidências antes de remover o overlay.
- A segunda VM da ISO usa scratch temporário gerado por StartVMFromISO,
  e não um segundo disco persistente de 10 GiB.
- Não enviar ISO nem segredos privados como artefatos do CI. Os logs ficam
  disponíveis no job por sete dias.
