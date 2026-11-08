package web

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
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
	rp := httptest.NewRequest("GET", "/", nil)
	rp.Host = "127.0.0.1:8080"
	wp := httptest.NewRecorder()
	h.ServeHTTP(wp, rp)
	m := valsRe.FindStringSubmatch(wp.Body.String())
	if m == nil {
		b.Fatal("a página não tem o #vivo")
	}
	query := valsParaQuery(m[1])
	novo := func() *http.Request {
		rq := httptest.NewRequest("GET", "/vivo?"+query, nil)
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
			rq := httptest.NewRequest("GET", "/", nil)
			rq.Host = "127.0.0.1:8080"
			w := httptest.NewRecorder()
			h.ServeHTTP(w, rq)
			if w.Code != 200 {
				b.Fatalf("status %d", w.Code)
			}
		}
	})
}

var valsRe = regexp.MustCompile(`hx-vals="([^"]*)"`)

// valsParaQuery transforma o hx-vals do #vivo (JSON, escapado em HTML) na query que o htmx manda.
func valsParaQuery(attr string) string {
	var m map[string]string
	if err := json.Unmarshal([]byte(html.UnescapeString(attr)), &m); err != nil {
		panic(err)
	}
	q := url.Values{}
	for k, v := range m {
		q.Set(k, v)
	}
	return q.Encode()
}

// queryDaPagina é o que o htmx da página mandaria ao /vivo.
func queryDaPagina(t *testing.T, c *cena, caminho string) url.Values {
	t.Helper()
	_, h := c.get(caminho)
	m := valsRe.FindStringSubmatch(h)
	if m == nil {
		t.Fatalf("%s não tem o #vivo", caminho)
	}
	q, _ := url.ParseQuery(valsParaQuery(m[1]))
	// O hx-include do #vivo manda também as pendências que a Central tem (input hidden "pi").
	if strings.Contains(h, `hx-include=".us-corpo input[name=pi]"`) {
		for _, pi := range piRe.FindAllStringSubmatch(h, -1) {
			q.Add("pi", html.UnescapeString(pi[1]))
		}
	}
	// A aba Pendências manda também a composição que renderizou (hx-include="#comp").
	if cm := compRe.FindStringSubmatch(h); cm != nil {
		q.Set("comp", html.UnescapeString(cm[1]))
	}
	return q
}

var piRe = regexp.MustCompile(`<input type="hidden" name="pi" value="([^"]*)">`)

var compRe = regexp.MustCompile(`id="comp" name="comp" value="([^"]*)"`)

func (c *cena) vivo(q url.Values, atual string) (int, string, http.Header) {
	c.t.Helper()
	cab := map[string]string{"HX-Request": "true"}
	if atual != "" {
		cab["HX-Current-URL"] = "http://127.0.0.1:8080" + atual
	}
	w := c.f.faz(req{metodo: "GET", caminho: "/vivo?" + q.Encode(), cab: cab})
	return w.Code, corpo(w), w.Header()
}

func semeada(t *testing.T) *cena {
	c := vazia(t, false)
	semear(t, c.ctx, c.st)
	return c
}

