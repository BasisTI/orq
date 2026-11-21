package web

import (
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/refs"
)

// O motivo da espera aparece na barra da US, depois das MRs e antes da conta de pendências, com o
// texto escapado; sem motivo, o espaço fica vazio (US #100).
func TestCentralMotivoDaEspera(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	_, html := c.get("/")
	contem(t, html, `<span class="us-espera" title=""></span>`)

	m := `Espera o <b>eSocial</b> & o arquivo`
	_, _, err := c.st.DefinirEspera(c.ctx, &co, refs.US{Projeto: "proj", Ref: 7}, &m)
	c.ok(err)
	_, html = c.get("/")
	contem(t, html, `Espera o &lt;b&gt;eSocial&lt;/b&gt; &amp; o arquivo`, `class="us-espera"`)
	if strings.Contains(html, "<b>eSocial</b>") {
		t.Errorf("texto do agente sem escape:\n%s", html)
	}
	i, j, k := strings.Index(html, `class="us-mrs"`), strings.Index(html, `class="us-espera"`), strings.Index(html, `class="conta-wrap"`)
	if !(i >= 0 && i < j && j < k) {
		t.Errorf("ordem: us-mrs=%d us-espera=%d conta=%d", i, j, k)
	}
}
