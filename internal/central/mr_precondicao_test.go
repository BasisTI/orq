package central

import (
	"context"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/refs"
)

func TestPrecondicaoDaMRNoSnapshotEDiario(t *testing.T) {
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
	eventos := func() int {
		es, err := s.Diario(ctx, nil, FiltroDiario{})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range es {
			if e.Tipo == "mr.precondicao" {
				n++
			}
		}
		return n
	}
	// Sem pré-condição: nula, e Draft comum continua comum.
	if err := s.GravarMR(ctx, url, ValorMR{Estado: "draft"}, nil); err != nil {
		t.Fatal(err)
	}
	if m := leitura(); m.Precondicao != nil || m.DraftComPrecondicao() {
		t.Fatalf("Draft comum: %+v", m)
	}
	antes := eventos()
	m, err := s.DefinirPrecondicaoMR(ctx, &c, us(t, "proj/1"), url, "  Mesclar depois de\n orq#27 e do orq upgrade ")
	if err != nil {
		t.Fatal(err)
	}
	const texto = "Mesclar depois de orq#27 e do orq upgrade"
	if m.Precondicao == nil || *m.Precondicao != texto {
		t.Fatalf("devolveu %+v", m.Precondicao)
	}
	if m := leitura(); m.Precondicao == nil || *m.Precondicao != texto || !m.DraftComPrecondicao() {
		t.Fatalf("Draft com pré-condição: %+v", m)
	}
	if eventos() != antes+1 {
		t.Fatalf("eventos: %d → %d", antes, eventos())
	}
	// O mesmo texto de novo não muda nada nem gera evento.
	if _, err := s.DefinirPrecondicaoMR(ctx, &c, us(t, "proj/1"), url, texto); err != nil {
		t.Fatal(err)
	}
	if eventos() != antes+1 {
		t.Fatal("texto igual gerou evento")
	}
	// A pré-condição só vale em Draft: a MR pronta continua pronta, com ou sem texto guardado.
	if err := s.GravarMR(ctx, url, ValorMR{Estado: "pronta"}, nil); err != nil {
		t.Fatal(err)
	}
	if m := leitura(); m.DraftComPrecondicao() {
		t.Fatalf("pronta com texto guardado virou Draft com pré-condição: %+v", m)
	}
	// Limpar zera e gera evento; MR sem texto nunca é Draft com pré-condição.
	if m, err := s.DefinirPrecondicaoMR(ctx, &c, us(t, "proj/1"), url, " "); err != nil || m.Precondicao != nil {
		t.Fatalf("limpar: %+v, %v", m.Precondicao, err)
	}
	if eventos() != antes+2 {
		t.Fatalf("limpar não gerou evento: %d", eventos())
	}
	if m := leitura(); m.Precondicao != nil {
		t.Fatalf("depois de limpar: %+v", m)
	}
}

func TestPrecondicaoRecusaMRNaoLigadaETextoGrande(t *testing.T) {
	ctx := context.Background()
	s := novoStore(t)
	c := coord(t, s, "alfa", "wA:p1", "proj")
	onda(t, s, c, NovaOnda{Titulo: "o", US: []refs.US{us(t, "proj/1")}})
	if _, err := s.DefinirPrecondicaoMR(ctx, &c, us(t, "proj/1"), "https://github.com/BasisTI/orq/pull/9", "Mesclar depois de x"); codigo(err) != "nao_encontrado" {
		t.Fatalf("MR não ligada: %v", err)
	}
	const url = "https://github.com/BasisTI/orq/pull/1"
	if _, _, err := s.AdicionarMR(ctx, &c, us(t, "proj/1"), url); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DefinirPrecondicaoMR(ctx, &c, us(t, "proj/1"), url, strings.Repeat("a", TamanhoMaxPrecondicao+1)); codigo(err) != "uso" {
		t.Fatalf("texto grande: %v", err)
	}
}
