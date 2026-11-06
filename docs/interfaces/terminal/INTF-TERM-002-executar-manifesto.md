<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-002 — Executar manifesto com SSH gerenciado

**Estado:** implementado para execução em VM QCOW2 previamente preparada com Cloud-Init, ou VM SSH externa. Instalação do SO a partir de ISO instaladora permanece pendente.

**Fluxo:** [FLUX-001](../../fluxos/FLUX-001-execucao-manifesto.md). **Biblioteca:** [ARQ-004](../../arquitetura/ARQ-004-biblioteca-go.md).

## Execução direta

```bash
go run . run -config config.yaml -manifest insula.yaml
```

Quando `insula.yaml` indica um `profile`, a CLI inicia a VM descartável, provisiona SSH e elimina a necessidade de informar chave, usuário ou endereço remoto. A conexão valida a chave de host da instância.

## Arquivos de configuração

**`config.yaml`** informa um único destino compartilhado por todas as VMs e transferências:

```yaml
guest:
  destination: /tmp/insulavm/workspace
ssh:
  user: insula
```

**`insula.yaml`** possui a Action, fontes locais e usuário opcional:

```yaml
profile: debian-minimal
repository:
  name: projeto
action: build
# ssh:
#   user: buildagent
imports:
  - source: /srv/projeto
  - source: /srv/extra.ini
pipelines:
  - name: build
    jobs:
      - name: listar
        commands:
          - pwd
          - ls -la
```

`workspace`, quando omitido, resolve para `guest.destination`. A pasta é criada pelo usuário SSH antes dos comandos. A Action não aceita `destination`, `key`, `known_hosts` ou senha. O `ssh.user` da Action, quando presente, é provisionado na VM criada; se ausente, usa o usuário do perfil ou o padrão global `insula`.

## CLI e parâmetros

| Opção | Obrigatória | Comportamento |
| --- | --- | --- |
| `-config` | Não | Arquivo global, padrão `config.yaml` |
| `-manifest` | Não | Action YAML, padrão `insula.yaml` |
| `-logs-root` | Não | Sobrescreve `storage.logs` |
| `-command-timeout` | Não | Padrão 30 minutos por comando |
| `-host` | Somente modo externo | Executa em VM já existente, sem iniciar perfil |
| `-user` | Não | Override legado; prefira `ssh.user` na Action |
| `-key` / `-known-hosts` | Não para VMs gerenciadas | Somente para host externo, quando não configurados globalmente |

Hosts externos **não gerenciados** exigem credenciais SSH previamente preparadas; informe seus caminhos no `config.yaml`:

```yaml
ssh:
  user: insula
  private_key: /etc/insulavm/external_id_ed25519
  known_hosts: /etc/insulavm/known_hosts
```

Não armazene conteúdo de chave privada em YAML. Para importações posteriores em uma VM registrada, use `go run . vm import -id UUIDv7 -source /srv/arquivo`; o banco SQLite fornece a identidade SSH e o destino sempre vem de `guest.destination`.

## Resultado e limitações

O terminal apresenta as linhas de cada job e o diretório com os logs persistentes. Código de saída `0` significa sucesso; falhas fatais retornam `1`. `Ctrl+C` cancela a operação e inicia limpeza da instância transitória.

Requer Linux com KVM/Libvirt e imagem QCOW2 **já instalada com Cloud-Init e SSH** para provisionamento automático. Ainda não instala SO da ISO instaladora nem clona automaticamente repositórios Git; pode importar arquivos locais via `imports`.
