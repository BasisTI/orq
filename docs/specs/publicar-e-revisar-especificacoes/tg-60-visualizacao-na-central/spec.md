# TG-60 — Visualizar as especificações em Markdown na Central

**Épico:** [Publicar e revisar especificações na Central](../README.md)
**Depende de:** TG-59 (registro da spec, pendência `validacao`, campo `spec` no snapshot)
**Status da spec:** em validação

## O que muda

A Parte 2 do [desenho do épico](../README.md#parte-2--visualização-na-central-orq-60) é implementada: o daemon lê os arquivos da spec registrada pela API da forja, guarda o conteúdo num cache no `central.db` e a Central os renderiza com o tema dela, numa página da US e dentro da pendência de validação.

## Leitura e cache

- **Fonte:** API da forja (`gh api` / `glab api` de arquivo numa ref), por caminho absoluto, só leitura, sem `git fetch` nem checkout local.
- **Ref:** a da MR da spec registrada — branch de origem com a MR aberta, branch alvo depois de mesclada. Vale para todos os arquivos, inclusive o `decisoes.md`.
- **Conjunto lido:** a allowlist da Parte 3 do desenho (`.md` diretamente na pasta da US, `README.md` do épico, `.md` do épico referenciados por link relativo normalizado).
- **Releitura só quando o SHA da ref muda:** a cada ciclo o daemon compara o SHA atual da ref com o do cache e só busca os arquivos quando ele mudou.
- **Cache por par (spec, SHA):** mantém a versão da ref atual e a de cada `validacao` aberta. A versão de uma pendência nunca é substituída.
- **Limite:** 1 MiB por arquivo e 4 MiB por spec. Acima do limite, o arquivo não é guardado e a aba mostra "arquivo grande demais (N KiB), veja na MR", com o link da MR.
- **Falha de leitura** (forja fora do ar, branch apagada, limite de taxa): a Central mostra o último conteúdo guardado, com o SHA e a hora da leitura. O cache é gerado pelo daemon, não é estado da Central e não grava diário.

## Renderização

- goldmark no servidor, o mesmo de `internal/web/render.go` (GFM, sem `WithUnsafe`), sem JavaScript novo.
- Link relativo para um arquivo da allowlist vira link para a aba correspondente; os demais links relativos viram texto (a mesma transformação que a TG-61 usa na página pública). Imagens continuam removidas.
- Tema da Central; a CSP não muda.

## Página da spec

- Rota `GET /p/{projeto}/{ref}/spec`, aberta pelo card da US.
- **Abas por arquivo:** Spec, Plano, Decisões e, quando houver, README do épico e specs compartilhadas. Uma aba por vez, trocada por link (htmx ou âncora, sem script inline).
- **Faixa no topo:** estado da spec (da TG-59), ref, SHA, hora da leitura e link da MR da spec.

## Spec na pendência de validação

A página da pendência `validacao` mostra as mesmas abas, lidas pelo SHA da pendência (não pela cabeça), ao lado das opções Aprovar e Pedir ajustes: duas colunas em tela larga, empilhadas no celular.

## Fora

TUI; edição pela web; busca de decisões.

## Critérios de aceite

- Daemon lê os arquivos da allowlist pela forja na ref correta (origem com a MR aberta, alvo depois de mesclada), por caminho absoluto, sem tocar o disco do repositório.
- Sem mudança do SHA da ref, nenhum arquivo é relido (conferido com forja falsa contando chamadas).
- Versão da `validacao` guardada por SHA e preservada quando a cabeça anda.
- Arquivo acima de 1 MiB ou spec acima de 4 MiB: aviso com o link da MR, sem guardar o conteúdo.
- Forja falhando: a página mostra o último conteúdo com SHA e hora da leitura.
- `/p/{projeto}/{ref}/spec` com abas, faixa de estado e link da MR; card da US leva a ela.
- Página da pendência `validacao` mostra a spec no SHA dela ao lado das opções, responsiva.
- Link relativo da allowlist vira link de aba; os demais, texto; HTML cru não passa.
- `go vet`, `go test -race ./...`, `gofmt -l .` limpos; `internal/web/estatico/` rebuildado e `npm run contraste` passando.
