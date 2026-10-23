package web

import (
	stdhtml "html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// Testes da story 3 (#24): a aba Pendências, o corpo da pendência, o POST por htmx, os erros
// dentro do formulário, a cascata, a troca de rotas e a exceção do histórico.

const hxAtual = "http://127.0.0.1:8080"

// hx faz uma requisição com HX-Request e o HX-Current-URL da aba em que a pessoa está.
func (f *fixture) hx(metodo, caminho, atual, corpoForm string, extra ...string) *httptest.ResponseRecorder {
	f.t.Helper()
	cab := map[string]string{"HX-Request": "true"}
	if atual != "" {
		cab["HX-Current-URL"] = hxAtual + atual
	}
	for i := 0; i+1 < len(extra); i += 2 {
		cab[extra[i]] = extra[i+1]
	}
	return f.faz(req{metodo: metodo, caminho: caminho, corpo: corpoForm, cab: cab})
}

var detailsAbertoRe = regexp.MustCompile(`<details[^>]*\sopen[\s>=]`)

func semDetailsAberto(t *testing.T, nome, html string) {
	t.Helper()
	if detailsAbertoRe.MatchString(html) {
		t.Errorf("%s serve <details open> (o corpo da pendência nunca carregaria, spec §2.3 M6)", nome)
	}
}

func TestTrocaDeRotas(t *testing.T) {
	c := semeada(t)
	// / é a Central, aba Todos.
	code, html := c.get("/")
	if code != 200 {
		t.Fatalf("GET / = %d", code)
	}
	contem(t, html, `<h1 class="t-pagina">Ondas abertas</h1>`, `<a href="/" aria-current="page">Todos`, `<a href="/pendencias">Pendências`)
	naoContem(t, html, `id="pend-conteudo"`)
	// /central é 308 para /, com a query.
	for de, para := range map[string]string{"/central": "/", "/central?x=1": "/?x=1"} {
		w := c.f.faz(req{metodo: "GET", caminho: de})
		if w.Code != http.StatusPermanentRedirect || w.Header().Get("Location") != para {
			t.Errorf("GET %s = %d %q, quer 308 %q", de, w.Code, w.Header().Get("Location"), para)
		}
	}
	// Os filtros da página antiga passam para /pendencias com a mesma query.
	for de, para := range map[string]string{
		"/?coord=aurora":                    "/pendencias?coord=aurora",
		"/?projeto=nuvem&coord=cais&onda=5": "/pendencias?projeto=nuvem&coord=cais&onda=5",
		"/?onda=abc":                        "/pendencias?onda=abc",
		"/?coord=":                          "/pendencias?coord=",
	} {
		w := c.f.faz(req{metodo: "GET", caminho: de})
		if w.Code != http.StatusPermanentRedirect || w.Header().Get("Location") != para {
			t.Errorf("GET %s = %d %q, quer 308 %q", de, w.Code, w.Header().Get("Location"), para)
		}
	}
	// A aba Pendências aponta para /pendencias e está ativa lá.
	_, html = c.get("/pendencias")
	contem(t, html, `<a href="/pendencias" aria-current="page">Pendências`, `<a href="/">Todos`)
	// A situação da Central leva para /pendencias.
	_, html = c.get("/")
	contem(t, html, `<a href="/pendencias"><svg class="ic" aria-hidden="true"><use href="#i-user"/></svg>`)
	// Só GET: POST em / e em /pendencias é 405, /estilo.css não existe.
	for _, p := range []string{"/", "/pendencias", "/central"} {
		if w := c.f.faz(req{metodo: "POST", caminho: p, corpo: "x=1"}); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, quer 405", p, w.Code)
		}
	}
	if w := c.f.faz(req{metodo: "GET", caminho: "/estilo.css"}); w.Code != 404 {
		t.Errorf("/estilo.css = %d", w.Code)
	}
}

