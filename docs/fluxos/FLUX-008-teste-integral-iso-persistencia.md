<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# FLUX-008 — Teste Integral 3: preservar /tmp/workspace na ISO Live

**Estado:** implementação em branch isolada; KVM real depende do workflow da PR.
**Motor Go:** [CreateProfileFromISO](../../insulavm/create_profile_iso.go), [StartVMWithOptions](../../insulavm/start_options.go), [ExecVM](../../insulavm/exec.go), [ExportVMToLiveISO](../../insulavm/export_vm_iso.go), [StartVMFromISO](../../insulavm/live_iso.go).
**Execução:** [integral3_iso_persistence_test.go](../../tests/integration/integral3_iso_persistence_test.go) e [integral-3-iso-persistence.sh](../../scripts/tests/integral-3-iso-persistence.sh).
**Guia:** [GUIA-007](../guias/GUIA-007-teste-integral-3-iso-persistencia.md).

## Objetivo

Provar que a imagem inicializável gerada pelo motor preserva realmente uma pasta e um arquivo criados por comando remoto dentro de /tmp/workspace da VM original. Não basta verificar a criação da ISO; é necessário iniciar uma segunda VM pela ISO e verificar o conteúdo e hash, sem copiar novamente o arquivo a partir do host.

## Recursos

- **ISO original leve:** mini.iso oficial Debian 13 amd64 netboot, cerca de 64 MB, origem HTTPS e SHA-256 verificado com SHA256SUMS do Debian.
- **Instalador:** usa 1024 MiB e 1 vCPU **somente durante a instalação**, porque CreateProfileFromISO na implementação atual recusa RAM inferior a 1024 MiB; não simular 500 MiB no instalador.
- **Disco instalado:** QCOW2 de 10 GiB virtuais comprovados com qemu-img info.
- **Primeira VM gerenciada:** 1 vCPU, 500 MiB; sobreposição QCOW2, Cloud-Init e SSH.
- **Segunda VM da ISO exportada:** 1 vCPU e 500 MiB; scratch temporário controlado pelo StartVMFromISO. O scratch padrão da ISO Live é independente do disco instalado de 10 GiB; não alegar que a segunda VM possui disco persistente de 10 GiB.

## Sequência

1. Baixar e validar a mini.iso, usar CreateProfileFromISO para instalação desassistida e publicar perfil QCOW2 independente de 10 GiB.
2. Iniciar uma VM gerenciada pelo perfil, sobrescrevendo RAM para 500 MiB e CPU para 1; comprovar no dumpxml do Libvirt.
3. Enviar comando ExecVM para criar /tmp/workspace/created e escrever um token aleatório em /tmp/workspace/created/state.txt. Encerrar a sessão SSH e, por outra chamada ExecVM, confirmar SHA-256 do arquivo e montagem do caminho.
4. Preparar live-boot e executar ExportVMToLiveISO, com desligamento gracioso, limpeza de identidade e criação de QCOW2 autônomo e ISO Live. Não aceitar desligamento forçado.
5. Verificar ausência do primeiro domínio Libvirt, remoção da pasta temporária e que o QCOW2 congelado conserva 10 GiB de tamanho virtual.
6. Chamar StartVMFromISO com a ISO recém-gerada, não com QCOW2, exigindo boot real e autenticação SSH com novo UUID.
7. Antes de qualquer upload ou alteração na segunda VM, testar existência da pasta e do arquivo e SHA-256 exato do conteúdo original. Repetir verificação com a CLI vm exec para garantir paridade da interface.
8. Desligar a segunda VM por StopVMWithOptions com ForceAfterTimeout=false; exigir Graceful=true, Forced=false, domínio ausente e remoção de scratch e chaves temporárias.

O marcador INSULA_INTEGRAL_3_OK só é emitido após todos esses critérios. A ausência da pasta, do conteúdo, do DHCP, de SSH, do boot da ISO ou da limpeza faz o teste falhar com código não zero.

## Semântica de /tmp

O diretório /tmp **não oferece garantia genérica de persistência** entre boots; pode ser tmpfs ou ter conteúdo limpo pelo sistema durante inicialização. Por isso a persistência em /tmp/workspace é o comportamento exigido e verificado neste teste — não uma premissa. O teste não move os arquivos para /var, não reinsere dados após o freeze nem se satisfaz apenas com uma ISO gerada.

A ISO Live tem SquashFS de uma captura do sistema instalado, com overlay temporário no novo boot. Alterações feitas na **segunda VM** não são gravadas automaticamente na ISO. Consulte [manual Debian live-boot](https://manpages.debian.org/unstable/live-boot-doc/live-boot.7.en.html).

## Integração CI

O workflow [.github/workflows/integral-3-iso-workspace.yml](../../.github/workflows/integral-3-iso-workspace.yml) usa pull_request direcionado **somente para main**. Eventos: opened, synchronize, reopened, ready_for_review, edited. Job/check estável: integral-3-iso-workspace-persistence. Sem filtros paths, sem continue-on-error e sem fallback sem KVM.

Rodar essa instalação em cada PR pode consumir tempo e banda por instalar Debian pela rede, instalar live-boot e gerar SquashFS. O workflow usa runner efêmero GitHub-hosted e instala as dependências explícitas, sem acessar VMs do host do usuário.

A existência de um workflow **não torna o check bloqueante**: exigir integral-3-iso-workspace-persistence nas regras de proteção de main, depois de incorporar o workflow à branch de destino. Não alterar main diretamente.

## Erros, segurança e evidências

- Falhas de installer, libguestfs, GRUB, Live ISO, DHCP/SSH, integridade do arquivo e shutdown não são ignoradas.
- Diretórios e nomes de teste são aleatórios; a limpeza só considera UUIDs das VMs criadas pela própria execução.
- Executar testes com imagem e scripts confiáveis; o instalador usa rede e requer acesso aos espelhos Debian.
- Preservar logs de Go/ExecVM e snapshots do host como artefatos do job, não os discos inteiros nem chaves privadas.
- Não atribuir falha de DHCP à rede sem verificar a etapa comprovada no console serial.
- O perfil congelado independente é o artefato intermediário de recuperação; não substitui comprovação de boot da ISO.

## Contrato CLI e API

Os métodos Go usam o motor InsulaVM. O teste invoca vm exec como verificação independente da CLI; Freezing e Live ISO também possuem comandos CLI existentes (vm export-iso, vm start-iso). Nenhuma lógica de negócio é duplicada em uma implementação específica de testes.
