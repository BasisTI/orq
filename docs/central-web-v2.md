# Central web v2 (spec da implementação)

Spec da US #21. Ela diz como implementar, em `internal/web`, o que o protótipo aprovado mostra. O que a tela é, para quem e por quê está em [`PRODUCT.md`](../PRODUCT.md); o tema, os componentes e as regras de selo e de cor, em [`DESIGN.md`](../DESIGN.md); os dados, em [`docs/snapshot.md`](snapshot.md); as rotas e a segurança de hoje, no README (seções "Pendências com contexto e resposta pela web" e "Central pela web"). Este documento só registra o que muda.

- **Protótipo aprovado:** `https://cedagente-orq.basisti.com.br/prototipo/` (atrás de login). Aprovado como composição em 2026-10-22. Ele é descartável e **não** entra no repositório; seus dados são sintéticos.
- **Origem das decisões:** cada decisão abaixo traz a origem entre parênteses: `P` é o retorno da pessoa sobre o protótipo (2026-10-22), `R` é o README, `S` é o `docs/snapshot.md`, `H` é o código do htmx 2.0.6 lido para esta spec.
- **Revisada** por revisão independente (r1, sobre o head `cc04725`); as correções desta versão estão no relatório da rodada r3. Os achados citados abaixo como B/S/M são os dessa revisão.
- Estado atual conferido no `main` (`9e811b6`): rotas em `internal/web/web.go`, modelos em `internal/web/tmpl/`, um `estilo.css` único embutido, CSP `default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'`, sem JS.

## 1. O que muda, em uma tela

A web deixa de ter "pendências" como página inicial e a Central como página à parte. Passa a ter uma Central só, com abas (a troca de rotas é feita por último, §2.2):

- **Todos** e **uma aba por projeto** (slug do Taiga): as ondas abertas em accordion, e dentro delas as US em accordion, com MRs e pendências.
- **Pendências**: as esperando você e as respondidas, com filtros em cascata (projeto → coordenador → onda).
- **Responder** passa a ser feito no lugar (na US ou na aba Pendências), sem trocar de página, por `hx-post`. A página `/p/…` continua para link direto e para quem está sem JS.

A web continua somente leitura, **exceto** pela resposta às pendências (R). Merge, status, encerramento e despacho não ganham botão (P).

## 2. Arquitetura em `internal/web`

### 2.1 Regra central: a tela sai do snapshot

**A tela é montada a partir do `central.Snapshot`, nunca de consultas ao banco feitas pelo handler.** O projeto multi-máquina (fora do escopo, §9) vai trocar a fonte do snapshot; a tela não pode precisar ser refeita. Isso vira três regras de código:

1. **Uma função pura por tela:** `vistaCentral(snap central.Snapshot, agora time.Time, f Filtro) Central`. Não abre banco, não chama ferramenta, não lê relógio (recebe `agora`). Os testes alimentam um `Snapshot` montado à mão (§8).
2. **Uma interface de leitura**, no pacote `web`, em vez de `*central.Store` espalhado:

   ```go
   type Fonte interface {
       Snapshot(ctx context.Context, incluirEncerradas bool) (central.Snapshot, error)
       VerPendencia(ctx context.Context, coord *central.Coordenador, p refs.Pend) (central.Pendencia, error)
       HistoricoRespostas(ctx context.Context, p refs.Pend) ([]central.RespostaRegistrada, error)
       LerUS(ctx context.Context, u refs.US) (central.US, error)
       ListarPendencias(ctx context.Context, f central.FiltroPendencias) ([]central.Pendencia, error)
       ContarPendencias(ctx context.Context, f central.FiltroPendencias) (int, error)
       Diario(ctx context.Context, coord *central.Coordenador, f central.FiltroDiario) ([]central.Evento, error)
       ContarDiario(ctx context.Context, coord *central.Coordenador, f central.FiltroDiario) (int, error)
   }
   ```

   `*central.Store` a implementa; **os nomes e assinaturas acima são os do `Store` no `main` (`9e811b6`)**: `VerPendencia`, `HistoricoRespostas`, `LerUS`, `ListarPendencias`, `ContarPendencias`, `Diario` e `ContarDiario` existem hoje e são os que o `internal/web` já chama. A story 2 só confere se algum mudou até lá. A **escrita** continua pelo `Store` (`ResponderPendencia`), e é a única (R).
3. **O snapshot é lido uma vez por requisição** e o resultado serve a página inteira. Dois handlers na mesma requisição não podem ver instantes diferentes.

### 2.2 Rotas

A troca de `/` e de `/central` é **a última** a acontecer (story 3, §11): até lá, a web nunca fica sem a lista de pendências. A tabela mostra o **estado final**; a coluna "Story" diz onde cada linha entra.

| Rota | Antes | Depois | Story |
|---|---|---|---|
| `GET /central` | Central | **a Central nova** (aba Todos) até a story 3; depois, `308` para `/` | 2 (nova), 3 (308) |
| `GET /` | pendências abertas | lista de pendências **atual**, no visual novo, até a story 3; depois, **Central, aba Todos**. Com `coord`, `projeto` ou `onda` na query (os filtros da página antiga), `308` para `/pendencias` com a mesma query | 3 |
| `GET /projeto/{slug}` | não existe | aba de um projeto; as ondas com ao menos uma US dele; 404 se o slug não tem onda aberta | 2 |
| `GET /pendencias` | não existe | aba Pendências, filtros `?projeto=&coord=&onda=`; com `HX-Request`, só o fragmento `#pend-conteudo` (exceção abaixo) | 3 |
| `GET /p/{projeto}/{ref}/{n}` | detalhe | **continua**, no visual novo: link direto, sem accordion | 3 |
| `GET /p/{projeto}/{ref}/{n}/corpo` | não existe | **fragmento** do corpo da pendência (contexto, opções, formulário); carregado na primeira abertura do accordion | 3 |
| `POST /p/{projeto}/{ref}/{n}/responder` | grava e redireciona | **mesmo contrato** (§5.3); com `HX-Request` devolve fragmento, sem ele continua redirecionando (post/redirect/get) | 3 |
| `GET /vivo` | não existe | **fragmento** da atualização de 30 s (§6) | 2 |
| `GET /onda/{id}` | diário | continua, no visual novo | 4 |
| `GET /historico` | resolvidas | continua; ganha link no rodapé da aba Pendências | 3 |
| `GET /saude` | saúde | continua, sem mudança | – |
| `GET /estatico/{arquivo}` | `GET /estilo.css` | **novo**: CSS, `htmx.min.js`, `app.js`, fontes e imagens embutidos (§4); `/estilo.css` é removido | 1 |

