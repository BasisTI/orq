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
- Comentário do usuário do sistema pelo link. O retorno dele chega pelo responsável.
- Busca de decisões na Central. Para pesquisar, o caminho continua sendo perguntar ao agente, que consulta o ai-memory e as specs.
- Spec "viva" por módulo. A spec de uma US fica congelada depois da entrega como registro do que foi feito. Se um manual for necessário, ele é gerado a partir das regras e decisões.
- Protótipos em imagem, exemplos reais e mensagens ao coordenador (#62, #63 e #64).

## Anti-objetivos

- Qualquer segunda cópia mantida à mão (HTML estático, export) que possa divergir do repositório.
- O link compartilhado dar acesso a qualquer coisa além daquela spec: outras US, ondas, pendências ou nomes de pane.
- A Central ganhar um mecanismo novo de escrita além da resposta de pendência, que já existe. Gerar e revogar link fica na CLI.

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
- **`decisoes.md` da US** é o diário das decisões: as da conversa com o planejador, as respostas de pendência e os ajustes de plano durante a onda. Cada entrada traz data e origem (por exemplo, `TG-331#P1, respondida em 2026-11-11`).
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

0. O planejamento acontece dentro de uma onda com limite "até plano", como já ocorre hoje (por exemplo, a onda 198 do orq). Se ainda não houver US para a spec, o coordenador cria a US em New antes de planejar, e a ref dela passa a ser a chave da spec. *(Escolha do Assistente Global.)*
1. O planejador grava `spec.md`, `plano.md` e `decisoes.md` na pasta da US, **na branch da US** (`TG-<ref>`), e continua gravando o `retorno-planejamento.md` como hoje (#52).
2. **MR só da spec** *(decidido pelo responsável)*: o coordenador abre na branch da US uma MR que contém apenas a pasta da spec. O código vem depois, em MR própria.
3. O coordenador registra no orq a spec daquela US: repositório e caminho relativo da pasta.
4. O coordenador abre a pendência de validação: "Spec da TG-<ref> pronta para validar", com as opções **Aprovar** e **Pedir ajustes**, mais o texto livre que a pendência já aceita. A pendência mostra a spec renderizada e o link da MR da spec.
5. **Aprovar:** a resposta entra no `decisoes.md`, a MR da spec é mesclada na branch base pelo fluxo normal de revisão e merge do projeto, e o coordenador passa a US de New para Ready. Assim a spec chega à branch base antes do código.
6. **Pedir ajustes:** a resposta entra no `decisoes.md`, e o coordenador devolve os pontos ao planejador para uma nova rodada na mesma MR.
7. Durante a onda, o coordenador acrescenta ao `decisoes.md` as respostas de pendência e os ajustes de plano daquela US. Essas alterações seguem na MR do código da US.
8. Depois do merge do código, a pasta da US fica congelada.

## Parte 2 — Visualização na Central (orq #60)

- O card da US na Central dá acesso à spec completa da US, com `spec.md`, `plano.md`, `decisoes.md` e, quando houver, o `README.md` do épico e as specs compartilhadas do épico para as quais a pasta da US aponta.
- **De onde o conteúdo vem** *(escolha do Assistente Global)*: o daemon lê os arquivos do repositório. Enquanto a MR da spec ou do código estiver aberta, lê da branch da US. Depois do merge, lê da branch base. A leitura é pelo git (ref + caminho), não pela worktree, para não depender de worktrees que o `orq limpar` apaga. O daemon guarda uma cópia com hash no `central.db`, como já faz com o detalhe da pendência, e a web continua sem ler o disco. Essa cópia é cache gerado pelo daemon, não uma segunda fonte mantida à mão.
- Todos os repositórios coordenados estão na mesma máquina da Central (`cedagente`). Multi-máquina é outro épico (#35).
- A pendência de validação mostra a spec renderizada ao lado das opções.
- A renderização usa o tema da Central.

**Recomendação do Assistente Global, a confirmar na #60:** usar o goldmark no servidor, que a Central já usa em `internal/web/render.go` (`RenderizarMarkdown`, com GFM e sem `WithUnsafe`) para o detalhe da pendência. A US registra o markdown-it no cliente como hipótese inicial. A CSP atual (`script-src 'self'`) permite as duas opções se o markdown-it for embutido nos estáticos.

## Parte 3 — Link compartilhável (orq #61)

- Um link próprio por spec de US, com um código difícil de adivinhar.
- Abre só aquela spec, em modo leitura, sem nada do resto da Central.
- **Conteúdo do link** *(escolha do Assistente Global)*: os arquivos da pasta da US, o `README.md` do épico e as specs compartilhadas do épico para as quais a pasta da US aponta. Um link relativo para qualquer outro arquivo do repositório aparece só como texto, sem navegação.
- Revogável. Depois de revogado, o link deixa de abrir.
- **Gerar e revogar pela CLI, via coordenador** *(decidido pelo responsável)*: o responsável pede ao coordenador ("gera o link da TG-123", "revoga o link da TG-123"), e o coordenador usa um comando do orq. A web não ganha botão nem escrita nova.
- Hoje a Central inteira fica atrás do `basic_auth` do Caddy na `cedagente`, e a autenticação chega pelo cabeçalho `X-Orq-User`. O link precisa passar por fora desse login.
- **Socket separado** *(escolha do Assistente Global)*: a página pública é servida num socket próprio, que só responde às rotas do link. O Caddy libera sem login apenas esse socket, de modo que um erro de roteamento não expõe a Central.
- **Critério de pronto da #61:** inclui a configuração do Caddy na `cedagente` ajustada e validada (link abre sem login, link revogado não abre, a Central continua pedindo login). A configuração do Caddy fica no host, fora deste repositório.

## Restrições

- A Central continua somente leitura, com uma única escrita: a resposta de pendência. Gerar e revogar link fica na CLI.
- Pane do Herdr nunca aparece (PRODUCT.md), nem na Central nem no link.
- Specs podem conter informação interna do cliente. O link expõe exatamente o conteúdo descrito na Parte 3, e nada além.
- Documentação em português, sem nome de pessoa nas instruções dos repositórios (usar o papel).

## Escolhas do Assistente Global (não decididas pelo responsável)

- Pasta para US sem épico: `specs/sem-epico/`.
- Nomes de arquivo `spec.md`, `plano.md` e `decisoes.md`, em português, como o resto da estrutura.
- O orq usa o caminho registrado pelo coordenador e não deduz a pasta pelo nome.
- Goldmark como recomendação para a #60 (ver Parte 2).
- Planejamento dentro de onda "até plano"; US criada em New antes de planejar quando não existir (fluxo, passo 0).
- Leitura pelo git (branch da US, depois branch base) com cache com hash no `central.db` (Parte 2).
- Conteúdo do link: pasta da US + README do épico + specs compartilhadas apontadas; demais links relativos só como texto (Parte 3).
- Socket separado para a página pública e Caddy no critério de pronto da #61 (Parte 3).
- Na #59, a verificação da estrutura antes de planejar é instrução da skill `orquestrador`, sem comando novo no orq.

## Deixado para quem implementar

- Modelo de dados da spec registrada e do link (tabelas e migrations no `central.db`), comandos da CLI para registrar a spec e para gerar e revogar link.
- Rota e layout da página da spec e da página pública do link.
- Formato exato das entradas do `decisoes.md`.
- Texto da instrução no `AGENTS.md` dos projetos e nas referências e assets da skill `orquestrador` (`references/planejamento.md`, `assets/pacote-planejamento.md`, `assets/retorno-planejamento.md`).
