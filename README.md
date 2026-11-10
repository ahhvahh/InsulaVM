# InsulaVM

Ferramenta em **Go** para criar e gerenciar VMs Linux descartáveis com **QEMU/KVM + Libvirt**, executar comandos por SSH, importar arquivos e registrar logs no hospedeiro. Funciona como CLI e biblioteca Go importável.

**Implementado:** perfis QCOW2 reutilizáveis, identidade por VM com Cloud-Init NoCloud, registro SQLite, criação e encerramento de instâncias, importação, deploy, execução de pipelines, congelamento de uma VM preparada em novo perfil nomeado e encerramento em lote de VMs gerenciadas.

**Ainda não implementado:** checkout automático de repositórios Git, instalador interativo por console e contrato canônico `CreateVM` com origem por UUID e mudança de disco. **Implementado e não validado em KVM real:** instalação automática de Debian 13 amd64 a partir de ISO local usando `virt-install`/Preseed, com registro de perfil QCOW2. **Novo, sem validação KVM real:** clonagem independente de perfis, exportação de ISO Live Debian com live-boot, boot da ISO Live e desligamento gracioso com escalada. A imagem-base QCOW2 deve ter um sistema Linux previamente instalado e preparado com Cloud-Init, SSH, rede DHCP e, para congelamento, sudo sem senha.

## Contrato canônico CLI / biblioteca Go

O padrão de integração para **ambas** as interfaces está em [CONTR-001 — Operações canônicas](docs/contratos/CONTR-001-operacoes-canonicas.md) e [CONTR-002 — Transporte STDIO](docs/contratos/CONTR-002-transporte-stdio.md). Os oito métodos de referência são `CreateProfileFromISO`, `CreateVM`, `UploadArchive`, `ExecuteScript`, `ExecuteCommand`, `StopVM`, `StopAllVMs` e `FreezeVM`.

**Atenção:** esta padronização define a API-alvo. O pacote atual implementa apenas parte dela; consulte a matriz de situação nos contratos antes de integrar. Métodos legados da CLI e biblioteca permanecem documentados como implementados. Nenhum método inexistente deve ser considerado disponível apenas por constar na especificação.

## Testes guiados da VM e diagnóstico SSH

A CLI `test` permite instalar, preparar, diagnosticar e exportar uma VM,
sem depender de GitHub Actions.

```bash
go build -o insula .
./insula test doctor
./insula test download
./insula test install -profile debian13-mini
./insula test boot -profile debian13-mini
./insula test ssh -id UUID_DA_VM
./insula test shell -id UUID_DA_VM
./insula test iso -id UUID_DA_VM -name debian13-preparado -confirm

# Do início ao fim, sem interação, usando script confiável:
./insula test cycle -profile debian13-mini -name debian13-pronto \
  -startup-script /caminho/preparar.sh -confirm
```

Erros de autenticação agora informam tentativas e limites independentes de DHCP
e SSH. `test boot` preserva a VM após falha para diagnóstico.
A `main` não é modificada. O workflow **Go tests** executa a suíte unitária completa apenas em `push` para `develop`; os testes integrais 1–3 executam separadamente em PRs para `develop`, `homolog` ou `main`.
Veja [GUIA-004 — comandos test](docs/guias/GUIA-004-testes-guiados.md).

Alternativamente, o roteiro manual de ponta a ponta permite instalar, abrir
uma sessão interativa e exportar a ISO após confirmação:

```bash
bash scripts/tests/manual-debian-mini-iso.sh all
```

Consulte [GUIA-004 — roteiro manual](docs/guias/GUIA-004-teste-manual-debian-mini-iso.md).

## Laboratorio Debian 13 minimo (5 GiB, 1 CPU, 1 GiB RAM)

Na pasta com `./insula` e `config.yaml` ja configurados:

```bash
bash /CAMINHO/InsulaVM/scripts/preparar-deb13-abioma-tests.sh all
```

