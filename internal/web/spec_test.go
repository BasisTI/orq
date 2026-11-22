package web

import (
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

const (
	pastaDaSpec = "docs/specs/e/tg-7-x"
	shaCab      = "cabcabcabcabcabc"
	shaVal      = "1234567890abcdef"
)

func arqPasta(nome, conteudo string, ordem int) central.ArquivoSpec {
	return central.ArquivoSpec{Caminho: pastaDaSpec + "/" + nome, Papel: central.PapelPasta, Ordem: ordem, Conteudo: conteudo, Tamanho: int64(len(conteudo))}
}

func arqsDaSpec(spec string) []central.ArquivoSpec {
	return []central.ArquivoSpec{
		arqPasta("spec.md", spec, 0),
		arqPasta("plano.md", "# Plano\n\nPasso **um**.", 1),
		arqPasta("decisoes.md", "# Decisões\n\nD1 — sim.", 2),
		{Caminho: "docs/specs/e/README.md", Papel: central.PapelEpico, Ordem: 3, Conteudo: "# Épico\n\nVisão do épico.", Tamanho: 30},
		{Caminho: "docs/specs/e/ref.md", Papel: central.PapelEpico, Ordem: 4, Conteudo: "referência", Tamanho: 10},
	}
}

const specComLinks = "# Spec\n\nVeja o [plano](plano.md#p), o [épico](../README.md), a [ref](../ref.md), a [outra US](../tg-8-y/spec.md), " +
	"a [fora](../../x/y.md), o [site](https://exemplo.com/a) e [esta](#topo).\n\n<script>alert(1)</script>\n\n<div onclick=\"x()\">cru</div>\n\n![img](a.png)\n"

// spec registrada, lida e uma US para a página dela.
func comSpecLida(t *testing.T) (*cena, central.Coordenador, refs.US) {
	t.Helper()
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	_, err := c.st.RegistrarSpec(c.ctx, &co, u, pastaDaSpec, mrDaSpec, false)
	c.ok(err)
	return c, co, u
}

func TestPaginaDaSpecTemAbasFaixaELinkDaMR(t *testing.T) {
	c, _, u := comSpecLida(t)
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaCab, true, arqsDaSpec(specComLinks)))
	code, html := c.get("/p/proj/7/spec")
	if code != 200 {
		t.Fatalf("status %d\n%s", code, html)
	}
	contem(t, html,
		"Spec da TG-7", pastaDaSpec,
		"spec registrada", "TG-7", "cabcabca", "lida em", `href="`+mrDaSpec+`"`,
		`aria-label="Arquivos da spec"`,
		`href="/p/proj/7/spec?aba=spec.md" aria-current="page">Spec`,
		`href="/p/proj/7/spec?aba=plano.md">Plano`, `>Decisões`, `>README do épico`, `>Épico: ref`,
		"<h1>Spec</h1>")
	naoContem(t, html, "Passo <strong>um</strong>")
	// Links relativos da allowlist viram links de aba; fora dela, texto; https e âncora seguem.
	contem(t, html,
		`<a href="/p/proj/7/spec?aba=plano.md" rel="noopener noreferrer nofollow">plano</a>`,
		`<a href="/p/proj/7/spec?aba=epico%3AREADME.md" rel="noopener noreferrer nofollow">épico</a>`,
		`<a href="/p/proj/7/spec?aba=epico%3Aref.md" rel="noopener noreferrer nofollow">ref</a>`,
		`<a href="https://exemplo.com/a"`, `<a href="#topo"`)
	contem(t, html, "a outra US,", "a fora,")
	naoContem(t, html, `href="../tg-8-y`, `href="../../x/y.md"`, `href="plano.md`, `>outra US</a>`, `>fora</a>`)
	// HTML cru não passa; imagem perde o endereço.
	naoContem(t, html, "<script>alert", "<div onclick", `src="a.png"`, `<img src="a.png"`)
	contem(t, html, "&lt;script&gt;alert(1)&lt;/script&gt;")
}

func TestPaginaDaSpecTrocaDeAbaEAbaDesconhecidaVoltaParaASpec(t *testing.T) {
	c, _, u := comSpecLida(t)
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaCab, true, arqsDaSpec("# Spec\n\nconteúdo da spec")))
	_, html := c.get("/p/proj/7/spec?aba=plano.md")
	contem(t, html, "<h1>Plano</h1>", `href="/p/proj/7/spec?aba=plano.md" aria-current="page"`)
	naoContem(t, html, "conteúdo da spec")
	_, html = c.get("/p/proj/7/spec?aba=epico%3AREADME.md")
	contem(t, html, "Visão do épico.")
	_, html = c.get("/p/proj/7/spec?aba=../../etc/passwd")
	contem(t, html, "conteúdo da spec", `aria-current="page">Spec`)
}

func TestPaginaDaSpecArquivoGrandeDemaisMostraAvisoComLinkDaMR(t *testing.T) {
	c, _, u := comSpecLida(t)
	as := arqsDaSpec("x")
	as[1] = central.ArquivoSpec{Caminho: pastaDaSpec + "/plano.md", Papel: central.PapelPasta, Ordem: 1, GrandeDemais: true, Tamanho: 3 << 20}
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaCab, true, as))
	_, html := c.get("/p/proj/7/spec?aba=plano.md")
	contem(t, html, "Arquivo grande demais (3072 KiB)", "Veja na MR", `href="`+mrDaSpec+`"`)
	naoContem(t, html, `class="detalhe spec-corpo"`)
}

