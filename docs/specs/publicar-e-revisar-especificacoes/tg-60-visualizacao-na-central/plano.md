# Plano — TG-60

Spec: [spec.md](spec.md). Decisões: [decisoes.md](decisoes.md). Começa depois do merge da TG-59.

## Passos

1. **Migration** `internal/central/migrations/NNNN_spec_cache.sql` (número livre no `main`): tabela do cache da spec por (spec, SHA), com arquivo, conteúdo, tamanho, "grande demais", hora da leitura e erro. Aditiva.
2. **Cache** em `internal/central/cache.go` (ou `spec_cache.go`): gravar e ler por (spec, SHA); manter a versão da ref atual e as de `validacao` aberta; descartar as demais.
3. **Leitor da forja** em `internal/enriquecer`: SHA atual da ref (origem pela MR já enriquecida; alvo por consulta à branch), listagem da pasta e leitura de arquivo na ref (`gh api repos/{o}/{r}/contents/{caminho}?ref=`, `glab api projects/{id}/repository/files/{caminho}/raw?ref=`), allowlist e normalização de links relativos, limites de 1 MiB/4 MiB.
4. **Laço do daemon**: por spec registrada, comparar o SHA da ref com o cache e reler só se mudou; para cada `validacao` aberta sem versão no cache, ler pelo SHA dela. Falha vira `Erro` no cache.
5. **Renderização** em `internal/web/render.go`: transformação de links relativos (allowlist → link de aba; resto → texto), compartilhada com a TG-61.
6. **Rotas e templates** em `internal/web`: `GET /p/{projeto}/{ref}/spec` (+ fragmento htmx da aba), `tmpl/spec.html`; `tmpl/detalhe.html` com as abas ao lado das opções quando o tipo for `validacao`; link no card da US.
7. **CSS** em `internal/web/frontend/input.css` (abas, duas colunas, faixa) e `scripts/build-frontend.sh`; commitar `internal/web/estatico/`.
8. **Docs**: README (página da spec) e `docs/central-web-v2.md`.

## Testes

- Leitor com forja falsa: ref de origem/alvo, allowlist, links fora da pasta, limites, contagem de chamadas sem mudança de SHA.
- Cache: versão da pendência preservada; falha mostra o último conteúdo.
- Web: abas, faixa, link de aba, texto para link fora da allowlist, ausência de HTML cru, página da pendência com a spec no SHA dela.
