# Plano — TG-61

Spec: [spec.md](spec.md). Decisões: [decisoes.md](decisoes.md). Começa depois do merge da TG-60.

## Passos

1. **Migration** `internal/central/migrations/NNNN_spec_link.sql` (número livre no `main`): tabela de links (id, spec, hash do código único, rótulo, criado em, expira em, revogado em, último acesso, contador) e coluna `versao` no registro da spec, preenchida com 1 + o número de `spec.registrar` com substituição no diário. Aditiva.
2. **Store** `internal/central/spec_link.go`: `GerarLink` (32 bytes de `crypto/rand`, base64url, guarda SHA-256, evento `spec.link.gerar` sem o código), `RevogarLink` (evento), `ListarLinks`, `SpecPorHash` (ativo e não expirado) e `MarcarAcesso` (fora do diário). Em `internal/central/spec.go`, o `RegistrarSpec` incrementa `versao` no `--substituir` e devolve quantos links ativos a spec tem.
3. **CLI** `internal/cli/spec.go`: `orq spec link gerar|listar|revogar`; o `gerar` monta a URL com `--url-publica` ou `ORQ_WEB_PUBLICO_URL` e recusa sem elas (`url_publica_ausente`), sem criar o link. O `orq spec registrar --substituir` avisa "N links ativos passam a mostrar a spec nova".
4. **Transformação de links** em `internal/web/render.go`: parametrizar pelo destino — aba da Central (TG-60) ou âncora `#<arquivo>` da página pública; o resto, texto; imagens removidas.
5. **Servidor público** em `internal/web` (arquivo próprio, ex. `publico.go`): mux só com `GET /s/{codigo}` e estáticos necessários; interface mínima; 404 único; cabeçalhos; template `tmpl/publico.html` numa página só, com índice, seções e faixa (título, hora, estado simplificado, aviso de versão a partir da 2), sem ref, SHA, link da MR nem nada da Central.
6. **Central**: aviso "Versão N — substituída em …" na faixa de `/p/{projeto}/{ref}/spec` (`tmpl/spec.html`).
7. **Daemon** `internal/cli/daemon.go`: flag `--web-publico <socket>`, socket Unix em diretório curto nos testes; `ORQ_WEB_PUBLICO_URL` repassado pelo `--detach` se o daemon precisar dele.
8. **CSS** em `internal/web/frontend/input.css` se a página pública precisar e `scripts/build-frontend.sh`.
9. **Docs**: README (comandos, `ORQ_WEB_PUBLICO_URL`, flag, configuração esperada do proxy, como referência para o coordenador) e `docs/central-web-v2.md`.
10. **Depois do merge (coordenador):** aplicar e validar o proxy reverso da máquina da Central.

## Testes

- Código: tamanho, unicidade, só hash no banco (varredura da tabela não acha o código).
- `gerar` sem base: `url_publica_ausente`, nenhum link criado; com `ORQ_WEB_URL` só, também recusa.
- 404 idêntico para inexistente, revogado e expirado.
- Revogação definitiva e novo código ao gerar de novo.
- Handler público não responde a rotas da Central; cabeçalhos presentes.
- Página pública: seções e índice; link para arquivo do conjunto vira âncora; os demais, texto; varredura do HTML sem `href` começando por `/p/` nem contendo projeto ou ref; sem SHA nem URL da MR.
- Faixa: mapa do estado simplificado; aviso de versão ausente na 1 e presente na 2, na pública e na Central.
- `--substituir` com links ativos: links seguem, aviso com a contagem, versão incrementada; migration calcula a versão dos registros existentes.
- Último acesso e contador sem evento no diário.