- **Por que essa ordem (S1).** O fluxo real é mesclar a story e rodar `orq upgrade` no daemon real (R: "Atualização"); a pessoa responde pendências pelo celular. Se a story 2 trocasse `/` pela Central, a pessoa ficaria sem lista de pendências até a story 3 (a Central nova ainda não tem pendência nas US). Por isso a story 2 entrega a Central nova em **`/central`** (a rota que já existe e que a pessoa e a skill do coordenador já usam), e as abas dela apontam: Todos → `/central`, projeto → `/projeto/{slug}`, Pendências → `/` (a lista atual). A story 3 inverte: `/` vira a Central, `/central` vira 308 e a aba Pendências aponta para `/pendencias`. **A cada merge a web tem os dois caminhos funcionando.**
- **Só `GET` e o `POST` de resposta.** `POST` nas demais, `PUT`, `DELETE` e `PATCH` continuam `405` (o que a #16 garante, R). O `/vivo` e os `/corpo` são `GET` e não gravam.
- **O que é fragmento htmx e o que é página:** são fragmentos `/vivo`, `/p/…/corpo`, a resposta do `POST` com `HX-Request` e `/pendencias` com `HX-Request`. Tudo o mais é página inteira. **Página inteira funciona sem JS** (a lista das ondas, os accordions por `<details>` e o `POST` por formulário); o JS só melhora (P: "fácil de entender").
- **Exceção: restauração do histórico (B3).** O htmx, ao restaurar a página no botão Voltar com `historyCacheSize:0`, pede a URL de novo e **troca o `<body>` pela resposta**. Com `HX-Request: true` o servidor devolveria só o fragmento e a página viraria o fragmento (provado no Chromium). Duas defesas, as duas obrigatórias: (1) a meta do htmx traz `"historyRestoreAsHxRequest":false`, e o pedido de restauração sai **sem** `HX-Request`; (2) o servidor trata `HX-History-Restore-Request: true` como página inteira, **mesmo que `HX-Request` venha**. A regra "com `HX-Request`, só o fragmento" vale **só quando não há `HX-History-Restore-Request`**. O `HX-Replace-Url` funciona com cache 0 (provado).
- **Mudanças de comportamento a avisar (M16):** (a) `/` deixa de ser a lista de pendências; o aviso que o daemon manda ao coordenador, o `registrada.json`, o `orq pend ver` e o TUI usam `/p/…` (`refs.URLPendencia`), que **não muda**; (b) os filtros `GET /?coord=&projeto=&onda=` da página atual passam a `308` para `/pendencias` com a mesma query, em vez de serem ignorados em silêncio pela aba Todos; (c) `/central` e `/onda/<id>` aparecem no README (tabela de rotas, "Central pela web") e na skill `orquestrador` (`references/central.md`, que manda o coordenador enviar `<ORQ_WEB_URL>/central` à pessoa); o `308` os mantém vivos, mas o **README é atualizado na story 3** e a **skill é atualizada pelo coordenador** depois do merge dela (§11); (d) `/estilo.css` some (só o `base.html` e o README o citam). `ORQ_WEB_URL` não é afetado. A regra por caminho no Caddy (operação, fora do `orq`) não foi lida: se existir para `/` ou `/estilo.css`, é preciso conferir.
- **Modelos:** `html/template` como hoje, embutidos. Um `base` (cabeçalho, abas, rodapé, `<link rel="icon">`) e um modelo por tela; os fragmentos são `define`s reaproveitados pela página inteira e pelo `/vivo`, para o mesmo HTML nunca existir duas vezes (§5.2).

### 2.3 O que sai do `Store.Snapshot` e o que precisa de consulta nova

O snapshot já traz tudo o que as abas, as barras e as linhas precisam. **Nenhuma consulta nova ao banco é necessária para as telas.**

| Elemento da tela | Origem no snapshot |
|---|---|
| contagem de ondas por aba (Todos, projeto) | `coordenadores[].ondas[]` (só abertas, o padrão do snapshot); projeto de uma onda = conjunto de `us[].projeto` |
| contagem da aba Pendências | `pendencias_abertas[]` com `estado == "aberta"` |
| barra da onda | `Onda` (`id`, `titulo`, `autorizacao_ate`, `ordem`), `Coordenador.nome`, `us[].status` (prontas/total) |
| pendências abertas da onda | `Pendencia.onda_id` e `estado` |
| sinal agregado das MRs | `MR.estado`, `pipeline` das MRs não `mesclada` nem `fechada` |
| selo `concluída` | a regra de [Encerrar a onda](../README.md#encerrar-a-onda), já usada pelo TUI e por `/central` (`central.EncerramentoDe(o).Pronta`) |
| estado do agente, `vencido` | `US.herdr.status` e `central.Vencido("herdr", …)`; **nome, pane e workspace nunca chegam ao modelo** (R) |
| status no Taiga, link | `US.taiga` |
| MR: estado, pipeline, aprovação, `vencido`, "consulta extra falhou" | `MR.*`, `situacao_em`, `situacao_erro` (US #16 e #19, S) |
| idade das fontes | `fontes.*` |
| cor do projeto | `projetos[].indice` (4 tons no protótipo; o TUI tem 8) |
| opções da cascata | do snapshot (§7), **não** das pendências como hoje |
| cabeçalho "quem sou" | `usuarioDe(r)` |

**O que continua precisando de consulta além do snapshot:**

- **O detalhe da pendência** (até 64 KiB cada, S: "o `detalhe` não vai no snapshot"). Por isso o accordion da pendência traz só o resumo na página e busca o corpo em `/p/…/corpo` na primeira abertura (`hx-trigger="toggle once"`). Uma Central com 20 pendências não carrega 20 detalhes por padrão. Usa `VerPendencia` e `HistoricoRespostas`, que o `detalhe` já usa.
- **O histórico de respostas** (`HistoricoRespostas`), que só a página `/p/…` e o corpo mostram.
- **O Taiga e as MRs de uma US** na página `/p/…` (`LerUS`).
- **Regra do accordion de pendência (M6):** o servidor **nunca serve `<details open>` em pendência**. Um `<details>` servido aberto não dispara `toggle` (0 pedidos de corpo na carga) e o primeiro fechamento consome o `once`: o corpo nunca carregaria. Aberto só por clique, ou por `app.js` ao seguir `#hash` (isso dispara `toggle`, provado).
- **O diário da onda**, como hoje.

Resultado: o custo de uma página das abas é o de **um `Snapshot`** (uma transação de leitura, sem consulta externa: a leitura é do cache que o daemon grava, S).

### 2.4 Estados que a vista deriva (sem gravar nada)

- **Ordem das ondas: por atenção** (P, decisão 1), depois por id: 3 = tem pendência esperando; 2 = tem MR aberta com pipeline `falhou`; 1 = tem MR `pronta`; 0 = o resto. Uma nota na página diz a ordem. É a única ordem diferente da do snapshot (S: "ondas por id"); a vista reordena.
- **Selo de MR depende do limite da onda:** em onda `até merge`, MR `pronta` é `espera merge`; em `até mr-pronta` e `até plano`, é `pronta` (o merge não está autorizado ali).
- **"Esperam você"** conta pendências `aberta` (sem resposta). A **respondida sai** dessa contagem, mas **segura o `concluída`** até o coordenador resolver (P, decisão 2; R: "a respondida continua contando como aberta para encerrar a onda"). A tela mostra as duas coisas sem contradição: o selo some e o `concluída` só aparece quando não há pendência aberta nem respondida.
- **Estados do agente em português**, com o código do snapshot no `title` (P, decisão 3): `working` trabalhando, `idle` ocioso, `done` concluído, `blocked` bloqueado, `unknown` sem agente, `ausente` pane ausente, nulo "sem resposta".
- **Dado vencido nunca finge ser atual** (R): a regra de hoje (`central.Vencido`, 3 × o TTL) vale para o agente, o Taiga, a MR e a fonte, e a linha de situação avisa quando alguma fonte está vencida.

## 3. Marcação e acessibilidade (o que a implementação reproduz do protótipo)

- **Accordions são `<details>/<summary>`.** Funcionam sem JS, o teclado e o leitor de tela já os entendem, e o estado aberto é do navegador. O endereço com `#pend-…`, `#us-…` ou `#onda-…` abre a cadeia de ancestrais (a única necessidade de JS próprio, §4.3).
- **Aba ativa por `aria-current="page"`**, nunca por classe; o CSS desenha a partir do atributo.
- **Estado de selo nunca só por cor:** texto sempre, ícone quando cabe; `desconhecido` e `sem estado` em contorno tracejado. Ícones em SVG inline de um sprite (um `<svg>` com `<symbol>`), com traço único.
- **Confirmação da resposta** por região `role="status"` (§5.3), não por modal.
- **Movimento só para estado:** o ponto do agente `trabalhando` pulsa e o item recém-respondido pisca uma vez; ambos desligam em `prefers-reduced-motion`.
- **Alvos de toque ≥ 44 px** nos botões e nas abas; as abas rolam na horizontal no celular.
- **Títulos longos** truncam em duas linhas na barra fechada e aparecem inteiros quando a onda abre.

## 4. Build do frontend

### 4.1 Pilha e versões exatas

Versões **exatas**, sem `^` nem `~`, travadas por `package-lock.json`:

| Pacote | Versão | Para quê |
|---|---|---|
| `tailwindcss` | `4.1.12` | utilitários |
| `@tailwindcss/cli` | `4.1.12` | compilar o CSS |
| `daisyui` | `5.0.46` | componentes, plugin `daisyui` e `daisyui/theme` |
| `htmx.org` | `2.0.6` | só o `dist/htmx.min.js` |
| `@fontsource/carlito` | `5.2.5` | Carlito 400 e 700, latin |
| `@fontsource/inter-tight` | `5.2.6` | Inter Tight 600 e 700, latin |

São as versões do protótipo (`@tailwindcss/browser@4.1.12`, `htmx.org@2.0.6`, `css/daisyui.css` idêntico ao `daisyui@5.0.46`). A skill `basis-web-frontend` fixa só as **fontes** (`tema-interno.md`); o `frontend-build.md` dela usa faixas (`^`), e aqui as versões são exatas de propósito. As seis existem no registro do npm. Subir versão é uma mudança de código com a regeneração do CSS (§4.4) no mesmo commit.

### 4.2 Estrutura

```text
internal/web/
  frontend/                 # só para quem regenera; NÃO vai para o binário
    package.json            # versões exatas
    package-lock.json
    input.css               # @import "tailwindcss" source(none); @plugin daisyui; tema basis-interno; @font-face
  estatico/                 # versionado e embutido por go:embed
    app.css                 # compilado e minificado
    htmx.min.js             # cópia de htmx.org@2.0.6/dist/htmx.min.js
    app.js                  # ~20 linhas próprias (§4.3)
    fontes/*.woff2          # as 4 fontes
    orq-icon-64.png         # cabeçalho e favicon (arte fornecida pela pessoa)
    orq-banner-light.png    # ver §10
  tmpl/*.html
scripts/build-frontend.sh   # o único ponto de regeneração
```

- **`//go:embed estatico`** (a **forma de diretório**, não `estatico/*`): a lista com `*` embute arquivos que começam com `.` ou `_` (`.DS_Store`, `_x`), a forma de diretório não. O teste (§8.1) lista o `embed.FS` e compara com uma **lista fechada** de arquivos; arquivo a mais ou a menos falha. **`go install` e `scripts/build-plugin.sh` seguem sem Node:** o `build-plugin.sh` só roda `go build` (R, "Plugin do Herdr") e passa a ter o CSS pronto no repositório. Node é dependência **só de quem edita o frontend**, e o `AGENTS.md` ganha essa linha.
- **O `input.css` usa `source(none)` com `@source "../tmpl/**/*.html"`.** O Tailwind trata como classe qualquer palavra do que varre, comentário inclusive; sem isso, classe citada em `docs/` ou em skill entraria no CSS (`tema-interno.md`). O `@source` aponta só para os modelos (e para o `app.js`, se usar classe).
- **Tema:** `@plugin "daisyui" { themes: false; }` e `@plugin "daisyui/theme"` com o `basis-interno` (valores no `DESIGN.md`), `default: true`. As sobrescritas do daisyUI que esmaecem texto vão em `@layer utilities` e **acompanham o seletor que o daisyUI 5.0.46 gera, não o de `tema-interno.md`** (S2): aquele arquivo traz o seletor da aba do 5.5.19 (`… [aria-current="true"], [aria-current="page"]`), e o 5.0.46 gera `.tab:not(:checked, label:has(:checked), :hover, .tab-active, [aria-selected="true"])` a 50%. **A tela não usa `.tab` do daisyUI** (as abas são `.abas-in a`, com `aria-current`), então a sobrescrita da aba **não entra** no `input.css`; se um modelo passar a usar `.tab`, o teste de contraste (§8.1) reprova até a sobrescrita, escrita para a versão travada, existir. Entram as de `.label`, `thead`/`tfoot`, `.stat-*` e `.menu-title` se o build as gerar (o teste diz), o botão sem variante, e duas medidas no protótipo: o `placeholder` (3,07:1 no daisyUI, vai a `#5B6B7A`, 5,48:1) e o contorno de `.radio`, `.select` e `.textarea` (`--input-color: #66768A`, 4,64:1; o do daisyUI dá 1,48:1).
- **Fontes servidas pelo binário, sem Google Fonts em produção** (CSP `font-src 'self'`). Os `.woff2` vêm do `@fontsource` (licença OFL; o texto da licença vai em `estatico/fontes/LICENSE-*.txt`). `font-display: swap`.
- **Imagens:** o ícone (64 px) e o banner (880×440) são a arte da pessoa, reduzidos; a procedência está no relatório da US #21 r1.

### 4.3 O JS próprio

Um arquivo, `estatico/app.js`, sem inline e sem dependência: abre os `<details>` ancestrais de `location.hash` (na carga e em `hashchange`) e rola até o alvo; e, na faixa de abas, rola a aba ativa para a vista e marca `data-esq`/`data-dir` onde há aba escondida (o CSS esmaece a borda; story 2, item d). Nada mais. A confirmação, a troca de contagens e o filtro são do htmx e do servidor.

### 4.4 Como regenerar

```bash
scripts/build-frontend.sh                    # npm ci, compila, copia htmx e fontes para internal/web/estatico
git add -N internal/web/estatico && git diff --exit-code -- internal/web/estatico   # vazio se nada mudou
```

O script faz, nesta ordem, a partir da raiz do repositório (**S4: o comando precisa rodar de dentro de `frontend/`**, onde está o `node_modules`):

```bash
cd internal/web/frontend
npm ci --no-audit --no-fund                  # nunca `npm install`
npx --no-install tailwindcss -i input.css -o ../estatico/app.css --minify
cp node_modules/htmx.org/dist/htmx.min.js ../estatico/
cp node_modules/@fontsource/…/files/….woff2 ../estatico/fontes/   # as 4 fontes
```

- O `@tailwindcss/cli` instala o binário `tailwindcss`; o pacote `tailwindcss` v4 **não tem `bin`**, então um `npx tailwindcss` rodado **fora** de `frontend/` vai ao registro, não acha executável (`could not determine executable to run`) e, num terminal interativo, ofereceria instalar uma versão **não travada**. `--no-install` e o `cd` evitam as duas coisas.
- **Node 24 LTS**, de propósito (M10): é o que a máquina de desenvolvimento tem e o que o `frontend-build.md` da skill usa (`v24.14.0`). A versão fica em `frontend/.nvmrc` e o CI usa o mesmo arquivo. **O determinismo do build foi provado só no Node 24** (três builds, `rm -rf node_modules && npm ci`, cópia em outro diretório: mesmo hash); em outro Node não foi testado, por isso a versão é fixa. O `package-lock.json` traz as 12 entradas `@tailwindcss/oxide-*` e o `npm ci` roda em `ubuntu-latest`.
- Quem mexe em modelo ou `input.css` roda o script e commita o resultado.

### 4.5 Job de CI

Um job novo, `frontend`, ao lado do `unit` em `.github/workflows/ci.yml`:

1. `actions/setup-node` com a versão do `frontend/.nvmrc` e cache do `npm`;
2. `scripts/build-frontend.sh`;
3. **arquivo novo também conta (S5):** `git add -N internal/web/estatico` e `git diff --exit-code -- internal/web/estatico` (ou, equivalente, `test -z "$(git status --porcelain -- internal/web/estatico)"`). O `git diff` sozinho **não vê arquivo não rastreado**: uma fonte, uma licença ou um PNG gerados e esquecidos passariam no job e o `main` ficaria sem eles no binário (provado: `exit 0`). **Falha se o CSS, o `htmx.min.js`, as fontes ou qualquer arquivo de `estatico/` versionado não bate com o gerado**, com a mensagem "rode scripts/build-frontend.sh e commite internal/web/estatico";
4. **o teste de contraste do tema** (S3), `npm run contraste` em `frontend/`: compila o CSS **sem `--minify`** num diretório temporário (esse arquivo **não** é versionado nem embutido) e confere, com um porte do `ContrasteDoTemaTest.java` da skill, que nenhum texto ativo fica abaixo de 70% sem sobrescrita de mesmo seletor. O teste da skill lê o CSS **linha a linha**: no CSS minificado ele acha **zero** regras e passa vazio, e por isso roda sobre o não minificado. O teste tem o guarda de "achou regras" (falha se não encontra nenhuma) e se testa com CSS sintético, como o original.

O job `unit` **não instala Node**: se alguém depender dele, o `go test` quebra no CI, que é o que se quer provar. O `go test` confere só o que não precisa de Node (§8.1).

## 5. CSP, segurança e a resposta pela web

### 5.1 CSP

```text
default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self';
connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'
```

- **Sem `unsafe-inline` e sem `unsafe-eval`.** Nenhum `<script>` e **nenhum atributo `style=""`** nas páginas (M4). O protótipo tem 10 `style=` na Central (25 nas páginas todas): a **barra de progresso** (`style="width:67%"`) e o **sprite de ícones** (`<svg style="position:absolute">`); os dois violam `style-src 'self'` (`style-src-attr`, com `console.error` por atributo, provado). Na implementação a barra vira o elemento nativo `<progress value max>` e o sprite vira `<svg class="sprite" …>` com a classe no `app.css`. O htmx em si não viola nada: com `includeIndicatorStyles:false` ele não injeta `<style>`, e as mutações dele são só de `class` e `disabled`.
- **O que o htmx 2.0.6 exige (H, lido no `htmx.js` e provado no Chromium):** o `eval` e o `new Function` (4 pontos) estão todos dentro de `maybeEval`, que respeita `htmx.config.allowEval`; com `false`, ele dispara `htmx:evalDisallowedError` e **não executa**. **Alcance exato (M5):** isso corta `hx-on:*` e `hx-vals="js:…"`. **Não corta o filtro de gatilho** (`every 30s [cond]`): com `allowEval:false` a condição vira **sempre verdadeira** (o pedido é feito, mais o evento de erro). A tela não usa nenhum dos três. `allowScriptTags:false` impede que um `<script>` de um fragmento seja executado (**M7**: não remove atributos de evento inline como `<img onerror>`; quem barra esses é a CSP, pelo `script-src` sem `unsafe-inline`; é defesa em profundidade, o fragmento já sai de `html/template`). `historyCacheSize:0` impede o htmx de guardar páginas, mas **o cache de histórico dele é `sessionStorage`, não `localStorage`** (M8); com 0 a chave de páginas nunca aparece, e só `htmx-current-path-for-history` (a URL) continua gravada.

  ```html
  <meta name="htmx-config" content='{"allowEval":false,"allowScriptTags":false,"includeIndicatorStyles":false,"historyCacheSize":0,"historyRestoreAsHxRequest":false,"responseHandling":[{"code":"204","swap":false},{"code":"[23]..","swap":true},{"code":"[45]..","swap":true,"error":true}]}'>
  ```

  `selfRequestsOnly` já é o padrão. A meta não é script, então a CSP não a barra (provado). `historyRestoreAsHxRequest:false` é do **B3** (§2.2).
- **`error:true` tem custo (S6):** cada resposta 4xx/5xx gera **dois** `console.error` (o do navegador, "Failed to load resource", e o do htmx, "Response Status Error Code"). O teste de navegador (§8.1) aceita esses dois **só nos pedidos de erro que ele mesmo provoca**, por uma lista de permissões, e continua falhando em qualquer outra mensagem. A story 1 também põe `<link rel="icon" href="/estatico/orq-icon-64.png">` no `base`: sem ele, toda carga pede `/favicon.ico`, recebe 404 e gera um erro de console.
- Cabeçalhos que já existem seguem: `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`.
- **Cache, decidido (M3, M12):** páginas, fragmentos e `/vivo`: `Cache-Control: no-store`. Estáticos: **nunca `public`**. A web fica atrás de `basic_auth`, e `public` é o que autoriza um cache compartilhado a guardar resposta a pedido com `Authorization` (RFC 9111 §3.5); o dano seria nulo hoje (só CSS, fontes e htmx), mas não há razão para abrir a porta. Dois grupos: (a) `/estatico/app.css?v=<hash>`, `/estatico/htmx.min.js?v=<hash>` e `/estatico/app.js?v=<hash>` (o hash do conteúdo embutido, calculado na partida): `Cache-Control: private, max-age=31536000, immutable`; (b) fontes e imagens, que o Tailwind deixa **sem hash** dentro do `app.css` (`url(/estatico/fontes/….woff2)`): `Cache-Control: private, max-age=86400`. O custo é uma revalidação por dia; o ganho é não precisar reescrever as `url()` do CSS. Não testado em Caddy.

### 5.2 O que continua igual

- **`CrossOriginProtection`** em volta de tudo (R): `POST` de outra origem recusado (403). O `hx-post` do htmx é da mesma origem, então passa. Nada de token CSRF novo.
- **Quem responde:** o `respondida_por` vem do cabeçalho do proxy (`X-Orq-User`, `--web-cabecalho`) no socket Unix, e **a requisição sem ele é recusada com 401** (só `/saude` passa); em TCP o cabeçalho é ignorado e vale `local`; `Host` conferido em TCP (421). O fragmento, o `/vivo` e os `/corpo` passam pelo mesmo `comUsuario`: **sem o cabeçalho no socket Unix, 401 também neles.**
- **Escape:** tudo vem de `html/template`; o markdown do detalhe continua pelo goldmark **sem `WithUnsafe`** (HTML bruto escapado, só `http`, `https`, `mailto`, âncora e relativo viram link, imagem perde o endereço). **Texto de agente, título de onda e de US e resposta da pessoa nunca são `template.HTML`.** A vista não tem campo `template.HTML` além do detalhe renderizado.
- **Nada de pane no diário** (R, #16): a lista `camposDoDiario` e o `[pane]` dos textos seguem como estão. O diário novo reaproveita a função, não a reescreve.
- **Limites do POST:** corpo até 16 KiB (413), resposta até 8 KiB (413), controles recusados (400), `Content-Type` `application/x-www-form-urlencoded` (415). O `hx-post` envia exatamente isso.
- **Métodos recusados:** só `GET` e o `POST` de resposta (405 nos demais); `OPTIONS` e `HEAD` seguem o comportamento do `net/http`.
- **Log:** **padrão da rota e status** (`web <rota> <status>`, como `comLog` faz hoje; **sem o método**; o README descreve o mesmo, e a story 3 o corrige). As rotas novas entram com o padrão (`/p/{projeto}/{ref}/{n}/corpo`, `/vivo`), nunca com o caminho nem a query.

### 5.3 A resposta por `hx-post`

**Contrato do pedido (não muda):** `POST /p/{projeto}/{ref}/{n}/responder`, `application/x-www-form-urlencoded`, campos `opcao` (opcional, inteiro ≥ 1) e `texto`. O formulário do protótipo usa `comentario`; **na implementação o nome é `texto`**, o do contrato de hoje (`r.PostForm.Get("texto")`).

**O formulário** carrega `action` e `method="post"` (funciona sem JS) e `hx-post` para o mesmo endereço. **Todo elemento com `hx-get` ou `hx-post` declara o seu `hx-target` e o seu `hx-swap`** (M9): o `hx-swap` é **herdado** dos ancestrais, e um `<details hx-get=… hx-trigger="toggle once">` sem `hx-swap` sob um elemento com `hx-swap="none"` pede o corpo e **não o troca** (provado). O formulário declara `hx-target="closest .pend"` e `hx-swap="outerHTML"`. Marcas do protótipo que a implementação mantém: opção obrigatória quando há opções (`required` no rádio), comentário obrigatório quando não há. O formulário tem um espaço reservado `<div class="erro" role="alert"></div>` (vazio) para o erro (abaixo).

**A resposta do servidor (200, `HX-Request: true`):**
1. **na US** (a pendência está dentro da linha da US): o **item respondido e fechado** é a resposta principal, que o `outerHTML` do `closest .pend` troca no lugar. **Na aba Pendências** (o item muda de grupo): a resposta principal é **vazia** (o `outerHTML` com fragmento vazio **remove** o item de cima, provado) e o item respondido vai por oob para o grupo de baixo (item 2).
2. **mudança de grupo (S7):** o oob **precisa do embrulho**: `<div hx-swap-oob="afterbegin:#lista-resp"><div class="pend" id="pend-…">…</div></div>`. **Não** vale pôr `hx-swap-oob="afterbegin:#lista-resp"` no próprio `<div class="pend">`: para estilo ≠ `outerHTML`, o htmx insere o **conteúdo** do elemento, não o elemento, e o item some dos dois grupos (provado). Oob `true` com o mesmo `id` troca no lugar e o item **fica** no grupo de cima (provado), por isso não serve para mudar de grupo.
3. **grupo vazio (S7):** `#lista-resp` e `#lista-esp` **existem sempre** (o servidor os renderiza vazios), para o `afterbegin` ter alvo; a frase "Nenhuma pendência…" é um irmão (`<p id="vazio-resp">`, `<p id="vazio-esp">`) renderizado com `hidden` quando o grupo tem itens. A resposta troca por oob o `#vazio-esp` (sem `hidden`, se o grupo ficou vazio) e o `#vazio-resp` (com `hidden`) e atualiza os contadores dos grupos.
4. **contagens por oob `true` com `id` estável:** a aba Pendências, o selo `esperam você` da barra da onda e o da linha da US, a linha de situação.
5. `<div id="aviso" role="status" hx-swap-oob="true">` com "Resposta enviada: TG-78 P1. O coordenador é avisado." (some por animação de CSS, sem JS).

**Por que `hx-swap-oob` e não evento:** o servidor sabe todas as contagens (ele tem o snapshot) e o navegador não precisa saber onde cada uma está; um evento (`HX-Trigger`) obrigaria o JS próprio a buscar os números. O servidor decide **quais** ids incluir pela aba, lida do `HX-Current-URL` (que o htmx envia sempre, provado), com a função `abaDe(url)` que reconhece `/central`, `/` (e, depois da story 3, só `/`), `/projeto/{slug}` e `/pendencias`.

**Erros (B2): o 4xx não pode substituir o formulário.** Com o `responseHandling` da §5.1 (`swap:true` para 4xx/5xx), a resposta de erro seria trocada **no alvo do pedido** (`closest .pend`, `outerHTML`): o `<p role="alert">` ocuparia o lugar do item, o formulário sumiria e **o texto digitado se perderia** (provado: `formExiste:false`, `texto:null`). A correção é feita **pela resposta**, que já sabe que é erro: todo 4xx/5xx de fragmento devolve os cabeçalhos

```text
HX-Retarget: find .erro
HX-Reswap: innerHTML
```

e o corpo `<p>…</p>` com a mensagem. O `.erro` (`role="alert"`) é o espaço reservado do formulário; o formulário e o texto ficam. **Os dois cabeçalhos juntos:** só o `HX-Retarget` não basta, porque o `outerHTML` herdado do formulário apagaria o espaço reservado na primeira falha (provado). O servidor tem **uma função só** (`erroFragmento(w, status, alvo)`) para isso, usada pelos handlers de fragmento. A alternativa, `target` e `swapOverride` na entrada do `responseHandling` da meta, **foi recusada**: `target` é relativo ao disparador e um 4xx do `hx-get` do `/corpo` (que não tem `.erro` por perto) daria `htmx:targetError` e deixaria o accordion em "carregando…" (provado). **O `/corpo` não manda `HX-Retarget` nem `HX-Reswap` (S8).** O pedido dele já declara `hx-target="find .pend-corpo"` e `hx-swap="innerHTML"` (§5.3, M9), e o `responseHandling` da §5.1 já manda trocar o 4xx/5xx: o erro entra no `.pend-corpo` e o `<summary>` fica. `HX-Retarget: this` seria o próprio `<details>`, e `innerHTML` apagaria **todos** os filhos dele, o `<summary>` e o `.pend-corpo` (provado: `temSummary: false`, `temPendCorpo: false`; o accordion fica sem título e sem lugar para recarregar o corpo). `HX-Retarget: find .pend-corpo` dá o mesmo resultado que não mandar nada, então é dispensado. Por isso `erroFragmento(w, status, alvo)` **com `alvo` vazio omite os dois cabeçalhos**, e o `/corpo` o chama com alvo vazio. O `/vivo` não precisa disso: o elemento dele tem `hx-swap="none"`, e erro dele não troca nada (§6). Efeito de `error:true`: dois `console.error` por 4xx (S6, §5.1).

Status devolvidos, como hoje: 400, 409 `pendencia_resolvida`, 413, 415, 503.

**Trocar a resposta** (R: "a resposta pode ser trocada até a pendência ser resolvida"): o botão do item respondido faz `hx-get` do `/corpo?trocar=1` (com `hx-target` e `hx-swap` próprios), que devolve o formulário com a resposta atual indicada; a nova resposta gera um `pend.responder` e zera o aviso ao coordenador, como hoje. **Trocar não muda as contagens** (já saiu do "esperam você").

**O que não muda:** `central.ResponderPendencia`, o evento `pend.responder` no diário, o aviso ao coordenador pelo `Avisador`, e a regra "worker e revisor nunca respondem".

## 6. Atualização automática de 30 s

- **Elemento dedicado, com `hx-swap="none"` (B1).** O disparo **não** vai no `<body>`:

  ```html
  <div id="vivo" hx-get="/vivo?v=<hash>" hx-trigger="every 30s" hx-swap="none" hidden></div>
  ```

  Sem `hx-swap`, o htmx faz `innerHTML` no próprio alvo: um `/vivo` no `<body>`, cuja resposta só tem oob, **apagaria a página inteira no primeiro ciclo** (provado: `bodyChildren: 0`, `htmx:oobErrorNoTarget` nos ciclos seguintes; resposta vazia ou só espaço dá o mesmo). Com `hx-swap="none"` os oob são aplicados e `<details open>` e `<textarea>` sobrevivem (provado). **`hx-swap` é herdado (M9)**, e é por isso que o `none` fica num elemento **sem filhos** e nada na página depende dele: nenhum descendente herda. Todo outro `hx-get`/`hx-post` declara o próprio `hx-swap` (§5.3).
- **O que é consultado:** `GET /vivo?v=<hash>`, um por página aberta. A resposta é só `hx-swap-oob`:
  - a linha de situação e as contagens das abas;
  - a lista "Idade dos dados";
  - **as barras das ondas e as linhas das US**, trocando **só o `<summary>`** de cada uma (`<summary id="onda-61-barra">`), **nunca o `<details>`**. Provado: o oob em `<summary>` troca só o `<summary>`, preserva o `open` e o `<textarea>` digitado dentro, também na US aninhada;
  - na aba Pendências, os contadores dos grupos;
  - o próprio `#vivo`, por oob `true`, com o `v` novo (o hash corrente), para o próximo ciclo mandar o hash certo.
- **O que nunca é trocado:** o `<details>` (e portanto se está aberto), o corpo de uma pendência, o formulário e o que foi digitado nele. Nada com `<form>`, `<textarea>`, `<input>` ou `.pend-corpo` está na resposta do `/vivo`. **Teste:** um teste de `httptest` garante que o corpo do `/vivo` não contém essas marcas (§8.1).
- **Mudança de composição (onda nova, onda encerrada, pendência nova):** o oob não pode inserir nem remover itens sem pisar no que está aberto. O `/vivo` recebe `?v=<hash>` (hash de ids e ordem das ondas, ou das pendências da aba, calculado na renderização); se o hash do servidor difere, a resposta traz, além do resto, `<div id="aviso-lista" hx-swap-oob="true">A lista mudou. <a href="">Atualizar</a></div>`. A pessoa decide quando recarregar (aprovado pela pessoa em 2026-10-22). **A tela nunca reordena sozinha embaixo da mão de quem está lendo.**
- **Aba em segundo plano:** o navegador já reduz os temporizadores a cerca de 1 por minuto; a spec **não** usa filtro de gatilho de visibilidade, porque, com `allowEval:false`, a condição vira sempre verdadeira (§5.1).
- **O período recomeça no swap (M17).** Quando o oob troca o `#vivo`, o htmx arma um temporizador novo no elemento processado, e o do antigo morre: o ciclo efetivo é **30 s + a latência da resposta**, não 30 s de relógio. É irrelevante para a pessoa, mas o `BenchmarkVivo` da story 2 **não conta ciclos pelo relógio de parede**: mede a duração de **uma** resposta do `/vivo` (o `Snapshot` mais a renderização) e o número de respostas por pedido, não o intervalo entre elas.
- **Custo no daemon:** o `/vivo` roda no processo do daemon (`orq daemon --web`) e custa **um `Snapshot`**: uma transação de leitura do SQLite (WAL) sobre o cache que o daemon já gravou, sem chamar Herdr, forja nem Taiga (S). Uma pessoa por máquina (P) e algumas abas abertas dão uma leitura a cada 30 s por aba. A story 2 mede o tempo do `Snapshot` com a Central do protótipo (9 ondas) num `BenchmarkVivo` e registra o número no relatório; se passar de 50 ms, o `/vivo` ganha um cache de 5 s por processo (a chave é a versão do banco). **Isso é uma proposta de limite, não uma medição.**
- **Diário e erros** não fazem `/vivo`; o diário de hoje recarrega por `<meta refresh>`, e o v2 o troca pelo `/vivo` da onda só se a story 4 provar que a lista de eventos cabe num oob; senão fica sem recarga automática e o README é atualizado.

## 7. Filtros em cascata (aba Pendências)

**Regra:** projeto → coordenador → onda, nessa ordem.

- O filtro de **coordenador** lista só quem tem **onda com US do projeto escolhido**; sem projeto, todos os coordenadores com onda aberta.
- O filtro de **onda** lista só as **ondas desse coordenador com US desse projeto**; sem coordenador, as ondas do projeto; sem projeto, todas.
- **Trocar um filtro de cima limpa os de baixo.** Cada um começa em "todos". **Nenhuma combinação impossível pode ser montada** (P).
- **De onde saem as opções:** do **snapshot** (as ondas abertas e o projeto das US), e **não** das pendências, que é como a página de hoje faz. Assim uma onda sem pendência continua escolhível e dá a lista vazia, em vez de sumir do filtro. As opções são calculadas pela vista (`opcoesCascata(snap, filtro)`), uma função pura.
- **A URL guarda o filtro:** `/pendencias?projeto=nuvem&coord=cais&onda=65`, com `hx-push-url="true"` no formulário: o link é compartilhável, o botão Voltar funciona e a recarga mantém o filtro. Sem JS, o formulário é `method="get"` com um botão "Filtrar".
- **Validação no servidor:** o servidor recebe `projeto`, `coord` e `onda` como vieram. Percorre de cima para baixo; no primeiro valor que não existe naquela combinação, **descarta esse e os de baixo** e devolve o fragmento já normalizado, com `HX-Replace-Url` para a URL corrigida. Valor que nem é id (`onda=abc`) também é descartado, nunca é 400: o link velho de uma onda encerrada abre a lista sem o filtro. Quem monta a URL à mão não consegue uma combinação impossível: ela é normalizada.
- **O `hx-get` do formulário** manda os três campos (`hx-trigger="change"`, `hx-target="#pend-conteudo"`, `hx-swap="outerHTML"`), e o `id` dos `<select>` é estável para o htmx devolver o foco depois da troca (teclado). O fragmento devolvido traz os três filtros de novo (com as opções certas) e os dois grupos.
- **Respondidas** ficam na lista ("Respondidas") até o coordenador resolver; as resolvidas continuam em `/historico` (R).

## 8. Testes

### 8.1 O que muda em `internal/web`

- **`central_test.go`, `central_regressao_test.go`, `situacao_vencida_test.go`, `web_test.go`:** hoje conferem HTML da Central (`/central`) e da lista de pendências (`/`). Passam a conferir as rotas novas, **na ordem das stories** (§2.2): `/central` é a Central nova a partir da story 2 e vira 308 na 3. Os **casos de regressão da #16 e #19 não saem**: cada um ganha o equivalente na vista nova (nada de pane no diário, `vencido`, `desconhecido` por extenso, "consulta extra falhou", escape de HTML hostil, métodos recusados, 401 sem cabeçalho, 421 por `Host`).
- **Vista pura (`vista_test.go`, novo):** alimenta `vistaCentral` e `opcoesCascata` com `central.Snapshot` montados à mão, **sem banco e sem `httptest`**. É o que mantém a regra da §2.1 verdadeira. Cobre: ordem por atenção, sinal de MR por limite da onda, "esperam você" sem a respondida, `concluída` com respondida aberta, onda em duas abas, projeto padrão diferente do projeto das US, todas as combinações da cascata (nenhuma impossível), título de 200 caracteres, texto com `<script>`.
- **`httptest`:** prova o que é de rede e de cabeçalho: a CSP exata (§5.1) em página, fragmento e estático; `/vivo` sem `<form>`, `<textarea>`, `<input>` nem `.pend-corpo`; `/vivo?v=` com hash velho traz o aviso de lista; `POST` com `HX-Request` devolve a resposta e os `hx-swap-oob` esperados (na US, o item; na aba Pendências, resposta principal vazia e o item embrulhado em `afterbegin:#lista-resp`, §5.3), e **sem** `HX-Request` redireciona (303); **4xx de fragmento traz `HX-Retarget: find .erro` e `HX-Reswap: innerHTML`** (e o 4xx do `/corpo` **não traz nenhum dos dois cabeçalhos**: o teste confere a ausência de `HX-Retarget` e de `HX-Reswap`, e que o `<summary>` e o `.pend-corpo` continuam no navegador); **`/pendencias` com `HX-Request` devolve só o fragmento, mas com `HX-History-Restore-Request: true` devolve a página inteira, com ou sem `HX-Request` (B3)**; nenhum fragmento de pendência com `<details open>` (M6); o `Cache-Control` de cada grupo da §5.1 e nenhum `public`; `/central` dá 308 (só depois da story 3); `/?coord=…` dá 308 para `/pendencias` (story 3); `/estilo.css` dá 404; `/estatico/` não lista diretório nem sai do `embed`; `POST` em `/` é 405.
- **Estático (`estatico_test.go`):** **não depende de Node e não lê o CSS para medir contraste** (S3, §4.5). Confere: o `embed.FS` tem **exatamente a lista fechada** de arquivos (M11); o `app.css` embutido contém o tema `basis-interno` (`data-theme`) e as quatro `@font-face`; as quatro fontes existem; o hash da URL muda quando o arquivo muda. O **contraste** é do job `frontend` do CI (`npm run contraste`, sobre o CSS não minificado).
- **Navegador:** um teste opcional (`-tags=browser`, fora do `go test` padrão e do CI de PR) sobe o servidor em loopback, abre cada tela no Chromium e **falha em qualquer violação de CSP e em qualquer erro de console que não esteja numa lista de permissões** (S6): a lista admite só os dois `console.error` de cada 4xx que o próprio teste provoca ("Failed to load resource" e "Response Status Error Code") e nada mais; o `<link rel="icon">` evita o 404 de `/favicon.ico`. Cobre também o que só o navegador prova: o `/vivo` preserva `<details open>` e o texto digitado, o erro 4xx **não** remove o formulário nem o texto, e **Voltar depois de dois filtros mantém a página inteira** (B3). Rodar como parte da revisão das stories de tela; fora do CI obrigatório para não pôr navegador no job `unit`.
- **Concorrência e daemon:** o que sobe daemon continua parando-o no `t.Cleanup` e usando `ORQ_STATE_DIR` e `ORQ_TRABALHO_DIR` temporários (`AGENTS.md`).

### 8.2 Capturas como evidência

**Cada story de tela** (2, 3, 4, 5) anexa ao PR capturas em **390 px e 1440 px** (no celular, 2x), das telas que mexeu, **aberto e fechado**, mais os estados vazio e erro quando couberem. Regras: de `127.0.0.1`, com a animação desligada, a partir do topo do documento, cada arquivo aberto uma vez para conferir que mostra o que o nome diz. As capturas **não são versionadas** (ficam no PR e no relatório da rodada). O revisor independente da story abre as capturas e confere contra o protótipo aprovado.

## 9. Fora do escopo

- **Multi-máquina e multiusuário** (outro projeto, depois, inspirado no ai-memory). A tela já mostra quem respondeu (`respondida_por`), mas **não tem papéis, nem conta, nem permissão por usuário**. A regra da §2.1 existe para esse projeto não precisar refazer a tela. (P, decisão 7: "a decidir".)
- **Notificação fora da página** (push, e-mail, mensagem). O aviso ao coordenador continua o do `Avisador`; o aviso à pessoa continua sendo a própria Central aberta.
- **Merge, mudança de status, encerramento de onda e despacho pela web.** Continuam com o coordenador e com a pessoa no chat.
- **Tema escuro.** A pessoa escolheu só o claro (R, `PRODUCT.md`).
- **JSON da web.** A web não serve JSON nem o `snapshot` (R); o htmx recebe HTML.
- **Caddy, relay e credencial** (R: operação fora do `orq`).

## 10. O banner

A arte do `orq-banner-light.png` está sendo refeita pela pessoa, com **fundo `#EDF1F5` (`base-200`), do mesmo tamanho (880×440 depois da redução), ou transparente**. Regra:

- **Com o banner novo, ele vai direto sobre a página, sem o cartão creme**, na tela vazia ("nenhuma onda aberta") e nas páginas de erro (401, 404). O fundo cinza-frio da arte e o da página têm de ser o mesmo token.
- **Até a arte chegar**, a implementação mantém o banner atual dentro de um cartão com o creme `#FEFAF2`, o que o protótipo mostra. O cartão é **provisório** e fica atrás de uma classe só (`.banner-cartao`), para a story 5 removê-lo sem mexer nas telas.
- **Arquivo pendente da pessoa.** Quem acompanha a entrega do arquivo é o coordenador; a story 5 só começa quando ele existir em `referencias/` (ver §11).
- O banner escuro (`orq-banner.png`) **não entra** (P, tema só claro).

## 11. Proposta de stories

Cada uma cabe numa rodada de executor e tem revisão independente. Títulos no infinitivo. **A proposta fica aqui; quem cria as stories no Taiga é o coordenador, depois da aprovação da pessoa.** A ordem respeita a regra da §2.2: **a cada merge a web continua com a lista de pendências funcionando** (S1).

**1. Montar o build do frontend e a base visual da web.** *(sem dependência; a primeira)*
- `internal/web/frontend/` (`package.json` com as versões da §4.1, `package-lock.json`, `.nvmrc` do Node 24, `input.css` com o tema `basis-interno`), `scripts/build-frontend.sh` (§4.4), `internal/web/estatico/` versionado e embutido por `//go:embed estatico` (§4.2); rota `/estatico/*` com os dois grupos de cache da §5.1; `/estilo.css` removido.
- CSP nova da §5.1 e `<meta name="htmx-config">` completa (inclui `historyRestoreAsHxRequest:false`), base (cabeçalho, abas, rodapé, `<link rel="icon">`, sprite com classe, sem `style=`) e as páginas atuais passando a usar o `app.css`. **As rotas não mudam nesta story.**
- Job `frontend` no CI com `git add -N` (§4.5) e o `npm run contraste`, linha do Node no `AGENTS.md`.
- **Aceite:** `go build` e `go install` sem Node; o job passa e **falha de propósito** em dois commits (um muda o `input.css` sem regenerar; outro acrescenta um arquivo novo em `estatico/` sem rastrear); a página carrega sob a CSP sem violação (teste de navegador); `go test -race ./...` verde; capturas 390/1440 do cabeçalho com abas.

**2. Mostrar a Central com abas, ondas e US em accordion, atualizando sozinha.** *(depende da 1)*
- `vistaCentral` pura, `Fonte`, a **Central nova em `/central`** (as abas: Todos → `/central`, projeto → `/projeto/{slug}`, Pendências → `/`, a lista atual), `/projeto/{slug}`, `/vivo` com o `#vivo` dedicado e `hx-swap="none"` (§6); barra da onda, linha da US, MRs, selos, linha de situação, idade das fontes e a nota da ordem por atenção; estados vazio e vencido. **`/` continua a lista atual.**
- **Itens abertos da revisão final do protótipo viram critério de aceite (M15):** (a) título de onda longo: decidir e registrar entre 3 linhas ou corte com o título inteiro ao abrir; (b) a coluna "limite · ordem" não quebra a 1440 px (`até mr-pronta · estrita` em uma linha); (c) o link "N pendências esperam você" da situação tem indício visual de link (sublinhado); (d) a faixa de abas no celular indica que rola (borda esmaecida) e rola a aba ativa para a vista; (e) a barra de progresso é `<progress>`, sem `style=`.
- **Aceite:** o que o protótipo mostra, com os dados do snapshot; `vista_test.go` sem banco; `/vivo` sem formulário e **sem apagar a página** (teste de navegador); hash de lista; `BenchmarkVivo` registrado (duração de uma resposta, não ciclos por relógio de parede, §6); os cinco itens acima; capturas 390/1440, aberto e fechado, da aba Todos, de um projeto (a onda com US de dois projetos aparece nas duas abas) e da onda de dados vencidos. Nada de pane na página. **A lista de pendências em `/` segue respondendo.**

**3. Responder as pendências no lugar, filtrar em cascata e trocar a página inicial.** *(depende da 2)*
- Aba `/pendencias`, `/p/…/corpo`, o `hx-post` com a resposta e os oob (§5.3), os erros por `HX-Retarget`/`HX-Reswap`, "trocar a resposta", a cascata e a URL com o filtro (§7), a pendência dentro da US e na aba (o mesmo `define`), o estado sem detalhe, `/p/…` e `/historico` no visual novo, a exceção do histórico (§2.2).
- **A troca de rotas acontece aqui, e só aqui (S1):** `/` passa a ser a Central, `/central` responde 308, `/?coord=&projeto=&onda=` responde 308 para `/pendencias`, a aba Pendências aponta para `/pendencias`.
- **README (M16):** atualizar a tabela de rotas, "Central pela web", o log (sem o método) e os avisos de mudança da §2.2. **O coordenador atualiza a skill `orquestrador` (`references/central.md`) depois do merge desta story.**
- **Aceite:** `POST` com `HX-Request` devolve o item e as contagens; sem `HX-Request` redireciona; 401 sem cabeçalho no socket Unix; 413, 415, 409 viram alerta **dentro do formulário, sem perder o texto** (teste de navegador); combinação impossível na URL é normalizada; o `/vivo` não toca o corpo aberto; **Voltar depois de dois filtros mantém a página inteira**; o item muda de grupo e o grupo vazio mostra a frase; capturas 390/1440 da cascata em uso e de uma pendência respondida (contagens atualizadas).

**4. Redesenhar o diário da onda e as páginas de erro.** *(depende da 1; pode andar junto da 2 ou da 3)*
- `/onda/{id}` em lista, com o paginador e a regra de nada de pane; vazio, 401 e 404 com o banner provisório (§10); mensagens de erro do `falha` no visual novo.
- **Aceite:** os testes da #16 do diário passam sem mudar a lista `camposDoDiario`; 404 para onda inexistente e id mal formado; capturas 390/1440 do diário, do vazio e dos erros.

**5. Trocar o banner pela arte nova, sem o cartão creme.** *(depende da 4 e da arte da pessoa)*
- Substitui `orq-banner-light.png` pela arte nova, remove `.banner-cartao`, confere que o fundo da arte é o `base-200`. Atualiza o `DESIGN.md` e o `PRODUCT.md` (regra do banner: de "provisório" para "vigente").
- **Aceite:** capturas 390/1440 da tela vazia e do 404 sem emenda visível entre a arte e a página; o peso da imagem ≤ 400 KB.

**Ordem:** 1 → 2 → 3, com a 4 em paralelo à 2 ou à 3, e a 5 por último e só com a arte. **Revisão independente em todas.** A story 1 é a de maior risco (CSP e CI do frontend); se a revisão achar que o build não é determinístico entre máquinas, a 2 espera. **Ações fora do repositório, do coordenador:** atualizar a skill `orquestrador` depois da story 3; conferir a regra por caminho do Caddy para `/` e `/estilo.css`, se existir.

## 12. Pendências da revisão final do protótipo

O finish reviewer (duas rodadas) fechou o protótipo com `fix`. O que sobrou é pequeno e **virou critério de aceite da story 2** (M15, §11); a seção "Em aberto" do `DESIGN.md` tem a mesma lista:

- Títulos de onda longos ainda truncam na barra fechada (2 linhas a partir de 760 px, 3 no celular).
- A coluna "limite · ordem" quebra em duas linhas a 1440 px para `até mr-pronta · estrita`.
- O link "N pendências esperam você" da linha de situação não tem sublinhado nem outro indício de link.
- A faixa de abas no celular corta "Faturamento" sem indicar que rola.
- O `style="width:…"` da barra de progresso e o `style=` do sprite não valem sob a CSP (§5.1).
- Não se rodou o `concept-seed`: o brief traz `seed: none (unavailable)` por pedido do coordenador (estrutura fixada pela pessoa), e o reviewer anotou que a máquina consegue rodá-lo. É só nota.

## 13. Riscos e o que não foi verificado

A revisão independente (r1) **provou no Chromium 153 com o htmx 2.0.6** os pontos B1, B2, B3, S7 e M5 a M9, e a r3 os **reproduziu** nos scripts da revisão (`t1`, `t4`, `t8`, `t9`, `t10`). A r2 provou, com a meta e a CSP exatas desta spec (`r2-delta.mjs`, reexecutado na r4): **o `#vivo` rearmado por oob a cada ciclo** (os pedidos saem com `v=h0, h1, h2, h3`, um só `#vivo` no DOM, `<details open>` e texto digitado preservados; sem rearmar o `v` fica fixo), **um 503 num ciclo do `/vivo` sem efeito** (`hx-swap="none"`: nada é trocado, a página fica intacta e o poll continua), **a regra do histórico** (Voltar com a meta real e só com a defesa do servidor devolve a página inteira) e o erro do POST com os dois cabeçalhos. O erro do `/corpo` **sem** cabeçalho de retarget também foi provado (S8). O que continua sem prova:

- **O build Node em outro Node ou outro SO.** O determinismo foi provado no Node 24 (três builds, `rm -rf node_modules && npm ci`, outro diretório). Node 22 e macOS não foram testados; por isso a versão é fixa (§4.4).
- **Caddy:** cabeçalhos de cache e regra por caminho não foram lidos nem testados.
- **O custo do `/vivo`** não foi medido (§6).
- **Hospedagem do protótipo no Artifact** não foi testada nas telas; ela não afeta a implementação.
