package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

const arquivo = `## Contexto
Texto com <script>alert(1)</script> e [clique](javascript:alert(2)) e [dados](data:text/html;base64,PHNjcmlwdD4=) e [ok](https://exemplo.com/x).

<img src=x onerror=alert(3)>

## Pergunta
Qual?

## Recomendação
A segunda.

## O que fica bloqueado
Nada.

## Evidências
- https://gitlab.exemplo/mr/1
`

type fixture struct {
	t   *testing.T
	st  *central.Store
	co  central.Coordenador
	srv *Servidor
	log []string
}

func nova(t *testing.T, unix bool) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := central.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	co, err := st.RegistrarCoord(ctx, central.Registro{Nome: "coord", Pane: "wA:p1", Projeto: "orq"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CriarOnda(ctx, co, central.NovaOnda{Titulo: "o", US: []refs.US{{Projeto: "orq", Ref: 15}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AdicionarMR(ctx, &co, refs.US{Projeto: "orq", Ref: 15}, "https://github.com/BasisTI/orq/pull/20"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RegistrarPendencia(ctx, central.NovaPendencia{US: refs.US{Projeto: "orq", Ref: 15}, Tipo: "decisao", Titulo: "Qual <b>biblioteca</b>?", Autor: "w", Detalhe: arquivo, Slug: "d", Hash: "h",
		Opcoes: []central.Opcao{{ID: 1, Titulo: "A", Descricao: "primeira"}, {ID: 2, Titulo: "B", Descricao: "segunda", Recomendada: true}}}); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, st: st, co: co}
	f.srv = &Servidor{St: st, Unix: unix, Log: func(format string, args ...any) { f.log = append(f.log, strings.TrimSpace(sprintf(format, args...))) }}
	if !unix {
		f.srv.Porta = 8080
	}
	return f
}

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }

type req struct {
	metodo, caminho, corpo string
	cab                    map[string]string
}

func (f *fixture) faz(r req) *httptest.ResponseRecorder {
	f.t.Helper()
	var body io.Reader
	if r.corpo != "" {
		body = strings.NewReader(r.corpo)
	}
	rq := httptest.NewRequest(r.metodo, r.caminho, body)
	if !f.srv.Unix {
		rq.Host = "127.0.0.1:8080"
	} else {
		rq.Host = "pend.exemplo"
		rq.Header.Set("X-Orq-User", "ana")
	}
	if r.metodo == "POST" {
		rq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range r.cab {
		if v == "" {
			rq.Header.Del(k)
		} else {
			rq.Header.Set(k, v)
		}
		if k == "Host" {
			rq.Host = v
		}
	}
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, rq)
	return w
}

func (f *fixture) pend() central.Pendencia {
	f.t.Helper()
	p, err := f.st.VerPendencia(context.Background(), nil, refs.Pend{US: refs.US{Projeto: "orq", Ref: 15}, Numero: 1})
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func corpo(w *httptest.ResponseRecorder) string {
	b, _ := io.ReadAll(w.Result().Body)
	return string(b)
}

func form(v url.Values) string { return v.Encode() }

// doMain devolve a página a partir do <main>: o cabeçalho e o <head> são do orq (logo, <script src>
// do htmx), e os testes de escape olham só o que o agente e a pessoa escreveram.
func doMain(html string) string {
	if _, resto, ok := strings.Cut(html, "<main"); ok {
		return resto
	}
	return html
}

// temInline diz se há <script> sem src, atributo style="" ou atributo de evento: o que a CSP
// proíbe e o que as páginas não podem ter (o <script src> do htmx e do app.js é permitido).
func temInline(html string) bool {
	for resto := html; ; {
		i := strings.Index(resto, "<script")
		if i < 0 {
			break
		}
		tag, depois, _ := strings.Cut(resto[i:], ">")
		if !strings.Contains(tag, " src=") || !strings.HasPrefix(depois, "</script>") {
			return true
		}
		resto = depois
	}
	for _, a := range []string{"style=", " onclick=", " onload=", " onerror=", " onchange=", " onsubmit="} {
		if strings.Contains(html, a) {
			return true
		}
	}
	return false
}

func TestDetalheEscapaScriptEJavascript(t *testing.T) {
	f := nova(t, false)
	w := f.faz(req{metodo: "GET", caminho: "/p/orq/15/1"})
	html := doMain(corpo(w))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, html)
	}
	for _, perigo := range []string{"<script>alert", "javascript:", "data:text/html", "onerror=alert(3)>", "<img "} {
		if strings.Contains(html, perigo) && perigo != "onerror=alert(3)>" {
			t.Errorf("a página tem %q", perigo)
		}
	}
	if strings.Contains(html, "<img src=x") || strings.Contains(html, "<script") {
		t.Errorf("HTML bruto ativo:\n%s", html)
	}
	// O texto do agente continua visível, escapado.
	if !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("o <script> não saiu escapado:\n%s", html)
	}
	if !strings.Contains(html, `href="https://exemplo.com/x"`) || !strings.Contains(html, `rel="noopener noreferrer nofollow"`) {
		t.Errorf("link comum perdido:\n%s", html)
	}
	// O título (do agente) também sai escapado.
	if !strings.Contains(html, "Qual &lt;b&gt;biblioteca&lt;/b&gt;?") || strings.Contains(html, "<b>biblioteca") {
		t.Errorf("título sem escape")
	}
	// Opções como rádio, a recomendada com selo e nenhuma marcada.
	if strings.Count(html, `type="radio"`) != 2 || strings.Contains(html, "checked") || strings.Count(html, "recomendada</span>") != 1 {
		t.Errorf("opções:\n%s", html)
	}
	if !strings.Contains(html, "<textarea") || strings.Contains(html, "http-equiv=\"refresh\"") || strings.Contains(html, "<script") {
		t.Errorf("formulário ou recarga:\n%s", html)
	}
	// MR e link da US.
	if !strings.Contains(html, "https://github.com/BasisTI/orq/pull/20") {
		t.Errorf("sem a MR:\n%s", html)
	}
}

func TestMarkdownLinksPerigosos(t *testing.T) {
	for _, md := range []string{
		"[x](javascript:alert(1))", "[x](JaVaScRiPt:alert(1))", "[x](java\tscript:alert(1))", "[x](data:text/html,<script>)", "[x](vbscript:x)", "[x](//evil.example/x)",
		"<a href=\"javascript:alert(1)\">x</a>", "![x](javascript:alert(1))", "<javascript:alert(1)>", "[x]: javascript:alert(1)\n\n[x]",
	} {
		h := string(RenderizarMarkdown(md))
		if strings.Contains(strings.ToLower(h), "href=\"javascript") || strings.Contains(strings.ToLower(h), "href=\"data:") || strings.Contains(h, "href=\"//") || strings.Contains(strings.ToLower(h), "src=\"javascript") || strings.Contains(h, "<a href=\"java") || strings.Contains(h, "<script") {
			t.Errorf("%q → %s", md, h)
		}
	}
	if h := string(RenderizarMarkdown("<script>x</script>")); strings.Contains(h, "<script>") || !strings.Contains(h, "&lt;script&gt;") {
		t.Errorf("script bruto: %s", h)
	}
	if h := string(RenderizarMarkdown("[ok](/p/orq/15/1) [a](#x) [m](mailto:a@b.c)")); strings.Count(h, "href=") != 3 {
		t.Errorf("links bons: %s", h)
	}
}

// cspEsperada é a CSP da spec (§5.1), escrita por extenso: o teste não a deriva da constante.
const cspEsperada = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

func TestCabecalhosDeSegurancaEmToda(t *testing.T) {
	f := nova(t, false)
	for _, c := range []struct{ m, p string }{{"GET", "/"}, {"GET", "/p/orq/15/1"}, {"GET", "/historico"}, {"GET", "/saude"}, {"GET", "/estilo.css"}, {"GET", "/estatico/app.css"}, {"GET", "/nao-existe"}, {"POST", "/p/orq/15/1/responder"}} {
		w := f.faz(req{metodo: c.m, caminho: c.p})
		h := w.Header()
		if h.Get("Content-Security-Policy") != cspEsperada || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s %s: cabeçalhos %v", c.m, c.p, h)
		}
	}
	if w := f.faz(req{metodo: "GET", caminho: "/estilo.css"}); w.Code != 404 {
		t.Errorf("/estilo.css = %d, quer 404 (saiu na story 1)", w.Code)
	}
}

