package enriquecer

import (
	"context"
	"errors"

	"github.com/BasisTI/orq/internal/central"
)

// ConferenciaSpecs é o ciclo do daemon que confere as specs aprovadas e ainda não mescladas: lê a
// cabeça da MR da spec e, se ela andou desde a última conferência, compara a pasta da spec com a do
// SHA aprovado. O resultado vai para o cache (central.GravarConferenciaSpec), que o snapshot e o
// card da US mostram. Falha da forja vai para o cache e para o log e não derruba o laço.
type ConferenciaSpecs struct {
	E   *Enriquecedor
	Log func(format string, args ...any)
}

func (c *ConferenciaSpecs) logf(f string, a ...any) {
	if c.Log != nil {
		c.Log(f, a...)
	}
}

// Ciclo confere cada spec aprovada. O primeiro erro de banco volta no fim.
func (c *ConferenciaSpecs) Ciclo(ctx context.Context) error {
	specs, err := c.E.St.SpecsParaConferir(ctx)
	if err != nil || len(specs) == 0 {
		return err
	}
	sus, err := c.E.St.Suspensoes(ctx)
	if err != nil {
		return err
	}
	var primeiro error
	for _, s := range specs {
		if ctx.Err() != nil {
			break
		}
		fj, err := forjaDe(s.MRURL)
		if err != nil || c.E.suspensa(sus, fj.ferramenta) {
			continue
		}
		cabeca, err := c.E.F.CabecaMR(ctx, s.MRURL)
		var conf central.ConferenciaSpec
		if err == nil {
			if s.Conferencia != nil && s.Conferencia.SHACabeca == cabeca && s.Erro == nil {
				continue // a cabeça não andou desde a última conferência
			}
			conf, err = c.E.F.ConferirSpecCabeca(ctx, s.MRURL, s.Caminho, s.SHAAprovado, cabeca)
		}
		if err != nil {
			f := comoFalha(err)
			if s.Erro == nil || s.Erro.Codigo != f.Codigo || s.Erro.Mensagem != f.Mensagem {
				c.logf("spec %s: conferência: %s", s.US.Chave(), f.Error())
				if e := c.E.St.GravarConferenciaSpec(ctx, s.US, nil, f.cache()); e != nil && primeiro == nil {
					primeiro = e
				}
			}
			if f.Codigo == ErrAutenticacao {
				if e := c.E.St.Suspender(ctx, fj.ferramenta, *f.cache()); e != nil && primeiro == nil {
					primeiro = e
				}
				sus[fj.ferramenta] = central.Erro{Codigo: f.Codigo, Mensagem: f.Mensagem, Em: c.E.agora().UTC().Format("2006-01-02T15:04:05Z")}
			}
			continue
		}
		if err := c.E.St.GravarConferenciaSpec(ctx, s.US, &conf, nil); err != nil {
			primeiro = errors.Join(primeiro, err)
			continue
		}
		if !conf.PodeMesclar {
			c.logf("spec %s: mudou além do decisoes.md: %v", s.US.Chave(), conf.MudouNaPasta)
		}
	}
	return primeiro
}
