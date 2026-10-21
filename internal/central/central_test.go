package central

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

func novoStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	base := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	n := 0
	s.Now = func() time.Time { n++; return base.Add(time.Duration(n) * time.Second) }
	return s
}

func coord(t *testing.T, s *Store, nome, pane, projeto string) Coordenador {
	t.Helper()
	c, err := s.RegistrarCoord(context.Background(), Registro{Nome: nome, Pane: pane, Projeto: projeto})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func us(t *testing.T, s string) refs.US {
	t.Helper()
	u, err := refs.ParseUS(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func onda(t *testing.T, s *Store, c Coordenador, n NovaOnda) Onda {
	t.Helper()
	o, err := s.CriarOnda(context.Background(), c, n)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func codigo(err error) string {
	if err == nil {
		return ""
	}
	return output.AsError(err).Code
}

func eventos(t *testing.T, s *Store) []Evento {
	t.Helper()
	es, err := s.Diario(context.Background(), nil, FiltroDiario{})
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func TestOpenReabreSemRemigrar(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		s, err := Open(ctx, dir)
		if err != nil {
			t.Fatalf("abertura %d: %v", i, err)
		}
		var v int
		var modo string
		if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != Schema() {
			t.Fatalf("user_version = %d (queria %d), %v", v, Schema(), err)
		}
		if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&modo); err != nil || modo != "wal" {
			t.Fatalf("journal_mode = %q, %v", modo, err)
		}
		_ = s.Close()
	}
}

func TestDirPadrao(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"ORQ_STATE_DIR": "/x", "XDG_STATE_HOME": "/y", "HOME": "/h"}, "/x"},
		{map[string]string{"XDG_STATE_HOME": "/y", "HOME": "/h"}, "/y/orq"},
		{map[string]string{"XDG_STATE_HOME": "relativo", "HOME": "/h"}, "/h/.local/state/orq"},
		{map[string]string{"HOME": "/h"}, "/h/.local/state/orq"},
	}
	for _, c := range cases {
		got, err := DirPadrao(func(k string) string { return c.env[k] })
		if err != nil || got != c.want {
			t.Errorf("DirPadrao(%v) = %q, %v; queria %q", c.env, got, err, c.want)
		}
	}
}

func TestStatusTransicoes(t *testing.T) {
	cases := []struct {
		de, para string
		forcar   bool
		erro     string
	}{
		{"wait", "plan", false, ""},
		{"wait", "exec", false, ""},
		{"plan", "exec", false, ""},
		{"exec", "rev", false, ""},
		{"rev", "exec", false, ""},
		{"rev", "done", false, ""},
		{"exec", "done", false, ""},
		{"exec", "wait", false, ""},
		{"done", "done", false, ""},
		{"done", "rev", false, "transicao_invalida"},
		{"done", "wait", false, "transicao_invalida"},
		{"done", "exec", true, ""},
		{"exec", "pronto", false, "uso"},
	}
	for _, c := range cases {
		t.Run(c.de+"→"+c.para, func(t *testing.T) {
			s := novoStore(t)
			ctx := context.Background()
			co := coord(t, s, "a", "p1", "proj")
			onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
			if c.de != "wait" {
				if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "1"), MudancaStatus{Status: c.de}); err != nil {
					t.Fatal(err)
				}
			}
			antes := len(eventos(t, s))
			u, alterada, err := s.AtualizarStatus(ctx, &co, us(t, "1"), MudancaStatus{Status: c.para, Forcar: c.forcar})
			if got := codigo(err); got != c.erro {
				t.Fatalf("erro = %q (%v), queria %q", got, err, c.erro)
			}
			depois := eventos(t, s)
			if c.erro != "" {
				if len(depois) != antes {
					t.Fatal("erro não pode gerar evento")
				}
				return
			}
			if u.Status != c.para || alterada != (c.de != c.para) {
				t.Fatalf("status = %s, alterada = %v", u.Status, alterada)
			}
			if want := antes; alterada {
				if len(depois) != want+1 || depois[len(depois)-1].Tipo != "us.status" {
					t.Fatalf("eventos = %d, queria %d com us.status", len(depois), want+1)
				}
				var d map[string]any
				_ = json.Unmarshal(depois[len(depois)-1].Detalhe, &d)
				if d["de"] != c.de || d["para"] != c.para || (c.forcar && d["forcado"] != true) {
					t.Fatalf("detalhe = %v", d)
				}
			} else if len(depois) != antes {
				t.Fatal("sem mudança não gera evento")
			}
		})
	}
}

