---
name: orq Central web v2
description: Fila de trabalho clara e fria em basis-interno (navy, laranja só de marca), lida de cima para baixo.
colors:
  base-100: "#FFFFFF"
  base-200: "#EDF1F5"
  base-300: "#D3DAE2"
  base-content: "#1B2A38"
  primary: "#0A2E5C"
  primary-content: "#FFFFFF"
  navy: "#071A2E"
  laranja: "#F68B1F"
  laranja-texto: "#B35A00"
  info: "#0A6E9E"
  success: "#137547"
  warning: "#8F5A00"
  error: "#B3261E"
  texto-secundario: "#2B3A48"
  contorno-controle: "#66768A"
  placeholder: "#5B6B7A"
  selo-ok-fundo: "#E1F1E8"
  selo-ok-texto: "#0E5C38"
  selo-erro-fundo: "#FBE6E3"
  selo-erro-texto: "#9C1F18"
  selo-aviso-fundo: "#FBEFD6"
  selo-aviso-texto: "#7A4D00"
  selo-info-fundo: "#E0EEF6"
  selo-info-texto: "#0A5E88"
  selo-marca-fundo: "#E2E9F2"
  selo-neutro-fundo: "#E8EDF2"
  banner-creme: "#FEFAF2"
  banner-creme-borda: "#EADFC7"
  tom-0: "#2B6CB0"
  tom-1: "#B7791F"
  tom-2: "#2F855A"
  tom-3: "#9B2C86"
typography:
  display:
    fontFamily: "Inter Tight, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.45rem"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "-0.01em"
  title:
    fontFamily: "Inter Tight, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.05rem"
    fontWeight: 700
    lineHeight: 1.25
    letterSpacing: "-0.01em"
  wordmark:
    fontFamily: "Inter Tight, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.4rem"
    fontWeight: 700
    lineHeight: 1.45
    letterSpacing: "-0.02em"
  body:
    fontFamily: "Carlito, Calibri, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.45
  label:
    fontFamily: "Carlito, Calibri, ui-sans-serif, system-ui, sans-serif"
    fontSize: "0.8rem"
    fontWeight: 700
    lineHeight: 1.2
rounded:
  selector: "0.25rem"
  field: "0.375rem"
  box: "0.5rem"
  pill: "9999px"
spacing:
  xs: "0.35rem"
  sm: "0.6rem"
  md: "1rem"
  lg: "1.5rem"
  pagina-celular: "16px"
  pagina-larga: "24px"
components:
  cabecalho:
    backgroundColor: "{colors.navy}"
    textColor: "#FFFFFF"
    height: "3.5rem"
  aba-ativa:
    backgroundColor: "{colors.base-100}"
    textColor: "{colors.navy}"
    height: "3rem"
    padding: "0 0.85rem"
  aba-contagem:
    backgroundColor: "{colors.base-200}"
    rounded: "{rounded.pill}"
    padding: "0 0.5rem"
  aba-contagem-aviso:
    backgroundColor: "{colors.warning}"
    textColor: "#FFFFFF"
    rounded: "{rounded.pill}"
  situacao:
    backgroundColor: "{colors.base-100}"
    rounded: "{rounded.box}"
    padding: "0.75rem 1rem"
  onda:
    backgroundColor: "{colors.base-100}"
    rounded: "{rounded.box}"
    padding: "0.85rem 0.9rem"
  selo:
    backgroundColor: "{colors.selo-neutro-fundo}"
    textColor: "{colors.texto-secundario}"
    rounded: "{rounded.selector}"
    padding: "0.2rem 0.55rem"
    height: "1.5rem"
  selo-aviso-forte:
    backgroundColor: "{colors.warning}"
    textColor: "#FFFFFF"
    rounded: "{rounded.selector}"
  selo-erro:
    backgroundColor: "{colors.selo-erro-fundo}"
    textColor: "{colors.selo-erro-texto}"
    rounded: "{rounded.selector}"
  selo-info:
    backgroundColor: "{colors.selo-info-fundo}"
    textColor: "{colors.selo-info-texto}"
    rounded: "{rounded.selector}"
  selo-ok:
    backgroundColor: "{colors.selo-ok-fundo}"
    textColor: "{colors.selo-ok-texto}"
    rounded: "{rounded.selector}"
  selo-aviso:
    backgroundColor: "{colors.selo-aviso-fundo}"
    textColor: "{colors.selo-aviso-texto}"
    rounded: "{rounded.selector}"
  opcao:
    backgroundColor: "{colors.base-100}"
    rounded: "{rounded.field}"
    padding: "0.7rem 0.8rem"
    height: "3rem"
  botao:
    height: "2.75rem"
    rounded: "{rounded.field}"
  toast:
    backgroundColor: "{colors.navy}"
    textColor: "#FFFFFF"
    rounded: "{rounded.field}"
    padding: "0.7rem 1rem"
  cartao-banner-provisorio:
    backgroundColor: "{colors.banner-creme}"
    rounded: "{rounded.box}"
