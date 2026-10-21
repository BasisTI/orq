package central

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

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
	// Pipeline e Aprovacao vêm do último valor bom do cache; nulos até a forja os dar.
	Pipeline  *string `json:"pipeline"`
	Aprovacao *string `json:"aprovacao"`
	// SituacaoEm é quando os dois foram lidos da forja; SituacaoErro, a falha da consulta extra
	// que não os renovou. Sem falha, valem AtualizadoEm.
	SituacaoEm   *string `json:"situacao_em"`
	SituacaoErro *Erro   `json:"situacao_erro"`
}

// RefCurta é a referência curta da MR: `!32` no GitLab e `repo#29` no GitHub.
func (m MR) RefCurta() string {
	if strings.Contains(m.URL, "/-/merge_requests/") {
		return "!" + strconv.Itoa(m.IID)
	}
	repo := m.ProjetoPath[strings.LastIndex(m.ProjetoPath, "/")+1:]
	return repo + "#" + strconv.Itoa(m.IID)
}

// colunasMR lê a MR com o cache dela; scanMR casa com a ordem.
const colunasMR = `SELECT mr.url, mr.projeto_path, mr.iid, mr.iac, mr.estado, mr.adicionada_em, c.valor_em, c.valor, c.erro_codigo, c.erro_mensagem, c.erro_em
	FROM mr LEFT JOIN cache c ON c.fonte = 'mr' AND c.chave = mr.url `

func scanMR(r scanner) (MR, error) {
	var m MR
	var codigo, mensagem, em, valor *string
	err := r.Scan(&m.URL, &m.ProjetoPath, &m.IID, &m.IaC, &m.Estado, &m.AdicionadaEm, &m.AtualizadoEm, &valor, &codigo, &mensagem, &em)
	if valor != nil {
		var v ValorMR
		if json.Unmarshal([]byte(*valor), &v) == nil {
			if v.Pipeline != "" {
				m.Pipeline = &v.Pipeline
			}
			if v.Aprovacao != "" {
				m.Aprovacao = &v.Aprovacao
			}
			if v.SituacaoEm != "" {
				m.SituacaoEm = &v.SituacaoEm
			}
			m.SituacaoErro = v.SituacaoErro
		}
	}
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
