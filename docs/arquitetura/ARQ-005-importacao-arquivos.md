<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-005 — Importação TAR.GZ com destino global

**Estado:** implementado em CLI, biblioteca Go e `insula.yaml`. Testado com servidor SSH simulado; teste real em KVM/Libvirt permanece pendente.

**Código:** [archive.go](../../pkg/transfer/archive.go), [remote.go](../../pkg/transfer/remote.go), [import.go](../../insulavm/import.go), [import_vm.go](../../insulavm/import_vm.go), [import_cli.go](../../import_cli.go).

## Contrato de diretório único

Um único destino é escolhido no arquivo global `config.yaml`, dentro da VM, e reutilizado por **todas as execuções e importações posteriores**.

```yaml
guest:
  destination: /tmp/insulavm/workspace
```

- `guest.destination` deve ser um caminho Linux absoluto e diferente de `/`. Se omitido, usa `/tmp/insulavm/workspace`.
- Não existe mais `destination` em `imports` do manifesto, `-dest` nos comandos CLI, nem `DestinationPath` nas estruturas públicas `ImportRequest` e `VMImportRequest`.
- O InsulaVM cria o diretório dentro do convidado com o usuário SSH normal. Se o local exigir escrita privilegiada, o perfil QCOW2 precisa ter as permissões preparadas. Nenhuma elevação implícita é feita.
- O executor de Action utiliza esse mesmo destino como `workspace` quando a opção legada `workspace` não foi definida no manifesto; `ExecVM` também o usa por padrão.
- Uma pasta local transfere **seu conteúdo**, sem criar uma subpasta adicional; um arquivo transfere o arquivo pelo próprio nome. Importações sucessivas caem na mesma pasta e preservam os arquivos anteriores quando seus nomes não colidem.

## Fluxo implementado

1. Validar origem no hospedeiro (arquivo ou diretório). Recusar symlinks, dispositivos e entradas especiais.
2. Compactar arquivo(s) em TAR.GZ temporário no hospedeiro.
3. Autenticar por SSH com host key validada. Para VMs gerenciadas, usar as credenciais individuais recuperadas pelo UUIDv7 no SQLite; nenhuma chave é passada pelo consumidor.
4. Transferir o TAR.GZ pelo canal SSH. O convidado guarda em diretório temporário de staging.
5. Conferir tamanho recebido, extrair com `tar -xzf` no `guest.destination` e exigir confirmação explícita após o comando retornar sucesso.
6. Excluir arquivos temporários e retornar `ImportResult{Imported:true}`. Erros retornam `Imported:false` e evento `failed`.

**Limitações atuais:** não existe SHA-256 remoto adicional, extração atômica, retomada ou proteção completa contra symlinks preexistentes na pasta de destino. Conteúdo parcialmente extraído pode permanecer após falha. Use diretório controlado pelo próprio usuário da VM.

## Eventos de progresso

A CLI imprime textos e a biblioteca recebe os mesmos estágios por `OnProgress func(ImportProgress)`:

```text
[InsulaVM][import:connecting] Conectando via SSH ...
[InsulaVM][import:validating] Verificando arquivo ou diretorio de origem
[InsulaVM][import:compressing] Compactando conteudo em TAR.GZ
[InsulaVM][import:compressed] Compactacao concluida
[InsulaVM][import:uploading] Enviando: 20480/20480 bytes
[InsulaVM][import:verifying] Verificando integridade do tamanho recebido
[InsulaVM][import:extracting] Descompactando TAR.GZ dentro da VM
[InsulaVM][import:completed] Arquivo importado com sucesso
```

São exemplos ilustrativos; os textos reais incluem números e caminhos. `ImportProgress` inclui `Stage`, `Message`, `SourcePath`, `DestinationPath` (somente retorno informativo), `BytesSent`, `TotalBytes`, `Files` e `At`. Callbacks são síncronos; não faça operações bloqueantes dentro deles.

## CLI: importação por UUIDv7

```bash
go run . vm import -config config.yaml -id UUIDv7 -source /srv/projeto
go run . vm import -config config.yaml -id UUIDv7 -source /srv/config.ini
```

O comando resolve IP, porta, usuário, chave privada e `known_hosts` a partir do cadastro SQLite, sem solicitá-los novamente.

Para uma VM **não administrada** pelo InsulaVM:

```bash
go run . import -config config.yaml -host 192.168.122.10:22 -source /srv/projeto
```

Nesse modo externo, o host/conta/identidade devem ser provisionados pelo administrador; `ssh.private_key` e `ssh.known_hosts` ficam na configuração global.

## Biblioteca Go

```go
result, err := engine.ImportToVM(ctx, insulavm.VMImportRequest{
    VMID: vmID,
    SourcePath: "/srv/projeto",
    Output: os.Stdout,
    OnProgress: func(p insulavm.ImportProgress) {
        fmt.Println(p.Stage, p.Message)
    },
})
if err != nil { return err }
if !result.Imported { return errors.New("importação não confirmada") }
```

O motor obtém o destino de `guest.destination`, sem opção de alterá-lo por chamada. `Engine.ImportPath` tem o mesmo destino global, mas é reservado a SSH existente que já possui endereço e autenticação configurados.

## YAML de Action

```yaml
profile: debian-minimal
repository:
  name: meu-projeto
action: test
# ssh:
#   user: buildagent
imports:
  - source: /home/usuario/projeto
  - source: /home/usuario/config.ini
pipelines:
  - name: smoke
    jobs:
      - name: listar-arquivos
        commands:
          - ls -la
```

As importações são executadas antes dos jobs e sempre utilizam o destino global. `RunRequest.OnImportProgress` emite o acompanhamento para o consumidor Go, `RunResult.Imports` contém os resultados. Se o username opcional da Action for definido, ele será provisionado no novo convidado com a chave individual de SSH do InsulaVM. Para um host externo, a conta indicada precisa existir e estar autorizada a conectar com a chave configurada.

## Deploy de arquivo e script

O comando `vm deploy` combina duas importações e uma execução Bash, com extração TAR.GZ opcional e interrupção imediata se uma etapa falhar. O contrato completo do fluxo está em [FLUX-005](../fluxos/FLUX-005-deploy-arquivos.md), e as opções CLI estão em [INTF-TERM-005](../interfaces/terminal/INTF-TERM-005-deploy-vm.md). O destino padrão permanece `guest.destination`; esta operação não inicia uma nova VM.

## Referências

- [Go archive/tar](https://pkg.go.dev/archive/tar)
- [Go compress/gzip](https://pkg.go.dev/compress/gzip)
- [Go SSH Session](https://pkg.go.dev/golang.org/x/crypto/ssh#Session)
- [ARQ-006 — Identidade e acesso SSH](ARQ-006-identidade-registro-sudo.md)
