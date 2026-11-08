package central

import (
	"context"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/refs"
)

func chaves(o Onda) string {
	var cs []string
	for _, u := range o.US {
		cs = append(cs, u.Chave)
	}
	return strings.Join(cs, ",")
}

func TestAdicionarUSNoFimEDepoisDeUma(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "a", "p1", "proj")
	o := onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1"), us(t, "2"), us(t, "3")}})

	v, err := s.AdicionarUS(ctx, &co, o.ID, NovaAdicao{US: []refs.US{us(t, "4")}})
	if err != nil || chaves(v) != "proj/1,proj/2,proj/3,proj/4" {
		t.Fatalf("no fim: %v, %v", chaves(v), err)
	}
	apos := us(t, "1")
	v, err = s.AdicionarUS(ctx, &co, o.ID, NovaAdicao{US: []refs.US{us(t, "5"), us(t, "6")}, Apos: &apos})
	if err != nil || chaves(v) != "proj/1,proj/5,proj/6,proj/2,proj/3,proj/4" {
		t.Fatalf("depois de 1: %v, %v", chaves(v), err)
	}
	for i, u := range v.US {
		if u.Posicao != i+1 || (i >= 1 && i <= 2 && u.Status != "wait") {
			t.Fatalf("posição/status de %s = %d/%s", u.Chave, u.Posicao, u.Status)
		}
	}
	es, err := s.Diario(ctx, &co, FiltroDiario{OndaID: o.ID})
	if err != nil {
		t.Fatal(err)
	}
	var adicoes int
	for _, e := range es {
		if e.Tipo == "onda.adicionar" {
			adicoes++
			if e.Coordenador == nil || *e.Coordenador != "a" {
				t.Fatalf("evento sem coordenador: %+v", e)
			}
		}
	}
	if adicoes != 2 {
		t.Fatalf("eventos onda.adicionar = %d", adicoes)
	}
}

func TestAdicionarUSRecusas(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "a", "p1", "proj")
	o1 := onda(t, s, co, NovaOnda{Titulo: "um", US: []refs.US{us(t, "1")}})
	o2 := onda(t, s, co, NovaOnda{Titulo: "dois", US: []refs.US{us(t, "2")}})
	fora := us(t, "9")
	cases := []struct {
		nome string
		id   int64
		n    NovaAdicao
		erro string
	}{
		{"em outra onda aberta", o1.ID, NovaAdicao{US: []refs.US{us(t, "3"), us(t, "2")}}, "us_em_outra_onda"},
		{"já nesta onda", o1.ID, NovaAdicao{US: []refs.US{us(t, "1")}}, "us_ja_na_onda"},
		{"onda inexistente", 99, NovaAdicao{US: []refs.US{us(t, "3")}}, "nao_encontrado"},
		{"sem US", o1.ID, NovaAdicao{}, "uso"},
		{"repetida", o1.ID, NovaAdicao{US: []refs.US{us(t, "3"), us(t, "3")}}, "uso"},
		{"apos fora da onda", o1.ID, NovaAdicao{US: []refs.US{us(t, "3")}, Apos: &fora}, "uso"},
		{"apos de outra onda", o1.ID, NovaAdicao{US: []refs.US{us(t, "3")}, Apos: &refs.US{Projeto: "proj", Ref: 2}}, "uso"},
	}
	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			if _, err := s.AdicionarUS(ctx, &co, c.id, c.n); codigo(err) != c.erro {
				t.Fatalf("erro = %q (%v), queria %q", codigo(err), err, c.erro)
			}
		})
	}
	// Recusa desfaz tudo: a US 3 que vinha antes da 2 não entrou, e as posições não se mexeram.
	v1, _ := s.VerOnda(ctx, o1.ID)
	v2, _ := s.VerOnda(ctx, o2.ID)
	if chaves(v1) != "proj/1" || chaves(v2) != "proj/2" || v1.US[0].Posicao != 1 {
		t.Fatalf("ondas = %s | %s", chaves(v1), chaves(v2))
	}

	if _, err := s.EncerrarOnda(ctx, co.Nome, o1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdicionarUS(ctx, &co, o1.ID, NovaAdicao{US: []refs.US{us(t, "3")}}); codigo(err) != "onda_encerrada" {
		t.Fatalf("onda encerrada: %v", err)
	}
}

func TestAdicionarUSTrazDeOndaEncerradaESemCoordenador(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "a", "p1", "proj")
	velha := onda(t, s, co, NovaOnda{Titulo: "velha", Ate: "plano", US: []refs.US{us(t, "7")}})
	if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "7"), MudancaStatus{Status: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EncerrarOnda(ctx, co.Nome, velha.ID); err != nil {
		t.Fatal(err)
	}
	o := onda(t, s, co, NovaOnda{Titulo: "nova", US: []refs.US{us(t, "1")}})
	// Sem coordenador corrente: o projeto padrão e a assinatura vêm do dono da onda.
	v, err := s.AdicionarUS(ctx, nil, o.ID, NovaAdicao{US: []refs.US{us(t, "7")}})
	if err != nil || chaves(v) != "proj/1,proj/7" || v.US[1].Status != "wait" {
		t.Fatalf("US trazida = %+v, %v", v.US, err)
	}
	es, _ := s.Diario(ctx, nil, FiltroDiario{OndaID: o.ID})
	var tipos []string
	for _, e := range es {
		if e.Tipo == "onda.adicionar" && (e.Coordenador == nil || *e.Coordenador != "a" || !strings.Contains(string(e.Detalhe), "da onda")) {
			t.Fatalf("evento = %+v %s", e, e.Detalhe)
		}
		tipos = append(tipos, e.Tipo)
	}
	if !strings.Contains(strings.Join(tipos, ","), "us.status") {
		t.Fatalf("faltou o evento us.status do reinício: %v", tipos)
	}
}

func TestAdicionarUSEntraNaProximaEEncerramento(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	co := coord(t, s, "a", "p1", "proj")
	o := onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "1"), MudancaStatus{Status: "done"}); err != nil {
		t.Fatal(err)
	}
	p, _ := s.ProximaUS(ctx, o.ID)
	if p.US != nil || p.Encerramento == nil || !p.Encerramento.Pronta {
		t.Fatalf("antes: %+v", p)
	}
	if _, err := s.AdicionarUS(ctx, &co, o.ID, NovaAdicao{US: []refs.US{us(t, "2")}}); err != nil {
		t.Fatal(err)
	}
	p, _ = s.ProximaUS(ctx, o.ID)
	if p.US == nil || p.US.Chave != "proj/2" || p.Encerramento != nil {
		t.Fatalf("depois: %+v", p)
	}
	v, _ := s.VerOnda(ctx, o.ID)
	if EncerramentoDe(v) != nil {
		t.Fatal("onda com US em wait não pode estar pronta para encerrar")
	}
	if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "2"), MudancaStatus{Status: "done"}); err != nil {
		t.Fatal(err)
	}
	v, _ = s.VerOnda(ctx, o.ID)
	if e := EncerramentoDe(v); e == nil || !e.Pronta {
		t.Fatalf("encerramento = %+v", e)
	}
}
