<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-005 — Importação TAR.GZ do hospedeiro para VM via SSH

**Estado:** implementado na biblioteca, no comando CLI `import` e na etapa `imports` de um manifesto. Testes com servidor SSH simulado; não há teste de ponta a ponta em VM real.

**Código:** [pkg/transfer/archive.go](../../pkg/transfer/archive.go), [pkg/transfer/remote.go](../../pkg/transfer/remote.go), [insulavm/import.go](../../insulavm/import.go) e [import_cli.go](../../import_cli.go).

## Objetivo e comportamento

Receber um caminho local que aponte para **um arquivo ou diretório**, compactar em **TAR.GZ** no hospedeiro, enviar o arquivo pelo SSH já autenticado, verificar o tamanho recebido, descompactar no diretório de destino da VM e confirmar o resultado **somente após o `tar` remoto terminar com código zero**.

- **Origem arquivo:** `/home/user/build.tar` gera o item `build.tar` dentro do diretório remoto.
- **Origem diretório:** `/home/user/projeto` gera o **conteúdo** de `projeto` diretamente no diretório remoto (sem acrescentar uma subpasta `projeto`).
- Subdiretórios vazios são preservados; links simbólicos, sockets, dispositivos e arquivos especiais são **recusados**, para não incluir conteúdo inesperado nem criar atalhos para locais externos.
- A origem não é apagada nem movida. A compactação é escrita em arquivo temporário local e removida ao final.
- O destino é um **diretório absoluto no Linux convidado**, que não pode ser `/`. O diretório é criado caso não exista, pelo usuário SSH.
- A transferência utiliza o próprio canal SSH, executando `cat` para gravar um arquivo temporário de acesso restrito no convidado. **Não exige SFTP nem abre uma porta adicional.** Ao finalizar, remove o temporário remoto.
- A verificação de integridade inclui tamanho do arquivo recebido e a validação GZIP/TAR do comando de extração. Ainda **não** há checksum SHA-256 ponta a ponta.
- Se houver falha de SSH, compressão, tamanho ou extração, a biblioteca retorna um erro e emite `failed`; não retorna `Imported: true`. O conteúdo de uma extração interrompida **pode permanecer parcialmente aplicado** ao destino; não há publicação atômica nesse ciclo.

## Etapas visíveis

```text
[InsulaVM][import:connecting] Conectando via SSH a VM ...
[InsulaVM][import:validating] Verificando arquivo ou diretorio de origem
[InsulaVM][import:compressing] Compactando conteudo em TAR.GZ no hospedeiro
[InsulaVM][import:compressed] Compactacao concluida: 5 arquivo(s), 20480 bytes compactados
[InsulaVM][import:uploading] Enviando pacote de 20480 bytes para a VM
[InsulaVM][import:uploading] Enviando: 20480/20480 bytes
[InsulaVM][import:verifying] Verificando integridade do tamanho recebido na VM
[InsulaVM][import:extracting] Descompactando TAR.GZ dentro da VM em /tmp/projeto
[InsulaVM][import:completed] Arquivo importado com sucesso na VM: /tmp/projeto (5 arquivo(s))
```

Os números são **ilustrativos**, não resultados de uma execução real. Cada etapa também é enviada ao callback `OnProgress func(ImportProgress)` no mesmo processo. O objeto `ImportProgress` inclui `Stage`, `Message`, `SourcePath`, `DestinationPath`, `BytesSent`, `TotalBytes`, `Files`, `At`.

Os callbacks são síncronos durante a transferência; um callback bloqueante reduz o throughput. O chamador deve serializar acesso caso várias execuções escrevam no mesmo `io.Writer`.

## CLI

Para uma VM que já esteja em execução:

```bash
go run . import \
  -config config.yaml \
  -host 192.168.122.10:22 \
  -source /home/usuario/projeto \
  -dest /tmp/projeto \
  -user insula \
  -key /home/usuario/.ssh/id_ed25519 \
  -known-hosts /home/usuario/.ssh/known_hosts
```

Também é possível enviar apenas `/home/usuario/arquivo.tar.gz`, que será transportado como **um arquivo dentro de um TAR.GZ temporário**. A operação não descompacta o próprio arquivo `arquivo.tar.gz` por segunda vez.

## Biblioteca Go

```go
engine, err := insulavm.New("/etc/insulavm/config.yaml")
if err != nil { return err }
result, err := engine.ImportPath(ctx, insulavm.ImportRequest{
    Host: "192.168.122.10:22", // endereço de uma VM já iniciada
    SourcePath: "/srv/projeto",
    DestinationPath: "/tmp/projeto",
    User: "insula",
    PrivateKey: "/etc/insulavm/id_ed25519",
    KnownHosts: "/etc/insulavm/known_hosts",
    Output: os.Stdout,
    OnProgress: func(p insulavm.ImportProgress) {
        fmt.Println("etapa:", p.Stage, "texto:", p.Message)
    },
})
if err != nil { return err }
if !result.Imported { return errors.New("importacao nao confirmada") }
```

Um projeto orquestrador que inicia a VM usando `engine.StartVM(ctx, profileName)` deve passar o endereço `instance.Address` como `Host` para `ImportPath` e chamar `instance.Cleanup` quando terminar. `ImportPath` não cria uma nova VM.

## Importação automática antes de jobs

O manifesto aceita `imports` com `source` no hospedeiro e `destination` dentro do convidado. As importações são feitas **antes de setup e jobs**, reutilizando a mesma conexão SSH; se a importação falhar, nenhum job começa.

```yaml
profile: debian-minimal
repository:
  name: meu-projeto
action: test
workspace: /tmp/projeto
imports:
  - source: /home/usuario/projeto
    destination: /tmp/projeto
pipelines:
  - name: smoke
    jobs:
      - name: listar-arquivos
        commands:
          - ls -la
```

No modo de biblioteca, `RunRequest.OnImportProgress` disponibiliza o mesmo acompanhamento estruturado; `RunResult.Imports` contém os resultados das importações. Em CLI, `Output: os.Stdout` imprime os textos. Origem relativa é resolvida a partir do diretório corrente do processo, não da pasta da VM.

## Pré-requisitos e segurança

- SSH confiável por chave, com host key validada em `known_hosts`; o guest deve ter `sh`, `tar` (com `-xzf`), `mktemp`, `mkdir`, `cat`, `wc` e `rm`.
- O usuário SSH necessita permissão de escrita em `/tmp` para staging e no diretório `destination` para os arquivos finais.
- **Use usuário SSH não privilegiado.** A extração atual **não impede** que um diretório remoto preexistente contenha links simbólicos; não importe para áreas controladas por usuários não confiáveis.
- Manifestos e caminhos de entrada são considerados fornecidos por um orquestrador confiável. Não exponha a operação como importação arbitrária para usuários externos sem aplicar autorização, quotas de espaço e validação dos destinos.
- A importação ainda não implementa checkpoint de retomada, SHA-256 remoto ou troca atômica da árvore de destino. Evite importar artefatos com arquivos em modificação.
- Sem socket/HTTP nesta operação: a aplicação Go pode usar a API diretamente, e a CLI tem uma operação específica.

## Referências

- [Go archive/tar](https://pkg.go.dev/archive/tar)
- [Go compress/gzip](https://pkg.go.dev/compress/gzip)
- [Go SSH Session](https://pkg.go.dev/golang.org/x/crypto/ssh#Session)