func TestStatusGuardaPaneEPapel(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "a", "p1", "proj")
	onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	pane, papel, rodada := "wB:p3", "implementacao", 2
	if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "1"), MudancaStatus{Status: "exec", Pane: &pane, Papel: &papel, Rodada: &rodada}); err != nil {
		t.Fatal(err)
	}
	u, alterada, err := s.AtualizarStatus(ctx, &co, us(t, "1"), MudancaStatus{Status: "rev"})
	if err != nil || !alterada {
		t.Fatal(err)
	}
	if *u.PaneID != pane || *u.Papel != papel || *u.Rodada != 2 {
		t.Fatalf("us = %+v", u)
	}
	// Só a rodada muda: grava e gera evento.
	r3 := 3
	if _, alterada, _ := s.AtualizarStatus(ctx, &co, us(t, "1"), MudancaStatus{Status: "rev", Rodada: &r3}); !alterada {
		t.Fatal("rodada nova devia contar como mudança")
	}
	ruim := "gestor"
	if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "1"), MudancaStatus{Status: "rev", Papel: &ruim}); codigo(err) != "uso" {
		t.Fatalf("papel inválido: %v", err)
	}
	zero := 0
	if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "1"), MudancaStatus{Status: "rev", Rodada: &zero}); codigo(err) != "uso" {
		t.Fatalf("rodada 0: %v", err)
	}
}

// ondaFixa monta uma onda em memória para testar a escolha da próxima US sem banco.
func ondaFixa(ordem, ate string, status []string, deps map[int][]int) Onda {
	o := Onda{ID: 7, AutorizacaoAte: ate, Ordem: ordem}
	for i, st := range status {
		ref := 215 + i
		u := US{ID: refs.Exibir(ref), Chave: refs.US{Projeto: "p", Ref: ref}.Chave(), Ref: ref, Posicao: i + 1, Status: st, DependeDe: []string{}}
		for _, d := range deps[ref] {
			u.DependeDe = append(u.DependeDe, refs.US{Projeto: "p", Ref: d}.Chave())
		}
		o.US = append(o.US, u)
	}
	return o
}

func TestProxima(t *testing.T) {
	dep := map[int][]int{216: {215}}
	cases := []struct {
		nome       string
		ordem, ate string
		status     []string
		deps       map[int][]int
		encerrada  bool
		want       string // chave da próxima, "" = nada
		elegiveis  int
		motivo     string // trecho esperado do motivo quando nada é elegível
		bloqueadas int
	}{
		{nome: "estrita começa pela primeira", ordem: "estrita", ate: "mr-pronta", status: []string{"wait", "wait", "wait", "wait"}, deps: dep, want: "p/215", elegiveis: 1},
		{nome: "estrita espera a anterior", ordem: "estrita", ate: "mr-pronta", status: []string{"exec", "wait", "wait", "wait"}, deps: dep, motivo: "ordem estrita: TG-215 ainda em exec"},
		{nome: "estrita espera rev", ordem: "estrita", ate: "mr-pronta", status: []string{"rev", "wait", "wait", "wait"}, deps: dep, motivo: "TG-215 ainda em rev"},
		{nome: "estrita anda com done", ordem: "estrita", ate: "mr-pronta", status: []string{"done", "wait", "wait", "wait"}, deps: dep, want: "p/216", elegiveis: 1},
		{nome: "estrita pula as feitas", ordem: "estrita", ate: "mr-pronta", status: []string{"done", "done", "wait", "wait"}, deps: dep, want: "p/217", elegiveis: 1},
		{nome: "estrita concluída", ordem: "estrita", ate: "mr-pronta", status: []string{"done", "done", "done", "done"}, motivo: "onda concluída até mr-pronta"},
		{nome: "livre: dependência em wait segura a dependente", ordem: "livre", ate: "merge", status: []string{"wait", "wait"}, deps: dep, want: "p/215", elegiveis: 1, bloqueadas: 1},
		{nome: "livre libera as independentes", ordem: "livre", ate: "mr-pronta", status: []string{"exec", "wait", "wait", "wait"}, deps: dep, want: "p/217", elegiveis: 2, bloqueadas: 1},
		{nome: "livre dependência em rev ainda bloqueia", ordem: "livre", ate: "mr-pronta", status: []string{"rev", "wait"}, deps: dep, motivo: "nenhuma US em wait está liberada", bloqueadas: 1},
		{nome: "livre dependência done libera", ordem: "livre", ate: "mr-pronta", status: []string{"done", "wait"}, deps: dep, want: "p/216", elegiveis: 1},
		{nome: "livre nada em wait", ordem: "livre", ate: "mr-pronta", status: []string{"exec", "rev", "done"}, motivo: "nenhuma US em wait; em andamento: TG-215 em exec, TG-216 em rev"},
		{nome: "livre todas elegíveis na ordem", ordem: "livre", ate: "plano", status: []string{"wait", "wait", "wait"}, want: "p/215", elegiveis: 3},
		{nome: "encerrada", ordem: "livre", ate: "mr-pronta", status: []string{"wait"}, encerrada: true, motivo: "onda encerrada"},
	}
	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			o := ondaFixa(c.ordem, c.ate, c.status, c.deps)
			if c.encerrada {
				e := "2026-10-11T00:00:00Z"
				o.EncerradaEm = &e
			}
			p := proxima(o)
			got := ""
			if p.US != nil {
				got = p.US.Chave
			}
			if got != c.want || len(p.Elegiveis) != c.elegiveis || len(p.Bloqueadas) != c.bloqueadas {
				t.Fatalf("proxima = %q elegíveis %v bloqueadas %v motivo %q", got, p.Elegiveis, p.Bloqueadas, p.Motivo)
			}
			if c.want == "" && !strings.Contains(p.Motivo, c.motivo) {
				t.Fatalf("motivo = %q, queria %q", p.Motivo, c.motivo)
			}
			if c.want != "" && p.Motivo != "" {
				t.Fatalf("motivo com US elegível: %q", p.Motivo)
			}
			if p.AutorizacaoAte != c.ate || p.Limite != Limites[c.ate] || p.Limite == "" {
				t.Fatalf("autorização = %q limite %q", p.AutorizacaoAte, p.Limite)
			}
		})
	}
}

