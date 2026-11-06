<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-003 — Configuração global e comandos de perfil

**Estado:** Implementado para leitura, validação, listagem, inspeção e registro de perfis preparados. Instalação a partir de ISO permanece proposta.

**Arquitetura:** [ARQ-003](../../arquitetura/ARQ-003-configuracao-perfis.md).

## Comandos implementados

| Comando | Finalidade |
| --- | --- |
| `go run . config check -config config.yaml` | Validar conteúdo de `config.yaml` e mostrar diretórios |
| `go run . profile list -config config.yaml` | Listar perfis cujo YAML seja válido |
| `go run . profile show -config config.yaml -name debian-minimal` | Mostrar identidade, recursos e localização relativa da imagem |
| `go run . profile register -config config.yaml -name debian-minimal -image /tmp/debian.qcow2 -memory-mib 1024 -vcpus 1 -disk-gb 8` | Registrar imagem QCOW2 **já instalada** como novo perfil |
| `go run . run -config config.yaml -manifest insula.yaml` | Iniciar VM descartável do perfil selecionado e executar jobs |
| `go run . run -config config.yaml -manifest insula.yaml -host 192.0.2.10:22` | Executar jobs em guest SSH existente, sem criar instância |

O comando `profile register` aceita adicionalmente `-os`, `-iso` (metadado informativo, não instalação), `-address` e `-user`. A imagem de origem deve existir localmente e ser QCOW2 com sistema instalado. Os parâmetros `-memory-mib` e `-vcpus` são obrigatórios; `-disk-gb` recebe padrão de `config.yaml` caso ausente.

## Configuração mínima

```yaml
storage:
  root: ./data
  profiles: profiles
  runs: runs
  logs: logs
virtualization:
  provider: kvm
  libvirt_uri: qemu:///system
  network: default
  default_disk_gb: 8
ssh:
  user: insula
  connect_timeout_seconds: 60
```

Os caminhos SSH, `-key` e `-known-hosts`, devem ser configurados para que uma execução real seja autenticada. Chaves privadas não devem ser embutidas nos arquivos YAML; use apenas caminhos de arquivo.

## Seleção de perfil no manifesto

```yaml
profile: debian-minimal
repository:
  name: projeto-teste
action: test
workspace: /app/repo-workspace
pipelines:
  - name: checks
    jobs:
      - name: version
        commands:
          - uname -a
```

A CLI resolve `profile` a partir de `storage.profiles`, verifica `profile.yaml` e, se não existir endereço SSH previamente configurado nem argumento `-host`, cria overlay QCOW2 e inicia a VM. Um erro de SSH ou de boot interrompe a Action e tenta remover os recursos transitórios.

## Limitações

- Uma ISO de instalação não é um QCOW2 de perfil. A ferramenta **não instala** um SO automaticamente neste ciclo.
- A imagem deve conter SO já instalado, DHCP, SSH e o `workspace` existente; a aplicação ainda não transfere o código do repositório.
- Sem Cloud-Init por instância, preparação das chaves do host e da confiança SSH permanece tarefa do administrador.
- A VM precisa ser acessível pelo Libvirt e ter permissões sobre o arquivo de disco.
- `profile show/list` não inicializam VM. `profile register` copia o disco uma vez; Actions futuras utilizam overlays sem copiar todo o disco.

**Fluxos relacionados:** [FLUX-001](../../fluxos/FLUX-001-execucao-manifesto.md) e [FLUX-003](../../fluxos/FLUX-003-execucao-sequencial-logs.md).
