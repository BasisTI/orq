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
		{"sem modelo", "", "", []string{` hidden><dt>Modelo</dt>`}, []string{`class="modelo`}},
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

// Trocar o modelo da US com a página aberta atualiza também a linha "Modelo" do corpo do
// card, e a linha nasce no lugar quando antes não havia modelo (orq #89).
func TestVivoTrocaOModeloNoCorpoDoCard(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	pane := "wA:p2"
	_, _, err := c.st.AtualizarStatus(c.ctx, &co, u, central.MudancaStatus{Status: "exec", Pane: &pane})
	c.ok(err)
	_, pagina := c.get("/")
	contem(t, pagina, ` hidden><dt>Modelo</dt>`)
	q := queryDaPagina(t, c, "/")

	for _, k := range []struct{ modelo, quer string }{{"claude-fable-5-2", "fable-5-2"}, {"gpt-5.5", "gpt-5.5"}} {
		m := k.modelo
		_, _, err = c.st.AtualizarStatus(c.ctx, &co, u, central.MudancaStatus{Status: "exec", Modelo: &m})
		c.ok(err)
		_, html, _ := c.vivo(q, "/")
		i := strings.Index(html, `-modelo"`)
		if i < 0 {
			t.Fatalf("%s: o /vivo não trouxe o campo Modelo do corpo:\n%s", m, html)
		}
		i = strings.LastIndex(html[:i], "<div")
		campo := html[i : i+strings.Index(html[i:], "</dd>")]
		if !strings.Contains(campo, `hx-swap-oob="true"`) || !strings.Contains(campo, k.quer) || strings.Contains(campo, `" hidden>`) || strings.Contains(campo, "\" hidden><dt>") {
			t.Errorf("%s: campo = %s", m, campo)
		}
	}
}
