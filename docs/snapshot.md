# Contrato do `orq snapshot` (versão 1)

`orq snapshot` imprime, sempre em JSON, o estado consolidado da Central: coordenadores → ondas → US (com MRs e pendências), mais a lista das pendências abertas e o estado vivo que os enriquecedores trazem do Herdr, das forjas e do Taiga. É o que o TUI e o daemon consomem; o resto da CLI pode mudar de forma, este documento não sem subir a versão.

- Por padrão, só as ondas abertas. `--incluir-encerradas` traz todas.
- `orq snapshot --json`, a forma do desenho, também vale: a flag é aceita e não muda nada. `--output text` é ignorado.
- A leitura é uma transação só: o snapshot é um instante coerente do banco.

## Estabilidade

- `versao` sobe quando um campo é removido, muda de tipo ou de sentido. Campo novo não sobe a versão: o consumidor ignora o que não conhece.
- Toda chave listada aqui sempre aparece. Valor ausente é `null`; lista vazia é `[]`, nunca `null`.
- Datas em RFC 3339, UTC, com segundos (`2026-10-11T14:03:40Z`).
- Ordem: coordenadores por nome; ondas por id; US pela posição na onda; MRs pela ordem em que foram ligadas; pendências pela abertura.

### Histórico

- Versão 1, US #3: o esqueleto.
- Ainda versão 1, US #4: os campos `herdr` (coordenador e US), `taiga` (US), `atualizado_em` e `erro` (MR) e `fontes` (raiz) entraram, e `MR.estado` passou a ser preenchido. Tudo aditivo: `estado` já estava declarado como `draft|aberta|pronta|mesclada|fechada`, e quem lia a versão 1 continua lendo.
- Ainda versão 1, US #10: o campo `projetos` (raiz) entrou, com o índice de tom de cada projeto. Aditivo.
- Ainda versão 1, US #15: a `Pendencia` ganhou `chave`, `estado`, `tem_detalhe`, `opcoes`, `slug`, `arquivo`, `autor`, `resposta_opcao`, `respondida_em`, `respondida_por`, `avisada_em`, `arquivo_alterado` e `sem_coordenador`. Aditivo: o sentido dos campos antigos não mudou, e `aberta` continua `true` na pendência respondida (ela só fecha em `orq pend resolver`). O `detalhe` (até 64 KiB por pendência) não vai no snapshot: `orq pend ver` o traz.

## Estado vivo (US #4)

O snapshot não consulta nada: ele lê o cache que `orq daemon` (em laço) e `orq atualizar` (uma vez) gravam. Cada fonte guarda o último valor bom e o erro da última consulta, então um valor pode estar velho e com `erro` ao lado; quem desenha decide pelo `atualizado_em`.

| Fonte | O que lê | Cadência no daemon |
|---|---|---|
| `herdr` | `herdr api snapshot`: status do agente, nome e workspace de cada pane, tokens | a cada tick (5 s) e a cada `pane.agent_status_changed` |
| `mr` | `glab api` (GitLab) ou `gh api` (GitHub), só leitura | TTL de 60 s por MR |
| `taiga` | `taiga story get --output json`, só leitura | TTL de 5 min por US |

Estado da MR: `mesclada`, `fechada`, `draft`, `pronta` (aberta, não draft e mesclável: `detailed_merge_status` `mergeable` no GitLab, `mergeable_state` `clean` ou `has_hooks` no GitHub) ou `aberta` (o resto: conflito, pipeline, revisão obrigatória, ainda não calculado). Mudança de estado gera `mr.estado` no diário.

Erro de autenticação de `glab`, `gh` ou `taiga` suspende a ferramenta por 30 min: o orq não tenta login nem define token. Na taiga-cli, é o exit 3 (`auth_*`, `session_*`, `secret_*`, `keyring_*`, `*_secret_*`, `password_file_*`). `orq atualizar --forcar` passa por cima de uma suspensão anterior, mas uma recusa na própria execução interrompe as chamadas seguintes da ferramenta. A suspensão aparece em `fontes.<fonte>.erro`.

O snapshot não traz os tokens de sidebar. O daemon publica, com nomes próprios, `orq` no pane do coordenador e `orq_mrs` no workspace, e nunca toca token de outro produtor (README, "Daemon e enriquecedores").

## Esquema

