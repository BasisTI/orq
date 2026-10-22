---
version: 1
slug: "prototipo-index-html"
primary_target: "prototipo/index.html"
related_targets: []
---

# Central web v2 — protótipo (superfície inteira)

## Escopo e modo

Operate. A Central web do orq: ondas, US, MRs e pendências de todos os projetos, somente leitura salvo a resposta às pendências. Superfície inteira num mundo estabelecido (`basis-interno`, fixado pela pessoa) com estrutura fixada pela pessoa (abas, accordion de onda, accordion de US, pendência respondida nos dois lugares, filtros em cascata). Pedido preciso: sem concept-seed nem página de decisão. Code-led. Protótipo estático e descartável, publicado como Artifact para celular e monitor.

## Visitante

Cedric, operador único. No celular: acompanhar e responder longe da máquina. No monitor: painel ao lado do terminal. Tarefa: saber de relance se alguma onda depende dele, e responder.

## Decisões em aberto

Ordem das ondas (por atenção, no protótipo), multiusuário/papéis, contagem de "respondida" na barra da onda.

## Direction contract

THESIS: a Central é uma fila de trabalho que se lê de cima para baixo, não um painel de métricas. A barra da onda carrega o estado inteiro mesmo fechada; o que depende da pessoa sobe para a frente. Recusa o painel de números grandes e o cartão-dentro-de-cartão.

OWN-WORLD: `basis-interno` claro. Fundo `base-200` #EDF1F5 frio, superfícies `base-100`, bordas #D3DAE2, texto #1B2A38. Cabeçalho navy #071A2E com o ícone do orq e "orq" em texto. Ação primária navy #0A2E5C; laranja só em marca, aba ativa e foco (#B35A00 como texto/anel). Carlito no corpo, Inter Tight nos títulos. Semântica por tema: success, error, âmbar-escuro #8F5A00 para atenção e espera. Linhas e divisórias de 1px no lugar de cartões; selos sempre com texto, nunca só cor. Banner creme #FEFAF2 só em cartão na tela vazia e no erro.

STORY: ao abrir, a pessoa lê uma linha de situação (quantas ondas, quantas esperam por ela, MRs com pipeline falhando ou esperando merge), vê as ondas que pedem atenção primeiro, abre uma pendência onde ela aparece, escolhe a opção, comenta e envia; o item confirma, fecha, e as contagens descem.

FIRST VIEWPORT: 390 px: cabeçalho navy compacto, abas roláveis (Todos, quatro projetos, Pendências), linha de situação, e as primeiras barras de onda, cada uma com título em até duas linhas e os selos quebrando em uma segunda linha. 1440 px: cabeçalho e abas em largura plena; coluna das ondas (~860 px) à esquerda e uma coluna lateral à direita com a idade das fontes (herdr, mr, taiga, com `vencido`) e a legenda dos selos; as barras de onda em uma linha só (título | coordenador | limite e ordem | progresso | pendências | MRs). A ação primária da tela é "Enviar resposta", dentro da pendência aberta.

FORM: lista de accordions aninhados (onda > US > MR e pendência) sob abas de projeto, com coluna lateral de apoio no monitor; posição 1 de 1, estrutura fixada pela pessoa; seed: none (unavailable) — estrutura fixada pela pessoa.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