func TestRespostaPostRedirectGet(t *testing.T) {
	f := nova(t, true)
	w := f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: form(url.Values{"opcao": {"2"}, "texto": {"linha 1\r\nlinha 2"}})})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/p/orq/15/1?registrada=1" {
		t.Fatalf("POST = %d %v %s", w.Code, w.Header(), corpo(w))
	}
	p := f.pend()
	if p.Estado != central.EstadoRespondida || *p.RespostaOpcao != 2 || *p.RespondidaPor != "ana" || *p.Resposta != "linha 1\nlinha 2" {
		t.Fatalf("pendência = %+v", p)
	}
	// O GET do redirecionamento mostra a confirmação e a resposta atual.
	g := f.faz(req{metodo: "GET", caminho: w.Header().Get("Location")})
	html := corpo(g)
	if g.Code != 200 || !strings.Contains(html, "Resposta registrada.") || !strings.Contains(html, "Trocar a resposta") || !strings.Contains(html, "opção 2 (B)") {
		t.Fatalf("GET depois do POST:\n%s", html)
	}
	// Recarregar o GET não reenvia nada: um evento só.
	f.faz(req{metodo: "GET", caminho: w.Header().Get("Location")})
	es, _ := f.st.Diario(context.Background(), nil, central.FiltroDiario{})
	n := 0
	for _, e := range es {
		if e.Tipo == "pend.responder" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d pend.responder", n)
	}
	// Trocar a resposta entra no histórico da página.
	f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: form(url.Values{"opcao": {"1"}})})
	if html := corpo(f.faz(req{metodo: "GET", caminho: "/p/orq/15/1"})); !strings.Contains(html, "Histórico de respostas") || strings.Count(html, "ana") < 3 {
		t.Fatalf("sem histórico:\n%s", html)
	}
}

