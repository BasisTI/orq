# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

htmx + Tailwind v4 + daisyUI 5, decidida pela pessoa. Páginas renderizadas no servidor (`html/template`); a web não serve JSON. Na implementação, o CSS compilado e o `htmx.min.js` ficam versionados e embutidos no binário (`go:embed`), para `go install` e plugin seguirem sem Node. CSP em `script-src 'self'`, sem inline.

## Users

Na v2, um operador por máquina: o Cedric, que coordena ondas de agentes de código em vários projetos da Basis. Usa a Central de dois jeitos, por igual: no celular, longe da máquina, para acompanhar as ondas e responder pendências; no monitor, como painel aberto ao lado do terminal durante o dia. A tela já mostra quem respondeu, mas não tem papéis. Multiusuário e multi-máquina são outro projeto, posterior (inspirado no ai-memory); a decidir.

## Product Purpose

`orq` é a Central de US do orquestrador multiagente. A web da Central existe para ver de relance o estado de todas as ondas abertas e responder o que depende da pessoa. É somente leitura, exceto pela resposta às pendências: merge, status, encerramento e despacho continuam com o coordenador e com a pessoa no chat. Sucesso: abrir a página e saber, sem rolar, se alguma onda precisa dela.

## Positioning

A Central enxerga ao mesmo tempo as ondas, as US, os agentes (Herdr), as MRs (pipeline e aprovação) e as pendências com contexto completo de coordenadores e projetos diferentes, e deixa a pessoa decidir ali mesmo, sem abrir o terminal.

## Operating Context

- Coordenadores e workers são agentes de código em panes do Herdr; o daemon do `orq` lê o estado vivo (Herdr, forjas GitLab/GitHub, Taiga) e o grava em cache.
- Uma onda agrupa US de um ou mais projetos (slug do Taiga); o projeto padrão do coordenador pode diferir do projeto das US da onda.
- Pendências (dúvida, autorização, decisão) nascem de um arquivo markdown com contexto, opções e recomendação, e a resposta da pessoa avisa o coordenador.
- A web roda atrás de um proxy autenticado (socket Unix) ou em loopback; o usuário vem num cabeçalho.
- Dado vencido (cache velho) é mostrado como `vencido`, nunca como atual.

## Capabilities and Constraints

- Somente leitura, exceto a resposta de pendência (única escrita, pela `Store`).
- Nome, id de pane e workspace do Herdr não aparecem na web, nem no diário.
- Terminologia fixa: Central, onda, US, coordenador, pendência, limite (`até merge`, `até mr-pronta`, `até plano`), ordem (`livre`, `estrita`), `concluída`.
- Estados de MR: `draft`, `aberta`, `pronta`, `mesclada`, `fechada`, `sem estado`; pipeline e aprovação com `desconhecido` por extenso.
- Mensagens e docs em português.
- Atualização automática a cada 30 s, sem trocar o que está aberto ou digitado.
- A decidir: multiusuário e papéis; notificação fora da página.

## Brand Commitments

- Tema `basis-interno` (skill `basis-web-frontend`), fixado pela pessoa: ação primária navy, laranja só como marca (ativo, foco, espera), conteúdo claro e frio, contraste WCAG medido. Só tema claro.
- Arte do orq fornecida pelo Cedric: `orq-icon.png` no cabeçalho (com "orq" em texto) e como favicon; `orq-banner-light.png` só na tela vazia e na página de erro, e nunca com tela de dados. O banner escuro não entra. **Vigente: o banner vai direto sobre a página (`base-200`), sem cartão.** A arte nova (fundo cinza-frio do tema, bordas esfumadas até `#EDF1F5`) substituiu a antiga; o cartão creme (`#FEFAF2`) saiu.

## Evidence on Hand

- Contrato de dados: `docs/snapshot.md` e `orq snapshot`.
- Páginas atuais: `internal/web` (pendências, `/central`, `/onda/<id>`) e a seção "Central pela web" do README.
- Arte: `/home/cedric/tmp/orquestrador/orq/TG-21/referencias/`.
- Não há dados de produção para exibir em protótipo; ele usa dados sintéticos.

## Product Principles

- O estado de relance vem antes do detalhe: a barra da onda diz o que importa mesmo fechada.
- O que depende da pessoa aparece primeiro e se responde onde se vê, sem trocar de tela.
- Dado velho nunca finge ser atual.
- Celular e monitor são cenas de uso iguais; a largura do monitor é aproveitada, não só esticada.
- A web não decide pelo coordenador: ver e responder, nada além.

## Accessibility & Inclusion

Contraste WCAG AA medido nos pares do tema (texto esmaecido no mínimo a 70%), foco visível, navegação por teclado nos accordions e abas, alvos de toque adequados no celular, `prefers-reduced-motion` respeitado.
