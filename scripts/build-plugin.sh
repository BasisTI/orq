#!/bin/sh
# Build do plugin do Herdr (o [[build]] do herdr-plugin.toml): gera bin/orq na raiz do plugin.
#
# O build não recebe o contexto do plugin e pode rodar com PATH curto, então o go é procurado
# no PATH e, depois, nos lugares em que ele costuma ser instalado. Precisa de Go 1.27 ou mais
# novo; ORQ_GO aponta outro binário.
set -eu
cd "$(dirname "$0")/.."

go_bin=${ORQ_GO:-}
if [ -z "$go_bin" ]; then
	go_bin=$(command -v go 2>/dev/null || true)
fi
if [ -z "$go_bin" ]; then
	for c in /usr/local/go/bin/go "$HOME/go/bin/go" "$HOME/.local/go/bin/go" "$HOME/sdk/go/bin/go" \
		/home/linuxbrew/.linuxbrew/bin/go /opt/homebrew/bin/go /usr/lib/go/bin/go /usr/bin/go; do
		if [ -x "$c" ]; then
			go_bin=$c
			break
		fi
	done
fi
if [ -z "$go_bin" ]; then
	echo "orq: go não encontrado (PATH, /usr/local/go/bin, ~/go/bin, /home/linuxbrew/.linuxbrew/bin…); instale Go 1.27+ ou defina ORQ_GO" >&2
	exit 1
fi

versao=dev
commit=
if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
	versao=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
	versao=${versao#v}
	commit=$(git rev-parse HEAD 2>/dev/null || true)
fi

mkdir -p bin
CGO_ENABLED=0 "$go_bin" build -trimpath -ldflags "-X github.com/BasisTI/orq/internal/cli.Version=$versao -X github.com/BasisTI/orq/internal/cli.Commit=$commit" -o bin/orq ./cmd/orq
echo "orq: bin/orq $versao ($("$go_bin" version))"
