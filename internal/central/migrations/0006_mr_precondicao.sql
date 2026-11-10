-- Pré-condição da MR em Draft (US #68): o texto de "Mesclar depois de …". Só coluna nova, nula:
-- o orq 4 ainda lê o banco e o orq 5 ainda escreve (ligar e desligar MR não toca na coluna).
--
-- orq:leitor-minimo 4
-- orq:escritor-minimo 5
ALTER TABLE mr ADD COLUMN precondicao TEXT;
