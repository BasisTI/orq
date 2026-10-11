package central

import (
	"context"
	"database/sql"

	"github.com/BasisTI/orq/internal/refs"
)

// MR é uma merge request (ou pull request) ligada a uma US. Estado fica nulo até o enriquecedor
// consultar a forja e, depois, guarda o último estado bom.
type MR struct {
	URL          string  `json:"url"`
	ProjetoPath  string  `json:"projeto_path"`
	IID          int     `json:"iid"`
	IaC          bool    `json:"iac"`
	Estado       *string `json:"estado"`
	AdicionadaEm string  `json:"adicionada_em"`
	AtualizadoEm *string `json:"atualizado_em"` // última consulta bem-sucedida à forja
	Erro         *Erro   `json:"erro"`          // última consulta, se falhou
}

// colunasMR lê a MR com o cache dela; scanMR casa com a ordem.
const colunasMR = `SELECT mr.url, mr.projeto_path, mr.iid, mr.iac, mr.estado, mr.adicionada_em, c.valor_em, c.erro_codigo, c.erro_mensagem, c.erro_em
	FROM mr LEFT JOIN cache c ON c.fonte = 'mr' AND c.chave = mr.url `

func scanMR(r scanner) (MR, error) {
	var m MR
	var codigo, mensagem, em *string
	err := r.Scan(&m.URL, &m.ProjetoPath, &m.IID, &m.IaC, &m.Estado, &m.AdicionadaEm, &m.AtualizadoEm, &codigo, &mensagem, &em)
	m.Erro = erroDe(codigo, mensagem, em)
	return m, err
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
		var err error
		out, err = scanMR(tx.QueryRowContext(ctx, colunasMR+`WHERE mr.projeto = ? AND mr.ref = ? AND mr.url = ?`, alvo.Projeto, alvo.Ref, m.URL))
		return err
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
		out, err = scanMR(tx.QueryRowContext(ctx, colunasMR+`WHERE mr.projeto = ? AND mr.ref = ? AND mr.url = ?`, alvo.Projeto, alvo.Ref, m.URL))
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
	rows, err := q.QueryContext(ctx, colunasMR+`WHERE mr.projeto = ? AND mr.ref = ? ORDER BY mr.id`, projeto, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ms := []MR{}
	for rows.Next() {
		m, err := scanMR(rows)
		if err != nil {
			return nil, err
		}
		ms = append(ms, m)
	}
	return ms, rows.Err()
}
