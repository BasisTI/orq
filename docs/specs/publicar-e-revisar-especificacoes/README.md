# Publicar e revisar especificações na Central

**Épico Taiga:** orq — "Publicar e revisar especificações na Central"
**US cobertas por este desenho:** orq #59, #60, #61
**Fora deste desenho:** #62 (protótipos em imagem), #63 (exemplos reais), #64 (mensagens ao coordenador)
**Depende de:** orq #52 (papel do planejador, em Ready for test em 2026-11-21)
**Data:** 2026-11-21
**Origem:** conversa de brainstorming entre o responsável pelo orq e o Assistente Global, 2026-11-12 a 2026-11-21

## Por que

Hoje, a spec de uma US é difícil de achar inteira. O levantamento de 2026-11-12 no portal-liven, colaboradados, plataforma-iac e triagem.ai encontrou:

- Dois sistemas de spec convivendo em cada projeto. Um é o design datado por entrega (`docs/superpowers/specs/AAAA-MM-DD-...-design.md`). O outro é a spec "viva" por módulo ou feature (`docs/specs/absorcao-painel/produto/SPEC-D-formulario.md`, `apps/beneficios/spec/features/F016-...md`, `docs/specs/SPEC-sgo.md`).
- Spec e plano da mesma US em árvores diferentes. Os planos aparecem em `superpowers/plans/`, `planos/` e em `PLANO-*` dentro de `specs/`.
- Decisões tomadas durante a implementação em seções avulsas ("Decisões deste plano", "Decisões tomadas nesta rodada", "Decisões da implementação"). As decisões da conversa com o planejador ficam só no `retorno-planejamento.md`, em `~/tmp/orquestrador`, que o `orq limpar` apaga.
- O orq sem nenhum registro de onde estão a spec e o plano de uma story.

Hoje a spec é usada em três momentos:

1. **No planejamento**, para validar a spec e às vezes levar pontos ao usuário do sistema. Para isso o responsável monta à mão uma versão apresentável no `cedagente-static`, o que cria mais uma fonte de dados.
2. **Na decisão de produto.** Hoje se resolve perguntando ao agente.
3. **Durante e depois da onda**, para responder dúvidas sobre o que foi decidido. Hoje se resolve perguntando ao agente, que pesquisa no ai-memory e nas specs.

## Objetivos

- **Fonte única.** A spec existe só no repositório do projeto, em pasta previsível. A Central apenas renderiza o que está lá, sem cópia.
- **Spec completa num lugar só.** Spec completa = spec + plano + decisões tomadas, na mesma pasta da US.
- **Versão apresentável junto do resto.** A spec aparece na Central ao lado do status, da onda e das pendências da US.
- **Link compartilhável para o usuário do sistema.** Um link por spec, só leitura e revogável, como os links do Google Drive. Ele substitui o uso do `cedagente-static` para specs.
- **Validação pela Central.** O responsável aprova ou pede ajustes pela pendência de validação. O coordenador conduz o fluxo, porque tem a skill `orquestrador`.

## Não objetivos

