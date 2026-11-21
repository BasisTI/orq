-- Nome explícito da onda (US #88): o nome que o orq dá ao rótulo e ao agente do pane do
-- coordenador. Só coluna nova, nula: o orq 4 ainda lê o banco e o orq 5 em diante ainda
-- escrevem (criam ondas sem nome, que o orq lê como nome ausente e deduz do título quando o
-- início dele é um nome válido).
--
-- orq:leitor-minimo 4
-- orq:escritor-minimo 5
ALTER TABLE onda ADD COLUMN nome TEXT;
