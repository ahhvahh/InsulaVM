<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-005 — Teste integral obrigatório da ISO Alpine Linux

**Estado:** teste implementado; boot KVM real ainda não verificado.
**Fluxo:** [FLUX-006](../fluxos/FLUX-006-teste-integral-iso-vm.md).
**Código:** [alpine_iso_test.go](../../tests/integration/alpine_iso_test.go) e [alpine-iso.sh](../../scripts/tests/alpine-iso.sh).

## Objetivo e escopo

Verificar inicialização direta de ISO Alpine Virtual original, execução comprovada de comando no convidado e desligamento normal. O teste usa o console serial disponibilizado pela ISO Alpine Virtual e não depende de DHCP, Cloud-Init ou SSH. É um teste independente do fluxo de VM gerenciada por perfil QCOW2, sem nova operação pública na CLI ou biblioteca Go. Não substitui os testes do motor InsulaVM para ISO Live Debian.

## Pré-condições

- Host Linux x86_64, KVM real (/dev/kvm), Libvirt, virsh, Go 1.23+.
- Configuração válida com URI Libvirt, acesso ao hipervisor e pasta de logs gravável.
- Para download automático: curl, sha256sum, HTTPS para Alpine e aproximadamente 67 MB livres.
- QEMU precisa conseguir ler a ISO. O runner faz download em diretório legível pelo serviço QEMU sob /var/tmp.
- Host de testes confiável: mídia Live Alpine permite login root sem senha por console.

## Executar

~~~bash
bash scripts/tests/alpine-iso.sh --config /etc/insulavm/test-config.yaml

# Alternativa: ISO local confiável
bash scripts/tests/alpine-iso.sh \
  --config /etc/insulavm/test-config.yaml \
  --iso /var/lib/libvirt/images/alpine-virt-3.24.4-x86_64.iso
~~~

Para integrar ao pipeline integral:

~~~bash
bash scripts/tests/integration.sh \
  --config /etc/insulavm/test-config.yaml \
  --base-qcow2 /var/lib/insulavm-test/debian13-cloud.qcow2
# Opcional: --alpine-iso /caminho/absoluto.iso
~~~

A ISO oficial Alpine Virtual 3.24.4 é obtida em https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/x86_64/ . O runner compara SHA-256 calculado e o manifesto publicado pelo espelho oficial. Com mídia local, a verificação de procedência cabe ao operador.

## Contrato e evidências

1. **ISO:** mídia existente e não vazia, com verificação SHA-256 no download automático.
2. **Libvirt:** domínio transitório com 1 vCPU e 500 MiB, boot por CD-ROM e parâmetros confirmados via dumpxml.
3. **Boot:** console serial ttyS0 apresenta login Alpine; autenticação root da mídia Live e prompt de shell.
4. **Comando real:** exigir versão Alpine 3.24.4 em /etc/alpine-release, arquitetura x86_64 e duas linhas emitidas dentro do guest: INSULA_ALPINE_GUEST_OK_<token>:3.24.4 e INSULA_ALPINE_EXIT_<token>:0. O token exclusivo impede que um simples eco da entrada seja confundido com sucesso.
5. **Desligamento:** enviar poweroff pelo shell. A confirmação exige ausência do domínio em virsh list --all --name.
6. **Limpeza:** em falha, tentar destroy apenas do domínio transitório aleatório da execução. Desligamento forçado não passa no aceite.

Logs privados são mantidos em storage.logs/alpine-iso-*/domain.xml e serial.log. Sucesso só é aceito quando o script termina com INSULA_ALPINE_INTEGRATION_OK depois da validação efetiva do comando e desligamento.

O cenário é **obrigatório** na suíte integral mesmo quando --skip-live-iso ou --skip-go-api forem selecionados. Sem KVM real o script falha explicitamente. Um go test isolado sem mídia fornecida pode registrar skip, mas isso não equivale à aprovação do runner obrigatório.

## Limitações e segurança

- Não comprova o boot das ISOs Live produzidas pelo InsulaVM, que continuam exigindo cenários de SSH/Cloud-Init separados.
- Usa console serial TCP somente em 127.0.0.1 durante a execução; nunca abre conexão externa.
- O boot do Alpine Virtual não instala sistema em disco e não altera imagem original.
- O login root sem senha só é aceitável em laboratório descartável e confiável.
- Execução real no hypervisor deve ser registrada separadamente; compilação de teste não prova boot.

## Execução automática em PRs — Teste Integral 1

O arquivo [Integral 1 - Alpine ISO](../../.github/workflows/integral-1-alpine-iso.yml)
define o check estável **integral-1-alpine-iso** no GitHub Actions.
A execução é disparada por `pull_request` para as branches `develop`,
`promolog` e `main`: abertura, reabertura, novos commits na PR
(`synchronize`), marcação como pronta para revisão e edição da PR (inclusive
mudança de base). Não há filtros por arquivos nem cancelamento automático de
uma execução anterior, portanto todo evento de commit recebido gera nova
tentativa.

O job usa `ubuntu-24.04` hospedado pelo GitHub, com token somente de leitura,
instala Libvirt/QEMU, **falha** se /dev/kvm estiver ausente, executa
`scripts/tests/alpine-iso.sh` e publica `serial.log` e `domain.xml`
como artefatos, inclusive quando o boot falha. Não usa runner self-hosted
para executar código de PR pública. O GitHub não garante suporte oficial à
virtualização aninhada nos runners hospedados; o resultado real de CI
precisa ser observado. Uma falha de infraestrutura não é considerada sucesso.

### Tornar o check bloqueante para merge

1. Incorporar este workflow em **cada branch de destino**. Sem o arquivo na
   base da PR, não assumir que o GitHub vai processar seu evento.
2. No GitHub, acessar **Settings > Rules > Rulesets** (ou **Branches**)
   e criar proteção para `develop`, `promolog` e `main`.
3. Habilitar **Require status checks to pass before merging** e selecionar
   o check `integral-1-alpine-iso` (origem GitHub Actions). Recomenda-se
   desabilitar bypass para pessoas que não possam ignorar o gate.
4. Criar ou atualizar uma PR de teste e verificar **Actions > Integral 1 -
   Alpine ISO**, saída `INSULA_ALPINE_INTEGRATION_OK`, artefatos e status
   exigido no painel da PR. Repetir após um novo commit.

Em 09/11/2026, `develop` e `main` existiam e não estavam protegidas;
`promolog` ainda não existia. Este commit prepara o workflow na branch
de desenvolvimento específica, mas **não afirma** que as regras de proteção
já foram aplicadas. Nenhum outro workflow antes desativado foi reativado.

O status deve ser avaliado **no commit mais recente**. Quando um PR possuir
conflitos de merge, o GitHub pode não disparar `pull_request`; esses conflitos
precisam ser resolvidos antes de obter o check exigido.
