package enriquecer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// TTLs padrão de cada fonte. O Herdr é consultado a cada tick do daemon.
var TTLPadrao = map[string]time.Duration{
	central.FonteHerdr: 5 * time.Second,
	central.FonteMR:    60 * time.Second,
	central.FonteTaiga: 5 * time.Minute,
}

// SuspensaoPadrao é quanto uma ferramenta fica sem ser chamada depois de erro de autenticação.
const SuspensaoPadrao = 30 * time.Minute

// Enriquecedor consulta as fontes e grava o cache.
type Enriquecedor struct {
	St *central.Store
	F  Ferramentas
	// Now é o relógio do TTL; nil usa time.Now.
	Now func() time.Time
	// TTL por fonte; fonte ausente usa TTLPadrao.
	TTL map[string]time.Duration
	// Suspensao é a pausa depois de erro de autenticação; zero usa SuspensaoPadrao.
	Suspensao time.Duration
}

// Opcoes de uma atualização. Fontes vazio quer dizer todas.
type Opcoes struct {
	Fontes []string
	Forcar bool // ignora TTL e suspensão
}

// Resultado conta o que cada fonte fez.
type Resultado struct {
	Fontes map[string]*ResultadoFonte `json:"fontes"`
	Tokens *ResultadoTokens           `json:"tokens"`
}

// ResultadoFonte conta as consultas de uma fonte.
type ResultadoFonte struct {
	Consultadas int         `json:"consultadas"`
	EmDia       int         `json:"em_dia"`    // puladas pelo TTL
	Suspensas   int         `json:"suspensas"` // puladas por suspensão de autenticação
	Erros       []ErroChave `json:"erros"`
}

// ErroChave é a falha da consulta de uma chave (URL da MR, projeto/ref, ou "" no Herdr).
type ErroChave struct {
	Chave string       `json:"chave"`
	Erro  central.Erro `json:"erro"`
}

func (e *Enriquecedor) agora() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Enriquecedor) ttl(fonte string) time.Duration {
	if d, ok := e.TTL[fonte]; ok {
		return d
	}
	return TTLPadrao[fonte]
}

// emDia diz se a última consulta da chave ainda vale pelo TTL.
func (e *Enriquecedor) emDia(c central.Cache, ok bool, fonte string) bool {
	if !ok {
		return false
	}
	t, err := time.Parse(time.RFC3339, c.ConsultadoEm)
	return err == nil && e.agora().Sub(t) < e.ttl(fonte)
}

// ValidarFontes confere os nomes de --fonte.
func ValidarFontes(fs []string) error {
	for _, f := range fs {
		if !contem(central.Fontes, f) {
			return fmt.Errorf("fonte %q desconhecida: use %s", f, strings.Join(central.Fontes, ", "))
		}
	}
	return nil
}

