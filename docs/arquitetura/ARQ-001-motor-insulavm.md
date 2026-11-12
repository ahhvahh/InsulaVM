<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-001 — Arquitetura do motor InsulaVM

**Estado:** motor de execução, perfis QCOW2, Libvirt, Cloud-Init, SQLite, importação, deploy, congelamento de perfil e gerenciamento de VMs implementados. **Propostos:** instalação interativa do SO a partir de ISO instaladora e checkout Git automático.
**Escopo:** motor local em Go, hospedeiro Linux e VM convidada Debian.
**Fluxos:** [FLUX-001 — Executar Action](../fluxos/FLUX-001-execucao-manifesto.md) e [FLUX-002 — Criar perfil](../fluxos/FLUX-002-criacao-perfil-vm.md).
**Persistência das VMs:** [ARQ-002 — Perfis e overlays](ARQ-002-perfis-vm.md).

## Status verificável dos componentes

- **Implementado:** [executor SSH](../../pkg/executor/ssh.go), [armazenamento local de logs](../../pkg/storage/logger.go), [manifesto YAML](../../pkg/manifest/manifest.go) e [CLI](../../cmd/insula/main.go), seguindo o [FLUX-003](../fluxos/FLUX-003-execucao-sequencial-logs.md).
- **Implementado:** perfis QCOW2, gerenciamento temporário de VM por Libvirt, Cloud-Init NoCloud com identidade SSH por instância, importação TAR.GZ, pasta `guest.destination` configurável, jobs sequenciais e limpeza de overlays. **Proposto:** instalação do Linux pela ISO instaladora e checkout Git automático.

## Objetivo e limites

O InsulaVM é uma ferramenta CLI open-source que permite preparar perfis persistentes de VM Debian e, depois, lê `insula.yaml` para executar `setup` e pipelines em instâncias descartáveis derivadas desses perfis. O hospedeiro controla virtualização, transferência do projeto, comunicação SSH, logs e encerramento; o convidado não recebe agente próprio do InsulaVM.

A CLI e a biblioteca Go compartilham o motor atual; alguns recursos futuros permanecem planejados. Consulte [ARQ-004 — Biblioteca Go](ARQ-004-biblioteca-go.md) e [FLUX-004 — Congelamento](../fluxos/FLUX-004-congelar-vm.md) para as operações de ciclo de vida já disponíveis.

## Contexto e componentes

```mermaid
flowchart LR
    U[Operador / automação] --> CLI[InsulaVM CLI - Go]
    Y[insula.yaml] --> CLI
    CLI --> VM[Gerenciador de VM]
    VM --> LV[Libvirt / QEMU / KVM]
    LV --> G[Debian convidado]
    CLI --> EX[Executor SSH]
    EX --> G
    EX --> LOG[Logs no terminal hospedeiro]
    SRC[Repositório local ou remoto] --> CLI
    CLI --> G
```

| Componente | Responsabilidade | Tecnologia |
| --- | --- | --- |
| CLI/orquestrador | Criar perfis interativamente, validar manifesto, coordenar etapas, cancelamento e resultado | Go |
| Gerenciador de perfis | Salvar imagem-base QCOW2 imutável, metadados e overlays por Action | Libvirt/QEMU; [ARQ-002](ARQ-002-perfis-vm.md) |
| Gerenciador de VM | Criar disco temporário, gerar XML, ISO Cloud-Init, iniciar/parar/remover domínio | `pkg/vm/runtime.go`; KVM, QEMU e Libvirt |
| Preparação da instância | Configurar identidade, usuário, rede e chave pública SSH quando o sistema instalado tiver Cloud-Init operacional | Cloud-Init / NoCloud |
| Executor remoto | Conectar, executar comandos sequenciais no contexto correto e capturar saída | `pkg/executor/ssh.go`; `golang.org/x/crypto/ssh` |
| Preparador de repositório | Obter referência remota ou empacotar projeto local; disponibilizar checkout | Git + transporte autenticado |
| Saída de execução | Exibir stdout/stderr por job, falhas e resultado final | Terminal da máquina hospedeira |

## Virtualização e ciclo de vida

- **Hospedeiro:** Linux com KVM disponível, QEMU, Libvirt e permissões restritas para criar domínios e discos.
- **Imagem base:** um perfil Debian já instalado é salvo como QCOW2 imutável; cada Action usa overlay QCOW2 descartável e preserva a imagem compartilhada. O registro atual exige QCOW2 com Linux instalado; instalação por ISO local/console é **proposta**, conforme [ARQ-002](ARQ-002-perfis-vm.md).
- **Libvirt implementado:** gerar XML do domínio transitório com CPU, memória, overlay QCOW2, rede Libvirt e ISO NoCloud, iniciar com `virsh create`, consultar estado e encerrar/remover domínio e temporários. **Proposto:** boot por ISO de instalação para instalar um novo sistema.
- **Cloud-Init:** para convidados que tenham Cloud-Init habilitado, preparar ISO NoCloud com rótulo `CIDATA`, `user-data`, `meta-data` e eventual `network-config`, criando identidade/credenciais próprias por instância. **A ISO convencional do instalador Debian não é uma imagem Cloud-Init e precisa de instalação do SO**, manual ou automatizada por Preseed; isso não pode ser substituído por anexar somente a ISO NoCloud.
- **Conectividade:** obter IP interno associado à instância (ex.: leases DHCP do Libvirt) e aguardar inicialização do SSH com prazo e tentativas limitados. IP atribuído não garante prontidão de Cloud-Init; confirmar inicialização antes dos jobs.
- **Remoção:** após Action, fechar SSH, encerrar domínio transitório e remover ISO de configuração, overlay e credenciais temporárias — **preservando o perfil base**. **Implementado separadamente:** `vm freeze` publica um perfil QCOW2 nomeado e só então limpa a VM original. Pergunta interativa para salvar/descartar staging de instalação ISO é proposta.

