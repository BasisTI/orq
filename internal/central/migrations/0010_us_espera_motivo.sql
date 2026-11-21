-- Motivo da espera da US (US #100): texto curto que o coordenador registra quando a US está
-- parada por algo externo. Só coluna nova, nula: o orq 4 ainda lê o banco e o orq 5 em diante
-- ainda escrevem (não conhecem a coluna, então não a tocam; o motivo gravado fica como estava).
--
-- orq:leitor-minimo 4
-- orq:escritor-minimo 5
ALTER TABLE us ADD COLUMN espera_motivo TEXT;
