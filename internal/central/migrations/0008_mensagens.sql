-- Mensagens enviadas pela web ao coordenador da onda e respostas dele (US #64).
-- Tabela nova, aditiva: o orq 4 ainda lê o banco e o orq 5..7 ainda escrevem sem tocá-la.
--
-- orq:leitor-minimo 4
-- orq:escritor-minimo 5
CREATE TABLE mensagem (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    onda_id INTEGER NOT NULL REFERENCES onda(id),
    texto TEXT NOT NULL,
    enviada_por TEXT NOT NULL,
    enviada_em TEXT NOT NULL,
    avisada_em TEXT,
    aviso_falhou_em TEXT,
    resposta TEXT,
    respondida_por TEXT,
    respondida_em TEXT
);

CREATE INDEX idx_mensagem_onda_id ON mensagem (onda_id, id DESC);
