# Decisões — TG-60

## D1 — Renderização com goldmark no servidor
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** a spec é renderizada no servidor pelo goldmark já usado na Central; sem markdown-it no cliente.
- **Fala:** "A, goldmark no servidor"

## D2 — Página da spec com abas por arquivo
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** uma aba por arquivo (Spec, Plano, Decisões, README do épico, specs compartilhadas), com faixa de estado, ref, SHA, hora e link da MR no topo.
- **Fala:** "A, abas por arquivo"

## D3 — Spec dentro da página da pendência de validação
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** a página da pendência `validacao` mostra as abas lidas pelo SHA dela, ao lado das opções.
- **Fala:** "A, na própria página da pendência"

## D4 — Limite do cache
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** 1 MiB por arquivo e 4 MiB por spec; acima disso, aviso com o link da MR.
- **Fala:** "Opção B"

## D5 — Releitura só quando o SHA muda
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** o daemon só relê os arquivos quando o SHA da ref muda.
- **Fala:** "A, reler só quando o SHA muda"

## D6 — Rota da página da spec
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** `GET /p/{projeto}/{ref}/spec`.
- **Fala:** "Aprovado"

## D7 — Cache por par (spec, SHA)
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** guarda a versão da ref atual e a de cada `validacao` aberta; a versão da pendência nunca é substituída.
- **Fala:** "Aprovado"

## D8 — Links relativos
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** link relativo para arquivo da allowlist vira link de aba; os demais viram texto, com a mesma transformação da TG-61.
- **Fala:** "Aprovado"

## D9 — TUI fora do épico
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** o TUI não ganha indicador nem spec.
- **Fala:** "Aprovado"