// A regra de encerramento sobre a onda em memória: toda US no limite, toda MR mesclada ou
// fechada (menos na onda plano) e nenhuma pendência aberta.
func TestEncerramento(t *testing.T) {
	mr := func(url, estado string) MR {
		m := MR{URL: url}
		if estado != "" {
			m.Estado = &estado
		}
		return m
	}
	pend := func(aberta bool) []Pendencia { return []Pendencia{{ID: "TG-215#P1", Numero: 1, Aberta: aberta}} }
	cases := []struct {
		nome      string
		ate       string
		status    []string
		mrs       []MR
		pends     []Pendencia
		encerrada bool
		nula      bool
		pronta    bool
		faltam    []string // trechos, um por item de Faltam
		passo     string
	}{
		{nome: "em curso", ate: "mr-pronta", status: []string{"done", "rev"}, nula: true},
		{nome: "com US em wait", ate: "mr-pronta", status: []string{"done", "wait"}, nula: true},
		{nome: "encerrada", ate: "mr-pronta", status: []string{"done"}, encerrada: true, nula: true},
		{nome: "no limite com MR aberta", ate: "mr-pronta", status: []string{"done", "done"}, mrs: []MR{mr("u/32", "pronta"), mr("u/31", "mesclada")},
			faltam: []string{"p/215: MR u/32 pronta (falta merge ou fechamento)"}, passo: "falta concluir"},
		{nome: "MR sem estado não conta como mesclada", ate: "merge", status: []string{"done"}, mrs: []MR{mr("u/33", "")},
			faltam: []string{"p/215: MR u/33 sem estado conhecido (falta conferir)"}, passo: "falta concluir"},
		{nome: "com pendência aberta", ate: "mr-pronta", status: []string{"done"}, mrs: []MR{mr("u/29", "mesclada")}, pends: pend(true),
			faltam: []string{"p/215#P1: pendência aberta"}, passo: "falta concluir"},
		{nome: "pronta para encerrar", ate: "mr-pronta", status: []string{"done", "done"}, mrs: []MR{mr("u/29", "mesclada"), mr("u/31", "fechada")}, pends: pend(false),
			pronta: true, passo: "encerre a onda: orq onda encerrar 7"},
		{nome: "pronta sem MR", ate: "merge", status: []string{"done"}, pronta: true, passo: "orq onda encerrar 7"},
		{nome: "plano não olha as MRs", ate: "plano", status: []string{"done"}, mrs: []MR{mr("u/34", "draft"), mr("u/35", "")},
			pronta: true, passo: "e crie a onda de implementação com as mesmas US"},
		{nome: "plano com pendência aberta", ate: "plano", status: []string{"done"}, pends: pend(true), faltam: []string{"p/215#P1: pendência aberta"}},
	}
	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			o := ondaFixa("livre", c.ate, c.status, nil)
			o.US[0].MRs, o.US[0].Pendencias = c.mrs, c.pends
			if c.encerrada {
				e := "2026-10-11T00:00:00Z"
				o.EncerradaEm = &e
			}
			e := EncerramentoDe(o)
			if c.nula {
				if e != nil {
					t.Fatalf("encerramento = %+v, queria nulo", e)
				}
				return
			}
			if e == nil || e.Pronta != c.pronta || len(e.Faltam) != len(c.faltam) || !strings.Contains(e.Passo, c.passo) {
				t.Fatalf("encerramento = %+v", e)
			}
			for i, f := range c.faltam {
				if !strings.Contains(e.Faltam[i], f) {
					t.Errorf("faltam[%d] = %q, queria %q", i, e.Faltam[i], f)
				}
			}
			if c.pronta != (e.Comando != nil) || (e.Comando != nil && *e.Comando != "orq onda encerrar 7") {
				t.Errorf("comando = %v", e.Comando)
			}
			// orq onda proxima traz a mesma decisão quando a onda chega ao limite.
			if p := proxima(o); p.Encerramento == nil || p.Encerramento.Pronta != e.Pronta || len(p.Encerramento.Faltam) != len(e.Faltam) {
				t.Errorf("proxima.encerramento = %+v", p.Encerramento)
			}
		})
	}
	if p := proxima(ondaFixa("livre", "mr-pronta", []string{"done", "exec"}, nil)); p.Encerramento != nil {
		t.Errorf("onda em curso com encerramento: %+v", p.Encerramento)
	}
}