- Editar a spec pela web.
- Login individual para o usuário do sistema. Entra com o multi-usuário (#56, épico "Operar o orq com vários usuários e várias máquinas").
- Comentário ou registro de análise do usuário do sistema pelo link. *(Decidido pelo responsável, 2026-11-21.)* O retorno dele chega pelo responsável: a análise do usuário final (aceite, pontos a melhorar) entra pela resposta do responsável à pendência de validação e vai para o `decisoes.md`. A descrição da #61 no Taiga é ajustada pelo coordenador para não contradizer isto.
- Busca de decisões na Central. Para pesquisar, o caminho continua sendo perguntar ao agente, que consulta o ai-memory e as specs.
- Spec "viva" por módulo. A spec de uma US fica congelada depois da entrega como registro do que foi feito. Se um manual for necessário, ele é gerado a partir das regras e decisões.
- Protótipos em imagem, exemplos reais e mensagens ao coordenador (#62, #63 e #64).

## Anti-objetivos

- Qualquer segunda cópia mantida à mão (HTML estático, export) que possa divergir do repositório.
- O link compartilhado dar acesso a qualquer coisa além daquela spec: outras US, ondas, pendências ou nomes de pane.
- A Central ganhar um mecanismo novo de escrita além das que já existem (resposta de pendência e mensagem ao coordenador, #64). Gerar e revogar link fica na CLI.

## Parte 1 — Estrutura padrão de documentação (orq #59)

Decidida na conversa e válida para todos os projetos coordenados pelo orq:

```
docs/
├── decisoes/        ADR-NNN-titulo.md — decisões que valem para o projeto todo
├── entradas/        análise do que já existe: legado, documentação, levantamentos
├── ideias/          visão e requisitos de alto nível que ainda não viraram US
├── runbooks/
└── specs/
    ├── <épico>/
    │   ├── README.md            visão do épico, índice das US e specs que abrangem várias US
    │   └── tg-<ref>-<nome-curto>/
    │       ├── spec.md
    │       ├── plano.md
    │       └── decisoes.md
    └── sem-epico/
        └── tg-<ref>-<nome-curto>/
```

Regras decididas:

- **Plano na pasta da US**, sem `plans/` na raiz. Foi uma mudança sobre a primeira proposta do responsável, aceita na conversa.
- **Nome da pasta da US com o número da story** (`tg-123-...`). Dentro de um repositório o número não se repete. Repositórios que compartilham um board (argocd-apps, ci-templates no infra-2025) têm cada um a própria `docs/`.
- **`decisoes.md` da US** é o diário das decisões: as da conversa com o planejador, as respostas de pendência e os ajustes de plano durante a onda. Cada entrada traz data e origem (por exemplo, `TG-331#P1, respondida em 2026-11-11`). **Quem escreve nele escreve para o cliente:** o `decisoes.md` é lido pelo usuário do sistema pelo link (Parte 3). Origem sempre por referência (`TG-331#P1`), nunca nome de pane, e sem comentário interno (por exemplo, sobre o próprio cliente). Se a resposta de pendência tiver texto interno, o coordenador registra só a decisão. A entrada de aprovação cita o SHA aprovado (fluxo, passo 5).
- **Decisão que vale além da US** é proposta como ADR em `docs/decisoes/`, pelo planejador ou pelo coordenador. É o mesmo movimento do ADR-013 do portal-liven.
- **Spec congelada após a entrega.** Depois que a US é entregue, `spec.md`, `plano.md` e `decisoes.md` não são reescritos.
- **`entradas/` e `ideias/`** seguem o que já existe: o `docs/modernizacao/` do portal-liven, com o ciclo anotada → promovida → fechada descrito no README dele, e o `docs/` do odoo customizacoes.
- **Superpowers e demais skills de spec** gravam na pasta da US, e não em `docs/superpowers/`. A instrução vai no `AGENTS.md` de cada projeto e no pacote do planejador.

### Verificação e reorganização antes de planejar

Antes de abrir um planejamento, o coordenador verifica se o projeto segue a estrutura. Se não segue, mostra o que está fora do padrão e pergunta ao responsável se pode reorganizar. Decidido: **projetos fora do padrão são reorganizados**, e as specs antigas são movidas para a nova estrutura.

- Spec antiga que cobre uma US vai para a pasta dessa US.
- Spec antiga que abrange várias US (como `SPEC-D-formulario`, para tg44 a tg47, ou `F016-coparticipacao`, para TG-249 e TG-323) fica **inteira** no nível do épico, junto do `README.md` do épico. Cada pasta de US aponta para ela. O texto não é dividido.
- A reorganização de cada projeto é trabalho do coordenador daquele projeto, com autorização própria, depois que a #59 estiver pronta. Não faz parte da implementação do orq.

### Fluxo planejador → coordenador → orq

0. O planejamento acontece dentro de uma onda com limite "até plano", como já ocorre hoje (por exemplo, a onda 198 do orq). Se ainda não houver US para a spec, o coordenador cria a US em New antes de planejar, e a ref dela passa a ser a chave da spec. *(Escolha do Assistente Global.)* Criar a US em New e, no passo 5, passá-la para Ready usa a autorização de transição de status do coordenador (`taiga-mcp.md#atualizar-o-status`), não a do planejamento, que não autoriza escrever no Taiga (`planejamento.md`).
1. Hoje o planejador não tem worktree nem branch: o resultado do planejamento vai para `<raiz>/<projeto>/TG-<ref>/planejamento/`. Neste fluxo, **antes de abrir o planejador**, o coordenador cria o worktree e a branch da US (o cwd do pane do planejador é o worktree) e grava os recursos da US (`orq recursos gravar`); sem isso o `orq limpar` recusa a US. O planejador grava `spec.md`, `plano.md` e `decisoes.md` na pasta da US, **na branch da US** (`TG-<ref>`, ou o nome com sufixo de rodada numa reabertura; o orq usa o nome registrado na MR, não o deduz), e continua gravando o `retorno-planejamento.md` como hoje (#52).
2. **MR só da spec** *(decidido pelo responsável)*: o coordenador abre, a partir da branch do planejamento (a "branch da spec"), uma MR que contém apenas a pasta da spec. O código vem depois, em MR própria e em branch própria (passo 5a).
3. O coordenador registra no orq a spec daquela US: repositório e caminho relativo da pasta, **e a MR da spec como MR da US** (`orq mr add`). O registro da spec guarda a **URL da MR da spec**, porque a US pode ter duas MRs (spec e código) e o `orq mr add` não marca qual é qual. É dessa URL que o orq tira a ref de leitura e o estado "aberta" ou "mesclada" (Parte 2).
4. O coordenador abre a pendência de validação: "Spec da TG-<ref> pronta para validar", com as opções **Aprovar** e **Pedir ajustes**, mais o texto livre que a pendência já aceita. A pendência **registra o SHA da cabeça da MR da spec** no momento da abertura. A Central mostra esse SHA e renderiza essa versão, não a cabeça móvel, ao lado do link da MR da spec. Se a cabeça andar depois, a pendência continua apontando para o SHA que ela abriu.
5. **Aprovar:** a resposta entra no `decisoes.md`, e a entrada cita o **SHA aprovado**. Antes de mesclar, o coordenador confere que, entre o SHA aprovado e a cabeça da MR, só mudou o `decisoes.md`; se mudou mais, não mescla e abre nova pendência. Então a MR da spec é mesclada na branch base pelo fluxo normal de revisão e merge do projeto (na revisão, o `--aprovado <url>=<sha>` do orq), e o coordenador passa a US de New para Ready. Assim a spec chega à branch base antes do código.
5a. **Da spec para o código.** Depois do merge da MR da spec, a limpeza automática da US roda, e isso é esperado: ela leva o worktree e a branch do planejamento (a MR da spec está mesclada e `recursos.json` está gravado). Para o código, o coordenador roda `orq recursos reabrir <ref>`, cria worktree e branch **novos, com sufixo de rodada**, regrava os recursos e liga a MR do código com `orq mr add`. **Nunca reaproveita o worktree nem o nome de branch da MR da spec**: recriar `TG-<ref>` sem commit novo a deixa integrada e a limpeza a apaga de novo. Os panes seguem, porque a US segue aberta no Taiga.
6. **Pedir ajustes:** a resposta entra no `decisoes.md`, e o coordenador devolve os pontos ao planejador para uma nova rodada na mesma MR. A nova rodada exige nova pendência, com o novo SHA.
7. Durante a onda, o coordenador acrescenta ao `decisoes.md` as respostas de pendência e os ajustes de plano daquela US, no worktree e na branch do código (passo 5a), e elas seguem na MR do código.
8. Depois do merge do código, a pasta da US fica congelada.

## Parte 2 — Visualização na Central (orq #60)

- O card da US na Central dá acesso à spec completa da US, com `spec.md`, `plano.md`, `decisoes.md` e, quando houver, o `README.md` do épico e as specs compartilhadas do épico que a pasta da US referencia (conjunto definido na Parte 3, "Conjunto exposto").
- **De onde o conteúdo vem** *(escolha do Assistente Global; leitura pela API da forja decidida pelo responsável, 2026-11-21)*: o daemon lê os arquivos **pela API da forja** (`gh api` ou `glab api` de arquivo numa ref), que os enriquecedores já usam só para ler. A ref vem da MR da spec registrada como MR da US (fluxo, passo 3): a **branch de origem** enquanto a MR da spec estiver aberta e a **branch alvo** depois de mesclada. Essa regra vale para todos os arquivos da spec, inclusive o `decisoes.md`: com a MR da spec mesclada, a Central e o link leem o `decisoes.md` da **base**, a versão revisada e mesclada, **mesmo enquanto a MR do código está aberta**. As entradas que o passo 7 acrescenta aparecem na Central e no link só depois do merge do código *(decisão do coordenador, r3)*. O daemon não deduz o nome da branch (na reabertura ela ganha sufixo de rodada) e continua só leitura, sem `git fetch`; nada depende do checkout local, do `main` dele nem de worktrees que o `orq limpar` apaga (a limpeza apaga a branch da US depois do merge, e num squash o conteúdo só existe no commit de squash do remoto). O daemon guarda no `central.db` o último conteúdo lido, **com o SHA** da ref, e, quando a leitura falha (forja fora do ar, branch apagada, limite de taxa), a Central mostra esse conteúdo com o SHA e a hora da leitura. A web continua sem ler o disco nem a forja. Esse cache é gerado pelo daemon, não é uma segunda fonte mantida à mão.
- **O cache da spec é um mecanismo novo**, não a mesma coisa do detalhe da pendência. O detalhe da pendência é gravado uma vez na importação e nunca refrescado, tem limite de 64 KiB e seções obrigatórias; spec, plano e decisões juntos passam desse limite. O cache da spec é refrescado a cada ciclo do daemon, fica separado da pendência e não herda esses limites (o tamanho máximo é decidido pela #60).
- Todos os repositórios coordenados estão na mesma máquina da Central (`cedagente`). Multi-máquina é outro épico (#35).
- A pendência de validação mostra a spec renderizada ao lado das opções, na versão do SHA que ela registrou (fluxo, passo 4), lida da forja por esse SHA e não da cabeça móvel. Como a pendência aponta para a spec registrada (campo no frontmatter, tipo de pendência) e se ela renderiza a spec fora do `detalhe` é decisão da #60 (ver "Deixado para quem implementar").
- A renderização usa o tema da Central.

**Recomendação do Assistente Global, a confirmar na #60:** usar o goldmark no servidor, que a Central já usa em `internal/web/render.go` (`RenderizarMarkdown`, com GFM e sem `WithUnsafe`) para o detalhe da pendência. A US registra o markdown-it no cliente como hipótese inicial. A CSP atual (`script-src 'self'`) permite as duas opções se o markdown-it for embutido nos estáticos.

## Parte 3 — Link compartilhável (orq #61)

- Um link próprio por spec de US, com um código que não se adivinha. **O código** tem no mínimo 128 bits vindos de `crypto/rand` (base64url ou hex). O `central.db` guarda **só o hash** do código, e a consulta é por esse hash, sem comparar o código em claro. Código inexistente e código revogado dão a **mesma resposta (404)**, indistinguível. O socket público tem **limite de taxa** por origem.
- Abre só aquela spec, em modo leitura, sem nada do resto da Central.
- **Conjunto exposto** *(escolha do Assistente Global; ajustada pela revisão r1, S4)*: é uma allowlist, e o conteúdo vem de branch escrita por agente, logo não é confiável. Entram só:
  1. arquivos regulares `.md` **diretamente** na pasta da US registrada (sem subpastas, sem symlink);
  2. o `README.md` do épico;
  3. os `.md` **diretamente** na pasta do épico (nunca dentro de `tg-*/`, que é de outra US) que algum arquivo da US referencia por link relativo. O caminho do link é normalizado antes da checagem; o que sair da pasta do épico (`../../...`), apontar para outra `tg-*/` ou para symlink fica de fora.

  Todo o resto, inclusive `.csv`, imagem (até a #62 decidir) e qualquer outro arquivo do repositório, aparece só como texto na página pública, sem navegação. O orq não deduz a pasta da US pelo nome (usa a registrada); a dedução do item 3 é só a leitura dos links relativos do próprio conteúdo, com a checagem acima.
- Revogável. Depois de revogado, o link deixa de abrir. **A revogação é definitiva:** gerar de novo depois de revogar cria um **código novo**, nunca reativa o antigo, e o `gerar` nunca devolve um código já revogado.
- **Gerar e revogar pela CLI, via coordenador** *(decidido pelo responsável)*: o responsável pede ao coordenador ("gera o link da TG-123", "revoga o link da TG-123"), e o coordenador usa um comando do orq. A web não ganha botão nem escrita nova.
- Hoje a Central inteira fica atrás do `basic_auth` do Caddy na `cedagente`, e a autenticação chega pelo cabeçalho `X-Orq-User`. O link precisa passar por fora desse login.
- **Socket separado** *(escolha do Assistente Global)*: a página pública é servida num socket próprio, que só responde às rotas do link. O Caddy libera sem login apenas esse socket, de modo que um erro de roteamento não expõe a Central.
- **Handler público restrito:** o handler do socket público recebe uma interface de leitura mínima ("spec por hash de código"), sem acesso ao `Snapshot` nem à `Store` inteira. O renderer atual (`internal/web/render.go`) deixa passar link relativo; para a página pública é preciso uma **transformação nova** que converta link relativo em texto (e mantenha as imagens removidas).
- **Cabeçalhos e logs**, porque o código vai na URL: a página pública responde com `Referrer-Policy: no-referrer`, `Cache-Control: no-store` e `X-Robots-Tag: noindex`; o log de acesso do Caddy para esse socket não grava o caminho (ou grava com o código mascarado).
- **Critério de pronto da #61:** inclui a configuração do Caddy na `cedagente` ajustada e validada (link abre sem login, link revogado não abre, a Central continua pedindo login, limite de taxa ativo no socket público, log sem o código), os cabeçalhos acima, o código de ≥128 bits com só o hash guardado, o 404 idêntico e a transformação de links relativos. A configuração do Caddy fica no host, fora deste repositório.

## Restrições

- A Central não ganha escrita nova: continuam só as que já existem (resposta de pendência e mensagem ao coordenador, `POST /onda/{id}/mensagens`, da #64). Gerar e revogar link fica na CLI.
- Pane do Herdr nunca aparece (PRODUCT.md), nem na Central nem no link.
- Specs podem conter informação interna do cliente. O link expõe exatamente o conteúdo descrito na Parte 3, e nada além.
- Documentação em português, sem nome de pessoa nas instruções dos repositórios (usar o papel).

## Escolhas do Assistente Global (não decididas pelo responsável)

- Pasta para US sem épico: `specs/sem-epico/`.
- Nomes de arquivo `spec.md`, `plano.md` e `decisoes.md`, em português, como o resto da estrutura.
- O orq usa o caminho registrado pelo coordenador e não deduz a pasta pelo nome.
- Goldmark como recomendação para a #60 (ver Parte 2).
- Planejamento dentro de onda "até plano"; US criada em New antes de planejar quando não existir (fluxo, passo 0).
- Leitura do conteúdo com cache no `central.db` (Parte 2). *Mudança da r2:* a leitura deixou de ser "pelo git local" e passou a ser pela API da forja, por decisão do responsável (2026-11-21), porque a versão anterior era inviável depois do merge.
- Conteúdo do link: allowlist de `.md` da pasta da US + README do épico + `.md` do épico referenciados por link relativo normalizado; demais links relativos só como texto (Parte 3). *Mudança da r2:* "apontar" passou a significar link relativo, e a allowlist foi escrita porque o texto anterior era ambíguo.
- Socket separado para a página pública e Caddy no critério de pronto da #61 (Parte 3).
- Na #59, a verificação da estrutura antes de planejar é instrução da skill `orquestrador`, sem comando novo no orq.

## Deixado para quem implementar

- Modelo de dados da spec registrada e do link (tabelas e migrations no `central.db`), comandos da CLI para registrar a spec e para gerar e revogar link, respeitando o que a Parte 3 já fixa (código, hash, 404, revogação definitiva).
- Como a pendência de validação aponta para a spec registrada e para o SHA (campo novo no frontmatter ou tipo de pendência) e se ela renderiza a spec fora do `detalhe`; o limite de tamanho do cache da spec.
- Rota e layout da página da spec e da página pública do link.
- Formato exato das entradas do `decisoes.md`.
- Texto da instrução no `AGENTS.md` dos projetos e nas referências e assets da skill `orquestrador` (`references/planejamento.md`, `assets/pacote-planejamento.md`, `assets/retorno-planejamento.md`).