---

# Design System: orq Central web v2

Fonte: protótipo aprovado (`css/tema.css`, `prototipo-gerador/gerar.mjs`), fora do repositório e descartável. A implementação em `internal/web` reproduz este documento; a spec está em [`docs/central-web-v2.md`](docs/central-web-v2.md). O que é do daisyUI 5.0.46 (a biblioteca) não é documentado aqui; só o que o tema sobrescreve.

## Overview

**Creative North Star: "A fila de trabalho"**

A Central é uma lista que se lê de cima para baixo, não um painel de métricas. A barra da onda carrega o estado inteiro mesmo fechada; o que depende da pessoa sobe para a frente. O mundo é o `basis-interno` claro: fundo cinza-frio, superfícies brancas, uma faixa navy no topo, divisórias de 1px no lugar de cartões. É denso e calmo, feito para ser lido de relance no monitor ao lado do terminal e usado com o polegar no celular.

Laranja é a marca e o foco, nunca informação. A informação mora em selos com texto e ícone; a cor só reforça. Dado vencido nunca finge ser atual.

**Key Characteristics:**
- Linhas e divisórias (1px `base-300`) em vez de cartões; accordions aninhados onda > US > MR e pendência.
- Plano: sem sombra de decoração, sem gradiente (`--depth: 0`, `--noise: 0`).
- Selo sempre com texto; ícone SVG inline quando cabe; tracejado para o que é desconhecido.
- Carlito no corpo, Inter Tight só nos títulos e identificadores.
- Mesmo vocabulário em onda, US e MR: "pipeline falhou", "MR pronta", "esperam você".
- Só tema claro.

## Colors

Paleta fria e sóbria: navy e azul-institucional para ação, âmbar-escuro para "depende de você", semântica de selo em fundos pastéis com texto escuro.

### Primary
- **Azul Basis** (`primary`): ação primária ("Enviar resposta"), link, barra de progresso, borda da opção marcada e da onda aberta. Texto branco sobre ele: 13,48:1.
- **Navy de cabeçalho** (`navy`, também `secondary`/`neutral`): faixa do cabeçalho, títulos (`h1`-`h4`), aba ativa (texto), toast.

### Secondary
- **Laranja de marca** (`laranja`): só marca, sublinhado de 3px da aba ativa, anel de foco dentro do cabeçalho e seleção de texto (35%). Nunca é texto nem preenchimento de informação.
- **Laranja de texto** (`laranja-texto`): o anel de foco de 2px (offset 2px) fora do cabeçalho. Contra branco: 4,80:1. É a única cor laranja que pode tocar texto ou contorno de controle.

### Tertiary (semânticas)
- **Âmbar-escuro** (`warning`): "depende de você". Fundo do selo `aviso-forte` ("esperam você") e da contagem de aba com pendência; branco sobre ele: 5,78:1.
- **Erro** (`error`), **Sucesso** (`success`), **Info** (`info`): vêm do tema; na tela aparecem através dos pares fundo pastel/texto escuro dos selos (`selo-*`), mais o ponto do agente.
- **Tons de projeto** (`tom-0` a `tom-3`): quatro tons, só no ponto de 0,6rem ao lado do slug do projeto.

### Neutral
- **Fundo frio** (`base-200`): fundo da página e da meta da onda.
- **Superfície** (`base-100`): barras, listas, situação, opções.
- **Divisória** (`base-300`, também `--linha`): toda borda de 1px.
- **Tinta** (`base-content`): texto; contra branco 14,64:1. Texto esmaecido é a mesma tinta a 70% (`--suave`); contra branco 5,53:1.
- **Texto secundário** (`texto-secundario`): metadados que precisam de mais corpo que o esmaecido (limite, caminho da MR, descrição da opção).
- **Contorno de controle** (`contorno-controle`): `--input-color` de rádio, select e textarea (≥3:1 sobre branco). **Placeholder** (`placeholder`), também borda do selo tracejado e ponto do agente sem resposta.

