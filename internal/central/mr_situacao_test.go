package central

import (
	"context"
	"testing"

	"github.com/BasisTI/orq/internal/refs"
)

func TestSnapshotTrazPipelineEAprovacaoDaMR(t *testing.T) {
	ctx := context.Background()
	s := novoStore(t)
	c := coord(t, s, "alfa", "wA:p1", "proj")
	onda(t, s, c, NovaOnda{Titulo: "o", US: []refs.US{us(t, "proj/1")}})
	const url = "https://github.com/BasisTI/orq/pull/1"
	if _, _, err := s.AdicionarMR(ctx, &c, us(t, "proj/1"), url); err != nil {
		t.Fatal(err)
	}
	leitura := func() MR {
		snap, err := s.Snapshot(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		return snap.Coordenadores[0].Ondas[0].US[0].MRs[0]
	}
	if m := leitura(); m.Pipeline != nil || m.Aprovacao != nil {
		t.Fatalf("sem consulta: %v %v", m.Pipeline, m.Aprovacao)
	}
	// Valor de uma versão antiga do cache (sem os campos novos): continuam nulos.
	if err := s.GravarMR(ctx, url, ValorMR{Estado: "aberta"}, nil); err != nil {
		t.Fatal(err)
	}
	if m := leitura(); m.Estado == nil || m.Pipeline != nil || m.Aprovacao != nil {
		t.Fatalf("cache antigo: %+v", m)
	}
	if err := s.GravarMR(ctx, url, ValorMR{Estado: "aberta", Pipeline: "rodando", Aprovacao: "nao_exigida"}, nil); err != nil {
		t.Fatal(err)
	}
	if m := leitura(); m.Pipeline == nil || *m.Pipeline != "rodando" || *m.Aprovacao != "nao_exigida" {
		t.Fatalf("com valores: %+v", m)
	}
	// A consulta que falha preserva o último valor bom.
	if err := s.GravarMR(ctx, url, ValorMR{}, &Erro{Codigo: "falha_comando", Mensagem: "x", Em: "2026-10-21T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if m := leitura(); m.Erro == nil || m.Pipeline == nil || *m.Pipeline != "rodando" {
		t.Fatalf("depois do erro: %+v", m)
	}
}