// O estado da MR vem do cache do daemon: sem consulta, a onda no limite ainda não está pronta.
func TestEncerramentoNoBanco(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "colab", "wB:p1", "colaboradados")
	o := onda(t, s, co, NovaOnda{Titulo: "noite", US: []refs.US{us(t, "215")}})
	url := "https://gitlab.basis.com.br/basis/x/-/merge_requests/32"
	if _, _, err := s.AdicionarMR(ctx, &co, us(t, "215"), url); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "215"), MudancaStatus{Status: "done"}); err != nil {
		t.Fatal(err)
	}
	passo := func() *Encerramento {
		t.Helper()
		p, err := s.ProximaUS(ctx, o.ID)
		if err != nil || p.Encerramento == nil {
			t.Fatalf("proxima = %+v, %v", p, err)
		}
		return p.Encerramento
	}
	if e := passo(); e.Pronta || len(e.Faltam) != 1 || !strings.Contains(e.Faltam[0], "falta conferir") {
		t.Fatalf("sem estado: %+v", e)
	}
	if err := s.GravarMR(ctx, url, ValorMR{Estado: "pronta"}, nil); err != nil {
		t.Fatal(err)
	}
	if e := passo(); e.Pronta || !strings.Contains(e.Faltam[0], "falta merge") {
		t.Fatalf("pronta: %+v", e)
	}
	if err := s.GravarMR(ctx, url, ValorMR{Estado: "mesclada"}, nil); err != nil {
		t.Fatal(err)
	}
	if e := passo(); !e.Pronta || *e.Comando != "orq onda encerrar 1" {
		t.Fatalf("mesclada: %+v", e)
	}
	// Só sugere: a onda continua aberta.
	if v, _ := s.VerOnda(ctx, o.ID); v.EncerradaEm != nil {
		t.Fatal("proxima encerrou a onda")
	}
}

// A onda do colaboradados ponta a ponta no banco: TG-215→218, ordem estrita, 216 depende de 215.
func TestProximaNoBanco(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "colab", "wB:p1", "colaboradados")
	o := onda(t, s, co, NovaOnda{Titulo: "noite", Ordem: "estrita", US: []refs.US{us(t, "215"), us(t, "216"), us(t, "217"), us(t, "218")},
		Deps: [][2]refs.US{{us(t, "216"), us(t, "215")}}})
	seq := []string{}
	for i := 0; i < 5; i++ {
		p, err := s.ProximaUS(ctx, o.ID)
		if err != nil {
			t.Fatal(err)
		}
		if p.US == nil {
			if p.Motivo != "onda concluída até mr-pronta" {
				t.Fatalf("motivo = %q", p.Motivo)
			}
			break
		}
		seq = append(seq, p.US.ID)
		for _, st := range []string{"plan", "exec", "rev", "exec", "rev", "done"} {
			if _, _, err := s.AtualizarStatus(ctx, &co, us(t, p.US.ID), MudancaStatus{Status: st}); err != nil {
				t.Fatal(err)
			}
			if st != "done" {
				if q, _ := s.ProximaUS(ctx, o.ID); q.US != nil {
					t.Fatalf("com %s em %s, a próxima devia esperar; veio %s", p.US.ID, st, q.US.ID)
				}
			}
		}
	}
	if strings.Join(seq, ",") != "TG-215,TG-216,TG-217,TG-218" {
		t.Fatalf("sequência = %v", seq)
	}
}

func TestCriarOndaValidacoes(t *testing.T) {
	cases := []struct {
		nome string
		n    NovaOnda
		erro string
	}{
		{"sem US", NovaOnda{Titulo: "o"}, "uso"},
		{"sem título", NovaOnda{US: []refs.US{{Ref: 1}}}, "uso"},
		{"ate inválido", NovaOnda{Titulo: "o", Ate: "deploy", US: []refs.US{{Ref: 1}}}, "uso"},
		{"ordem inválida", NovaOnda{Titulo: "o", Ordem: "aleatoria", US: []refs.US{{Ref: 1}}}, "uso"},
		{"repetida", NovaOnda{Titulo: "o", US: []refs.US{{Ref: 1}, {Projeto: "proj", Ref: 1}}}, "uso"},
		{"dependência fora", NovaOnda{Titulo: "o", US: []refs.US{{Ref: 1}}, Deps: [][2]refs.US{{{Ref: 1}, {Ref: 9}}}}, "uso"},
		{"de si mesma", NovaOnda{Titulo: "o", US: []refs.US{{Ref: 1}}, Deps: [][2]refs.US{{{Ref: 1}, {Ref: 1}}}}, "uso"},
		{"ciclo", NovaOnda{Titulo: "o", US: []refs.US{{Ref: 1}, {Ref: 2}, {Ref: 3}},
			Deps: [][2]refs.US{{{Ref: 1}, {Ref: 2}}, {{Ref: 2}, {Ref: 3}}, {{Ref: 3}, {Ref: 1}}}}, "uso"},
		{"estrita para trás", NovaOnda{Titulo: "o", Ordem: "estrita", US: []refs.US{{Ref: 1}, {Ref: 2}}, Deps: [][2]refs.US{{{Ref: 1}, {Ref: 2}}}}, "uso"},
		{"livre para trás vale", NovaOnda{Titulo: "o", US: []refs.US{{Ref: 1}, {Ref: 2}}, Deps: [][2]refs.US{{{Ref: 1}, {Ref: 2}}}}, ""},
		{"projeto inválido", NovaOnda{Titulo: "o", Projeto: "Com Espaço", US: []refs.US{{Ref: 1}}}, "uso"},
	}
	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			s := novoStore(t)
			co := coord(t, s, "a", "p1", "proj")
			_, err := s.CriarOnda(context.Background(), co, c.n)
			if got := codigo(err); got != c.erro {
				t.Fatalf("erro = %q (%v), queria %q", got, err, c.erro)
			}
		})
	}
}

