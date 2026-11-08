<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# ARQ-006 — Instâncias registradas, SSH automático e elevação sudo

**Estado:** implementado no motor Go, biblioteca e CLI. Validado por testes de código, geração de Cloud-Init com comando ISO simulado, SQLite real e SSH simulado. **Não validado ainda em um hipervisor KVM/Libvirt real**.

## Dois tipos diferentes de ISO

- **ISO instaladora do Linux:** usada para instalar Debian/Arch/Alpine em um disco vazio. **Ainda não implementada** a instalação desassistida pelo InsulaVM.
- **ISO NoCloud/CIDATA (`seed.iso`):** configura uma **imagem QCOW2 com Linux já instalado** e pacotes `cloud-init`, `openssh-server` e `sudo` disponíveis. O InsulaVM **gera esta ISO automaticamente** para cada inicialização e anexa como um CD-ROM virtual em Libvirt.

Pré-requisito: preparar uma imagem-base compatível, com cache do Cloud-Init limpo e DHCP habilitado. Ao iniciar, a VM precisa ter VirtIO, systemd/OpenRC ou init equivalente, suporte a cloud-init e SSH. Uma instalação comum sem cloud-init não aceita as credenciais geradas pelo novo fluxo.

## Instalação administrativa na criação da VM

A biblioteca oferece `StartVMWithOptions(ctx, VMStartOptions{Profile,StartupScriptPath}, onProgress)`: o arquivo de script deve estar no host, ser regular, absoluto e ter no máximo 256 KiB. O InsulaVM coloca seu conteúdo no `user-data` do seed NoCloud usando codificação Base64, gera `write_files` e `runcmd` e o executa **como root no primeiro boot**. A inicialização só retorna como pronta depois de `cloud-init status --wait --long` e do marcador de sucesso do script; caso contrário retorna erro e tenta descartar a VM. O script **não** é carregado do manifesto do repositório ou de um TAR.GZ de testes, somente de origem administrativa confiável. O perfil QCOW2 pode ser reutilizado com scripts distintos porque o script é específico da instância.

## Bootstrap automático

1. Gerar **UUIDv7** exclusivo, MAC aleatório QEMU e dois pares de chaves Ed25519: chave do cliente e chave de identidade SSH do servidor convidado.
2. Escrever a chave **privada do cliente** em arquivo exclusivo do host, modo `0600`; a pública no Cloud-Init como `ssh_authorized_keys`.
3. Escrever a chave **privada de host do convidado** no `user-data` do Cloud-Init, como `ssh_keys.ed25519_private`; o host retém a **pública** para validar a conexão, evitando TOFU/`InsecureIgnoreHostKey`. O arquivo de seed contém a chave privada de host, portanto é sensível, precisa de permissões restritas e é removido ao encerrar a VM.
4. Gerar `meta-data` (`instance-id` = UUIDv7) e `user-data`, criar ISO9660 rotulada `cidata` com `genisoimage`, `mkisofs` ou `xorriso`.
5. Criar overlay QCOW2 privado e domínio Libvirt transitório com UUID, MAC, disco VirtIO e CD-ROM `seed.iso`.
6. Descobrir IP atribuído pelo DHCP Libvirt. Gerar `known_hosts` local com a chave pública **previamente conhecida** da VM.
7. Aguardar SSH autenticado, validando host key. Se sudo estiver autorizado, testar `sudo -n true` por SSH.
8. Atualizar registro SQLite para `running`. Em falhas, destruir a VM quando possível, remover temporários e registrar `failed`.

**Não existe negociação de senha SSH nem login como root.** O usuário normal (padrão: `insula`) autentica via chave Ed25519 individual da instância. O IP é uma propriedade que pode mudar; o identificador permanente é o UUIDv7.

## Seleção do usuário SSH por Action

```yaml
# config.yaml: padrão de todas as VMs
guest:
  destination: /tmp/insulavm/workspace
ssh:
  user: insula
```

```yaml
# insula.yaml: override opcional da Action
profile: debian-minimal
ssh:
  user: buildagent
repository:
  name: projeto
action: testar
imports:
  - source: /srv/projeto
setup:
  - id -un
```

