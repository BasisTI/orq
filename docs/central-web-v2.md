# Central web v2 (spec da implementação)

Spec da US #21. Ela diz como implementar, em `internal/web`, o que o protótipo aprovado mostra. O que a tela é, para quem e por quê está em [`PRODUCT.md`](../PRODUCT.md); o tema, os componentes e as regras de selo e de cor, em [`DESIGN.md`](../DESIGN.md); os dados, em [`docs/snapshot.md`](snapshot.md); as rotas e a segurança de hoje, no README (seções "Pendências com contexto e resposta pela web" e "Central pela web"). Este documento só registra o que muda.

- **Protótipo aprovado:** `https://cedagente-orq.basisti.com.br/prototipo/` (atrás de login). Aprovado como composição em 2026-10-22. Ele é descartável e **não** entra no repositório; seus dados são sintéticos.
- **Origem das decisões:** cada decisão abaixo traz a origem entre parênteses: `P` é o retorno da pessoa sobre o protótipo (2026-10-22), `R` é o README, `S` é o `docs/snapshot.md`, `H` é o código do htmx 2.0.6 lido para esta spec.
- Estado atual conferido no `main` (`9e811b6`): rotas em `internal/web/web.go`, modelos em `internal/web/tmpl/`, um `estilo.css` único embutido, CSP `default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'`, sem JS.

## 1. O que muda, em uma tela

A web deixa de ter "pendências" como página inicial e a Central como página à parte. Passa a ter uma Central só, com abas:

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
       LerPendencia(ctx context.Context, ref refs.Pend) (central.Pendencia, []central.RespostaRegistrada, error)
       Diario(ctx context.Context, f central.FiltroDiario) ([]central.Evento, error)
   }
   ```

   `*central.Store` a implementa. Os nomes exatos seguem o que o `Store` já tem hoje; a story 2 acerta a assinatura. A **escrita** continua pelo `Store` (`ResponderPendencia`), e é a única (R).
3. **O snapshot é lido uma vez por requisição** e o resultado serve a página inteira. Dois handlers na mesma requisição não podem ver instantes diferentes.

### 2.2 Rotas

| Rota | Antes | Depois |
|---|---|---|
| `GET /` | pendências abertas | **Central, aba Todos** (página inteira) |
| `GET /projeto/{slug}` | não existe | aba de um projeto; as ondas com ao menos uma US dele; 404 se o slug não tem onda aberta |
| `GET /pendencias` | não existe | aba Pendências, filtros `?projeto=&coord=&onda=`; com `HX-Request`, só o fragmento `#pend-conteudo` |
| `GET /central` | Central | `308` para `/` (links antigos) |
| `GET /p/{projeto}/{ref}/{n}` | detalhe | **continua**, no visual novo: link direto, sem accordion |
| `GET /p/{projeto}/{ref}/{n}/corpo` | não existe | **fragmento** do corpo da pendência (contexto, opções, formulário); carregado na primeira abertura do accordion |
| `POST /p/{projeto}/{ref}/{n}/responder` | grava e redireciona | **mesmo contrato** (§5.3); com `HX-Request` devolve fragmento, sem ele continua redirecionando (post/redirect/get) |
| `GET /vivo` | não existe | **fragmento** da atualização de 30 s (§6) |
| `GET /onda/{id}` | diário | continua, no visual novo |
| `GET /historico` | resolvidas | continua; ganha link no rodapé da aba Pendências |
| `GET /saude` | saúde | continua, sem mudança |
| `GET /estatico/{arquivo}` | `GET /estilo.css` | **novo**: CSS, `htmx.min.js`, `app.js`, fontes e imagens embutidos (§4); `/estilo.css` é removido |

