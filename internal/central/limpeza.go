package central

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/BasisTI/orq/internal/refs"
)

// Localizacao é a US achada pela referência, com a onda e o dono dela.
type Localizacao struct {
	refs.US
	OndaID      int64
	Coordenador string
}

// LocalizarUS resolve a referência (com as regras de projeto padrão do coordenador) e devolve a
// US com o estado atual, sem escrever nada.
func (s *Store) LocalizarUS(ctx context.Context, coord *Coordenador, ref refs.US) (Localizacao, US, error) {
	var alvo usAlvo
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		alvo, err = resolverUS(ctx, tx, ref, coord)
		return err
	})
	if err != nil {
		return Localizacao{}, US{}, err
	}
	u, err := s.verUS(ctx, alvo.US)
	return Localizacao{US: alvo.US, OndaID: alvo.ondaID, Coordenador: alvo.coordenador}, u, err
}

// EventoExterno é o registro, no diário, de algo feito fora do banco (arquivo, pane, git,
// Docker). Tipo precisa ser um dos TiposExternos.
type EventoExterno struct {
	Tipo    string
	Detalhe map[string]any
}

// TiposExternos são os eventos que a limpeza e os recursos gravam no diário.
var TiposExternos = []string{"recursos.gravar", "recursos.reabrir", "limpar.removido", "limpar.preservado", "limpar.falhou", "limpar.recusada", "limpar.onda"}

// RegistrarExternos grava os eventos de uma US numa transação só. Eles descrevem o que o orq
// fez fora do banco; o fato de gravar não muda o estado da Central.
func (s *Store) RegistrarExternos(ctx context.Context, coord *Coordenador, u refs.US, es []EventoExterno) error {
	if len(es) == 0 {
		return nil
	}
	for _, e := range es {
		if !contem(TiposExternos, e.Tipo) {
			return fmt.Errorf("tipo de evento %q não permitido", e.Tipo)
		}
	}
	return s.escrever(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, u, coord)
		if err != nil {
			return err
		}
		for _, e := range es {
			if err := s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: e.Tipo, detalhe: e.Detalhe}); err != nil {
				return err
			}
		}
		return nil
	})
}

// RegistrarExternosOnda é o RegistrarExternos de eventos da onda (sem US).
func (s *Store) RegistrarExternosOnda(ctx context.Context, ondaID int64, es []EventoExterno) error {
	if len(es) == 0 {
		return nil
	}
	for _, e := range es {
		if !contem(TiposExternos, e.Tipo) {
			return fmt.Errorf("tipo de evento %q não permitido", e.Tipo)
		}
	}
	return s.escrever(ctx, func(tx *sql.Tx) error {
		var dono string
		if err := tx.QueryRowContext(ctx, `SELECT coordenador FROM onda WHERE id = ?`, ondaID).Scan(&dono); err != nil {
			if semLinha(err) {
				return naoEncontrado(fmt.Sprintf("onda %d não existe", ondaID), "")
			}
			return err
		}
		for _, e := range es {
			if err := s.registrar(ctx, tx, evento{coordenador: dono, ondaID: ondaID, tipo: e.Tipo, detalhe: e.Detalhe}); err != nil {
				return err
			}
		}
		return nil
	})
}