func TestCriarOndaSemProjeto(t *testing.T) {
	s := novoStore(t)
	co := coord(t, s, "a", "p1", "")
	if _, err := s.CriarOnda(context.Background(), co, NovaOnda{Titulo: "o", US: []refs.US{{Ref: 1}}}); codigo(err) != "uso" {
		t.Fatalf("sem projeto: %v", err)
	}
	o := onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{{Projeto: "x", Ref: 1}}})
	if o.US[0].Chave != "x/1" || o.AutorizacaoAte != "mr-pronta" || o.Ordem != "livre" {
		t.Fatalf("onda = %+v", o)
	}
}

func TestUSEmOutraOnda(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "a", "p1", "proj")
	o1 := onda(t, s, co, NovaOnda{Titulo: "um", US: []refs.US{us(t, "1"), us(t, "2")}, Deps: [][2]refs.US{{us(t, "2"), us(t, "1")}}})
	if _, err := s.CriarOnda(ctx, co, NovaOnda{Titulo: "dois", US: []refs.US{us(t, "2")}}); codigo(err) != "us_em_outra_onda" {
		t.Fatalf("US em onda aberta: %v", err)
	}
	if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "2"), MudancaStatus{Status: "exec"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EncerrarOnda(ctx, co.Nome, o1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EncerrarOnda(ctx, co.Nome, o1.ID); codigo(err) != "onda_encerrada" {
		t.Fatalf("encerrar duas vezes: %v", err)
	}
	o2 := onda(t, s, co, NovaOnda{Titulo: "dois", US: []refs.US{us(t, "2")}})
	if len(o2.US) != 1 || o2.US[0].Status != "exec" || len(o2.US[0].DependeDe) != 0 || o2.US[0].Posicao != 1 {
		t.Fatalf("US movida = %+v", o2.US)
	}
	v1, _ := s.VerOnda(ctx, o1.ID)
	if len(v1.US) != 1 {
		t.Fatalf("onda antiga ficou com %d US", len(v1.US))
	}
}

func TestPendenciaIDs(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "a", "p1", "proj")
	onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{us(t, "312"), us(t, "313")}})
	abrir := func(ref string) string {
		t.Helper()
		p, err := s.AbrirPendencia(ctx, &co, us(t, ref), "duvida", "texto de "+ref, "wB:p9")
		if err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	got := []string{abrir("312"), abrir("312"), abrir("313"), abrir("312")}
	if strings.Join(got, ",") != "TG-312#P1,TG-312#P2,TG-313#P1,TG-312#P3" {
		t.Fatalf("ids = %v", got)
	}
	r, err := s.ResolverPendencia(ctx, &co, refs.Pend{US: us(t, "312"), Numero: 2}, "sim")
	if err != nil || r.Aberta || *r.Resposta != "sim" || r.ResolvidaEm == nil {
		t.Fatalf("resolver = %+v, %v", r, err)
	}
	// Número resolvido não volta: a próxima é P4, não P2.
	if id := abrir("312"); id != "TG-312#P4" {
		t.Fatalf("depois de resolver P2, veio %s", id)
	}
	if _, err := s.ResolverPendencia(ctx, &co, refs.Pend{US: us(t, "312"), Numero: 2}, ""); codigo(err) != "pendencia_resolvida" {
		t.Fatalf("resolver de novo: %v", err)
	}
	if _, err := s.ResolverPendencia(ctx, &co, refs.Pend{US: us(t, "312"), Numero: 9}, ""); codigo(err) != "nao_encontrado" {
		t.Fatalf("P9: %v", err)
	}
	for _, c := range []struct{ tipo, texto string }{{"pedido", "x"}, {"duvida", "  "}} {
		if _, err := s.AbrirPendencia(ctx, &co, us(t, "312"), c.tipo, c.texto, ""); codigo(err) != "uso" {
			t.Fatalf("%+v: %v", c, err)
		}
	}
	abertas, _ := s.ListarPendencias(ctx, FiltroPendencias{Abertas: true})
	todas, _ := s.ListarPendencias(ctx, FiltroPendencias{})
	if len(abertas) != 4 || len(todas) != 5 {
		t.Fatalf("abertas %d, todas %d", len(abertas), len(todas))
	}
}

