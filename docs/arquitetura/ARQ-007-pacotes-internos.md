<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-007 — Divisão entre API pública e pacotes internos

**Estado:** arquitetura implementada e incremental; contrato público preservado.
**Escopo:** organização do código Go, sem alteração de fluxo de VM ou regras de negócio.
**Referências:** [ARQ-004 — Biblioteca Go](ARQ-004-biblioteca-go.md), [CONTR-001 — Operações canônicas](../contratos/CONTR-001-operacoes-canonicas.md).

## Objetivo

Preservar a API `github.com/ahhvahh/InsulaVM/insulavm` e o binário `insula`, limitando operações privadas de baixo nível a pacotes `internal/` sem quebrar os caminhos de importação preexistentes. Em Go, cada diretório é um pacote: mover apenas arquivos sem separar dependências ou alterar os imports não funciona.

## Pacotes e contratos

| Pacote | Responsabilidade | Dependências / Entradas | Saídas / erros |
| --- | --- | --- | --- |
| `cmd/insula/` | Comandos e testes CLI | Argumentos, configuração, API `insulavm` | Stdout/stderr, códigos de saída |
| `insulavm/` | Motor público e orquestração | `Engine`, `context.Context`, requests e callbacks | Resultados públicos e erros, sem duplicar lógica |
| `internal/guestfs/` | Montagem read-only e desmontagem segura; recuperação privada de supermin | Caminhos da imagem/montagem, contexto; ferramentas GuestFS/APT | Função cleanup, `error`, `ErrUnmount` |
| `internal/installer/` | Download e SHA256 do Debian, validação de ISO, Preseed, domínio instalador e verificação de QCOW2 | Contexto, caminhos ISO/QCOW2, Libvirt e ferramentas host | SHA256, texto Preseed, erro de estado, `guestfs.ErrUnmount` |
| `internal/shell/` | Preparação de workspace SSH, transporte de sessão de longa duração, delimitação binária e comandos serializados | `ssh.Client`, comando, contexto, workspace | Saída, exit code e erros de transporte; `Session` |
| `internal/liveiso/` | Primitivos do host para ISO Live, kernel/initramfs, GRUB e cópias seguras | Diretórios e comandos do host, contexto | Caminhos, bytes/erro sem sobrescrever destino |
| `internal/vmstop/` | Políticas de shutdown individual e em lote | Status registrado, operações do motor e contextos | Resultado parcial, erro agregado; recusa VMs sem identidade comprovada |
| `pkg/` | Pacotes de infraestrutura já existentes | Libvirt, SQLite, SSH, configuração, transferência | Tipos e erros existentes, contratos preservados |

## Regras e segurança

- A CLI consome o mesmo `Engine` da biblioteca Go; operações públicas não foram alteradas.
- Nenhum projeto externo deve importar `internal/*`; Go bloqueia importações externas desses pacotes.
- `internal/guestfs` não modifica imagens QCOW2 e somente limpa recursos privados após desmontagem bem-sucedida; o erro `ErrUnmount` permite preservar evidências.
- `internal/shell` mantém sessão SSH e serialização por token, limita comandos/saída e fecha conexões após falhas.
- `internal/liveiso` não sobrescreve saídas existentes; a publicação atômica da ISO continua sob responsabilidade de `Engine.ExportProfileLiveISOTo`.
- Os helpers de `pkg/` continuam com os caminhos originais; sua eventual migração exige auditoria separada de consumidores.

## Fluxos afetados

1. Exportação de ISO: `Engine.ExportProfileLiveISOTo` → `internal/guestfs` (montagem) → `internal/liveiso` (preparo) → desmontagem → publicação.
2. Instalação de Debian: `Engine.CreateProfileFromISO` → `internal/installer` (Preseed, Libvirt e verificação) → `internal/guestfs` (montagem somente leitura) → registro de perfil QCOW2.
3. Desligamento seguro: `Engine.StopVMWithOptions`/`Engine.StopAllVMs` → `internal/vmstop` → adaptador do motor Libvirt/SQLite, preservando validação de ownership.
4. Sessão Shell: `Engine.ExecuteShellCommand` → conexão e registro SQLite → `internal/shell` (sessão, comando e resposta) → persistência.
5. Erro de desmontagem: retornar `guestfs.ErrUnmount`, preservar a área de staging e permitir diagnóstico posterior.

## Testes e validação

```bash
go build -o insula ./cmd/insula
go test ./...
go test -race ./...
go vet ./...
go test -tags integration -run '^$' ./internal/guestfs
```

O teste funcional `TestFunctionalGuestfsPrivateKernelMount` pertence a `internal/guestfs` e requer ferramentas e recursos reais; sua execução continua condicionada ao pipeline integral existente. Testes com stubs não comprovam boot de VM real.

## Referência técnica

[Go — Organizing a Go module](https://go.dev/doc/modules/layout). A seção sobre pacotes de suporte recomenda `internal/` quando o contrato não deve ser importável fora do projeto.