- **Só `GET` e o `POST` de resposta.** `POST` nas demais, `PUT`, `DELETE` e `PATCH` continuam `405` (o que a #16 garante, R). O `/vivo` e os `/corpo` são `GET` e não gravam.
- **O que é fragmento htmx e o que é página:** são fragmentos `/vivo`, `/p/…/corpo`, a resposta do `POST` com `HX-Request` e `/pendencias` com `HX-Request`. Tudo o mais é página inteira. **Página inteira funciona sem JS** (a lista das ondas, os accordions por `<details>` e o `POST` por formulário); o JS só melhora (P: "fácil de entender").
- **Mudança de comportamento a avisar no README:** `/` deixa de ser a lista de pendências. O aviso de pendência que o daemon manda ao coordenador usa `/p/…` (R), então não quebra; o bookmark antigo de `/` cai na aba Todos.
- **Modelos:** `html/template` como hoje, embutidos. Um `base` (cabeçalho, abas, rodapé) e um modelo por tela; os fragmentos são `define`s reaproveitados pela página inteira e pelo `/vivo`, para o mesmo HTML nunca existir duas vezes (§5.2).

### 2.3 O que sai do `Store.Snapshot` e o que precisa de consulta nova

O snapshot já traz tudo o que as abas, as barras e as linhas precisam. **Nenhuma consulta nova ao banco é necessária para as telas.**

| Elemento da tela | Origem no snapshot |
|---|---|
| contagem de ondas por aba (Todos, projeto) | `coordenadores[].ondas[]` (só abertas, o padrão do snapshot); projeto de uma onda = conjunto de `us[].projeto` |
| contagem da aba Pendências | `pendencias_abertas[]` com `estado == "aberta"` |
| barra da onda | `Onda` (`id`, `titulo`, `autorizacao_ate`, `ordem`), `Coordenador.nome`, `us[].status` (prontas/total) |
| pendências abertas da onda | `Pendencia.onda_id` e `estado` |
| sinal agregado das MRs | `MR.estado`, `pipeline` das MRs não `mesclada` nem `fechada` |
| selo `concluída` | a regra de [Encerrar a onda](../README.md#encerrar-a-onda), já usada pelo TUI e por `/central` (`central.Concluida`) |
| estado do agente, `vencido` | `US.herdr.status` e `central.Vencido("herdr", …)`; **nome, pane e workspace nunca chegam ao modelo** (R) |
| status no Taiga, link | `US.taiga` |
| MR: estado, pipeline, aprovação, `vencido`, "consulta extra falhou" | `MR.*`, `situacao_em`, `situacao_erro` (US #16 e #19, S) |
| idade das fontes | `fontes.*` |
| cor do projeto | `projetos[].indice` (4 tons no protótipo; o TUI tem 8) |
| opções da cascata | do snapshot (§7), **não** das pendências como hoje |
| cabeçalho "quem sou" | `usuarioDe(r)` |

**O que continua precisando de consulta além do snapshot:**

- **O detalhe da pendência** (até 64 KiB cada, S: "o `detalhe` não vai no snapshot"). Por isso o accordion da pendência traz só o resumo na página e busca o corpo em `/p/…/corpo` na primeira abertura (`hx-trigger="toggle once"`). Uma Central com 20 pendências não carrega 20 detalhes por padrão. Usa `LerPendencia`, que o `detalhe` já usa.
- **O histórico de respostas** (`RespostaRegistrada`), que só a página `/p/…` e o corpo mostram.
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

São as versões do protótipo e as da skill `basis-web-frontend` (`references/tema-interno.md`). Subir versão é uma mudança de código com a regeneração do CSS (§4.4) no mesmo commit.

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

- **`go:embed`** pega `estatico/*` e `estatico/fontes/*`. **`go install` e `scripts/build-plugin.sh` seguem sem Node:** o `build-plugin.sh` só roda `go build` (R, "Plugin do Herdr") e passa a ter o CSS pronto no repositório. Node é dependência **só de quem edita o frontend**, e o `AGENTS.md` ganha essa linha.
- **O `input.css` usa `source(none)` com `@source "../tmpl/**/*.html"`.** O Tailwind trata como classe qualquer palavra do que varre, comentário inclusive; sem isso, classe citada em `docs/` ou em skill entraria no CSS (`tema-interno.md`). O `@source` aponta só para os modelos (e para o `app.js`, se usar classe).
- **Tema:** `@plugin "daisyui" { themes: false; }` e `@plugin "daisyui/theme"` com o `basis-interno` (valores no `DESIGN.md`), `default: true`. As sobrescritas do daisyUI que esmaecem texto (`.label`, `thead`, `.tab`, `.stat-*`, `.menu-title`) e o botão sem variante vão em `@layer utilities` (as mesmas de `tema-interno.md`). Aqui há duas a mais, medidas no protótipo: o `placeholder` (3,07:1 no daisyUI, vai a `#5B6B7A`, 5,48:1) e o contorno de `.radio`, `.select` e `.textarea` (`--input-color: #66768A`, 4,64:1; o do daisyUI dá 1,48:1).
- **Fontes servidas pelo binário, sem Google Fonts em produção** (CSP `font-src 'self'`). Os `.woff2` vêm do `@fontsource` (licença OFL; o texto da licença vai em `estatico/fontes/LICENSE-*.txt`). `font-display: swap`.
- **Imagens:** o ícone (64 px) e o banner (880×440) são a arte da pessoa, reduzidos; a procedência está no relatório da US #21 r1.

### 4.3 O JS próprio

Um arquivo, `estatico/app.js`, sem inline e sem dependência: abre os `<details>` ancestrais de `location.hash` (na carga e em `hashchange`) e rola até o alvo. Nada mais. A confirmação, a troca de contagens e o filtro são do htmx e do servidor.

### 4.4 Como regenerar

```bash
scripts/build-frontend.sh          # npm ci, compila, copia htmx e fontes para internal/web/estatico
git diff --exit-code internal/web/estatico   # vazio se nada mudou
```

O script usa `npm ci` (nunca `npm install`), `npx tailwindcss -i frontend/input.css -o estatico/app.css --minify` e `cp` dos arquivos de `node_modules`. Node **22 LTS** (versão em `frontend/.nvmrc`; o CI usa a mesma). Quem mexe em modelo ou `input.css` roda o script e commita o resultado.

### 4.5 Job de CI

Um job novo, `frontend`, ao lado do `unit` em `.github/workflows/ci.yml`:

1. `actions/setup-node` com a versão do `.nvmrc`, cache do `npm`;
2. `scripts/build-frontend.sh`;
3. `git diff --exit-code -- internal/web/estatico`: **falha se o CSS (ou o `htmx.min.js`, ou as fontes) versionado não bate com o compilado**, com a mensagem "rode scripts/build-frontend.sh e commite internal/web/estatico".

O job `unit` **não instala Node**: se alguém depender dele, o `go test` quebra no CI, que é o que se quer provar. Um teste Go (§8) lê o `app.css` embutido e confere o contraste do tema (o equivalente ao `ContrasteDoTemaTest` da skill).

## 5. CSP, segurança e a resposta pela web

### 5.1 CSP

```text
default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self';
connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'
```

- **Sem `unsafe-inline` e sem `unsafe-eval`.** Nenhum `<script>` nem `style=""` inline nas páginas (o protótipo usa `style="width:…"` na barra de progresso: na implementação vira classe por faixa, ou o elemento nativo `<progress>`).
- **O que o htmx 2.0.6 exige (H, lido no `htmx.js`):** o código usa `eval` e `new Function` só dentro de `maybeEval`, que respeita `htmx.config.allowEval`; com `false`, ele dispara `htmx:evalDisallowedError` e não executa. Isso corta `hx-on:*`, filtros de gatilho (`every 30s [cond]`) e `hx-vals="js:…"`, que a tela não usa. O htmx também injeta um `<style>` dos indicadores, a não ser que `includeIndicatorStyles` seja `false`. Configuração no `<meta name="htmx-config">` (que não é script, então a CSP não a barra):

  ```html
  <meta name="htmx-config" content='{"allowEval":false,"allowScriptTags":false,"includeIndicatorStyles":false,"historyCacheSize":0,"responseHandling":[{"code":"204","swap":false},{"code":"[23]..","swap":true},{"code":"[45]..","swap":true,"error":true}]}'>
  ```

  `selfRequestsOnly` já é o padrão. `allowScriptTags:false` impede que um fragmento traga `<script>` e o htmx o execute (defesa em profundidade: o fragmento sai de `html/template`). `historyCacheSize:0` impede o htmx de guardar páginas (com pendências) no `localStorage`. **A story 1 prova isso**: um teste de navegador (§8) carrega a página sob essa CSP e falha em qualquer violação reportada.
- Cabeçalhos que já existem seguem: `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cache-Control: no-store` (também nos fragmentos e nos estáticos? `no-store` nos fragmentos; nos estáticos, `Cache-Control: public, max-age=…` com o hash do conteúdo na URL, ver abaixo).
- **Estáticos com cache:** `/estatico/app.css?v=<hash>` (o hash do conteúdo embutido, calculado na partida). Só o `/estatico/*` ganha cache longo; páginas e fragmentos continuam `no-store`.

### 5.2 O que continua igual

- **`CrossOriginProtection`** em volta de tudo (R): `POST` de outra origem recusado (403). O `hx-post` do htmx é da mesma origem, então passa. Nada de token CSRF novo.
- **Quem responde:** o `respondida_por` vem do cabeçalho do proxy (`X-Orq-User`, `--web-cabecalho`) no socket Unix, e **a requisição sem ele é recusada com 401** (só `/saude` passa); em TCP o cabeçalho é ignorado e vale `local`; `Host` conferido em TCP (421). O fragmento, o `/vivo` e os `/corpo` passam pelo mesmo `comUsuario`: **sem o cabeçalho no socket Unix, 401 também neles.**
- **Escape:** tudo vem de `html/template`; o markdown do detalhe continua pelo goldmark **sem `WithUnsafe`** (HTML bruto escapado, só `http`, `https`, `mailto`, âncora e relativo viram link, imagem perde o endereço). **Texto de agente, título de onda e de US e resposta da pessoa nunca são `template.HTML`.** A vista não tem campo `template.HTML` além do detalhe renderizado.
- **Nada de pane no diário** (R, #16): a lista `camposDoDiario` e o `[pane]` dos textos seguem como estão. O diário novo reaproveita a função, não a reescreve.
- **Limites do POST:** corpo até 16 KiB (413), resposta até 8 KiB (413), controles recusados (400), `Content-Type` `application/x-www-form-urlencoded` (415). O `hx-post` envia exatamente isso.
- **Métodos recusados:** só `GET` e o `POST` de resposta (405 nos demais); `OPTIONS` e `HEAD` seguem o comportamento do `net/http`.
- **Log:** método, padrão da rota e status, como hoje. As rotas novas entram com o padrão (`/p/{projeto}/{ref}/{n}/corpo`, `/vivo`), nunca com o caminho nem a query.

### 5.3 A resposta por `hx-post`

**Contrato do pedido (não muda):** `POST /p/{projeto}/{ref}/{n}/responder`, `application/x-www-form-urlencoded`, campos `opcao` (opcional, inteiro ≥ 1) e `texto`. O formulário do protótipo usa `comentario`; **na implementação o nome é `texto`**, o do contrato de hoje.

**O formulário** carrega `action` e `method="post"` (funciona sem JS) e `hx-post` para o mesmo endereço, `hx-target="closest .pend"` e `hx-swap="outerHTML"`. Marcas do protótipo que a implementação mantém: opção obrigatória quando há opções (`required` no rádio), comentário obrigatório quando não há.

**A resposta do servidor (200, `HX-Request: true`)** devolve:
1. o **item da pendência já respondido e fechado** (o mesmo `define` da lista), com a opção escolhida, o `respondida_por` e a hora; é o `outerHTML` do alvo;
2. **swaps fora de banda** (`hx-swap-oob="true"`) das contagens que mudaram, cada uma com `id` estável: a contagem da aba Pendências, o selo `esperam você` da barra da onda, o da linha da US, a linha de situação, e (na aba Pendências) os contadores dos grupos "Esperando você" e "Respondidas", e a **mudança de grupo** do item (o item sai do grupo de cima, entra no de baixo);
3. um `<div id="aviso" role="status" hx-swap-oob="true">` com "Resposta enviada: TG-78 P1. O coordenador é avisado." (some por animação de CSS, sem JS).

**Por que `hx-swap-oob` e não evento:** o servidor sabe todas as contagens (ele tem o snapshot) e o navegador não precisa saber onde cada uma está; um evento (`HX-Trigger`) obrigaria o JS próprio a buscar os números. O custo é que o servidor precisa **conhecer os ids** que a página tem: por isso a vista monta o mesmo `define` de contagem para a página e para o fragmento (§2.2), e a resposta só inclui oob dos ids que o pedido diz que existem (o formulário manda `hx-vals` não, e sim o `HX-Current-URL`, que o htmx envia sozinho; o servidor decide a aba pela URL).

**Erros:** o servidor devolve o **status de sempre** (400, 409 `pendencia_resolvida`, 413, 415, 503) com um fragmento `<p role="alert">` e o `responseHandling` do htmx (§5.1) troca-o dentro do formulário (`hx-target-error` fica fora: a configuração acima faz o 4xx/5xx trocar). **O que a pessoa digitou não se perde:** o fragmento de erro vai para um espaço reservado no formulário, não substitui o formulário.

**Trocar a resposta** (R: "a resposta pode ser trocada até a pendência ser resolvida"): o botão do item respondido faz `hx-get` do `/corpo?trocar=1`, que devolve o formulário com a resposta atual indicada; a nova resposta gera um `pend.responder` e zera o aviso ao coordenador, como hoje. **Trocar não muda as contagens** (já saiu do "esperam você").

**O que não muda:** `central.ResponderPendencia`, o evento `pend.responder` no diário, o aviso ao coordenador pelo `Avisador`, e a regra "worker e revisor nunca respondem".

## 6. Atualização automática de 30 s

- **O que é consultado:** `GET /vivo`, um por página aberta, com `hx-trigger="every 30s"` no `<body>` da tela (não por elemento). A resposta é só `hx-swap-oob`:
  - a linha de situação e as contagens das abas;
  - a lista "Idade dos dados";
  - **as barras das ondas e as linhas das US**, trocando **só o `<summary>`** de cada uma (`<summary id="onda-61-barra">`), **nunca o `<details>`**;
  - na aba Pendências, os contadores dos grupos.
- **O que nunca é trocado:** o `<details>` (e portanto se está aberto), o corpo de uma pendência, o formulário e o que foi digitado nele. Nada com `<form>`, `<textarea>`, `<input>` ou `.pend-corpo` está na resposta do `/vivo`. **Teste:** um teste de `httptest` garante que o corpo do `/vivo` não contém essas marcas (§8).
- **Mudança de composição (onda nova, onda encerrada, pendência nova):** o oob não pode inserir nem remover itens sem pisar no que está aberto. O `/vivo` recebe `?v=<hash>` (hash de ids e ordem das ondas, ou das pendências da aba, calculado na renderização); se o hash do servidor difere, a resposta traz, além do resto, `<div id="aviso-lista" hx-swap-oob="true">A lista mudou. <a href="">Atualizar</a></div>`. A pessoa decide quando recarregar. **A tela nunca reordena sozinha embaixo da mão de quem está lendo** (é a mesma razão por que o protótipo diz "o que está aberto ou digitado não é trocado").
- **Aba em segundo plano:** o navegador já reduz os temporizadores a cerca de 1 por minuto; a spec **não** usa filtro de gatilho de visibilidade, porque ele exige `allowEval` (§5.1).
- **Custo no daemon:** o `/vivo` roda no processo do daemon (`orq daemon --web`) e custa **um `Snapshot`**: uma transação de leitura do SQLite (WAL) sobre o cache que o daemon já gravou, sem chamar Herdr, forja nem Taiga (S). Uma pessoa por máquina (P) e algumas abas abertas dão uma leitura a cada 30 s por aba. A story 2 mede o tempo do `Snapshot` com a Central do protótipo (9 ondas) num `BenchmarkVivo` e registra o número no relatório; se passar de 50 ms, o `/vivo` ganha um cache de 5 s por processo (a chave é a versão do banco). **Isso é uma proposta de limite, não uma medição** (não medi o custo real).
- **Páginas de diário e de erro** não fazem `/vivo`; o diário de hoje recarrega por `<meta refresh>`, e o v2 o troca pelo `/vivo` da onda só se a story 4 provar que a lista de eventos cabe num oob; senão fica sem recarga automática e a spec do README é atualizada.

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

- **`central_test.go`, `central_regressao_test.go`, `situacao_vencida_test.go`, `web_test.go`:** hoje conferem HTML da Central (`/central`) e da lista de pendências (`/`). Passam a conferir as rotas novas. Os **casos de regressão da #16 e #19 não saem**: cada um ganha o equivalente na vista nova (nada de pane no diário, `vencido`, `desconhecido` por extenso, "consulta extra falhou", escape de HTML hostil, métodos recusados, 401 sem cabeçalho, 421 por `Host`).
- **Vista pura (`vista_test.go`, novo):** alimenta `vistaCentral` e `opcoesCascata` com `central.Snapshot` montados à mão, **sem banco e sem `httptest`**. É o que mantém a regra da §2.1 verdadeira. Cobre: ordem por atenção, sinal de MR por limite da onda, "esperam você" sem a respondida, `concluída` com respondida aberta, onda em duas abas, projeto padrão diferente do projeto das US, todas as combinações da cascata (nenhuma impossível), título de 200 caracteres, texto com `<script>`.
- **`httptest`:** prova o que é de rede e de cabeçalho: a CSP exata (§5.1) em página, fragmento e estático; `/vivo` sem `<form>`, `<textarea>`, `<input>` nem `.pend-corpo`; `/vivo?v=` com hash velho traz o aviso de lista; `POST` com `HX-Request` devolve o item respondido e os `hx-swap-oob` esperados, e **sem** `HX-Request` redireciona (303); erro 4xx devolve o fragmento de alerta; `/central` dá 308; `/estilo.css` dá 404; `/estatico/` não lista diretório nem sai do `embed`; `POST` em `/` é 405.
- **Estático (`estatico_test.go`):** o `app.css` embutido tem o tema `basis-interno`, nenhuma regra de texto ativo abaixo de 70% sem sobrescrita (o equivalente ao `ContrasteDoTemaTest.java` da skill), as quatro fontes existem, e o hash da URL muda quando o arquivo muda.
- **Navegador:** um teste opcional (`-tags=browser`, fora do `go test` padrão e do CI de PR) sobe o servidor em loopback, abre cada tela no Chromium e **falha em qualquer violação de CSP e erro de console**; rodar como parte da revisão das stories de tela. Ele não entra no CI obrigatório para não pôr navegador no job `unit`.
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

Cada uma cabe numa rodada de executor e tem revisão independente. Títulos no infinitivo. **A proposta fica aqui; quem cria as stories no Taiga é o coordenador, depois da aprovação da pessoa.**

**1. Montar o build do frontend e a base visual da web.** *(sem dependência; a primeira)*
- `internal/web/frontend/` (`package.json` com as versões da §4.1, `package-lock.json`, `input.css` com o tema `basis-interno`), `scripts/build-frontend.sh`, `internal/web/estatico/` (CSS, `htmx.min.js`, fontes, ícone) versionado e embutido; rota `/estatico/*` com cache por hash; `/estilo.css` removido.
- CSP nova da §5.1, `<meta name="htmx-config">`, base (cabeçalho, abas, rodapé, sprite de ícones) e as páginas atuais passando a usar o `app.css`.
- Job `frontend` no CI (§4.5), linha do Node no `AGENTS.md`, teste estático de contraste.
- **Aceite:** `go build` e `go install` sem Node; `git diff --exit-code` do job passa e **falha de propósito** num commit que muda o `input.css` sem regenerar; a página carrega sob a CSP sem violação (teste de navegador); `go test -race ./...` verde; capturas 390/1440 do cabeçalho com abas vazias.

**2. Mostrar a Central com abas, ondas e US em accordion, atualizando sozinha.** *(depende da 1)*
- `vistaCentral` pura, `Fonte`, rotas `/`, `/projeto/{slug}`, `/central` (308), `/vivo`; barra da onda, linha da US, MRs, selos, linha de situação, idade das fontes e a nota da ordem por atenção; os estados vazio e vencido.
- **Aceite:** o que o protótipo mostra, com os dados do snapshot; `vista_test.go` sem banco; `/vivo` sem formulário; hash de lista; `BenchmarkVivo` registrado; capturas 390/1440, aberto e fechado, da aba Todos, de um projeto (a onda com US de dois projetos aparece nas duas abas) e da onda de dados vencidos. Nada de pane na página.

**3. Responder as pendências no lugar e filtrar em cascata.** *(depende da 2)*
- Aba `/pendencias`, `/p/…/corpo`, o `hx-post` com o fragmento e os `hx-swap-oob` (§5.3), "trocar a resposta", a cascata e a URL com o filtro (§7), a pendência dentro da US e na aba (o mesmo `define`), o estado sem detalhe, `/p/…` e `/historico` no visual novo.
- **Aceite:** `POST` com `HX-Request` devolve o item e as contagens; sem `HX-Request` redireciona; 401 sem cabeçalho no socket Unix; 413, 415, 409 viram alerta no formulário sem perder o texto; combinação impossível na URL é normalizada; o `/vivo` não toca o corpo aberto; capturas 390/1440 da cascata em uso e de uma pendência respondida (contagens atualizadas).

**4. Redesenhar o diário da onda e as páginas de erro.** *(depende da 1; pode andar junto da 3)*
- `/onda/{id}` em lista, com o paginador e a regra de nada de pane; vazio, 401 e 404 com o banner provisório (§10); mensagens de erro do `falha` no visual novo.
- **Aceite:** os testes da #16 do diário passam sem mudar a lista `camposDoDiario`; 404 para onda inexistente e id mal formado; capturas 390/1440 do diário, do vazio e dos erros.

**5. Trocar o banner pela arte nova, sem o cartão creme.** *(depende da 4 e da arte da pessoa)*
- Substitui `orq-banner-light.png` pela arte nova, remove `.banner-cartao`, confere que o fundo da arte é o `base-200`. Atualiza o `DESIGN.md` (regra do banner: de "provisório" para "vigente").
- **Aceite:** capturas 390/1440 da tela vazia e do 404 sem emenda visível entre a arte e a página; o peso da imagem ≤ 400 KB.

**Ordem:** 1 → 2 → 3, com a 4 em paralelo à 2 ou à 3, e a 5 por último e só com a arte. **Revisão independente em todas.** A story 1 é a de maior risco (CSP e CI do frontend); se a revisão achar que o build não é determinístico entre máquinas, a 2 espera.

## 12. Pendências da revisão final do protótipo

O finish reviewer (duas rodadas) fechou o protótipo com `fix`; o que sobrou é pequeno e **a implementação resolve** (a seção "Em aberto" do `DESIGN.md` tem a mesma lista):

- Títulos de onda longos ainda truncam na barra fechada (2 linhas a partir de 760 px, 3 no celular): subir o limite para 3 linhas em barras com selos, ou aceitar o corte (o título inteiro aparece quando a onda abre).
- A coluna "limite · ordem" quebra em duas linhas a 1440 px para `até mr-pronta · estrita`: `white-space: nowrap` ou um pouco mais de largura.
- O link "N pendências esperam você" da linha de situação não tem sublinhado nem outro indício de link.
- A faixa de abas no celular corta "Faturamento" sem indicar que rola; sugestão: esmaecer a borda direita e rolar a aba ativa para a vista.
- O `style="width:…"` da barra de progresso não vale sob a CSP (§5.1).
- Não se rodou o `concept-seed`: o brief traz `seed: none (unavailable)` por pedido do coordenador (estrutura fixada pela pessoa), e o reviewer anotou que a máquina consegue rodá-lo.

## 13. Riscos e o que não foi verificado

- **O build do CSS ser determinístico entre máquinas** (o job `frontend` do CI depende disso): o Tailwind 4 com `source(none)` e versões travadas deve ser, mas **não rodei o build Node nesta rodada** (o protótipo usa o build de navegador). A story 1 prova, e é por isso que o job falha ao divergir.
- **`responseHandling` do htmx para erro 4xx** com `target` por padrão: a configuração da §5.1 foi escrita a partir do código do htmx 2.0.6 (`resolveResponseHandling`) e **não foi executada**. O teste de navegador da story 3 confirma.
- **`hx-swap-oob` em `<summary>`:** troca de elemento com `id` dentro de um `<details>`, sem o `<details>`. Espera-se que preserve o `open`, **não testado**. Se não preservar, o `/vivo` passa a trocar um `<span>` interno de cada barra.
- **O custo do `/vivo`** não foi medido (§6).
- **Hospedagem do protótipo no Artifact** não foi testada nas telas; ela não afeta a implementação.
