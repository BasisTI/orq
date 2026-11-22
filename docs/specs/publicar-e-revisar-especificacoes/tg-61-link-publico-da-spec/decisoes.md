# Decisões — TG-61

## D1 — Vários links ativos por spec, com rótulo
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** cada spec pode ter vários links ativos, cada um com rótulo e revogado individualmente.
- **Fala:** "C, vários links ativos com rótulo"

## D2 — Validade opcional
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** `--expira` opcional no `gerar`; link expirado responde o mesmo 404 de revogado.
- **Fala:** "B, validade opcional"

## D3 — Último acesso e contador por link
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** o handler público registra último acesso e contador por um método estreito, fora do diário; a listagem os mostra.
- **Fala:** "B, último acesso e contador por link"

## D4 — Proxy reverso aplicado pelo coordenador
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** o coordenador aplica e valida a configuração do proxy reverso da máquina da Central depois do merge, com autorização própria.
- **Fala:** "C"

## D5 — Limite de taxa no proxy reverso
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** o limite de taxa por origem fica no proxy reverso, com o módulo de limite de taxa dele; o orq não implementa limite.
- **Fala:** "B"

## D6 — Comandos do link
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** `orq spec link gerar TG-<ref> --para "<rótulo>" [--expira 30d]` (URL impressa uma única vez), `orq spec link listar TG-<ref>` e `orq spec link revogar TG-<ref> <id>`.
- **Fala:** "Aprovado"

## D7 — Socket e rota públicos
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** `orq daemon --web-publico <socket>` e rota `GET /s/{codigo}`; a versão mostrada segue a regra de ref da Central.
- **Fala:** "Aprovado"

## D8 — Formato do código
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** 32 bytes de `crypto/rand` em base64url; o banco guarda o hash SHA-256.
- **Fala:** "Aprovado"

## D9 — Página pública numa página só, com âncoras
- **Data:** 2026-11-22
- **Origem:** TG-61#P2, rodada de ajuste r2
- **Decisão:** `GET /s/{codigo}` mostra todos os arquivos do conjunto exposto em seções, com índice; link para arquivo do conjunto vira âncora, os demais viram texto.
- **Fala:** "A, uma página só com âncoras"

## D10 — Faixa pública com título, hora e estado
- **Data:** 2026-11-22
- **Origem:** TG-61#P2, rodada de ajuste r2
- **Decisão:** a faixa da página pública mostra o título da US, a hora da leitura e o estado simplificado; sem ref, SHA nem link da MR.
- **Fala:** "B, título, hora e estado"

## D11 — Links seguem na substituição da spec, com aviso de versão
- **Data:** 2026-11-22
- **Origem:** TG-61#P2, rodada de ajuste r2
- **Decisão:** no `--substituir`, os links ativos seguem e passam a mostrar a spec nova, com aviso na saída; a exibição da spec mostra que ela foi substituída, com a data e a versão (1 = inicial, +1 a cada substituição).
- **Fala:** "A: Os links seguem, mas tem que aparece na exibição da Spec que ela foi substituída com a data e a versão: 1 = versão inicial, 2 = versão após a primeira substituição, etc"

## D12 — Aviso de versão na página pública e na Central
- **Data:** 2026-11-22
- **Origem:** TG-61#P2, rodada de ajuste r2
- **Decisão:** o aviso de versão aparece na página pública e na página da spec na Central.
- **Fala:** "B, na pública e na Central"

## D13 — Base da URL pública
- **Data:** 2026-11-22
- **Origem:** TG-61#P2, rodada de ajuste r2, fechamento
- **Decisão:** `--url-publica` ou `ORQ_WEB_PUBLICO_URL`; sem nenhum dos dois, o `gerar` recusa com `url_publica_ausente`, sem criar o link, e nunca usa `ORQ_WEB_URL`.
- **Fala:** "Aprovado"

## D14 — Texto exposto sem nome de máquina nem de módulo
- **Data:** 2026-11-22
- **Origem:** TG-61#P2, rodada de ajuste r2, fechamento
- **Decisão:** a spec, o plano e estas decisões falam em "proxy reverso da máquina da Central" e "módulo de limite de taxa do proxy"; o texto de D4 e D5 foi ajustado nesse sentido, sem mudar a decisão.
- **Fala:** "Aprovado"

## D15 — Mapa do estado simplificado
- **Data:** 2026-11-22
- **Origem:** TG-61#P2, rodada de ajuste r2, fechamento
- **Decisão:** "em validação" para registrada, em validação e mudança além do `decisoes.md`; "aprovada" para aprovada e mesclada; "entregue" com a US encerrada na Central.
- **Fala:** "Aprovado"

## D16 — Forma do aviso de versão
- **Data:** 2026-11-22
- **Origem:** TG-61#P2, rodada de ajuste r2, fechamento
- **Decisão:** sem aviso na versão 1; a partir da 2, "Versão N — substituída em DD/MM/AAAA HH:MM", na pública e na Central.
- **Fala:** "Aprovado"

## D17 — Transformação de links por destino
- **Data:** 2026-11-22
- **Origem:** TG-61#P2, rodada de ajuste r2, fechamento
- **Decisão:** a transformação é parametrizada pelo destino (aba na Central, âncora na página pública); nenhum `href` da página pública começa por `/p/` nem cita projeto ou ref.
- **Fala:** "Aprovado"

## D18 — Número de versão no registro da spec
- **Data:** 2026-11-22
- **Origem:** TG-61#P2, rodada de ajuste r2, fechamento
- **Decisão:** a migration desta US cria a versão do registro da spec, calculada para os registros existentes pelas substituições já feitas; o registro com `--substituir` a incrementa; o aviso na página da spec da Central entra nesta US.
- **Fala:** "Aprovado"

## D19 — Spec aprovada
- **Data:** 2026-11-22
- **Origem:** TG-61#P3, respondida em 2026-11-22
- **Decisão:** spec, plano e decisões aprovados no SHA `8586a39aeaaa63625ec9e69e510fa42d0a722d3e`.
- **Fala:** "Aprovar"
