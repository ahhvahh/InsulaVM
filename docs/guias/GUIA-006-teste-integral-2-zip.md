<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-006 — Executar o Teste Integral 2 (ZIP e comandos)

**Estado:** implementado em branch específica; verificar resultados reais na PR.
**Fluxo:** [FLUX-007](../fluxos/FLUX-007-teste-integral-zip-vm.md).
**Código:** [integral2_zip_test.go](../../tests/integration/integral2_zip_test.go).

## Executar

No host Debian/Ubuntu com KVM, Libvirt, rede default DHCP e Go:

~~~bash
bash scripts/tests/integral-2-zip.sh
~~~

Sem download, com QCOW2 Debian Cloud de origem confiável:

~~~bash
bash scripts/tests/integral-2-zip.sh \
  --config /etc/insulavm/test-config.yaml \
  --qcow2 /var/lib/insulavm-test/debian-12-generic-amd64.qcow2
~~~

Quando --config não é informado, o script cria uma configuração privada em data/integral-2-ci/config.yaml, usando data/integral-2-ci/state como armazenamento isolado. Não reaproveita VM de produção e não altera perfis existentes.

Por padrão o runner obtém a imagem oficial Debian 12 generic QCOW2 de https://cloud.debian.org/images/cloud/bookworm/latest/ e valida SHA-512 pelo manifesto SHA512SUMS oficial. Uma imagem local fornecida com --qcow2 é confiada ao operador.

## Verificações

1. Cria perfil a partir do QCOW2 e sobe uma VM gerenciada com 1 CPU e 1024 MiB de RAM, DHCP, Cloud-Init e SSH por chave.
2. Gera api-payload.zip com 12 arquivos (inclusive binário) e usa o método Go ImportToVM, capturando os eventos de progresso.
3. Usa ExecVM para checar SHA-256 do ZIP recebido, contar arquivos contidos no ZIP, extrair com Python 3, contar arquivos extraídos e verificar hash do binário. Cada comando deve devolver marcador específico.
4. Comprova propagação de erro executando um comando que sai com status não zero.
5. Repete a importação e as verificações com cli-payload.zip por meio da CLI vm import e vm exec, sem duplicar lógica de importação no teste.
6. Solicita shutdown gracioso sem permissão de fallback forçado; exige retorno Graceful=true, domínio ausente e remoção do overlay e chaves temporárias.
7. Publica INSULA_INTEGRAL_2_OK somente se todas as etapas passarem.

Erro deixa logs sob data/integral-2-ci/state/logs e provoca saída não zero. Um mecanismo emergencial remove a VM temporária se necessário, sem declarar sucesso.

## PRs e execução obrigatória

Workflow: [.github/workflows/integral-2-zip.yml](../../.github/workflows/integral-2-zip.yml). Dispara ao abrir e atualizar PRs destinadas a develop, homolog e main (incluindo cada commit recebido por synchronize). O status estável do job é integral-2-zip-upload-command. O workflow não filtra diretórios nem aceita sucesso parcial; evidências são anexadas à execução.

Para bloquear merge quando o teste falhar, adicionar integral-2-zip-upload-command aos checks exigidos nas regras da branch de destino. Incorporar o workflow em cada branch-alvo; a branch promolog precisa existir. O Teste Integral 1 (ISO Alpine) permanece separado. Não ativar outros workflows.

## Diagnóstico

- Sem /dev/kvm: o runner encerra com falha explícita.
- Sem rede default DHCP: ativar a rede Libvirt e verificar virsh net-info default.
- Erro SSH/DHCP: examinar logs de boot, NoCloud e Libvirt; não aumentar timeout sem identificar a última etapa comprovada.
- Erro de transferência: analisar estágios uploading/verifying/extracting no log.
- Conteúdo incorreto: comparar marcadores SHA e contagens produzidos pelo guest.
- Stop forçado: o teste não passa, mesmo que recursos sejam limpos posteriormente.
- Race de desligamento em domínio transitório: se `virsh domstate` falhar após um `list`, o motor consulta `virsh list --all --uuid` novamente. Somente ausência confirmada com comando bem-sucedido permite finalizar a limpeza; erro de conexão ou domínio ainda listado preserva os discos.
- A falha observada no CI ao encerrar após upload e execução (estado Libvirt desconhecido) deve ser revalidada no Integral 2 real; mocks não comprovam desligamento gracioso.

**Limitação importante:** ImportToVM transfere o ZIP como um arquivo único envolto em TAR.GZ temporário, que é extraído automaticamente pela ferramenta. Quem extrai o conteúdo ZIP é um comando posterior enviado à VM; isso é intencional e respeita o contrato atual.

## Motivo do Debian 12 neste teste

A imagem Debian 13 Cloud original exibiu apenas "Booting Debian GNU/Linux" em loop no console do QEMU, sem chegar ao kernel, impedindo testar SSH e upload. Essa observação corresponde ao [Debian bug #1111240](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1111240), que descreve o problema do GRUB quando a saída gráfica e serial estão habilitadas em determinados dispositivos virtuais. O Teste Integral 2 usa Debian 12 Cloud para testar exclusivamente a transferência e execução sem alterar o motor de boot nem adulterar a imagem original.