```text
{
  "versao": 1,
  "gerado_em": data,
  "coordenadores": [Coordenador],
  "projetos": [ProjetoTom],                # índice de tom de cada projeto, na ordem de chegada
  "pendencias_abertas": [Pendencia],       # todas as abertas, de todos os coordenadores
  "fontes": {"herdr": Fonte, "mr": Fonte, "taiga": Fonte}
}

ProjetoTom {                               # o TUI pinta o título da onda com paleta[indice % len(paleta)]
  "projeto": string,
  "indice": int                            # 0, 1, 2…: dado na 1ª vez que o projeto aparece, nunca muda nem se repete
}

Fonte {
  "consultado_em": data|null,              # última consulta, boa ou não
  "atualizado_em": data|null,              # último valor bom
  "erro": Erro|null                        # o que vale para a fonte inteira: falha do Herdr ou ferramenta suspensa
}

Erro {
  "codigo": "binario_ausente"|"autenticacao"|"nao_encontrado"|"tempo_esgotado"|"falha_comando"|"resposta_invalida",
  "mensagem": string,                      # da ferramenta, cortada
  "em": data                               # na fonte, o início da suspensão
}

Herdr {                                    # estado vivo de um pane
  "status": "idle"|"working"|"blocked"|"done"|"unknown"|"ausente"|null,
                                           # unknown: pane sem agente; ausente: o Herdr respondeu e o pane não existe;
                                           # null: o Herdr nunca respondeu
  "agente": string|null,                   # "claude", "codex"
  "nome": string|null,                     # nome do agente no Herdr
  "workspace_id": string|null,
  "atualizado_em": data|null,
  "erro": Erro|null                        # a última consulta ao Herdr falhou: o resto é o último valor bom
}

Coordenador {
  "nome": string,
  "pane_id": string|null,                  # pane do Herdr do coordenador
  "workspace_id": string|null,
  "projeto_padrao": string|null,           # slug do Taiga usado em TG-<n> sem projeto
  "registrado_em": data,                   # último registro
  "herdr": Herdr|null,                     # nulo sem pane_id
  "ondas": [Onda]
}

Onda {
  "id": int,
  "coordenador": string,
  "titulo": string,
  "autorizacao_ate": "plano"|"mr-pronta"|"merge",
  "ordem": "estrita"|"livre",
  "criada_em": data,
  "encerrada_em": data|null,
  "us": [US]
}

US {
  "id": string,                            # exibição curta, "TG-215"
  "chave": string,                         # chave única, "colaboradados/215"
  "projeto": string,                       # slug do Taiga
  "ref": int,
  "onda_id": int,
  "posicao": int,                          # 1, 2… dentro da onda
  "depende_de": [string],                  # chaves das US de que esta depende
  "status": "wait"|"plan"|"exec"|"rev"|"done",
  "pane_id": string|null,                  # pane que conduz a US agora
  "papel": "planejamento"|"implementacao"|"revisao"|null,
  "rodada": int|null,
  "atualizado_em": data,
  "mrs": [MR],
  "pendencias": [Pendencia],               # abertas e resolvidas
  "herdr": Herdr|null,                     # nulo sem pane_id
  "taiga": {
    "titulo": string|null,
    "status": string|null,                 # nome do status no Taiga, "In progress"
    "url": string|null,
    "atualizado_em": data|null,
    "erro": Erro|null
  }
}

MR {
  "url": string,                           # normalizada, sem query, fragmento ou subpágina
  "projeto_path": string,                  # "basis/iac/plataforma" ou "BasisTI/orq"
  "iid": int,
  "iac": bool,                             # projeto sob basis/iac/
  "estado": "draft"|"aberta"|"pronta"|"mesclada"|"fechada"|null,   # nulo até a primeira consulta boa
  "adicionada_em": data,
  "atualizado_em": data|null,              # última consulta boa à forja
  "erro": Erro|null                        # última consulta, se falhou
}

Pendencia {
  "id": string,                            # "TG-215#P2"
  "us": string,                            # chave da US
  "numero": int,                           # sequencial por US, nunca reaproveitado
  "tipo": "duvida"|"autorizacao"|"decisao",
  "texto": string,
  "pane_origem": string|null,              # pane que abriu
  "aberta": bool,
  "resposta": string|null,                 # texto livre da pessoa; vira a resposta final ao resolver
  "aberta_em": data,
  "resolvida_em": data|null,
  "onda_id": int,                          # onda atual da US
  "coordenador": string,                   # dono dessa onda
  "chave": string,                         # "projeto/215#P2": o que orq pend ver aceita sem ambiguidade
  "estado": "aberta"|"respondida"|"resolvida",   # respondida: aberta = true e respondida_em preenchido
  "tem_detalhe": bool,                     # false nas pendências antigas, que só têm o título
  "opcoes": [Opcao],                       # [] sem opções
  "slug": string|null,                     # origem da importação (o arquivo <slug>.md)
  "arquivo": string|null,                  # caminho do arquivo importado
  "autor": string|null,                    # agente que abriu
  "resposta_opcao": int|null,              # id da opção que a pessoa escolheu
  "respondida_em": data|null,
  "respondida_por": string|null,           # usuário da web, "local" (web em TCP) ou "chat"
  "avisada_em": data|null,                 # quando o coordenador recebeu o aviso da resposta
  "arquivo_alterado": bool,                # o arquivo mudou depois do registro; a pendência não mudou
  "sem_coordenador": bool                  # respondida, e o aviso falhou porque o coordenador sumiu
}

Opcao {"id": int, "titulo": string, "descricao": string, "recomendada": bool}
```

