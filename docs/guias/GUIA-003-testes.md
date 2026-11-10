<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-003 — Executar testes unitários e integrais

**Estado:** scripts criados; boot KVM real não verificado.
**Fluxo:** [FLUX-006](../fluxos/FLUX-006-teste-integral-iso-vm.md).

## Unitários

Executar na raiz do projeto:

```bash
bash scripts/tests/unit.sh
```


**Execução automática:** [Go tests](../../.github/workflows/go-tests.yml) roda em cada `push` para **qualquer branch** do repositório (incluindo branches de correção, `develop`, `homolog` e `main`), sem filtro de caminhos. Um push com vários commits gera uma execução para a ponta da branch, não uma execução por commit individual. Não executa em pushes apenas de tags, abertura de PRs, agendamentos ou disparos manuais. A suíte também pode ser executada localmente pelo comando acima. Os testes integrais continuam restritos às PRs de promoção `develop` → `homolog`/`main`.

O script executa `bash -n`, `go vet ./...`, `go test -race` com relatório de cobertura, compilação do executável, verificação de `help` e compilação da suíte com build tag `integration` **sem executar VMs**. Os testes unitários incluem a matriz da CLI, métodos públicos Go, adaptadores Libvirt/SQLite simulados, escalada de desligamento, exportação ISO simulada, SSH e deploy.

## Teste Integral 3 — Persistência em QCOW2 comprimido

~~~bash
bash scripts/tests/integral-3-iso-persistence.sh
~~~

O teste cria um perfil Debian 13 a partir da mini.iso oficial, inicia VM 500 MiB/1 CPU/10 GiB, escreve `/home/insula/insulavm/workspace/created/state.txt` num sistema de arquivos **não volátil**, congela o disco como QCOW2 comprimido e inicia outra VM 500 MiB/1 CPU diretamente por esse perfil. Verifica a integridade QCOW2, ausência de backing, o mesmo SHA-256 sem reenvio e encerramento gracioso. O instalador usa 1024 MiB por requisito da API. O check de CI mantém o nome legado `integral-3-iso-workspace-persistence` para não alterar regras existentes; ele não exporta nem inicializa ISO Live. Executa somente em promoções `develop` → `homolog`/`main`. Veja [GUIA-007](GUIA-007-teste-integral-3-iso-persistencia.md).

## Integral 2 — ZIP e resposta da VM via Go e CLI

~~~bash
bash scripts/tests/integral-2-zip.sh
~~~

A suíte independente cria uma VM Debian 12 Cloud gerenciada por InsulaVM, importa dois ZIPs com 12 arquivos cada pelas interfaces Go e CLI, executa comandos SSH reais para comparar SHA-256 e contagens antes/depois da extração e exige parada graciosa. O workflow de PR é **integral-2-zip-upload-command**; habilite esse check nas regras de merge. Veja [GUIA-006](GUIA-006-teste-integral-2-zip.md).

## Teste integral obrigatório: Alpine Virtual ISO

Para comprovar **boot de uma ISO oficial sem Cloud-Init ou SSH**, executar em host KVM/Libvirt:

```bash
bash scripts/tests/alpine-iso.sh --config /etc/insulavm/test-config.yaml
# Alternativamente, utilizar ISO local confiável:
bash scripts/tests/alpine-iso.sh --config /etc/insulavm/test-config.yaml \
  --iso /var/lib/libvirt/images/alpine-virt-3.24.4-x86_64.iso
```

A ISO Alpine Virtual 3.24.4 (~67 MB) é baixada do espelho oficial e validada via SHA-256. O teste inicia um domínio Libvirt **transitório** com **1 vCPU, 500 MiB**, abre o console serial ttyS0, efetua login root da mídia Live, executa `uname -m` e valida `/etc/alpine-release` por um marcador de saída e código zero. Envia `poweroff` dentro do guest e exige que o domínio desapareça do Libvirt. Um `virsh destroy` usado apenas para limpeza após falhas **não passa** no teste. Evidências: `storage.logs/alpine-iso-*/domain.xml` e `serial.log` (arquivos privados). Sem KVM real, este teste **falha**, não conta como sucesso.

O runner `integration.sh` chama este teste obrigatoriamente, mesmo com `--skip-live-iso` ou `--skip-go-api`. `--alpine-iso /caminho/absoluto.iso` reutiliza uma ISO local. Este cenário isola a cadeia **ISO → firmware → bootloader → kernel → shell → shutdown**; não valida o provisionamento de SSH/Cloud-Init de `vm start-iso`, que continua coberto por cenários separados. Ver [GUIA-005](GUIA-005-alpine-iso.md).