func TestPendenciasPaginaEFragmento(t *testing.T) {
	c := semeada(t)
	w := c.f.faz(req{metodo: "GET", caminho: "/pendencias"})
	html := corpo(w)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	contem(t, html, "<!doctype html>", `id="pend-conteudo"`, `id="lista-esp"`, `id="lista-resp"`, `id="vazio-esp"`, `id="vazio-resp"`, `id="grupo-n-esp">5<`, `id="grupo-n-resp">1<`,
		`id="vivo"`, `id="aviso-lista"`, `id="pend-aviso"`, `id="aviso"`, `hx-trigger="toggle once"`, `hx-get="/p/catalogo/405/1/corpo"`, "Idade dos dados")
	naoContem(t, html, "http-equiv", "<textarea", `name="texto"`, "<blockquote") // o corpo não vai na página: só o espaço do carregamento
	semDetailsAberto(t, "/pendencias", html)
	if temInline(html) {
		t.Error("/pendencias tem script ou estilo inline")
	}

	// Com HX-Request, só o fragmento.
	w = c.f.hx("GET", "/pendencias?projeto=catalogo", "/pendencias", "")
	frag := corpo(w)
	if w.Code != 200 || !strings.HasPrefix(frag, `<section id="pend-conteudo"`) || strings.Contains(frag, "<html") || strings.Contains(frag, "<body") || strings.Contains(frag, "<nav") {
		t.Fatalf("fragmento: %d\n%s", w.Code, frag[:min(len(frag), 300)])
	}
	if got := w.Header().Get("HX-Push-Url"); got != "/pendencias?projeto=catalogo" {
		t.Errorf("HX-Push-Url = %q", got)
	}
	if w.Header().Get("HX-Replace-Url") != "" {
		t.Errorf("HX-Replace-Url sem normalização: %q", w.Header().Get("HX-Replace-Url"))
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Security-Policy") != csp {
		t.Errorf("cabeçalhos do fragmento: %v", w.Header())
	}
	semDetailsAberto(t, "fragmento", frag)

	// B3: a restauração do histórico devolve a página inteira, com ou sem HX-Request.
	for nome, cab := range map[string][]string{
		"HX-Request e HX-History-Restore-Request": {"HX-History-Restore-Request", "true"},
		"só HX-History-Restore-Request":           {"HX-History-Restore-Request", "true", "HX-Request", ""},
	} {
		w := c.f.hx("GET", "/pendencias?projeto=catalogo", "/pendencias", "", cab...)
		h := corpo(w)
		if w.Code != 200 || !strings.Contains(h, "<!doctype html>") || !strings.Contains(h, "<nav") || w.Header().Get("HX-Push-Url") != "" {
			t.Errorf("%s: %d, devolveu o fragmento (spec §2.2, B3)", nome, w.Code)
		}
	}
	if !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "HX-Request") {
		t.Errorf("Vary = %v", w.Header().Values("Vary"))
	}
}

func TestPendenciasFiltroNormalizaCombinacaoImpossivel(t *testing.T) {
	c := semeada(t)
	// Página inteira: 303 para a URL corrigida (o link velho abre a lista sem o filtro).
	for de, para := range map[string]string{
		"/pendencias?projeto=zeta":                    "/pendencias",
		"/pendencias?projeto=catalogo&coord=bussola":  "/pendencias?projeto=catalogo",
		"/pendencias?coord=aurora&onda=3":             "/pendencias?coord=aurora",
		"/pendencias?onda=abc":                        "/pendencias",
		"/pendencias?onda=99999999999999999999":       "/pendencias",
		"/pendencias?onda=05":                         "/pendencias",
		"/pendencias?projeto=nuvem&coord=cais&onda=1": "/pendencias?coord=cais&projeto=nuvem",
		"/pendencias?projeto=agenda&coord=aurora":     "/pendencias?projeto=agenda",
	} {
		w := c.f.faz(req{metodo: "GET", caminho: de})
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != para {
			t.Errorf("GET %s = %d %q, quer 303 %q", de, w.Code, w.Header().Get("Location"), para)
		}
	}
	// Fragmento: já normalizado, com HX-Replace-Url (sem push).
	w := c.f.hx("GET", "/pendencias?projeto=catalogo&coord=bussola&onda=3", "/pendencias", "")
	if w.Code != 200 || w.Header().Get("HX-Replace-Url") != "/pendencias?projeto=catalogo" || w.Header().Get("HX-Push-Url") != "" {
		t.Errorf("fragmento normalizado: %d %v", w.Code, w.Header())
	}
	if h := corpo(w); strings.Contains(h, `<option value="bussola" selected`) || strings.Contains(h, `<option value="3"`) {
		t.Errorf("o fragmento ainda traz a combinação impossível:\n%s", h)
	}
	// Combinação válida: as opções e os selecionados certos, só as pendências do filtro.
	w = c.f.faz(req{metodo: "GET", caminho: "/pendencias?projeto=nuvem&coord=cais&onda=5"})
	h := corpo(w)
	if w.Code != 200 {
		t.Fatalf("combinação válida = %d", w.Code)
	}
	contem(t, h, `<option value="nuvem" selected>`, `<option value="cais" selected>`, `<option value="5" selected>`, `id="grupo-n-esp">2<`, `id="grupo-n-resp">0<`, "pend-nuvem-78-1", "pend-nuvem-78-2")
	naoContem(t, h, "pend-catalogo-405-1", "pend-faturamento-513-1")
	// A onda sem pendência continua escolhível e dá a lista vazia com a frase.
	_, h = c.get("/pendencias?projeto=agenda&coord=bussola&onda=4")
	contem(t, h, `<option value="4" selected>`, `id="grupo-n-esp">0<`, `<p id="vazio-esp" class="vazio-linha vazio-caixa">Nenhuma pendência esperando para este filtro.</p>`)
}

