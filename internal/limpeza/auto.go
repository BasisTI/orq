package limpeza

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// ReintentoPadrao é a espera antes de o daemon tentar de novo uma US que ficou com algo
// preservado (agente trabalhando, árvore suja…).
const ReintentoPadrao = 5 * time.Minute

// LigadaPeloAmbiente diz se a limpeza automática está ligada: ORQ_LIMPEZA_AUTOMATICA vazia ou
// qualquer valor, menos 0, false, nao, não, off e desligada.
func LigadaPeloAmbiente(env func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(env("ORQ_LIMPEZA_AUTOMATICA"))) {
	case "0", "false", "nao", "não", "off", "desligada", "desligado":
		return false
	}
	return true
}

// Auto é a limpeza que o daemon dispara: a cada ciclo, roda o Limpar nas US com todas as MRs
// mescladas e recursos gravados e, quando a última US de uma onda termina, encerra a onda e
// limpa o Docker dela.
type Auto struct {
	L *Limpador
	// Log recebe uma linha por fato relevante; nil descarta.
	Log func(format string, args ...any)
	// Reintento é a espera entre tentativas numa US que não terminou; zero usa ReintentoPadrao.
	Reintento time.Duration
	// Agora é o relógio; nil usa time.Now.
	Agora func() time.Time

	mu      sync.Mutex
	tentado map[string]*tentativa
}

type tentativa struct {
	assinatura string
	proxima    time.Time
}

func (a *Auto) agora() time.Time {
	if a.Agora != nil {
		return a.Agora()
	}
	return time.Now()
}

func (a *Auto) logf(format string, args ...any) {
	if a.Log != nil {
		a.Log(format, args...)
	}
}

// candidata: a US tem MR, todas mescladas segundo o cache, e recursos gravados.
func (a *Auto) candidata(u central.US) bool {
	if len(u.MRs) == 0 {
		return false
	}
	for _, m := range u.MRs {
		if m.Estado == nil || *m.Estado != "mesclada" {
			return false
		}
	}
	_, err := os.Stat(a.L.Raizes.Arquivo(refs.US{Projeto: u.Projeto, Ref: u.Ref}))
	return err == nil
}

// Ciclo é uma passada: limpa as US candidatas e fecha as ondas que terminaram. Erro de uma US
// não impede as outras; só o erro do banco volta.
func (a *Auto) Ciclo(ctx context.Context) error {
	snap, err := a.L.St.Snapshot(ctx, true)
	if err != nil {
		return err
	}
	for _, c := range snap.Coordenadores {
		for _, o := range c.Ondas {
			for _, u := range o.US {
				if ctx.Err() != nil {
					return nil
				}
				if !a.candidata(u) {
					continue
				}
				if err := a.limparUS(ctx, u); err != nil {
					return err
				}
			}
		}
	}
	return a.encerrarOndas(ctx)
}

func (a *Auto) limparUS(ctx context.Context, u central.US) error {
	ref := refs.US{Projeto: u.Projeto, Ref: u.Ref}
	a.mu.Lock()
	if a.tentado == nil {
		a.tentado = map[string]*tentativa{}
	}
	t := a.tentado[u.Chave]
	if t == nil {
		t = &tentativa{}
		a.tentado[u.Chave] = t
	}
	espera := a.agora().Before(t.proxima)
	a.mu.Unlock()
	if espera {
		return nil
	}
	res, err := a.L.Limpar(ctx, nil, ref, Opcoes{Registrar: func(r Resultado) bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		nova := Assinatura(r)
		novo := nova != t.assinatura
		t.assinatura = nova
		return novo
	}})
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if res.Completa && res.Recusa == "" {
		delete(a.tentado, u.Chave)
		a.logf("limpeza de %s concluída", u.Chave)
		return nil
	}
	reintento := a.Reintento
	if reintento == 0 {
		reintento = ReintentoPadrao
	}
	t.proxima = a.agora().Add(reintento)
	a.logf("limpeza de %s incompleta (tenta de novo em %s): %s", u.Chave, reintento, resumoLinha(res))
	return nil
}

