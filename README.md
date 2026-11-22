# orq

Central de US para o orquestrador multiagente: CLI `orq` e plugin do [Herdr](https://github.com/ogulcancelik/herdr) que consolidam, por coordenador, as ondas de User Stories, o status de cada uma, os agentes, as merge requests e as pendências.

Desenho e entregas: projeto `orq` no Taiga (https://agile.basis.com.br/project/orq).

Esta versão traz o estado da Central num SQLite, os comandos de registro, o daemon que consulta o Herdr, as forjas (GitLab e GitHub) e o Taiga e publica o resumo na sidebar do Herdr, o TUI (`orq tui`), o workspace Central (`orq central abrir`) e o plugin do Herdr.

## Instalação

Como plugin do Herdr (sobe o daemon, traz as ações, o popup e o link handler):

```bash
herdr plugin install BasisTI/orq     # clona, roda scripts/build-plugin.sh (go build) e registra
orq plugin configurar                # linhas $orq/$orq_mrs da sidebar e as teclas, com backup e herdr config check
```

O `orq` do segundo comando pode ser o do plugin (`$(herdr plugin list --json | jq -r '.result.plugins[] | select(.plugin_id=="orq") | .plugin_root')/bin/orq`) ou um instalado à parte:

```bash
go install github.com/BasisTI/orq/cmd/orq@latest
orq version
```

Precisa de Go 1.27 ou mais novo, também para o build do plugin. Não usa cgo. O build do plugin não recebe o PATH do plugin: `scripts/build-plugin.sh` procura o `go` no PATH e, depois, em `/usr/local/go/bin`, `~/go/bin`, `~/.local/go/bin`, `~/sdk/go/bin`, `/home/linuxbrew/.linuxbrew/bin`, `/opt/homebrew/bin`, `/usr/lib/go/bin` e `/usr/bin`; `ORQ_GO` aponta outro.

### Atualização

O `orq` do `PATH` (o do `go install`) e o do plugin do Herdr compartilham o mesmo banco, e uma migration é de mão única: quem abre o banco migra. Um comando atualiza tudo:

```bash
orq upgrade [--ref main]
```

Ele roda `go install github.com/BasisTI/orq/cmd/orq@<ref>` (só quando existe um `orq` do `go install` que não é o mesmo arquivo que o do plugin) e `herdr plugin install BasisTI/orq --ref <ref> --yes`, e então reinicia o daemon: confere que o pid da trava é o `orq daemon` deste diretório de estado (mesmo usuário, executável `orq`, ambiente e dono da `flock`; senão recusa com `daemon_alheio` e não sinaliza nada), manda `SIGTERM` ao antigo, espera a trava `daemon.lock` soltar e sobe o novo com o mesmo ambiente e os mesmos `--tick` e `--intervalo-remoto`. No fim, confere que cada binário encontrado (`PATH`, `go install`, plugin) e o daemon em execução estão no mesmo commit; o que ficou para trás sai nomeado, com o motivo e se ainda abre o banco, e o comando termina com `upgrade_incompleto` (exit 4).

- Uma falha no `go install` ou no plugin não impede a outra etapa. Se nenhum binário atualizou, o daemon nem é tocado; se algum atualizou, o daemon sobe a partir dele. O daemon novo só conta como no ar depois de abrir o banco e achá-lo compatível (o `--detach` espera esse aviso, e não só a trava `daemon.lock`); se ele sai antes, o upgrade mostra o erro dele e o estado final. Se o daemon novo não sobe, o comando volta ao daemon anterior (guarda uma cópia do binário dele durante a troca). O daemon que não solta a trava em 30 s fica como está: o upgrade não usa `SIGKILL`. Nada disso deixa o daemon parado, a não ser que nem o anterior volte, e então a etapa `daemon` diz `daemon PARADO`.
- Rodar de novo só repete o que falta: o daemon que já roda a versão nova não é reiniciado.
- Um `orq` do `PATH` que não é o do `go install` nem o do plugin não é atualizado, mas entra na conferência.
- Os panes `orq tui` já abertos seguem com o binário antigo. O Herdr 0.9.3 não tem como reabrir um pane com outro comando, então o upgrade lista os pids e pede para fechá-los e abri-los de novo (`orq central abrir`).
- O comando não abre o banco, então funciona mesmo quando o `orq` instalado recusa o banco com `versao_do_banco`. Não reinicia nem para o servidor do Herdr.
- `orq version --output json` traz `versao`, `commit` e `schema`; é o que o upgrade lê de cada binário. O build do plugin (`scripts/build-plugin.sh`) grava o commit; o `go install` o traz na pseudo-versão.

#### Versão mínima de leitor

A versão de um `orq`, para ler o banco, é a última migration que ele embute (`schema`). Cada migration declara, em comentários do próprio `.sql`, a menor versão que ainda usa o banco migrado por ela:

```sql
-- orq:leitor-minimo 4
-- orq:escritor-minimo 5
```

`leitor-minimo` é a menor versão que abre o banco e lê; `escritor-minimo`, a que também escreve. A diretiva ocupa a linha inteira: depois do número só cabe o fim da linha (o parser lê o resto dela como número), então não ponha comentário ali.

Sem diretiva, vale o número da própria migration (só abre quem a tem, como antes); `escritor-minimo` sem declaração também vale o número dela, de modo que declarar só o leitor deixa o binário anterior **ler, mas não escrever**. O banco guarda o maior valor de cada um entre as migrations aplicadas, em `banco_meta` (`leitor_minimo`, `escritor_minimo`), criada pela migration 0004 (que declara 4 e 4). Um `orq` abre um banco mais novo que ele, sem migrar, quando o `schema` dele é maior ou igual ao `leitor_minimo`: para escrever, também ao `escritor_minimo`; abaixo disso as escritas saem com `banco_somente_leitura` (exit 4). Abaixo do `leitor_minimo`, ou em banco mais novo sem `banco_meta` (anterior à 0004), o `orq` recusa com `versao_do_banco`. Os dois erros apontam para `orq upgrade`. A conferência não vale só na abertura: toda transação, de leitura ou de escrita, revalida `user_version` e `banco_meta` (a de escrita depois de tomar a trava), porque outro processo pode migrar o banco enquanto este `orq` segue aberto. Uma conexão que perdeu o direito de escrever recebe `banco_somente_leitura`; a que perdeu o de ler, `versao_do_banco`. Os binários anteriores à 0004 não conhecem nada disso e recusam qualquer banco mais novo que o deles.

**Daemon.** O daemon fica aberto o tempo todo e confere o banco a cada ciclo. Se outro processo migrar o banco para além do que ele escreve (`banco_somente_leitura`) ou lê (`versao_do_banco`), ele registra `orq daemon sai por erro (<código>)` no log, solta a trava e sai com exit 4, em vez de ficar vivo sem escrever; `orq upgrade` sobe o daemon da versão nova.

**Depois do merge da #15 (migration 0005), rode `orq upgrade`.** A 0005 declara `leitor-minimo 4` e `escritor-minimo 5`: o `orq` 4 (inclusive o daemon do plugin) ainda lê o banco, mas não escreve. Um daemon 4 diante do banco 5 sai com `banco_somente_leitura` (exit 4), e o `--detach` dele falha com `daemon_nao_subiu`; sem o `upgrade`, o daemon não importa as pendências nem avisa ninguém.

A 0010 (US #100, `espera_motivo` em `us`) também só acrescenta uma coluna nula e declara `leitor-minimo 4` e `escritor-minimo 5`: o `orq` 5 em diante segue escrevendo sem tocar na coluna. Sem upgrade, um binário anterior não mostra o motivo.

A 0007 (US #81, `modelo` e `esforco` em `us`) também só acrescenta colunas nulas e declara `leitor-minimo 4` e `escritor-minimo 5`: o `orq` 5 e o 6 seguem escrevendo (sem tocar nas colunas, então o modelo gravado fica como estava até um `orq` 7 mudar o status), e não exige `orq upgrade` imediato. Sem upgrade, o daemon antigo não publica o token do modelo.

Antes de aplicar uma migration, o `orq` consulta `version` dos outros binários conhecidos (`PATH`, `go install`, plugin e o daemon em execução) e avisa no stderr os que deixam de abrir o banco, ou passam a abri-lo só para leitura, depois dela.

## Estado

O banco fica em `$ORQ_STATE_DIR/central.db`; sem a variável, em `$XDG_STATE_HOME/orq/central.db` e, sem ela, em `~/.local/state/orq/central.db`. Ele roda em modo WAL: vários coordenadores, em processos diferentes, escrevem ao mesmo tempo, e cada escrita espera a vez por até 15 s. A abertura (criação, ativação do WAL e migrations) é serializada por um `flock` em `central.db.init.lock`, no mesmo diretório.

## Saída e erros

- `--output json|text`. Sem a flag, texto num terminal e JSON fora dele. `orq snapshot` sai sempre em JSON.
- Erro vai para o stderr como `{"error":{"code","cause","recovery"}}` (ou `erro [code]: cause` em texto).
- `banco_ocupado` (exit 4) separa os dois casos no `recovery`: outro processo segurou o banco por mais de 15 s, ou o SQLite recusou a trava na hora (o que não devia acontecer; reporte).
- Códigos de saída: `0` ok, `1` inesperado, `2` uso (inclui `ref_ambigua` e `coord_nao_resolvido`), `4` conflito (`transicao_invalida`, `us_em_outra_onda`, `us_ja_na_onda`, `onda_encerrada`, `pendencia_resolvida`, `banco_ocupado`), `5` não encontrado.

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
| `orq onda criar <titulo> --nome <nome> --us 215,216,217,218 [--projeto <slug>] [--ate plano\|mr-pronta\|merge] [--ordem estrita\|livre] [--depende 216:215]` | Cria a onda com as US em `wait`, na ordem dada. `--nome` (obrigatório) é o nome da onda, validado como nome de agente do Herdr (`[a-z][a-z0-9_-]{0,31}`), gravado na coluna `onda.nome` (migration 0009) e devolvido no campo `nome` do JSON e do snapshot. `--ate` padrão `mr-pronta`, `--ordem` padrão `livre`. `--depende` é repetível e aceita `218:216+217`. US sem projeto usam `--projeto` ou o projeto padrão do coordenador. Depois de gravar, o orq renomeia o pane do coordenador (ver [Nome do pane do coordenador](#nome-do-pane-do-coordenador)). |
| `orq onda adicionar <id> <ref>… [--apos <ref>] [--projeto <slug>]` | Inclui US numa onda aberta, no fim ou logo depois de `--apos` (que precisa estar na onda); a US entra em `wait`, ou guarda o status se veio de onda encerrada de mesmo limite (com limite diferente volta a `wait`); grava `onda.adicionar` no diário. Mesma regra do `onda criar` para quem já tem onda: onda aberta recusa (`us_em_outra_onda`; `us_ja_na_onda` se já está nesta), onda encerrada cede a US. Onda encerrada não recebe (`onda_encerrada`). Não cria dependências. `onda proxima` e a regra de encerramento passam a contar a US nova. Sem coordenador corrente, assina o dono da onda. |
| `orq onda ver [<id>]` | Onda com US, dependências, MRs e pendências. Sem id, a única onda aberta do coordenador. |
| `orq onda list [--abertas]` | Lista as ondas. |
| `orq onda proxima [<id>]` | Próxima US em `wait` que pode ser despachada; sem nada elegível, `us` vem nulo e `motivo` diz por quê. Com todas as US no limite, `encerramento` diz o próximo passo (veja [Encerrar a onda](#encerrar-a-onda)). |
| `orq onda encerrar <id>` | Encerra a onda; as US ficam como estão e podem entrar numa onda nova (veja abaixo o que acontece com o status). Avisa no stderr, uma linha por pendência ainda aberta ou só respondida da onda (chave e estado, sem o texto); não resolve nada nem muda o exit. |
| `orq us espera <ref> "<motivo>" \| --limpar` | Registra, troca ou limpa o motivo da espera da US (texto curto, numa linha, até 200 caracteres), para o bloqueio externo que o coordenador conhece e a Central não mostrava. Aparece na lista de US da TUI e da web, depois da coluna da MR. Não muda o status, mas sair de `wait` (`orq us status <ref> exec` etc.) limpa o motivo, com um `us.espera` (`limpou`, `motivo_antes`) na mesma transação; fora isso, quem registra limpa quando a espera acaba. O diário ganha `us.espera` (`motivo`, `motivo_antes`, `limpou`). Texto igual ao gravado não grava nem gera evento. |
| `orq us status <ref> <wait\|plan\|exec\|rev\|done> [--pane <id>] [--papel planejamento\|implementacao\|revisao] [--rodada N] [--modelo <id>] [--esforco <nível>] [--forcar]` | Muda o status. `--modelo` e `--esforco` registram o modelo do pane atual (id inteiro, ex. `claude-fable-5-2`, e nível, ex. `medium`); a troca vai para o diário (`modelo`, `modelo_antes`). Transições livres, inclusive idas e voltas entre `exec` e `rev`; sair de `done` exige `--forcar`. Sem mudança, não grava nem gera evento. Ao mudar para `done`, avisa no stderr das pendências abertas da US, como o `onda encerrar`. |
| `orq mr add <ref> <url>` / `orq mr rm <ref> <url>` | Liga ou desliga a MR da US. A mesma URL de novo não muda nada. `--precondicao "Mesclar depois de orq#27"` registra já a pré-condição. |
| `orq mr precondicao <ref> <url> "<texto>"` / `--limpar` | Registra ou limpa a pré-condição da MR em Draft. A Central a mostra como **Draft com pré-condição**, distinta de pronta e de Draft comum (migration 0006, evento `mr.precondicao`). O texto só aparece enquanto o estado é `draft`; o mesmo texto de novo não gera evento. |
| `orq pend importar <arquivo>` | Importa o arquivo de pendência (veja [Pendências com contexto](#pendências-com-contexto-e-resposta-pela-web)), como o daemon faz a cada 5 s; para quando o daemon está parado. |
| `orq pend ver <ref>#P<n>` | A pendência inteira: detalhe, opções, resposta e quem a deu, histórico e origem. Aceita `projeto/312#P2`. |
| `orq pend responder <ref>#P<n> [--opcao N] ["texto"]` | A resposta da pessoa pelo chat, igual à da web, com `respondida_por = chat`. Só o coordenador usa. |
| `orq pend resolver <ref>#P<n> [--resposta "…"]` | Resolve a pendência. Sem `--resposta`, vale a que a pessoa registrou (o texto dela ou `opção N (título)`). |
| `orq pend list [--abertas\|--respondidas\|--resolvidas] [--coord X] [--onda N] [--projeto P]` | Lista as pendências. `--abertas` inclui as respondidas que ainda não foram resolvidas. |
| `orq pend add <ref> --tipo duvida\|autorizacao\|decisao "<texto>" [--pane <id>]` | Abre uma pendência só com o título, sem detalhe (o caminho antigo; o normal é o arquivo) e imprime o id (`TG-312#P2`). Com `--tipo validacao` não leva texto: abre a validação da spec (veja [Specs](#specs-registro-validação-e-conferência)). |
| `orq spec registrar <ref> --caminho <pasta> --mr <url> [--substituir]` | Registra a pasta e a MR da spec da US e liga a MR à US, numa transação (veja [Specs](#specs-registro-validação-e-conferência)). |
| `orq spec conferir <ref>` | Só leitura: diz se a MR da spec pode ser mesclada (só o `decisoes.md` mudou na pasta desde a aprovação). |
| `orq spec link gerar <ref> --para <rótulo> [--expira 30d] [--url-publica <base>]` | Gera um link público, só leitura, para a spec da US; a URL sai uma única vez (veja [Link público da spec](#link-público-da-spec)). |
| `orq spec link listar <ref>` | Lista os links: rótulo, criação, validade, estado, último acesso e acessos. |
| `orq spec link revogar <ref> <id>` | Revoga o link, de forma definitiva. |
| `orq msg ver <id>` | Mostra a mensagem enviada pela web ao coordenador e a resposta (se houver). |
| `orq msg responder <id> ["texto"] [--arquivo <caminho>]` | Registra a resposta única do coordenador (até 64 KiB, aceita markdown; `--arquivo -` lê de `stdin`). |
| `orq msg list [--onda <id>] [--abertas]` | Lista as mensagens em ordem decrescente de envio. |
| `orq diario [--us <ref>] [--onda N] [--coord X] [--limite 50]` | Eventos gravados, do mais antigo ao mais novo (toda escrita gera um). |
| `orq snapshot [--incluir-encerradas] [--json]` | Estado consolidado em JSON versionado (`--json` é aceito, mas a saída já é sempre JSON); contrato em [`docs/snapshot.md`](docs/snapshot.md). |
| `orq atualizar [--fonte herdr\|mr\|taiga] [--forcar]` | Consulta as fontes uma vez (todas, sem `--fonte`; repetível ou `mr,taiga`), grava o cache e, com o Herdr entre elas, confere os tokens de sidebar. Cada chave respeita o TTL; `--forcar` ignora o TTL e a suspensão por autenticação. |
| `orq daemon [--tick 5s] [--intervalo-remoto 15s] [--detach] [--web unix:<caminho>\|127.0.0.1:<porta>] [--web-url URL] [--web-cabecalho H]` | Mantém o cache e os tokens vivos, em primeiro plano, até SIGTERM/SIGINT. Um por máquina. Importa as pendências e avisa o coordenador; com `--web`, serve a página das pendências. `--detach` sobe o daemon noutro processo (sessão própria, log em `daemon.log` no diretório de estado), espera ele abrir o banco e sai (se o filho sai antes, falha com o erro dele); com um daemon já no ar, só diz o pid dele. |
| `orq tui [--coord X \| --todos] [--intervalo 2s]` | A Central no terminal (veja "TUI"). |
| `orq central abrir [--nome Central] [--sem-foco]` | Cria ou reaproveita o workspace Central com um pane `orq tui --coord X` por coordenador com onda aberta. |
| `orq focar [<pane>\|<url>]` | `herdr agent focus` no pane, dado pelo id ou por `https://orq.local/pane/<id>`; sem argumento, lê `$HERDR_PLUGIN_CLICKED_URL` (a ação `focar-pane`). |
| `orq plugin configurar [--tecla prefix+u] [--tecla-central prefix+shift+u] [--config <arquivo>]` | Acrescenta ao `config.toml` do Herdr as linhas da sidebar e as teclas do plugin. |
| `orq plugin desconfigurar [--config <arquivo>]` | Tira do `config.toml` só o que `configurar` acrescentou. |
| `orq plugin doctor [--config <arquivo>]` | Binários, daemon, plugin registrado, sidebar, teclas e banco. |
| `orq recursos gravar <ref> [--repo R --worktree W --branch B] [--pane NOME]… [--aprovado URL=SHA]…` | Grava (ou completa) o `recursos.json` da US na pasta de trabalho e imprime o `TMPDIR` do worker e os rótulos Docker (veja [Limpeza depois do merge](#limpeza-depois-do-merge)). |
| `orq recursos ver <ref>` | Mostra o `recursos.json` da US. |
| `orq recursos reabrir <ref>` | Abre a rodada de correção depois do teste: sobe a geração do `recursos.json` e guarda as MRs de agora como encerradas. Rode **antes** de recriar o worktree. |
| `orq limpar <ref> [--dry-run] [--forcar]` | Com todas as MRs da US mescladas, fecha os panes, remove o worktree, as branches, as pastas e os recursos Docker da US; o que não passa numa conferência fica preservado, com o motivo. `--dry-run` só mostra. Recusa se o `recursos.json` não registra repo nem pane; `--forcar` segue (não vale com `--onda`). |
| `orq limpar --onda <id> [--dry-run]` | Remove os recursos Docker com o rótulo `orq.onda=<id>` de uma onda encerrada ou pronta para encerrar. |
| `orq upgrade [--ref main]` | Atualiza o `orq` do `go install` e o do plugin, reinicia o daemon e confere que todos estão no mesmo commit (veja [Atualização](#atualização)). |
| `orq version` | Versão, commit e `schema` (a última migration que o binário embute). |

### Daemon e enriquecedores

`orq daemon` roda em primeiro plano (o log vai para o stderr; o plugin do Herdr o inicia com `--detach`, e o log vai para `daemon.log` no diretório de estado) e só existe um por diretório de estado: ele segura um `flock` em `daemon.lock`, e o segundo sai com `daemon_ativo` (exit 4) dizendo o pid do primeiro. A cada tick ele lê o Herdr (`herdr api snapshot`) e confere os tokens; MR e Taiga são olhados a cada `--intervalo-remoto`, cada chave com o seu TTL (MR 60 s, Taiga 5 min). Ele também assina `pane.agent_status_changed` dos panes das ondas abertas no socket do Herdr (`$HERDR_SOCKET_PATH`, a sessão de `$HERDR_SESSION` ou `~/.config/herdr/herdr.sock`) e republica na hora; em `events_lost`, reassina e reconcilia com uma leitura completa. Ao sair, limpa os tokens que publicou e que ainda têm o valor dele.

Só leituras nas forjas e no Taiga (a limpeza automática do merge é a exceção; veja [Limpeza depois do merge](#limpeza-depois-do-merge)). Falha de uma consulta (binário ausente, autenticação, rede) fica no cache com o código e o snapshot mostra o último valor bom com o `erro`; nenhuma derruba o processo. Erro de autenticação (na taiga-cli, o exit 3) suspende a ferramenta por 30 min: o orq não tenta login nem define token. `--forcar` passa por cima de uma suspensão anterior, mas uma recusa na própria execução interrompe as chamadas seguintes da ferramenta.

As ferramentas (`herdr`, `glab`, `gh`, `taiga`, `git`, `docker` e `go`) são chamadas por caminho absoluto, porque num pane ou numa ação de plugin o PATH não tem `~/.local/bin`. A ordem: `ORQ_<NOME>_BIN` (`ORQ_HERDR_BIN`, `ORQ_GLAB_BIN`, `ORQ_GH_BIN`, `ORQ_TAIGA_BIN`, `ORQ_GIT_BIN`, `ORQ_DOCKER_BIN`, `ORQ_GO_BIN`); para o herdr, `$HERDR_BIN_PATH`; e então, na ordem, `~/.local/bin`, `~/go/bin`, `~/bin`, os diretórios do Nix (`~/.nix-profile/bin`, `/etc/profiles/per-user/$USER/bin`, `/nix/var/nix/profiles/default/bin` e `/run/current-system/sw/bin`), `/home/linuxbrew/.linuxbrew/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin` e `/bin`. O `$USER` do diretório per-user vem de `USER` ou, sem ele, do uid. O PATH do processo não entra. Ferramenta que não está em nenhum deles dá `binario_ausente`, e a mensagem lista os diretórios tentados.

**Uma configuração só para o daemon e o coordenador.** O `orq.env` (veja [Plugin do Herdr](#plugin-do-herdr)) vale também para os comandos do terminal (`orq limpar`, `orq recursos`, `orq plugin doctor`…), não só para o daemon, as ações e os panes. Fora do plugin, `HERDR_PLUGIN_CONFIG_DIR` não está definido, e os comandos derivam o diretório sem chamar o Herdr, pela mesma regra do `herdr plugin config-dir orq` no Herdr 0.9.3:

1. `$HERDR_PLUGIN_CONFIG_DIR`, quando o Herdr o define;
2. `$XDG_CONFIG_HOME/herdr/plugins/config/orq`, quando `XDG_CONFIG_HOME` é um caminho absoluto;
3. `$HOME/.config/herdr/plugins/config/orq`.

`HERDR_CONFIG_PATH` só move o `config.toml` e não conta. (O Herdr aceita `XDG_CONFIG_HOME` vazio ou relativo e devolve então um caminho relativo ao diretório corrente; o orq ignora os dois e usa o `HOME`.) A precedência não muda: o ambiente do processo vence o arquivo, e o arquivo só fornece chaves `ORQ_`. `orq plugin doctor` mostra, para as sete ferramentas, de onde veio o caminho (`variavel`, `orq.env`, `HERDR_BIN_PATH` ou `diretorio`) e compara o que o daemon e o shell atual resolvem, nas ferramentas e em quatro opções: `ORQ_STATE_DIR`, `ORQ_TRABALHO_DIR`, `ORQ_TMP_DIR` e `ORQ_LIMPEZA_AUTOMATICA` (valor efetivo e origem — processo, `orq.env` ou padrão — dos dois lados; diferença é aviso, porque o daemon e o coordenador precisam das mesmas raízes). Com um daemon ativo do mesmo usuário (pid da `daemon.lock`, confirmado como o `orq upgrade` confirma: executável `orq daemon`, dono da flock), o doctor lê de `/proc/<pid>/environ` só `ORQ_*`, `HOME`, `USER`, `HERDR_BIN_PATH`, `HERDR_PLUGIN_CONFIG_DIR`, `XDG_CONFIG_HOME` e `XDG_STATE_HOME` e resolve com elas, mais o `orq.env`, o que o daemon usa. Sem daemon ou sem ambiente legível, o item `daemon e shell` diz que é uma simulação (só o `orq.env` fornece `ORQ_*` ao daemon) e que o daemon em execução não foi conferido; ele nunca vira `ok` assim. Se o `orq.env` é mais novo que o daemon, o item `orq.env e daemon` avisa que é preciso reiniciá-lo, porque ele só lê o arquivo ao subir. Ferramenta que nenhum dos lados resolve também é aviso, com o nome dela.

Tokens de sidebar (`herdr pane|workspace report-metadata --source orq`), conferidos a cada tick contra o que o Herdr mostra — os tokens somem quando o servidor do Herdr reinicia, e o daemon republica:

- `orq`, no pane de cada coordenador: as US das ondas abertas por status e as pendências abertas, ex. `2 Exec·1 Rev·1 pend`, com `⚠ ` na frente quando algum pane de US está `blocked`. Até 80 caracteres.
- `orq_mrs`, no workspace do coordenador (o registrado ou o do pane dele): as MRs `pronta`, `argocd-apps!32` no GitLab e `skills#29` no GitHub (sempre com o projeto, o da URL da MR); o que não cabe em 80 caracteres vira `+N`.

Os nomes `orq` e `orq_mrs` são do orq; `mrs_prontas`, `story` e `fase` são da publicação manual da skill do orquestrador (`--source orquestrador`), que convive com o daemon até ser aposentada. O plugin configura as linhas da sidebar com os nomes do orq.

Só pane e workspace que existem recebem token. No Herdr o token é por nome, não por fonte, então a propriedade vem do próprio orq: ele registra um token só depois de publicá-lo com sucesso, e o token continua sendo dele enquanto o Herdr mostrar o último valor que ele publicou. Um token ausente é (re)publicado. Um valor de outro produtor no mesmo nome não é adotado, sobrescrito nem limpo; vira aviso (`avisos` em `orq atualizar`, uma linha no log do daemon). O que deixou de valer só é limpo (`--clear-token`) se ainda tem o valor do orq; na saída, o daemon relê o Herdr antes de limpar.

### Specs: registro, validação e conferência

A spec de uma US é uma pasta `docs/specs/<épico>/tg-<ref>-<nome-curto>/` (`spec.md`, `plano.md`, `decisoes.md`) numa MR só dela. O orq guarda a pasta e a MR e acompanha o estado.

```bash
orq spec registrar TG-59 --caminho docs/specs/epico/tg-59-nome --mr https://github.com/BasisTI/orq/pull/44
```

- O repositório sai da URL da MR (não há `--repo`); o mesmo comando liga a MR à US, como `orq mr add`, na mesma transação. O orq usa a pasta registrada e nunca a deduz pelo nome. `--substituir` também invalida a validação anterior: só vale validação aberta depois do último registro.
- Uma spec por US. Com spec já registrada o comando recusa (`spec_ja_registrada`, exit 4) e mostra o registro atual: quem chama avisa o responsável e pergunta se substitui, e só com o sim repete com `--substituir`. O evento `spec.registrar` guarda os valores anteriores e os novos.

**Validação.** Pendência de tipo `validacao`, pelo arquivo (`tipo: validacao`, que não exige `titulo` nem `opcoes`) ou por `orq pend add TG-59 --tipo validacao`. O orq impõe o título "Spec da TG-59 pronta para validar" e as opções **Aprovar** e **Pedir ajustes** (nenhuma recomendada), seja qual for o conteúdo do arquivo, e grava na importação o **SHA da cabeça da MR da spec**; o coordenador não escreve o SHA, e a pendência segue presa a ele mesmo que a cabeça ande. Sem spec registrada, o arquivo é recusado (`.erro.txt`) e a CLI devolve erro de uso. Com a forja fora do ar (ou sem `gh`/`glab`), a pendência de arquivo fica **retida**, sem aparecer na Central e sem `.erro.txt`, e o daemon tenta de novo a cada ciclo; a CLI devolve `forja_indisponivel` na hora. MR que a forja diz não existir (404) é recusa com `.erro.txt`, não retenção.

**Conferência.** `orq spec conferir TG-59` compara o **conteúdo da pasta registrada** entre o SHA da validação aprovada e a cabeça atual da MR (árvores lidas pela API da forja, só leitura) e responde "pode mesclar" quando só `<pasta>/decisoes.md` mudou; senão lista os arquivos da pasta que mudaram. Arquivo alterado fora da pasta (o compare da forja) é outro sinal, "a MR não é só da spec", e não reprova a spec: um rebase ou o merge da base na branch traz arquivos de fora sem mudar o conteúdo da pasta. Pasta que não existe, não tem arquivos ou é um arquivo, no SHA aprovado ou na cabeça, é erro (`pasta_da_spec_ausente`, exit 4), nunca "pode mesclar": confira o `--caminho` e, com o sim da pessoa, `orq spec registrar --substituir`. Sem validação aprovada, diz isso. O daemon repete a conferência a cada ciclo (só relê as árvores quando a cabeça da MR anda) e guarda o resultado em `spec_conferencia`, que é cache: não gera evento.

**Estado.** O snapshot traz `spec` por US (`docs/snapshot.md`) e o card da US na Central mostra o selo com um de cinco estados: *registrada*, *em validação* (com o SHA), *aprovada* (com o SHA aprovado), *mudou além do decisoes.md* e *mesclada*. O TUI não mostra a spec.

**Leitura e página da spec** (US #60). O daemon lê os arquivos da spec registrada **pela API da forja** (`gh api`/`glab api`, só leitura, caminho absoluto, sem `git fetch` nem checkout) e guarda o conteúdo no `central.db` (cache: sem evento no diário). A ref é a **branch de origem** da MR da spec enquanto ela está aberta e a **branch alvo** depois de mesclada, para todos os arquivos, inclusive o `decisoes.md`. Lê só a allowlist: os `.md` diretamente na pasta da US (sem subpasta nem symlink), o `README.md` do épico (a pasta acima) e os `.md` diretamente na pasta do épico que algum arquivo da US referencia por link relativo, normalizado (o que sai da pasta do épico ou aponta para outra `tg-*/` fica de fora). O arquivo é lido pelo id do blob que a listagem da árvore deu. A cada ciclo o daemon compara o SHA da ref com o do cache e **só relê quando ele mudou**; para cada pendência `validacao` aberta sem versão no cache, lê pelo SHA dela. O cache é por (spec, SHA): guarda a versão da ref atual e a de cada `validacao` aberta (a de uma pendência nunca é substituída), e descarta as demais. Limites: 1 MiB por arquivo e 4 MiB por spec; acima deles o arquivo não é guardado e a aba diz "arquivo grande demais (N KiB)" com o link da MR. Forja fora do ar, branch apagada ou limite de taxa: a Central mostra o último conteúdo, com o SHA e a hora da leitura, e a falha. Para não gastar a cota da forja, a spec de MR mesclada (a branch alvo anda com cada commit) é consultada a cada 5 minutos, e uma spec que falhou, a cada 1.

A página `/p/<projeto>/<ref>/spec` renderiza no servidor (goldmark, sem HTML cru, sem imagem, sem JS novo). Link relativo para um arquivo da allowlist vira link para a aba dele; os outros viram texto. A página da **pendência `validacao`** mostra as mesmas abas, lidas pelo **SHA da pendência** (não pela cabeça), ao lado das opções Aprovar e Pedir ajustes: duas colunas em tela larga, empilhadas no celular. O TUI não mostra a spec.

**Migration 0012** (US #60): tabelas `spec_versao`, `spec_arquivo` e `spec_leitura` (cache), só aditivas: declara `leitor-minimo 4` e `escritor-minimo 5`, como a 0011.

**Migration 0011** (US #59): tabelas `spec` e `spec_conferencia`, coluna `sha_validacao` em `pendencia` e `pendencia` refeita com o mesmo conteúdo (o `CHECK` do tipo passa a aceitar `validacao`). Declara `leitor-minimo 4` e `escritor-minimo 5`: o `orq` 5 em diante segue escrevendo, sem conhecer a spec; um binário anterior à 0011 não mostra a spec nem importa `validacao`. Rode `orq upgrade` depois do merge.

### Link público da spec

O usuário final lê a spec da US por um **link público**, só leitura, que o coordenador gera e revoga pela CLI (a web não ganha botão nem escrita). O desenho é o da Parte 3 de `docs/specs/publicar-e-revisar-especificacoes/README.md`; a spec desta US está em `docs/specs/publicar-e-revisar-especificacoes/tg-61-link-publico-da-spec/`.

```bash
orq spec link gerar TG-61 --para "cliente X" --expira 30d    # imprime a URL, uma única vez
orq spec link listar TG-61
orq spec link revogar TG-61 2
```

- **Código.** 32 bytes de `crypto/rand` em base64url (256 bits). O `central.db` guarda só o SHA-256 dele; a consulta é pelo hash. O código não vai para o diário, para a listagem nem para o log, e o orq não consegue mostrar a URL de novo: se a perder, revogue e gere outro.
- **Vários links por spec**, cada um com rótulo (`--para`, até 80 caracteres), validade opcional (`--expira 30d`, `12h` ou `90m`; até 3650d) e revogação própria. A **revogação é definitiva**: `gerar` depois dela cria código novo e nunca devolve um revogado. `listar` mostra id, rótulo, criação, validade, estado (ativo, revogado, expirado), último acesso e contador.
- **Base da URL.** `--url-publica` ou, sem a flag, `ORQ_WEB_PUBLICO_URL` (ambiente ou `orq.env`). Sem nenhuma das duas, o `gerar` recusa com `url_publica_ausente` (exit 2) e não cria o link; nunca usa `ORQ_WEB_URL`, que é o da Central, atrás do login.
- **Socket próprio.** `orq daemon --web-publico unix:<caminho>` (ou só o caminho absoluto; ou `127.0.0.1:<porta>` para uso local; `ORQ_WEB_PUBLICO` é o padrão da flag, e o `--detach` a repassa). Tem de ser outro socket que o de `--web`. Só responde a `GET /s/<código>` e aos estáticos que a página usa (`app.css`, `orq-icon-64.png` e as fontes `.woff2`; o resto de `/estatico/` dá o 404); código inexistente, revogado, expirado ou malformado e qualquer outra rota dão o **mesmo 404**, com o mesmo corpo e os mesmos cabeçalhos. O handler só recebe a interface `web.FontePublica` (spec por hash e registro de acesso), sem `Snapshot` nem `Store`. Toda resposta leva `Referrer-Policy: no-referrer`, `Cache-Control: no-store` e `X-Robots-Tag: noindex`. O log do daemon só traz a rota (`/s/{codigo} 200`), nunca o caminho.
- **A página** é uma só, `GET /s/<código>`: a faixa do topo (título da US, hora da leitura e o estado), um índice de âncoras e uma seção por arquivo, nesta ordem: **Spec, Plano, Decisões, os outros `.md` da pasta da US, o README do épico e as specs compartilhadas**. As âncoras não saem do nome do arquivo: `us-spec`, `us-plano`, `us-decisoes`, `us-extra-N` (os outros `.md` da pasta da US, numerados na ordem da página), `epico-readme` e `epico-compartilhada-N`; um `README.md` extra da pasta da US (`us-extra-1`) não colide com o do épico. Link relativo para um arquivo do conjunto vira âncora da seção; qualquer outro link relativo vira texto; imagem vira o texto alternativo; HTML cru fica escapado e `javascript:`/`data:` são neutralizados (a mesma blindagem da página da Central). Link `http`, `https` e `mailto` do conteúdo fica como está. Nenhum `href` da página é de rota da Central (`/p/`, `/onda/`, `/projeto/`…) nem cita projeto ou ref, e ela não tem ref, SHA, link da MR, pane, onda nem pendência.
- **Estado na faixa:** *em validação* (spec registrada, em validação, ou com mudança além do `decisoes.md` depois da aprovação), *aprovada* (aprovada, ou com a MR da spec mesclada) e *entregue*, que só vale com **uma MR do código ligada à US e mesclada (qualquer MR da US que nunca foi MR de spec dela: a atual e as anteriores a um `--substituir` ficam de fora)**: o `done` da US e a onda encerrada não bastam, e a onda "até plano" encerrada com a spec aprovada mostra *aprovada*.
- **Versão.** O registro da spec tem um número de versão: 1 no registro inicial, +1 a cada `orq spec registrar --substituir`. O `--substituir` **não mexe nos links**: eles seguem e passam a mostrar a spec nova, e a saída avisa "N links ativos passam a mostrar a spec nova". Da versão 2 em, a faixa pública e a da página da spec na Central mostram "Versão N — substituída em DD/MM/AAAA HH:MM"; na versão 1 não há aviso.
- **Acessos.** Cada `GET` válido (`HEAD` não conta) atualiza último acesso e contador (`MarcarAcesso`), fora do diário, como o cache dos enriquecedores.

**Proxy reverso (passo do coordenador, fora do repositório).** O proxy reverso da máquina da Central precisa: (1) apontar um host público para o socket do `--web-publico`, **sem login** (a Central continua atrás do login, no socket do `--web`); (2) aplicar **limite de taxa por origem** nesse host, com o módulo de limite de taxa do proxy (o orq não limita); (3) gravar o log de acesso desse host **sem o caminho** (ou com o código mascarado) e **sem o cabeçalho `Location` das respostas**: o `//s/<código>` (barra dupla) recebe um redirecionamento 307 do `ServeMux` cujo `Location` traz o código. Depois, ponha `ORQ_WEB_PUBLICO` (o socket) e `ORQ_WEB_PUBLICO_URL` (a base pública, sem o `/s/…`) **no `orq.env` do plugin**, e reinicie o daemon **sem flag** (o `orq upgrade` faz isso, e traz a migration 0013). Não dependa só da flag `--web-publico` na linha de comando: o `orq upgrade` repassa ao daemon novo o ambiente e só `--tick`/`--intervalo-remoto`, então um daemon subido com a flag perde o socket público no próximo upgrade e o proxy passa a dar 502. Validar: o link abre sem login, o revogado dá 404, a Central pede login, o limite de taxa está ativo e o log não tem o código (nem no `Location`).

**Migration 0013** (US #61): coluna `spec.versao`, tabela `spec_link` e as colunas `caminho` e `mr_url` de `spec_versao` (cada versão do cache sabe de que registro veio; o daemon não grava, e a leitura não mostra, conteúdo de outra pasta ou MR depois de um `--substituir`). Só aditiva: `leitor-minimo 4`; `escritor-minimo 13`, a própria migration, porque um `orq` anterior faria `--substituir` sem incrementar a versão e gravaria cache sem as colunas novas. Rode `orq upgrade` depois do merge.

### Pendências com contexto e resposta pela web

Uma pendência é uma pergunta à pessoa: dúvida, autorização ou decisão. O worker (ou o coordenador) não a registra com um resumo de uma linha: ele grava um **arquivo**, e o daemon o importa.

**Onde.** O worker grava em `<pasta da rodada>/pendencias/<slug>.md` (por exemplo `~/tmp/orquestrador/orq/TG-15/implementacao/r1/pendencias/decisao-yaml.md`); o coordenador, em `<ORQ_TRABALHO_DIR>/<projeto>/TG-<ref>/pendencias/<slug>.md`. O projeto e a US saem do caminho, não do conteúdo, e a US precisa estar numa onda aberta. O slug tem minúsculas, dígitos, `-` e `_` (até 64).

**Formato.** Frontmatter em YAML e o detalhe em markdown:

```markdown
---
tipo: decisao            # duvida | autorizacao | decisao | validacao
titulo: Qual biblioteca de YAML?     # uma linha
autor: tg-15_executor
opcoes:
  - id: 1
    titulo: yaml.v3
    descricao: estável, sem manutenção
  - id: 2
    titulo: go.yaml.in/yaml/v3
    descricao: o fork mantido
    recomendada: true
---
## Contexto
## Pergunta
## Recomendação           (obrigatória em decisão)
## O que fica bloqueado
## Evidências             (caminhos, URLs de MR/pipeline, SHAs)
```

- `decisao` pede 2 a 10 opções e no máximo uma `recomendada`; `autorizacao` não traz opções (a Central cria `1 Autorizo` e `2 Não autorizo`); `duvida` aceita opções (2 ou mais) ou nenhuma. Campo desconhecido no frontmatter é recusado.
- Contexto, Pergunta, O que fica bloqueado e Evidências são obrigatórias e não podem ficar vazias; o texto do comentário entre parênteses no título da seção é ignorado. O detalhe (o markdown) tem no máximo 64 KiB; o arquivo todo, no máximo 96 KiB.

**Importação.** O worker grava `.<slug>.md.tmp` e faz `rename` para `<slug>.md`: arquivo oculto ou terminado em `.tmp` é ignorado. A cada tick do daemon (5 s) ele procura `<ORQ_TRABALHO_DIR>/*/TG-*/pendencias/*.md` e `<ORQ_TRABALHO_DIR>/*/TG-*/*/*/pendencias/*.md`. Toda leitura e escrita da importação (o `.md`, o `.tmp`, o `.registrada.json`, o `.erro.txt`) passa por um `os.Root` aberto em `ORQ_TRABALHO_DIR`: o que sai da raiz é recusado (`symlink_fora_da_raiz`), também quando uma pasta vira symlink no meio do registro, e nada é escrito fora. Symlink de destino absoluto é recusado mesmo apontando para dentro; o relativo, dentro da raiz, vale. Resultado, ao lado do arquivo:

- válido: a Central numera e grava a pendência e cria `<slug>.registrada.json` com `{"id","chave","url"}` (`TG-15#P1`, `orq/15#P1` e a página, se `ORQ_WEB_URL`/`--web-url` está definido; senão só o caminho `/p/orq/15/1`);
- inválido: cria `<slug>.erro.txt` com o motivo e a linha (`linha 12: seção ## Pergunta ausente`). O erro some quando a importação der certo. Pendência de US fora de onda aberta também vira erro.
- idempotente por `slug` + US: o mesmo arquivo de novo não cria outra pendência. Se o arquivo **mudar** depois do registro, a pendência não muda: o diário ganha um `pend.arquivo_alterado` (uma vez por conteúdo), e o TUI e a página avisam. Editar uma pendência registrada está fora do escopo: abra outra.

`orq pend importar <arquivo>` roda o mesmo código, para quando o daemon está parado (sai com `pendencia_invalida`, exit 2, e cria o `.erro.txt`, se o arquivo não vale).

**Estados.** Derivados das colunas: *aberta* (`aberta = 1`, sem resposta), *respondida* (`aberta = 1`, com `respondida_em`) e *resolvida* (`aberta = 0`). A respondida continua contando como aberta para encerrar a onda. A resposta pode ser trocada até a pendência ser resolvida: cada troca é um `pend.responder` no diário e zera o aviso ao coordenador. As pendências antigas (abertas por `pend add`) aparecem só com o título e a marca `sem detalhe`.

**Avisos.** O daemon avisa o coordenador da US pelo `herdr agent prompt` (a única escrita dele no Herdr além dos tokens), por uma interface (`Avisador`, com a implementação do Herdr e uma nula):

- ao importar: `[orq] <projeto> TG-<ref> pendência P<n> aberta: <título> — <url>`;
- quando a pessoa responde: `[orq] <projeto> TG-<ref> pendência P<n> respondida: opção <id> (<título da opção>) — leia com orq pend ver <chave>`. O texto livre da pessoa **não** vai no prompt: o coordenador o lê com `orq pend ver`.

Só com o coordenador `idle` ou `done` (`herdr agent get`); senão, tenta no ciclo seguinte. No máximo um aviso por coordenador por ciclo, e `avisada_em` (ou `abertura_avisada_em`) evita o duplicado, e o aviso confirma a **versão** da resposta (`resposta_versao`, que sobe a cada troca), não um horário; a troca de resposta gera um aviso novo, mesmo no mesmo segundo. Se o pane do coordenador não tem mais agente (`agent_not_found`), o daemon grava `pend.aviso_falhou` (uma vez), o TUI mostra `[respondida, sem coordenador]` e o aviso sai quando um coordenador reaparecer no pane. A resposta pelo chat (`orq pend responder`) já nasce avisada: quem a grava é o coordenador. Sem o `herdr` (ou com o Avisador nulo), o aviso fica devido e nada é marcado.

**Servidor web.** `orq daemon --web unix:<caminho>` (`--web unix:` usa `$XDG_RUNTIME_DIR/orq/web.sock`, modo 0600, pasta 0700) ou `--web 127.0.0.1:<porta>` para uso local; qualquer outro endereço é recusado. Sem `--web`, o daemon não abre servidor. A web abre antes da prontidão do `--detach`, depois do banco e do limpador; socket que não abre (`web_nao_abriu`, exit 4) é falha de partida. `ORQ_WEB`, `ORQ_WEB_URL` e `ORQ_WEB_CABECALHO` (no ambiente ou no `orq.env` do plugin) são o padrão de `--web`, `--web-url` e `--web-cabecalho`; `--detach` repassa as três ao filho. `ORQ_WEB_URL` também vale para `orq pend importar`, `orq pend ver` e para o link do TUI: use o mesmo valor em todos.

| Rota | Conteúdo |
|---|---|
| `GET /` | a Central, aba Todos (veja [Central pela web](#central-pela-web)). Com `coord`, `projeto` ou `onda` na query (os filtros da página antiga), responde `308` para `/pendencias` com a mesma query |
| `GET /central` | `308` para `/` (mantém a query) |
| `GET /pendencias?projeto=&coord=&onda=` | a aba Pendências: esperando você e respondidas, com filtros em cascata projeto → coordenador → onda; o endereço guarda o filtro e a combinação impossível é normalizada (`303` na página, `HX-Replace-Url` no fragmento). Com `HX-Request` devolve só o `#pend-conteudo`; com `HX-History-Restore-Request` (botão Voltar) devolve a página inteira. Atualiza sozinha pelo `/vivo` |
| `GET /p/<projeto>/<ref>/<n>` | o link direto da pendência (sem accordion, formulário comum, funciona sem JS): o detalhe renderizado, as opções como botões de rádio (a recomendada com selo, nenhuma marcada), o campo de texto, o histórico de respostas, o estado e os links da US no Taiga e das MRs; sem recarga automática |
| `GET /p/<projeto>/<ref>/spec` | a spec da US (orq #60): faixa com o estado da spec, a ref, o SHA e a hora da leitura e o link da MR da spec; uma aba por arquivo (Spec, Plano, Decisões, README do épico e os `.md` do épico que a pasta referencia), trocada por link (`?aba=`), sem script. O card da US leva a ela. Veja [Specs](#specs-registro-validação-e-conferência) |
| `GET /p/<projeto>/<ref>/<n>/corpo` | fragmento do corpo da pendência (contexto, opções, formulário; ou a resposta dada e "Trocar a resposta", `?trocar=1`), buscado na primeira abertura do accordion; sem `HX-Request` vai para `/p/…` |
| `POST /p/<projeto>/<ref>/<n>/responder` | grava (mesmo contrato: campo `texto`, `opcao`, `respondida_por` do proxy). Sem `HX-Request`, redireciona (post/redirect/get); com `HX-Request`, devolve o item respondido e as contagens por `hx-swap-oob`, o item muda de grupo conforme a composição que a página mandou (`comp`), sem rearmar o `/vivo`, e os erros (400, 409, 413, 415, 503) vão para o alerta do formulário (`HX-Retarget: find .erro`, `HX-Reswap: innerHTML`), sem perder o texto |
| `GET /historico?pagina=N` | as resolvidas, 20 por página |
| `GET /projeto/<slug>` | a mesma Central, só com as ondas que têm US do projeto; 404 se o slug não tem onda aberta |
| `GET /vivo?v=<hash>` | fragmento da atualização de 30 s, por aba (só `hx-swap-oob`: situação, abas, idade dos dados, o `<summary>` de cada onda e US; na Central, também o status do Taiga, as MRs e a lista de pendências de cada US, por diferença contra o `pi` que a página manda; na aba Pendências, os contadores dos grupos); nunca devolve o corpo nem o formulário de uma pendência e não troca nem remove a que está aberta ou com texto digitado (o `app.js` a retém e a marca como desatualizada); lista de ondas ou US diferente da da página vira o aviso "A lista mudou" |
| `GET /onda/<id>?pagina=N` | o diário da onda (`orq diario --onda`), do mais recente ao mais antigo, 25 por página; recarrega a cada 30 s |
| `POST /onda/<id>/mensagens` | envia uma mensagem ao coordenador da onda aberta (campo `texto`, até 8 KiB, `application/x-www-form-urlencoded`, mesma proteção de origem e usuário); sem `HX-Request` faz 303 para `/#onda-<id>`, com `HX-Request` devolve a lista `#onda-<id>-msgs` e atualiza `(N)` e `#aviso` via `hx-swap-oob`. O daemon avisa o coordenador via `herdr agent prompt` com `orq msg ver <id>` / `orq msg responder <id>` (sem texto livre no prompt) |
| `GET /saude` | `ok` se o banco responde e aceita escrita deste `orq` |
| `GET /estatico/<arquivo>` | o CSS (`app.css`), o `htmx.min.js`, o `app.js`, as fontes e as imagens embutidos no binário; `/estilo.css` deixou de existir. Só `GET`; diretório e o que não está no embed dão 404 |

As páginas são renderizadas no servidor (`html/template`), tema claro, pensadas para a largura de um celular; os formulários são POST e não pedem JS. O CSS (Tailwind + daisyUI, tema `basis-interno`), o htmx, as fontes e os modelos vão embutidos no binário; a web não serve arquivo do disco nem o JSON do `snapshot`. O CSS é gerado por `scripts/build-frontend.sh` (Node 24, só para quem edita o frontend; `go install` e o plugin seguem sem Node) e commitado em `internal/web/estatico/`; o job `frontend` do CI falha se o commitado não bate com o gerado. Spec: [docs/central-web-v2.md](docs/central-web-v2.md).

#### Central pela web

`GET /` (aba Todos; `/central` responde 308 para ela) e `GET /projeto/<slug>` mostram o que o TUI mostra, sem nenhuma ação (não há merge, encerramento de onda, mudança de status nem despacho pela web; só `GET`, e o `POST` da resposta às pendências, que é a única escrita). A lista de pendências está em `/pendencias`, a aba Pendências. A página é montada por uma função pura (`vistaCentral`, `internal/web/vista.go`) sobre o `Snapshot` lido uma vez por requisição (só as ondas abertas, como o `orq snapshot`), pela interface `Fonte`. As ondas vêm em accordion, ordenadas pelo que pede atenção (pendência esperando, pipeline falhou, MR pronta, o resto, por id), e trazem:

- *onda:* título (cortado na barra fechada, inteiro ao abrir), `até`, ordem, progresso (`<progress>`), link para o diário `/onda/<id>` e os selos `concluída` (a regra de [Encerrar a onda](#encerrar-a-onda), a mesma do TUI) e `encerrada`;
- *US:* id, status na Central, título e status no Taiga (do cache, com link), dependências e o estado do agente (`working`, `idle`, `done`, `blocked`…). **Nome, id de pane e workspace do Herdr não aparecem**, nem no diário: lá, cada tipo de evento tem uma lista do que pode sair (`camposDoDiario`, em `internal/web/central.go`), tipo fora da lista mostra "detalhe omitido", o alvo de um recurso do tipo `pane` some e, nos textos, o id (`w…:p…`, `w…:t…`) e o nome entre parênteses viram `[pane]`. Tipo de evento novo precisa entrar nessa lista para ter detalhe na página (um teste confere os tipos gravados hoje);
- *MRs:* link para a forja, o estado e, separados, o pipeline (`sucesso`, `falhou`, `rodando`, `pendente`, `sem pipeline`) e a aprovação (`aprovada`, `pendente`, `não exigida`), com `desconhecido` por extenso quando o cache não tem o dado (MR ainda não consultada, forja antiga, ou a consulta a mais falhou sem valor anterior; se havia, a MR mantém o último valor e a página mostra "consulta extra falhou" ao lado). `pronta` quer dizer mesclável, não aprovada: repositório sem exigência de revisão fica `pronta` com aprovação `não exigida`. MR que o daemon ainda não consultou aparece como `sem estado`. A origem dos dois campos está em [docs/snapshot.md](docs/snapshot.md) (GitLab: da própria resposta da MR; GitHub: um `gh pr view --json statusCheckRollup,reviewDecision` a mais por PR aberto a cada consulta, 60 s no máximo);
- *pendências:* "esperam você" (as sem resposta; a respondida sai da conta, mas segura o `concluída` até o coordenador resolver) na onda, na US, na situação e na aba Pendências, e, em cada US, as pendências abertas e respondidas como accordion: abrir busca o corpo em `/p/…/corpo` e responde-se ali mesmo (`hx-post`), sem trocar de página; o item respondido fica fechado no lugar, e as contagens da aba, da onda, da US e da situação mudam. Na aba Pendências, o item respondido passa para o grupo "Respondidas" e o grupo vazio mostra a frase. Trocar uma resposta não muda as contagens. O servidor nunca serve `<details open>` em pendência.

No topo, a idade do cache de cada fonte (`herdr`, `mr`, `taiga`), na mesma forma do rodapé do TUI (`central.Idade`, usada pelos dois). **Dado vencido aparece como vencido:** a idade passa do prazo da fonte (`central.PrazoVencido`: 3 × o TTL do enriquecedor, ou seja 15 s no Herdr, 3 min nas MRs e 15 min no Taiga) e ganha o selo `vencido` em amarelo, na fonte e em cada MR, US e agente; sem o daemon, a página inteira passa a mostrar `vencido` em vez de fingir que está atual. Fonte sem nenhum valor mostra `sem dado`, e consulta que falhou mostra `falhou: <código>` ao lado do último valor bom.

`GET /onda/<id>` traz o título e o resumo da onda (inclusive encerrada) e o diário dela paginado. O detalhe dos eventos sai em JSON compacto (só os campos da lista), cortado em 600 bytes, escapado pelo `html/template`. Onda inexistente ou id mal formado dá 404.

A forma é a das páginas de pendências: o mesmo CSS, a mesma CSP e os mesmos cabeçalhos, a recarga é `<meta http-equiv="refresh" content="30">` (sem JS) e, no socket Unix, requisição sem o cabeçalho do usuário recebe 401.

**Segurança.**

- *Quem responde.* Atrás do Caddy, no socket Unix, o usuário autenticado chega no cabeçalho `X-Orq-User` (`--web-cabecalho` troca o nome) e vira `respondida_por`. No socket Unix, **requisição sem o cabeçalho é recusada** (401; só `/saude` passa), porque quer dizer que não passou pela autenticação. Em TCP o cabeçalho é ignorado (qualquer processo local o forjaria) e `respondida_por = local`; o `Host` precisa ser `127.0.0.1`, `localhost` ou `[::1]` com a porta certa (421 senão). O chat grava `chat`.
- *Origem.* `http.CrossOriginProtection`: POST de outra origem é recusado (403). O formulário só aceita `application/x-www-form-urlencoded`.
- *Conteúdo.* O markdown vai pelo goldmark sem `WithUnsafe`: HTML bruto sai escapado (visível, nunca ativo), e só links `http`, `https`, `mailto`, âncora e caminho relativo sobrevivem (`javascript:`, `data:` e `//host` viram `#`); imagem perde o endereço. Título, opções e respostas são escapados pelo `html/template`.
- *Cabeçalhos.* `Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'` (sem `unsafe-inline`: nenhum `<script>` nem `style=` nas páginas), `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cache-Control: no-store`.
- *Cache.* Páginas: `no-store`. Estáticos, nunca `public` (a web fica atrás de `basic_auth`): `app.css`, `htmx.min.js` e `app.js` pedidos com `?v=<hash do conteúdo embutido>` levam `private, max-age=31536000, immutable`; fontes, imagens e o que vem sem hash, `private, max-age=86400`. Não testado atrás do Caddy.
- *Limites.* Corpo do POST até 16 KiB (413), resposta até 8 KiB (413), caracteres de controle recusados (400; a quebra de linha e a tabulação passam).
- *Log.* Só a rota (o padrão, `/p/{projeto}/{ref}/{n}`, sem o método, o caminho nem a query) e o status: `web <rota> <status>`.

**Caddy, relay e credencial** (operação, depois do merge e com a autorização de quem opera) estão fora do orq. O que o orq espera: o Caddy autentica, encaminha para o socket (`reverse_proxy unix//run/user/<uid>/orq/web.sock`) e põe o usuário no cabeçalho (`header_up X-Orq-User {http.auth.user.id}`), sem repassar um `X-Orq-User` vindo de fora; ele preserva `Host` e `Origin` do cliente (o `CrossOriginProtection` compara os dois).

**Resposta de quem.** O coordenador só trata como resposta da pessoa o que tem `respondida_por` igual ao usuário da web, `local`, ou `chat` gravado por ele mesmo a partir de uma fala da pessoa. Worker e revisor nunca rodam `orq pend responder` nem `orq pend resolver`.

### TUI

`orq tui` desenha a Central a partir do mesmo snapshot de `orq snapshot`, relido a cada `--intervalo` (2 s); quem consulta as fontes é o daemon, o TUI só lê o cache. Com `--coord X`, as ondas abertas de um coordenador (é o que roda nos panes do workspace Central); com `--todos`, um bloco por coordenador com o total de pendências abertas (o popup do plugin); sem nenhum dos dois, o coordenador corrente ou, sem ele, todos.

```
 convey · sgo-cli                               até MR pronta  1/4 ▰▱▱▱  1 pend
 Comandos api e jql
╭───┬──────┬──────────────────────────────────────┬───────────┬──────────────────────╮
│   │ US   │ título                               │ status    │ MRs                  │
├───┼──────┼──────────────────────────────────────┼───────────┼──────────────────────┤
│ ⚠ │ #313 │ sgo api: chamada REST                │ Exec r1 ↗ │ sgo-cli!33           │
│   │      │                                      │           │ plataforma!412 [IaC] │
│ ● │ #312 │ sgo jql: consulta avulsa por JQL c…  │ Rev r2 ↗  │ sgo-cli!32           │
│ – │ #314 │ Menores da TG-312                    │ Wait      │                      │
│ ✓ │ #225 │ Criação Suporte/OS                   │ Done ↗    │ sgo-cli!29           │
│   │      │                                      │           │ sgo-cli!31           │
╰───┴──────┴──────────────────────────────────────┴───────────┴──────────────────────╯
╭───────────┬─────────────┬───────────────────────────────────┬───────────┬───╮
│ id        │ tipo        │ texto                             │ idade     │   │
├───────────┼─────────────┼───────────────────────────────────┼───────────┼───┤
│ TG-313#P1 │ autorização │ instalar binário e aplicar config │ há 12 min │ ↗ │
│           │             │ pessoal                           │           │   │
╰───────────┴─────────────┴───────────────────────────────────┴───────────┴───╯
 herdr agora · mr há 1 min · taiga há 2 min ✗ autenticacao  ● trabalhando ⚠ bloqueado …  ↑↓ Enter foca · r relê · q sai
```

- Cada onda abre com uma faixa de fundo leve na largura toda: coordenador (link que foca o pane dele) e projetos, e à direita a autorização (`até MR pronta`), o progresso (`1/4 ▰▱▱▱`) e as pendências abertas, e o selo `concluída`, em verde, quando a onda está pronta para encerrar (a regra de [Encerrar a onda](#encerrar-a-onda)); na 2ª linha, o título da onda, em negrito e numa cor fixa por projeto. O projeto é o `projeto_padrao` do coordenador, quando está entre os das US da onda, ou o primeiro projeto da onda (a ordem da 1ª linha). A cor vem do tom do projeto, gravado na Central (`projeto_tom`) na ordem de chegada: o primeiro projeto que aparece (`orq coord registrar --projeto` ou `orq onda criar`) fica com o tom 0, o seguinte com o 1, e assim por diante, para sempre, e o `orq diario` registra cada atribuição (`projeto.tom`, com o projeto e o índice no `detalhe`). Exceção: o preenchimento da migration, para os projetos que já existiam, não gera eventos, e a ordem dele é reconstruída do estado atual (registro do coordenador e onda atual da US), não a primeira chegada histórica absoluta. A paleta tem 8 tons (azul, amarelo, magenta, verde, laranja, ciano, lilás e ardósia, com versão para o tema claro e para o escuro, sem vermelho); passado o oitavo projeto, os tons se repetem (o 9º volta ao azul). O snapshot traz os índices em `projetos`; projeto sem índice (snapshot antigo) usa o cinza. Limite: o projeto é o board do Taiga, então coordenadores de chaves diferentes no mesmo board (argocd-apps e convey, ambos `infra-2025`) têm a mesma cor. Coordenador sem onda aberta é uma linha discreta.
- O título da pendência leva as marcas `[respondida]`, `[respondida, sem coordenador]`, `[sem detalhe]` (pendência antiga) e `[arquivo alterado]`. Com `ORQ_WEB_URL` definido, a pendência selecionada mostra o link da página dela na linha de status (OSC 8).
- Tabela das US: agente | US | título | status | MRs, com fundo alternado. A 1ª coluna é o status Herdr do pane da US, não o da US: `●` working, `⚠` blocked, `·` idle, `✓` done, `○` pane sem agente (unknown), `✗` pane que sumiu (ausente), `–` US sem pane ou Herdr que nunca respondeu (a legenda vai no rodapé). A US com o pane `blocked` sobe para o topo, com fundo de destaque e título em negrito; a US Done sai esmaecida.
- `#ref` é link (OSC 8) para a story no Taiga; o título é cortado na largura com `…`. Num pane estreito o título encolhe primeiro e depois as MRs (`plat…`); num largo as colunas não passam do conteúdo.
- Status com cor (Wait cinza, Plan roxo, Exec amarelo, Rev azul, Done verde) e link para `https://orq.local/pane/<id>`: Ctrl+clique chama a ação `focar-pane` do plugin. Clique simples em qualquer linha da US, ou ↑/↓ e Enter, focam o pane pelo próprio TUI (`herdr agent focus`); a seleção é o fundo da linha. No popup, focar fecha o popup.
- MRs na mesma célula, uma por linha, rotuladas `<projeto>!<iid>` (GitLab) ou `<repo>#<n>` (GitHub), com o projeto da URL da MR, não o da onda, e link para a forja: draft vermelho, draft com pré-condição amarelo (com o texto numa linha `↳` sob a MR, e a coluna alarga até 48 colunas por ele), pronta verde, aberta sem cor, mesclada cinza tachado, fechada roxo itálico, `[IaC]` em laranja.
- Pendências abertas numa tabela menor: id, tipo com cor, texto em até 2 linhas (cortado com `…` depois), idade e o `↗`, que foca o pane que perguntou.
- Os tons de fundo seguem a cor de fundo que o terminal informa ao bubbletea (claro ou escuro), sem esperar a resposta: até ela chegar, e num terminal que não responde, valem os do escuro.
- Erro de fonte não esconde o valor: a tela mostra o último valor bom, um `*` apagado marca o título ou a MR cuja consulta mais recente falhou, e o rodapé dá a idade de cada fonte e o código do erro.
- Nada que a leitura do banco ou o foco façam derruba a tela: o erro vai para a linha de status (ex. `foco em wB:p3 falhou: binario_ausente: …`) e o TUI segue.

### Workspace Central

`orq central abrir` acha o workspace pelo rótulo (`--nome`, padrão `$ORQ_CENTRAL_NOME` ou `Central`), cria se não existe e deixa nele um pane rodando `orq tui --coord X` (pelo caminho absoluto do `orq` que rodou o comando, com `ORQ_STATE_DIR` e os binários dele) para cada coordenador com onda aberta. É idempotente: os panes são reconhecidos pelo rótulo `orq · <coordenador>` e conferidos com `herdr pane process-info`. Com o `orq tui --coord X` rodando, o pane fica (`mantido`); de volta ao shell, porque o TUI saiu com `q`, caiu ou nunca subiu, o TUI é lançado de novo no mesmo pane (`relancado`); com outro processo em primeiro plano, o orq não manda nada para ele (`ocupado`, com o processo no `detalhe`). O que falta é criado na grade (abaixo), e depois de cada lançamento o comando espera até 5 s o TUI aparecer; se não aparece, o `detalhe` da ação diz. O pane de um coordenador sem onda aberta é fechado, se estiver no TUI ou no shell, porque não teria o que mostrar (a próxima chamada o recria quando houver onda), e a grade se refaz sem buraco. Panes sem esse rótulo não são tocados. Sem coordenador com onda aberta, o workspace não é criado. Sem `--sem-foco`, foca o workspace no fim.

A Central é uma grade de duas colunas, preenchida por linha, na ordem de criação:

```
1 pane:  |   1   |        3 panes: |  1  |  2  |      5 panes: |  1  |  2  |
                                   |    3      |               |  3  |  4  |
                                                               |    5      |
```

O pane de posição par entra à direita do anterior, meio a meio; o de posição ímpar (a partir do 3º) abre uma linha de largura total embaixo de tudo, e as linhas ficam com a mesma altura depois de cada criação. Os panes que já existem não mudam de lugar ao criar um novo. Ao fechar o pane de um coordenador sem onda, os seguintes sobem uma posição; uma grade que o usuário arrumou de outro jeito (colunas, por exemplo) também é refeita, do 2º pane em diante. Os processos (`orq tui`) não reiniciam ao mover. Panes sem o rótulo `orq · ` não são tocados nem contados, mas ocupam espaço na aba: com um deles ao lado da grade, as linhas deixam de ter a largura da aba inteira.

O Herdr 0.9.3 não divide a aba inteira (`pane split` e `pane move --split` dividem um pane, e sem `--target-pane` o pane focado) e `pane move` dentro da mesma aba responde `same_tab`. Por isso a linha nova sai de tirar o pane da direita da última linha para uma aba temporária (`--new-tab`), dividir para baixo o da esquerda e devolver o outro com `--tab --target-pane --split right`; as alturas vêm de `pane resize`. Se um passo falha no meio, o erro diz qual pane ficou na aba temporária e o comando que o traz de volta.

### Plugin do Herdr

O manifesto é o `herdr-plugin.toml` da raiz (id `orq`, Linux, Herdr 0.9.3+):

| Entrada | O que faz |
|---|---|
| `[[build]]` | `scripts/build-plugin.sh`: `go build` para `bin/orq`, com a versão do `git describe`. |
| `[[startup]]` | `orq daemon --detach` no início do servidor do Herdr (não no `plugin link` nem no `enable`). O `flock` de `daemon.lock` impede um segundo daemon. |
| ação `abrir-central` | `orq central abrir`. |
| ação `popup-todos` | abre o pane `todos` (popup de 85% × 80% com `orq tui --todos`). |
| ação `focar-pane` | `orq focar`, que lê `$HERDR_PLUGIN_CLICKED_URL`. |
| link handler `pane` | `^https://orq\.local/pane/` → `focar-pane`: o Ctrl+clique no status ou no `↗` foca o pane. |

Não há `[[events]]`: o daemon já assina `pane.agent_status_changed` no socket e republica na hora; um hook de evento abriria um processo por mudança de status de qualquer pane da máquina sem acrescentar nada.

As ações, o startup e os panes do plugin rodam com o ambiente do servidor do Herdr, não com o do seu shell. Para apontar outro estado ou outros binários para eles, ponha `ORQ_*` em `orq.env`, no diretório de `herdr plugin config-dir orq` (linhas `CHAVE=valor`; só chaves `ORQ_`; o ambiente do processo vale mais que o arquivo). Os comandos do terminal leem o mesmo arquivo (a regra do diretório está em [Daemon e enriquecedores](#daemon-e-enriquecedores)), então um `orq.env` serve ao daemon e ao coordenador:

```bash
cat > "$(herdr plugin config-dir orq)/orq.env" <<'X'
ORQ_STATE_DIR=/caminho/do/estado
ORQ_CENTRAL_NOME=Central
# ORQ_LIMPEZA_AUTOMATICA=0   # desliga a limpeza do daemon no merge
# ORQ_WEB=unix:              # liga a web de pendências no socket padrão (veja "Pendências com contexto")
# ORQ_WEB_URL=https://pend.exemplo.com.br
# ORQ_WEB_PUBLICO=/run/user/1000/orq/publico.sock   # liga a página pública das specs (veja "Link público da spec")
# ORQ_WEB_PUBLICO_URL=https://specs.exemplo.com.br
X
```

`orq plugin configurar` acrescenta ao `config.toml` (`--config`, padrão `$HERDR_CONFIG_PATH`, `$XDG_CONFIG_HOME/herdr/config.toml` ou `~/.config/herdr/config.toml`):

- `[{ token = "$orq" }]` em `[ui.sidebar.agents] rows` e `[{ token = "$orq_mrs" }]` em `[ui.sidebar.spaces] rows` (sem `rows` no arquivo, as linhas padrão do Herdr mais a do orq);
- `prefix+u` → `orq.popup-todos` e `prefix+shift+u` → `orq.abrir-central` (`--tecla`, `--tecla-central`; `""` não cria a tecla; tecla já usada é `tecla_ocupada`).

Os tokens `mrs_prontas`, `story` e `fase` da skill manual não são tocados. Antes de escrever, o `herdr config check` tem de aprovar o arquivo atual (o orq não mexe num config que já reprova), e o arquivo é copiado para `config.toml.orq-AAAAMMDD-HHMMSS.bak`; depois, o check roda de novo e, se reprovar, o arquivo anterior volta (`config_check_falhou`). Sem mudança a fazer, nada é escrito. O editor mexe no texto: a linha do orq entra antes do `]` final de `rows` (numa linha própria, com a indentação e a vírgula final do arquivo, quando `rows` ocupa várias linhas), e comentários e formatação ficam como estão. `orq plugin desconfigurar` tira só o que o `configurar` põe: a linha `[{ token = "$orq" }]` (e a do `$orq_mrs`), do mesmo jeito, pelo texto, e os `[[keys.command]]` com o comentário `# orq:` logo acima. Um token do orq que você pôs numa linha sua, ou com estilo, e uma tecla sua para uma ação `orq.*` (sem a marca) ficam. As `rows` que o `configurar` criou do zero ficam, com os valores padrão. `[ui.sidebar.agents.rows_by_agent]` substitui `rows` para o agente: o `doctor` avisa quando ele esconde o `$orq`.

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
orq onda criar "Melhorias da noite" --nome melhorias-noite --us 215,216,217,218 --ordem estrita --depende 216:215
orq onda proxima          # TG-215
orq us status TG-215 exec --pane wB:p3 --papel implementacao --rodada 1 --modelo claude-sonnet-5-5 --esforco medium
orq pend add TG-215 --tipo autorizacao "aplicar a migration no banco de homologação"   # TG-215#P1
orq mr add TG-215 https://gitlab.basis.com.br/basis/colaboradados/api/-/merge_requests/41
orq us status TG-215 done
orq onda proxima          # TG-216
```

### Nome do pane do coordenador

O rótulo do pane (`herdr pane rename`) e o nome do agente (`herdr agent rename`) do coordenador têm de ser o nome da onda aberta **mais recente** dele; quem renomeia é o orq, não o agente. O pane é o registrado em `orq coord registrar` (não o `$HERDR_PANE_ID` do momento).

- `orq onda criar --nome`, `orq onda adicionar` e `orq onda encerrar` renomeiam ao terminar: com duas ondas abertas vale a mais recente, e ao encerrar volta o nome da que restou (encerrada a última, o pane fica como está). A saída traz o que foi feito (`pane_nome` no JSON; uma linha `pane: …` no texto), e cada renomeio vira o evento `onda.pane_nome` no diário. Falha do Herdr (ou pane sem agente registrado, caso em que só o rótulo muda) não impede criar, ampliar nem encerrar: sai como `aviso: pane …` e no evento (`erros`).
- O daemon confere a cada `--tick` (`herdr api snapshot`) o rótulo e o nome do agente do pane de cada coordenador com onda aberta e corrige o que divergiu (inclusive o que alguém renomeou à mão e a onda criada por um orq anterior). A mesma recusa do Herdr não volta ao diário nem ao log a cada tick.
- Onda antiga, criada sem `--nome`: o nome é o início do título, se for um nome válido; senão o campo `nome` fica vazio, o `orq onda ver` avisa `(sem nome …)` e o orq não renomeia o pane por ela (se for a mais recente, o pane fica como está).

### Encerrar a onda

Com todas as US em `done`, `orq onda proxima` traz `encerramento`; antes disso, ele vem nulo. A onda está pronta para encerrar (`pronta: true`) quando, além disso, toda MR da onda está `mesclada` ou `fechada` e nenhuma pendência está aberta. Aí `comando` traz `orq onda encerrar <id>`. Senão, `comando` é nulo e `faltam` lista o que impede, um item por MR ou pendência, com a chave completa (`colaboradados/215`, `colaboradados/215#P1`), que funciona colada em `orq pend resolver` mesmo numa onda de vários projetos. O estado da MR vem do cache do daemon: MR sem estado (o daemon não rodou ou ainda não consultou a forja) entra em `faltam` como "falta conferir", nunca como mesclada. Numa onda `plano` as MRs não contam: o plano aprovado encerra a onda, e `passo` lembra de criar a onda de implementação com as mesmas US.

```json
"encerramento": {
  "pronta": false,
  "comando": null,
  "faltam": ["colaboradados/215: MR https://gitlab.basis.com.br/basis/colaboradados/api/-/merge_requests/41 pronta (falta merge ou fechamento)"],
  "passo": "falta concluir antes de encerrar a onda"
}
```

Em texto, as linhas `próximo passo:` e `falta:`. O `orq onda proxima` só sugere; quem encerra é o coordenador, com `orq onda encerrar <id>`, ou o daemon, na [limpeza depois do merge](#limpeza-depois-do-merge). O TUI marca a faixa da onda pronta com o selo `concluída`, pela mesma regra.

### Limpeza depois do merge

Depois do merge, o orq limpa sozinho o que o coordenador criou para a US, sem perguntar. Se for preciso corrigir algo depois do teste, o coordenador recria worktree e panes a partir da `main`, numa branch nova, grava os recursos de novo e liga a MR nova; o `limpar` age outra vez no merge seguinte.

**Recursos conhecidos desde o despacho.** O coordenador grava, por US, o que criou, com `orq recursos gravar`. O arquivo é `recursos.json` na pasta de trabalho `~/tmp/orquestrador/<projeto>/TG-<ref>` (`ORQ_TRABALHO_DIR` troca a raiz):

```json
{
  "versao": 1, "projeto": "orq", "ref": 13,
  "repos": [{"repo": "/home/u/Projetos/orq", "worktree": "/home/u/Projetos/orq.TG-13", "branch": "TG-13"}],
  "panes": ["tg-13_executor", "tg-13_revisor"],
  "trabalho": "/home/u/tmp/orquestrador/orq/TG-13", "tmp": "/tmp/orq/orq/TG-13",
  "aprovados": {"https://github.com/BasisTI/orq/pull/9": "a1b2c3d"}
}
```

Não há migration nova: o estado é esse arquivo e o diário. Por isso a atualização dos binários (`orq upgrade`) não depende de ordem; só o daemon antigo, que não limpa, precisa ser reiniciado para passar a limpar.

**Convenção para os workers** (o coordenador põe nos pacotes):

- `TMPDIR=/tmp/orq/<projeto>/TG-<ref>` (`ORQ_TMP_DIR` troca a raiz), passado com `--env TMPDIR=…` no `herdr tab create`. `orq recursos gravar` imprime o valor e já cria a pasta.
- Contêineres, volumes, redes e imagens que o worker criar levam o rótulo `orq.us=<projeto>/<ref>` e `orq.onda=<id da onda>`. Num `docker compose`, o projeto se chama `orq-<projeto>-<ref>` e os serviços levam os rótulos em `labels:`.
- O Testcontainers limpa o que cria pelo Ryuk, desde que ele não esteja desligado: `orq recursos gravar` avisa se `TESTCONTAINERS_RYUK_DISABLED=true` ou `ryuk.disabled=true` em `~/.testcontainers.properties`.

**O que `orq limpar <ref>` faz.** Só começa se a US tem `recursos.json` e MR, e se cada MR está `mesclada` na forja (consulta na hora, não o cache) com o head igual ao de `aprovados`, que é obrigatório: conteúdo integrado não prova que o head foi aprovado, então sem `--aprovado <url>=<sha>` registrado para cada MR a limpeza recusa. Também recusa, sem `--forcar`, se o `recursos.json` não registra repo nem pane (worktrees, branches e panes ficariam órfãos); a simulação mostra então os panes `tg-<ref>_*` soltos. Senão sai com `limpeza_recusada` (exit 4), sem remover nada, e grava `limpar.recusada` no diário. Depois, na ordem:

| Item | Regra |
|---|---|
| panes | Só os nomes de agente listados em `panes` do `recursos.json` (nome parecido não prova posse: o Herdr é compartilhado com outros boards). Antes de fechar, confere o Taiga ao vivo (a US precisa estar em status fechado, `Ready for test`, `Ready to test` ou `Waiting for deployment`; `In progress` depois do merge e falha na consulta preservam), que o nome e o `PaneID` do Central não são de outra US e que o `cwd` ou o `foreground_cwd` do pane (sem o sufixo ` (deleted)`) está num worktree do `recursos.json`; sem como conferir, preserva e diz o motivo. Fecha só com o agente `idle`/`done`, relido do Herdr por último, logo antes de fechar. Nunca fecha o pane do coordenador nem o que roda o comando. |
| worktree | Registrado no repositório, na branch da US, sem árvore suja, fora do diretório corrente e com o conteúdo já na branch de destino. Sem `--force`. |
| branch local e remota | Mesma conferência do conteúdo. A remota só é apagada se ainda está no commit conferido (`--force-with-lease`). Nunca `main`, `master`, `develop`, `trunk` nem a branch de destino da MR. |
| pasta temporária | `/tmp/orq/<projeto>/TG-<ref>`. |
| Docker | Contêineres, redes, volumes e imagens com o rótulo `orq.us=<projeto>/<ref>`, nessa ordem, com os ids listados antes e o rótulo conferido de novo antes de cada remoção. Nunca `prune`. |
| pasta de trabalho | `~/tmp/orquestrador/<projeto>/TG-<ref>`, por último, e só se todo o resto foi resolvido: ela guarda o `recursos.json`, e sem ele o daemon esqueceria o que ficou. |

**Exclusão mútua e reabertura.** O `orq limpar` segura um `flock` exclusivo da US, em `<ORQ_TRABALHO_DIR>/<projeto>/.TG-<ref>.lock` (fora da pasta que ele apaga), do plano ao último item; `orq recursos gravar` e `orq recursos reabrir` seguram o mesmo, esperando até 2 min uma limpeza em curso. Com a trava ocupada, o `limpar` recusa (o daemon tenta de novo). Para corrigir depois do teste, a ordem é: (1) `orq recursos reabrir <ref>`, que só volta depois de qualquer limpeza em andamento e deixa a US "reaberta"; (2) recriar o worktree a partir da `main`, numa branch nova e **de preferência num caminho novo** (sufixo da rodada, ex. `…TG-13-r2`), para uma limpeza velha nunca apontar para o checkout novo (reusar o caminho funciona, e o `recursos gravar` avisa); (3) `orq recursos gravar` com o worktree novo e `orq mr add` com a MR nova. Enquanto a US não tem MR fora das encerradas, o `limpar` recusa. O `reabrir` só vale depois do merge: confere na forja que cada MR ainda não encerrada está mesclada (com o head aprovado registrado, se o arquivo existe) ou fechada, e recusa com `reabrir_recusado` se não. As MRs encerradas já passaram pelo gate na geração delas, então o `limpar` só exige "mesclada" e `aprovados` das MRs da geração atual (por isso a limpeza completa da geração anterior, que apaga o `recursos.json`, não trava a seguinte); uma MR nova sem aprovação continua recusando. Recriar o worktree sem o passo 1 não é protegido pela trava, só pelas conferências abaixo.

Antes de cada remoção o orq confere que o plano continua valendo: o `recursos.json` e o conjunto de MRs da US são os mesmos de quando foi conferido, e o worktree segue na branch e no commit conferidos e limpo. Se o coordenador reabriu a US no meio (outra branch, MR nova, recursos regravados), nada mais é removido e tudo vira `preservado`.

O conteúdo "já na branch de destino" é: o commit é o head mesclado da MR, ou mesclar o commit em `origin/<destino>` não muda a árvore (cobre merge, fast-forward e squash, em que a conferência é por conteúdo, não por histórico). Se a `main` mexeu depois nas mesmas linhas, não dá para provar e o recurso fica preservado. `git fetch --prune` roda antes (é a única mudança do `--dry-run`, só nas referências remotas).

**Mesmo `ORQ_TRABALHO_DIR`.** O daemon e o coordenador (os comandos `orq recursos`, `orq limpar`) precisam resolver o mesmo `ORQ_TRABALHO_DIR` (e o mesmo `ORQ_TMP_DIR`): é dele que saem o `recursos.json` e a trava. O daemon do plugin lê as variáveis do `orq.env`, não do shell do coordenador; com valores diferentes, ele não vê os recursos gravados (nunca limpa) e a trava deixa de excluir os dois. Confira com `orq recursos ver` e `orq plugin doctor`.

**Limite de confiança.** A lista `panes` do `recursos.json` é a autoridade de posse dos panes, e o arquivo é confiável no mesmo nível do usuário local: quem pode escrever nele já opera como esse usuário no Herdr. O orq não o autentica; ele só detecta mudança entre o plano e a execução.

Nunca se apaga caminho fora das raízes: as pastas são derivadas da raiz, do projeto e da ref; o caminho que o `recursos.json` declare diferente do derivado, um link simbólico (no próprio caminho ou nos pais) e uma raiz vazia, `/` ou `$HOME` viram `preservado`.

Cada item sai em `removido`, `preservado` (com o motivo), `falhou` ou `ausente` (já não existia). Cada um que não é `ausente` vira um evento no diário (`limpar.removido`, `limpar.preservado`, `limpar.falhou`). Um pane que não é `idle`/`done` preserva também o resto (worktree, branches, pastas, Docker), porque o agente pode estar usando; o mesmo vale para quem depende de um item preservado. Sem repo registrado, a pasta de trabalho (com o `recursos.json`) fica. Worktrees `TG-<ref>*` fora do `recursos.json`, nos repos registrados (`worktree_nao_registrado`, exceto os `prunable`), aparecem preservados e seguram a pasta de trabalho; panes `tg-<ref>_*` ou `<projeto>-tg-<ref>_*` fora dele (`pane_nao_registrado`) só aparecem, porque o nome não prova a posse num Herdr compartilhado, e não seguram nada. `--forcar` não libera a pasta com worktree solto.

**No merge, pelo daemon.** A cada `--intervalo-remoto`, o daemon roda o `limpar` nas US com MR, todas `mescladas` no cache, e `recursos.json`. Quando algo fica preservado, tenta de novo a cada 5 min e só grava no diário se o desfecho mudou. Quando a onda aberta (que não é só de plano) está pronta para encerrar (a regra de [Encerrar a onda](#encerrar-a-onda)) e nenhuma US dela tem mais `recursos.json`, o daemon remove o Docker com `orq.onda=<id>` e só então a encerra (`onda.encerrar`, `limpar.onda`); se o Docker não limpa por completo (volume em uso, Docker fora do ar), a onda segue aberta e ele tenta de novo a cada 5 min. `ORQ_LIMPEZA_AUTOMATICA=0` (também `false`, `nao`, `off`), no ambiente ou no `orq.env` do plugin, desliga as duas coisas; o padrão é ligado, e o daemon já em execução precisa ser reiniciado para ler a mudança. US sem `recursos.json` não são tocadas, e o orq não faz faxina de resíduos antigos.

## Licença

Apache License 2.0. Veja [LICENSE](LICENSE). Copyright 2026 Basis Tecnologia da Informação S.A.