func TestCascataOpcoesDoSnapshot(t *testing.T) {
	// O coordenador lista só quem tem onda com US do projeto; a onda, só as desse coordenador com US
	// desse projeto; sem projeto, todos. As opções saem do snapshot, não das pendências.
	snap := snapV(nil,
		ondaV(1, "a", "merge", usV("p", 1, "exec", nil)),
		ondaV(2, "a", "merge", usV("p", 2, "exec", nil), usV("q", 3, "exec", nil)),
		ondaV(3, "b", "merge", usV("q", 4, "exec", nil)),
	)
	snap.Coordenadores = append(snap.Coordenadores, central.CoordSnapshot{Coordenador: central.Coordenador{Nome: "b"}, Ondas: []central.Onda{snap.Coordenadores[0].Ondas[2]}})
	snap.Coordenadores[0].Ondas = snap.Coordenadores[0].Ondas[:2]
	snap.Coordenadores[0].Nome = "a"
	valores := func(os []OpcaoFiltro) string {
		var v []string
		for _, o := range os {
			v = append(v, o.Valor)
		}
		return strings.Join(v, ",")
	}
	casos := []struct {
		f                       FiltroPend
		projetos, coords, ondas string
		norm                    bool
	}{
		{FiltroPend{}, "p,q", "a,b", "1,2,3", false},
		{FiltroPend{Projeto: "p"}, "p,q", "a", "1,2", false},
		{FiltroPend{Projeto: "q"}, "p,q", "a,b", "2,3", false},
		{FiltroPend{Projeto: "q", Coord: "b"}, "p,q", "a,b", "3", false},
		{FiltroPend{Projeto: "p", Coord: "b"}, "p,q", "a", "1,2", true},
		{FiltroPend{Coord: "b"}, "p,q", "a,b", "3", false},
		{FiltroPend{Projeto: "p", Coord: "a", Onda: "3"}, "p,q", "a", "1,2", true},
		{FiltroPend{Projeto: "zeta", Coord: "a", Onda: "1"}, "p,q", "a,b", "1,2,3", true},
		{FiltroPend{Onda: "x"}, "p,q", "a,b", "1,2,3", true},
	}
	for _, k := range casos {
		c := opcoesCascata(snap, k.f)
		if valores(c.Projetos) != k.projetos || valores(c.Coords) != k.coords || valores(c.Ondas) != k.ondas || c.Normalizado != k.norm {
			t.Errorf("%+v: projetos %s coords %s ondas %s norm %v; quer %s / %s / %s / %v", k.f, valores(c.Projetos), valores(c.Coords), valores(c.Ondas), c.Normalizado, k.projetos, k.coords, k.ondas, k.norm)
		}
	}
	// Nenhuma combinação impossível: para toda mistura de valores (válidos e lixo), o resultado é
	// uma combinação que existe, e normalizar de novo não muda nada.
	brutos := []string{"", "p", "q", "zeta", "a", "b", "1", "2", "3", "9", "x", "01"}
	for _, p := range brutos {
		for _, co := range brutos {
			for _, o := range brutos {
				r := opcoesCascata(snap, FiltroPend{Projeto: p, Coord: co, Onda: o})
				f := r.Filtro
				if f.Onda != "" && !contemOpcao(r.Ondas, f.Onda) || f.Coord != "" && !contemOpcao(r.Coords, f.Coord) || f.Projeto != "" && !contemOpcao(r.Projetos, f.Projeto) {
					t.Fatalf("%q/%q/%q → %+v não está nas próprias opções", p, co, o, f)
				}
				if f.Onda != "" {
					ok := false
					for _, co2 := range snap.Coordenadores {
						for _, on := range co2.Ondas {
							temP := f.Projeto == ""
							for _, u := range on.US {
								if u.Projeto == f.Projeto {
									temP = true
								}
							}
							if strconvID(on.ID) == f.Onda && (f.Coord == "" || co2.Nome == f.Coord) && temP {
								ok = true
							}
						}
					}
					if !ok {
						t.Fatalf("%q/%q/%q → %+v é uma combinação impossível", p, co, o, f)
					}
				}
				if again := opcoesCascata(snap, f); again.Normalizado || again.Filtro != f {
					t.Fatalf("%+v não é ponto fixo: %+v", f, again)
				}
			}
		}
	}
}

