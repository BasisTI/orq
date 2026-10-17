package enriquecer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
)

// ArquivoTravaDaemon fica no diretório de estado e garante um daemon por máquina.
const ArquivoTravaDaemon = "daemon.lock"

// Daemon roda os enriquecedores em laço e mantém os tokens de sidebar.
type Daemon struct {
	E      *Enriquecedor
	Socket string // socket do Herdr; vazio desliga a assinatura de eventos
	// Tick do Herdr e da conferência dos tokens; zero usa 5 s.
	Tick time.Duration
	// IntervaloRemoto é a cadência com que MR e Taiga são olhados (cada chave ainda respeita o
	// TTL dela); zero usa 15 s.
	IntervaloRemoto time.Duration
	// Reconexao é a espera antes de tentar o socket de novo; zero usa 5 s.
	Reconexao time.Duration
	Log       io.Writer
	// Limpeza, se não nulo, roda a cada IntervaloRemoto: a limpeza automática dos recursos da US
	// no merge. Falha dela vai para o log e não derruba o laço.
	Limpeza interface{ Ciclo(context.Context) error }

	mu      sync.Mutex
	ultimos map[string]string
}

// Rodar segue até ctx acabar (SIGTERM/SIGINT no orq daemon) e, ao sair, relê o Herdr e limpa
// os tokens que publicou e ainda têm o valor do orq. Falha de fonte não derruba o laço; só o erro do banco volta.
func (d *Daemon) Rodar(ctx context.Context) error {
	tick := valorDur(d.Tick, 5*time.Second)
	gatilho := make(chan struct{}, 1)
	sinalizar := func() {
		select {
		case gatilho <- struct{}{}:
		default:
		}
	}
	panes := make(chan []string, 1)
	erros := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.remoto(ctx, sinalizar); err != nil {
			erros <- err
		}
	}()
	if d.Limpeza != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.limpeza(ctx)
		}()
	}
	if d.Socket != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.assinatura(ctx, panes, sinalizar)
		}()
	}
	d.logf("início", "orq daemon no ar (tick %s, socket %s, limpeza automática %s)", tick, valorOu(d.Socket, "desligado"), map[bool]string{true: "ligada", false: "desligada"}[d.Limpeza != nil])
	t := time.NewTicker(tick)
	defer t.Stop()
	var assinados []string
	enviou := false
	var falha error
	for falha == nil {
		ps, err := d.ciclo(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if !ocupado(err) {
				falha = err
				break
			}
			// Outro processo segurou o banco: tenta no próximo tick.
			d.logf("", "ciclo: %v", err)
		}
		if ps != nil && (!enviou || !slices.Equal(ps, assinados)) {
			assinados, enviou = ps, true
			select {
			case <-panes:
			default:
			}
			panes <- ps
		}
		select {
		case <-ctx.Done():
		case <-t.C:
		case <-gatilho:
		case falha = <-erros:
		}
		if ctx.Err() != nil {
			break
		}
	}
	wg.Wait()
	// ctx já acabou: a limpeza ganha um prazo próprio.
	fim, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Relê o Herdr para limpar só o que ainda mostra o valor do orq.
	if _, err := d.E.AtualizarHerdr(fim); err != nil {
		d.logf("", "leitura do Herdr antes da limpeza: %v", err)
	}
	if r, err := d.E.LimparTokens(fim); err != nil {
		d.logf("", "limpeza dos tokens: %v", err)
	} else {
		if len(r.Limpos) > 0 {
			d.logf("", "tokens limpos ao sair: %d", len(r.Limpos))
		}
		if len(r.Erros) > 0 {
			d.logf("", "erros ao limpar os tokens: %s", strings.Join(r.Erros, "; "))
		}
		for _, a := range r.Avisos {
			d.logf("", "%s", a)
		}
		if r.Pulado != nil {
			d.logf("", "limpeza dos tokens: %s", *r.Pulado)
		}
	}
	d.logf("fim", "orq daemon encerrado")
	return falha
}

