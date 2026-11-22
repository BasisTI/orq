package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// arqsPublicos é a spec lida: a pasta da US (com um README.md extra), o README do épico e uma spec
// compartilhada, com links relativos de todo tipo.
func arqsPublicos() []central.ArquivoSpec {
	spec := "# Spec\n\nVeja o [plano](plano.md#p), as [decisões](decisoes.md), o [épico](../README.md), a [ref](../ref.md), " +
		"o [extra](README.md), a [outra US](../tg-8-y/spec.md), a [fora](../../x/y.md), a [absoluta](/p/proj/7/spec), a [onda](/onda/1), " +
		"a [MR](https://github.com/BasisTI/orq/pull/44), o [site](https://exemplo.com/a) e [esta](#topo).\n\n" +
		"<script>alert(1)</script>\n\n[js](javascript:alert(1)) [dados](data:text/html,x)\n\n![img](a.png)\n"
	return []central.ArquivoSpec{
		arqPasta("spec.md", spec, 0),
		arqPasta("plano.md", "# Plano\n\nPasso **um**, volte à [spec](spec.md).", 1),
		arqPasta("decisoes.md", "# Decisões\n\nD1 — sim.", 2),
		arqPasta("README.md", "# README da US\n\nVeja o [épico](../README.md).", 3),
		{Caminho: "docs/specs/e/README.md", Papel: central.PapelEpico, Ordem: 4, Conteudo: "# Épico\n\nVisão do épico.", Tamanho: 30},
		{Caminho: "docs/specs/e/ref.md", Papel: central.PapelEpico, Ordem: 5, Conteudo: "# Referência\n\nreferência", Tamanho: 10},
	}
}

type cenaPublica struct {
	*cena
	srv    *httptest.Server
	codigo string
	id     int64
	co     central.Coordenador
	u      refs.US
	logs   *[]string
}

// publica monta a spec TG-7 lida, um link ativo e o servidor público sobre a Store.
func publica(t *testing.T) *cenaPublica {
	t.Helper()
	c, co, u := comSpecLida(t)
	c.taiga(u, "Título da US", "In progress")
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaCab, true, arqsPublicos()))
	l, err := c.st.GerarLink(c.ctx, &co, u, "cliente", 0)
	c.ok(err)
	var mu sync.Mutex
	logs := &[]string{}
	p := &Publico{Fonte: c.st, Log: func(f string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		*logs = append(*logs, strings.TrimSpace(fmt.Sprintf(f, a...)))
	}}
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	return &cenaPublica{cena: c, srv: srv, codigo: l.Codigo, id: l.ID, co: co, u: u, logs: logs}
}

type resp struct {
	code   int
	corpo  string
	header http.Header
}