O script usa a `mini.iso` atual da distribuicao `trixie`, com verificacao
SHA256 pelo metodo `test download`, e cria o perfil
`deb13-abioma-tests`. Ele abre uma sessao SSH para preparacao manual;
`exit` sai do terminal sem desligar a VM. Depois pergunta se deve congelar
a VM, encerrar e salvar `deb13-abioma-tests-preparado` como QCOW2 e ISO Live.

Para reconectar ao shell ou salvar mais tarde, use respectivamente `open`
e `export` no lugar de `all`. Nao execute `vm stop` antes de exportar:
isso elimina as alteracoes do overlay. A instalacao minima em disco de 5 GiB
ainda requer validacao KVM real e pode ficar sem espaco se muitos pacotes forem
instalados. O script nao habilita GitHub Actions.

## Começar

Requisitos: **Go 1.23+**, Linux com KVM/QEMU/Libvirt, `virsh`, `qemu-img`, rede Libvirt com DHCP, permissões para operar o hipervisor e geração de ISO NoCloud (`genisoimage` ou `xorriso`). O `config.yaml` é opcional na linha de comando, mas o arquivo precisa existir no local esperado.

```bash
go build -o insula .
./insula config check
./insula profile list

# Registrar um disco com Linux já instalado
./insula profile register -name debian13 -image ./debian13.qcow2 \
  -memory-mib 2048 -vcpus 2 -disk-gb 8

# Criar uma VM e consultar seu UUID
./insula vm start -profile debian13
./insula vm list
```

O registro de perfil é necessário apenas se ele ainda não existir. Consulte [GUIA-001](docs/guias/GUIA-001-operar-insulavm.md) para instalação de dependências, acesso SSH, importações e execução.

## Instalar um perfil a partir da ISO Debian 13

O comando cria uma VM instaladora temporária, instala Debian 13 em QCOW2 com Preseed, valida os componentes do sistema e registra o perfil. **Requer internet no instalador** para os pacotes de Cloud-Init/SSH/sudo/live-boot; não foi validado em host KVM real.

```bash
ISO="$(bash scripts/baixar-debian.sh)"  # baixa e valida SHA256; retorna caminho absoluto
./insula profile create-from-iso -name debian13 -iso "$ISO" \
  -memory-mib 2048 -vcpus 2 -disk-gb 12 -install-timeout 90m
./insula vm start -profile debian13
```

Biblioteca Go: `engine.CreateProfileFromISO(ctx, insulavm.CreateProfileFromISORequest{...})`. Dependências adicionais: `virt-install`, `openssl`, `guestmount`, `guestunmount` e acesso ao Libvirt. Veja [INTF-TERM-007](docs/interfaces/terminal/INTF-TERM-007-instalar-debian-iso.md).

## Exportar ISO diretamente de uma instância

```bash
./insula vm export-iso -id UUID_DA_VM -name debian13-live -prepare-live-boot
```

Essa operação instala `live-boot` no guest (quando solicitada), congela o QCOW2, **encerra e remove a VM de origem** e exporta uma ISO Live. A biblioteca expõe `ExportVMToLiveISO(ctx, VMExportISORequest)`; se a exportação falhar após o congelamento, o perfil QCOW2 fica preservado para nova tentativa.

## Preparar e congelar um ambiente

```bash
# Depois de configurar a VM de origem, desligar normalmente, salvar QCOW2 e ISO Live.
# -config e -id identificam a instancia; o ultimo argumento e o destino da ISO.
./insula vm freeze -config ./config.yaml -id UUID_DA_VM ./imagens/debian13-go-testes.iso

# O perfil reutilizavel se chamara debian13-go-testes
./insula vm start -config ./config.yaml -profile debian13-go-testes -memory-mib 4096 -vcpus 4

# Tambem aceita caminho absoluto, ex.: /tmp/debian13-go-testes.iso.
# Uso antigo com -name continua valido (usa o diretorio exports do perfil).
./insula vm freeze -config ./config.yaml -id UUID_DA_VM -name debian13-go-testes

# Somente QCOW2, sem Live ISO:
./insula vm freeze -config ./config.yaml -id UUID_DA_VM -name debian13-go-disco -export-iso=false
```

