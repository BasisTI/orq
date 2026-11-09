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
	// Precondicao é o texto de "Mesclar depois de …": o que falta para a MR em Draft sair dele.
	// Só vale enquanto o estado é draft; nula quando ninguém a registrou.
	Precondicao *string `json:"precondicao"`
}

// DraftComPrecondicao diz se a MR está em Draft de propósito, com o que falta registrado. É o
// terceiro caso, distinto de "pronta" e de "Draft" comum.
func (m MR) DraftComPrecondicao() bool {
	return m.Estado != nil && *m.Estado == "draft" && m.Precondicao != nil && *m.Precondicao != ""
}

// RefCurta é a referência curta da MR, sempre com o projeto (o da URL da MR, não o da onda):
// `argocd-apps!32` no GitLab e `repo#29` no GitHub. Nunca `!32` sozinho.
func (m MR) RefCurta() string {
	repo := m.ProjetoPath[strings.LastIndex(m.ProjetoPath, "/")+1:]
	if strings.Contains(m.URL, "/-/merge_requests/") {
		return repo + "!" + strconv.Itoa(m.IID)
	}
	return repo + "#" + strconv.Itoa(m.IID)
}

// colunasMR lê a MR com o cache dela; scanMR casa com a ordem.
const colunasMR = `SELECT mr.url, mr.projeto_path, mr.iid, mr.iac, mr.estado, mr.adicionada_em, c.valor_em, c.valor, c.erro_codigo, c.erro_mensagem, c.erro_em, mr.precondicao
	FROM mr LEFT JOIN cache c ON c.fonte = 'mr' AND c.chave = mr.url `

func scanMR(r scanner) (MR, error) {
	var m MR
	var codigo, mensagem, em, valor *string
	err := r.Scan(&m.URL, &m.ProjetoPath, &m.IID, &m.IaC, &m.Estado, &m.AdicionadaEm, &m.AtualizadoEm, &valor, &codigo, &mensagem, &em, &m.Precondicao)
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

// TamanhoMaxPrecondicao é o limite do texto da pré-condição, em runas.
const TamanhoMaxPrecondicao = 300

// DefinirPrecondicaoMR registra (ou, com texto vazio, limpa) a pré-condição de uma MR ligada à US.
// O mesmo texto de novo não muda nada nem gera evento.
func (s *Store) DefinirPrecondicaoMR(ctx context.Context, coord *Coordenador, ref refs.US, rawURL, texto string) (MR, error) {
	m, err := refs.ParseMR(rawURL)
	if err != nil {
		return MR{}, uso(err.Error(), "")
	}
	texto = strings.Join(strings.Fields(texto), " ")
	if len([]rune(texto)) > TamanhoMaxPrecondicao {
		return MR{}, uso("a pré-condição passa de "+strconv.Itoa(TamanhoMaxPrecondicao)+" caracteres", "resuma no que falta, por exemplo: Mesclar depois de orq#27")
	}
	var out MR
	err = s.escrever(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, ref, coord)
		if err != nil {
			return err
		}
		out, err = scanMR(tx.QueryRowContext(ctx, colunasMR+`WHERE mr.projeto = ? AND mr.ref = ? AND mr.url = ?`, alvo.Projeto, alvo.Ref, m.URL))
		if semLinha(err) {
			return naoEncontrado(m.URL+" não está ligada a "+alvo.Chave(), "ligue com orq mr add")
		}
		if err != nil {
			return err
		}
		antes := ""
		if out.Precondicao != nil {
			antes = *out.Precondicao
		}
		if antes == texto {
			return nil
		}
		var valor any
		if texto != "" {
			valor = texto
			out.Precondicao = &texto
		} else {
			out.Precondicao = nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE mr SET precondicao = ? WHERE projeto = ? AND ref = ? AND url = ?`, valor, alvo.Projeto, alvo.Ref, m.URL); err != nil {
			return err
		}
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "mr.precondicao",
			detalhe: map[string]any{"url": m.URL, "de": antes, "para": texto}})
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