func TestResolverRefAmbigua(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	a := coord(t, s, "a", "pa", "infra-2025")
	b := coord(t, s, "b", "pb", "convey")
	c := coord(t, s, "c", "pc", "convey")
	semPadrao := coord(t, s, "d", "pd", "")
	onda(t, s, a, NovaOnda{Titulo: "a", US: []refs.US{us(t, "infra-2025/312"), us(t, "infra-2025/400")}})
	onda(t, s, b, NovaOnda{Titulo: "b", US: []refs.US{us(t, "convey/312")}})
	casos := []struct {
		nome  string
		coord *Coordenador
		ref   string
		want  string
		erro  string
	}{
		{"projeto explícito", nil, "convey/312", "convey/312", ""},
		{"ref única sem coordenador", nil, "TG-400", "infra-2025/400", ""},
		{"ambígua sem coordenador", nil, "TG-312", "", "ref_ambigua"},
		{"onda aberta do coordenador decide", &a, "TG-312", "infra-2025/312", ""},
		{"onda aberta do outro coordenador", &b, "312", "convey/312", ""},
		{"projeto padrão decide", &c, "TG-312", "convey/312", ""},
		{"coordenador sem onda nem padrão", &semPadrao, "TG-312", "", "ref_ambigua"},
		{"inexistente", &a, "TG-999", "", "nao_encontrado"},
		{"projeto explícito inexistente", nil, "convey/400", "", "nao_encontrado"},
	}
	for _, x := range casos {
		t.Run(x.nome, func(t *testing.T) {
			u, _, err := s.AtualizarStatus(ctx, x.coord, us(t, x.ref), MudancaStatus{Status: "plan"})
			if got := codigo(err); got != x.erro {
				t.Fatalf("erro = %q (%v), queria %q", got, err, x.erro)
			}
			if x.erro == "" && u.Chave != x.want {
				t.Fatalf("resolveu %s, queria %s", u.Chave, x.want)
			}
			if x.erro == "ref_ambigua" && !strings.Contains(err.Error(), "convey/312") {
				t.Fatalf("a causa devia listar os candidatos: %v", err)
			}
		})
	}
}

func TestMR(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "a", "p1", "proj")
	onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	url := "https://gitlab.basis.com.br/basis/iac/x/-/merge_requests/4"
	m, criada, err := s.AdicionarMR(ctx, &co, us(t, "1"), url+"/diffs")
	if err != nil || !criada || m.URL != url || !m.IaC || m.IID != 4 || m.Estado != nil {
		t.Fatalf("add = %+v, %v, %v", m, criada, err)
	}
	antes := len(eventos(t, s))
	if _, criada, err := s.AdicionarMR(ctx, &co, us(t, "1"), url); err != nil || criada {
		t.Fatalf("add repetido = %v, %v", criada, err)
	}
	if len(eventos(t, s)) != antes {
		t.Fatal("add repetido não gera evento")
	}
	if _, _, err := s.AdicionarMR(ctx, &co, us(t, "1"), "https://x/issues/1"); codigo(err) != "uso" {
		t.Fatalf("URL inválida: %v", err)
	}
	if _, err := s.RemoverMR(ctx, &co, us(t, "1"), url+"#note"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoverMR(ctx, &co, us(t, "1"), url); codigo(err) != "nao_encontrado" {
		t.Fatalf("rm repetido: %v", err)
	}
}

func TestCoordenador(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	coord(t, s, "a", "wB:p1", "proj")
	b := coord(t, s, "b", "wB:p1", "")
	if *b.PaneID != "wB:p1" {
		t.Fatalf("b = %+v", b)
	}
	a, err := s.ResolverCoord(ctx, Resolucao{Flag: "a"})
	if err != nil || a.PaneID != nil || *a.ProjetoPadrao != "proj" {
		t.Fatalf("a perdeu o pane mas guarda o projeto: %+v, %v", a, err)
	}
	// Registrar de novo sem projeto mantém o que estava.
	a = coord(t, s, "a", "wB:p2", "")
	if *a.ProjetoPadrao != "proj" || *a.PaneID != "wB:p2" {
		t.Fatalf("a = %+v", a)
	}
	casos := []struct {
		r    Resolucao
		want string
		erro string
	}{
		{Resolucao{Flag: "a", EnvCoord: "b", EnvPane: "wB:p1"}, "a", ""},
		{Resolucao{EnvCoord: "b", EnvPane: "wB:p2"}, "b", ""},
		{Resolucao{EnvPane: "wB:p2"}, "a", ""},
		{Resolucao{EnvPane: "wB:p9"}, "", "coord_nao_resolvido"},
		{Resolucao{}, "", "coord_nao_resolvido"},
		{Resolucao{Flag: "zz"}, "", "nao_encontrado"},
	}
	for _, c := range casos {
		got, err := s.ResolverCoord(ctx, c.r)
		if codigo(err) != c.erro || (c.erro == "" && got.Nome != c.want) {
			t.Errorf("ResolverCoord(%+v) = %s, %v", c.r, got.Nome, err)
		}
	}
	if _, err := s.RegistrarCoord(ctx, Registro{Nome: " "}); codigo(err) != "uso" {
		t.Fatalf("nome vazio: %v", err)
	}
	es := eventos(t, s)
	var d map[string]any
	_ = json.Unmarshal(es[2].Detalhe, &d)
	if es[2].Tipo != "coord.registrar" || d["pane_liberado_de"] != "a" {
		t.Fatalf("evento = %s %s", es[2].Tipo, es[2].Detalhe)
	}
}

