package central

import (
	"context"
	"database/sql"
	"strings"

	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// Coordenador é um agente que conduz ondas, registrado com o pane do Herdr onde roda.
type Coordenador struct {
	Nome          string  `json:"nome"`
	PaneID        *string `json:"pane_id"`
	WorkspaceID   *string `json:"workspace_id"`
	ProjetoPadrao *string `json:"projeto_padrao"`
	RegistradoEm  string  `json:"registrado_em"`
}

// Registro são os dados de `orq coord registrar`; campo vazio mantém o valor gravado.
type Registro struct {
	Nome, Pane, Workspace, Projeto string
}

// RegistrarCoord cria ou atualiza um coordenador. O pane fica com um coordenador só: quem o
// tinha antes perde o vínculo, e o evento conta de quem ele saiu.
func (s *Store) RegistrarCoord(ctx context.Context, r Registro) (Coordenador, error) {
	nome := strings.TrimSpace(r.Nome)
	if nome == "" || temControle(nome) {
		return Coordenador{}, uso("nome de coordenador vazio ou com caractere de controle", "use um nome curto, ex. assistente")
	}
	if temControle(r.Pane) || temControle(r.Workspace) {
		return Coordenador{}, uso("pane ou workspace com caractere de controle", "")
	}
	projeto := ""
	if r.Projeto != "" {
		p, err := refs.ValidarProjeto(r.Projeto)
		if err != nil {
			return Coordenador{}, uso(err.Error(), "")
		}
		projeto = p
	}
	var c Coordenador
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		detalhe := map[string]any{}
		if r.Pane != "" {
			var outro string
			err := tx.QueryRowContext(ctx, `SELECT nome FROM coordenador WHERE pane_id = ? AND nome <> ?`, r.Pane, nome).Scan(&outro)
			if err == nil {
				if _, err := tx.ExecContext(ctx, `UPDATE coordenador SET pane_id = NULL WHERE nome = ?`, outro); err != nil {
					return err
				}
				detalhe["pane_liberado_de"] = outro
			} else if !semLinha(err) {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO coordenador (nome, pane_id, workspace_id, projeto_padrao, registrado_em) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (nome) DO UPDATE SET
			  pane_id = COALESCE(excluded.pane_id, pane_id),
			  workspace_id = COALESCE(excluded.workspace_id, workspace_id),
			  projeto_padrao = COALESCE(excluded.projeto_padrao, projeto_padrao),
			  registrado_em = excluded.registrado_em`,
			nome, nulo(r.Pane), nulo(r.Workspace), nulo(projeto), s.agora())
		if err != nil {
			return err
		}
		c, err = coordPorNome(ctx, tx, nome)
		if err != nil {
			return err
		}
		detalhe["pane_id"], detalhe["workspace_id"], detalhe["projeto_padrao"] = c.PaneID, c.WorkspaceID, c.ProjetoPadrao
		return s.registrar(ctx, tx, evento{coordenador: nome, tipo: "coord.registrar", detalhe: detalhe})
	})
	return c, err
}

// Resolucao diz de onde tirar o coordenador corrente, em ordem: flag, $ORQ_COORD e o pane.
type Resolucao struct {
	Flag, EnvCoord, EnvPane string
}

// ResolverCoord acha o coordenador corrente pela flag --coord, por $ORQ_COORD ou pelo
// $HERDR_PANE_ID registrado.
func (s *Store) ResolverCoord(ctx context.Context, r Resolucao) (Coordenador, error) {
	var c Coordenador
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		c, err = resolverCoord(ctx, tx, r)
		return err
	})
	return c, err
}

func resolverCoord(ctx context.Context, q querier, r Resolucao) (Coordenador, error) {
	registrar := "registre com orq coord registrar <nome>"
	for _, nome := range []string{r.Flag, r.EnvCoord} {
		if nome == "" {
			continue
		}
		c, err := coordPorNome(ctx, q, nome)
		if semLinha(err) {
			return c, naoEncontrado("coordenador "+nome+" não registrado", registrar)
		}
		return c, err
	}
	if r.EnvPane != "" {
		row := q.QueryRowContext(ctx, `SELECT nome, pane_id, workspace_id, projeto_padrao, registrado_em FROM coordenador WHERE pane_id = ?`, r.EnvPane)
		c, err := scanCoord(row)
		if semLinha(err) {
			return c, &output.Error{Code: "coord_nao_resolvido", Cause: "nenhum coordenador registrado no pane " + r.EnvPane, Recovery: registrar + " neste pane, ou passe --coord", Exit: output.ExitUsage}
		}
		return c, err
	}
	return Coordenador{}, &output.Error{Code: "coord_nao_resolvido", Cause: "coordenador não informado", Recovery: "passe --coord <nome>, defina ORQ_COORD ou rode dentro de um pane do Herdr registrado", Exit: output.ExitUsage}
}

// ListarCoords devolve os coordenadores em ordem de nome.
func (s *Store) ListarCoords(ctx context.Context) ([]Coordenador, error) {
	var cs []Coordenador
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		cs, err = listarCoords(ctx, tx)
		return err
	})
	return cs, err
}

func listarCoords(ctx context.Context, q querier) ([]Coordenador, error) {
	rows, err := q.QueryContext(ctx, `SELECT nome, pane_id, workspace_id, projeto_padrao, registrado_em FROM coordenador ORDER BY nome`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cs := []Coordenador{}
	for rows.Next() {
		c, err := scanCoord(rows)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, rows.Err()
}

func coordPorNome(ctx context.Context, q querier, nome string) (Coordenador, error) {
	return scanCoord(q.QueryRowContext(ctx, `SELECT nome, pane_id, workspace_id, projeto_padrao, registrado_em FROM coordenador WHERE nome = ?`, nome))
}

type scanner interface{ Scan(...any) error }

func scanCoord(r scanner) (Coordenador, error) {
	var c Coordenador
	err := r.Scan(&c.Nome, &c.PaneID, &c.WorkspaceID, &c.ProjetoPadrao, &c.RegistradoEm)
	return c, err
}

// querier é o que *sql.DB e *sql.Tx têm em comum.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
