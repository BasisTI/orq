package enriquecer

import (
	"context"
	"errors"
	"time"

	"github.com/BasisTI/orq/internal/central"
)

// Intervalos do ciclo de leitura das specs.
const (
	// EsperaAposFalha: depois de uma falha, a spec só é tentada de novo depois disso.
	EsperaAposFalha = time.Minute
	// IntervaloMesclada: a spec de MR mesclada é lida pela branch alvo, que anda com cada commit
	// dela; olhar a cada ciclo gastaria a cota da forja à toa.
	IntervaloMesclada = 5 * time.Minute
)

// LeituraSpecs é o ciclo do daemon que lê a spec registrada pela forja e guarda o conteúdo no cache
// da Central (central.GravarVersaoSpec). A ref é a branch de origem da MR da spec enquanto ela está
// aberta e a branch alvo depois de mesclada. A cada ciclo compara o SHA da ref com o do cache e só
// busca os arquivos quando ele mudou; para cada pendência de validação aberta sem versão no cache,
// lê pelo SHA dela. Falha vai para o cache e para o log, e o último conteúdo bom fica.
type LeituraSpecs struct {
	E   *Enriquecedor
	Log func(format string, args ...any)

	// ultimo é a hora da última tentativa e falhou diz se ela falhou; o ciclo só usa para
	// espaçar as consultas, e perdê-los num reinício só repete uma consulta.
	ultimo map[string]time.Time
	falhou map[string]bool
}

func (l *LeituraSpecs) logf(f string, a ...any) {
	if l.Log != nil {
		l.Log(f, a...)
	}
}

// Ciclo lê cada spec registrada. O primeiro erro de banco volta no fim.
func (l *LeituraSpecs) Ciclo(ctx context.Context) error {
	specs, err := l.E.St.SpecsParaLer(ctx)
	if err != nil || len(specs) == 0 {
		return err
	}
	sus, err := l.E.St.Suspensoes(ctx)
	if err != nil {
		return err
	}
	if l.ultimo == nil {
		l.ultimo, l.falhou = map[string]time.Time{}, map[string]bool{}
	}
	var primeiro error
	for _, s := range specs {
		if ctx.Err() != nil {
			break
		}
		fj, err := forjaDe(s.MRURL)
		if err != nil || l.E.suspensa(sus, fj.ferramenta) {
			continue
		}
		chave := s.US.Chave()
		agora := l.E.agora()
		if t, ok := l.ultimo[chave]; ok {
			espera := time.Duration(0)
			switch {
			case l.falhou[chave]:
				espera = EsperaAposFalha
			case s.Mesclada && s.SHAAtual != "" && s.Erro == nil && semPendenteDeLeitura(s):
				espera = IntervaloMesclada
			}
			if agora.Sub(t) < espera {
				continue
			}
		}
		l.ultimo[chave] = agora
		if err := l.lerUma(ctx, s); err != nil {
			l.falhou[chave] = true
			f := comoFalha(err)
			if f.Codigo == ErrAutenticacao {
				if e := l.E.St.Suspender(ctx, fj.ferramenta, *f.cache()); e != nil && primeiro == nil {
					primeiro = e
				}
				sus[fj.ferramenta] = central.Erro{Codigo: f.Codigo, Mensagem: f.Mensagem, Em: agora.UTC().Format("2006-01-02T15:04:05Z")}
			}
			// O log só registra a falha nova; a falha vai sempre ao cache, porque uma leitura
			// parcial no mesmo ciclo pode ter limpado a anterior.
			if s.Erro == nil || s.Erro.Codigo != f.Codigo || s.Erro.Mensagem != f.Mensagem {
				l.logf("spec %s: leitura: %s", chave, f.Error())
			}
			if e := l.E.St.GravarErroLeituraSpec(ctx, s.US, *f.cache()); e != nil {
				primeiro = errors.Join(primeiro, e)
			}
			continue
		}
		l.falhou[chave] = false
	}
	return primeiro
}

// semPendenteDeLeitura: toda validação aberta já tem a versão dela no cache.
func semPendenteDeLeitura(s central.SpecParaLer) bool {
	for _, v := range s.Validacoes {
		if !s.Versoes[v] {
			return false
		}
	}
	return true
}

// lerUma lê a ref atual da spec (se o SHA mudou) e a versão de cada validação aberta sem cache.
// Erro de banco e falha da forja voltam juntos; a falha da forja é a que o chamador grava.
func (l *LeituraSpecs) lerUma(ctx context.Context, s central.SpecParaLer) error {
	d, err := l.E.F.ConsultarMRDetalhe(ctx, s.MRURL)
	if err != nil {
		return err
	}
	sha := d.Head
	if d.Estado == "mesclada" {
		if sha, err = l.E.F.SHADaBranch(ctx, s.MRURL, d.Alvo); err != nil {
			return err
		}
	}
	if sha == "" {
		return &Falha{Codigo: ErrRespostaInvalida, Mensagem: "a forja não disse o SHA da ref da MR da spec"}
	}
	mudou := false
	if !s.Versoes[sha] {
		arquivos, err := l.E.F.LerSpecNoSHA(ctx, s.MRURL, s.Caminho, sha)
		if err != nil {
			return err
		}
		if err := l.E.St.GravarVersaoSpec(ctx, s.US, s.Caminho, s.MRURL, sha, true, arquivos); err != nil {
			return err
		}
		mudou = true
	} else if s.SHAAtual != sha || s.Erro != nil {
		// A versão já estava guardada (era a de uma validação, ou a falha anterior foi passageira).
		if err := l.E.St.MarcarLeituraSpec(ctx, s.US, sha); err != nil {
			return err
		}
		mudou = true
	}
	for _, v := range s.Validacoes {
		if v == sha || s.Versoes[v] {
			continue
		}
		arquivos, err := l.E.F.LerSpecNoSHA(ctx, s.MRURL, s.Caminho, v)
		if err != nil {
			return &Falha{Codigo: comoFalha(err).Codigo, Mensagem: "validação em " + shaCurto(v) + ": " + comoFalha(err).Mensagem}
		}
		if err := l.E.St.GravarVersaoSpec(ctx, s.US, s.Caminho, s.MRURL, v, false, arquivos); err != nil {
			return err
		}
		mudou = true
	}
	if mudou {
		l.logf("spec %s: lida em %s", s.US.Chave(), shaCurto(sha))
	}
	return nil
}

func shaCurto(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