func TestTodaEscritaGeraEvento(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "a", "p1", "proj")
	o := onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	_, _, _ = s.AtualizarStatus(ctx, &co, us(t, "1"), MudancaStatus{Status: "exec"})
	_, _, _ = s.AdicionarMR(ctx, &co, us(t, "1"), "https://github.com/BasisTI/orq/pull/1")
	_, _ = s.RemoverMR(ctx, &co, us(t, "1"), "https://github.com/BasisTI/orq/pull/1")
	_, _ = s.AbrirPendencia(ctx, &co, us(t, "1"), "autorizacao", "pode?", "")
	_, _ = s.ResolverPendencia(ctx, nil, refs.Pend{US: us(t, "1"), Numero: 1}, "")
	_, _ = s.EncerrarOnda(ctx, "", o.ID)
	var tipos []string
	for _, e := range eventos(t, s) {
		tipos = append(tipos, e.Tipo)
		if e.Coordenador == nil || *e.Coordenador != "a" {
			t.Fatalf("evento %s sem coordenador", e.Tipo)
		}
	}
	want := "coord.registrar,projeto.tom,onda.criar,us.status,mr.add,mr.rm,pend.add,pend.resolver,onda.encerrar"
	if strings.Join(tipos, ",") != want {
		t.Fatalf("tipos = %v", tipos)
	}
	porUS, _ := s.Diario(ctx, nil, FiltroDiario{US: &refs.US{Ref: 1}, Limite: 2})
	if len(porUS) != 2 || porUS[0].Tipo != "pend.add" || porUS[1].Tipo != "pend.resolver" {
		t.Fatalf("diário da US = %+v", porUS)
	}
}