func (p *cenaPublica) pede(metodo, caminho string) resp {
	p.t.Helper()
	req, err := http.NewRequest(metodo, p.srv.URL+caminho, nil)
	if err != nil {
		p.t.Fatal(err)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return resp{r.StatusCode, string(b), r.Header}
}

func (p *cenaPublica) pagina() string {
	p.t.Helper()
	r := p.pede("GET", "/s/"+p.codigo)
	if r.code != 200 {
		p.t.Fatalf("status %d\n%s", r.code, r.corpo)
	}
	return r.corpo
}

func TestPaginaPublicaUmaPaginaComIndiceESecoesNaOrdem(t *testing.T) {
	p := publica(t)
	r := p.pede("GET", "/s/"+p.codigo)
	if r.code != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
		t.Fatalf("status %d, %v", r.code, r.header)
	}
	h := r.corpo
	contem(t, h, "<h1 class=\"t-pagina\">Título da US</h1>", `aria-label="Índice"`, "Passo <strong>um</strong>", "Visão do épico.", "D1 — sim.")
	// Ordem das seções: Spec, Plano, Decisões, o .md extra da pasta da US, README do épico, spec compartilhada.
	var ids []string
	for _, m := range regexp.MustCompile(`<section class="pub-secao" id="([^"]+)"`).FindAllStringSubmatch(h, -1) {
		ids = append(ids, m[1])
	}
	if got := strings.Join(ids, ","); got != "us-spec,us-plano,us-decisoes,us-extra-1,epico-readme,epico-compartilhada-1" {
		t.Fatalf("seções = %s", got)
	}
	// O índice tem uma âncora por seção, na mesma ordem.
	i := strings.Index(h, `aria-label="Índice"`)
	for _, id := range ids {
		j := strings.Index(h[i:], `href="#`+id+`"`)
		if j < 0 {
			t.Fatalf("o índice não leva a #%s", id)
		}
		i += j
	}
	// Links entre arquivos do conjunto viram âncora; o resto vira texto.
	contem(t, h,
		`<a href="#us-plano" rel="noopener noreferrer nofollow">plano</a>`,
		`<a href="#us-decisoes" rel=`, `<a href="#epico-readme" rel="noopener noreferrer nofollow">épico</a>`,
		`<a href="#epico-compartilhada-1" rel=`, `<a href="#us-extra-1" rel=`, `<a href="#us-spec" rel=`,
		`<a href="https://exemplo.com/a"`, `<a href="https://github.com/BasisTI/orq/pull/44"`, `<a href="#topo"`)
	contem(t, h, "a outra US,", "a fora,", "a absoluta,", "a onda,")
	naoContem(t, h, `>outra US</a>`, `>fora</a>`, `>absoluta</a>`, `>onda</a>`, "<script>alert", `src="a.png"`, "javascript:", "data:text")
	contem(t, h, "&lt;script&gt;alert(1)&lt;/script&gt;")
}

func TestPaginaPublicaAncorasSaoUnicas(t *testing.T) {
	p := publica(t)
	h := p.pagina()
	vistos := map[string]bool{}
	for _, m := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllStringSubmatch(h, -1) {
		if vistos[m[1]] {
			t.Errorf("id repetido: %s", m[1])
		}
		vistos[m[1]] = true
	}
	// O README.md da pasta da US e o do épico têm âncoras diferentes.
	if !vistos["us-extra-1"] || !vistos["epico-readme"] {
		t.Fatalf("ids = %v", vistos)
	}
	// Mais de um .md extra e mais de uma spec compartilhada: numerados, sem colisão.
	anc := ancorasPublicas([]central.ArquivoSpec{
		arqPasta("spec.md", "", 0), arqPasta("README.md", "", 1), arqPasta("notas.md", "", 2), arqPasta("Spec.md", "", 3),
		{Caminho: "docs/e/README.md", Papel: central.PapelEpico}, {Caminho: "docs/e/a.md", Papel: central.PapelEpico}, {Caminho: "docs/e/b.md", Papel: central.PapelEpico},
	})
	vistos = map[string]bool{}
	for _, id := range anc {
		if vistos[id] {
			t.Errorf("âncora repetida: %s em %v", id, anc)
		}
		vistos[id] = true
	}
}

// O critério do href vale para links relativos e de mesma origem fora de âncora e /estatico/: nenhuma
// rota da Central (/p/, /onda/, /projeto/, ...) e nada que cite projeto ou ref. Link absoluto para
// outro site (a MR no GitHub, no conteúdo) é legítimo.
var reAtributo = regexp.MustCompile(`(?i)\b(?:href|src|action|formaction|data|poster)\s*=\s*"([^"]*)"`)

func hrefsLocais(t *testing.T, h string) []string {
	t.Helper()
	var out []string
	for _, m := range reAtributo.FindAllStringSubmatch(h, -1) {
		v := strings.TrimSpace(m[1])
		switch {
		case strings.HasPrefix(v, "#"), strings.HasPrefix(v, "/estatico/"):
			continue
		case strings.HasPrefix(v, "https://"), strings.HasPrefix(v, "http://"), strings.HasPrefix(v, "mailto:"):
			continue
		}
		out = append(out, v)
	}
	return out
}