func TestPostDeOutraOrigemRecusado(t *testing.T) {
	f := nova(t, true)
	corpoOK := form(url.Values{"opcao": {"1"}})
	for nome, cab := range map[string]map[string]string{
		"Origin de outro site":     {"Origin": "https://evil.example"},
		"Sec-Fetch-Site cross":     {"Sec-Fetch-Site": "cross-site"},
		"Sec-Fetch-Site same-site": {"Sec-Fetch-Site": "same-site"},
	} {
		w := f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: corpoOK, cab: cab})
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status %d", nome, w.Code)
		}
	}
	if p := f.pend(); p.Estado != central.EstadoAberta {
		t.Fatalf("gravou resposta de outra origem: %+v", p)
	}
	// A mesma origem passa.
	ok := f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: corpoOK, cab: map[string]string{"Origin": "http://pend.exemplo", "Sec-Fetch-Site": "same-origin"}})
	if ok.Code != http.StatusSeeOther {
		t.Fatalf("mesma origem: %d %s", ok.Code, corpo(ok))
	}
}

func TestUsuarioPeloCabecalhoSoNoSocketUnix(t *testing.T) {
	// Em TCP o cabeçalho é ignorado: respondida_por = local.
	tcp := nova(t, false)
	w := tcp.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: form(url.Values{"opcao": {"1"}}), cab: map[string]string{"X-Orq-User": "mallory"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("TCP: %d %s", w.Code, corpo(w))
	}
	if por := *tcp.pend().RespondidaPor; por != "local" {
		t.Fatalf("TCP respondida_por = %q", por)
	}
	// No Unix, vale o cabeçalho; sem ele, 401 e nada gravado.
	un := nova(t, true)
	un.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: form(url.Values{"opcao": {"1"}}), cab: map[string]string{"X-Orq-User": "maria"}})
	if por := *un.pend().RespondidaPor; por != "maria" {
		t.Fatalf("Unix respondida_por = %q", por)
	}
	sem := nova(t, true)
	for _, c := range []struct{ m, p string }{{"GET", "/"}, {"GET", "/p/orq/15/1"}, {"POST", "/p/orq/15/1/responder"}} {
		w := sem.faz(req{metodo: c.m, caminho: c.p, corpo: form(url.Values{"opcao": {"1"}}), cab: map[string]string{"X-Orq-User": ""}})
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s sem usuário: %d", c.m, c.p, w.Code)
		}
	}
	if sem.pend().Estado != central.EstadoAberta {
		t.Fatal("gravou sem usuário")
	}
	if w := sem.faz(req{metodo: "GET", caminho: "/saude", cab: map[string]string{"X-Orq-User": ""}}); w.Code != 200 {
		t.Errorf("/saude sem usuário: %d", w.Code)
	}
	// Usuário com controle é recusado.
	if w := sem.faz(req{metodo: "GET", caminho: "/", cab: map[string]string{"X-Orq-User": "a\x01b"}}); w.Code == 200 {
		t.Errorf("usuário com controle aceito")
	}
}

