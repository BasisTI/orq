package web

import (
	"fmt"
	"strings"
	"testing"
)

// Story 4 (#25): o diário em lista e as páginas de erro no visual novo.

func TestPaginasDeErroUsamOBannerProvisorio(t *testing.T) {
	c := vazia(t, false)
	for _, p := range []string{"/onda/999", "/onda/abc", "/onda/01", "/p/proj/xx/1"} {
		code, html := c.get(p)
		if code != 404 {
			t.Fatalf("GET %s = %d, quer 404", p, code)
		}
		contem(t, html, `class="banner"`, "/estatico/orq-banner-light.png", "Página não encontrada", "Código 404.", `role="alert"`, `href="`)
		if temInline(html) {
			t.Errorf("%s tem script ou estilo inline", p)
		}
	}
}

func TestErrosDoFalhaSemBanner(t *testing.T) {
	c := vazia(t, false)
	w := c.f.faz(req{metodo: "POST", caminho: "/p/proj/7/1/responder", corpo: "texto=x", cab: map[string]string{"Content-Type": "text/plain"}})
	if w.Code != 415 {
		t.Fatalf("status %d, quer 415", w.Code)
	}
	html := corpo(w)
	contem(t, html, "Formato não aceito", "Código 415.", `class="aviso erro"`, `role="alert"`)
	naoContem(t, html, `class="banner"`, "orq-banner-light.png")
}

func TestSemUsuarioNoSocketUnixDevolve401EmHTML(t *testing.T) {
	c := vazia(t, true)
	_, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	for _, p := range []string{"/", "/central", fmt.Sprintf("/onda/%d", o.ID)} {
		w := c.f.faz(req{metodo: "GET", caminho: p, cab: map[string]string{"X-Orq-User": ""}})
		if w.Code != 401 {
			t.Fatalf("GET %s sem usuário = %d, quer 401", p, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: Content-Type %q", p, ct)
		}
		html := corpo(w)
		contem(t, html, `class="banner"`, "Acesso não autenticado", "Código 401.", "X-Orq-User")
		naoContem(t, html, "alfa", "Onda de alfa")
		if temInline(html) {
			t.Errorf("%s tem script ou estilo inline", p)
		}
	}
}

func TestCentralVaziaMostraOBanner(t *testing.T) {
	c := vazia(t, false)
	code, html := c.get("/")
	if code != 200 {
		t.Fatal(code)
	}
	contem(t, html, `class="banner"`, "Nenhuma onda aberta", "Nenhum coordenador registrado")
	_, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	_, html = c.get("/")
	naoContem(t, html, `class="banner"`, "orq-banner-light.png")
	_, html = c.get(fmt.Sprintf("/onda/%d", o.ID))
	naoContem(t, html, `class="banner"`, "orq-banner-light.png") // o banner nunca aparece com dados
}

func TestDiarioEmListaComPaginador(t *testing.T) {
	c := vazia(t, false)
	_, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	href := fmt.Sprintf("/onda/%d", o.ID)
	code, html := c.get(href)
	if code != 200 {
		t.Fatal(code)
	}
	contem(t, html, `<ol class="diario">`, `class="d-quando"`, `class="d-tipo"`, "onda.criar", `<nav class="pager" aria-label="Páginas do diário">`, "página 1 de 1", fmt.Sprintf("#%d", o.ID))
	naoContem(t, html, "mais antigas", "mais recentes", "wA:p1")
}
