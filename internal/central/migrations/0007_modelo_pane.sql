-- Modelo e esforço do pane que conduz a US (US #81). Só colunas novas, nulas: o orq 4 ainda lê o
-- banco e o orq 5 e o 6 ainda escrevem (não conhecem as colunas, então não as tocam; o modelo
-- gravado fica no valor anterior até um orq 7 mudar o status com --modelo).
--
-- orq:leitor-minimo 4
-- orq:escritor-minimo 5
ALTER TABLE us ADD COLUMN modelo TEXT;
ALTER TABLE us ADD COLUMN esforco TEXT;
