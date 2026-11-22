# TG-61 — Dar ao usuário final acesso às especificações no orq

**Épico:** [Publicar e revisar especificações na Central](../README.md)
**Depende de:** TG-59 (spec registrada) e TG-60 (cache, renderização e transformação de links)
**Status da spec:** em validação (rodada r2)

## O que muda

A Parte 3 do [desenho do épico](../README.md#parte-3--link-compartilhável-orq-61) é implementada: links públicos, só leitura e revogáveis por spec, servidos num socket próprio, gerados e revogados pela CLI via coordenador. O usuário final não escreve nada pelo link; a análise dele chega pelo responsável, na resposta à pendência de validação.

## Comandos

```
orq spec link gerar TG-<ref> --para "<rótulo>" [--expira 30d] [--url-publica <base>]
orq spec link listar TG-<ref>
orq spec link revogar TG-<ref> <id>
```

- **Vários links ativos por spec**, cada um com rótulo e revogado individualmente.
- `gerar` imprime a URL **uma única vez**; o orq guarda só o hash e não consegue mostrá-la de novo.
- **Base da URL pública:** `--url-publica` ou, sem a flag, `ORQ_WEB_PUBLICO_URL` (ambiente ou `orq.env`). Sem nenhuma das duas, o `gerar` recusa com `url_publica_ausente` e não cria o link. Nunca usa o `ORQ_WEB_URL`, que é o da Central, atrás do login.
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

## Página pública

- **Uma página só**, em `GET /s/{codigo}`: todos os arquivos do conjunto exposto em seções, na ordem Spec, Plano, Decisões, README do épico e specs compartilhadas, com um índice de âncoras no topo. Não há parâmetro nem rota por arquivo.
- **Links relativos:** link para um arquivo do conjunto exposto vira âncora da seção (`#<arquivo>`, por exemplo `#plano`); qualquer outro link relativo vira texto; imagens removidas. A transformação da TG-60 é parametrizada pelo destino do link (aba na Central, âncora na página pública).
- **Nenhum `href` da página pública começa por `/p/` nem cita projeto ou ref.** Nada de outras US, ondas, pendências, nomes de pane, ref da forja, SHA ou link da MR.
- **Faixa do topo:** título da US, hora da leitura e estado simplificado:
  - **em validação**: spec registrada, em validação, ou com mudança além do `decisoes.md` depois da aprovação;
  - **aprovada**: aprovada ou com a MR da spec mesclada;
  - **entregue**: US encerrada na Central.
- **Versão do registro:** a partir da versão 2, a faixa mostra "Versão N — substituída em DD/MM/AAAA HH:MM"; na versão 1 não há aviso.

## Conteúdo exposto

- A versão mostrada segue a regra de ref da Central: branch de origem com a MR da spec aberta, alvo depois de mesclada; lida do cache da TG-60.
- Só a allowlist da Parte 3 do desenho.

## Substituição da spec com links ativos

- O `orq spec registrar --substituir` (TG-59) **não mexe nos links**: eles seguem e passam a mostrar a spec nova. A saída avisa "N links ativos passam a mostrar a spec nova".
- O registro da spec ganha um **número de versão**: 1 no registro inicial, +1 a cada `--substituir`. Esta US cria a coluna (com o valor dos registros existentes calculado pelas substituições no diário) e faz o `RegistrarSpec` incrementá-la.
- O aviso "Versão N — substituída em DD/MM/AAAA HH:MM" aparece na página pública e na página da spec na Central (`/p/{projeto}/{ref}/spec`, TG-60); o da Central entra nesta US.

## Proxy reverso (fora do repositório)

O coordenador aplica e valida a configuração do proxy reverso da máquina da Central depois do merge, com autorização própria:

- o socket público liberado sem login; a Central continua atrás do login;
- **limite de taxa por origem no proxy**, com o módulo de limite de taxa dele; o orq não implementa limite;
- log de acesso desse socket sem o caminho, ou com o código mascarado.

## Critérios de aceite

- `gerar` cria código de 256 bits, guarda só o hash, imprime a URL uma vez, com evento no diário; vários links ativos por spec com rótulos.
- `gerar` sem `--url-publica` nem `ORQ_WEB_PUBLICO_URL` recusa com `url_publica_ausente`, sem criar link; nunca usa `ORQ_WEB_URL`.
- `--expira` opcional; link expirado responde o mesmo 404 do revogado e do inexistente (corpo e cabeçalhos idênticos).
- `revogar` é definitivo; `gerar` depois de revogar cria código novo.
- `listar` mostra rótulo, criação, validade, estado, último acesso e contador.
- Handler público só com a interface mínima; não responde a nenhuma rota da Central.
- Página pública numa página só, com índice e seções; link para arquivo do conjunto vira âncora; os demais, texto.
- **Nenhum `href` da página pública começa por `/p/` nem cita projeto ou ref** (teste varre o HTML).
- Faixa pública com título, hora e estado simplificado conforme o mapa; sem ref, SHA nem link da MR.
- `--substituir` mantém os links, avisa quantos estão ativos e incrementa a versão; a faixa pública e a da Central mostram "Versão N — substituída em …" a partir da versão 2, e nada na versão 1.
- Página pública com os três cabeçalhos, só a allowlist, sem nome de pane.
- Acesso válido atualiza último acesso e contador, sem evento no diário.
- `go vet`, `go test -race ./...`, `gofmt -l .` limpos; frontend rebuildado se o CSS mudar.
- Depois do merge, validado pelo coordenador no proxy da máquina da Central: link abre sem login, revogado não abre, Central pede login, limite de taxa ativo no proxy, log sem o código.
