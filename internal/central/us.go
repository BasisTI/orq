package central

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// Valores aceitos para o status de uma US e o papel do pane que a conduz.
var (
	Status = []string{"wait", "plan", "exec", "rev", "done"}
	Papeis = []string{"planejamento", "implementacao", "revisao"}
)

// US é uma User Story dentro de uma onda.
type US struct {
	ID           string      `json:"id"`    // TG-215
	Chave        string      `json:"chave"` // projeto/215
	Projeto      string      `json:"projeto"`
	Ref          int         `json:"ref"`
	OndaID       int64       `json:"onda_id"`
	Posicao      int         `json:"posicao"`
	DependeDe    []string    `json:"depende_de"`
	Status       string      `json:"status"`
	PaneID       *string     `json:"pane_id"`
	Papel        *string     `json:"papel"`
	Rodada       *int        `json:"rodada"`
	AtualizadoEm string      `json:"atualizado_em"`
	MRs          []MR        `json:"mrs"`
	Pendencias   []Pendencia `json:"pendencias"`
	Herdr        *Herdr      `json:"herdr"` // nulo sem pane
	Taiga        Taiga       `json:"taiga"`
}

func carregarUS(ctx context.Context, q querier, clause string, args ...any) ([]US, error) {
	rows, err := q.QueryContext(ctx, `SELECT projeto, ref, onda_id, posicao, status, pane_id, papel, rodada, atualizado_em FROM us `+clause+` ORDER BY onda_id, posicao`, args...)
	if err != nil {
		return nil, err
	}
	us := []US{}
	for rows.Next() {
		var u US
		if err := rows.Scan(&u.Projeto, &u.Ref, &u.OndaID, &u.Posicao, &u.Status, &u.PaneID, &u.Papel, &u.Rodada, &u.AtualizadoEm); err != nil {
			rows.Close()
			return nil, err
		}
		u.ID, u.Chave = refs.Exibir(u.Ref), refs.US{Projeto: u.Projeto, Ref: u.Ref}.Chave()
		us = append(us, u)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	h, err := lerHerdr(ctx, q)
	if err != nil {
		return nil, err
	}
	for i := range us {
		u := &us[i]
		u.Herdr = h.pane(u.PaneID)
		if u.Taiga, err = carregarTaiga(ctx, q, u.Chave); err != nil {
			return nil, err
		}
		if u.DependeDe, err = carregarDeps(ctx, q, u.Projeto, u.Ref); err != nil {
			return nil, err
		}
		if u.MRs, err = carregarMRs(ctx, q, u.Projeto, u.Ref); err != nil {
			return nil, err
		}
		if u.Pendencias, err = carregarPendencias(ctx, q, `WHERE p.projeto = ? AND p.ref = ?`, u.Projeto, u.Ref); err != nil {
			return nil, err
		}
	}
	return us, nil
}

func carregarDeps(ctx context.Context, q querier, projeto string, ref int) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT d.dep_projeto, d.dep_ref FROM dependencia d LEFT JOIN us ON us.projeto = d.dep_projeto AND us.ref = d.dep_ref
		WHERE d.projeto = ? AND d.ref = ? ORDER BY us.posicao, d.dep_projeto, d.dep_ref`, projeto, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deps := []string{}
	for rows.Next() {
		var d refs.US
		if err := rows.Scan(&d.Projeto, &d.Ref); err != nil {
			return nil, err
		}
		deps = append(deps, d.Chave())
	}
	return deps, rows.Err()
}

// usAlvo é a US resolvida, com o dono da onda para registrar o evento.
type usAlvo struct {
	refs.US
	ondaID      int64
	coordenador string
}

// resolverUS acha a US de uma referência. Com projeto, procura direto. Sem projeto (`TG-312`),
// procura a ref em todos os projetos; havendo mais de uma, fica com a que está numa onda aberta
// do coordenador corrente e, se ainda sobrar mais de uma, com a do projeto padrão dele. O que
// continuar ambíguo é o erro ref_ambigua.
func resolverUS(ctx context.Context, q querier, u refs.US, coord *Coordenador) (usAlvo, error) {
	where, args := `WHERE us.ref = ?`, []any{u.Ref}
	if u.Projeto != "" {
		where, args = where+` AND us.projeto = ?`, append(args, u.Projeto)
	}
	rows, err := q.QueryContext(ctx, `SELECT us.projeto, us.onda_id, onda.coordenador, onda.encerrada_em IS NULL FROM us JOIN onda ON onda.id = us.onda_id `+where+` ORDER BY us.projeto`, args...)
	if err != nil {
		return usAlvo{}, err
	}
	type cand struct {
		usAlvo
		aberta bool
	}
	var cs []cand
	for rows.Next() {
		c := cand{usAlvo: usAlvo{US: refs.US{Ref: u.Ref}}}
		if err := rows.Scan(&c.Projeto, &c.ondaID, &c.coordenador, &c.aberta); err != nil {
			rows.Close()
			return usAlvo{}, err
		}
		cs = append(cs, c)
	}
	if err := rows.Close(); err != nil {
		return usAlvo{}, err
	}
	if len(cs) == 0 {
		return usAlvo{}, naoEncontrado(u.Chave()+" não está em nenhuma onda", "inclua a US numa onda com orq onda criar … --us "+fmt.Sprint(u.Ref))
	}
	filtrar := func(ok func(cand) bool) {
		var f []cand
		for _, c := range cs {
			if ok(c) {
				f = append(f, c)
			}
		}
		if len(f) > 0 {
			cs = f
		}
	}
	if len(cs) > 1 && coord != nil {
		filtrar(func(c cand) bool { return c.aberta && c.coordenador == coord.Nome })
		if len(cs) > 1 && coord.ProjetoPadrao != nil {
			filtrar(func(c cand) bool { return c.Projeto == *coord.ProjetoPadrao })
		}
	}
	if len(cs) > 1 {
		chaves := make([]string, len(cs))
		for i, c := range cs {
			chaves[i] = c.US.Chave()
		}
		return usAlvo{}, &output.Error{Code: "ref_ambigua", Cause: fmt.Sprintf("%s existe em mais de um projeto: %s", refs.Exibir(u.Ref), strings.Join(chaves, ", ")), Recovery: "use projeto/ref, ex. " + chaves[0], Exit: output.ExitUsage}
	}
	return cs[0].usAlvo, nil
}

// MudancaStatus são os dados de orq us status; ponteiro nulo mantém o valor gravado.
type MudancaStatus struct {
	Status string
	Pane   *string
	Papel  *string
	Rodada *int
	Forcar bool
}

// AtualizarStatus muda o status da US e, se vierem, o pane, o papel e a rodada. Toda transição
// é livre, inclusive as idas e voltas entre exec e rev, menos sair de done, que exige Forcar.
// Sem nada a mudar, não grava nem gera evento (Alterada = false).
func (s *Store) AtualizarStatus(ctx context.Context, coord *Coordenador, ref refs.US, m MudancaStatus) (US, bool, error) {
	if !contem(Status, m.Status) {
		return US{}, false, uso(fmt.Sprintf("status %q inválido", m.Status), "use "+strings.Join(Status, ", "))
	}
	if m.Papel != nil && !contem(Papeis, *m.Papel) {
		return US{}, false, uso(fmt.Sprintf("papel %q inválido", *m.Papel), "use "+strings.Join(Papeis, ", "))
	}
	if m.Rodada != nil && *m.Rodada < 1 {
		return US{}, false, uso("--rodada precisa ser 1 ou mais", "")
	}
	if m.Pane != nil && temControle(*m.Pane) {
		return US{}, false, uso("pane com caractere de controle", "")
	}
	var alvo usAlvo
	alterada := false
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		var err error
		if alvo, err = resolverUS(ctx, tx, ref, coord); err != nil {
			return err
		}
		var atual US
		err = tx.QueryRowContext(ctx, `SELECT status, pane_id, papel, rodada FROM us WHERE projeto = ? AND ref = ?`, alvo.Projeto, alvo.Ref).
			Scan(&atual.Status, &atual.PaneID, &atual.Papel, &atual.Rodada)
		if err != nil {
			return err
		}
		if atual.Status == "done" && m.Status != "done" && !m.Forcar {
			return conflito("transicao_invalida", fmt.Sprintf("%s está em done; voltar para %s exige --forcar", alvo.Chave(), m.Status), "repita com --forcar se a US precisa mesmo reabrir")
		}
		detalhe := map[string]any{}
		if atual.Status != m.Status {
			detalhe["de"], detalhe["para"] = atual.Status, m.Status
		}
		if m.Pane != nil && !iguais(atual.PaneID, m.Pane) {
			detalhe["pane_id"] = *m.Pane
		}
		if m.Papel != nil && !iguais(atual.Papel, m.Papel) {
			detalhe["papel"] = *m.Papel
		}
		if m.Rodada != nil && (atual.Rodada == nil || *atual.Rodada != *m.Rodada) {
			detalhe["rodada"] = *m.Rodada
		}
		if len(detalhe) == 0 {
			return nil
		}
		if atual.Status == "done" && m.Status != "done" {
			detalhe["forcado"] = true
		}
		detalhe["status"] = m.Status
		alterada = true
		_, err = tx.ExecContext(ctx, `UPDATE us SET status = ?, pane_id = COALESCE(?, pane_id), papel = COALESCE(?, papel), rodada = COALESCE(?, rodada), atualizado_em = ?
			WHERE projeto = ? AND ref = ?`, m.Status, m.Pane, m.Papel, m.Rodada, s.agora(), alvo.Projeto, alvo.Ref)
		if err != nil {
			return err
		}
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "us.status", detalhe: detalhe})
	})
	if err != nil {
		return US{}, false, err
	}
	u, err := s.verUS(ctx, alvo.US)
	return u, alterada, err
}

func (s *Store) verUS(ctx context.Context, u refs.US) (US, error) {
	var x US
	err := s.ler(ctx, func(tx *sql.Tx) error {
		us, err := carregarUS(ctx, tx, `WHERE projeto = ? AND ref = ?`, u.Projeto, u.Ref)
		if err != nil {
			return err
		}
		if len(us) == 0 {
			return naoEncontrado(u.Chave()+" não existe", "")
		}
		x = us[0]
		return nil
	})
	return x, err
}

// autor é quem assina o evento: o coordenador corrente ou, sem ele, o dono da onda da US.
func autor(coord *Coordenador, alvo usAlvo) string {
	if coord != nil {
		return coord.Nome
	}
	return alvo.coordenador
}

func iguais(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
