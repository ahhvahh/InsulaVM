<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# GUIA-004 — Teste intuitivo e automatizado de VMs/SSH/ISO

**Estado:** comandos implementados na CLI e API Go; teste KVM real deve ser executado no host do operador.
**Base técnica:** Debian 13 amd64, Libvirt/QEMU/KVM, NoCloud/Cloud-Init, SSH e Debian Live ISO.

## Preparar a CLI

Na raiz do projeto:

```bash
git switch fix/ssh-startup-test-cli
go build -o insula ./cmd/insula
./insula test doctor
```

`test doctor` verifica dependências do hipervisor, /dev/kvm e rede Libvirt. NÃO inicia uma VM.

## Fluxo em etapas — permite configurar a VM manualmente

```bash
./insula test download
./insula test install -profile debian13-mini
./insula test boot -profile debian13-mini
```

`download` baixa e valida SHA256 da ISO oficial `mini.iso` (~64 MiB) em `data/isos/mini.iso`. O download e a instalação precisam de internet. `install` só instala se o perfil ainda não existir. `boot` cria uma instância e imprime seu UUID e IP.

```bash
./insula test ssh -id UUID_DA_VM
./insula test shell -id UUID_DA_VM
```

Personalize o Debian com comandos como `sudo apt-get update`, `sudo apt-get install -y git` e `exit`. Sair do shell **não desliga** a instância.

Após a preparação:

```bash
./insula test iso -id UUID_DA_VM -name debian13-preparado -confirm
```

Essa operação instala/prepara `live-boot`, congela a VM como **QCOW2 autônomo**, encerra e remove a instância original, gera uma ISO Live do sistema preparado e, por padrão, inicia outra VM para verificar boot e execução SSH. `-confirm` é obrigatório porque há descarte da instância original após o congelamento.

Se quiser apenas exportar sem iniciar a VM de verificação, acrescente `-verify=false`. Para verificar uma ISO já existente:

```bash
./insula test verify -profile debian13-preparado
```

## Fluxo totalmente automático

Se você possui um script de preparação para o guest:

```bash
./insula test cycle \
  -profile debian13-mini \
  -name debian13-go-pronto \
  -startup-script /caminho/absoluto/preparar.sh \
  -confirm
```

O `cycle` baixa e instala a `mini.iso` se o perfil ainda não existir, inicia a VM, executa o script de preparação como root via Cloud-Init, espera SSH, exporta a ISO Live e testa seu boot. Quando `-confirm` for omitido, o comando **não congela nem apaga** a instância; imprime os comandos para abrir o shell e exportar depois.

O script é incorporado ao Cloud-Init e executado como **root**. Use exclusivamente script confiável (não vindo de repositório desconhecido).

Para testar diretamente o perfil existente e preparar manualmente:

```bash
./insula test boot -profile debian13-teste -ssh-ready-timeout 8m
./insula test ssh -id UUID_DA_VM
./insula test shell -id UUID_DA_VM
```

## Diagnóstico e preservação

Em testes, `test boot` sempre habilita `PreserveOnFailure`. Falhas após iniciar o domínio deixam overlay, ISO NoCloud, chave SSH e instância Libvirt preservados. O log de boot informa UUID, caminho e último erro SSH. O registro SQLite ficará com `status=failed`, mas o estado Libvirt poderá permanecer `running`.

```bash
./insula test ssh -id UUID_PRESERVADO
./insula vm console -id UUID_PRESERVADO
./insula vm shell -id UUID_PRESERVADO
./insula vm stop -id UUID_PRESERVADO
```

`vm stop` elimina o overlay temporário e o material de depuração da instância. Execute-o somente quando não precisar mais deles.

Para modo operacional fora do teste, a preservação é opcional:

```bash
./insula vm start -profile debian13-teste \
  -ssh-ready-timeout 8m -preserve-on-failure
```

A espera DHCP (3 min) e a SSH (padrão 5 min, ajustável 0..1h) são independentes. A biblioteca `executor.DialContext` limita conexão, troca de chaves e autenticação; o console exibe `ssh-retry` e o erro da última tentativa.

## Integração Go

```go
engine, _ := insulavm.New("config.yaml")
vm, err := engine.StartVMWithOptions(ctx, insulavm.VMStartOptions{
    Profile:"debian13-teste",
    SSHReadyTimeout:8*time.Minute,
    PreserveOnFailure:true,
}, nil)
diagnostico, err := engine.DiagnoseVMSSH(ctx, vm.ID, 15*time.Second)
_ = diagnostico
```

Em uma inicialização malsucedida, `StartVMWithOptions` retorna erro; o UUID fica nos eventos `VMProgress` (`registered`, `preserved`). Outros métodos disponíveis são `DownloadDebianMiniISO`, `CreateProfileFromISO`, `ExportVMToLiveISO` e `StartVMFromISO`.

## Execução local das suítes

```bash
bash scripts/tests/unit.sh
bash scripts/tests/integration.sh --config /CAMINHO/config.yaml --installer-iso /CAMINHO/mini.iso
```

A primeira suíte usa simuladores, a segunda depende de um host KVM real. O workflow GitHub Actions continua **desativado**, e a CLI `test` não reativa CI.

## Limitações conhecidas

- A ISO `mini.iso` requer conexão ao espelho Debian dentro do instalador.
- A ISO Live não é cópia bit a bit de um disco virtual: o perfil congelado QCOW2 mantém o disco; a ISO disponibiliza o filesystem como um sistema Live.
- A edição interativa só é possível após a VM atingir SSH. Em falhas, inspecione console serial, Cloud-Init e o teste Go de SSH.
- A validação integral (instalar, iniciar, congelar e executar da ISO Live) ainda depende de execução no servidor real com /dev/kvm.

## Documentação oficial
- Debian netboot: https://deb.debian.org/debian/dists/trixie/main/installer-amd64/current/images/netboot/
- virt-install: https://manpages.debian.org/trixie/virt-install/virt-install.1.en.html
- SSH ClientConfig: https://pkg.go.dev/golang.org/x/crypto/ssh#ClientConfig