// ciclo consulta o Herdr, confere os tokens e devolve os panes a assinar (nil se o Herdr
// falhou e a lista não é confiável).
func (d *Daemon) ciclo(ctx context.Context) ([]string, error) {
	r, err := d.E.AtualizarHerdr(ctx)
	if err != nil {
		return nil, err
	}
	if len(r.Erros) > 0 {
		d.logf("herdr", "herdr: %s: %s", r.Erros[0].Erro.Codigo, r.Erros[0].Erro.Mensagem)
		return nil, nil
	}
	d.logf("herdr", "")
	tk, err := d.E.PublicarTokens(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tk.Publicados {
		d.logf("", "token %s=%q publicado em %s %s", t.Nome, t.Valor, t.Alvo, t.AlvoID)
	}
	for _, t := range tk.Limpos {
		d.logf("", "token %s limpo em %s %s", t.Nome, t.Alvo, t.AlvoID)
	}
	d.logf("tokens", "%s", strings.Join(tk.Erros, "; "))
	d.logf("avisos", "%s", strings.Join(tk.Avisos, "; "))
	return d.panesAssinaveis(ctx)
}

// panesAssinaveis são os panes de US e de coordenador das ondas abertas que existem agora.
func (d *Daemon) panesAssinaveis(ctx context.Context) ([]string, error) {
	sessao, _, err := d.E.St.LerSessaoHerdr(ctx)
	if err != nil || sessao == nil {
		return nil, err
	}
	snap, err := d.E.St.Snapshot(ctx, false)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	add := func(p *string) {
		if p == nil {
			return
		}
		if _, ok := sessao.Panes[*p]; ok {
			set[*p] = true
		}
	}
	for _, c := range snap.Coordenadores {
		add(c.PaneID)
		for _, o := range c.Ondas {
			for _, u := range o.US {
				add(u.PaneID)
			}
		}
	}
	ps := make([]string, 0, len(set))
	for p := range set {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps, nil
}

// remoto olha MR e Taiga a cada IntervaloRemoto; cada chave respeita o TTL. Quando algo foi
// consultado, pede um ciclo para os tokens refletirem o estado novo das MRs.
func (d *Daemon) remoto(ctx context.Context, sinalizar func()) error {
	t := time.NewTicker(valorDur(d.IntervaloRemoto, 15*time.Second))
	defer t.Stop()
	for {
		r, err := d.E.Atualizar(ctx, Opcoes{Fontes: []string{central.FonteMR, central.FonteTaiga}})
		if err != nil && !ocupado(err) {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err != nil {
			d.logf("", "MR e Taiga: %v", err)
		}
		consultou := false
		for _, f := range []string{central.FonteMR, central.FonteTaiga} {
			rf := r.Fontes[f]
			if rf == nil {
				continue
			}
			consultou = consultou || rf.Consultadas > 0
			var es []string
			for _, e := range rf.Erros {
				es = append(es, e.Chave+": "+e.Erro.Codigo)
			}
			d.logf(f, "%s", strings.Join(es, "; "))
		}
		if consultou {
			sinalizar()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// limpeza chama a limpeza automática a cada IntervaloRemoto, depois de um primeiro ciclo logo
// ao subir. Fica fora do laço das MRs para um git fetch lento não atrasar a consulta às forjas.
func (d *Daemon) limpeza(ctx context.Context) {
	t := time.NewTicker(valorDur(d.IntervaloRemoto, 15*time.Second))
	defer t.Stop()
	for {
		if err := d.Limpeza.Ciclo(ctx); err != nil && ctx.Err() == nil {
			d.logf("limpeza", "limpeza automática: %v", err)
		} else if err == nil {
			d.logf("limpeza", "")
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// assinatura mantém a assinatura de eventos do Herdr com a lista de panes mais recente. Todo
// (re)início, inclusive depois de events_lost, pede um ciclo: a leitura completa reconcilia o
// que se perdeu.
func (d *Daemon) assinatura(ctx context.Context, panes <-chan []string, sinalizar func()) {
	espera := valorDur(d.Reconexao, 5*time.Second)
	var atual []string
	select {
	case <-ctx.Done():
		return
	case atual = <-panes:
	}
	for {
		sub, cancel := context.WithCancel(ctx)
		fim := make(chan error, 1)
		ps := atual
		go func() {
			fim <- Assinar(sub, d.Socket, ps, func() {
				d.logf("socket", "")
				sinalizar()
			}, func(EventoHerdr) { sinalizar() })
		}()
		select {
		case <-ctx.Done():
			cancel()
			<-fim
			return
		case atual = <-panes:
			cancel()
			<-fim
			continue
		case err := <-fim:
			cancel()
			if errors.Is(err, ErrEventosPerdidos) {
				d.logf("", "herdr: events_lost; reassinando e reconciliando")
				continue
			}
			if err != nil {
				d.logf("socket", "assinatura de eventos: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case atual = <-panes:
		case <-time.After(espera):
		}
	}
}

// logf escreve uma linha. Com chave, só escreve quando a mensagem muda (erro repetido a cada
// tick não enche o log) e uma mensagem vazia marca que o problema passou.
func (d *Daemon) logf(chave, format string, args ...any) {
	if d.Log == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	// Uma trava só para a tabela e para a escrita: o laço, a assinatura e a limpeza logam ao mesmo
	// tempo, e o Writer não é obrigado a aceitar escritas concorrentes.
	d.mu.Lock()
	defer d.mu.Unlock()
	if chave != "" {
		if d.ultimos == nil {
			d.ultimos = map[string]string{}
		}
		antes := d.ultimos[chave]
		d.ultimos[chave] = msg
		if msg == antes {
			return
		}
		if msg == "" {
			msg = chave + ": ok de novo"
		}
	}
	fmt.Fprintf(d.Log, "%s %s\n", time.Now().UTC().Format(time.RFC3339), msg)
}

// ocupado diz se err é o banco_ocupado: espera de outro processo, passageira.
func ocupado(err error) bool { return output.AsError(err).Code == "banco_ocupado" }

func valorDur(d, padrao time.Duration) time.Duration {
	if d <= 0 {
		return padrao
	}
	return d
}
