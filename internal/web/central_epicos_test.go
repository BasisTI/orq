package web

import (
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

func (c *cena) taigaEpicos(u refs.US, epicos ...central.EpicoTaiga) {
	c.t.Helper()
	c.ok(c.st.GravarCache(c.ctx, central.FonteTaiga, u.Chave(), central.ValorTaiga{Titulo: "t", Status: "New", URL: "https://taiga.exemplo/project/" + u.Projeto + "/us/1", Epicos: epicos}, nil))
}

func TestCentralOndaMostraEpicosSemRepetir(t *testing.T) {
	c := vazia(t, false)
	co, err := c.st.RegistrarCoord(c.ctx, central.Registro{Nome: "alfa", Pane: "wA:p1", Projeto: "proj"})
	c.ok(err)
	u1, u2, u3 := refs.US{Projeto: "proj", Ref: 1}, refs.US{Projeto: "proj", Ref: 2}, refs.US{Projeto: "proj", Ref: 3}
	_, err = c.st.CriarOnda(c.ctx, co, central.NovaOnda{Titulo: "Onda", Ate: "merge", US: []refs.US{u1, u2, u3}})
	c.ok(err)
	e30 := central.EpicoTaiga{Ref: 30, Titulo: "Ajustar o orquestrador", URL: "https://taiga.exemplo/project/proj/epic/30"}
	e31 := central.EpicoTaiga{Ref: 31, Titulo: "Outro", URL: "https://taiga.exemplo/project/proj/epic/31"}
	c.taigaEpicos(u1, e30)
	c.taigaEpicos(u2, e30, e31)
	c.taiga(u3, "sem épico", "New")

	_, html := c.get("/")
	contem(t, html, "Épicos", `href="https://taiga.exemplo/project/proj/epic/30"`, `href="https://taiga.exemplo/project/proj/epic/31"`, ">#30<", ">#31<")
	if n := strings.Count(html, ">#30<"); n != 1 {
		t.Errorf("épico #30 ×%d, quer 1", n)
	}
}

func TestCentralOndaSemEpicoNaoMostraRotulo(t *testing.T) {
	c := vazia(t, false)
	c.coord("alfa", "wA:p1", "sgo", 7, "merge")
	// Sem Taiga (projeto sem cache) e com Taiga sem épico.
	_, html := c.get("/")
	naoContem(t, html, "onda-epicos", "Épico")
	c.taiga(refs.US{Projeto: "sgo", Ref: 7}, "sem épico", "New")
	_, html = c.get("/")
	naoContem(t, html, "onda-epicos", "Épico")
	contem(t, html, "Diário da onda")
}

func TestCentralEpicoSemURLNaoViraLink(t *testing.T) {
	c := vazia(t, false)
	c.coord("alfa", "wA:p1", "proj", 7, "merge")
	c.taigaEpicos(refs.US{Projeto: "proj", Ref: 7}, central.EpicoTaiga{Ref: 5, Titulo: "X"})
	_, html := c.get("/")
	contem(t, html, "onda-epicos", "#5")
	naoContem(t, html, "/epic/")
}

// Épico que entra no Taiga com a página aberta aparece no /vivo, no lugar, sem recarregar.
func TestVivoTrocaOsEpicosDaOnda(t *testing.T) {
	c := vazia(t, false)
	c.coord("alfa", "wA:p1", "proj", 7, "merge")
	u := refs.US{Projeto: "proj", Ref: 7}
	c.taiga(u, "sem épico", "New")
	_, pagina := c.get("/")
	contem(t, pagina, `id="onda-1-epicos"`)
	naoContem(t, pagina, `class="onda-epicos"`)
	q := queryDaPagina(t, c, "/")

	c.taigaEpicos(u, central.EpicoTaiga{Ref: 30, Titulo: "Novo", URL: "https://taiga.exemplo/project/proj/epic/30"})
	_, html, _ := c.vivo(q, "/")
	contem(t, html, `id="onda-1-epicos"`, `hx-swap-oob="true"`, `class="onda-epicos"`, ">#30<")
	naoContem(t, html, `id="onda-1-epicos" hidden`)
}