### Named Rules
**A Regra do Laranja Mudo.** Laranja é marca, aba ativa e foco. Texto nunca é `#F68B1F`; quando precisa de laranja em texto ou anel, é `#B35A00`.
**A Regra do Âmbar Escuro.** "Depende de você" é âmbar-escuro (`#8F5A00` cheio com texto branco, ou `#7A4D00` sobre pastel). Nunca laranja vivo.
**A Regra dos 70%.** Texto esmaecido nunca abaixo de 70% da tinta; contorno de controle nunca abaixo de 3:1; placeholder `#5B6B7A`. Quem esmaece mais que isso (`.label`, `thead` do daisyUI) é sobrescrito.
**A Regra do Selo sem Cor Solitária.** Estado nunca é só cor: texto sempre, ícone quando cabe, tracejado quando o estado é desconhecido ou "sem estado".
**A Regra dos 4 Tons.** A cor do projeto existe só no ponto ao lado do nome, em 4 tons; nunca em fundo, borda ou texto.

Razões medidas (informadas na revisão do protótipo): texto/branco 14,64; esmaecido/branco 5,53; branco/`#8F5A00` 5,78; branco/primary 13,48; foco `#B35A00`/branco 4,80; placeholder 5,48; contorno de controle 4,64.

## Typography

**Display Font:** Inter Tight (600/700) com `ui-sans-serif, system-ui` de reserva.
**Body Font:** Carlito (400/700) com Calibri e `ui-sans-serif, system-ui` de reserva.
**Mono:** `ui-monospace, "SF Mono", Menlo, Consolas`, só em `code` (0,88em, fundo `base-200`).

**Character:** Carlito, a Calibri livre, dá o corpo de documento da Basis; tem olho pequeno, por isso o `html` está em 106,25%. Inter Tight dá voz firme e compacta aos títulos. Fonte do sistema nunca fala como título.

Escala em `rem` sobre `html { font-size: 106.25% }`; `line-height` 1,45 no corpo; números tabulares em `td`, `th`, selos, contagens e progresso. Carlito e Inter Tight servidas pelo binário (`font-display: swap`, sem Google Fonts em produção; o protótipo as carrega do Google só por ser estático).

### Hierarchy
- **Título de página** (Inter Tight 700, 1.45rem, 1.2, `text-wrap: balance`): "Pendências", "Central" nas páginas internas e de erro.
- **Título de onda** (Inter Tight 700, 1.05rem, 1.25): barra da onda; 3 linhas no celular, 2 a partir de 760px, inteiro quando a onda abre.
- **Marca** (Inter Tight 700, 1.4rem, -0.02em): "orq" no cabeçalho.
- **Título de grupo** (700, 1.05rem): "Esperando você", "Respondidas". **Título de bloco** (700, 0.98rem): coluna lateral; `.sec` 0.95rem.
- **Corpo** (Carlito 400, 1rem): texto corrido, `max-width` 70ch (markdown, resposta, nota).
- **Rótulo/aba** (Carlito 700, 1rem): abas e links de ação. **Selo** (700, 0.8rem). **Metadado** (400, 0.82-0.92rem, esmaecido).
- **Identificadores** (Inter Tight, 0.92-0.95rem): `#61`, `TG-78`, chave da pendência.

### Named Rules
**A Regra da Voz Dupla.** Inter Tight para título e identificador; Carlito para todo o resto. Sem terceira fonte.
**A Regra do Corpo Legível.** Texto nunca abaixo de 0.8rem (só selos); metadado esmaecido, no mínimo 0.82rem.

## Layout

Página única de `max-width` 1360px, centralizada, padding 16px (24px a partir de 1024px). Rolagem vertical; sem painel fixo além das abas.