func TestPaginaPublicaNaoTemHrefDaCentralNemCitaProjetoOuRef(t *testing.T) {
	p := publica(t)
	h := p.pagina()
	if locais := hrefsLocais(t, h); len(locais) != 0 {
		t.Fatalf("hrefs locais que não são âncora nem estático: %q", locais)
	}
	// Os atributos que a varredura aceita (âncora, estático, externo) também não citam projeto nem ref.
	for _, m := range reAtributo.FindAllStringSubmatch(h, -1) {
		v := m[1]
		if strings.HasPrefix(v, "http") {
			continue // link do conteúdo: a MR no GitHub é legítima
		}
		for _, proibido := range []string{"/p/", "/onda/", "/projeto/", "proj", "TG-7", "tg-7", "/7/"} {
			if strings.Contains(v, proibido) {
				t.Errorf("href %q cita %q", v, proibido)
			}
		}
	}
	// Teste da própria varredura: ela pega o que deve pegar.
	for _, ruim := range []string{`<a href="/p/proj/7/spec">`, `<a href="/onda/1">`, `<a href="/projeto/proj">`, `<a href="plano.md">`, `<form action="/p/x/1/responder">`, `<img src="/img.png">`} {
		if len(hrefsLocais(t, ruim)) != 1 {
			t.Errorf("a varredura não pegou %s", ruim)
		}
	}
	for _, bom := range []string{`<a href="#a">`, `<link href="/estatico/app.css?v=1">`, `<a href="https://github.com/BasisTI/orq/pull/44">`} {
		if len(hrefsLocais(t, bom)) != 0 {
			t.Errorf("a varredura pegou o que é legítimo: %s", bom)
		}
	}
}

func TestPaginaPublicaNaoExpoeNadaDaCentral(t *testing.T) {
	p := publica(t)
	h := p.pagina()
	naoContem(t, h,
		"TG-7", "tg-7", "proj/7", "alfa", "wA:p1", "Onda de alfa", // ref, projeto, coordenador, pane, onda
		shaCab, "cabcabca", // SHA
		pastaDaSpec,               // pasta registrada
		"spec-mr", `>MR da spec<`, // link da MR
		"/pendencias", "/historico", "Central", "avisos", "<script", "htmx", "X-Orq-User", "Você é", "sprite")
	contem(t, h, "em validação", "lida em", `name="robots" content="noindex, nofollow"`)
	// A MR só aparece se o próprio conteúdo a citar (ele a cita, com link externo); nenhum rótulo "MR da spec".
}

func TestPaginaPublicaCabecalhos(t *testing.T) {
	p := publica(t)
	for _, caminho := range []string{"/s/" + p.codigo, "/s/" + strings.Repeat("a", 43), "/nada", "/estatico/app.css"} {
		r := p.pede("GET", caminho)
		for k, v := range map[string]string{"Referrer-Policy": "no-referrer", "Cache-Control": "no-store", "X-Robots-Tag": "noindex", "X-Content-Type-Options": "nosniff"} {
			if k == "Cache-Control" && strings.HasPrefix(caminho, "/estatico/") {
				continue // o estático leva o cache dele
			}
			if r.header.Get(k) != v {
				t.Errorf("%s: %s = %q, queria %q", caminho, k, r.header.Get(k), v)
			}
		}
		if !strings.Contains(r.header.Get("Content-Security-Policy"), "default-src 'none'") {
			t.Errorf("%s: sem CSP", caminho)
		}
	}
}

