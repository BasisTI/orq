package central

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/refs"
)

func tons(t *testing.T, s *Store) map[string]int {
	t.Helper()
	snap, err := s.Snapshot(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]int{}
	for _, p := range snap.Projetos {
		m[p.Projeto] = p.Indice
	}
	return m
}

func eventosTom(t *testing.T, s *Store) []string {
	t.Helper()
	var ps []string
	for _, e := range eventos(t, s) {
		if e.Tipo == "projeto.tom" {
			ps = append(ps, string(e.Detalhe))
		}
	}
	return ps
}

// Projeto novo recebe o próximo índice, na ordem de chegada; repetir não muda; cada atribuição
// deixa o seu evento e só ela.
func TestTomPorOrdemDeChegada(t *testing.T) {
	s := novoStore(t)
	a := coord(t, s, "a", "wA:p1", "zeta")
	if got := tons(t, s); !reflect.DeepEqual(got, map[string]int{"zeta": 0}) {
		t.Fatalf("depois do coord: %v", got)
	}
	onda(t, s, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "zeta/1"), us(t, "alfa/2"), us(t, "mid/3")}})
	coord(t, s, "a", "wA:p1", "zeta") // de novo: o mesmo índice
	coord(t, s, "b", "wB:p1", "alfa") // já existe por US: nada muda
	coord(t, s, "c", "wC:p1", "novo")
	want := map[string]int{"zeta": 0, "alfa": 1, "mid": 2, "novo": 3}
	if got := tons(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("tons = %v, queria %v", got, want)
	}
	if ev := eventosTom(t, s); len(ev) != 4 {
		t.Fatalf("eventos projeto.tom = %v", ev)
	}
	// Coordenador sem projeto não atribui nada.
	coord(t, s, "d", "wD:p1", "")
	if len(tons(t, s)) != 4 {
		t.Fatal("coordenador sem projeto atribuiu tom")
	}
}

// A migration preenche os projetos que já existem na ordem de primeira aparição (o registro do
// coordenador ou a onda da US), com o nome desempatando.
func TestMigracaoPreencheTons(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	n := 0
	s.Now = func() time.Time { n++; return base.Add(time.Duration(n) * time.Second) }
	a := coord(t, s, "a", "wA:p1", "zeta")
	coord(t, s, "b", "wB:p1", "alfa")
	// mid e abc entram na mesma onda: mesmo instante, o nome desempata (abc antes de mid).
	onda(t, s, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "mid/3"), us(t, "abc/4")}})
	// Simula um banco anterior à migration: sem a tabela, na versão 2.
	if _, err := s.db.ExecContext(ctx, "DROP TABLE projeto_tom"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "DROP TABLE banco_meta"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	want := map[string]int{"zeta": 0, "alfa": 1, "abc": 2, "mid": 3}
	if got := tons(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("backfill = %v, queria %v", got, want)
	}
	// O próximo projeto continua a contagem.
	coord(t, s, "c", "wC:p1", "novo")
	if got := tons(t, s)["novo"]; got != 4 {
		t.Fatalf("índice do projeto novo = %d, queria 4", got)
	}
}