func TestHostEmTCPSoLoopback(t *testing.T) {
	f := nova(t, false)
	for _, h := range []string{"evil.example", "evil.example:8080", "127.0.0.1:9999", "10.0.0.1:8080"} {
		if w := f.faz(req{metodo: "GET", caminho: "/", cab: map[string]string{"Host": h}}); w.Code != http.StatusMisdirectedRequest {
			t.Errorf("Host %q: %d", h, w.Code)
		}
	}
	for _, h := range []string{"127.0.0.1:8080", "localhost:8080"} {
		if w := f.faz(req{metodo: "GET", caminho: "/", cab: map[string]string{"Host": h}}); w.Code != 200 {
			t.Errorf("Host %q: %d", h, w.Code)
		}
	}
}

func TestLimitesDoPost(t *testing.T) {
	f := nova(t, true)
	ruins := map[string]struct {
		corpo string
		quer  int
	}{
		"corpo de 17 KiB":      {form(url.Values{"opcao": {"1"}, "texto": {strings.Repeat("a", 17<<10)}}), http.StatusRequestEntityTooLarge},
		"texto de 8 KiB + 1":   {form(url.Values{"opcao": {"1"}, "texto": {strings.Repeat("a", 8<<10+1)}}), http.StatusRequestEntityTooLarge},
		"controle no texto":    {form(url.Values{"opcao": {"1"}, "texto": {"a\x00b"}}), http.StatusBadRequest},
		"escape no texto":      {form(url.Values{"opcao": {"1"}, "texto": {"a\x1b[2Jb"}}), http.StatusBadRequest},
		"opção inexistente":    {form(url.Values{"opcao": {"9"}}), http.StatusBadRequest},
		"opção não numérica":   {form(url.Values{"opcao": {"x"}}), http.StatusBadRequest},
		"sem opção na decisão": {form(url.Values{"texto": {"só texto"}}), http.StatusBadRequest},
		"vazio":                {"", http.StatusBadRequest},
	}
	for nome, c := range ruins {
		w := f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: c.corpo})
		if w.Code != c.quer {
			t.Errorf("%s: status %d, queria %d", nome, w.Code, c.quer)
		}
	}
	if f.pend().Estado != central.EstadoAberta {
		t.Fatal("gravou resposta recusada")
	}
	// Exatamente 8 KiB de texto passa.
	ok := f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: form(url.Values{"opcao": {"1"}, "texto": {strings.Repeat("a", 8<<10)}})})
	if ok.Code != http.StatusSeeOther {
		t.Fatalf("8 KiB: %d %s", ok.Code, corpo(ok))
	}
	// Outro Content-Type não é aceito.
	w := f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: `{"opcao":1}`, cab: map[string]string{"Content-Type": "application/json"}})
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("JSON: %d", w.Code)
	}
}