- **Celular (<760px):** cabeçalho de 3.5rem; abas roláveis na horizontal (sticky no topo, alvo 3rem); tudo em coluna única; a barra da onda em grade de três linhas (título; coordenador; limite e progresso; sinais).
- **≥760px:** a barra da onda vira uma linha de seis colunas (`1.25rem | minmax(16rem,1.4fr) | 8rem | 7.5rem | 6.2rem | 10.5rem`): chevron, título, coordenador, limite e ordem, progresso, sinais; a linha da US vira cinco colunas; a pendência, seis.
- **≥1280px:** duas colunas, ondas à esquerda e coluna lateral de 320px à direita (280px entre 1280 e 1480px), sticky em `top: 4.5rem`.
- **Ritmo:** lacunas de .35 / .6 / .75 / 1rem; espaçamento entre ondas .75rem; páginas internas `max-width` 760px (900px a larga).
- **Alvos de toque:** botões 2.75rem, `btn-sm` 2.25rem, abas e opções 3rem.
- **Hierarquia por atenção:** ondas ordenadas por pendência esperando você, depois pipeline falhou, depois MR pronta, depois o resto; uma nota sob o título diz isso.

## Elevation & Depth

Plano. A profundidade vem de divisórias de 1px (`base-300`), fundo `base-200` contra superfície `base-100`, e de um tom de fundo no hover (`base-200` 55% sobre branco). Estado aberto: borda da onda mistura 45% de primary; opção marcada ganha borda primary e anel interno de 1px.

A única sombra do sistema é a do toast (`0 6px 20px rgba(0,0,0,.25)`), porque ele flutua fixo sobre o conteúdo; não é decoração e não se estende a outros elementos.

### Named Rules
**A Regra do Plano.** Sem sombra, gradiente ou ruído em superfícies em repouso. Separação por linha.
**A Regra do Movimento de Estado.** Movimento só para estado: o ponto do agente "trabalhando" pulsa (1.6s), a pendência recém-respondida pisca verde uma vez (2.2s), o chevron gira 90° (180ms), o toast sobe (200ms). Tudo desliga em `prefers-reduced-motion`.

## Shapes

Cantos pequenos e contidos: selo/seletor `0.25rem`, campos e botões `0.375rem`, caixas (barra de onda, lista, situação, coluna lateral) `0.5rem`; pílula (`9999px`) só nas contagens e na barra de progresso; ponto do agente e do projeto circulares. Ícone do cabeçalho com `0.5rem`. Borda padrão `1px` `base-300`; contorno tracejado (`1px dashed`) para "sem dado/sem estado" e para caixa vazia. Ícones: SVG inline de um sprite, traço único, `1em`.

## Components

### Cabeçalho
Faixa navy de 3.5rem: `orq-icon` (32px, cantos 0.5rem) + "orq" em Inter Tight 700 + subtítulo "Central" (branco 72%, separado por filete) à esquerda; usuário à direita (branco 72%). Foco no cabeçalho usa laranja vivo. O ícone é o mesmo do favicon (`orq-icon-64.png`). O ícone leva `alt=""`: o nome está em texto.

### Abas
Lista horizontal rolável, sticky, fundo branco, filete inferior 1px. Item: 3rem de alto, Carlito 700, tinta; hover `base-200`. Aba ativa por `aria-current="page"` (nunca classe): texto navy e sublinhado interno de 3px laranja. Cada aba traz a contagem numa pílula (`base-200`, 0.85rem, 400, esmaecida); a contagem de Pendências, quando há pendência esperando, vira âmbar-escuro com texto branco 700. Abas: Todos, uma por projeto, Pendências (à direita a partir de 900px).

### Linha de situação
Caixa branca de 1px: uma frase e uma lista em linha de fatos em 700, cada um com ícone e cor de texto escura (âmbar `#7A4D00` o primeiro, "N pendências esperam você" e é link; vermelho, azul e verde para pipeline, MR, ok). Se houver fonte vencida, uma segunda faixa, separada por filete, em âmbar com relógio: "Dados vencidos: ..."

### Barra de onda
`<details>` branco, 1px, 0.5rem. A barra fechada diz tudo: chevron, `#id` (esmaecido) e título (Inter Tight 700, trunca em 3 linhas no celular e 2 a partir de 760px), coordenador e projetos (ponto + slug), `até <limite> · <ordem>`, progresso `n/total` com barra de 4.5rem em primary, e os selos de sinal em fim de linha. Aberta: título inteiro, borda azulada, corpo com uma meta em `base-200` (diário em link) e a lista de US.

### Linha de US
`<details>` sem caixa, separada por filete. Resumo: chevron, id e título (2 linhas), selos de status/Taiga e estado do agente, MRs em resumo, contagem de "espera você". Corpo em fundo `base-200` 35%, filete tracejado em cima, `dl` de fatos (No Taiga com link externo, agente, dependências em `.dep`), seção de MRs e de pendências.

