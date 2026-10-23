package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
)

// BenchmarkVivo mede a duração de UMA resposta do /vivo (o Snapshot mais a renderização) sobre a
// Central do protótipo (9 ondas), não ciclos por relógio de parede (spec §6, M17). Se passar de
// 50 ms, o /vivo ganha um cache de 5 s por processo.
func BenchmarkVivo(b *testing.B) {
	ctx := context.Background()
	st, err := central.Open(ctx, b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = st.Close() })
	semear(b, ctx, st)
	srv := &Servidor{St: st, Porta: 8080}
	h := srv.Handler()
	// O hash que a página tem: com ele a resposta traz tudo (barras, US, abas); com um velho, só o aviso.
	rp := httptest.NewRequest("GET", "/central", nil)
	rp.Host = "127.0.0.1:8080"
	wp := httptest.NewRecorder()
	h.ServeHTTP(wp, rp)
	m := regexp.MustCompile(`/vivo\?v=([0-9a-f]+)`).FindStringSubmatch(wp.Body.String())
	if m == nil {
		b.Fatal("a página não tem o #vivo")
	}
	novo := func() *http.Request {
		rq := httptest.NewRequest("GET", "/vivo?v="+m[1], nil)
		rq.Host = "127.0.0.1:8080"
		rq.Header.Set("HX-Request", "true")
		return rq
	}
	b.Run("resposta", func(b *testing.B) {
		b.ReportAllocs()
		var bytes int
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, novo())
			if w.Code != 200 {
				b.Fatalf("status %d", w.Code)
			}
			bytes = w.Body.Len()
		}
		b.ReportMetric(float64(bytes), "bytes/resposta")
	})
	b.Run("snapshot", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := st.Snapshot(ctx, false); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("pagina", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			rq := httptest.NewRequest("GET", "/central", nil)
			rq.Host = "127.0.0.1:8080"
			w := httptest.NewRecorder()
			h.ServeHTTP(w, rq)
			if w.Code != 200 {
				b.Fatalf("status %d", w.Code)
			}
		}
	})
}

func hashDaPagina(t *testing.T, c *cena, caminho string) string {
	t.Helper()
	_, html := c.get(caminho)
	m := regexp.MustCompile(`/vivo\?v=([0-9a-f]+)`).FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("%s não tem o #vivo", caminho)
	}
	return m[1]
}

func (c *cena) vivo(v, atual string) (int, string, http.Header) {
	c.t.Helper()
	cab := map[string]string{"HX-Request": "true"}
	if atual != "" {
		cab["HX-Current-URL"] = "http://127.0.0.1:8080" + atual
	}
	w := c.f.faz(req{metodo: "GET", caminho: "/vivo?v=" + v, cab: cab})
	return w.Code, corpo(w), w.Header()
}

func semeada(t *testing.T) *cena {
	c := vazia(t, false)
	semear(t, c.ctx, c.st)
	return c
}