Para instalar dependências administrativas durante a primeira inicialização, a API Go oferece `VMStartOptions.StartupScriptPath`: caminho absoluto de script confiável, incluído no Cloud-Init e executado como root antes de a VM ficar pronta. Veja [ARQ-004](docs/arquitetura/ARQ-004-biblioteca-go.md).

O congelamento **encerra e remove a VM original** após consolidar seu overlay em um QCOW2 autônomo. Por padrão, `vm freeze` também prepara e exporta uma ISO Live Debian; se esta segunda etapa falhar, o perfil QCOW2 **permanece salvo**. A ISO gerada fica em `data/profiles/NOME/exports/NOME-live.iso`. Não execute `vm stop` antes de congelar, pois isso apaga o overlay temporário.

**Destino da ISO:** o último argumento do `vm freeze` é um arquivo terminado em `.iso`. Um caminho iniciado com `./` é resolvido a partir da pasta de onde o comando é executado, independentemente da localização de `config.yaml`. O diretório será criado quando necessário; o InsulaVM nunca sobrescreve uma ISO já existente. A preparação do `live-boot` ocorre antes do desligamento e a geração da ISO só começa **depois de o Libvirt confirmar que a VM encerrou**. A origem é removida somente após salvar o QCOW2 e publicar a ISO.

**Desligamento e recuperação:** o domínio recém-criado declara ACPI e canal do QEMU Guest Agent. Se uma VM **antiga** ignorar o desligamento gracioso, use `-force-freeze-shutdown` apenas se aceitar o risco de disco crash-consistent. Após uma tentativa antiga que já tenha limpado chaves SSH, use `-skip-guest-cleanup -prepare-live-boot=false` para não repetir SSH; `-recover-stopped` é permitido somente para disco temporário intacto e domínio já ausente no Libvirt (ver FLUX-004). Sem `-force-freeze-shutdown`, a ferramenta **não** força o desligamento. Leia [FLUX-004](docs/fluxos/FLUX-004-congelar-vm.md).

## Deploy, execução e encerramento

```bash
./insula vm deploy -id UUID_DA_VM -source ./pacote.tar.gz \
  -script ./instalar.sh -extract

./insula vm stop-all -dry-run  # apenas mostra os alvos gerenciados
./insula vm stop-all           # encerra os domínios registrados
```

`stop-all` consulta o SQLite e o estado real no Libvirt, sem encerrar VMs externas não registradas. Consulte [INTF-TERM-004](docs/interfaces/terminal/INTF-TERM-004-gerenciar-vms.md) e [INTF-TERM-005](docs/interfaces/terminal/INTF-TERM-005-deploy-vm.md).

## Configuração, API Go e documentação

- [config.yaml](config.yaml) — diretórios, rede Libvirt, conta SSH, sudo e destino de importação.
- [insula.yaml.example](insula.yaml.example) — perfil, setup, imports e jobs de pipeline.
- [Exemplos de perfis](examples/profiles/debian-minimal/profile.yaml) — metadados ilustrativos; não incluem discos instalados.
- [API Go e integração](docs/arquitetura/ARQ-004-biblioteca-go.md) — `github.com/ahhvahh/InsulaVM/insulavm`.
- [GUIA-001 — Operação](docs/guias/GUIA-001-operar-insulavm.md) e [GUIA-002 — Permissões Libvirt](docs/guias/GUIA-002-permissoes-libvirt.md).
- [Índice completo da documentação](docs/README.md).

A API ainda é experimental, sem compromisso de compatibilidade de versão estável. O projeto utiliza a convenção [A-Bioma Doc Standard](docs/README.md) para organizar a documentação, sem alegar certificação formal.

