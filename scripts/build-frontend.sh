#!/bin/sh
# Regenera internal/web/estatico a partir de internal/web/frontend (Tailwind + daisyUI + htmx +
# fontes). Só quem edita modelo ou input.css precisa de Node (24, em frontend/.nvmrc); go build,
# go install e scripts/build-plugin.sh não usam. Rode da raiz ou de qualquer pasta, e commite o
# resultado: o job frontend do CI refaz o build e falha se estatico/ não bate.
set -eu
cd "$(dirname "$0")/../internal/web/frontend"

# `npx tailwindcss` fora de frontend/ iria ao registro (o pacote tailwindcss v4 não tem bin) e
# poderia instalar uma versão não travada: por isso o cd acima e o --no-install abaixo.
npm ci --no-audit --no-fund
npx --no-install tailwindcss -i input.css -o ../estatico/app.css --minify

cp node_modules/htmx.org/dist/htmx.min.js ../estatico/htmx.min.js
mkdir -p ../estatico/fontes
for f in carlito-latin-400-normal carlito-latin-700-normal; do
	cp "node_modules/@fontsource/carlito/files/$f.woff2" ../estatico/fontes/
done
for f in inter-tight-latin-600-normal inter-tight-latin-700-normal; do
	cp "node_modules/@fontsource/inter-tight/files/$f.woff2" ../estatico/fontes/
done
cp node_modules/@fontsource/carlito/LICENSE ../estatico/fontes/LICENSE-carlito.txt
cp node_modules/@fontsource/inter-tight/LICENSE ../estatico/fontes/LICENSE-inter-tight.txt