func strconvID(id int64) string { return strconv.FormatInt(id, 10) }

func refsPend(projeto string, ref, n int) refs.Pend {
	return refs.Pend{US: refs.US{Projeto: projeto, Ref: ref}, Numero: n}
}

func contemOpcao(os []OpcaoFiltro, v string) bool {
	for _, o := range os {
		if o.Valor == v {
			return true
		}
	}
	return false
}

func TestCorpoDaPendencia(t *testing.T) {
	f := nova(t, true)
	w := f.hx("GET", "/p/orq/15/1/corpo", "/", "")
	html := corpo(w)
	if w.Code != 200 {
		t.Fatalf("status %d\n%s", w.Code, html)
	}
	contem(t, html, `<form class="resposta" method="post" action="/p/orq/15/1/responder" hx-post="/p/orq/15/1/responder" hx-sync="closest #principal:queue all" hx-target="closest .pend" hx-swap="outerHTML">`,
		`<div class="erro" role="alert"></div>`, `name="texto"`, `name="opcao" value="1"`, `name="opcao" value="2"`, "recomendada", "Enviar resposta", `href="/p/orq/15/1"`)
	// O markdown do agente nunca vira HTML: o <script> e o <img onerror> saem escapados.
	naoContem(t, html, "<script", "<img", "javascript:", "<details", "hx-include")
	contem(t, html, "&lt;script&gt;")
	// Na aba Pendências o formulário inclui o #comp (na Central o campo não existe e o htmx reclamaria).
	if h := corpo(f.hx("GET", "/p/orq/15/1/corpo", "/pendencias", "")); !strings.Contains(h, `hx-include="#comp"`) {
		t.Errorf("o formulário da aba Pendências não inclui o #comp:\n%s", h)
	}
	if w.Header().Get("HX-Retarget") != "" || w.Header().Get("HX-Reswap") != "" {
		t.Errorf("o /corpo não pode mandar HX-Retarget nem HX-Reswap: %v", w.Header())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Security-Policy") != csp {
		t.Errorf("cabeçalhos: %v", w.Header())
	}
	// Sem HX-Request, vai para a página da pendência.
	if w := f.faz(req{metodo: "GET", caminho: "/p/orq/15/1/corpo"}); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/p/orq/15/1" {
		t.Errorf("sem HX-Request: %d %q", w.Code, w.Header().Get("Location"))
	}
	// Respondida: mostra a resposta e o botão de trocar; ?trocar=1 devolve o formulário com a atual.
	f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: form(url.Values{"opcao": {"2"}, "texto": {"porque sim"}})})
	html = corpo(f.hx("GET", "/p/orq/15/1/corpo", "/", ""))
	contem(t, html, "Opção 2", "<b>B</b>", "respondida por ana", "porque sim", `hx-get="/p/orq/15/1/corpo?trocar=1"`, `hx-target="closest .pend-corpo"`, `hx-swap="innerHTML"`, "Trocar a resposta")
	naoContem(t, html, "<form")
	html = corpo(f.hx("GET", "/p/orq/15/1/corpo?trocar=1", "/", ""))
	contem(t, html, "<form", `value="2" checked`, ">porque sim</textarea>", "Sua nova escolha", "Trocando a resposta de ana")
	// Inexistente: erro no corpo, sem retarget (o <summary> fica).
	for _, p := range []string{"/p/orq/15/9/corpo", "/p/orq/99/1/corpo", "/p/orq/x/1/corpo"} {
		w := f.hx("GET", p, "/", "")
		if w.Code != 404 || w.Header().Get("HX-Retarget") != "" || w.Header().Get("HX-Reswap") != "" || !strings.Contains(corpo(w), `role="alert"`) {
			t.Errorf("GET %s = %d %v", p, w.Code, w.Header())
		}
	}
	// Resolvida: sem formulário.
	if _, err := f.st.ResolverPendencia(t.Context(), &f.co, refsPend("orq", 15, 1), "feito"); err != nil {
		t.Fatal(err)
	}
	html = corpo(f.hx("GET", "/p/orq/15/1/corpo", "/", ""))
	naoContem(t, html, "<form")
	contem(t, html, "Resolvida")
}

