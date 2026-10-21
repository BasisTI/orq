-- Pendência com contexto completo e resposta pela web (US #15). Só colunas novas, com NULL
-- ou default: as consultas do orq 4 continuam válidas, mas ele não escreve mais no banco
-- (o escritor mínimo sobe para 5), porque não conhece a resposta nem os avisos.
--
-- orq:leitor-minimo 4
-- orq:escritor-minimo 5
ALTER TABLE pendencia ADD COLUMN detalhe TEXT;
ALTER TABLE pendencia ADD COLUMN opcoes TEXT;
ALTER TABLE pendencia ADD COLUMN slug TEXT;
ALTER TABLE pendencia ADD COLUMN arquivo TEXT;
ALTER TABLE pendencia ADD COLUMN arquivo_hash TEXT;
ALTER TABLE pendencia ADD COLUMN arquivo_alterado TEXT;
ALTER TABLE pendencia ADD COLUMN autor TEXT;
ALTER TABLE pendencia ADD COLUMN resposta_opcao INTEGER;
ALTER TABLE pendencia ADD COLUMN respondida_em TEXT;
ALTER TABLE pendencia ADD COLUMN respondida_por TEXT;
ALTER TABLE pendencia ADD COLUMN avisada_em TEXT;
ALTER TABLE pendencia ADD COLUMN abertura_avisada_em TEXT;
ALTER TABLE pendencia ADD COLUMN aviso_falhou_em TEXT;
-- Sobe a cada troca de resposta. Os avisos confirmam esta versão, e não um horário: duas trocas no
-- mesmo segundo teriam o mesmo respondida_em.
ALTER TABLE pendencia ADD COLUMN resposta_versao INTEGER NOT NULL DEFAULT 0;
-- UNIQUE(projeto, ref, slug): o SQLite não aceita UNIQUE em ADD COLUMN, e o índice trata
-- NULL como distinto, então as pendências antigas (sem slug) não colidem.
CREATE UNIQUE INDEX pendencia_slug ON pendencia(projeto, ref, slug);
