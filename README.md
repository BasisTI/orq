# orq

Central de US para o orquestrador multiagente: CLI `orq` e plugin do [Herdr](https://github.com/ogulcancelik/herdr) que consolidam, por coordenador, as ondas de User Stories, o status de cada uma, os agentes, as merge requests e as pendências.

Desenho e entregas: projeto `orq` no Taiga (https://agile.basis.com.br/project/orq).

Esta versão traz o estado da Central num SQLite, os comandos de registro e o daemon que consulta o Herdr, as forjas (GitLab e GitHub) e o Taiga e publica o resumo na sidebar do Herdr. O TUI e o plugin chegam nas próximas entregas.

## Instalação

```bash
go install github.com/BasisTI/orq/cmd/orq@latest
orq version
```

Precisa de Go 1.27 ou mais novo. Não usa cgo.

## Estado

O banco fica em `$ORQ_STATE_DIR/central.db`; sem a variável, em `$XDG_STATE_HOME/orq/central.db` e, sem ela, em `~/.local/state/orq/central.db`. Ele roda em modo WAL: vários coordenadores, em processos diferentes, escrevem ao mesmo tempo, e cada escrita espera a vez por até 15 s. A abertura (criação, ativação do WAL e migrations) é serializada por um `flock` em `central.db.init.lock`, no mesmo diretório.

## Saída e erros

- `--output json|text`. Sem a flag, texto num terminal e JSON fora dele. `orq snapshot` sai sempre em JSON.
- Erro vai para o stderr como `{"error":{"code","cause","recovery"}}` (ou `erro [code]: cause` em texto).
- `banco_ocupado` (exit 4) separa os dois casos no `recovery`: outro processo segurou o banco por mais de 15 s, ou o SQLite recusou a trava na hora (o que não devia acontecer; reporte).
- Códigos de saída: `0` ok, `1` inesperado, `2` uso (inclui `ref_ambigua` e `coord_nao_resolvido`), `4` conflito (`transicao_invalida`, `us_em_outra_onda`, `onda_encerrada`, `pendencia_resolvida`, `banco_ocupado`), `5` não encontrado.

## Coordenador corrente

`orq coord registrar` grava o pane do coordenador (`$HERDR_PANE_ID` por padrão). Os outros comandos acham o coordenador corrente, nesta ordem, por `--coord <nome>`, por `$ORQ_COORD` ou pelo `$HERDR_PANE_ID` registrado. Criar onda e `onda ver`/`proxima` sem id exigem coordenador; as escritas numa US funcionam sem ele e assinam o evento com o dono da onda.

Em `onda list` e `pend list`, `--coord` filtra pelo coordenador dono da onda; em `diario`, pelo coordenador que assinou o evento.

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
| `orq onda encerrar <id>` | Encerra a onda; as US ficam como estão e podem entrar numa onda nova (veja abaixo o que acontece com o status). |
| `orq us status <ref> <wait\|plan\|exec\|rev\|done> [--pane <id>] [--papel planejamento\|implementacao\|revisao] [--rodada N] [--forcar]` | Muda o status. Transições livres, inclusive idas e voltas entre `exec` e `rev`; sair de `done` exige `--forcar`. Sem mudança, não grava nem gera evento. |
| `orq mr add <ref> <url>` / `orq mr rm <ref> <url>` | Liga ou desliga a MR da US. A mesma URL de novo não muda nada. |
| `orq pend add <ref> --tipo duvida\|autorizacao\|decisao "<texto>" [--pane <id>]` | Abre a pendência e imprime o id (`TG-312#P2`). |
| `orq pend resolver <ref>#P<n> [--resposta "…"]` | Resolve a pendência. |
| `orq pend list [--abertas] [--coord X] [--onda N]` | Lista as pendências. |
| `orq diario [--us <ref>] [--onda N] [--coord X] [--limite 50]` | Eventos gravados, do mais antigo ao mais novo (toda escrita gera um). |
| `orq snapshot [--incluir-encerradas] [--json]` | Estado consolidado em JSON versionado (`--json` é aceito, mas a saída já é sempre JSON); contrato em [`docs/snapshot.md`](docs/snapshot.md). |
| `orq atualizar [--fonte herdr\|mr\|taiga] [--forcar]` | Consulta as fontes uma vez (todas, sem `--fonte`; repetível ou `mr,taiga`), grava o cache e, com o Herdr entre elas, confere os tokens de sidebar. Cada chave respeita o TTL; `--forcar` ignora o TTL e a suspensão por autenticação. |
| `orq daemon [--tick 5s] [--intervalo-remoto 15s]` | Mantém o cache e os tokens vivos, em primeiro plano, até SIGTERM/SIGINT. Um por máquina. |
| `orq version` | Versão. |

### Daemon e enriquecedores

`orq daemon` roda em primeiro plano (o plugin do Herdr o inicia; o log vai para o stderr) e só existe um por diretório de estado: ele segura um `flock` em `daemon.lock`, e o segundo sai com `daemon_ativo` (exit 4) dizendo o pid do primeiro. A cada tick ele lê o Herdr (`herdr api snapshot`) e confere os tokens; MR e Taiga são olhados a cada `--intervalo-remoto`, cada chave com o seu TTL (MR 60 s, Taiga 5 min). Ele também assina `pane.agent_status_changed` dos panes das ondas abertas no socket do Herdr (`$HERDR_SOCKET_PATH`, a sessão de `$HERDR_SESSION` ou `~/.config/herdr/herdr.sock`) e republica na hora; em `events_lost`, reassina e reconcilia com uma leitura completa. Ao sair, limpa os tokens que publicou.

Só leituras nas forjas e no Taiga. Falha de uma consulta (binário ausente, autenticação, rede) fica no cache com o código e o snapshot mostra o último valor bom com o `erro`; nenhuma derruba o processo. Erro de autenticação suspende a ferramenta por 30 min: o orq não tenta login nem define token.

As ferramentas são chamadas por caminho absoluto, porque num pane ou numa ação de plugin o PATH não tem `~/.local/bin`. A ordem: `ORQ_HERDR_BIN`, `ORQ_GLAB_BIN`, `ORQ_GH_BIN` e `ORQ_TAIGA_BIN`; para o herdr, `$HERDR_BIN_PATH`; e então `~/.local/bin`, `~/go/bin`, `~/bin`, `/home/linuxbrew/.linuxbrew/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin` e `/bin`. O PATH do processo não entra.

Tokens de sidebar (`herdr pane|workspace report-metadata --source orq`), conferidos a cada tick contra o que o Herdr mostra — os tokens somem quando o servidor do Herdr reinicia, e o daemon republica:

- `orq`, no pane de cada coordenador: as US das ondas abertas por status e as pendências abertas, ex. `2 Exec·1 Rev·1 pend`, com `⚠ ` na frente quando algum pane de US está `blocked`. Até 80 caracteres.
- `mrs_prontas`, no workspace do coordenador (o registrado ou o do pane dele): as MRs `pronta`, `!32` no GitLab e `skills#29` no GitHub; o que não cabe em 80 caracteres vira `+N`.

Só pane e workspace que existem recebem token, e o que deixou de valer é limpo (`--clear-token`).

### Ondas e a próxima US

A onda declara até onde vai (`--ate`) e a ordem. `done` numa US quer dizer que ela chegou a esse limite: plano aprovado (`plano`), MR pronta para revisão humana, sem merge (`mr-pronta`), ou MR mesclada (`merge`). `orq onda proxima` devolve a autorização e o texto do limite junto com a US, para quem despacha saber até onde ir.

- Uma US está liberada quando todas as dependências estão em `done`.
- `--ordem estrita`: uma US de cada vez, na ordem. Só a primeira US ainda não concluída é candidata, e só quando está em `wait` e liberada. Em ordem estrita, uma US só pode depender de outra que vem antes dela.
- `--ordem livre`: qualquer US em `wait` liberada é elegível; `us` é a primeira na ordem e `elegiveis` lista todas. As que esperam dependência aparecem em `bloqueadas`, com o motivo.
- Onda encerrada ou concluída não tem próxima.
- Uma US de onda encerrada pode entrar numa onda nova. Se o limite (`--ate`) é o mesmo, ela guarda o status. Se é outro, volta a `wait`, porque o `done` valia para o limite anterior: o plano aprovado de uma onda `plano` não é a MR pronta de uma onda `mr-pronta`. O diário registra um `us.status` com `de`, `para` e `motivo`. É o fluxo planejamento → implementação → merge.

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
