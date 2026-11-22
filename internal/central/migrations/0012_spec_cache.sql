-- Cache da leitura da spec pela forja (US #60): o conteúdo dos arquivos da spec por (spec, SHA) e o
-- estado da última leitura da ref atual. É cache do daemon, não estado da Central: sem evento no
-- diário.
--
-- Só tabelas novas, que os binários anteriores não conhecem nem tocam: o orq 4 ainda lê o banco e o
-- orq 5 em diante ainda escrevem.
--
-- orq:leitor-minimo 4
-- orq:escritor-minimo 5
CREATE TABLE spec_versao (
  projeto TEXT NOT NULL,
  ref     INTEGER NOT NULL,
  sha     TEXT NOT NULL,
  lida_em TEXT NOT NULL,
  PRIMARY KEY (projeto, ref, sha)
);

CREATE TABLE spec_arquivo (
  projeto       TEXT NOT NULL,
  ref           INTEGER NOT NULL,
  sha           TEXT NOT NULL,
  caminho       TEXT NOT NULL,
  papel         TEXT NOT NULL CHECK (papel IN ('pasta', 'epico')),
  ordem         INTEGER NOT NULL,
  conteudo      TEXT NOT NULL,
  tamanho       INTEGER NOT NULL,
  grande_demais INTEGER NOT NULL CHECK (grande_demais IN (0, 1)),
  PRIMARY KEY (projeto, ref, sha, caminho)
);

-- A leitura da ref atual: qual SHA é a versão atual, quando a forja foi consultada pela última
-- vez e a última falha (com ela, o último conteúdo bom continua valendo).
CREATE TABLE spec_leitura (
  projeto       TEXT NOT NULL,
  ref           INTEGER NOT NULL,
  sha           TEXT,
  consultado_em TEXT NOT NULL,
  erro_codigo   TEXT,
  erro_mensagem TEXT,
  erro_em       TEXT,
  PRIMARY KEY (projeto, ref)
);
