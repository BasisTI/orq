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

## D4 — Caddy aplicado pelo coordenador
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** o coordenador aplica e valida a configuração do Caddy na `cedagente` depois do merge, com autorização própria.
- **Fala:** "C"

## D5 — Limite de taxa no Caddy
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** o limite de taxa por origem fica no Caddy, com o plugin `caddy-ratelimit`; o orq não implementa limite.
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