### MR
Linha dentro da US: link em 700 (`!N`, caminho em 400) e um bloco de selos em par rótulo/valor: estado (`draft`, `aberta`, `pronta`, `mesclada`, `fechada`, `sem estado`), pipeline, aprovação, `vencido` e "consulta extra falhou". Em onda `até merge`, MR pronta aparece "MR espera merge"; nas demais, "MR pronta" (o merge não está autorizado).

### Pendência
`<details>` com resumo em seis colunas (chave `TG-78 P1`, tipo, idade, título 1 linha, estado). Aberta, o título fica em 700 e o corpo (`max-width` 62rem) traz o markdown do contexto (`max-width` 70ch), a recomendação, as opções e o formulário.
- **Opções:** cartões de rádio (`.opcao`), 1px `base-300`, `min-height` 3rem; título em 700, descrição em texto secundário; marcada: borda e anel primary e fundo `#EEF3FA`; foco-visível: anel laranja-texto. Rádio obrigatório quando há opções; comentário obrigatório quando não há.
- **Resposta:** textarea com rótulo em 700, botão primário "Enviar resposta" navy-azul (2.75rem), link direto à página `/p/...` ao lado.
- **Estados:** `aberta` (selo âmbar "espera você"); `respondida` (linha verde com ícone "Respondida por ... com a opção ...", comentário em citação com filete, botão de trocar a resposta, pisca verde ao chegar); `sem detalhe` (faixa âmbar clara com ícone: o arquivo da pendência não tem corpo para mostrar).
- Aparece em dois lugares com o mesmo markup: dentro da US e na aba Pendências.

### Selos
Uma pílula de cantos pequenos (0.8rem, 700, 1.5rem de alto, ícone de .95em à esquerda). Variantes: `ok`, `erro`, `aviso`, `aviso-forte` (âmbar cheio, texto branco), `info`, `marca`, `tipo` (branco com filete), `tracejado` (transparente, `1px dashed`, para "desconhecido" e "sem estado"), `apagado` (só contorno), neutro. Vencido: variante `aviso` com ícone de relógio e `title` "o cache passou do prazo da fonte".
- **Vocabulário único**, igual na barra, na US, na MR, na situação e na legenda: "pipeline falhou" / "pipelines falharam", "MR pronta" / "MRs prontas", "MR espera merge", "espera você" / "esperam você", "vencido", "desconhecido" por extenso, "concluída".
- Singular/plural flexiona; a contagem leva numerais tabulares.

### Agente
Ponto de 0.6rem + texto em português; o código do snapshot vai no `title` ("estado do agente: working"). Trabalhando (ponto azul que pulsa), concluído (verde), bloqueado (âmbar-escuro), ocioso e sem resposta (cinza `placeholder`), sem agente/pane ausente (anel vazio, texto esmaecido). Nunca nome, pane ou workspace.

### Filtros em cascata
Aba Pendências: três `select` (Projeto, Coordenador, Onda), rótulo 0.88rem em 700 acima, 2.75rem, fundo branco, contorno `contorno-controle`. Ordem fixa e trocar um de cima limpa os de baixo; as opções saem das ondas abertas (não das pendências). Duas colunas no celular (Onda ocupa a linha), `11rem 11rem 1fr` a partir de 760px.

### Toast
Faixa navy flutuante, centro inferior, `max-width` `min(92vw, 28rem)`, texto branco com ícone, `role="status"`: "Resposta enviada: TG-78 P1. O coordenador é avisado." Sobe em 200ms; some sem JS.

### Coluna lateral
Visível ≥1280px, sticky. Dois blocos brancos de 1px: "Idade dos dados" (fonte em 700, idade, selo `em dia` ou `vencido`: herdr, mr, taiga) e a legenda dos selos (selo + descrição em texto secundário), com nota esmaecida.

### Diário
Lista branca de 1px (`ol` sem marcador), um evento por linha: quando (esmaecido, 5.5rem), tipo (700) e detalhe; a partir de 760px, três colunas. Paginador abaixo. Nada de pane.