A opção `ssh.user` da Action é **opcional**: se ausente, o InsulaVM usa o usuário do perfil (quando definido) ou a configuração global (`insula` por padrão). O Cloud-Init provisiona o usuário escolhido e injeta sua chave pública individual. **Nunca** informe `key`, `known_hosts`, senha ou diretório remoto no YAML da Action.

O caminho `guest.destination` é sempre o destino dos arquivos e o diretório inicial dos jobs quando `workspace` for omitido. O InsulaVM tenta criá-lo **como o usuário SSH**, sem elevar privilégios automaticamente. Para pastas protegidas, prepare permissões no perfil ou escolha um caminho acessível.

Se a Action for executada numa VM SSH **externa**, não gerenciada pelo InsulaVM, a conta já precisa existir e aceitar uma chave previamente autorizada. Nesse caso, configure `ssh.private_key` e `ssh.known_hosts` no `config.yaml` global; não há como provisionar uma conta remota sem acesso prévio.

## SQLite e arquivos de instância

A CLI e as aplicações Go importadoras usam a mesma configuração `config.yaml`. O caminho-padrão de banco é `<storage.root>/insula.db`, ajustável com `storage.database`.

A tabela `vm_instances` mantém UUIDs, perfil, identidade e endereços SSH, caminhos dos temporários, estados e datas. Os **campos e tipos canônicos** estão em [PERS-001 — Registro SQLite](../persistencia/PERS-001-registro-instancias.md), derivados de [`pkg/registry/sqlite.go`](../../pkg/registry/sqlite.go).

Estados persistidos: `creating`, `running`, `failed`, `destroyed`. Porta SSH padrão: `22`, também registrada para suportar destinos com porta diferente. Banco WAL e timeout de espera; histórico permanece após limpeza dos discos e chaves. A chave privada **não é armazenada no SQLite**. Cada instância mantém os dados sensíveis em um diretório próprio e restrito dentro de `storage.runs`.

Limite: a versão atual ainda não faz reconciliação automática após desligamento inesperado ou queda do serviço. Por isso, um registro `running` deve ser considerado estado **persistido** até ser verificado contra Libvirt.

## Política sudo

O modo padrão de biblioteca **sem configuração explícita** é `disabled`; no `config.yaml` de exemplo, `passwordless` está explicitamente habilitado para uso em **VMs descartáveis confiáveis**.

```yaml
ssh:
  user: insula
  sudo_mode: passwordless # ou disabled
```

O Cloud-Init cria o usuário com senha bloqueada, `ssh_pwauth: false`, `disable_root: true`. Se `passwordless`, configura `sudo: "ALL=(ALL) NOPASSWD:ALL"`; caso contrário, `sudo: false`.

Para comandos privilegiados via SSH, use **`sudo -n`**, sem PTY e sem senha. A opção `-n` impede prompts bloqueantes. A VM exige que o pacote `sudo` esteja instalado.

**Segurança:** `NOPASSWD:ALL` equivale a superusuário dentro do guest. Não habilite para executar jobs de terceiros não confiáveis sem isolamento, políticas adicionais e validação. Este acesso não concede root do hospedeiro diretamente, mas KVM/Libvirt devem continuar isolados e atualizados.

## Observabilidade e consistência do estado

- `vm start` e `RunRequest.OnVMProgress` notificam as etapas `profile`, `profile-ok`, `preparing`, `cloud-init`, `disk`, `permissions`, `registered`, `booting`, `running`, `dhcp`, `ip`, `ssh`, `sudo`, `ready`, `cleaning` e `failed` quando aplicáveis.
- `StartVMWithProgress(ctx, profile, callback)` permite à biblioteca acompanhar o fluxo; `StartVM` continua disponível sem callback. A CLI imprime eventos em `stderr`.
- A verificação de estado usa `virsh list --all --uuid` e `virsh domstate` para diferenciar domínios `running`, `blocked`, `paused`, `shut off` e `absent`, sem considerar o registro SQLite como prova de que a VM está em execução.
- `vm list` e `vm status -id UUID` mostram **estado registrado** e **estado real**. Divergências são sinalizadas sem alterar automaticamente o banco; indisponibilidade do Libvirt resulta em erro.
- Durante a inicialização, se o domínio parar enquanto aguarda IP ou SSH, o processo falha imediatamente com o estado efetivo, em vez de aguardar todo o timeout.
- `vm stop` consulta Libvirt antes de remover o overlay; se não puder confirmar a ausência/desligamento seguro do domínio, mantém o disco para análise.
- Uma nova execução de `vm start` cria uma **nova VM descartável**; a CLI avisa quando encontra outra VM ativa do mesmo perfil.

