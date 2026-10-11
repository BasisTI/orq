# orq

Central de US para o orquestrador multiagente: CLI `orq` e plugin do [Herdr](https://github.com/ogulcancelik/herdr) que consolidam, por coordenador, as ondas de User Stories, o status de cada uma, os agentes, as merge requests e as pendências.

Desenho e entregas: projeto `orq` no Taiga (https://agile.basis.com.br/project/orq).

Esta versão traz o estado da Central num SQLite e os comandos de registro. A consulta ao Herdr, ao GitLab e ao Taiga, o daemon, o TUI e o plugin chegam nas próximas entregas.

## Instalação

```bash
go install github.com/BasisTI/orq/cmd/orq@latest
orq version
```

Precisa de Go 1.27 ou mais novo. Não usa cgo.

## Estado

O banco fica em `$ORQ_STATE_DIR/central.db`; sem a variável, em `$XDG_STATE_HOME/orq/central.db` e, sem ela, em `~/.local/state/orq/central.db`. Ele roda em modo WAL: vários coordenadores, em processos diferentes, escrevem ao mesmo tempo, e cada escrita espera a vez por até 15 s.

## Saída e erros

- `--output json|text`. Sem a flag, texto num terminal e JSON fora dele. `orq snapshot` sai sempre em JSON.
- Erro vai para o stderr como `{"error":{"code","cause","recovery"}}` (ou `erro [code]: cause` em texto).
- Códigos de saída: `0` ok, `1` inesperado, `2` uso (inclui `ref_ambigua` e `coord_nao_resolvido`), `4` conflito (`transicao_invalida`, `us_em_outra_onda`, `onda_encerrada`, `pendencia_resolvida`, `banco_ocupado`), `5` não encontrado.

## Coordenador corrente

`orq coord registrar` grava o pane do coordenador (`$HERDR_PANE_ID` por padrão). Os outros comandos acham o coordenador corrente, nesta ordem, por `--coord <nome>`, por `$ORQ_COORD` ou pelo `$HERDR_PANE_ID` registrado. Criar onda e `onda ver`/`proxima` sem id exigem coordenador; as escritas numa US funcionam sem ele e assinam o evento com o dono da onda.

Em `onda list`, `pend list` e `diario`, `--coord` filtra pelo coordenador dono da onda.

## Referências

- US: `projeto/312` (slug do Taiga) ou só `TG-312`/`312`. Sem projeto, a ref é procurada em todas as ondas; se houver mais de uma, vale a que está numa onda aberta do coordenador corrente e, depois, a do projeto padrão dele. O que continuar ambíguo é o erro `ref_ambigua`, que lista os candidatos.
- Pendência: `TG-312#P2` (ou `projeto/312#P2`). O número é sequencial por US e nunca é reaproveitado.
- MR: URL do GitLab (`…/<grupo>/<projeto>/-/merge_requests/N`) ou do GitHub (`…/<dono>/<repo>/pull/N`). Query, fragmento e subpáginas (`/diffs`) são descartados. Projeto sob `basis/iac/` é marcado como IaC.

## Comandos

| Comando | O que faz |
|---|---|
| `orq coord registrar <nome> [--pane <id>] [--workspace <id>] [--projeto <slug>]` | Cria ou atualiza o coordenador. Campo omitido mantém o valor gravado; o pane fica com um coordenador só. |
| `orq coord list` | Lista os coordenadores. |
| `orq onda criar <titulo> --us 215,216,217,218 [--projeto <slug>] [--ate plano\|mr-pronta\|merge] [--ordem estrita\|livre] [--depende 216:215]` | Cria a onda com as US em `wait`, na ordem dada. `--ate` padrão `mr-pronta`, `--ordem` padrão `livre`. `--depende` é repetível e aceita `218:216+217`. US sem projeto usam `--projeto` ou o projeto padrão do coordenador. |
| `orq onda ver [<id>]` | Onda com US, dependências, MRs e pendências. Sem id, a única onda aberta do coordenador. |
| `orq onda list [--abertas]` | Lista as ondas. |
| `orq onda proxima [<id>]` | Próxima US em `wait` que pode ser despachada; sem nada elegível, `us` vem nulo e `motivo` diz por quê. |
| `orq onda encerrar <id>` | Encerra a onda; as US ficam como estão e podem entrar numa onda nova. |
| `orq us status <ref> <wait\|plan\|exec\|rev\|done> [--pane <id>] [--papel planejamento\|implementacao\|revisao] [--rodada N] [--forcar]` | Muda o status. Transições livres, inclusive idas e voltas entre `exec` e `rev`; sair de `done` exige `--forcar`. Sem mudança, não grava nem gera evento. |
| `orq mr add <ref> <url>` / `orq mr rm <ref> <url>` | Liga ou desliga a MR da US. A mesma URL de novo não muda nada. |
| `orq pend add <ref> --tipo duvida\|autorizacao\|decisao "<texto>" [--pane <id>]` | Abre a pendência e imprime o id (`TG-312#P2`). |
| `orq pend resolver <ref>#P<n> [--resposta "…"]` | Resolve a pendência. |
| `orq pend list [--abertas] [--coord X] [--onda N]` | Lista as pendências. |
| `orq diario [--us <ref>] [--onda N] [--coord X] [--limite 50]` | Eventos gravados, do mais antigo ao mais novo (toda escrita gera um). |
| `orq snapshot [--incluir-encerradas]` | Estado consolidado em JSON versionado; contrato em [`docs/snapshot.md`](docs/snapshot.md). |
| `orq version` | Versão. |

### Ondas e a próxima US

A onda declara até onde vai (`--ate`) e a ordem. `done` numa US quer dizer que ela chegou a esse limite: plano aprovado (`plano`), MR pronta para revisão humana, sem merge (`mr-pronta`), ou MR mesclada (`merge`). `orq onda proxima` devolve a autorização e o texto do limite junto com a US, para quem despacha saber até onde ir.

- Uma US está liberada quando todas as dependências estão em `done`.
- `--ordem estrita`: uma US de cada vez, na ordem. Só a primeira US ainda não concluída é candidata, e só quando está em `wait` e liberada. Em ordem estrita, uma US só pode depender de outra que vem antes dela.
- `--ordem livre`: qualquer US em `wait` liberada é elegível; `us` é a primeira na ordem e `elegiveis` lista todas. As que esperam dependência aparecem em `bloqueadas`, com o motivo.
- Onda encerrada ou concluída não tem próxima.

Exemplo, a onda do colaboradados:

```bash
orq coord registrar colaboradados --projeto colaboradados
orq onda criar "Melhorias da noite" --us 215,216,217,218 --ordem estrita --depende 216:215
orq onda proxima          # TG-215
orq us status TG-215 exec --pane wB:p3 --papel implementacao --rodada 1
orq pend add TG-215 --tipo autorizacao "aplicar a migration no banco de homologação"   # TG-215#P1
orq mr add TG-215 https://gitlab.basis.com.br/basis/colaboradados/api/-/merge_requests/41
orq us status TG-215 done
orq onda proxima          # TG-216
```
