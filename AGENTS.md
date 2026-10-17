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
- `internal/central`: store SQLite, migrations embutidas (`migrations/NNNN_*.sql`, aplicadas por `PRAGMA user_version`) e regras (status, onda, próxima US, pendências), mais o cache dos enriquecedores (`cache.go`).
- `internal/enriquecer`: consultas ao Herdr, às forjas e ao Taiga, tokens de sidebar, cliente do socket do Herdr e o laço do `orq daemon`.
- `internal/tui`: o `orq tui` (bubbletea v2 + lipgloss v2, de `charm.land`). Não volte ao bubbletea v1: o `init` dele consulta o fundo do terminal (OSC 11) em todo processo, e qualquer comando `orq` num tty que não responde esperava 5 s. `tela.go` desenha a partir do snapshot, sem terminal, e é o que os testes conferem (texto, estilos, OSC 8); `modelo.go` é o laço, o teclado e o mouse.
- `internal/herdrcfg`: edição do `config.toml` do Herdr para `orq plugin configurar|desconfigurar` (lexer que preserva o texto, backup, `herdr config check` e restauração).
- `herdr-plugin.toml` e `scripts/build-plugin.sh`: o plugin do Herdr e o build dele.
- `internal/herdrfalso`: herdr falso dos testes de `orq central abrir` (árvore de splits em JSON, regras do Herdr 0.9.3). Os testes de `internal/cli` o compilam uma vez (`go build`); não entra no binário `orq`.
- `internal/refs`: parse de `projeto/312`, `TG-312`, `TG-312#P2` e URL de MR.
- `internal/output`: envelope de erro `{"error":{"code","cause","recovery"}}` e códigos de saída.
- `docs/snapshot.md`: contrato do `orq snapshot`.

## Convenções

- Mensagens, docs e códigos de erro em português; saída JSON fora de terminal, texto num terminal (`--output` força).
- Toda escrita no banco é uma transação curta (`BEGIN IMMEDIATE` via `_txlock`) e grava o seu evento no diário na mesma transação. A exceção é o que não é estado da Central: cache, suspensão e tokens publicados dos enriquecedores. A mudança de estado de uma MR é estado e gera `mr.estado`.
- Ferramenta externa (`herdr`, `glab`, `gh`, `taiga`) é chamada por caminho absoluto (`enriquecer.Ferramentas`), nunca pelo PATH, e só para ler — as exceções são o `orq central abrir` (workspace e panes da Central), o foco de pane (`herdr agent focus`) e o daemon, que escreve no Herdr apenas os tokens `orq` (pane) e `orq_mrs` (workspace), com `--source orq`. No Herdr o token é por nome, não por fonte: registre em `token_publicado` só depois de um `report-metadata` bem-sucedido, e limpe só se o Herdr ainda mostra o último valor do orq. Valor de outro produtor (ex. `mrs_prontas` da skill manual) não é adotado, sobrescrito nem limpo. Falha de subprocesso vira `Erro` no cache; não derruba o processo.
- A abertura do banco (criação, WAL, migrations) fica dentro do `flock` de `central.db.init.lock`: a troca para WAL num banco novo devolve `SQLITE_BUSY` na hora, sem passar pelo `busy_timeout`.
- O TUI não pode cair por falha de subprocesso ou de leitura: erro vai para a linha de status. Texto de agente passa por `limpar` antes de chegar ao terminal.
- O plugin roda com o ambiente do servidor do Herdr (PATH mínimo): comandos do manifesto chamam `$HERDR_PLUGIN_ROOT/bin/orq` e `$HERDR_BIN_PATH`; `ORQ_*` para eles vem de `orq.env` no config-dir do plugin.
- Não edite o `~/.config/herdr/config.toml` real em teste: use `--config` numa cópia.
- Migration nova é arquivo novo; nunca edite uma já publicada.
- Mudança que remove ou muda o sentido de um campo do snapshot sobe `versao` e atualiza `docs/snapshot.md`.
- Commit: verbo no infinitivo e `orq #N` no final (ex.: `Criar o esqueleto da CLI orq orq #3`). Um PR por US, base `main`.