## Teste Integral 3 — ISO, congelamento e /tmp/workspace

Somente para PRs destinadas a **develop**, **homolog** ou **main**, o [workflow Integral 3](.github/workflows/integral-3-iso-workspace.yml) instala Debian 13 a partir da **mini.iso (~64 MB)** em um QCOW2 de **10 GiB**, inicia uma VM de **1 vCPU e 500 MiB**, cria um arquivo em `/tmp/workspace` através de `ExecVM`, congela e exporta uma ISO Live, inicializa outra VM pela ISO gerada e confirma o **mesmo SHA-256 do arquivo** sem novo upload. Por fim encerra a segunda VM graciosamente. A instalação inicial usa **1 GiB**, mínimo exigido por `CreateProfileFromISO`; ambas as VMs gerenciadas usam 500 MiB.

~~~bash
bash scripts/tests/integral-3-iso-persistence.sh
~~~

**Status de CI:** `integral-3-iso-workspace-persistence`. Para bloquear merges em falha, exigir esse check nas regras de proteção das três branches, após o workflow estar incorporado à branch-base. Os únicos eventos automáticos são abertura, reabertura ou novo commit da PR; execuções anteriores da mesma PR são canceladas. Nenhum push direto, schedule ou edição de PR dispara o teste. O caminho `/tmp` não é inerentemente persistente: o teste falha se os dados não sobreviverem. Ver [FLUX-008](docs/fluxos/FLUX-008-teste-integral-iso-persistencia.md) e [GUIA-007](docs/guias/GUIA-007-teste-integral-3-iso-persistencia.md).

## Teste Integral 2 — enviar ZIP, executar comandos e encerrar VM

O runner [integral-2-zip.sh](scripts/tests/integral-2-zip.sh) usa um QCOW2 Debian 12 Cloud verificado, inicializa uma VM por Cloud-Init/SSH, envia um ZIP via **API Go**, conta 12 itens no ZIP dentro da VM, descompacta e confirma 12 arquivos extraídos e hashes SHA-256. Repete o teste via **CLI** com outro ZIP, valida respostas de comandos e só aprova o encerramento gracioso (sem destroy). Não modifica o motor: ImportToVM utiliza TAR.GZ como transporte, preservando o ZIP para os comandos posteriores.

~~~bash
bash scripts/tests/integral-2-zip.sh
~~~

O [workflow Integral 2](.github/workflows/integral-2-zip.yml) é acionado na abertura e em novos commits de PRs para **develop**, **promolog** e **main**. Para bloquear merges, exigir o status **integral-2-zip-upload-command** nas regras dessas branches. A execução real deve ser verificada no GitHub Actions. Veja [GUIA-006](docs/guias/GUIA-006-teste-integral-2-zip.md).

## Teste integral obrigatório: ISO Alpine Linux

```bash
bash scripts/tests/alpine-iso.sh --config /caminho/absoluto/config.yaml
# ou com ISO local:
bash scripts/tests/alpine-iso.sh --config /caminho/absoluto/config.yaml --iso /isos/alpine-virt-3.24.4-x86_64.iso
```

Baixa e verifica a ISO oficial Alpine Virtual (~67 MB), inicia **1 CPU/500 MiB** pelo Libvirt/KVM, executa e valida comando no console serial do guest e exige desligamento completo. Esse teste é obrigatório em `scripts/tests/integration.sh` e não depende de SSH/Cloud-Init. Requer um host com `/dev/kvm`; a execução real ainda precisa ser comprovada. Veja [GUIA-005](docs/guias/GUIA-005-alpine-iso.md).