**Terminologia:** criar, iniciar, desligar e destruir são fases distintas. Destruir no Libvirt encerra a execução do domínio; a remoção de definição e arquivos de disco é uma ação adicional, que precisa ser tratada separadamente.

## Dependências Go planejadas

| Finalidade | Import recomendado | Observação |
| --- | --- | --- |
| API do Libvirt | `libvirt.org/go/libvirt` | Binding oficial atual com Go modules; substitui `libvirt.org/libvirt-go` legado. `github.com/libvirt/libvirt-go` é espelho arquivado. Usa CGO e biblioteca nativa Libvirt. |
| SSH | `golang.org/x/crypto/ssh` | Sessões SSH e pipes separados para stdout/stderr. |

**Nota:** `go.mod` contém as dependências SSH e YAML do executor implementado. O binding Libvirt permanece previsto, ainda não adicionado.

## Comunicação, execução e telemetria

1. Gerar par de chaves SSH **no hospedeiro**, proteger a chave privada e injetar somente a pública via Cloud-Init; não transmitir senhas em logs.
2. **Implementado:** gerar a chave SSH Ed25519 do servidor por instância, incluir a chave privada no seed protegido e gravar a pública confiável em `known_hosts` no hospedeiro antes da conexão. A validação não utiliza `InsecureIgnoreHostKey`; ver [ARQ-006](ARQ-006-identidade-registro-sudo.md).
3. Criar sessão SSH para cada comando/job; montar o contexto do diretório da pipeline na raiz do checkout, por exemplo `cd <guest.destination> && <comando>`. Escapar o caminho de forma segura e não concatenar valores arbitrários sem validação.
4. Configurar `StdoutPipe()` e `StderrPipe()` antes de `Start()`. Consumir os dois fluxos concorrentemente, linha a linha, com buffers dimensionados para linhas longas. Depois aguardar `Wait()` e registrar status de saída.
5. Mostrar mensagens com prefixo identificador do job, como `[run-tests] Instalando pacotes...`. Preservar a ordem **dentro de cada fluxo**; a interleaving de stdout e stderr não garante ordem global. Incluir origem do fluxo no registro interno quando pertinente.
6. Não usar `Output()` nem `CombinedOutput()` como mecanismo de streaming. `Wait()` é necessário para obter o resultado, sem impedir que os leitores publiquem linhas enquanto o comando executa.
7. Usar retorno do processo para decidir sucesso/falha; conteúdo em stderr, isoladamente, não determina falha. Definir timeout, cancelamento, mascaramento de segredos e política de tratamento de linhas muito extensas.

## Repositório e diretório de trabalho

- **Remoto:** obter URL e referência solicitadas (branch/tag/commit) e materializar uma revisão identificável. Para repositórios privados, evitar gravar credenciais no disco ou na ISO da VM.
- **Local:** compactar o conteúdo de trabalho autorizado no hospedeiro e transferir por canal autenticado (por exemplo, archive TAR via SSH); não pressupor que todo diretório local seja um repositório Git válido.
- **Raiz de execução:** `guest.destination` do `profile.yaml` selecionado define o destino dos imports e workspace padrão dos comandos, como `/tmp/insulavm/workspace`. `setup` roda como usuário SSH não-root, sem sudo automático; `workspace` do manifesto pode sobrescrever o diretório de trabalho.
- **Ordem:** materialização do projeto antes de `setup`; execução de `setup` antes dos jobs, conforme [FLUX-001](../fluxos/FLUX-001-execucao-manifesto.md).

## Segurança e requisitos não funcionais

- Aplicar limites de vCPU, memória, armazenamento, tempo de boot, tempo por etapa e duração total; números ainda não decididos.
- Isolar rede, volumes e permissões da VM. KVM oferece virtualização assistida por hardware, mas a expressão “performance bare-metal” não é garantia de desempenho igual ao do hospedeiro.
- Tratar manifesto e scripts como código potencialmente não confiável. A VM reduz a superfície de risco, mas não a elimina.
- Não habilitar SSH por senha ou login remoto como root por padrão. Usar permissões mínimas e escalonamento controlado quando `setup` exigir privilégios.
- Redigir/filtrar tokens, chaves e credenciais antes de exibir ou persistir logs; remover recursos efêmeros mesmo após erros.

## Pontos para decisão antes da implementação

- Schema versionado do `insula.yaml`: referência/versão do perfil, origem do projeto, branch/commit, recursos, `setup`, pipelines, jobs, política de falhas e timeout.
- Configurações globais do diretório de perfis, instalação de ISO, disco padrão e política de versões, conforme [ARQ-002](ARQ-002-perfis-vm.md).
- Validação de ponta a ponta em hipervisor real da identidade SSH por instância, DHCP e bootstrap Cloud-Init; a estratégia de pinagem de host key e o handshake SSH estão implementados.
- Acesso a repositórios privados, inclusão de arquivos locais ignorados, submódulos e tratamento de segredos.
- Política de retenção/recuperação de VMs após falhas e autenticação de usuários que solicitam operações administrativas. `setup` já usa usuário SSH não-root e workspace do manifesto ou o destino global.
- Destino persistente de logs/artifacts, ordem entre fluxos concorrentes e política de retomada de jobs.

## Referências externas

- Libvirt Go (módulos): https://libvirt.org/go/libvirt.html
- Libvirt Domain XML: https://libvirt.org/formatdomain.html
- Cloud-Init NoCloud: https://docs.cloud-init.io/en/26.1/reference/datasources/nocloud.html
- Go SSH: https://pkg.go.dev/golang.org/x/crypto/ssh