func TestPostComHXRequestNaUS(t *testing.T) {
	c := semeada(t)
	_, antes := c.get("/")
	n0 := strings.Count(antes, `id="aba-n-pendencias"`)
	if n0 != 1 {
		t.Fatalf("aba Pendências: %d", n0)
	}
	w := c.f.hx("POST", "/p/catalogo/405/1/responder", "/", form(url.Values{"opcao": {"1"}, "texto": {"autorizado"}}))
	html := corpo(w)
	if w.Code != 200 {
		t.Fatalf("status %d\n%s", w.Code, html)
	}
	// O item respondido e fechado é a resposta principal.
	contem(t, html, `<details class="pend pend-ok pend-nova" id="pend-catalogo-405-1"`, "respondida", `hx-get="/p/catalogo/405/1/corpo"`)
	semDetailsAberto(t, "resposta", html)
	// As contagens vão por oob com id estável: a aba, a linha de situação, a barra da onda e a da US.
	contem(t, html, `id="aba-n-pendencias"`, `id="situacao"`, `id="onda-1-barra"`, `id="us-catalogo-405-barra"`, `id="aviso"`, "Resposta enviada: TG-405#P1. O coordenador é avisado.")
	if strings.Count(html, `hx-swap-oob="true"`) != 5 {
		t.Errorf("oob true = %d, quer 5 (aba, situação, onda, US, aviso)", strings.Count(html, `hx-swap-oob="true"`))
	}
	naoContem(t, html, "afterbegin", `id="lista-resp"`, `id="vivo"`)
	// A contagem da aba desceu de 5 para 4.
	if m := regexp.MustCompile(`id="aba-n-pendencias"[^>]*>(\d+)<`).FindStringSubmatch(html); m == nil || m[1] != "4" {
		t.Errorf("contagem da aba = %v, quer 3", m)
	}
	// A resposta foi gravada pelo mesmo contrato: texto, autor local (TCP) e o evento.
	p, err := c.st.VerPendencia(c.ctx, nil, refsPend("catalogo", 405, 1))
	if err != nil || p.Estado != central.EstadoRespondida || derefStr(p.Resposta) != "autorizado" || derefStr(p.RespondidaPor) != "local" {
		t.Fatalf("pendência = %+v %v", p, err)
	}
	// Trocar a resposta: o item no lugar; as contagens são as do servidor e não mudam.
	w = c.f.hx("POST", "/p/catalogo/405/1/responder", "/", form(url.Values{"opcao": {"1"}, "texto": {"autorizado de novo"}}))
	html = corpo(w)
	contem(t, html, `id="pend-catalogo-405-1"`, "Resposta trocada: TG-405#P1")
	if m := regexp.MustCompile(`id="aba-n-pendencias"[^>]*>(\d+)<`).FindStringSubmatch(html); m == nil || m[1] != "4" {
		t.Errorf("a troca mudou a contagem da aba: %v", m)
	}
}

func TestPostComHXRequestNaAbaPendencias(t *testing.T) {
	c := semeada(t)
	// Filtro numa onda com uma só pendência esperando: depois da resposta o grupo fica vazio.
	atual := "/pendencias?onda=3"
	w := c.f.hx("POST", "/p/faturamento/513/1/responder", atual, form(url.Values{"texto": {"sim"}}))
	html := corpo(w)
	if w.Code != 200 {
		t.Fatalf("status %d\n%s", w.Code, html)
	}
	// A resposta principal é vazia (o outerHTML remove o item de cima): tudo é oob.
	principal := html
	for _, re := range []string{
		`(?s)<div hx-swap-oob="afterbegin:#lista-resp">.*?</details></div>`,
		`<span [^>]*hx-swap-oob="true">[^<]*</span>`,
		`<p id="vazio-[a-z]+"[^>]*hx-swap-oob="true">[^<]*</p>`,
		`<div id="[a-z]+"[^>]*hx-swap-oob="true"[^>]*>[^<]*</div>`,
		`<input type="hidden" id="comp"[^>]*>`,
	} {
		principal = regexp.MustCompile(re).ReplaceAllString(principal, "")
	}
	if strings.TrimSpace(principal) != "" {
		t.Errorf("a resposta principal não é vazia:\n%s", principal)
	}
	// O item entra em cima de #lista-resp com o embrulho (sem o embrulho o item some dos dois grupos).
	contem(t, html, `<div hx-swap-oob="afterbegin:#lista-resp"><details class="pend pend-ok pend-nova" id="pend-faturamento-513-1"`)
	naoContem(t, html, `<details class="pend pend-ok pend-nova" id="pend-faturamento-513-1" hx-swap-oob`)
	// As contagens dos grupos e as frases dos grupos vazios.
	contem(t, html, `id="grupo-n-esp" hx-swap-oob="true">0<`, `id="grupo-n-resp" hx-swap-oob="true">1<`, `id="aba-n-pendencias"`)
	if m := regexp.MustCompile(`<p id="vazio-esp"[^>]*>`).FindString(html); m == "" || strings.Contains(m, "hidden") {
		t.Errorf("o grupo esperando ficou vazio e a frase não aparece: %q", m)
	}
	if m := regexp.MustCompile(`<p id="vazio-resp"[^>]*>`).FindString(html); !strings.Contains(m, "hidden") {
		t.Errorf("o grupo respondidas tem item e a frase aparece: %q", m)
	}
	// A página passa a ter a composição dela com o item no outro grupo (#comp, oob), e só isso: o
	// #vivo não é rearmado e o ciclo seguinte não avisa lista mudada.
	novo := queryDaPagina(t, c, "/pendencias?onda=3")
	m := compRe.FindStringSubmatch(html)
	if m == nil || html2(novo.Get("comp")) != m[1] {
		t.Errorf("o #comp da resposta (%v) não é a composição de agora (%s)", m, novo.Get("comp"))
	}
	naoContem(t, html, `id="vivo"`)
	if _, h, _ := c.vivo(novo, atual); strings.Contains(h, "A lista mudou") {
		t.Errorf("o ciclo seguinte à resposta avisa lista mudada:\n%s", h)
	}
	// Trocar a resposta fica no lugar: sem embrulho, sem contadores.
	w = c.f.hx("POST", "/p/faturamento/513/1/responder", atual, form(url.Values{"texto": {"sim, de novo"}}))
	html = corpo(w)
	contem(t, html, `id="pend-faturamento-513-1"`)
	naoContem(t, html, "afterbegin", "grupo-n-")
}