O workflow [Teste Integral 1](../../.github/workflows/integral-1-alpine-iso.yml) foi
preparado apenas para PRs com origem `develop` e destino `homolog` ou `main`, incluindo
novos commits (`synchronize`). Para bloquear merges, configurar o check
obrigatório `integral-1-alpine-iso` nas regras de proteção e incorporar o
workflow em cada branch de destino (`homolog` e `main`). Veja [GUIA-005](GUIA-005-alpine-iso.md).

## Integrais

**Executar somente em host dedicado** com Libvirt/KVM, rede DHCP, configuração e imagem Debian preparadas. O teste cria e destrói instâncias reais; os perfis criados ficam no storage configurado para inspeção.

```bash
bash scripts/tests/integration.sh \
  --config /etc/insulavm/test-config.yaml \
  --base-qcow2 /var/lib/insulavm-test/debian13-cloud.qcow2
# O teste Alpine roda primeiro e é obrigatório.
```


- O perfil precisa ter Debian 13 instalado, SSH, Cloud-Init e sudo sem senha.
- Para a instalação ISO inicial, são exigidos guestmount/guestunmount; a exportação Live requer ainda grub-mkrescue, mksquashfs, lsinitramfs e xorriso, mas está desabilitada por padrão.
- O guest precisa de acesso ao APT para instalar live-boot apenas quando o teste experimental `--live-iso` for solicitado.
- O script valida upload, execução com token, congelamento QCOW2 comprimido e clonagem; boot ISO Live somente com `--live-iso`.
- Um guest descartável ignora o botão de energia ACPI enquanto executa loop infinito; é obrigatório retornar `forced=true` e `timed_out=true`, com estado `absent` após o prazo de graça de 30 s.
- Por padrão o teste executa também `go test -tags integration ./tests/integration`, consumindo o `Engine` como biblioteca Go para registro, clone, start, status, exec, import, deploy, stop e freeze; o cenário de ISO Live é opt-in.
- `--skip-go-api` desativa apenas os cenários reais da API Go. `--live-iso` habilita explicitamente a exportação e inicialização experimental de ISO Live; `--skip-live-iso` permanece por compatibilidade.
- O aceite padrão para clonagem não depende de ISO Live: o Integral 3 exige o boot real a partir do QCOW2 congelado. ISO Live mantém homologação futura independente.

O marcador de êxito é INSULA_INTEGRATION_OK. Em erro, o script termina com código diferente de zero. Não considerar sucesso apenas porque existe um QCOW2: boot real e integridade precisam ser verificados.

## Troubleshooting

- /dev/kvm indisponível: executar no host com virtualização ativada.
- Libvirt sem permissão: ajustar acesso ao URI conforme [GUIA-002](GUIA-002-permissoes-libvirt.md).
- DHCP/SSH falha: conferir rede Libvirt, Cloud-Init e usuário configurado no guest.
- Falha na ISO: conferir live-boot no initramfs e dependências do host.
- Falha ao encerrar: analisar estado real com insula vm status -id UUID e virsh; não apagar discos de instâncias ativas.

## Cobertura e limites

A matriz de testes unitários invoca todos os subcomandos da CLI e todos os métodos públicos de `Engine` pelo menos em um caminho de validação ou cenário controlado. Isso **não significa** cobertura total dos caminhos ou das linhas: a suíte integral é necessária para os fluxos positivos dependentes de Libvirt/SSH. `CreateProfileFromISO` (instalação automática de Debian 13 via Preseed) tem testes unitários com ferramentas do host simuladas e opção de teste integral com ISO real; o cenário de boot de instalador ainda não foi executado em KVM.

O `vm stop` utiliza `virsh shutdown UUID --mode acpi` para que o teste de botão de energia ignorado seja determinístico mesmo quando o guest tiver QEMU Guest Agent. O timeout cobre também a tentativa inicial de `shutdown`; ao expirar, somente a VM registrada pode receber `virsh destroy`. Esse comportamento usa as opções documentadas no [manual oficial virsh](https://www.libvirt.org/manpages/virsh.html). O shutdown/limpeza completa pode exceder a janela de 30 s por operações de Libvirt e armazenamento.

## Instalar Debian 13 da ISO durante a suíte integral

Com `--installer-iso`, o script instala Debian da ISO e pode usar o QCOW2 produzido como base quando `--base-qcow2` não for fornecido. Requer `virt-install`, `openssl`, `guestmount`, `guestunmount`, acesso a repositórios APT no guest e rede Libvirt.

```bash
bash scripts/tests/integration.sh \
  --config /etc/insulavm/test-config.yaml \
  --installer-iso /isos/debian-13-amd64-netinst.iso
```

Com a suíte Go habilitada, também executa o teste opt-in `TestCreateProfileFromDebianInstallerISO` que chama diretamente a biblioteca, inicia uma VM do perfil criado e, com testes Live habilitados, tenta a exportação da própria VM e novo boot pela ISO. Este teste prolongado não é executado no CI padrão sem KVM.