**GitHub Actions / Teste Integral 1:** O workflow
[`integral-1-alpine-iso.yml`](.github/workflows/integral-1-alpine-iso.yml)
roda em abertura, reabertura e novo commit de PRs para `develop`,
`promolog` e `main`. Status do job: `integral-1-alpine-iso`.
Para impedir merge em caso de falha, exigir esse check em
**Settings > Rules > Rulesets/Branches** e incluir o workflow nas
branches de destino. `promolog` ainda não existe; a validação real
do runner GitHub-hosted com KVM está pendente.

## ISO Live e testes de integração

Para preparar uma ISO Live, instalar `live-boot` e reconstruir o initramfs **dentro da VM antes de `freeze`**. Depois executar `insula profile export-iso -name PERFIL` e `insula vm start-iso -profile PERFIL`. ISO Debian netinst comum **não** é suportada por `start-iso`; consulte [INTF-TERM-006](docs/interfaces/terminal/INTF-TERM-006-iso-live.md).

O teste unitário está em `bash scripts/tests/unit.sh`. O integral real (VMs, scripts, loop infinito, timeout, clone, freeze e ISO Live) está em [GUIA-003](docs/guias/GUIA-003-testes.md). **Não executado no hipervisor aqui.**

## Testes

```bash
go test -race ./...
go vet ./...
```

Testes automatizados não substituem validação de ciclo completo em um hipervisor KVM/Libvirt real.

## Diagnóstico de ISO Live que não obtém IP

`vm start-iso` aceita o timeout configurável `-ssh-ready-timeout` (padrão 5m).
Quando a ISO não conclui DHCP/SSH, preserve a VM para inspecionar o console
sem recorrer diretamente ao `virsh`:

```bash
./insula vm start-iso -config ./config.yaml \
  -profile deb13-abioma-tests-final -iso ./deb13-abioma-tests-final.iso \
  -memory-mib 2048 -vcpus 2 \
  -ssh-ready-timeout 8m -preserve-on-failure

# A falha imprime um UUID de diagnóstico; use-o nos comandos seguintes:
./insula vm status -config ./config.yaml -id UUID_DA_VM
./insula vm console -config ./config.yaml -id UUID_DA_VM
```

O `start-iso` não garante automaticamente que a ISO esteja inicializável;
a saída serial preservada é necessária para descobrir problemas de boot.
`vm stop` remove a VM descartável após o diagnóstico.

## Boot por ISO Live: evidências e recuperação

Uma falha `start-iso` na etapa DHCP **não comprova** que a ISO inicializou.
Para cada tentativa, os eventos e snapshots são preservados no Linux em
`storage.logs/live-iso/<UUID>/`. O padrão da CLI `start-iso` agora é
`-preserve-on-failure=true`; um domínio já iniciado fica disponível para
investigação após erros de DHCP/SSH. O console serial opcional usa `-serial-log`:

```bash
./insula vm start-iso -config ./config.yaml \
  -profile deb13-abioma-tests-final \
  -iso ./deb13-abioma-tests-final.iso \
  -memory-mib 1024 -vcpus 1 -serial-log
# Se falhar, o UUID da instância será impresso:
./insula vm diagnose -config ./config.yaml -id UUID_DA_VM
./insula vm console -config ./config.yaml -id UUID_DA_VM
```

O comando `vm diagnose` coleta estado do domínio, interface e concessão DHCP
para o MAC exato, exibindo caminhos dos logs e seus últimos 16 KiB.
`-serial-log` depende de permissões de gravação suportadas pelo host.
`vm stop` limpa a VM descartável: execute apenas após o diagnóstico.

**Para recriar o Debian instalado mesmo se a ISO Live falhar**, use o
QCOW2 registrado pelo `freeze`:

```bash
./insula vm start -config ./config.yaml \
  -profile deb13-abioma-tests-final -memory-mib 1024 -vcpus 1
```

Esta inicialização a partir do perfil QCOW2 é distinta do boot da ISO Live.
Consulte a [interface e diagnóstico de ISO](docs/interfaces/terminal/INTF-TERM-006-iso-live.md).
