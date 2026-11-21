# TG-59 — Padronizar specs e o fluxo planejador, coordenador e orq

**Épico:** [Publicar e revisar especificações na Central](../README.md)
**Status da spec:** em validação

## O que muda

A Parte 1 do [desenho do épico](../README.md#parte-1--estrutura-padrão-de-documentação-orq-59) vira regra: a estrutura `docs/specs/<épico>/tg-<ref>-<nome-curto>/` com `spec.md`, `plano.md` e `decisoes.md` e o fluxo planejador → MR só da spec → pendência de validação → Ready. Esta US entrega também a parte do orq que o fluxo exige:

1. **Registro da spec** de uma US: pasta e MR da spec, com estado no snapshot.
2. **Pendência de validação** (`validacao`), presa ao SHA da cabeça da MR da spec.
3. **Conferência antes de mesclar**: só o `decisoes.md` pode ter mudado entre o SHA aprovado e a cabeça.
4. **Indicador de estado da spec** no card da US na Central.
5. **Instruções** na skill `orquestrador` e no `AGENTS.md` do orq.

A renderização da spec na Central é da TG-60; o link público, da TG-61.

## Registro da spec

```
orq spec registrar TG-<ref> --caminho <pasta relativa> --mr <url da MR da spec> [--substituir]
```

- O repositório sai da URL da MR; não há `--repo`.
- O mesmo comando liga a MR à US (como `orq mr add`), se ainda não estiver ligada, na mesma transação.
- Uma spec por US. Se a US já tem spec registrada, o comando recusa e mostra o registro atual. Quem chama avisa o responsável e pergunta se substitui; com o sim, repete com `--substituir`. O evento no diário guarda os valores anteriores e os novos.
- O orq usa a pasta registrada e nunca deduz a pasta pelo nome.

## Pendência de validação

- Tipo novo `validacao`, aberto pelo arquivo de pendência ou por `orq pend add --tipo validacao`.
- Título gerado pelo orq: "Spec da TG-<ref> pronta para validar". Opções fixas **Aprovar** e **Pedir ajustes**, nenhuma recomendada, mais o texto livre que a pendência já aceita.
- Na importação, o orq grava o **SHA da cabeça da MR da spec registrada**; o coordenador não escreve o SHA. Se a cabeça andar depois, a pendência continua presa ao SHA que gravou.
- **Sem spec registrada:** a importação é recusada, com o motivo no `.erro.txt`, como em arquivo inválido. Pela CLI, erro de uso.
- **Falha da forja ao ler o SHA** (fora do ar, limite de taxa): pelo arquivo, a pendência fica retida, sem aparecer na Central e sem `.erro.txt`, e o daemon tenta de novo no ciclo seguinte. Pela CLI, erro na hora.

## Conferência antes de mesclar

```
orq spec conferir TG-<ref>
```

Só leitura. Toma o SHA da última `validacao` aprovada, compara com a cabeça da MR da spec pela API da forja e responde "pode mesclar" ou lista os arquivos que mudaram além do `decisoes.md`. O daemon faz a mesma conferência e a mostra no card.

## Estado no snapshot e na Central

O snapshot ganha, por US, o campo `spec` (aditivo, sem subir `versao`): caminho, URL da MR da spec, estado e SHAs. O card da US mostra um indicador com um destes estados:

- **registrada**: spec registrada, sem validação;
- **em validação**: `validacao` aberta, com o SHA;
- **aprovada**: com o SHA aprovado;
- **mudou além do decisoes.md**: a cabeça da MR andou depois da aprovação em outro arquivo;
- **mesclada**: MR da spec mesclada.

O TUI fica fora do épico.

## Formato do `decisoes.md`

Uma seção por entrada, com campos fixos. Quem escreve escreve para o cliente: origem por referência (`TG-<ref>#P<n>`), sem nome de pane nem comentário interno.

```markdown
## D<n> — <título curto>
- **Data:** AAAA-MM-DD
- **Origem:** conversa de planejamento (r<n>) | TG-<ref>#P<n>, respondida em AAAA-MM-DD
- **Decisão:** <o que foi decidido>
- **Fala:** "<fala do responsável>"
```

A entrada de aprovação traz também `- **SHA aprovado:** <sha>` e, quando houver, a análise do usuário final na decisão.

## Critérios de aceite

- `orq spec registrar` grava a spec e liga a MR à US numa transação, com evento no diário; recusa duplicidade sem `--substituir`, mostrando o registro atual; com `--substituir`, o evento traz os valores anteriores.
- `orq spec registrar` sem `--repo`: o repositório vem da URL; URL que não é MR é recusada.
- Pendência `validacao` importada grava o SHA da cabeça da MR da spec; título e opções são os do orq, independentemente do que o arquivo traga.
- `validacao` sem spec registrada: recusada com `.erro.txt` (arquivo) ou erro de uso (CLI).
- `validacao` com forja falhando: retida sem `.erro.txt` e importada no ciclo em que a forja volta; pela CLI, erro.
- `orq spec conferir` diz "pode mesclar" quando só o `decisoes.md` mudou e lista os arquivos quando mudou mais; sem validação aprovada, diz isso.
- Snapshot com `spec` por US, `versao` inalterada, `docs/snapshot.md` atualizado.
- Card da US mostra os cinco estados.
- Migration nova, aditiva, com `-- orq:leitor-minimo` e `-- orq:escritor-minimo` abaixo do número dela.
- Skill `orquestrador` (PR no BasisTI/skills) e `AGENTS.md` do orq descrevem a estrutura, o fluxo, o registro, a validação, a conferência e o formato do `decisoes.md`.
- `go vet`, `go test -race ./...` e `gofmt -l .` limpos; frontend rebuildado se o template mudar.
