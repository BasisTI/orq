package web

import (
	"context"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// Fonte é tudo o que a web lê (spec §2.1). A tela sai do snapshot, nunca de consulta do handler:
// o projeto multi-máquina troca esta interface, não a tela. A escrita (ResponderPendencia) fica
// no *central.Store e é a única.
type Fonte interface {
	Snapshot(ctx context.Context, incluirEncerradas bool) (central.Snapshot, error)
	VerPendencia(ctx context.Context, coord *central.Coordenador, p refs.Pend) (central.Pendencia, error)
	HistoricoRespostas(ctx context.Context, p refs.Pend) ([]central.RespostaRegistrada, error)
	LerUS(ctx context.Context, u refs.US) (central.US, error)
	ListarPendencias(ctx context.Context, f central.FiltroPendencias) ([]central.Pendencia, error)
	ContarPendencias(ctx context.Context, f central.FiltroPendencias) (int, error)
	Diario(ctx context.Context, coord *central.Coordenador, f central.FiltroDiario) ([]central.Evento, error)
	ContarDiario(ctx context.Context, coord *central.Coordenador, f central.FiltroDiario) (int, error)
}

var _ Fonte = (*central.Store)(nil)

// fonte é de onde as páginas leem: Leitura, se o servidor tem uma (testes), senão o Store.
func (s *Servidor) fonte() Fonte {
	if s.Leitura != nil {
		return s.Leitura
	}
	return s.St
}
