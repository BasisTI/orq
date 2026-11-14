package tui

import (
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/charmbracelet/x/ansi"
)

func comModelo(modelo, esforco string) central.Snapshot {
	s := snapshot()
	us := s.Coordenadores[0].Ondas[0].US
	if modelo != "" {
		us[0].Modelo = ptr(modelo)
	}
	if esforco != "" {
		us[0].Esforco = ptr(esforco)
	}
	return s
}

func todoTexto(t Tela) string {
	var b strings.Builder
	for _, l := range t.Linhas {
		b.WriteString(l.Texto + "\n")
	}
	return b.String()
}

func TestModeloNoStatusDaUS(t *testing.T) {
	op := Opcoes{Coord: "convey", Largura: 120, Agora: agora}
	plano := strings.Join(planas(cores.Desenhar(comModelo("claude-fable-5-2", "medium"), op)), "\n")
	if !strings.Contains(plano, "Rev r2  [C] fable-5-2 medium ↗") {
		t.Errorf("selo ausente ou fora do lugar:\n%s", plano)
	}
	// Sem esforço, só o modelo curto.
	plano = strings.Join(planas(cores.Desenhar(comModelo("gpt-5.5", ""), op)), "\n")
	if !strings.Contains(plano, "Rev r2  [O] gpt-5.5 ↗") {
		t.Errorf("gpt sem esforço:\n%s", plano)
	}
	// Família desconhecida: só o texto, sem selo.
	plano = strings.Join(planas(cores.Desenhar(comModelo("llama-4", "high"), op)), "\n")
	if !strings.Contains(plano, "Rev r2  llama-4 high ↗") || strings.Contains(plano, "[L]") {
		t.Errorf("desconhecido:\n%s", plano)
	}
	// Sem modelo registrado, a tela é a de antes.
	plano = strings.Join(planas(cores.Desenhar(comModelo("", ""), op)), "\n")
	if !strings.Contains(plano, "Rev r2 ↗") {
		t.Errorf("sem modelo:\n%s", plano)
	}
}

func TestSeloDoModeloNaCorDaMarca(t *testing.T) {
	op := Opcoes{Coord: "convey", Largura: 120, Agora: agora}
	casos := map[string]string{
		"claude-fable-5-2": "\x1b[1;38;2;224;139;107m[C]",
		"gpt-5.5":          "\x1b[1;38;2;63;194;155m[O]",
		"deepseek-v4":      "\x1b[1;38;2;111;139;255m[D]",
		"kimi-k3":          "\x1b[1;38;2;74;163;255m[K]",
		"gemini-3":         "\x1b[1;38;2;169;143;224m[G]",
		"glm-5":            "\x1b[1;38;2;111;140;255m[Z]",
	}
	for modelo, quer := range casos {
		if got := todoTexto(cores.Desenhar(comModelo(modelo, ""), op)); !strings.Contains(got, quer) {
			t.Errorf("%s: falta %q", modelo, quer)
		}
	}
	// Desconhecido: o texto vai apagado, sem a cor de nenhuma marca.
	got := todoTexto(cores.Desenhar(comModelo("llama-4", ""), op))
	if !strings.Contains(got, "llama-4") || strings.Contains(got, "\x1b[1;38;2;224;139;107m") {
		t.Errorf("desconhecido colorido: %q", got)
	}
}

func TestModeloNaoQuebraAlinhamento(t *testing.T) {
	for _, l := range []int{60, 80, 120} {
		tela := cores.Desenhar(comModelo("claude-fable-5-2", "medium"), Opcoes{Coord: "convey", Largura: l, Agora: agora})
		w0 := ansi.StringWidth(tela.Linhas[2].Texto)
		for i, ln := range tela.Linhas {
			if w := ansi.StringWidth(ln.Texto); w > l {
				t.Errorf("largura %d, linha %d com %d colunas", l, i, w)
			}
		}
		if w0 > l {
			t.Errorf("largura %d: tabela com %d colunas", l, w0)
		}
	}
}

func TestModeloLimpaControlesDoTexto(t *testing.T) {
	s := comModelo("claude-x\x1b[31m", "")
	got := todoTexto(cores.Desenhar(s, Opcoes{Coord: "convey", Largura: 120, Agora: agora}))
	if strings.Contains(got, "\x1b[31m") {
		t.Errorf("controle do modelo chegou ao terminal: %q", got)
	}
}
