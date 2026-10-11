package central

import (
	"context"
	"database/sql"

	"github.com/BasisTI/orq/internal/refs"
)

// MR é uma merge request (ou pull request) ligada a uma US. Estado fica nulo até os
// enriquecedores da US #4 consultarem a forja.
type MR struct {
	URL          string  `json:"url"`
	ProjetoPath  string  `json:"projeto_path"`
	IID          int     `json:"iid"`
	IaC          bool    `json:"iac"`
	Estado       *string `json:"estado"`
	AdicionadaEm string  `json:"adicionada_em"`
}

// AdicionarMR liga a MR à US. A mesma URL de novo não muda nada (Criada = false, sem evento).
func (s *Store) AdicionarMR(ctx context.Context, coord *Coordenador, ref refs.US, rawURL string) (MR, bool, error) {
	m, err := refs.ParseMR(rawURL)
	if err != nil {
		return MR{}, false, uso(err.Error(), "")
	}
	criada := false
	var alvo usAlvo
	err = s.escrever(ctx, func(tx *sql.Tx) error {
		var err error
		if alvo, err = resolverUS(ctx, tx, ref, coord); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO mr (projeto, ref, url, projeto_path, iid, iac, adicionada_em) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			alvo.Projeto, alvo.Ref, m.URL, m.ProjetoPath, m.IID, m.IaC, s.agora())
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		criada = true
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "mr.add",
			detalhe: map[string]any{"url": m.URL, "projeto_path": m.ProjetoPath, "iid": m.IID, "iac": m.IaC}})
	})
	if err != nil {
		return MR{}, false, err
	}
	var out MR
	err = s.ler(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT url, projeto_path, iid, iac, estado, adicionada_em FROM mr WHERE projeto = ? AND ref = ? AND url = ?`, alvo.Projeto, alvo.Ref, m.URL).
			Scan(&out.URL, &out.ProjetoPath, &out.IID, &out.IaC, &out.Estado, &out.AdicionadaEm)
	})
	return out, criada, err
}

// RemoverMR desliga a MR da US; a URL passa pela mesma normalização do add.
func (s *Store) RemoverMR(ctx context.Context, coord *Coordenador, ref refs.US, rawURL string) (MR, error) {
	m, err := refs.ParseMR(rawURL)
	if err != nil {
		return MR{}, uso(err.Error(), "")
	}
	var out MR
	err = s.escrever(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, ref, coord)
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT url, projeto_path, iid, iac, estado, adicionada_em FROM mr WHERE projeto = ? AND ref = ? AND url = ?`, alvo.Projeto, alvo.Ref, m.URL).
			Scan(&out.URL, &out.ProjetoPath, &out.IID, &out.IaC, &out.Estado, &out.AdicionadaEm)
		if semLinha(err) {
			return naoEncontrado(m.URL+" não está ligada a "+alvo.Chave(), "veja as MRs com orq onda ver")
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM mr WHERE projeto = ? AND ref = ? AND url = ?`, alvo.Projeto, alvo.Ref, m.URL); err != nil {
			return err
		}
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "mr.rm", detalhe: map[string]any{"url": m.URL}})
	})
	return out, err
}

func carregarMRs(ctx context.Context, q querier, projeto string, ref int) ([]MR, error) {
	rows, err := q.QueryContext(ctx, `SELECT url, projeto_path, iid, iac, estado, adicionada_em FROM mr WHERE projeto = ? AND ref = ? ORDER BY id`, projeto, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ms := []MR{}
	for rows.Next() {
		var m MR
		if err := rows.Scan(&m.URL, &m.ProjetoPath, &m.IID, &m.IaC, &m.Estado, &m.AdicionadaEm); err != nil {
			return nil, err
		}
		ms = append(ms, m)
	}
	return ms, rows.Err()
}