Estas verificações evitam confundir registros `creating` persistidos após um erro com máquinas efetivamente executando.

## CLI

```bash
# Inicia uma VM de um perfil QCOW2 com Cloud-Init já preparado.
go run . vm start -config config.yaml -profile debian-minimal

# Exibe UUIDv7, IP, MAC e estado registrados.
go run . vm list -config config.yaml
go run . vm status -config config.yaml -id <UUIDv7>

# Executa comando como usuário insula.
go run . vm exec -config config.yaml -id <UUIDv7> -command "id -un"

# Eleva um comando individual por sudo -n (quando autorizado).
go run . vm exec -config config.yaml -id <UUIDv7> -command "id -u" -sudo

# Copia arquivos pelo UUID, usando as credenciais guardadas no cadastro.
go run . vm import -config config.yaml -id <UUIDv7> -source /srv/projeto

# Encerra por ID, remove overlay/seed/chaves e mantém histórico SQLite.
go run . vm stop -config config.yaml -id <UUIDv7>
```

Os exemplos com `<UUIDv7>` contêm marcadores ilustrativos. Em comandos reais, substitua pelo ID retornado pela inicialização.

## Biblioteca Go

```go
engine, err := insulavm.New("config.yaml")
if err != nil { return err }

instance, err := engine.StartVM(ctx, "debian-minimal")
if err != nil { return err }

vmInfo, err := engine.GetVM(ctx, instance.ID)
if err != nil { return err }
fmt.Println(vmInfo.IP, vmInfo.MAC)

execResult, err := engine.ExecVM(ctx, insulavm.ExecRequest{
    VMID: instance.ID,
    Command: "id -u",
    Sudo: true,
    Output: os.Stdout,
})
if execResult != nil { fmt.Println(execResult.LogDirectory) }
if err != nil { return err }

// Chamar quando o projeto orquestrador terminar.
return engine.StopVM(ctx, instance.ID)
```

`Engine.Run` também utiliza o mesmo bootstrap de credenciais quando cria uma VM descartável. O modo `Host` que conecta a uma VM existente continua exigindo credenciais SSH configuradas previamente.

## Encerramento em lote e congelamento

- `StopAllVMs` e `vm stop-all [-dry-run]` consultam **apenas registros gerenciados** e verificam o estado de cada domínio no Libvirt antes da limpeza. Não devem atingir VMs externas. Ver [INTF-TERM-004](../interfaces/terminal/INTF-TERM-004-gerenciar-vms.md) e [PERS-001](../persistencia/PERS-001-registro-instancias.md).
- `FreezeVM`, via `vm freeze -id ... -name ...`, limpa cache de Cloud-Init e chaves SSH do convidado, aguarda seu desligamento e registra uma imagem QCOW2 reutilizável. Ver [FLUX-004](../fluxos/FLUX-004-congelar-vm.md). A remoção da chave de identidade SSH **não cobre tokens ou dados de aplicações**.

## Requisitos de host

- KVM/QEMU, Libvirt com rede DHCP ativa, `qemu-img`, `virsh`, `genisoimage` ou `xorriso`.
- A conta responsável pelo Libvirt/QEMU deve conseguir **ler os discos QCOW2 e o CD-ROM seed.iso**, sem tornar os dados sensíveis públicos. Configure usuários/grupos/ACLs do hipervisor conforme o host Linux.
- A instalação de distribuição Linux a partir de ISO bare-metal, atualização/recuperação de imagens e reconciliação de estado são evoluções futuras.

## Fontes oficiais

- [Cloud-Init NoCloud](https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html)
- [Cloud-Init usuários, grupos e sudo](https://docs.cloud-init.io/en/latest/reference/yaml_examples/user_groups.html)
- [Cloud-Init SSH keys](https://docs.cloud-init.io/en/latest/reference/modules.html)
- [sudo(8) — opção -n](https://man7.org/linux/man-pages/man8/sudo.8.html)
- [SQLite driver pure-Go](https://pkg.go.dev/modernc.org/sqlite)
