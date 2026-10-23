package web

import (
	"io/fs"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// arquivosEstaticos é a lista fechada do que estatico/ embute (spec §8.1): arquivo a mais ou a
// menos falha. Mudar a lista é mudar o que o binário serve, então é decisão de revisão.
var arquivosEstaticos = []string{
	"app.css",
	"app.js",
	"fontes/LICENSE-carlito.txt",
	"fontes/LICENSE-inter-tight.txt",
	"fontes/carlito-latin-400-normal.woff2",
	"fontes/carlito-latin-700-normal.woff2",
	"fontes/inter-tight-latin-600-normal.woff2",
	"fontes/inter-tight-latin-700-normal.woff2",
	"htmx.min.js",
	"orq-banner-light.png",
	"orq-icon-64.png",
}

func TestEstaticoEmbutidoEhAListaFechada(t *testing.T) {
	var achados []string
	err := fs.WalkDir(estaticoFS, "estatico", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			achados = append(achados, strings.TrimPrefix(p, "estatico/"))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(achados)
	if strings.Join(achados, "\n") != strings.Join(arquivosEstaticos, "\n") {
		t.Errorf("estatico/ embutido:\n%s\nquer:\n%s", strings.Join(achados, "\n"), strings.Join(arquivosEstaticos, "\n"))
	}
}

func TestAppCSSTemOTemaEAsFontes(t *testing.T) {
	b, err := estaticoFS.ReadFile("estatico/app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	if !strings.Contains(css, `data-theme=basis-interno`) && !strings.Contains(css, `data-theme="basis-interno"`) {
		t.Error("app.css sem o tema basis-interno (data-theme)")
	}
	if n := strings.Count(css, "@font-face"); n != 4 {
		t.Errorf("@font-face = %d, quer 4", n)
	}
	for _, f := range []string{"carlito-latin-400-normal", "carlito-latin-700-normal", "inter-tight-latin-600-normal", "inter-tight-latin-700-normal"} {
		if !strings.Contains(css, "/estatico/fontes/"+f+".woff2") {
			t.Errorf("app.css não aponta para a fonte %s", f)
		}
		if _, err := fs.Stat(estaticoFS, "estatico/fontes/"+f+".woff2"); err != nil {
			t.Errorf("fonte %s não está embutida", f)
		}
	}
	// O comentário de licença do Tailwind e o xmlns de um SVG em data: têm http(s)://; o que a CSP
	// barraria é url() ou @import externo.
	if externo := regexp.MustCompile(`(?i)(url\(\s*["']?|@import\s*["']?)(https?:)?//`).FindString(css); externo != "" || strings.Contains(css, "fonts.googleapis") || strings.Contains(css, "fonts.gstatic") {
		t.Errorf("app.css referencia recurso externo (%q); a CSP só aceita 'self'", externo)
	}
}

func TestHashDaURLMudaComOArquivo(t *testing.T) {
	a, b := hashDe([]byte("a{}")), hashDe([]byte("a{ }"))
	if a == b || len(a) != 12 {
		t.Errorf("hashes %q %q", a, b)
	}
	if hashDe([]byte("a{}")) != a {
		t.Error("o hash não é determinístico")
	}
}

var reVersao = regexp.MustCompile(`/estatico/(app\.css|htmx\.min\.js|app\.js)\?v=([0-9a-f]{12})`)

func TestPaginaReferenciaOsEstaticosComHashEServeComCacheCerto(t *testing.T) {
	f := nova(t, false)
	w := f.faz(req{metodo: "GET", caminho: "/"})
	html := corpo(w)
	achados := map[string]string{}
	for _, m := range reVersao.FindAllStringSubmatch(html, -1) {
		achados[m[1]] = m[2]
	}
	if len(achados) != 3 {
		t.Fatalf("a página deveria referenciar app.css, htmx.min.js e app.js com ?v=: %v", achados)
	}
	for _, quer := range []string{`<link rel="icon" type="image/png" href="/estatico/orq-icon-64.png">`, `<meta name="htmx-config" content='{"allowEval":false,"allowScriptTags":false,"includeIndicatorStyles":false,"historyCacheSize":0,"historyRestoreAsHxRequest":false,"attributesToSettle":["class","width","height"],"responseHandling":[{"code":"204","swap":false},{"code":"[23]..","swap":true},{"code":"[45]..","swap":true,"error":true}]}'>`, `data-theme="basis-interno"`, `aria-current="page"`} {
		if !strings.Contains(html, quer) {
			t.Errorf("a página não tem %s", quer)
		}
	}
	if strings.Contains(html, "estilo.css") || strings.Contains(html, "fonts.googleapis") {
		t.Error("a página ainda cita /estilo.css ou Google Fonts")
	}

	for nome, v := range achados {
		w := f.faz(req{metodo: "GET", caminho: "/estatico/" + nome + "?v=" + v})
		if w.Code != 200 || w.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
			t.Errorf("%s com hash: %d %q", nome, w.Code, w.Header().Get("Cache-Control"))
		}
		// Hash que não é o do conteúdo embutido (ou ausente) não ganha immutable.
		for _, q := range []string{"", "?v=000000000000"} {
			w := f.faz(req{metodo: "GET", caminho: "/estatico/" + nome + q})
			if w.Header().Get("Cache-Control") != "private, max-age=86400" {
				t.Errorf("%s%s: Cache-Control %q", nome, q, w.Header().Get("Cache-Control"))
			}
		}
	}
	for _, c := range []struct{ caminho, tipo string }{
		{"app.css", "text/css"}, {"htmx.min.js", "text/javascript"}, {"app.js", "text/javascript"},
		{"fontes/carlito-latin-400-normal.woff2", "font/woff2"}, {"orq-icon-64.png", "image/png"}, {"orq-banner-light.png", "image/png"},
		{"fontes/LICENSE-carlito.txt", "text/plain"},
	} {
		w := f.faz(req{metodo: "GET", caminho: "/estatico/" + c.caminho})
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), c.tipo) {
			t.Errorf("%s: %d %q", c.caminho, w.Code, w.Header().Get("Content-Type"))
		}
		cache := w.Header().Get("Cache-Control")
		if strings.Contains(cache, "public") || !strings.HasPrefix(cache, "private") {
			t.Errorf("%s: Cache-Control %q (nunca public)", c.caminho, cache)
		}
		if w.Header().Get("Content-Security-Policy") != cspEsperada || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s sem os cabeçalhos de segurança", c.caminho)
		}
		if strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s servido como HTML", c.caminho)
		}
	}
}

