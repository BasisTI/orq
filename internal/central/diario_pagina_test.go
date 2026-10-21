package central

import (
	"context"
	"testing"

	"github.com/BasisTI/orq/internal/refs"
)

func TestDiarioDeslocamentoEContar(t *testing.T) {
	ctx := context.Background()
	s := novoStore(t)
	c := coord(t, s, "alfa", "wA:p1", "proj")
	o := onda(t, s, c, NovaOnda{Titulo: "o", US: []refs.US{us(t, "proj/1")}})
	outra := onda(t, s, c, NovaOnda{Titulo: "p", US: []refs.US{us(t, "proj/2")}})

	if err := s.RegistrarExternos(ctx, &c, us(t, "proj/1"), []EventoExterno{{Tipo: "limpar.removido", Detalhe: map[string]any{"n": 1}}, {Tipo: "limpar.removido", Detalhe: map[string]any{"n": 2}}}); err != nil {
		t.Fatal(err)
	}
	todos, err := s.Diario(ctx, nil, FiltroDiario{OndaID: o.ID})
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.ContarDiario(ctx, nil, FiltroDiario{OndaID: o.ID})
	if err != nil || n != len(todos) || n < 2 {
		t.Fatalf("contagem %d (%v), eventos %d", n, err, len(todos))
	}
	// Página 1 (os dois mais recentes) e página 2 (o resto), cada uma em ordem cronológica.
	p1, _ := s.Diario(ctx, nil, FiltroDiario{OndaID: o.ID, Limite: 1})
	p2, _ := s.Diario(ctx, nil, FiltroDiario{OndaID: o.ID, Limite: 1, Deslocamento: 1})
	if len(p1) != 1 || len(p2) != 1 || p1[0].ID != todos[n-1].ID || p2[0].ID != todos[n-2].ID {
		t.Errorf("páginas %v %v, todos %v", p1, p2, todos)
	}
	if n2, _ := s.ContarDiario(ctx, nil, FiltroDiario{OndaID: outra.ID}); n2 != 1 {
		t.Errorf("contagem da outra onda = %d", n2)
	}
}