func TestSnapshot(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	a := coord(t, s, "a", "p1", "proj")
	coord(t, s, "b", "p2", "")
	o := onda(t, s, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	onda(t, s, a, NovaOnda{Titulo: "velha", US: []refs.US{us(t, "2")}})
	_, _ = s.AbrirPendencia(ctx, &a, us(t, "1"), "decisao", "qual?", "")
	if _, err := s.EncerrarOnda(ctx, "a", o.ID+1); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(snap)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["versao"].(float64) != 1 || len(snap.Coordenadores) != 2 || len(snap.Coordenadores[0].Ondas) != 1 || len(snap.PendenciasAbertas) != 1 {
		t.Fatalf("snapshot = %s", b)
	}
	if snap.Coordenadores[1].Ondas == nil {
		t.Fatal("lista vazia tem de sair [] e não null")
	}
	// Toda chave do contrato (docs/snapshot.md) aparece, mesmo com valor nulo.
	chaves := func(v any) []string {
		var ks []string
		for k := range v.(map[string]any) {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	coordJ := m["coordenadores"].([]any)[0].(map[string]any)
	ondaJ := coordJ["ondas"].([]any)[0].(map[string]any)
	usJ := ondaJ["us"].([]any)[0].(map[string]any)
	pendJ := usJ["pendencias"].([]any)[0].(map[string]any)
	contrato := []struct {
		nome string
		v    any
		want string
	}{
		{"raiz", m, "coordenadores,fontes,gerado_em,pendencias_abertas,projetos,versao"},
		{"fontes", m["fontes"], "herdr,mr,taiga"},
		{"fonte", m["fontes"].(map[string]any)["mr"], "atualizado_em,consultado_em,erro"},
		{"coordenador", coordJ, "herdr,nome,ondas,pane_id,projeto_padrao,registrado_em,workspace_id"},
		{"herdr", coordJ["herdr"], "agente,atualizado_em,erro,nome,status,workspace_id"},
		{"onda", ondaJ, "autorizacao_ate,coordenador,criada_em,encerrada_em,id,ordem,titulo,us"},
		{"us", usJ, "atualizado_em,chave,depende_de,herdr,id,mrs,onda_id,pane_id,papel,pendencias,posicao,projeto,ref,rodada,status,taiga"},
		{"taiga", usJ["taiga"], "atualizado_em,erro,status,titulo,url"},
		{"pendencia", pendJ, "aberta,aberta_em,arquivo,arquivo_alterado,autor,avisada_em,chave,coordenador,estado,id,numero,onda_id,opcoes,pane_origem,resolvida_em,respondida_em,respondida_por,resposta,resposta_opcao,resposta_versao,sem_coordenador,slug,tem_detalhe,texto,tipo,us"},
	}
	for _, c := range contrato {
		if got := strings.Join(chaves(c.v), ","); got != c.want {
			t.Errorf("chaves de %s = %s, queria %s", c.nome, got, c.want)
		}
	}
	_, _, _ = s.AdicionarMR(ctx, &a, us(t, "1"), "https://github.com/BasisTI/orq/pull/1")
	comMR, _ := s.Snapshot(ctx, false)
	mb, _ := json.Marshal(comMR.Coordenadores[0].Ondas[0].US[0].MRs[0])
	var mm map[string]any
	_ = json.Unmarshal(mb, &mm)
	if got := strings.Join(chaves(mm), ","); got != "adicionada_em,aprovacao,atualizado_em,erro,estado,iac,iid,pipeline,projeto_path,situacao_em,situacao_erro,url" {
		t.Errorf("chaves de mr = %s", got)
	}
	todas, _ := s.Snapshot(ctx, true)
	if len(todas.Coordenadores[0].Ondas) != 2 {
		t.Fatal("--incluir-encerradas devia trazer a onda encerrada")
	}
}

// done quer dizer "chegou ao limite da onda": trocar de onda com outro limite não pode herdar
// essa conclusão, senão a onda nova se dá por concluída sem implementar nada.
func TestMoverUSEntreLimites(t *testing.T) {
	cases := []struct {
		de, para string
		status   string
		want     string
	}{
		{"plano", "mr-pronta", "done", "wait"},
		{"mr-pronta", "merge", "done", "wait"},
		{"plano", "mr-pronta", "exec", "wait"},
		{"mr-pronta", "mr-pronta", "done", "done"},
		{"merge", "merge", "rev", "rev"},
		{"plano", "merge", "wait", "wait"},
	}
	for _, c := range cases {
		t.Run(c.de+"→"+c.para+"/"+c.status, func(t *testing.T) {
			s := novoStore(t)
			ctx := context.Background()
			co := coord(t, s, "qa", "", "orq")
			o1 := onda(t, s, co, NovaOnda{Titulo: "antes", Ate: c.de, US: []refs.US{us(t, "3")}})
			if c.status != "wait" {
				if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "3"), MudancaStatus{Status: c.status}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.EncerrarOnda(ctx, co.Nome, o1.ID); err != nil {
				t.Fatal(err)
			}
			antes := len(eventos(t, s))
			o2 := onda(t, s, co, NovaOnda{Titulo: "depois", Ate: c.para, US: []refs.US{us(t, "3")}})
			if got := o2.US[0].Status; got != c.want {
				t.Fatalf("status na onda nova = %s, queria %s", got, c.want)
			}
			es := eventos(t, s)[antes:]
			reiniciou := c.want != c.status
			if len(es) != 1+map[bool]int{true: 1}[reiniciou] || es[0].Tipo != "onda.criar" {
				t.Fatalf("eventos = %+v", es)
			}
			p, err := s.ProximaUS(ctx, o2.ID)
			if err != nil {
				t.Fatal(err)
			}
			if reiniciou {
				var d map[string]any
				_ = json.Unmarshal(es[1].Detalhe, &d)
				if es[1].Tipo != "us.status" || d["de"] != c.status || d["para"] != "wait" || !strings.Contains(d["motivo"].(string), "até "+c.de) {
					t.Fatalf("evento = %s %s", es[1].Tipo, es[1].Detalhe)
				}
				if p.US == nil || p.US.Chave != "orq/3" {
					t.Fatalf("a onda nova devia despachar orq/3: %+v", p)
				}
			}
		})
	}
}

func TestOcupadoSeparaRecusaDeTimeout(t *testing.T) {
	busy := errors.New("database is locked (5) (SQLITE_BUSY)")
	imediato := output.AsError(ocupado(busy, time.Now()))
	esgotado := output.AsError(ocupado(busy, time.Now().Add(-busyTimeout)))
	if imediato.Code != "banco_ocupado" || !strings.Contains(imediato.Recovery, "na hora") || !strings.Contains(imediato.Cause, "depois de 0 ms") {
		t.Fatalf("imediato = %+v", imediato)
	}
	if esgotado.Code != "banco_ocupado" || !strings.Contains(esgotado.Recovery, "mais de 15 s") {
		t.Fatalf("esgotado = %+v", esgotado)
	}
	if outro := errors.New("x"); ocupado(outro, time.Now()) != outro {
		t.Fatal("erro que não é SQLITE_BUSY passa direto")
	}
}