func TestEstaticoNaoListaNemSaiDoEmbed(t *testing.T) {
	f := nova(t, true)
	for _, p := range []string{"/estatico/", "/estatico", "/estatico/fontes", "/estatico/fontes/", "/estatico/../web.go", "/estatico/%2e%2e/web.go", "/estatico/fontes/../../go.mod", "/estatico/nao-existe.css", "/estatico/.", "/estatico//app.css", "/estilo.css"} {
		w := f.faz(req{metodo: "GET", caminho: p})
		if w.Code == 200 {
			t.Errorf("GET %s = 200\n%s", p, corpo(w))
		}
		if b := corpo(w); strings.Contains(b, "package web") || strings.Contains(b, "module github.com") {
			t.Errorf("GET %s vazou: %s", p, b)
		}
	}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if w := f.faz(req{metodo: m, caminho: "/estatico/app.css"}); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /estatico/app.css = %d, quer 405", m, w.Code)
		}
	}
}

func TestEstaticoExigeUsuarioNoSocketUnix(t *testing.T) {
	f := nova(t, true)
	if w := f.faz(req{metodo: "GET", caminho: "/estatico/app.css", cab: map[string]string{"X-Orq-User": ""}}); w.Code != 401 {
		t.Errorf("sem cabeçalho = %d, quer 401", w.Code)
	}
	if w := f.faz(req{metodo: "GET", caminho: "/estatico/app.css"}); w.Code != 200 {
		t.Errorf("com cabeçalho = %d", w.Code)
	}
}

func TestPaginasNaoTemScriptNemEstiloInline(t *testing.T) {
	f := nova(t, false)
	for _, p := range []string{"/", "/pendencias", "/p/orq/15/1", "/historico", "/nao-existe"} {
		w := f.faz(req{metodo: "GET", caminho: p})
		// Até o <main>: cabeçalho, abas e sprite são do orq. O que vem depois é texto de agente (a
		// fixture traz "onerror=" escapado), coberto pelos testes de escape.
		h, _, _ := strings.Cut(corpo(w), "<main")
		if temInline(h) {
			t.Errorf("%s tem <script> sem src, style= ou atributo de evento", p)
		}
		if strings.Contains(corpo(w), "<script>") {
			t.Errorf("%s tem <script> inline", p)
		}
	}
}
