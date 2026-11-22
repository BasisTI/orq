-- Link público da spec (US #61): os links por spec (só o hash do código fica no banco), o número
-- de versão do registro da spec e o registro (pasta e MR) a que cada versão do cache pertence.
--
-- Tudo aditivo: tabela nova e colunas com valor padrão. O orq 4 ainda lê o banco (seleciona as
-- colunas que conhece). O escritor mínimo é esta migration (13): um orq anterior faria
-- `orq spec registrar --substituir` sem incrementar `spec.versao`, e o cache da spec que ele grava
-- não leva `caminho` nem `mr_url`.
--
-- orq:leitor-minimo 4
-- orq:escritor-minimo 13

-- Versão do registro: 1 no registro inicial, +1 a cada --substituir. Os registros existentes
-- recebem 1 mais as substituições que o diário já tem.
ALTER TABLE spec ADD COLUMN versao INTEGER NOT NULL DEFAULT 1;
UPDATE spec SET versao = 1 + (
  SELECT COUNT(*) FROM evento e
  WHERE e.tipo = 'spec.registrar' AND e.projeto = spec.projeto AND e.ref = spec.ref
    AND json_extract(e.detalhe, '$.substituiu') = 1
);

-- Cada versão do cache sabe de que registro (pasta e MR) veio: o daemon pode estar lendo a spec
-- velha quando o --substituir acontece, e um binário que não conhece estas colunas não limpa o cache.
ALTER TABLE spec_versao ADD COLUMN caminho TEXT;
ALTER TABLE spec_versao ADD COLUMN mr_url TEXT;
UPDATE spec_versao SET
  caminho = (SELECT s.caminho FROM spec s WHERE s.projeto = spec_versao.projeto AND s.ref = spec_versao.ref),
  mr_url  = (SELECT s.mr_url  FROM spec s WHERE s.projeto = spec_versao.projeto AND s.ref = spec_versao.ref);

-- Links públicos. O código nunca é guardado: só o SHA-256 dele (hex), pelo qual o socket público
-- consulta. Revogar grava revogado_em e é definitivo. ultimo_acesso e acessos não são estado da
-- Central (sem evento no diário).
CREATE TABLE spec_link (
  id            INTEGER PRIMARY KEY,
  projeto       TEXT NOT NULL,
  ref           INTEGER NOT NULL,
  hash          TEXT NOT NULL UNIQUE,
  rotulo        TEXT NOT NULL,
  criado_em     TEXT NOT NULL,
  expira_em     TEXT,
  revogado_em   TEXT,
  ultimo_acesso TEXT,
  acessos       INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY (projeto, ref) REFERENCES spec(projeto, ref)
);
CREATE INDEX spec_link_spec ON spec_link(projeto, ref);