func TestPublicoServeOsEstaticosDaPagina(t *testing.T) {
	p := publica(t)
	if r := p.pede("GET", "/estatico/app.css"); r.code != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/css") || len(r.corpo) < 100 {
		t.Fatalf("app.css: %d %v", r.code, r.header)
	}
	if r := p.pede("GET", "/estatico/orq-icon-64.png"); r.code != 200 {
		t.Fatalf("ícone: %d", r.code)
	}
	if r := p.pede("GET", "/estatico/../publico.go"); r.code != 404 {
		t.Fatalf("fora do embed: %d", r.code)
	}
}

// Código inexistente, revogado e expirado: o mesmo 404, com o mesmo corpo e os mesmos cabeçalhos.
func TestPublico404IdenticoParaInexistenteRevogadoExpiradoERotaDesconhecida(t *testing.T) {
	p := publica(t)
	co, u := p.co, p.u
	rev, err := p.st.GerarLink(p.ctx, &co, u, "revogado", 0)
	p.ok(err)
	_, err = p.st.RevogarLink(p.ctx, &co, u, rev.ID)
	p.ok(err)
	base := time.Now().UTC()
	exp, err := p.st.GerarLink(p.ctx, &co, u, "expira", time.Minute)
	p.ok(err)
	p.st.Now = func() time.Time { return base.Add(2 * time.Minute) }
	t.Cleanup(func() { p.st.Now = time.Now })

	casos := map[string]resp{}
	for nome, caminho := range map[string]string{
		"inexistente":   "/s/" + strings.Repeat("A", 43),
		"revogado":      "/s/" + rev.Codigo,
		"expirado":      "/s/" + exp.Codigo,
		"formato":       "/s/curto",
		"longo":         "/s/" + strings.Repeat("a", 200),
		"raiz":          "/",
		"da Central":    "/p/proj/7/spec",
		"pendências":    "/pendencias",
		"saúde":         "/saude",
		"onda":          "/onda/1",
		"projeto":       "/projeto/proj",
		"sem código":    "/s/",
		"estático novo": "/estatico/nao-existe.css",
	} {
		casos[nome] = p.pede("GET", caminho)
	}
	casos["POST"] = p.pede("POST", "/s/"+p.codigo)
	casos["DELETE"] = p.pede("DELETE", "/s/"+rev.Codigo)
	ref := casos["inexistente"]
	if ref.code != 404 {
		t.Fatalf("inexistente: %d", ref.code)
	}
	for nome, r := range casos {
		if nome == "estático novo" {
			continue // 404 do http.NotFound dos estáticos: é outra rota, mas não revela nada do link
		}
		if r.code != 404 || r.corpo != ref.corpo {
			t.Errorf("%s: %d %q (queria o 404 de %q)", nome, r.code, r.corpo, ref.corpo)
		}
		for _, k := range []string{"Content-Type", "Cache-Control", "Referrer-Policy", "X-Robots-Tag", "Content-Security-Policy", "X-Content-Type-Options", "Content-Length"} {
			if r.header.Get(k) != ref.header.Get(k) {
				t.Errorf("%s: cabeçalho %s = %q, queria %q", nome, k, r.header.Get(k), ref.header.Get(k))
			}
		}
	}
	// O link ativo segue abrindo (o relógio andou, mas ele não expira).
	if r := p.pede("GET", "/s/"+p.codigo); r.code != 200 {
		t.Fatalf("ativo: %d", r.code)
	}
}

func TestPublicoRegistraAcessoSoNoValidoESemEventoNoDiario(t *testing.T) {
	p := publica(t)
	co, u := p.co, p.u
	antes, err := p.st.Diario(p.ctx, nil, central.FiltroDiario{})
	p.ok(err)
	p.pede("GET", "/s/"+p.codigo)
	p.pede("GET", "/s/"+p.codigo)
	p.pede("GET", "/s/"+strings.Repeat("A", 43))
	p.pede("GET", "/estatico/app.css")
	ls, err := p.st.ListarLinks(p.ctx, &co, u)
	p.ok(err)
	if len(ls) != 1 || ls[0].Acessos != 2 || ls[0].UltimoAcesso == nil {
		t.Fatalf("ls = %+v", ls)
	}
	depois, err := p.st.Diario(p.ctx, nil, central.FiltroDiario{})
	p.ok(err)
	if len(depois) != len(antes) {
		t.Fatalf("o acesso gerou evento: %d → %d", len(antes), len(depois))
	}
}

