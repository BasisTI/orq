CREATE TABLE coordenador (
  nome           TEXT PRIMARY KEY,
  pane_id        TEXT UNIQUE,
  workspace_id   TEXT,
  projeto_padrao TEXT,
  registrado_em  TEXT NOT NULL
);

CREATE TABLE onda (
  id              INTEGER PRIMARY KEY,
  coordenador     TEXT NOT NULL REFERENCES coordenador(nome),
  titulo          TEXT NOT NULL,
  autorizacao_ate TEXT NOT NULL CHECK (autorizacao_ate IN ('plano', 'mr-pronta', 'merge')),
  ordem           TEXT NOT NULL CHECK (ordem IN ('estrita', 'livre')),
  criada_em       TEXT NOT NULL,
  encerrada_em    TEXT
);

CREATE TABLE us (
  projeto           TEXT NOT NULL,
  ref               INTEGER NOT NULL,
  onda_id           INTEGER NOT NULL REFERENCES onda(id),
  posicao           INTEGER NOT NULL,
  status            TEXT NOT NULL CHECK (status IN ('wait', 'plan', 'exec', 'rev', 'done')),
  pane_id           TEXT,
  papel             TEXT CHECK (papel IN ('planejamento', 'implementacao', 'revisao')),
  rodada            INTEGER,
  proxima_pendencia INTEGER NOT NULL DEFAULT 1,
  atualizado_em     TEXT NOT NULL,
  PRIMARY KEY (projeto, ref)
);
CREATE INDEX us_onda ON us(onda_id, posicao);
CREATE INDEX us_ref ON us(ref);

CREATE TABLE dependencia (
  projeto     TEXT NOT NULL,
  ref         INTEGER NOT NULL,
  dep_projeto TEXT NOT NULL,
  dep_ref     INTEGER NOT NULL,
  PRIMARY KEY (projeto, ref, dep_projeto, dep_ref),
  FOREIGN KEY (projeto, ref) REFERENCES us(projeto, ref),
  FOREIGN KEY (dep_projeto, dep_ref) REFERENCES us(projeto, ref)
);

CREATE TABLE mr (
  id            INTEGER PRIMARY KEY,
  projeto       TEXT NOT NULL,
  ref           INTEGER NOT NULL,
  url           TEXT NOT NULL,
  projeto_path  TEXT NOT NULL,
  iid           INTEGER NOT NULL,
  iac           INTEGER NOT NULL CHECK (iac IN (0, 1)),
  -- Preenchido pelos enriquecedores da US #4 (draft, aberta, pronta, mesclada, fechada).
  estado        TEXT,
  adicionada_em TEXT NOT NULL,
  UNIQUE (projeto, ref, url),
  FOREIGN KEY (projeto, ref) REFERENCES us(projeto, ref)
);

CREATE TABLE pendencia (
  id           INTEGER PRIMARY KEY,
  projeto      TEXT NOT NULL,
  ref          INTEGER NOT NULL,
  numero       INTEGER NOT NULL,
  tipo         TEXT NOT NULL CHECK (tipo IN ('duvida', 'autorizacao', 'decisao')),
  texto        TEXT NOT NULL,
  pane_origem  TEXT,
  aberta       INTEGER NOT NULL CHECK (aberta IN (0, 1)),
  resposta     TEXT,
  aberta_em    TEXT NOT NULL,
  resolvida_em TEXT,
  UNIQUE (projeto, ref, numero),
  FOREIGN KEY (projeto, ref) REFERENCES us(projeto, ref)
);
CREATE INDEX pendencia_aberta ON pendencia(aberta);

CREATE TABLE evento (
  id          INTEGER PRIMARY KEY,
  ts          TEXT NOT NULL,
  coordenador TEXT,
  onda_id     INTEGER,
  projeto     TEXT,
  ref         INTEGER,
  tipo        TEXT NOT NULL,
  detalhe     TEXT NOT NULL
);
CREATE INDEX evento_us ON evento(projeto, ref);
CREATE INDEX evento_onda ON evento(onda_id);