### Banner e telas de vazio e erro
`orq-banner-light.png` (880×440, orc com três esferas luminosas) aparece **só** na tela vazia ("nenhuma onda aberta") e nas páginas de erro (401, 404).
- **Hoje (provisório):** dentro de um cartão creme (`#FEFAF2`, borda `#EADFC7`, 0.5rem), com o texto abaixo; na implementação, atrás de uma só classe (`.banner-cartao`).
- **Regra vigente com a arte nova:** a pessoa está refazendo a arte com fundo `#EDF1F5` (`base-200`) ou transparente. Quando o arquivo chegar, o banner vai **direto sobre a página, sem cartão creme**; o fundo da arte e o da página são o mesmo token. Arquivo novo pendente da pessoa.
- O banner escuro não entra. O banner nunca aparece em tela com dados.

## Do's and Don'ts

### Do:
- **Do** manter o estado inteiro na barra da onda fechada; o que depende da pessoa sobe para a frente.
- **Do** usar selo com texto (e ícone, quando cabe) para todo estado; tracejado para desconhecido e sem estado; relógio e "vencido" para dado velho.
- **Do** usar o mesmo vocabulário em todo lugar: "pipeline falhou", "MR pronta", "esperam você".
- **Do** ordenar as ondas por atenção e dizê-lo numa nota; estados do agente em português com o código no `title`.
- **Do** marcar a aba ativa por `aria-current="page"`.
- **Do** usar laranja só em marca, aba ativa e foco; `#B35A00` onde o laranja tocar texto ou anel.
- **Do** manter esmaecido ≥70%, contorno de controle ≥3:1 (`#66768A`) e placeholder `#5B6B7A`.
- **Do** dar às ações alvo de 44px ou mais.
- **Do** confirmar a resposta no lugar (item fechado + toast), nunca em modal.

### Don't:
- **Don't** pôr cartão dentro de cartão; separe por filete de 1px.
- **Don't** usar kicker/eyebrow acima de títulos.
- **Don't** usar sombra ou gradiente decorativos; a sombra do toast é a única, por flutuar.
- **Don't** usar fonte do sistema como voz de título.
- **Don't** transmitir estado só por cor, nem usar a cor do projeto fora do ponto, nem passar de 4 tons.
- **Don't** usar texto laranja `#F68B1F` nem esmaecer texto abaixo de 70%.
- **Don't** mostrar nome, pane ou workspace do Herdr; **don't** mostrar dado vencido como atual.
- **Don't** pôr o banner em tela com dados, nem o banner escuro; **don't** manter o cartão creme depois que a arte nova chegar.
- **Don't** trocar o que está aberto ou digitado na atualização de 30 s.

## Decisões de produto que viraram regra visual

Confirmadas pela pessoa: ondas ordenadas por atenção (3 pendência esperando, 2 pipeline falhou, 1 MR pronta, 0 o resto); a respondida sai de "esperam você" mas segura o `concluída`; estados do agente em português com o código no `title`; cor por projeto só no ponto, em 4 tons; só tema claro; estrutura em abas (Todos, projetos, Pendências) com accordion de onda e de US e a pendência respondida nos dois lugares; filtros em cascata.

## Resolvido na implementação (story #23)

- **Título de onda longo:** corta na barra fechada (3 linhas no celular, 2 a partir de 760px) e aparece inteiro quando a onda abre; o `title` da barra traz o texto todo. Decidido por corte, não por 3 linhas fixas, para a barra fechada manter altura previsível.
- **Coluna "limite · ordem":** `white-space: nowrap` numa coluna de 11rem a partir de 1100px (abaixo disso a barra usa o layout do celular, em três linhas); o teste de navegador confere uma linha só a 1440px.
- **Link "N pendências esperam você":** sublinhado.
- **Faixa de abas no celular:** borda esmaecida (máscara) do lado em que há aba escondida, e a aba ativa é rolada para a vista (`app.js`).
- **Barra de progresso:** `<progress value max>` nativo, sem `style=`.

## Em aberto

- Multiusuário e papéis continuam "a decidir". A ordem das ondas por atenção e a contagem de "respondida" (sai do "esperam você", segura o `concluída`) estão **confirmadas pela pessoa** (2026-10-22) e valem como regra.

Desvios da construção não canonizados: `style="width:%"` na barra de progresso (a CSP da implementação o proíbe: use classe por faixa ou `<progress>`); cores de texto dos selos e do secundário escritas como hex soltos em vez de tokens do tema; `.selo-neutro` sem regra própria (cai no padrão).
