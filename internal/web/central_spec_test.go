package web

import (
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

const mrDaSpec = "https://github.com/Exemplo/repo/pull/44"

// O card da US mostra os cinco estados da spec (registrada, em validação, aprovada, mudou além do
// decisoes.md, mesclada), com o SHA quando há um; sem spec, a linha fica escondida (orq #59).
func TestCentralMostraOEstadoDaSpecNoCard(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	_, html := c.get("/")
	contem(t, html, ` hidden><dt>Spec</dt>`)
	if strings.Contains(html, "selo-neutro\">"+`<svg class="ic" aria-hidden="true"><use href="#i-arquivo"/></svg>spec`) {
		t.Fatal("sem spec não há selo")
	}

	_, err := c.st.RegistrarSpec(c.ctx, &co, u, "docs/specs/e/tg-7-x", mrDaSpec, false)
	c.ok(err)
	_, html = c.get("/")
	contem(t, html, "spec registrada", "docs/specs/e/tg-7-x", mrDaSpec, "<dt>Spec</dt>")
	if strings.Contains(html, ` hidden><dt>Spec</dt>`) {
		t.Error("com spec, a linha não fica escondida")
	}

	p, err := c.st.AbrirValidacao(c.ctx, &co, u, "1234567890abcdef", "")
	c.ok(err)
	_, html = c.get("/")
	contem(t, html, "spec em validação 12345678", "selo-aviso")
	if strings.Contains(html, "1234567890abcdef") {
		t.Error("só o SHA curto aparece")
	}

	opcao := central.OpcaoAprovar
	_, err = c.st.ResponderPendencia(c.ctx, &co, refs.Pend{US: u, Numero: p.Numero}, central.Resposta{Opcao: &opcao, Por: "ana"})
	c.ok(err)
	_, html = c.get("/")
	contem(t, html, "spec aprovada 12345678", "selo-ok")

	conf := central.ConferenciaSpec{SHAAprovado: "1234567890abcdef", SHACabeca: "fedcba0987654321", MudouNaPasta: []string{"spec.md"}, ForaDaPasta: []string{"go.mod", "README.md"}}
	c.ok(c.st.GravarConferenciaSpec(c.ctx, u, &conf, nil))
	_, html = c.get("/")
	contem(t, html, "spec mudou além do decisoes.md 12345678", "selo-erro", "mudou além do decisoes.md: spec.md", "MR com 2 arquivo(s) fora da pasta")

	conf = central.ConferenciaSpec{SHAAprovado: "1234567890abcdef", SHACabeca: "fedcba0987654321", PodeMesclar: true}
	c.ok(c.st.GravarConferenciaSpec(c.ctx, u, &conf, nil))
	_, html = c.get("/")
	contem(t, html, "spec aprovada 12345678", "pode mesclar: só o decisoes.md mudou")

	c.ok(c.st.GravarMR(c.ctx, mrDaSpec, central.ValorMR{Estado: "mesclada"}, nil))
	_, html = c.get("/")
	contem(t, html, "spec mesclada 12345678")
}

// O /vivo troca a linha da spec e o selo da barra quando o estado muda com a página aberta.
func TestVivoTrocaOEstadoDaSpec(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	_, err := c.st.RegistrarSpec(c.ctx, &co, u, "docs/x", mrDaSpec, false)
	c.ok(err)
	_, pagina := c.get("/")
	contem(t, pagina, "spec registrada")
	q := queryDaPagina(t, c, "/")
	_, err = c.st.AbrirValidacao(c.ctx, &co, u, "abcdef0123456789", "")
	c.ok(err)
	_, html, _ := c.vivo(q, "/")
	contem(t, html, `id="us-proj-7-spec"`, "spec em validação abcdef01")
}
