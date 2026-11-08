<!-- A-BIOMA-DOC-STANDARD -->
<!-- A-BIOMA-DOC-REVISADO -->

# INTF-TERM-001 — Criar perfil de máquina pelo terminal

**Estado:** Proposto — sem executável/flags implementadas.
**Plataforma:** CLI no hospedeiro Linux.
**Fluxo associado:** [FLUX-002 — Criação de perfil de VM](../../fluxos/FLUX-002-criacao-perfil-vm.md).
**Regras de persistência:** [ARQ-002 — Perfis persistentes](../../arquitetura/ARQ-002-perfis-vm.md).
**Contrato canônico proposto:** [CONTR-001 — CreateProfileFromISO](../../contratos/CONTR-001-operacoes-canonicas.md).

## Objetivo

Disponibilizar operação interativa de criação e configuração de perfil de VM, com instalação Debian pela ISO local e escolha de salvar ou descartar ao encerrar o terminal de manutenção.

## Invocação planejada

O nome de operação canônico **proposto em CONTR-001** é:

```bash
insula profile create-from-iso
```

Este comando **não existe na CLI atual**. O nome está definido como alvo documental; a forma interativa ou as flags, validação e códigos de saída ainda dependem da implementação. Não usar `insula vm profile create`, que diverge do contrato.

## Fluxo já implementado para preservar VMs preparadas

A criação interativa **a partir de ISO instaladora** descrita aqui permanece proposta. Para uma VM já iniciada a partir de perfil QCOW2, existe atualmente `insula vm freeze -id UUID -name PERFIL`. Sua interface e limitações estão em [INTF-TERM-004](INTF-TERM-004-gerenciar-vms.md), e a sequência em [FLUX-004](../../fluxos/FLUX-004-congelar-vm.md). Não confundir os dois fluxos.

## Perguntas ao usuário

| Ordem | Entrada | Validação pretendida |
| --- | --- | --- |
| 1 | Nome do perfil da VM | Não vazio, seguro para diretório e sem duplicidade |
| 2 | Memória RAM em **MiB** | Inteiro positivo dentro dos limites do host |
| 3 | Quantidade de núcleos virtuais (vCPUs) | Inteiro positivo, sujeito a limites de alocação |
| 4 | Tamanho do disco virtual em **GiB** | Inteiro positivo e espaço suficiente no host, sem reduzir disco existente |
| 5 | Caminho absoluto de ISO Debian já baixada | Arquivo existe, é legível e compatível com o modo de boot |
| 6 | Confirmação de provisionamento | Mostrar resumo antes de criar |
| Após saída do terminal | Salvar alterações? **Sim/Não** | Exigir escolha explícita |

O tamanho do disco integra a entrada canônica `DiskGiB` de `CreateProfileFromISO`. Sua coleta é obrigatória no fluxo de criação proposto; não há flag nem prompt implementado.

## Comportamento do terminal

- Exibir avanço da criação (validação, criação de disco, definição do domínio, boot, prontidão).
- Abrir console de instalação apropriado à ISO e indicar como retornar à CLI.
- Após a instalação, abrir um **novo terminal interativo SSH com PTY** para manutenção do Debian. Se SSH não estiver pronto, oferecer console de recuperação.
- Ao encerrar a shell de manutenção, retomar a CLI e perguntar se o usuário deseja salvar alterações.
- Em caso de **sim**, mostrar andamento do desligamento, validação do disco e publicação da imagem de perfil. Em caso de **não**, descartar staging da sessão.
- Não tratar perda inesperada de conexão como uma resposta de descarte ou salvamento.
- Não exibir dados secretos ou caminhos confidenciais de credenciais na saída.

## Exemplo textual ilustrativo

```text
InsulaVM — Criar perfil (proposta)
Nome do perfil: debian-build
RAM (MiB): 4096
vCPUs: 2
Disco virtual (GiB): 16
Caminho da ISO local: /iso/debian-installer.iso

Provisionando VM temporária...
Abrindo console de instalação Debian...
[Após instalar e reiniciar no disco]
Abrindo terminal de manutenção...
[O usuário encerra a shell]
Deseja salvar as alterações? [s/N]: s
Desligando o Debian e salvando imagem QCOW2...
Perfil 'debian-build' publicado.
```

O texto acima é **simulação documental**, não evidência de um comando funcional.

## Entradas, saídas e cancelamento

- **stdin/TTY:** perguntas, confirmações e interação com a shell do convidado, após transferência explícita do controle.
- **stdout:** avanço, indicação de sessão, resumo de resultado e local lógico do perfil.
- **stderr:** validação, falha no boot, instalação incompleta, SSH e falha de publicação.
- **Códigos de saída:** **não definidos**; não inferir ou assumir `0`/`1` como contrato atual.
- **Cancelamento:** pedir confirmação, encerrar a instância quando seguro e tratar recursos temporários conforme [FLUX-002](../../fluxos/FLUX-002-criacao-perfil-vm.md).

## Decisões de interface pendentes

- Flags definitivas, uso de parâmetros diretos versus perguntas e formato da saída; o nome-alvo `profile create-from-iso` permanece o do CONTR-001.
- Teclas de retorno do console e escolha de terminal externo ou PTY no próprio processo.
- Apresentação de estados de recuperação e reconexão após queda do SSH.
- Formato da configuração global de diretórios, armazenamento e recursos padrão.
