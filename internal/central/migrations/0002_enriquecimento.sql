-- Cache dos enriquecedores (US #4): o último valor bom de cada consulta e o último erro.
-- Não é estado da Central: gravar aqui não gera evento no diário.
CREATE TABLE cache (
  fonte         TEXT NOT NULL CHECK (fonte IN ('herdr', 'mr', 'taiga')),
  -- '' no herdr (a sessão inteira), a URL normalizada na mr, projeto/ref no taiga.
  chave         TEXT NOT NULL,
  valor         TEXT,
  valor_em      TEXT,
  consultado_em TEXT NOT NULL,
  erro_codigo   TEXT,
  erro_mensagem TEXT,
  erro_em       TEXT,
  PRIMARY KEY (fonte, chave)
);

-- Ferramenta suspensa por erro de autenticação: o orq não tenta login, só para de chamar.
CREATE TABLE suspensao (
  ferramenta TEXT PRIMARY KEY,
  codigo     TEXT NOT NULL,
  mensagem   TEXT NOT NULL,
  desde      TEXT NOT NULL
);

-- Tokens de sidebar que o orq publicou no Herdr, para limpar o que deixou de valer.
CREATE TABLE token_publicado (
  alvo         TEXT NOT NULL CHECK (alvo IN ('pane', 'workspace')),
  alvo_id      TEXT NOT NULL,
  nome         TEXT NOT NULL,
  valor        TEXT NOT NULL,
  publicado_em TEXT NOT NULL,
  PRIMARY KEY (alvo, alvo_id, nome)
);
