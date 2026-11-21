package central

import (
	"context"
	"github.com/BasisTI/orq/internal/refs"
	"testing"
)

func TestNomeValido(t *testing.T) {
	for nome, quer := range map[string]bool{
		"orq-comunicacao": true, "a": true, "liven_ui-3": true, "a23456789012345678901234567890ab": true,
		"": false, "Orq": false, "1orq": false, "-orq": false, "orq comunicacao": false, "orq.x": false,
		"a234567890123456789012345678901ab": false, "ação": false,
	} {
		if NomeValido(nome) != quer {
			t.Errorf("NomeValido(%q) = %v", nome, !quer)
		}
	}
}

func TestCriarOndaGravaOuRecusaONome(t *testing.T) {
	ctx := context.Background()
	s := novoStore(t)
	c := coord(t, s, "a", "wA:p1", "proj")
	o := onda(t, s, c, NovaOnda{Titulo: "qualquer título", Nome: "minha-onda", US: []refs.US{us(t, "proj/1")}})
	if o.Nome != "minha-onda" {
		t.Fatalf("nome = %q", o.Nome)
	}
	if _, err := s.CriarOnda(ctx, c, NovaOnda{Titulo: "x", Nome: "Inválido", US: []refs.US{us(t, "proj/2")}}); codigo(err) != "uso" {
		t.Fatalf("nome inválido = %v", err)
	}
}

// Onda criada por um binário anterior (sem a coluna nome): o nome vem do início do título, se
// for válido; senão é vazio, e o pane não é renomeado por ela.
func TestNomeDeOndaAntiga(t *testing.T) {
	ctx := context.Background()
	s := novoStore(t)
	c := coord(t, s, "a", "wA:p1", "proj")
	for i, titulo := range []string{"orq-papeis-fluxos — onda grande", "IDEIA-011 conciliação", "Wazuh e outras coisas"} {
		// O INSERT do orq anterior: sem a coluna nome.
		if _, err := s.db.ExecContext(ctx, `INSERT INTO onda (coordenador, titulo, autorizacao_ate, ordem, criada_em) VALUES (?, ?, 'merge', 'livre', ?)`, c.Nome, titulo, "2026-11-0"+string(rune('1'+i))+"T10:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	os, err := s.ListarOndas(ctx, FiltroOndas{})
	if err != nil {
		t.Fatal(err)
	}
	quer := []string{"orq-papeis-fluxos", "", ""}
	for i, o := range os {
		if o.Nome != quer[i] {
			t.Errorf("onda %d: nome = %q, queria %q", o.ID, o.Nome, quer[i])
		}
	}
}

func TestPaneEsperadoSegueAOndaAbertaMaisRecente(t *testing.T) {
	ctx := context.Background()
	s := novoStore(t)
	c := coord(t, s, "a", "wA:p1", "proj")
	coord(t, s, "sempane", "", "proj")
	if p, err := s.PaneEsperadoDe(ctx, "a"); err != nil || p != nil {
		t.Fatalf("sem onda = %+v, %v", p, err)
	}
	o1 := onda(t, s, c, NovaOnda{Titulo: "t1", Nome: "primeira", US: []refs.US{us(t, "proj/1")}})
	o2 := onda(t, s, c, NovaOnda{Titulo: "t2", Nome: "segunda", US: []refs.US{us(t, "proj/2")}})
	esperar := func(nome string, id int64) {
		t.Helper()
		p, err := s.PaneEsperadoDe(ctx, "a")
		if err != nil || p == nil || p.Nome != nome || p.OndaID != id || p.Pane != "wA:p1" {
			t.Fatalf("esperado = %+v, %v; queria %s (onda %d)", p, err, nome, id)
		}
	}
	esperar("segunda", o2.ID)
	if _, err := s.EncerrarOnda(ctx, "a", o2.ID); err != nil {
		t.Fatal(err)
	}
	esperar("primeira", o1.ID)
	if _, err := s.EncerrarOnda(ctx, "a", o1.ID); err != nil {
		t.Fatal(err)
	}
	if p, err := s.PaneEsperadoDe(ctx, "a"); err != nil || p != nil {
		t.Fatalf("tudo encerrado = %+v, %v", p, err)
	}
	// Coordenador sem pane registrado não entra.
	sp, _ := s.ListarCoords(ctx)
	onda(t, s, sp[1], NovaOnda{Titulo: "t", Nome: "x", US: []refs.US{us(t, "proj/9")}})
	ps, err := s.PanesEsperados(ctx)
	if err != nil || len(ps) != 0 {
		t.Fatalf("panes esperados = %+v, %v", ps, err)
	}
}