// S1: o POST aplica só a mudança dele à composição que a página mandou; pendência que chegou depois
// da carga continua diferente e o /vivo avisa. M1: quem decide mover é a página, não a versão.
func TestPostPublicaSoAComposicaoDaPagina(t *testing.T) {
	c := semeada(t)
	atual := "/pendencias?projeto=nuvem"
	pagina := queryDaPagina(t, c, atual) // a página de antes de a P3 chegar
	_, err := c.st.RegistrarPendencia(c.ctx, central.NovaPendencia{US: refsUS("nuvem", 78), Tipo: "duvida", Titulo: "chegou depois", Autor: "w", Detalhe: "d", Slug: "p3", Hash: "p3"})
	c.ok(err)
	w := c.f.hx("POST", "/p/nuvem/78/1/responder", atual, form(url.Values{"opcao": {"1"}, "comp": {pagina.Get("comp")}}))
	html := corpo(w)
	m := compRe.FindStringSubmatch(html)
	if w.Code != 200 || m == nil {
		t.Fatalf("POST = %d\n%s", w.Code, html)
	}
	if strings.Contains(m[1], "pend-nuvem-78-3") {
		t.Fatalf("o #comp publicou a pendência que a página nunca recebeu: %s", m[1])
	}
	contem(t, html, `afterbegin:#lista-resp`, `id="grupo-n-esp" hx-swap-oob="true">1<`, `id="grupo-n-resp" hx-swap-oob="true">1<`)
	// O ciclo seguinte, com a composição que o POST deixou, avisa.
	q := pagina
	q.Set("comp", stdhtml.UnescapeString(m[1]))
	_, h, _ := c.vivo(q, atual)
	contem(t, h, "A lista mudou")
	naoContem(t, h, "grupo-n-")
	// M1: a aba antiga, com o item ainda em Esperando, responde uma pendência que já estava
	// respondida: o item muda de grupo (RespostaVersao > 1 não decide).
	c.ok2(c.st.ResponderPendencia(c.ctx, nil, refsPend("faturamento", 513, 1), central.Resposta{Texto: "outra aba", Por: "ana"}))
	old := queryDaPagina(t, c, "/pendencias?onda=3")
	old.Set("comp", "e:pend-faturamento-513-1;r:")
	w = c.f.hx("POST", "/p/faturamento/513/1/responder", "/pendencias?onda=3", form(url.Values{"texto": {"de novo"}, "comp": {old.Get("comp")}}))
	h = corpo(w)
	contem(t, h, `afterbegin:#lista-resp`, `id="grupo-n-esp" hx-swap-oob="true">0<`, `id="grupo-n-resp" hx-swap-oob="true">1<`, "Resposta trocada")
	// Item que a página já tem em Respondidas: fica no lugar.
	w = c.f.hx("POST", "/p/faturamento/513/1/responder", "/pendencias?onda=3", form(url.Values{"texto": {"mais uma"}, "comp": {"e:;r:pend-faturamento-513-1"}}))
	naoContem(t, corpo(w), "afterbegin", "grupo-n-")
	// Composição inválida é ignorada, sem erro.
	w = c.f.hx("POST", "/p/faturamento/513/1/responder", "/pendencias?onda=3", form(url.Values{"texto": {"x"}, "comp": {"<script>"}}))
	if w.Code != 200 {
		t.Errorf("comp inválida: %d", w.Code)
	}
}

