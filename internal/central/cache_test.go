package central

import (
	"context"
	"testing"

	"github.com/BasisTI/orq/internal/refs"
)

func TestCacheGuardaUltimoValorBom(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	if err := s.GravarCache(ctx, FonteTaiga, "orq/4", ValorTaiga{Titulo: "t", Status: "New", URL: "u"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.GravarCache(ctx, FonteTaiga, "orq/4", nil, &Erro{Codigo: "rede", Mensagem: "caiu"}); err != nil {
		t.Fatal(err)
	}
	cs, err := s.LerCache(ctx, FonteTaiga)
	if err != nil {
		t.Fatal(err)
	}
	c := cs["orq/4"]
	if string(c.Valor) != `{"titulo":"t","status":"New","url":"u"}` || c.Erro == nil || c.Erro.Codigo != "rede" || c.ValorEm == nil || *c.ValorEm == c.ConsultadoEm {
		t.Fatalf("cache = %+v %+v", c, c.Erro)
	}
	// Consulta boa de novo apaga o erro.
	_ = s.GravarCache(ctx, FonteTaiga, "orq/4", ValorTaiga{Titulo: "t2"}, nil)
	cs, _ = s.LerCache(ctx, FonteTaiga)
	if cs["orq/4"].Erro != nil || string(cs["orq/4"].Valor) != `{"titulo":"t2","status":"","url":""}` {
		t.Fatalf("cache = %+v", cs["orq/4"])
	}
}

func TestGravarMRMudaEstadoComEvento(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	a := coord(t, s, "a", "p1", "proj")
	onda(t, s, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	url := "https://github.com/BasisTI/orq/pull/1"
	if _, _, err := s.AdicionarMR(ctx, &a, us(t, "1"), url); err != nil {
		t.Fatal(err)
	}
	antes := len(eventos(t, s))
	for _, e := range []string{"aberta", "aberta", "mesclada"} {
		if err := s.GravarMR(ctx, url, ValorMR{Estado: e}, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Erro depois não apaga o estado.
	if err := s.GravarMR(ctx, url, ValorMR{}, &Erro{Codigo: "autenticacao", Mensagem: "401"}); err != nil {
		t.Fatal(err)
	}
	es := eventos(t, s)[antes:]
	if len(es) != 2 || es[0].Tipo != "mr.estado" || string(es[1].Detalhe) != `{"de":"aberta","para":"mesclada","url":"`+url+`"}` {
		t.Fatalf("eventos = %+v", es)
	}
	snap, _ := s.Snapshot(ctx, false)
	m := snap.Coordenadores[0].Ondas[0].US[0].MRs[0]
	if m.Estado == nil || *m.Estado != "mesclada" || m.Erro == nil || m.Erro.Codigo != "autenticacao" || m.AtualizadoEm == nil {
		t.Fatalf("mr = %+v", m)
	}
}

func TestSnapshotEnriquecido(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	a := coord(t, s, "a", "w1:p1", "proj")
	coord(t, s, "b", "w9:p9", "")
	onda(t, s, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1"), us(t, "2"), us(t, "3")}})
	p2, p3 := "w1:p2", "w1:p3"
	if _, _, err := s.AtualizarStatus(ctx, &a, us(t, "1"), MudancaStatus{Status: "exec", Pane: &p2}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AtualizarStatus(ctx, &a, us(t, "2"), MudancaStatus{Status: "exec", Pane: &p3}); err != nil {
		t.Fatal(err)
	}

	// Antes de qualquer consulta: objetos presentes, valores nulos.
	snap, _ := s.Snapshot(ctx, false)
	u := snap.Coordenadores[0].Ondas[0].US
	if u[0].Herdr == nil || u[0].Herdr.Status != nil || u[2].Herdr != nil || u[0].Taiga.Titulo != nil {
		t.Fatalf("sem cache: %+v %+v", u[0].Herdr, u[2].Herdr)
	}
	sessao := SessaoHerdr{
		Panes:      map[string]PaneHerdr{"w1:p1": {Status: "working", Agente: "claude", WorkspaceID: "w1"}, "w1:p2": {Status: "blocked", Agente: "claude", Nome: "exec", WorkspaceID: "w1"}},
		Workspaces: map[string]WorkspaceHerdr{"w1": {}},
	}
	_ = s.GravarCache(ctx, FonteHerdr, "", sessao, nil)
	_ = s.GravarCache(ctx, FonteTaiga, "proj/1", ValorTaiga{Titulo: "Título", Status: "In progress", URL: "https://x/us/1"}, nil)
	_ = s.Suspender(ctx, "taiga", Erro{Codigo: "autenticacao", Mensagem: "session_expired"})
	// Herdr caiu depois: fica o último valor bom com o erro.
	_ = s.GravarCache(ctx, FonteHerdr, "", nil, &Erro{Codigo: "falha_comando", Mensagem: "socket"})

	snap, _ = s.Snapshot(ctx, false)
	c := snap.Coordenadores[0]
	u = c.Ondas[0].US
	if c.Herdr == nil || *c.Herdr.Status != "working" || c.Herdr.Erro == nil {
		t.Fatalf("coord herdr = %+v", c.Herdr)
	}
	if *u[0].Herdr.Status != "blocked" || *u[0].Herdr.Nome != "exec" || *u[0].Herdr.WorkspaceID != "w1" {
		t.Fatalf("us1 herdr = %+v", u[0].Herdr)
	}
	if *u[1].Herdr.Status != "ausente" || u[1].Herdr.Agente != nil {
		t.Fatalf("pane que sumiu devia ser ausente: %+v", u[1].Herdr)
	}
	if *snap.Coordenadores[1].Herdr.Status != "ausente" {
		t.Fatal("coordenador b sem pane no Herdr devia ser ausente")
	}
	if *u[0].Taiga.Titulo != "Título" || *u[0].Taiga.Status != "In progress" || u[1].Taiga.Titulo != nil {
		t.Fatalf("taiga = %+v / %+v", u[0].Taiga, u[1].Taiga)
	}
	f := snap.Fontes
	if f["herdr"].Erro == nil || f["herdr"].AtualizadoEm == nil || f["taiga"].Erro == nil || f["taiga"].Erro.Codigo != "autenticacao" || f["mr"].ConsultadoEm != nil {
		t.Fatalf("fontes = %+v", f)
	}
}