func TestPublicoLogNaoTemOCodigoNemOCaminho(t *testing.T) {
	p := publica(t)
	p.pede("GET", "/s/"+p.codigo)
	p.pede("GET", "/s/"+strings.Repeat("B", 43))
	p.pede("GET", "/p/proj/7/spec")
	todo := strings.Join(*p.logs, "\n")
	if len(*p.logs) != 3 || !strings.Contains(todo, "/s/{codigo} 200") || !strings.Contains(todo, "/s/{codigo} 404") || !strings.Contains(todo, "/ 404") {
		t.Fatalf("logs = %q", *p.logs)
	}
	for _, proibido := range []string{p.codigo, "BBBB", "proj", "spec"} {
		if strings.Contains(todo, proibido) {
			t.Fatalf("o log cita %q: %s", proibido, todo)
		}
	}
}

func TestPaginaPublicaFaixaMostraOEstadoSimplificado(t *testing.T) {
	p := publica(t)
	co, u := p.co, p.u
	h := p.pagina()
	contem(t, h, `<span class="selo selo-aviso">em validação</span>`)
	naoContem(t, h, "Versão ")
	// Aprovada.
	v, err := p.st.AbrirValidacao(p.ctx, &co, u, shaVal, "")
	p.ok(err)
	op := central.OpcaoAprovar
	_, err = p.st.ResponderPendencia(p.ctx, &co, refs.Pend{US: u, Numero: v.Numero}, central.Resposta{Opcao: &op, Por: "ana"})
	p.ok(err)
	contem(t, p.pagina(), `<span class="selo selo-ok">aprovada</span>`)
	// A onda "até plano" encerrada e a US done, sem MR do código mesclada: segue "aprovada", não "entregue".
	_, _, err = p.st.AtualizarStatus(p.ctx, &co, u, central.MudancaStatus{Status: "done"})
	p.ok(err)
	var ondaID int64
	os, err := p.st.ListarOndas(p.ctx, central.FiltroOndas{})
	p.ok(err)
	ondaID = os[0].ID
	_, err = p.st.EncerrarOnda(p.ctx, co.Nome, ondaID)
	p.ok(err)
	h = p.pagina()
	contem(t, h, `>aprovada</span>`)
	naoContem(t, h, "entregue")
	// MR do código mesclada: entregue.
	mrCodigo := "https://github.com/BasisTI/orq/pull/61"
	_, _, err = p.st.AdicionarMR(p.ctx, &co, u, mrCodigo)
	p.ok(err)
	p.ok(p.st.GravarMR(p.ctx, mrCodigo, central.ValorMR{Estado: "mesclada"}, nil))
	contem(t, p.pagina(), `<span class="selo selo-ok">entregue</span>`)
}

func TestPaginaPublicaAvisoDeVersaoSoAPartirDaSegunda(t *testing.T) {
	p := publica(t)
	co, u := p.co, p.u
	naoContem(t, p.pagina(), "Versão 1", "substituída")
	p.st.Now = func() time.Time { return time.Date(2026, 11, 22, 15, 30, 0, 0, time.UTC) } // 12:30 em BRT
	t.Cleanup(func() { p.st.Now = time.Now })
	_, err := p.st.RegistrarSpec(p.ctx, &co, u, pastaDaSpec, mrDaSpec, true)
	p.ok(err)
	// Mesmo registro, mesma pasta e MR: o cache segue valendo.
	h := p.pagina()
	contem(t, h, `<span class="spec-versao">Versão 2 — substituída em 22/11/2026 12:30</span>`)
	// A Central mostra o mesmo aviso na página da spec; na versão 1 não mostrava.
	_, hc := p.get("/p/proj/7/spec")
	contem(t, hc, "Versão 2 — substituída em 22/11/2026 12:30")
}