`texto`, `titulo` e `resposta` vêm de agentes e podem trazer qualquer caractere; quem desenha no terminal deve escapar controles (a CLI faz isso no modo texto).

## Exemplo

```json
{
  "versao": 1,
  "gerado_em": "2026-10-11T14:10:00Z",
  "coordenadores": [
    {
      "nome": "colaboradados",
      "pane_id": "wB:p1",
      "workspace_id": null,
      "projeto_padrao": "colaboradados",
      "registrado_em": "2026-10-11T14:00:00Z",
      "herdr": {
        "status": "working",
        "agente": "claude",
        "nome": null,
        "workspace_id": "wB",
        "atualizado_em": "2026-10-11T14:09:58Z",
        "erro": null
      },
      "ondas": [
        {
          "id": 1,
          "coordenador": "colaboradados",
          "titulo": "Melhorias da noite",
          "autorizacao_ate": "mr-pronta",
          "ordem": "estrita",
          "criada_em": "2026-10-11T14:00:01Z",
          "encerrada_em": null,
          "us": [
            {
              "id": "TG-215",
              "chave": "colaboradados/215",
              "projeto": "colaboradados",
              "ref": 215,
              "onda_id": 1,
              "posicao": 1,
              "depende_de": [],
              "status": "exec",
              "pane_id": "wB:p3",
              "papel": "implementacao",
              "rodada": 1,
              "atualizado_em": "2026-10-11T14:02:00Z",
              "mrs": [
                {
                  "url": "https://gitlab.basis.com.br/basis/colaboradados/api/-/merge_requests/41",
                  "projeto_path": "basis/colaboradados/api",
                  "iid": 41,
                  "iac": false,
                  "estado": "pronta",
                  "adicionada_em": "2026-10-11T14:05:00Z",
                  "atualizado_em": "2026-10-11T14:09:30Z",
                  "erro": null
                }
              ],
              "pendencias": [
                {
                  "id": "TG-215#P1",
                  "us": "colaboradados/215",
                  "numero": 1,
                  "tipo": "autorizacao",
                  "texto": "aplicar a migration no banco de homologação",
                  "pane_origem": "wB:p3",
                  "aberta": true,
                  "resposta": null,
                  "aberta_em": "2026-10-11T14:04:00Z",
                  "resolvida_em": null,
                  "onda_id": 1,
                  "coordenador": "colaboradados"
                }
              ],
              "herdr": {
                "status": "blocked",
                "agente": "claude",
                "nome": "tg215-exec",
                "workspace_id": "wB",
                "atualizado_em": "2026-10-11T14:09:58Z",
                "erro": null
              },
              "taiga": {
                "titulo": "Exportar a folha em CSV",
                "status": "In progress",
                "url": "https://agile.basis.com.br/project/colaboradados/us/215",
                "atualizado_em": "2026-10-11T14:06:00Z",
                "erro": null
              }
            }
          ]
        }
      ]
    }
  ],
  "projetos": [
    {"projeto": "colaboradados", "indice": 0}
  ],
  "pendencias_abertas": [
    {
      "id": "TG-215#P1",
      "us": "colaboradados/215",
      "numero": 1,
      "tipo": "autorizacao",
      "texto": "aplicar a migration no banco de homologação",
      "pane_origem": "wB:p3",
      "aberta": true,
      "resposta": null,
      "aberta_em": "2026-10-11T14:04:00Z",
      "resolvida_em": null,
      "onda_id": 1,
      "coordenador": "colaboradados"
    }
  ],
  "fontes": {
    "herdr": {"consultado_em": "2026-10-11T14:09:58Z", "atualizado_em": "2026-10-11T14:09:58Z", "erro": null},
    "mr": {"consultado_em": "2026-10-11T14:09:30Z", "atualizado_em": "2026-10-11T14:09:30Z", "erro": null},
    "taiga": {
      "consultado_em": "2026-10-11T14:06:00Z",
      "atualizado_em": "2026-10-11T14:06:00Z",
      "erro": {"codigo": "autenticacao", "mensagem": "taiga suspenso: taiga: session_expired", "em": "2026-10-11T14:08:00Z"}
    }
  }
}
```