// Atualizar consulta as fontes pedidas. Falha de uma consulta fica no cache e no Resultado;
// o erro devolvido é só o do banco.
func (e *Enriquecedor) Atualizar(ctx context.Context, op Opcoes) (Resultado, error) {
	fontes := op.Fontes
	if len(fontes) == 0 {
		fontes = central.Fontes
	}
	res := Resultado{Fontes: map[string]*ResultadoFonte{}}
	snap, err := e.St.Snapshot(ctx, false)
	if err != nil {
		return res, err
	}
	for _, f := range central.Fontes {
		if !contem(fontes, f) {
			continue
		}
		var r ResultadoFonte
		switch f {
		case central.FonteHerdr:
			r, err = e.atualizarHerdr(ctx, op.Forcar)
		case central.FonteMR:
			r, err = e.atualizarMRs(ctx, snap, op.Forcar)
		case central.FonteTaiga:
			r, err = e.atualizarTaiga(ctx, snap, op.Forcar)
		}
		if r.Erros == nil {
			r.Erros = []ErroChave{}
		}
		res.Fontes[f] = &r
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

// AtualizarHerdr consulta o Herdr agora, sem olhar o TTL (o tick do daemon).
func (e *Enriquecedor) AtualizarHerdr(ctx context.Context) (ResultadoFonte, error) {
	return e.atualizarHerdr(ctx, true)
}

func (e *Enriquecedor) atualizarHerdr(ctx context.Context, forcar bool) (ResultadoFonte, error) {
	var r ResultadoFonte
	if !forcar {
		cs, err := e.St.LerCache(ctx, central.FonteHerdr)
		if err != nil {
			return r, err
		}
		if c, ok := cs[""]; e.emDia(c, ok, central.FonteHerdr) {
			r.EmDia++
			return r, nil
		}
	}
	r.Consultadas++
	s, err := e.F.ConsultarHerdr(ctx)
	if err != nil {
		ce := comoFalha(err).cache()
		r.Erros = append(r.Erros, ErroChave{Erro: *ce})
		return r, e.St.GravarCache(ctx, central.FonteHerdr, "", nil, ce)
	}
	return r, e.St.GravarCache(ctx, central.FonteHerdr, "", s, nil)
}

// suspensa diz se a ferramenta está suspensa por autenticação e a pausa ainda não acabou.
func (e *Enriquecedor) suspensa(sus map[string]central.Erro, ferramenta string) bool {
	s, ok := sus[ferramenta]
	if !ok {
		return false
	}
	t, err := time.Parse(time.RFC3339, s.Em)
	pausa := e.Suspensao
	if pausa == 0 {
		pausa = SuspensaoPadrao
	}
	return err != nil || e.agora().Sub(t) < pausa
}

// consultaChave é o laço comum de MR e Taiga: TTL, suspensão, consulta e gravação.
type consultaChave struct {
	chave      string
	ferramenta string
	consultar  func() (any, error)
	gravar     func(valor any, erro *central.Erro) error
}

func (e *Enriquecedor) consultarChaves(ctx context.Context, fonte string, cs []consultaChave, forcar bool) (ResultadoFonte, error) {
	var r ResultadoFonte
	cache, err := e.St.LerCache(ctx, fonte)
	if err != nil {
		return r, err
	}
	sus, err := e.St.Suspensoes(ctx)
	if err != nil {
		return r, err
	}
	novas := map[string]bool{}
	for _, c := range cs {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		if l, ok := cache[c.chave]; !forcar && e.emDia(l, ok, fonte) {
			r.EmDia++
			continue
		}
		// --forcar passa por cima da suspensão anterior, não da que nasceu nesta execução.
		if (!forcar && e.suspensa(sus, c.ferramenta)) || novas[c.ferramenta] {
			r.Suspensas++
			continue
		}
		r.Consultadas++
		v, err := c.consultar()
		if err != nil {
			f := comoFalha(err)
			r.Erros = append(r.Erros, ErroChave{Chave: c.chave, Erro: *f.cache()})
			if err := c.gravar(nil, f.cache()); err != nil {
				return r, err
			}
			if f.Codigo == ErrAutenticacao {
				// Não tenta login nem define token: para de chamar a ferramenta e registra.
				if err := e.St.Suspender(ctx, c.ferramenta, *f.cache()); err != nil {
					return r, err
				}
				novas[c.ferramenta] = true
				sus[c.ferramenta] = central.Erro{Codigo: f.Codigo, Mensagem: f.Mensagem, Em: e.agora().UTC().Format(time.RFC3339)}
			}
			continue
		}
		if _, ok := sus[c.ferramenta]; ok {
			if err := e.St.Retomar(ctx, c.ferramenta); err != nil {
				return r, err
			}
			delete(sus, c.ferramenta)
		}
		if err := c.gravar(v, nil); err != nil {
			return r, err
		}
	}
	return r, nil
}

func (e *Enriquecedor) atualizarMRs(ctx context.Context, snap central.Snapshot, forcar bool) (ResultadoFonte, error) {
	var cs []consultaChave
	vistas := map[string]bool{}
	for _, c := range snap.Coordenadores {
		for _, o := range c.Ondas {
			for _, u := range o.US {
				for _, m := range u.MRs {
					if vistas[m.URL] {
						continue
					}
					vistas[m.URL] = true
					url := m.URL
					ferramenta := "gh"
					if fj, err := forjaDe(url); err == nil {
						ferramenta = fj.ferramenta
					}
					cs = append(cs, consultaChave{
						chave: url, ferramenta: ferramenta,
						consultar: func() (any, error) { return e.F.ConsultarMR(ctx, url) },
						gravar: func(v any, erro *central.Erro) error {
							vm, _ := v.(central.ValorMR)
							return e.St.GravarMR(ctx, url, vm, erro)
						},
					})
				}
			}
		}
	}
	return e.consultarChaves(ctx, central.FonteMR, cs, forcar)
}

func (e *Enriquecedor) atualizarTaiga(ctx context.Context, snap central.Snapshot, forcar bool) (ResultadoFonte, error) {
	var cs []consultaChave
	vistas := map[string]bool{}
	for _, c := range snap.Coordenadores {
		for _, o := range c.Ondas {
			for _, u := range o.US {
				if vistas[u.Chave] {
					continue
				}
				vistas[u.Chave] = true
				chave, alvo := u.Chave, refs.US{Projeto: u.Projeto, Ref: u.Ref}
				cs = append(cs, consultaChave{
					chave: chave, ferramenta: "taiga",
					consultar: func() (any, error) { return e.F.ConsultarTaiga(ctx, alvo.Projeto, alvo.Ref) },
					gravar: func(v any, erro *central.Erro) error {
						if erro != nil {
							return e.St.GravarCache(ctx, central.FonteTaiga, chave, nil, erro)
						}
						return e.St.GravarCache(ctx, central.FonteTaiga, chave, v, nil)
					},
				})
			}
		}
	}
	return e.consultarChaves(ctx, central.FonteTaiga, cs, forcar)
}

func contem(xs []string, x string) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}