func TestResponderResolvidaEInexistente(t *testing.T) {
	f := nova(t, true)
	if _, err := f.st.ResolverPendencia(context.Background(), &f.co, refs.Pend{US: refs.US{Projeto: "orq", Ref: 15}, Numero: 1}, "feito"); err != nil {
		t.Fatal(err)
	}
	if w := f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: form(url.Values{"opcao": {"1"}})}); w.Code != http.StatusConflict {
		t.Errorf("resolvida: %d", w.Code)
	}
	html := corpo(f.faz(req{metodo: "GET", caminho: "/p/orq/15/1"}))
	if strings.Contains(html, "<form") || !strings.Contains(html, "Resolvida") {
		t.Errorf("página da resolvida:\n%s", html)
	}
	for _, p := range []string{"/p/orq/15/9", "/p/orq/99/1", "/p/orq/x/1", "/p/ORQ/15/1", "/p/orq/15/0"} {
		if w := f.faz(req{metodo: "GET", caminho: p}); w.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d", p, w.Code)
		}
	}
	if w := f.faz(req{metodo: "PUT", caminho: "/p/orq/15/1"}); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT: %d", w.Code)
	}
}

func TestIndiceEHistorico(t *testing.T) {
	f := nova(t, true)
	html := corpo(f.faz(req{metodo: "GET", caminho: "/"}))
	if !strings.Contains(html, "TG-15#P1") || !strings.Contains(html, `http-equiv="refresh" content="30"`) || !strings.Contains(html, "Esperando você") {
		t.Fatalf("índice:\n%s", html)
	}
	// Respondida muda de seção; os filtros escondem o resto.
	f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: form(url.Values{"opcao": {"1"}})})
	html = corpo(f.faz(req{metodo: "GET", caminho: "/"}))
	if !strings.Contains(html, `<span class="contagem">0</span>`) || !strings.Contains(html, "selo-respondida") {
		t.Fatalf("respondida no índice:\n%s", html)
	}
	for _, q := range []string{"?coord=outro", "?projeto=zeta", "?onda=77"} {
		if h := corpo(f.faz(req{metodo: "GET", caminho: "/" + q})); strings.Contains(h, "TG-15#P1</strong>") {
			t.Errorf("filtro %s não escondeu", q)
		}
	}
	if h := corpo(f.faz(req{metodo: "GET", caminho: "/?coord=coord&projeto=orq&onda=1"})); !strings.Contains(h, "TG-15#P1</strong>") {
		t.Errorf("filtro certo escondeu:\n%s", h)
	}
	// Histórico paginado: 25 resolvidas → 2 páginas.
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		res, err := f.st.RegistrarPendencia(ctx, central.NovaPendencia{US: refs.US{Projeto: "orq", Ref: 15}, Tipo: "duvida", Titulo: "dúvida " + strings.Repeat("x", i%3), Autor: "w", Detalhe: "d", Slug: "s" + strings.Repeat("a", i), Hash: "h"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.st.ResolverPendencia(ctx, &f.co, refs.Pend{US: refs.US{Projeto: "orq", Ref: 15}, Numero: res.Pendencia.Numero}, "ok"); err != nil {
			t.Fatal(err)
		}
	}
	p1 := corpo(f.faz(req{metodo: "GET", caminho: "/historico"}))
	p2 := corpo(f.faz(req{metodo: "GET", caminho: "/historico?pagina=2"}))
	if strings.Count(p1, `class="cartao"`) != 20 || strings.Count(p2, `class="cartao"`) != 5 || !strings.Contains(p1, "página 1 de 2") || !strings.Contains(p1, "pagina=2") || !strings.Contains(p2, "pagina=1") {
		t.Fatalf("páginas: %d / %d\n%s", strings.Count(p1, "cartao"), strings.Count(p2, "cartao"), p1)
	}
	if h := corpo(f.faz(req{metodo: "GET", caminho: "/historico?pagina=99"})); !strings.Contains(h, "página 2 de 2") {
		t.Errorf("página fora do intervalo: %s", h)
	}
}

func TestLogSoTemRotaEStatus(t *testing.T) {
	f := nova(t, true)
	f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder?segredo=1", corpo: form(url.Values{"opcao": {"1"}, "texto": {"MINHA-SENHA-123"}})})
	f.faz(req{metodo: "GET", caminho: "/p/orq/15/1?x=MINHA-SENHA-123"})
	if len(f.log) < 2 {
		t.Fatalf("log = %v", f.log)
	}
	for _, l := range f.log {
		if strings.Contains(l, "SENHA") || strings.Contains(l, "segredo") || strings.Contains(l, "ana") || strings.Contains(l, "?") {
			t.Errorf("linha de log com dado: %q", l)
		}
	}
	if f.log[0] != "web POST /p/{projeto}/{ref}/{n}/responder 303" {
		t.Errorf("log[0] = %q", f.log[0])
	}
}

