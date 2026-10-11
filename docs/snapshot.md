# Contrato do `orq snapshot` (versão 1)

`orq snapshot` imprime, sempre em JSON, o estado consolidado da Central: coordenadores → ondas → US (com MRs e pendências), mais a lista das pendências abertas. É o que o TUI e o daemon consomem; o resto da CLI pode mudar de forma, este documento não sem subir a versão.

- Por padrão, só as ondas abertas. `--incluir-encerradas` traz todas.
- `orq snapshot --json`, a forma do desenho, também vale: a flag é aceita e não muda nada. `--output text` é ignorado.
- A leitura é uma transação só: o snapshot é um instante coerente do banco.

## Estabilidade

- `versao` sobe quando um campo é removido, muda de tipo ou de sentido. Campo novo não sobe a versão: o consumidor ignora o que não conhece.
- Toda chave listada aqui sempre aparece. Valor ausente é `null`; lista vazia é `[]`, nunca `null`.
- Datas em RFC 3339, UTC, com segundos (`2026-10-11T14:03:40Z`).
- Ordem: coordenadores por nome; ondas por id; US pela posição na onda; MRs pela ordem em que foram ligadas; pendências pela abertura.

## Esquema

```text
{
  "versao": 1,
  "gerado_em": data,
  "coordenadores": [Coordenador],
  "pendencias_abertas": [Pendencia]        # todas as abertas, de todos os coordenadores
}

Coordenador {
  "nome": string,
  "pane_id": string|null,                  # pane do Herdr do coordenador
  "workspace_id": string|null,
  "projeto_padrao": string|null,           # slug do Taiga usado em TG-<n> sem projeto
  "registrado_em": data,                   # último registro
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
  "pendencias": [Pendencia]                # abertas e resolvidas
}

MR {
  "url": string,                           # normalizada, sem query, fragmento ou subpágina
  "projeto_path": string,                  # "basis/iac/plataforma" ou "BasisTI/orq"
  "iid": int,
  "iac": bool,                             # projeto sob basis/iac/
  "estado": string|null,                   # nulo até os enriquecedores (US #4); depois draft|aberta|pronta|mesclada|fechada
  "adicionada_em": data
}

Pendencia {
  "id": string,                            # "TG-215#P2"
  "us": string,                            # chave da US
  "numero": int,                           # sequencial por US, nunca reaproveitado
  "tipo": "duvida"|"autorizacao"|"decisao",
  "texto": string,
  "pane_origem": string|null,              # pane que abriu
  "aberta": bool,
  "resposta": string|null,
  "aberta_em": data,
  "resolvida_em": data|null,
  "onda_id": int,                          # onda atual da US
  "coordenador": string                    # dono dessa onda
}
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
                  "estado": null,
                  "adicionada_em": "2026-10-11T14:05:00Z"
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
              ]
            }
          ]
        }
      ]
    }
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
  ]
}
```