// html2 escapa um trecho como o html/template o escreve num atributo.
func html2(s string) string { return strings.ReplaceAll(s, `"`, "&#34;") }

func TestErrosDeFragmentoVaoParaODentroDoFormulario(t *testing.T) {
	f := nova(t, true)
	casos := map[string]struct {
		corpo, ct string
		quer      int
	}{
		"413 corpo":  {form(url.Values{"opcao": {"1"}, "texto": {strings.Repeat("a", 17<<10)}}), "", http.StatusRequestEntityTooLarge},
		"413 texto":  {form(url.Values{"opcao": {"1"}, "texto": {strings.Repeat("a", 8<<10+1)}}), "", http.StatusRequestEntityTooLarge},
		"415":        {`{"opcao":1}`, "application/json", http.StatusUnsupportedMediaType},
		"400 opção":  {form(url.Values{"opcao": {"9"}}), "", http.StatusBadRequest},
		"400 vazio":  {"", "", http.StatusBadRequest},
		"400 número": {form(url.Values{"opcao": {"x"}}), "", http.StatusBadRequest},
	}
	for nome, k := range casos {
		cab := []string{}
		if k.ct != "" {
			cab = append(cab, "Content-Type", k.ct)
		}
		w := f.hx("POST", "/p/orq/15/1/responder", "/", k.corpo, cab...)
		html := corpo(w)
		if w.Code != k.quer {
			t.Errorf("%s: status %d, quer %d", nome, w.Code, k.quer)
		}
		// Os dois cabeçalhos juntos: só o retarget faria o outerHTML apagar o espaço reservado.
		if w.Header().Get("HX-Retarget") != "find .erro" || w.Header().Get("HX-Reswap") != "innerHTML" {
			t.Errorf("%s: HX-Retarget %q HX-Reswap %q", nome, w.Header().Get("HX-Retarget"), w.Header().Get("HX-Reswap"))
		}
		if !strings.HasPrefix(html, "<p>") || strings.Contains(html, "<html") {
			t.Errorf("%s: o corpo do erro não é só o <p>: %s", nome, html)
		}
	}
	// 409: já resolvida.
	if _, err := f.st.ResolverPendencia(t.Context(), &f.co, refsPend("orq", 15, 1), "feito"); err != nil {
		t.Fatal(err)
	}
	w := f.hx("POST", "/p/orq/15/1/responder", "/", form(url.Values{"opcao": {"1"}}))
	if w.Code != http.StatusConflict || w.Header().Get("HX-Retarget") != "find .erro" || w.Header().Get("HX-Reswap") != "innerHTML" {
		t.Errorf("409: %d %v", w.Code, w.Header())
	}
	// 404 de pendência que não existe: também dentro do formulário.
	w = f.hx("POST", "/p/orq/15/9/responder", "/", form(url.Values{"opcao": {"1"}}))
	if w.Code != http.StatusNotFound || w.Header().Get("HX-Retarget") != "find .erro" {
		t.Errorf("404: %d %v", w.Code, w.Header())
	}
	// Sem HX-Request o erro continua sendo a página de erro (sem os cabeçalhos).
	w = f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: form(url.Values{"opcao": {"1"}})})
	if w.Code != http.StatusConflict || w.Header().Get("HX-Retarget") != "" || !strings.Contains(corpo(w), "<!doctype html>") {
		t.Errorf("409 sem HX: %d %v", w.Code, w.Header())
	}
}

func TestSemCabecalhoDoProxyNoSocketUnix401NosFragmentos(t *testing.T) {
	f := nova(t, true)
	sem := "X-Orq-User"
	for _, c := range []struct{ m, p, corpo string }{
		{"POST", "/p/orq/15/1/responder", form(url.Values{"opcao": {"1"}})},
		{"GET", "/p/orq/15/1/corpo", ""},
		{"GET", "/pendencias", ""},
		{"GET", "/vivo", ""},
	} {
		for _, hx := range []bool{false, true} {
			cab := map[string]string{sem: ""}
			if hx {
				cab["HX-Request"] = "true"
			}
			w := f.faz(req{metodo: c.m, caminho: c.p, corpo: c.corpo, cab: cab})
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s (hx=%v) sem cabeçalho = %d, quer 401", c.m, c.p, hx, w.Code)
			}
		}
	}
	if f.pend().Estado != central.EstadoAberta {
		t.Fatal("gravou sem usuário")
	}
	// Com HX-Request, o 401 do POST também vai para dentro do formulário.
	w := f.faz(req{metodo: "POST", caminho: "/p/orq/15/1/responder", corpo: "x=1", cab: map[string]string{sem: "", "HX-Request": "true"}})
	if w.Header().Get("HX-Retarget") != "find .erro" {
		t.Errorf("401 do POST por htmx: %v", w.Header())
	}
}

