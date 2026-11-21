# Plano — TG-61

Spec: [spec.md](spec.md). Decisões: [decisoes.md](decisoes.md). Começa depois do merge da TG-60.

## Passos

1. **Migration** `internal/central/migrations/NNNN_spec_link.sql` (número livre no `main`): tabela de links (id, spec, hash do código único, rótulo, criado em, expira em, revogado em, último acesso, contador). Aditiva.
2. **Store** `internal/central/spec_link.go`: `GerarLink` (32 bytes de `crypto/rand`, base64url, guarda SHA-256, evento `spec.link.gerar` sem o código), `RevogarLink` (evento), `ListarLinks`, `SpecPorHash` (ativo e não expirado) e `MarcarAcesso` (fora do diário).
3. **CLI** `internal/cli/spec.go`: `orq spec link gerar|listar|revogar`; o `gerar` imprime a URL (base configurável do link público) uma vez.
4. **Servidor público** em `internal/web` (arquivo próprio, ex. `publico.go`): mux só com `GET /s/{codigo}` e estáticos necessários; interface mínima; 404 único; cabeçalhos; template `tmpl/publico.html` sem nada da Central.
5. **Daemon** `internal/cli/daemon.go`: flag `--web-publico <socket>`, socket Unix em diretório curto nos testes.
6. **Renderização**: reutilizar a transformação de links da TG-60 (allowlist; resto texto; imagens removidas).
7. **Docs**: README (comandos, flag, configuração esperada do Caddy com `caddy-ratelimit` e log mascarado, como referência para o coordenador) e `docs/central-web-v2.md`.
8. **Depois do merge (coordenador):** aplicar e validar o Caddy na `cedagente`.

## Testes

- Código: tamanho, unicidade, só hash no banco (varredura da tabela não acha o código).
- 404 idêntico para inexistente, revogado e expirado.
- Revogação definitiva e novo código ao gerar de novo.
- Handler público não responde a rotas da Central; cabeçalhos presentes.
- Allowlist e links como texto; ausência de nome de pane.
- Último acesso e contador sem evento no diário.
