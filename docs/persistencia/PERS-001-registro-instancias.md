<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# PERS-001 — Registro SQLite de instâncias gerenciadas

**Estado:** implementado. Fonte canônica: [`pkg/registry/sqlite.go`](../../pkg/registry/sqlite.go).
**Escopo:** metadados persistentes de instâncias de VM; não é o catálogo de imagens QCOW2.

## Banco e tabela

O arquivo SQLite é definido por `storage.database` em [config.yaml](../../config.yaml); por padrão fica em `storage.root/insula.db`. A tabela `vm_instances` armazena histórico das instâncias que o InsulaVM iniciou. O banco utiliza WAL e `busy_timeout=10000`. As chaves privadas são guardadas **como caminhos**, nunca como conteúdo no banco.

```mermaid
erDiagram
    vm_instances {
        TEXT id PK
        TEXT libvirt_uuid UK
        TEXT name UK
        TEXT profile
        TEXT ip
        TEXT mac
        TEXT host_fingerprint
        TEXT sudo_mode
        TEXT ssh_user
        INTEGER ssh_port
        TEXT private_key_path
        TEXT known_hosts_path
        TEXT instance_dir
        TEXT status
        TEXT created_at
        TEXT updated_at
        TEXT finished_at
    }
```

### Colunas reais do schema

| Coluna | Tipo | Obrigatória / origem |
| --- | --- | --- |
| `id` | TEXT, PK | UUIDv7 gerado pela aplicação |
| `libvirt_uuid` | TEXT, UNIQUE | UUID do domínio criado no Libvirt |
| `name` | TEXT, UNIQUE | Nome do domínio `insula-...` |
| `profile` | TEXT | Nome do perfil de imagem-base selecionado |
| `ip` | TEXT | Padrão `''`, atualizado quando DHCP é confirmado |
| `mac` | TEXT | Endereço MAC gerado para instância |
| `host_fingerprint` | TEXT | Impressão digital da chave SSH do convidado |
| `sudo_mode` | TEXT | Modo de elevação configurado |
| `ssh_user` | TEXT | Conta SSH provisionada |
| `ssh_port` | INTEGER | Padrão 22 |
| `private_key_path` | TEXT | Caminho da chave privada de cliente no hospedeiro |
| `known_hosts_path` | TEXT | Caminho de `known_hosts` do hospedeiro |
| `instance_dir` | TEXT | Diretório temporário de disco/seed e credenciais |
| `status` | TEXT | `creating`, `running`, `failed` ou `destroyed` |
| `created_at` | TEXT | Instante UTC no formato RFC3339Nano |
| `updated_at` | TEXT | Última alteração UTC RFC3339Nano |
| `finished_at` | TEXT | Padrão `''`; instante UTC de finalização quando aplicável |

Todas as colunas são `NOT NULL`. `status` possui restrição `CHECK`. Existe índice `vm_instances_by_status(status, created_at)`. Não há FK para perfis: o nome `profile` referencia logicamente o catálogo em arquivos `profile.yaml`.

## Ciclo de vida

```mermaid
stateDiagram-v2
    [*] --> creating: Insert
    creating --> running: SetRunning
    creating --> failed: SetFailed
    creating --> destroyed: SetDestroyed
    running --> destroyed: SetDestroyed
    running --> failed: SetFailed
    failed --> destroyed: SetDestroyed
```

Os métodos `Insert`, `SetRunning`, `SetFailed` e `SetDestroyed` são a fonte de verdade dos estados **registrados**. O estado efetivo é consultado separadamente no Libvirt via [`pkg/vm/status.go`](../../pkg/vm/status.go). O valor `running` do SQLite sozinho **não comprova** que o domínio esteja ativo.

## Relações e consumo

| Consumidor | Dados necessários | Comportamento |
| --- | --- | --- |
| `vm list` / `vm status` | `id`, `name`, `profile`, `ip`, `status`, `libvirt_uuid` | Combina registro e consulta ao Libvirt |
| `vm exec` / `vm import` / `vm deploy` | `id`, `ip`, `ssh_port`, `ssh_user`, caminhos SSH | Localiza destino e credenciais da VM gerenciada |
| `vm stop` / `vm stop-all` | `id`, `libvirt_uuid`, `instance_dir`, `status` | Reconfirma domínio e remove recursos temporários |
| `vm freeze` | `id`, `profile`, `sudo_mode`, `instance_dir` | Confirma origem e captura disco em novo perfil |

`StopAllVMs` usa **apenas registros desta tabela**, nunca VMs externas não registradas. Perfis congelados são armazenados no filesystem sob `storage.profiles`; não criam uma tabela SQL adicional.

## Alocação e exclusividade do ID de VM

- Identidade: `UUIDv7` criptograficamente aleatório, usado igualmente em `vm_instances.id`, `vm_instances.libvirt_uuid`, `VMProgress.ID`, `Instance.ID` e no XML do domínio Libvirt.
- Antes de atribuir ID a uma nova instância, `unusedVMID` verifica o histórico SQLite. IDs de VMs `failed` e `destroyed` **não são reciclados**; após colisão, outro UUIDv7 é gerado (até oito tentativas).
- Antes de chamar `virsh create`, a inicialização executa `INSERT` normal, não `INSERT OR REPLACE`. `id PRIMARY KEY`, `libvirt_uuid UNIQUE` e `name UNIQUE` garantem recusa atômica de duplicidade inclusive em criações concorrentes. O registro de outra VM não é alterado.
- O estágio `registered` do callback `VMProgress` só é emitido **depois** desse `INSERT` ser concluído. O estágio `ready` ocorre após Libvirt, DHCP, SSH e sudo validados.
- Um domínio Libvirt que não consta do SQLite não é tratado como pertencente ao InsulaVM. Uma eventual recusa do Libvirt durante `virsh create` é erro de inicialização; o motor não assume esse domínio externo como sua VM.
- Consumidores da biblioteca podem obter detalhes pelo ID em `Engine.GetVM` e `Engine.GetVMStatus`, sem procurar pelo nome do perfil.

**Limite:** a pré-checagem de IDs é otimista, portanto não reserva um UUID globalmente entre bancos de dados ou hospedeiros diferentes. A unicidade atômica é garantida para instâncias que compartilham o mesmo SQLite; o Libvirt rejeita conflito de identidade no mesmo hipervisor.

## Migração e limitações

A inicialização aplica migrações aditivas para as colunas `host_fingerprint`, `sudo_mode` e `ssh_port` em bancos antigos. Não existe reconciliação automática de divergências SQLite/Libvirt ao iniciar o motor. Após remoção da VM, o histórico permanece no banco, mas os arquivos de credenciais e o overlay temporário são descartados.

## Evidências

- [Schema e operações SQLite](../../pkg/registry/sqlite.go)
- [Consulta real de estado](../../pkg/vm/status.go)
- [Gerenciamento de ciclo de vida](../../pkg/vm/runtime.go)