func resumoLinha(r Resultado) string {
	if r.Recusa != "" {
		return "recusada: " + r.Recusa
	}
	var ps []string
	for _, i := range r.Itens {
		if i.Estado == Preservado || i.Estado == Falhou {
			ps = append(ps, fmt.Sprintf("%s %s %s: %s", i.Estado, i.Tipo, i.Alvo, i.Motivo))
		}
	}
	return strings.Join(ps, "; ")
}

// encerrarOndas encerra a onda aberta (que não é só de plano) quando a regra de encerramento diz
// pronta — todas as US no limite, MRs mescladas ou fechadas, nenhuma pendência aberta — e
// nenhuma US dela ainda tem recursos por limpar. Antes de encerrar, remove o Docker com o rótulo da onda; se isso não termina, a onda segue aberta e tenta de novo.
func (a *Auto) encerrarOndas(ctx context.Context) error {
	snap, err := a.L.St.Snapshot(ctx, false)
	if err != nil {
		return err
	}
	for _, c := range snap.Coordenadores {
		for _, o := range c.Ondas {
			if ctx.Err() != nil {
				return nil
			}
			if o.AutorizacaoAte == "plano" {
				continue
			}
			if enc := central.EncerramentoDe(o); enc == nil || !enc.Pronta {
				continue
			}
			pendente := false
			for _, u := range o.US {
				if _, err := os.Stat(a.L.Raizes.Arquivo(refs.US{Projeto: u.Projeto, Ref: u.Ref})); err == nil {
					pendente = true
				}
			}
			if pendente {
				continue
			}
			if err := a.fecharOnda(ctx, c.Nome, o.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// fecharOnda limpa o Docker da onda e só então a encerra: com a limpeza incompleta (volume em
// uso, Docker fora do ar) a onda segue aberta e pronta, e o próximo ciclo, depois do reintento,
// tenta de novo sem depender de memória do daemon.
func (a *Auto) fecharOnda(ctx context.Context, coord string, id int64) error {
	chave := fmt.Sprintf("onda %d", id)
	a.mu.Lock()
	if a.tentado == nil {
		a.tentado = map[string]*tentativa{}
	}
	t := a.tentado[chave]
	if t == nil {
		t = &tentativa{}
		a.tentado[chave] = t
	}
	espera := a.agora().Before(t.proxima)
	a.mu.Unlock()
	if espera {
		return nil
	}
	res, err := a.L.LimparOnda(ctx, id, false, func(r Resultado) bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		nova := Assinatura(r)
		novo := nova != t.assinatura
		t.assinatura = nova
		return novo
	})
	if err != nil {
		return err
	}
	if !res.Completa {
		reintento := a.Reintento
		if reintento == 0 {
			reintento = ReintentoPadrao
		}
		a.mu.Lock()
		t.proxima = a.agora().Add(reintento)
		a.mu.Unlock()
		a.logf("Docker da onda %d incompleto, a onda segue aberta (tenta de novo em %s): %s", id, reintento, resumoLinha(res))
		return nil
	}
	a.mu.Lock()
	delete(a.tentado, chave)
	a.mu.Unlock()
	if _, err := a.L.St.EncerrarOnda(ctx, coord, id); err != nil {
		return err
	}
	a.logf("onda %d encerrada: todas as US prontas e limpas", id)
	return a.L.St.RegistrarExternosOnda(ctx, id, []central.EventoExterno{{Tipo: "limpar.onda", Detalhe: map[string]any{"acao": "encerrada pelo daemon", "motivo": "todas as US no limite, MRs mescladas ou fechadas, sem pendência aberta, sem recursos por limpar e com o Docker da onda limpo"}}})
}
