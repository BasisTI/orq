# orq

CLI da Central de US do orquestrador multiagente. Go, cobra, SQLite sem cgo (`modernc.org/sqlite`). Desenho: projeto `orq` no Taiga (https://agile.basis.com.br/project/orq).

## Build e testes

```bash
go build ./cmd/orq
go vet ./...
go test -race ./...
gofmt -l .        # tem de sair vazio
```

O CI (`.github/workflows/ci.yml`) roda os três. O teste de concorrência (`internal/cli`) sobe o próprio binário de teste como segundo processo; `go test -short` o pula.

## Layout

- `cmd/orq`: `main`.
- `internal/cli`: comandos cobra e saída em texto.
- `internal/central`: store SQLite, migrations embutidas (`migrations/NNNN_*.sql`, aplicadas por `PRAGMA user_version`) e regras (status, onda, próxima US, pendências).
- `internal/refs`: parse de `projeto/312`, `TG-312`, `TG-312#P2` e URL de MR.
- `internal/output`: envelope de erro `{"error":{"code","cause","recovery"}}` e códigos de saída.
- `docs/snapshot.md`: contrato do `orq snapshot`.

## Convenções

- Mensagens, docs e códigos de erro em português; saída JSON fora de terminal, texto num terminal (`--output` força).
- Toda escrita no banco é uma transação curta (`BEGIN IMMEDIATE` via `_txlock`) e grava o seu evento no diário na mesma transação.
- Migration nova é arquivo novo; nunca edite uma já publicada.
- Mudança que remove ou muda o sentido de um campo do snapshot sobe `versao` e atualiza `docs/snapshot.md`.
- Commit: verbo no infinitivo e `orq #N` no final (ex.: `Criar o esqueleto da CLI orq orq #3`). Um PR por US, base `main`.