func TestPaginaDaCentralSemAvisoNaVersao1(t *testing.T) {
	c, _, u := comSpecLida(t)
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaCab, true, arqsDaSpec("# Spec")))
	_, html := c.get("/p/proj/7/spec")
	naoContem(t, html, "Versão 1", "substituída")
}

func TestPaginaPublicaAindaNaoLidaEArquivoGrande(t *testing.T) {
	c, co, u := comSpecLida(t)
	l, err := c.st.GerarLink(c.ctx, &co, u, "x", 0)
	c.ok(err)
	srv := httptest.NewServer((&Publico{Fonte: c.st}).Handler())
	defer srv.Close()
	get := func() string {
		r, err := http.Get(srv.URL + "/s/" + l.Codigo)
		c.ok(err)
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		if r.StatusCode != 200 {
			t.Fatalf("status %d", r.StatusCode)
		}
		return string(b)
	}
	h := get()
	contem(t, h, "ainda não está disponível", "<h1 class=\"t-pagina\">Especificação</h1>")
	naoContem(t, h, `aria-label="Índice"`, mrDaSpec)
	as := arqsPublicos()
	as[1] = central.ArquivoSpec{Caminho: pastaDaSpec + "/plano.md", Papel: central.PapelPasta, Ordem: 1, GrandeDemais: true, Tamanho: 3 << 20}
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaCab, true, as))
	h = get()
	contem(t, h, "grande demais (3072 KiB)")
	naoContem(t, h, mrDaSpec, "Veja na MR")
}

// A Fonte pública é uma interface de duas funções: o handler não alcança nada além dela.
type fontePublicaFalsa struct {
	err  error
	sp   central.SpecPublica
	hash string
	n    int
}

func (f *fontePublicaFalsa) SpecPorHash(_ context.Context, h string) (central.SpecPublica, error) {
	f.hash = h
	return f.sp, f.err
}

func (f *fontePublicaFalsa) MarcarAcesso(context.Context, string) error { f.n++; return nil }

func TestPublicoSoUsaAInterfaceMinima(t *testing.T) {
	f := &fontePublicaFalsa{sp: central.SpecPublica{Titulo: "T", Estado: central.PublicoAprovada, Versao: 1}}
	srv := httptest.NewServer((&Publico{Fonte: f}).Handler())
	defer srv.Close()
	codigo := strings.Repeat("c", 43)
	r, err := http.Get(srv.URL + "/s/" + codigo)
	if err != nil || r.StatusCode != 200 || f.n != 1 || f.hash != central.HashCodigoLink(codigo) || f.hash == codigo {
		t.Fatalf("%v %v hash=%q n=%d", r, err, f.hash, f.n)
	}
	r.Body.Close()
	// Erro que não é "não encontrado": 503 sem detalhe, e não conta acesso.
	f.err, f.n = errors.New("banco caiu: segredo-interno"), 0
	r, err = http.Get(srv.URL + "/s/" + codigo)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 503 || f.n != 0 || strings.Contains(string(b), "segredo-interno") {
		t.Fatalf("erro: %d %q n=%d", r.StatusCode, b, f.n)
	}
	// "Não encontrado" e código mal formado dão 404; o malformado nem chega à fonte.
	f.err, f.hash = &output.Error{Code: "nao_encontrado", Exit: output.ExitNotFound}, ""
	if r, _ = http.Get(srv.URL + "/s/" + codigo); r.StatusCode != 404 {
		t.Fatalf("404: %d", r.StatusCode)
	}
	f.hash = ""
	if r, _ = http.Get(srv.URL + "/s/curto"); r.StatusCode != 404 || f.hash != "" {
		t.Fatalf("malformado: %d hash=%q", r.StatusCode, f.hash)
	}
}
