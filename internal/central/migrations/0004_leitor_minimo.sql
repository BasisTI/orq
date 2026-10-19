-- Versão mínima de leitor (US #11): o banco passa a guardar a menor versão de orq (a última
-- migration que o binário embute) que ainda o lê e a que ainda escreve nele. Cada migration
-- declara os dois com as diretivas abaixo; sem diretiva, vale o número da própria migration.
-- Esta é a primeira que declara: um orq sem esta migration nem sabe ler o valor.
--
-- orq:leitor-minimo 4
-- orq:escritor-minimo 4
CREATE TABLE banco_meta (
  chave TEXT PRIMARY KEY,
  valor TEXT NOT NULL
);
