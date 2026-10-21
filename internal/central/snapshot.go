package central

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/BasisTI/orq/internal/refs"
)

// VersaoSnapshot é a versão do esquema de orq snapshot (docs/snapshot.md). Mudança que remove
// ou muda o sentido de um campo sobe a versão; campo novo não sobe.
const VersaoSnapshot = 1

// Snapshot é o estado consolidado que o TUI e o daemon consomem.
type Snapshot struct {
	Versao            int              `json:"versao"`
	GeradoEm          string           `json:"gerado_em"`
	Coordenadores     []CoordSnapshot  `json:"coordenadores"`
	Projetos          []ProjetoTom     `json:"projetos"`
	PendenciasAbertas []Pendencia      `json:"pendencias_abertas"`
	Fontes            map[string]Fonte `json:"fontes"`
}

// CoordSnapshot é um coordenador com o estado vivo do pane dele e as ondas.
type CoordSnapshot struct {
	Coordenador
	Herdr *Herdr `json:"herdr"`
	Ondas []Onda `json:"ondas"`
}

// Snapshot lê tudo numa transação só. Por padrão traz só as ondas abertas.
func (s *Store) Snapshot(ctx context.Context, incluirEncerradas bool) (Snapshot, error) {
	snap := Snapshot{Versao: VersaoSnapshot, GeradoEm: s.agora(), Coordenadores: []CoordSnapshot{}, Projetos: []ProjetoTom{}}
	err := s.ler(ctx, func(tx *sql.Tx) error {
		cs, err := listarCoords(ctx, tx)
		if err != nil {
			return err
		}
		h, err := lerHerdr(ctx, tx)
		if err != nil {
			return err
		}
		for _, c := range cs {
			clause := `WHERE coordenador = ?`
			if !incluirEncerradas {
				clause += ` AND encerrada_em IS NULL`
			}
			os, err := carregarOndas(ctx, tx, clause, c.Nome)
			if err != nil {
				return err
			}
			snap.Coordenadores = append(snap.Coordenadores, CoordSnapshot{Coordenador: c, Herdr: h.pane(c.PaneID), Ondas: os})
		}
		if snap.Projetos, err = carregarTons(ctx, tx); err != nil {
			return err
		}
		if snap.PendenciasAbertas, err = carregarPendencias(ctx, tx, `WHERE p.aberta = 1`); err != nil {
			return err
		}
		snap.Fontes, err = carregarFontes(ctx, tx)
		return err
	})
	return snap, err
}

// Evento é uma linha do diário.
type Evento struct {
	ID          int64           `json:"id"`
	TS          string          `json:"ts"`
	Coordenador *string         `json:"coordenador"`
	OndaID      *int64          `json:"onda_id"`
	US          *string         `json:"us"`
	Tipo        string          `json:"tipo"`
	Detalhe     json.RawMessage `json:"detalhe"`
}

// FiltroDiario restringe orq diario; Limite 0 traz tudo. Deslocamento pula os eventos mais
// recentes (a página da web); o resultado continua em ordem cronológica.
type FiltroDiario struct {
	US           *refs.US
	OndaID       int64
	Coord        string
	Limite       int
	Deslocamento int
}

// condicoes monta o WHERE do diário.
func (f FiltroDiario) condicoes(ctx context.Context, tx *sql.Tx, coord *Coordenador) (string, []any, error) {
	var where []string
	var args []any
	if f.US != nil {
		alvo, err := resolverUS(ctx, tx, *f.US, coord)
		if err != nil {
			return "", nil, err
		}
		where, args = append(where, "projeto = ? AND ref = ?"), append(args, alvo.Projeto, alvo.Ref)
	}
	if f.OndaID != 0 {
		where, args = append(where, "onda_id = ?"), append(args, f.OndaID)
	}
	if f.Coord != "" {
		where, args = append(where, "coordenador = ?"), append(args, f.Coord)
	}
	if len(where) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(where, " AND "), args, nil
}

// ContarDiario conta os eventos que o filtro pega, sem Limite nem Deslocamento.
func (s *Store) ContarDiario(ctx context.Context, coord *Coordenador, f FiltroDiario) (int, error) {
	var n int
	err := s.ler(ctx, func(tx *sql.Tx) error {
		w, args, err := f.condicoes(ctx, tx, coord)
		if err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM evento`+w, args...).Scan(&n)
	})
	return n, err
}

// Diario devolve os eventos mais recentes, em ordem cronológica.
func (s *Store) Diario(ctx context.Context, coord *Coordenador, f FiltroDiario) ([]Evento, error) {
	es := []Evento{}
	err := s.ler(ctx, func(tx *sql.Tx) error {
		w, args, err := f.condicoes(ctx, tx, coord)
		if err != nil {
			return err
		}
		q := `SELECT id, ts, coordenador, onda_id, projeto, ref, tipo, detalhe FROM evento` + w + " ORDER BY id DESC"
		if f.Limite > 0 {
			q, args = q+" LIMIT ? OFFSET ?", append(args, f.Limite, f.Deslocamento)
		}
		rows, err := tx.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Evento
			var projeto *string
			var ref *int
			var detalhe string
			if err := rows.Scan(&e.ID, &e.TS, &e.Coordenador, &e.OndaID, &projeto, &ref, &e.Tipo, &detalhe); err != nil {
				return err
			}
			if projeto != nil && ref != nil {
				k := refs.US{Projeto: *projeto, Ref: *ref}.Chave()
				e.US = &k
			}
			e.Detalhe = json.RawMessage(detalhe)
			es = append(es, e)
		}
		return rows.Err()
	})
	for i, j := 0, len(es)-1; i < j; i, j = i+1, j-1 {
		es[i], es[j] = es[j], es[i]
	}
	return es, err
}
