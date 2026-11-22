# Decisões — TG-59

## D1 — O registro da spec e o SHA da validação entram nesta US
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** a TG-59 entrega, além da estrutura e das instruções, o registro da spec no orq e o SHA na pendência de validação. A TG-60 fica com a leitura pela forja, o cache e a renderização.
- **Fala:** "B, o registro da spec e o SHA entram na #59"

## D2 — Pendência de tipo `validacao`, com o SHA gravado pelo orq
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** tipo novo `validacao`, com opções fixas; o orq grava na importação o SHA da cabeça da MR da spec registrada.
- **Fala:** "A, tipo validacao com SHA gravado pelo orq"

## D3 — Validação sem spec é recusada; falha da forja retém
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** sem spec registrada, a importação é recusada com `.erro.txt`; com falha da forja, a pendência fica retida, sem aparecer e sem `.erro.txt`, e o daemon tenta de novo.
- **Fala:** "C, recusa sem spec e retém na falha da forja"

## D4 — Registrar de novo só com `--substituir`, depois de perguntar
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** sem `--substituir`, o registro duplicado é recusado; quem chama avisa o responsável e pergunta se substitui, e só com o sim usa `--substituir`. O evento guarda os valores anteriores.
- **Fala:** "B, quem chama faz uma primeira tentativa sem --substituir, em caso de duplicidade avisa o usuário e pede se deve substituir, se sim use o --substituir, Igual a A deve guardar o evento com os valores anteriores"

## D5 — Conferência antes de mesclar pelo orq e na Central
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** comando de leitura `orq spec conferir TG-<ref>`, e o daemon mostra o resultado na Central.
- **Fala:** "C"

## D6 — Indicador de estado da spec no card, nesta US
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** o indicador de estado da spec e da conferência no card da US entra na TG-59.
- **Fala:** "A, na #59"

## D7 — `decisoes.md` com uma seção por entrada
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** cada entrada é uma seção com Data, Origem, Decisão e Fala; a aprovação acrescenta o SHA aprovado.
- **Fala:** "A, uma seção por entrada"

## D8 — Um comando registra a spec e liga a MR
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1)
- **Decisão:** `orq spec registrar` também liga a MR da spec à US, na mesma transação.
- **Fala:** "A, um comando só"

## D9 — Comando sem `--repo`
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** `orq spec registrar TG-<ref> --caminho <pasta> --mr <url>`; o repositório vem da URL da MR.
- **Fala:** "Aprovado"

## D10 — Migration aditiva e snapshot sem subir a versão
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** migration nova e aditiva, com leitor mínimo abaixo do número dela; o snapshot ganha `spec` por US sem subir `versao`, e `docs/snapshot.md` é atualizado.
- **Fala:** "Aprovado"

## D11 — `validacao` também pela CLI
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** `orq pend add --tipo validacao` é aceito, com a mesma recusa sem spec; com falha da forja, a CLI devolve erro na hora.
- **Fala:** "Aprovado"

## D12 — Título e opções fixos
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** título "Spec da TG-<ref> pronta para validar" e opções Aprovar e Pedir ajustes, nenhuma recomendada.
- **Fala:** "Aprovado"

## D13 — Estados do indicador
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** registrada, em validação (SHA), aprovada (SHA), mudou além do decisoes.md, mesclada.
- **Fala:** "Aprovado"

## D14 — Onde ficam as instruções
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** a skill `orquestrador` muda em PR separado no BasisTI/skills, referenciando esta US; o `AGENTS.md` do orq muda no PR desta US; o dos outros projetos, na reorganização de cada um.
- **Fala:** "Aprovado"

## D15 — TUI fora do épico
- **Data:** 2026-11-21
- **Origem:** conversa de planejamento (r1), fechamento
- **Decisão:** o TUI não ganha indicador nem spec.
- **Fala:** "Aprovado"

## D16 — Spec aprovada
- **Data:** 2026-11-22
- **Origem:** TG-59#P1, respondida em 2026-11-22
- **Decisão:** spec, plano e decisões aprovados no SHA `8c9d43de5bdf2e1f74cf0913319aff780fcceb88`.
- **Fala:** "Aprovar"

## D17 — Conferência pela pasta da spec, na implementação
- **Data:** 2026-11-22
- **Origem:** TG-59#P2, respondida em 2026-11-22 (achado da revisão independente da spec)
- **Decisão:** a spec entra como aprovada e a implementação corrige a conferência: compara só o conteúdo da pasta registrada nos dois SHAs e exige que mude apenas `<pasta>/decisoes.md`; arquivo fora da pasta é outro sinal ("a MR não é só da spec"). O caso de rebase ou merge da base na branch da spec entra nos testes.
- **Fala:** "Mesclar a spec aprovada e levar o Sério à implementação"
