-- Spec da US (US #59): a pasta e a MR da spec registradas, o SHA que a pendência de validação
-- guarda e o resultado da conferência que o daemon refaz (cache, sem evento no diário).
--
-- Só tabelas novas e uma coluna nula em pendencia. O CHECK de pendencia.tipo não aceita
-- 'validacao' e o SQLite não o altera: a tabela é refeita com o mesmo conteúdo, o mesmo id e os
-- mesmos índices. O orq 4 ainda lê o banco e o orq 5 em diante ainda escrevem (não conhecem a
-- spec nem a coluna; abrir pendência dos tipos antigos segue valendo).
--
-- orq:leitor-minimo 4
-- orq:escritor-minimo 5
CREATE TABLE spec (
  projeto       TEXT NOT NULL,
  ref           INTEGER NOT NULL,
  caminho       TEXT NOT NULL,
  mr_url        TEXT NOT NULL,
  registrada_em TEXT NOT NULL,
  atualizada_em TEXT NOT NULL,
  PRIMARY KEY (projeto, ref),
  FOREIGN KEY (projeto, ref) REFERENCES us(projeto, ref)
);

-- Último resultado da conferência da spec aprovada (valor em JSON). Não é estado da Central.
CREATE TABLE spec_conferencia (
  projeto       TEXT NOT NULL,
  ref           INTEGER NOT NULL,
  valor         TEXT,
  valor_em      TEXT,
  consultado_em TEXT NOT NULL,
  erro_codigo   TEXT,
  erro_mensagem TEXT,
  erro_em       TEXT,
  PRIMARY KEY (projeto, ref)
);

CREATE TABLE pendencia_nova (
  id                  INTEGER PRIMARY KEY,
  projeto             TEXT NOT NULL,
  ref                 INTEGER NOT NULL,
  numero              INTEGER NOT NULL,
  tipo                TEXT NOT NULL CHECK (tipo IN ('duvida', 'autorizacao', 'decisao', 'validacao')),
  texto               TEXT NOT NULL,
  pane_origem         TEXT,
  aberta              INTEGER NOT NULL CHECK (aberta IN (0, 1)),
  resposta            TEXT,
  aberta_em           TEXT NOT NULL,
  resolvida_em        TEXT,
  detalhe             TEXT,
  opcoes              TEXT,
  slug                TEXT,
  arquivo             TEXT,
  arquivo_hash        TEXT,
  arquivo_alterado    TEXT,
  autor               TEXT,
  resposta_opcao      INTEGER,
  respondida_em       TEXT,
  respondida_por      TEXT,
  avisada_em          TEXT,
  abertura_avisada_em TEXT,
  aviso_falhou_em     TEXT,
  resposta_versao     INTEGER NOT NULL DEFAULT 0,
  sha_validacao       TEXT,
  UNIQUE (projeto, ref, numero),
  FOREIGN KEY (projeto, ref) REFERENCES us(projeto, ref)
);
INSERT INTO pendencia_nova (id, projeto, ref, numero, tipo, texto, pane_origem, aberta, resposta, aberta_em, resolvida_em, detalhe, opcoes, slug, arquivo, arquivo_hash,
    arquivo_alterado, autor, resposta_opcao, respondida_em, respondida_por, avisada_em, abertura_avisada_em, aviso_falhou_em, resposta_versao)
  SELECT id, projeto, ref, numero, tipo, texto, pane_origem, aberta, resposta, aberta_em, resolvida_em, detalhe, opcoes, slug, arquivo, arquivo_hash,
    arquivo_alterado, autor, resposta_opcao, respondida_em, respondida_por, avisada_em, abertura_avisada_em, aviso_falhou_em, resposta_versao
  FROM pendencia;
DROP TABLE pendencia;
ALTER TABLE pendencia_nova RENAME TO pendencia;
CREATE INDEX pendencia_aberta ON pendencia(aberta);
CREATE UNIQUE INDEX pendencia_slug ON pendencia(projeto, ref, slug);
