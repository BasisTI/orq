# TG-61 — Dar ao usuário final acesso às especificações no orq

**Épico:** [Publicar e revisar especificações na Central](../README.md)
**Depende de:** TG-59 (spec registrada) e TG-60 (cache, renderização e transformação de links)
**Status da spec:** em validação

## O que muda

A Parte 3 do [desenho do épico](../README.md#parte-3--link-compartilhável-orq-61) é implementada: links públicos, só leitura e revogáveis por spec, servidos num socket próprio, gerados e revogados pela CLI via coordenador. O usuário final não escreve nada pelo link; a análise dele chega pelo responsável, na resposta à pendência de validação.

## Comandos

```
orq spec link gerar TG-<ref> --para "<rótulo>" [--expira 30d]
orq spec link listar TG-<ref>
orq spec link revogar TG-<ref> <id>
```

- **Vários links ativos por spec**, cada um com rótulo e revogado individualmente.
- `gerar` imprime a URL **uma única vez**; o orq guarda só o hash e não consegue mostrá-la de novo.
- `--expira` é opcional; sem ele, o link vale até ser revogado.
- `listar` mostra id, rótulo, criação, validade, estado (ativo, revogado, expirado), último acesso e contador de acessos.
- **Revogação definitiva:** gerar de novo cria código novo; o `gerar` nunca devolve código revogado.
- Gerar e revogar são escritas da Central: transação curta com evento no diário. A web não ganha botão nem escrita.

## Código

32 bytes de `crypto/rand` em base64url (256 bits). O `central.db` guarda só o hash SHA-256; a consulta é pelo hash, sem comparar o código em claro.

## Socket público

- `orq daemon --web-publico <socket>`: socket próprio, que só responde à rota `GET /s/{codigo}` (e aos estáticos que a página usa).
- O handler recebe uma interface mínima: "spec por hash de código" e `marcarAcesso(hash)`, sem `Snapshot` nem `Store` inteira.
- `marcarAcesso` grava último acesso e contador fora do diário, como o cache dos enriquecedores.
- Código inexistente, revogado e expirado dão a **mesma resposta 404**, indistinguível.
- Cabeçalhos: `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, `X-Robots-Tag: noindex`.

## Conteúdo exposto

- A versão mostrada segue a regra de ref da Central: branch de origem com a MR da spec aberta, alvo depois de mesclada; lida do cache da TG-60.
- Só a allowlist da Parte 3 do desenho; links relativos fora dela viram texto, imagens removidas; nada de outras US, ondas, pendências ou nomes de pane.

## Caddy (fora do repositório)

O coordenador aplica e valida a configuração do Caddy na `cedagente` depois do merge, com autorização própria:

- o socket público liberado sem login; a Central continua atrás do `basic_auth`;
- **limite de taxa por origem no Caddy**, com o plugin `caddy-ratelimit` (o build do Caddy precisa tê-lo); o orq não implementa limite;
- log de acesso desse socket sem o caminho, ou com o código mascarado.

## Critérios de aceite

- `gerar` cria código de 256 bits, guarda só o hash, imprime a URL uma vez, com evento no diário; vários links ativos por spec com rótulos.
- `--expira` opcional; link expirado responde o mesmo 404 do revogado e do inexistente (corpo e cabeçalhos idênticos).
- `revogar` é definitivo; `gerar` depois de revogar cria código novo.
- `listar` mostra rótulo, criação, validade, estado, último acesso e contador.
- Handler público só com a interface mínima; não responde a nenhuma rota da Central.
- Página pública com os três cabeçalhos, só a allowlist, links relativos fora dela como texto, sem nome de pane.
- Acesso válido atualiza último acesso e contador, sem evento no diário.
- `go vet`, `go test -race ./...`, `gofmt -l .` limpos.
- Depois do merge, validado pelo coordenador na `cedagente`: link abre sem login, revogado não abre, Central pede login, limite de taxa ativo no Caddy, log sem o código.
