# InsulaVM

O **InsulaVM** é uma ferramenta open-source de automação CLI em **Go** para executar scripts e pipelines em VMs **Debian Linux** isoladas.

**Estado atual:** o executor SSH sequencial, a leitura de `insula.yaml` e a persistência física de logs por job estão implementados. **Ainda não estão implementados** o provisionamento KVM/Libvirt, criação de perfis QCOW2 e transferência/clonagem do repositório. Nesta etapa, a VM já deve estar ligada e conter o diretório de trabalho.

## Funcionalidades disponíveis

- Interpretação de `repository.name`, `action`, `workspace`, `setup` e `pipelines[].jobs[]` em `insula.yaml`.
- Conexão SSH autenticada por chave privada e verificação obrigatória de `known_hosts`.
- Execução estritamente sequencial: só inicia o comando seguinte após o término e verificação do código de saída do anterior.
- Streaming simultâneo de stdout/stderr para terminal hospedeiro e arquivo físico.
- Um arquivo por job, com `>> ` para comandos e `<< ` para linhas recebidas.
- Controle de `allow_failure`, timeout por comando, limpeza de sessões e logs persistidos com escrita buffered.

Os logs ficam em:

```text
<logs-root>/<repositorio>/<action-ou-pipeline>/<YYYYMMDD_HHMMSS>/<job>.log
```

## Executar contra VM Debian existente

Pré-requisitos: Go 1.23+, VM acessível por SSH com chave válida em `known_hosts`, shell POSIX e projeto já existente dentro do `workspace` especificado no manifesto.

```bash
cp insula.yaml.example insula.yaml
# Ajuste os comandos no manifesto para a aplicação presente na VM.
go mod download
go run . \
  -manifest insula.yaml \
  -logs-root /var/lib/insulavm/logs \
  -host 192.0.2.10:22 \
  -user insula \
  -key ~/.ssh/id_ed25519 \
  -known-hosts ~/.ssh/known_hosts \
  -command-timeout 30m
```

`192.0.2.10` é endereço fictício. `INSULA_LOG_ROOT` também pode fornecer a raiz dos logs.

**Segurança:** os comandos e saídas são armazenados literalmente nos arquivos; nunca envie senhas/tokens embutidos nos comandos de um manifesto. Não há mascaramento automático nem rotação de logs neste ciclo.

## Testes

```bash
go test -race ./...
go vet ./...
```

## Arquitetura planejada

1. Criar perfil: informar nome, RAM (MiB), vCPUs e ISO Debian local; instalar e ajustar o sistema.
2. Salvar perfil: guardar imagem QCOW2 imutável no diretório configurado.
3. Iniciar Action: gerar overlay QCOW2 descartável a partir do perfil e iniciar VM via KVM/QEMU e Libvirt.
4. Materializar projeto local/remoto e executar `setup` e pipelines usando o executor SSH já existente.
5. Preservar logs e descartar instância temporária sem modificar o perfil.

## Documentação

- [Índice geral](docs/README.md)
- [ARQ-001 — Arquitetura do motor](docs/arquitetura/ARQ-001-motor-insulavm.md)
- [ARQ-002 — Perfis QCOW2](docs/arquitetura/ARQ-002-perfis-vm.md)
- [FLUX-001 — Action completa, proposta](docs/fluxos/FLUX-001-execucao-manifesto.md)
- [FLUX-002 — Criação de perfil, proposta](docs/fluxos/FLUX-002-criacao-perfil-vm.md)
- [FLUX-003 — Execução sequencial e logs, implementado](docs/fluxos/FLUX-003-execucao-sequencial-logs.md)
- [INTF-TERM-002 — Execução pelo terminal](docs/interfaces/terminal/INTF-TERM-002-executar-manifesto.md)
