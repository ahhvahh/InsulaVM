<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# FLUX-007 — Teste Integral 2: ZIP, respostas SSH e encerramento

**Estado:** implementação em branch de teste. Validação KVM real depende de execução da PR.
**Biblioteca Go:** [Engine.ImportToVM e ExecVM](../../insulavm/import_vm.go).
**Interface CLI:** [vm import e vm exec](../../vm_cli.go).
**Guia:** [GUIA-006](../guias/GUIA-006-teste-integral-2-zip.md).
**Runner:** [integral-2-zip.sh](../../scripts/tests/integral-2-zip.sh).

## Objetivo

Provar que o InsulaVM gerencia uma VM real, transfere um ZIP local pelo transporte público autenticado da aplicação, executa comandos no convidado para verificar, descompactar e contar os arquivos, recebe as respostas reais do processo e encerra a VM sem destruição forçada. O teste cobre a API Go e a CLI, que compartilham o motor.

## Dependências e entradas

- Host Linux amd64 com KVM/Libvirt, rede default ativa e DHCP, Go 1.23+, qemu-img, genisoimage, virsh e ACL de acesso ao UID QEMU.
- QCOW2 Debian 12 Cloud com Cloud-Init, SSH e Python 3: obtido do espelho oficial e validado por SHA-512 pelo runner, ou fornecido pelo operador.
- Configuração e SQLite isolados, diretório de workspace configurado em guest.destination.
- Sem credenciais pré-compartilhadas: o InsulaVM gera SSH/host keys por VM no NoCloud seed.

## Sequência

~~~mermaid
sequenceDiagram
    autonumber
    participant T as Teste Integral 2
    participant E as Engine / CLI InsulaVM
    participant H as Libvirt/host
    participant G as Debian guest
    T->>E: RegisterProfile(QCOW2 oficial)
    E->>H: Cópia do QCOW2 base
    T->>E: StartVMWithOptions(1 CPU/1024 MiB)
    E->>H: Overlay + NoCloud + domínio Libvirt
    G-->>E: DHCP e autenticação SSH
    T->>E: ImportToVM(arquivo ZIP gerado)
    E->>G: TAR.GZ de transporte, confere bytes e extrai ZIP original
    G-->>T: ImportResult + progresso
    T->>E: ExecVM(SHA/contagem/extração/contagem)
    G-->>T: Marcadores e retornos calculados na VM
    T->>E: CLI vm import (segundo ZIP)
    E->>G: Mesmo motor de importação
    T->>E: CLI vm exec (mesmos checks)
    G-->>T: Marcadores e retornos calculados na VM
    T->>E: StopVMWithOptions(force=false)
    E->>H: ACPI shutdown + limpeza do overlay
    H-->>T: Domínio ausente
~~~

## Critérios de aprovação

| Etapa | Evidência obrigatória |
| --- | --- |
| Boot | VM ativa e IP DHCP resolvido; chave SSH do guest validada |
| Importação Go | Imported=true, Files=1 (ZIP em arquivo único), tamanho compatível, destino correto e eventos de progresso |
| Conteúdo do ZIP | Exatamente 12 arquivos contados dentro do ZIP pela VM |
| Integridade | SHA-256 do ZIP remoto igual ao ZIP do host |
| Extração | Comando remoto Python 3 zipfile executado; 12 arquivos descompactados no diretório da VM |
| Integridade binária | SHA-256 do arquivo com bytes NUL e bytes não ASCII equivalente após descompactação |
| Erros | Comando inválido remoto produz erro ao consumidor Go |
| CLI | Segundo ZIP enviado por vm import, com verificação dos mesmos 5 checkpoints via vm exec |
| Shutdown | Graceful=true, Forced=false e TimedOut=false; domínio Libvirt absent; pasta temporária da instância removida |
| Preservação | Imagem QCOW2 de origem não é iniciada diretamente; InsulaVM opera com perfil copiado e overlay |

O teste só passa com o marcador INSULA_INTEGRAL_2_OK gravado após todas as verificações. Erro do ambiente, download, Libvirt, transferência, contagem ou shutdown produz exit code não zero. Em falhas de teste, cleanup pode recorrer a StopVMWithOptions com ForceAfterTimeout=true, mas isso não conta como sucesso.

## Observações de contrato

**Não existe upload ZIP bruto como operação específica.** ImportToVM aceita um arquivo regular (inclusive .zip), comprime o próprio arquivo em TAR.GZ *para transporte*, transmite por SSH e extrai o pacote TAR.GZ automaticamente, deixando o .zip original no workspace do guest. A descompactação interna do ZIP é feita por um comando **posterior** com ExecVM. Isso usa contratos atuais, sem alegar suporte ao UploadArchive canônico ainda pendente.

Os comandos usam Python 3 da imagem Debian Cloud para manipular ZIP, evitando dependência da ferramenta externa unzip. A contagem e o hash vêm da VM; arquivos simplesmente presentes no host não são considerados evidência.

O workflow independente usa pull_request (opened, synchronize, reopened, ready_for_review, edited) nas bases develop, promolog e main, runner GitHub-hosted Ubuntu 24.04, status integral-2-zip-upload-command e upload de logs. Bloqueio de merge depende da configuração de required status checks; não é implicado apenas pela existência do workflow. Nenhuma imagem-base é alterada pelo teste.

## Limitações

- Não valida envio de ZIP malicioso de usuário final, não garante defesa contra ZIP slip; somente ZIP gerado pelo teste é descompactado.
- A VM usa uma imagem Debian já instalada em QCOW2; não testa instalação do SO a partir de ISO (coberta por outros fluxos).
- Em CI o download da imagem Cloud pode consumir centenas de MB; a referência é verificada com SHA-512 do repositório oficial.
- Sem execução KVM real, apenas testes auxiliares podem ser declarados aprovados.

## Compatibilidade da imagem de teste

A configuração de teste utiliza Debian 12 Cloud (Bookworm) para validar transporte SSH e não o boot de imagens Debian 13. Uma tentativa real com Debian 13 Cloud falhou antes do kernel: o console repetiu "Booting Debian GNU/Linux", sem lease DHCP. O comportamento coincide com [bug Debian #1111240](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1111240), que relata interação entre GRUB terminal_output gfxterm serial e console QEMU/Libvirt. Não confundir essa falha de boot com erro de envio de arquivos ou de rede DHCP. O motor original não foi alterado neste teste; um teste próprio deve validar correções de boot do Debian 13.
