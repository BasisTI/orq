package central

import (
	"context"
	"database/sql"
)

// atribuirTom dá ao projeto o próximo índice de tom livre, se ele ainda não tem um, e grava o
// evento projeto.tom na mesma transação. A transação é BEGIN IMMEDIATE, então dois processos
// não leem o mesmo máximo; o UNIQUE de indice é a garantia de que, mesmo assim, nunca há dois
// projetos com o mesmo índice nem dois índices para o mesmo projeto (PRIMARY KEY).
func (s *Store) atribuirTom(ctx context.Context, tx *sql.Tx, projeto, coord string) error {
	if projeto == "" {
		return nil
	}
	var indice int
	err := tx.QueryRowContext(ctx, `INSERT INTO projeto_tom (projeto, indice)
		SELECT ?, COALESCE(MAX(indice) + 1, 0) FROM projeto_tom WHERE true
		ON CONFLICT (projeto) DO NOTHING RETURNING indice`, projeto).Scan(&indice)
	if semLinha(err) {
		return nil // já tinha
	}
	if err != nil {
		return err
	}
	return s.registrar(ctx, tx, evento{coordenador: coord, projeto: projeto, tipo: "projeto.tom", detalhe: map[string]any{"indice": indice}})
}

// ProjetoTom é o índice de tom de um projeto, na ordem em que ele chegou à Central.
type ProjetoTom struct {
	Projeto string `json:"projeto"`
	Indice  int    `json:"indice"`
}

func carregarTons(ctx context.Context, q querier) ([]ProjetoTom, error) {
	rows, err := q.QueryContext(ctx, `SELECT projeto, indice FROM projeto_tom ORDER BY indice`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ts := []ProjetoTom{}
	for rows.Next() {
		var t ProjetoTom
		if err := rows.Scan(&t.Projeto, &t.Indice); err != nil {
			return nil, err
		}
		ts = append(ts, t)
	}
	return ts, rows.Err()
}
