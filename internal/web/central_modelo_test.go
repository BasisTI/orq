package web

import (
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// O modelo do pane aparece na barra e no corpo da US, com o ícone da família; família
// desconhecida só com o texto; sem modelo, nada (US #81).
func TestCentralModeloDoPane(t *testing.T) {
	casos := []struct {
		nome, modelo, esforco string
		quer, nao             []string
	}{
		{"claude", "claude-fable-5-2", "medium",
			[]string{`class="modelo modelo-claude"`, `<use href="#m-claude"/>`, "fable-5-2 medium", `title="Claude · claude-fable-5-2 · esforço medium"`, "<dt>Modelo</dt>"}, nil},
		{"gpt sem esforço", "gpt-5.5", "", []string{`modelo-gpt`, `<use href="#m-gpt"/>`, "gpt-5.5<", `title="GPT · gpt-5.5"`}, []string{"esforço"}},
		{"desconhecido", "llama-4", "high", []string{`class="modelo"`, "llama-4 high", `title="modelo: llama-4 · esforço high"`}, []string{`<use href="#m-llama`, "modelo-llama"}},
		{"sem modelo", "", "", nil, []string{`class="modelo`, "<dt>Modelo</dt>"}},
	}
	for _, k := range casos {
		c := vazia(t, false)
		co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
		pane := "wA:p2"
		m := central.MudancaStatus{Status: "exec", Pane: &pane}
		if k.modelo != "" {
			m.Modelo = &k.modelo
		}
		if k.esforco != "" {
			m.Esforco = &k.esforco
		}
		_, _, err := c.st.AtualizarStatus(c.ctx, &co, refs.US{Projeto: "proj", Ref: 7}, m)
		c.ok(err)
		_, html := c.get("/")
		contem(t, html, k.quer...)
		for _, n := range k.nao {
			if strings.Contains(html, n) {
				t.Errorf("%s: a página tem %q", k.nome, n)
			}
		}
		// A CSP não aceita style= inline: o selo usa só classe.
		for _, pedaco := range strings.Split(html, `class="modelo`)[1:] {
			if i := strings.Index(pedaco, ">"); strings.Contains(pedaco[:i], "style=") {
				t.Errorf("%s: style inline no selo: %s", k.nome, pedaco[:i])
			}
		}
	}
}

func TestSpriteTemOsIconesDasFamilias(t *testing.T) {
	c := vazia(t, false)
	_, html := c.get("/")
	for _, f := range []string{"claude", "gpt", "deepseek", "glm", "kimi", "gemini"} {
		contem(t, html, `<symbol id="m-`+f+`"`)
	}
}