func TestVivoSoTemOobESemFormulario(t *testing.T) {
	c := semeada(t)
	code, html, h := c.vivo(queryDaPagina(t, c, "/"), "/central")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	contem(t, html, `id="situacao"`, `id="fontes"`, `id="onda-1-barra"`, `id="us-catalogo-404-barra"`, `id="aba-n-todos"`, `id="aba-n-pendencias"`)
	naoContem(t, html, "<form", "<textarea", "<input", "pend-corpo", "<details", "<body", "<main", `id="aviso-lista"`, `id="vivo"`)
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

func TestVivoComHashVelhoAvisaEAtualizaSoOQueAPaginaTem(t *testing.T) {
	c := semeada(t)
	q := queryDaPagina(t, c, "/")
	// A página tem só as ondas 1 e 2 (a lista do servidor é maior) e um hash que não é o atual.
	q.Set("v", "000000000000")
	q.Set("o", "1,2")
	q.Set("a", "aba-n-todos")
	_, html, _ := c.vivo(q, "/")
	contem(t, html, `id="aviso-lista"`, "A lista mudou", `id="situacao"`, `id="fontes"`, `id="onda-1-barra"`, `id="onda-2-barra"`, `id="aba-n-todos"`, `id="us-catalogo-404-barra"`)
	naoContem(t, html, `id="onda-3-barra"`, `id="onda-9-barra"`, `id="aba-n-pendencias"`, `id="aba-n-p-nuvem"`, `id="us-nuvem-78-barra"`)
	// lista inválida: nada é atualizado além da situação e das fontes, e o aviso aparece.
	q.Set("o", "<x>")
	_, html, _ = c.vivo(q, "/")
	naoContem(t, html, "<x>", "<summary")
	contem(t, html, "A lista mudou")
}

// A pendência nova, resolvida ou respondida aparece na lista da US sem avisar que "a lista mudou":
// a contagem do título e a lista concordam (US #41). Só o que mudou vai na resposta.
func TestVivoAtualizaAListaDePendenciasNoLugar(t *testing.T) {
	c := semeada(t)
	antes := queryDaPagina(t, c, "/")
	_, html, _ := c.vivo(antes, "/")
	naoContem(t, html, "A lista mudou", "pend-nova", "hx-swap-oob=\"beforeend", "hx-swap-oob=\"delete\"", "<details")
	// Nova, partindo de "Nenhuma pendência." (a 406 não tem nenhuma).
	_, err := c.st.RegistrarPendencia(c.ctx, central.NovaPendencia{US: refsUS("catalogo", 406), Tipo: "duvida", Titulo: "nova", Autor: "w", Detalhe: "d", Slug: "n1", Hash: "n1"})
	c.ok(err)
	_, html, _ = c.vivo(antes, "/")
	naoContem(t, html, "A lista mudou")
	contem(t, html, `id="us-catalogo-406-barra"`, "1 espera você", `<div hx-swap-oob="beforeend:#us-catalogo-406-pend">`, `id="pend-catalogo-406-1"`, `<p id="us-catalogo-406-vazio" class="vazio-linha" hidden hx-swap-oob="true">`)
	if n := strings.Count(html, "<details"); n != 1 {
		t.Errorf("a resposta trouxe %d pendência(s), quer só a nova", n)
	}
	// A página aplicou a mudança: o ciclo seguinte não repete o item.
	depois := queryDaPagina(t, c, "/")
	_, html, _ = c.vivo(depois, "/")
	naoContem(t, html, "<details", "hx-swap-oob=\"delete\"", "A lista mudou")
	// Respondida por outra pessoa: o item é trocado (estado novo), a contagem cai.
	_, err = c.st.ResponderPendencia(c.ctx, nil, refs.Pend{US: refsUS("catalogo", 406), Numero: 1}, central.Resposta{Texto: "ok", Por: "ana"})
	c.ok(err)
	_, html, _ = c.vivo(depois, "/")
	naoContem(t, html, "A lista mudou", "beforeend")
	contem(t, html, `<details class="pend pend-ok" id="pend-catalogo-406-1" data-estado="respondida" hx-swap-oob="true"`)
	// Resolvida: sai da lista, e "Nenhuma pendência." volta.
	depois = queryDaPagina(t, c, "/")
	_, err = c.st.ResolverPendencia(c.ctx, nil, refs.Pend{US: refsUS("catalogo", 406), Numero: 1}, "feito")
	c.ok(err)
	_, html, _ = c.vivo(depois, "/")
	contem(t, html, `<div id="pend-catalogo-406-1" hx-swap-oob="delete"></div>`, `<p id="us-catalogo-406-vazio" class="vazio-linha" hx-swap-oob="true">`)
	naoContem(t, html, "A lista mudou", "<details", `id="us-catalogo-406-vazio" class="vazio-linha" hidden`)
}

// O pi que a página manda é entrada do cliente: valor fora da forma esperada é ignorado, e a
// remoção só alcança pendência da própria US.
func TestVivoIgnoraPiInvalido(t *testing.T) {
	c := semeada(t)
	q := queryDaPagina(t, c, "/")
	q.Add("pi", "<script>:aberta")
	q.Add("pi", "pend-catalogo-9999-1:aberta")
	q.Add("pi", "sem-dois-pontos")
	q.Add("pi", "pend-x-1-1:<x>")
	_, html, _ := c.vivo(q, "/")
	naoContem(t, html, "<script>", "<x>", "pend-x-1-1", "A lista mudou")
	// Anchor de outra US (catalogo-9999 não existe na Central): nada a remover.
	naoContem(t, html, `hx-swap-oob="delete"`)
}

func TestVivoPorAbaDeProjeto(t *testing.T) {
	c := semeada(t)
	q := queryDaPagina(t, c, "/projeto/agenda")
	_, html, _ := c.vivo(q, "/projeto/agenda")
	contem(t, html, `id="onda-4-barra"`, `id="onda-7-barra"`)
	naoContem(t, html, `id="onda-1-barra"`, "A lista mudou")
	// O mesmo pedido, vindo da aba Todos, é de outra lista.
	_, html, _ = c.vivo(q, "/")
	contem(t, html, "A lista mudou")
	naoContem(t, html, `id="onda-1-barra"`)
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
	for _, p := range []string{"/", "/pendencias", "/projeto/catalogo", "/vivo"} {
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
	_, html := c.get("/")
	naoContem(t, html, "<details open", "wA:p", "sintetico-")
	contem(t, html, "<progress")
	for _, p := range []string{"/vivo", "/projeto/nuvem", "/pendencias"} {
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
	_, html := c.get("/")
	naoContem(t, html, "<script>alert", "<img src=x")
	contem(t, html, "&lt;script&gt;alert(1)&lt;/script&gt;")
}

// A remoção só alcança pendência da própria US ("infra-2025" não é "infra" com a US 2025), e a
// inserção segue o número da pendência (P2 antes de P10).
func TestPendMudaPrefixoExatoEOrdemNumerica(t *testing.T) {
	u := USVista{ID: "TG-2025", Anchor: "us-infra-2025", Projeto: ProjetoTag{Slug: "infra"}}
	for _, n := range []string{"10", "2"} {
		u.Pendencias = append(u.Pendencias, PendItem{Anchor: "pend-infra-2025-" + n, Estado: "aberta"})
	}
	tem := map[string]string{"pend-infra-2025-5-1": "aberta", "pend-infra-2025-9": "aberta"}
	var got []string
	for _, m := range pendMuda(u, tem) {
		got = append(got, m.Acao+":"+m.Item.Anchor)
	}
	want := []string{"inserir:pend-infra-2025-2", "remover:pend-infra-2025-9", "inserir:pend-infra-2025-10"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pendMuda = %v, quer %v", got, want)
	}
}
