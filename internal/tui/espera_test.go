package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A coluna "espera" vem depois das MRs e só existe quando alguma US da tela tem motivo (US #100).
func TestColunaDaEsperaDepoisDaMR(t *testing.T) {
	op := Opcoes{Coord: "convey", Largura: 140, Agora: agora}
	s := snapshot()
	semMotivo := strings.Join(planas(cores.Desenhar(s, op)), "\n")
	if strings.Contains(semMotivo, "espera") && strings.Contains(semMotivo, "│ espera") {
		t.Errorf("a coluna não devia existir sem motivo:\n%s", semMotivo)
	}
	s.Coordenadores[0].Ondas[0].US[0].EsperaMotivo = ptr("Espera a carga corrigida\x1b[31m no colaboradados")
	plano := planas(cores.Desenhar(s, op))
	todo := strings.Join(plano, "\n")
	if !strings.Contains(todo, "espera") || !strings.Contains(todo, "Espera a carga corrigida") {
		t.Fatalf("motivo ausente:\n%s", todo)
	}
	if strings.Contains(todo, "\x1b") {
		t.Errorf("escape do agente chegou ao terminal: %q", todo)
	}
	for _, l := range plano {
		if strings.Contains(l, "Espera a carga") {
			if i, j := strings.Index(l, "!"), strings.Index(l, "Espera a carga"); i >= 0 && j < i {
				t.Errorf("o motivo vem antes da MR: %q", l)
			}
		}
		if w := ansi.StringWidth(l); w > op.Largura {
			t.Errorf("linha de %d colunas passa de %d: %q", w, op.Largura, l)
		}
	}
}

// Num pane estreito a coluna encolhe, mas nenhuma linha passa da largura.
func TestColunaDaEsperaNaoEstouraEmPaneEstreito(t *testing.T) {
	s := snapshot()
	s.Coordenadores[0].Ondas[0].US[0].EsperaMotivo = ptr(strings.Repeat("motivo longo ", 10))
	for _, w := range []int{60, 80, 100, 120} {
		for _, l := range planas(cores.Desenhar(s, Opcoes{Coord: "convey", Largura: w, Agora: agora})) {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("largura %d: linha de %d: %q", w, got, l)
			}
		}
	}
}
