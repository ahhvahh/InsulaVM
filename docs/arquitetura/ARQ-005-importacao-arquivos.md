<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-005 — Arquitetura de importação TAR.GZ para o workspace da VM

**Estado:** importação implementada na CLI, biblioteca Go e manifesto; verificada com servidor SSH simulado. Execução real em KVM/Libvirt ainda não validada.
**Contratos:** [CONTR-001 — Operações e importação atual](../contratos/CONTR-001-operacoes-canonicas.md) e [CONTR-002 — STDIO futuro](../contratos/CONTR-002-transporte-stdio.md).
**Código:** [archive.go](../../pkg/transfer/archive.go), [remote.go](../../pkg/transfer/remote.go), [import.go](../../insulavm/import.go), [import_vm.go](../../insulavm/import_vm.go) e [import_cli.go](../../import_cli.go).

## Componentes e responsabilidades

| Componente | Responsabilidade implementada |
| --- | --- |
| `pkg/config` | Fornecer o `guest.destination` único, configurado no host e usado dentro do convidado |
| `pkg/transfer/archive.go` | Preparar TAR.GZ temporário dos arquivos e diretórios locais permitidos |
| `pkg/transfer/remote.go` | Enviar pacote via sessão SSH, verificar o tamanho recebido e extrair no destino |
| `insulavm/import_vm.go` | Resolver estado, IP e credenciais individuais da VM cadastrada pelo UUIDv7 |
| `insulavm/import.go` | Compartilhar a transferência com hosts SSH externos configurados, publicar progresso e resultado |

O destino é uma pasta **dentro da VM**, não no hospedeiro, e é criado com as permissões do usuário SSH. VMs gerenciadas usam credenciais individuais do registro SQLite; hosts externos usam a identidade SSH explicitamente configurada. A configuração e os campos válidos do destino estão em [ARQ-003 — Configuração](ARQ-003-configuracao-perfis.md) e no [CONTR-001](../contratos/CONTR-001-operacoes-canonicas.md).

## Modelo de transferência existente

A arquitetura usa compactação TAR.GZ temporária no host e transmissão SSH para uma área temporária no convidado. Após conferir o **tamanho** recebido, extrai o conteúdo no `guest.destination`. Importações da CLI e do manifesto utilizam a mesma implementação. O material transferido pode coexistir com arquivos já presentes na pasta de trabalho; nomes conflitantes podem ser sobrescritos.

**Importante:** `ImportToVM` e `ImportPath` atualmente extraem o conteúdo. O futuro `UploadArchive`, que deverá **salvar o pacote** com extração opcional, é uma operação ainda **proposta** no CONTR-001. A biblioteca e a CLI atuais não devem ser descritas como se já oferecessem esse comportamento.

## Limites de isolamento e recuperação

- Não há verificação remota adicional por SHA-256; a validação atual compara o tamanho transferido.
- Não há extração atômica, retomada de upload nem proteção completa contra links simbólicos já existentes no destino; parte do conteúdo pode permanecer após falha.
- O processo remoto exige um workspace acessível ao usuário SSH. Não há elevação de privilégio implícita na importação.
- A origem precisa ser confiável: TAR.GZ pode incluir caminhos e conteúdo que representam risco quando descompactados em um diretório com arquivos preexistentes.

## Documentação complementar

- **Parâmetros e retornos da operação:** [CONTR-001](../contratos/CONTR-001-operacoes-canonicas.md)
- **Comandos CLI atuais de importação por UUID:** [INTF-TERM-004](../interfaces/terminal/INTF-TERM-004-gerenciar-vms.md)
- **Execução de manifesto e exemplos de `imports`:** [INTF-TERM-002](../interfaces/terminal/INTF-TERM-002-executar-manifesto.md) e [FLUX-001](../fluxos/FLUX-001-execucao-manifesto.md)
- **API Go pública:** [ARQ-004](ARQ-004-biblioteca-go.md)
- **Deploy com upload e script:** [FLUX-005](../fluxos/FLUX-005-deploy-arquivos.md) e [INTF-TERM-005](../interfaces/terminal/INTF-TERM-005-deploy-vm.md)
- **Identidade SSH e permissões:** [ARQ-006](ARQ-006-identidade-registro-sudo.md)
- [Go archive/tar](https://pkg.go.dev/archive/tar)
- [Go compress/gzip](https://pkg.go.dev/compress/gzip)
- [Go SSH Session](https://pkg.go.dev/golang.org/x/crypto/ssh#Session)