func TestVivoDaAbaPendencias(t *testing.T) {
	c := semeada(t)
	q := queryDaPagina(t, c, "/pendencias")
	if q.Get("g") == "" || q.Get("v") == "" || q.Get("a") == "" {
		t.Fatalf("hx-vals da aba Pendências: %v", q)
	}
	code, html, h := c.vivo(q, "/pendencias")
	if code != 200 || h.Get("Cache-Control") != "no-store" {
		t.Fatalf("vivo = %d %v", code, h)
	}
	contem(t, html, `id="fontes"`, `id="aba-n-pendencias"`, `id="grupo-n-esp"`, `id="grupo-n-resp"`)
	// Nunca toca o corpo aberto nem o texto digitado, nem a lista.
	naoContem(t, html, "<form", "<textarea", "<input", "<select", "pend-corpo", "<details", "<body", "<main", `id="vivo"`, `id="situacao"`, `id="lista-`, "A lista mudou")
	// Lista igual: limpa o aviso que um ciclo anterior deixou (M1, story 28).
	contem(t, html, `<div id="aviso-lista" class="aviso-lista" role="status" hx-swap-oob="true"></div>`)
	// Lista diferente da da página: não mexe nos grupos e avisa.
	q.Set("v", "0000")
	_, html, _ = c.vivo(q, "/pendencias")
	contem(t, html, `id="aviso-lista"`, "A lista mudou", `id="fontes"`, `id="aba-n-pendencias"`)
	naoContem(t, html, "grupo-n-")
	// Pendência nova muda o hash.
	antes := queryDaPagina(t, c, "/pendencias").Get("comp")
	_, err := c.st.RegistrarPendencia(c.ctx, central.NovaPendencia{US: refsUS("agenda", 221), Tipo: "duvida", Titulo: "nova", Autor: "w", Detalhe: "d", Slug: "n", Hash: "n"})
	c.ok(err)
	if depois := queryDaPagina(t, c, "/pendencias").Get("comp"); depois == antes {
		t.Error("pendência nova não muda a composição da aba Pendências")
	}
	// O filtro entra no hash: a mesma lista, filtrada, tem outro hash; e o /vivo lê o filtro do HX-Current-URL.
	filtrado := queryDaPagina(t, c, "/pendencias?projeto=nuvem")
	_, html, _ = c.vivo(filtrado, "/pendencias?projeto=nuvem")
	naoContem(t, html, "A lista mudou")
	_, html, _ = c.vivo(filtrado, "/pendencias")
	contem(t, html, "A lista mudou")
}

func TestPendenciaNoLugarEscapaHTMLHostil(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	_ = co
	payload := `<img src=x onerror=alert(1)>"'&`
	_, err := c.st.RegistrarPendencia(c.ctx, central.NovaPendencia{US: refsUS("proj", 7), Tipo: "duvida", Titulo: payload, Autor: "w", Detalhe: payload, Slug: "s", Hash: "h"})
	c.ok(err)
	for _, p := range []string{"/", "/pendencias", "/projeto/proj"} {
		_, h := c.get(p)
		if strings.Contains(h, "<img src=x") || !strings.Contains(h, "&lt;img src=x") {
			t.Errorf("%s: título hostil sem escape", p)
		}
		semDetailsAberto(t, p, h)
	}
	h := corpo(c.f.hx("GET", "/p/proj/7/1/corpo", "/", ""))
	if strings.Contains(h, "<img src=x") {
		t.Errorf("corpo: detalhe hostil sem escape:\n%s", h)
	}
}

func TestLogDasRotasNovas(t *testing.T) {
	f := nova(t, true)
	f.hx("GET", "/p/orq/15/1/corpo", "/", "")
	f.hx("GET", "/pendencias?projeto=segredo", "/", "")
	f.hx("GET", "/vivo?v=1", "/", "")
	f.hx("POST", "/p/orq/15/1/responder", "/", form(url.Values{"opcao": {"1"}, "texto": {"MINHA-SENHA"}}))
	f.faz(req{metodo: "GET", caminho: "/central"})
	want := []string{
		"web /p/{projeto}/{ref}/{n}/corpo 200",
		"web /pendencias 200",
		"web /vivo 200",
		"web /p/{projeto}/{ref}/{n}/responder 200",
		"web /central 308",
	}
	if len(f.log) != len(want) {
		t.Fatalf("log = %v", f.log)
	}
	for i, l := range f.log {
		if l != want[i] {
			t.Errorf("log[%d] = %q, quer %q", i, l, want[i])
		}
	}
}