func TestPaginaDaSpecComFalhaMostraOUltimoConteudoComSHAEHora(t *testing.T) {
	c, _, u := comSpecLida(t)
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaCab, true, arqsDaSpec("# Spec\n\núltimo conteúdo bom")))
	c.ok(c.st.GravarErroLeituraSpec(c.ctx, u, central.Erro{Codigo: "falha_comando", Mensagem: "gh saiu com 1: HTTP 500"}))
	_, html := c.get("/p/proj/7/spec")
	contem(t, html, "A última leitura na forja falhou", "falha_comando: gh saiu com 1: HTTP 500", "último conteúdo guardado", "SHA cabcabca", "último conteúdo bom", `role="alert"`)
}

func TestPaginaDaSpecAindaNaoLida(t *testing.T) {
	c, _, _ := comSpecLida(t)
	code, html := c.get("/p/proj/7/spec")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	contem(t, html, "O daemon ainda não leu a spec na forja.", `href="`+mrDaSpec+`"`)
	naoContem(t, html, "Arquivos da spec")
}

func TestPaginaDaSpecErros(t *testing.T) {
	c := vazia(t, false)
	c.coord("alfa", "wA:p1", "proj", 7, "")
	for _, caminho := range []string{"/p/proj/7/spec", "/p/proj/abc/spec", "/p/proj/0/spec", "/p/Proj/7/spec", "/p/inexistente/9/spec"} {
		if code, _ := c.get(caminho); code != 404 {
			t.Errorf("%s: status %d, queria 404", caminho, code)
		}
	}
}

// O card da US leva à página da spec.
func TestCardDaUSTemOLinkDaSpec(t *testing.T) {
	c, _, _ := comSpecLida(t)
	_, html := c.get("/")
	contem(t, html, `<a class="spec-abrir" href="/p/proj/7/spec">Abrir a spec</a>`)
}

// A pendência de validação mostra a spec no SHA dela, não na cabeça, ao lado das opções.
func TestPendenciaDeValidacaoMostraASpecNoSHADela(t *testing.T) {
	c, co, u := comSpecLida(t)
	p, err := c.st.AbrirValidacao(c.ctx, &co, u, shaVal, "")
	c.ok(err)
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaCab, true, arqsDaSpec("# Spec\n\nda CABEÇA")))
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaVal, false, arqsDaSpec("# Spec\n\nda VALIDAÇÃO")))
	rota := refs.RotaPendencia("proj", 7, p.Numero)
	code, html := c.get(rota)
	if code != 200 {
		t.Fatalf("status %d\n%s", code, html)
	}
	contem(t, html, `class="valid-duas"`, `class="valid-spec"`, `class="valid-resp"`,
		"da VALIDAÇÃO", "SHA <code>12345678</code>", "(da validação)",
		`href="`+rota+`?aba=plano.md#spec"`, "Aprovar", "Pedir ajustes", `href="`+mrDaSpec+`"`)
	naoContem(t, html, "da CABEÇA")
	// A aba escolhida vale na pendência e os links entre arquivos apontam para a própria pendência.
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaVal, false, arqsDaSpec(specComLinks)))
	_, html = c.get(rota + "?aba=decisoes.md")
	contem(t, html, "<h1>Decisões</h1>")
	_, html = c.get(rota)
	contem(t, html, `<a href="`+rota+`?aba=plano.md#spec" rel=`)
}

func TestPendenciaDeValidacaoSemAVersaoGuardada(t *testing.T) {
	c, co, u := comSpecLida(t)
	p, err := c.st.AbrirValidacao(c.ctx, &co, u, shaVal, "")
	c.ok(err)
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaCab, true, arqsDaSpec("# Spec\n\nda cabeça")))
	_, html := c.get(refs.RotaPendencia("proj", 7, p.Numero))
	contem(t, html, "A versão da validação (SHA 12345678) não está guardada", `href="/p/proj/7/spec"`, "Aprovar")
	naoContem(t, html, "da cabeça")
}

func TestPendenciaQueNaoEValidacaoNaoTemPainelDeSpec(t *testing.T) {
	c, co, u := comSpecLida(t)
	p, err := c.st.AbrirPendencia(c.ctx, &co, u, "duvida", "uma dúvida", "")
	c.ok(err)
	c.ok(c.st.GravarVersaoSpec(c.ctx, u, pastaDaSpec, mrDaSpec, shaCab, true, arqsDaSpec("x")))
	_, html := c.get(refs.RotaPendencia("proj", 7, p.Numero))
	naoContem(t, html, "valid-duas", `id="spec"`)
}

func TestRenderizarSpecLinksRelativos(t *testing.T) {
	links := LinksSpec{Base: "docs/e/tg-1", Destino: func(c string) (string, bool) {
		if c == "docs/e/tg-1/plano.md" || c == "docs/e/README.md" {
			return "#" + strings.ReplaceAll(c, "/", "-"), true // a #61 aponta para âncora na página pública
		}
		return "", false
	}}
	h := string(RenderizarSpec("[a](plano.md) [b](../README.md) [c](../x.md) [d](https://a.b) [e](mailto:x@y.z) [f](javascript:alert(1)) [g](//evil.com/x) [`cód`](../x.md) [**forte**](../../../../x.md)", links))
	contem(t, h,
		`<a href="#docs-e-tg-1-plano.md"`, `<a href="#docs-e-README.md"`, `<a href="https://a.b"`, `<a href="mailto:x@y.z"`)
	contem(t, h, " c ", "<code>cód</code>", "<strong>forte</strong>")
	naoContem(t, h, `href="../x.md"`, `>c</a>`, "javascript:", `href="//evil.com`, `href="../../`)
	// Sem LinksSpec no contexto (o detalhe da pendência), o link relativo continua como está.
	if h := string(RenderizarMarkdown("[a](plano.md)")); !strings.Contains(h, `href="plano.md"`) {
		t.Fatalf("o detalhe da pendência não devia mudar: %s", h)
	}
}