func TestSaude(t *testing.T) {
	f := nova(t, true)
	if w := f.faz(req{metodo: "GET", caminho: "/saude"}); w.Code != 200 || strings.TrimSpace(corpo(w)) != "ok" {
		t.Fatalf("saude = %d %s", w.Code, corpo(w))
	}
}

func TestNaoServeArquivosNemSnapshot(t *testing.T) {
	f := nova(t, true)
	for _, p := range []string{"/snapshot", "/etc/passwd", "/../../etc/passwd", "/tmpl/base.html", "/estilo.css/../web.go", "/p/orq/15/1/../../../../etc/passwd", "/central.db"} {
		w := f.faz(req{metodo: "GET", caminho: p})
		if w.Code == 200 && p != "/estilo.css/../web.go" {
			t.Errorf("GET %s = 200", p)
		}
		if b := corpo(w); strings.Contains(b, "root:") || strings.Contains(b, "package web") || strings.Contains(b, `"versao"`) {
			t.Errorf("GET %s vazou: %s", p, b)
		}
	}
}

func TestAbrirEscuta(t *testing.T) {
	dir, err := os.MkdirTemp("", "orqweb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	env := func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return dir
		}
		return ""
	}
	e, err := Abrir("unix:", env)
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "orq", "web.sock")
	st, err := os.Stat(sock)
	if err != nil || st.Mode().Perm() != 0o600 || !e.Unix {
		t.Fatalf("socket = %v, %v", st, err)
	}
	if d, _ := os.Stat(filepath.Dir(sock)); d.Mode().Perm() != 0o700 {
		t.Errorf("pasta = %v", d.Mode())
	}
	// Segundo no mesmo socket: recusado, sem apagar o do primeiro.
	if _, err := Abrir("unix:"+sock, env); err == nil || !strings.Contains(err.Error(), "web_nao_abriu") {
		t.Fatalf("segundo = %v", err)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatal("o segundo apagou o socket do primeiro")
	}
	_ = e.Close()
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket ficou depois do Close: %v", err)
	}
	// Sobra de um daemon que caiu (socket sem dono): é reaproveitada.
	l, _ := Abrir("unix:"+sock, env)
	if f, ok := l.Listener.(interface{ SetUnlinkOnClose(bool) }); ok {
		f.SetUnlinkOnClose(false)
	}
	_ = l.Close()
	if _, err := os.Stat(sock); err != nil {
		t.Fatal("esperava a sobra do socket")
	}
	e2, err := Abrir("unix:"+sock, env)
	if err != nil {
		t.Fatalf("sobra não foi reaproveitada: %v", err)
	}
	_ = e2.Close()
	// Arquivo comum no caminho não é apagado.
	comum := filepath.Join(dir, "comum")
	_ = os.WriteFile(comum, []byte("x"), 0o600)
	if _, err := Abrir("unix:"+comum, env); err == nil {
		t.Fatal("sobrescreveu arquivo comum")
	}
	if _, err := os.Stat(comum); err != nil {
		t.Fatal("apagou o arquivo comum")
	}
	// Endereços recusados.
	for _, s := range []string{"0.0.0.0:8080", ":8080", "192.168.1.5:80", "8080", "unix:rel/web.sock", "pend.exemplo:443"} {
		if _, err := Abrir(s, env); err == nil {
			t.Errorf("aceitou %q", s)
		}
	}
	if _, err := Abrir("unix:", func(string) string { return "" }); err == nil {
		t.Error("padrão sem XDG_RUNTIME_DIR")
	}
	tcp, err := Abrir("127.0.0.1:0", env)
	if err != nil || tcp.Unix || tcp.Porta == 0 {
		t.Fatalf("tcp = %+v, %v", tcp, err)
	}
	_ = tcp.Close()
}