func TestVivoSoTemOobESemFormulario(t *testing.T) {
	c := semeada(t)
	code, html, h := c.vivo(hashDaPagina(t, c, "/central"), "/central")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	contem(t, html, `id="situacao"`, `id="fontes"`, `id="onda-1-barra"`, `id="us-catalogo-404-barra"`, `id="aba-n-todos"`, `id="aba-n-pendencias"`, `id="vivo"`, `hx-swap="none"`)
	naoContem(t, html, "<form", "<textarea", "<input", "pend-corpo", "<details", "<body", "<main", `id="aviso-lista"`)
	// Cada <summary> da resposta é um oob.
	if n, o := strings.Count(html, "<summary"), strings.Count(html, `<summary class="`); n == 0 || n != o || strings.Count(html, `hx-swap-oob="true"`) < n {
		t.Errorf("<summary> sem oob: %d summaries", n)
	}
	if got := h.Get("Content-Security-Policy"); got != csp {
		t.Errorf("CSP = %q", got)
	}
	if got := h.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestVivoComHashVelhoAvisaENaoTocaNaLista(t *testing.T) {
	c := semeada(t)
	_, html, _ := c.vivo("000000000000", "/central")
	contem(t, html, `id="aviso-lista"`, "A lista mudou", `id="situacao"`, `id="fontes"`, `v=000000000000`)
	naoContem(t, html, "<summary", `id="aba-n-`)
	// hash inválido não é ecoado: vira o corrente.
	_, html, _ = c.vivo("<x>", "/central")
	naoContem(t, html, "<x>")
	contem(t, html, "A lista mudou")
}

func TestVivoPorAbaDeProjeto(t *testing.T) {
	c := semeada(t)
	v := hashDaPagina(t, c, "/projeto/agenda")
	_, html, _ := c.vivo(v, "/projeto/agenda")
	contem(t, html, `id="onda-4-barra"`, `id="onda-7-barra"`)
	naoContem(t, html, `id="onda-1-barra"`, "A lista mudou")
	// A mesma hash na aba Todos é de outra lista.
	_, html, _ = c.vivo(v, "/central")
	contem(t, html, "A lista mudou")
}

func TestVivoSemCabecalhoNoSocketUnix(t *testing.T) {
	c := vazia(t, true)
	w := c.f.faz(req{metodo: "GET", caminho: "/vivo", cab: map[string]string{"X-Orq-User": ""}})
	if w.Code != 401 {
		t.Errorf("/vivo sem usuário = %d, quer 401", w.Code)
	}
}

func TestProjetoEAbas(t *testing.T) {
	c := semeada(t)
	code, html := c.get("/projeto/nuvem")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	contem(t, html, `aria-current="page"`, "Ondas com US de nuvem", "Painel único de custos por projeto")
	naoContem(t, html, "Reagendamento em um toque")
	// A onda com US de dois projetos está nas duas abas.
	_, fat := c.get("/projeto/faturamento")
	contem(t, fat, "Painel único de custos por projeto")
	for _, slug := range []string{"inexistente", "Nuvem", "n%2Fv"} {
		if code, _ := c.get("/projeto/" + slug); code != 404 {
			t.Errorf("/projeto/%s = %d, quer 404", slug, code)
		}
	}
	// Só uma aba leva aria-current, e é a do projeto.
	if n := strings.Count(html, `aria-current="page"`); n != 1 {
		t.Errorf("aria-current ×%d", n)
	}
	if code, _ := c.get("/"); code != 200 {
		t.Errorf("/ (lista de pendências) = %d", code)
	}
}

func TestCentralPaginaSemInlineEComCSP(t *testing.T) {
	c := semeada(t)
	for _, p := range []string{"/central", "/projeto/catalogo", "/vivo"} {
		w := c.f.faz(req{metodo: "GET", caminho: p})
		if w.Header().Get("Content-Security-Policy") != csp {
			t.Errorf("%s sem a CSP exata", p)
		}
		if temInline(corpo(w)) {
			t.Errorf("%s tem inline", p)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s Cache-Control %q", p, w.Header().Get("Cache-Control"))
		}
	}
	_, html := c.get("/central")
	naoContem(t, html, "<details open", "wA:p", "sintetico-")
	contem(t, html, "<progress")
	for _, p := range []string{"/vivo", "/projeto/nuvem"} {
		if w := c.f.faz(req{metodo: "POST", caminho: p, corpo: "x=1"}); w.Code != 405 {
			t.Errorf("POST %s = %d", p, w.Code)
		}
	}
}

func TestCentralEscapaTextoHostil(t *testing.T) {
	c := vazia(t, false)
	_, _ = c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refsUS("proj", 7)
	c.taiga(u, `<script>alert(1)</script>`, `<img src=x onerror=alert(2)>`)
	_, html := c.get("/central")
	naoContem(t, html, "<script>alert", "<img src=x")
	contem(t, html, "&lt;script&gt;alert(1)&lt;/script&gt;")
}
