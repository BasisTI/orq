package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
)

// ondaSaida é a onda mais o que o orq fez no pane do coordenador (pane_nome, ausente quando não
// havia o que renomear). Os campos da onda ficam onde estavam.
type ondaSaida struct {
	central.Onda
	PaneNome *enriquecer.RenomeioPane `json:"pane_nome,omitempty"`
}

// emitOnda escreve a onda depois de pôr no pane do coordenador dela o nome da onda aberta mais
// recente. A falha ao renomear não falha o comando: sai na resposta e no diário.
func (a *App) emitOnda(ctx context.Context, st *central.Store, o central.Onda, texto func(io.Writer, central.Onda) error) error {
	f := enriquecer.Ferramentas{Env: a.Env, Timeout: 15 * time.Second}
	r, err := f.SincronizarNomePane(ctx, st, o.Coordenador)
	saida := ondaSaida{Onda: o, PaneNome: r}
	return a.emit(saida, func(w io.Writer) error {
		if err := texto(w, o); err != nil {
			return err
		}
		if err != nil {
			_, e := fmt.Fprintf(w, "aviso: o diário não registrou o nome do pane: %s\n", q(err.Error()))
			return e
		}
		if r == nil {
			return nil
		}
		prefixo := "pane"
		if len(r.Erros) > 0 {
			prefixo = "aviso: pane"
		}
		if _, err := fmt.Fprintf(w, "%s: %s\n", prefixo, q(r.Resumo(f.ReferenciaViva(ctx, r.Pane)))); err != nil {
			return err
		}
		for _, n := range r.Notas {
			if _, err := fmt.Fprintf(w, "  nota: %s\n", q(n)); err != nil {
				return err
			}
		}
		return nil
	})
}
