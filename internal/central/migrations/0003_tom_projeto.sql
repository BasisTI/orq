-- Tom do título da onda na Central (US #10): o índice de cada projeto na paleta do TUI, dado na
-- ordem em que o projeto chega e mantido para sempre. É estado da Central: atribuir gera evento.
CREATE TABLE projeto_tom (
  projeto TEXT PRIMARY KEY,
  indice  INTEGER NOT NULL UNIQUE CHECK (indice >= 0)
);

-- Projetos que já existem, na ordem da primeira aparição: o coordenador que o tem por padrão
-- (registrado_em) ou a onda de uma US dele (criada_em), do mais antigo ao mais novo, e o nome
-- desempata.
INSERT INTO projeto_tom (projeto, indice)
SELECT projeto, ROW_NUMBER() OVER (ORDER BY ts, projeto) - 1
FROM (
  SELECT projeto, MIN(ts) AS ts FROM (
    SELECT projeto_padrao AS projeto, registrado_em AS ts FROM coordenador WHERE projeto_padrao IS NOT NULL
    UNION ALL
    SELECT us.projeto, onda.criada_em FROM us JOIN onda ON onda.id = us.onda_id
  ) GROUP BY projeto
);
