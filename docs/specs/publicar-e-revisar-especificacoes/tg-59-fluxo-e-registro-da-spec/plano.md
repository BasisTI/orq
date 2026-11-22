# Plano — TG-59

Spec: [spec.md](spec.md). Decisões: [decisoes.md](decisoes.md).

## Passos

1. **Migration** `internal/central/migrations/NNNN_spec.sql` (número livre no `main` na hora de implementar; a onda em paralelo pode ocupar o seguinte): tabela da spec por US (projeto, ref, caminho, URL da MR da spec, criado/atualizado em) e colunas da pendência para o SHA da validação e o estado de retenção. Aditiva, com leitor e escritor mínimos abaixo do número dela.
2. **Store** `internal/central/spec.go`: `RegistrarSpec` (com `substituir`, liga a MR pela mesma lógica de `mr.go`, evento `spec.registrar` com anterior e novo no diário, na mesma transação); leitura da spec da US.
3. **Pendência `validacao`** em `internal/central/pend.go` e `pend_importacao.go`: tipo novo, título e opções fixos, SHA obrigatório. `internal/pendencias/arquivo.go` aceita `tipo: validacao` sem exigir opções; `importar.go` recusa sem spec (`.erro.txt`) e retém em falha da forja (sem `.erro.txt`, nova tentativa no ciclo seguinte).
4. **Leitura do SHA e do compare** em `internal/enriquecer`: cabeça da MR (já lida em `fontes.go`) e lista de arquivos alterados entre dois SHAs (`gh api repos/…/compare/A...B`, `glab api projects/…/repository/compare`), por caminho absoluto, só leitura.
5. **CLI** `internal/cli/spec.go`: `orq spec registrar`, `orq spec conferir`; `orq pend add --tipo validacao` em `pend.go`. Saída JSON fora de terminal, texto num terminal, erros no envelope.
6. **Daemon**: a cada ciclo, conferência das specs aprovadas e estado no cache do enriquecedor (não é estado da Central).
7. **Snapshot** `internal/central/snapshot.go`: campo `spec` por US; `docs/snapshot.md`.
8. **Web**: indicador no card da US (`internal/web/tmpl/central.html` / `onda.html`), com o estado e o SHA curto; `scripts/build-frontend.sh` se o CSS mudar.
9. **Docs**: `AGENTS.md` do orq (estrutura de `docs/specs`, skills de spec gravam na pasta da US) e README (comandos novos).
10. **Skill `orquestrador`** (BasisTI/skills, PR separado com `orq #59`): `references/planejamento.md` (verificação da estrutura antes de planejar, worktree antes do planejador, fluxo passos 0–8, `orq spec registrar|conferir`, pendência `validacao`, pergunta antes de `--substituir`), `assets/pacote-planejamento.md`, `assets/retorno-planejamento.md`, formato do `decisoes.md`.

## Testes

- Store: registrar, duplicidade, substituir com evento anterior/novo, MR ligada uma vez.
- Importação: `validacao` com spec (SHA gravado), sem spec (`.erro.txt`), forja falhando (retida e importada depois), título/opções impostos.
- Conferência com forja falsa: só `decisoes.md`, outros arquivos, sem aprovação.
- Snapshot: campo `spec`, `versao` 1.
- Web: os cinco estados no card.
