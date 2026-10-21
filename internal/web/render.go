package web

import (
	"bytes"
	"html"
	"html/template"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gohtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// md renderiza o markdown do detalhe. Sem WithUnsafe: o HTML bruto não vira HTML (aqui sai
// escapado, para a pessoa ver o que o agente escreveu) e javascript:/data: não viram link.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(linksSeguros{}, 100))),
	goldmark.WithRendererOptions(
		renderer.WithNodeRenderers(util.Prioritized(htmlEscapado{}, 50)),
		gohtml.WithHardWraps(),
	),
)

// RenderizarMarkdown devolve o HTML do markdown, já seguro para o template.
func RenderizarMarkdown(src string) template.HTML {
	var b bytes.Buffer
	if err := md.Convert([]byte(src), &b); err != nil {
		return template.HTML("<pre>" + html.EscapeString(src) + "</pre>")
	}
	return template.HTML(b.String())
}

// linksSeguros deixa passar só http, https, mailto, âncora e caminho relativo; o resto vira
// "#". Imagens perdem o endereço: a CSP não carregaria mesmo, e o alt fica no lugar.
type linksSeguros struct{}

func (linksSeguros) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	fonte := reader.Source()
	var perigosos []*ast.AutoLink
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Link:
			if !destinoSeguro(string(v.Destination)) {
				v.Destination = []byte("#")
			}
			v.SetAttributeString("rel", []byte("noopener noreferrer nofollow"))
		case *ast.AutoLink:
			if v.AutoLinkType == ast.AutoLinkURL && !destinoSeguro(string(v.URL(fonte))) {
				perigosos = append(perigosos, v)
			}
		case *ast.Image:
			v.Destination = []byte("")
		}
		return ast.WalkContinue, nil
	})
	// O que não é endereço seguro vira texto comum, escapado pelo renderer.
	for _, v := range perigosos {
		v.Parent().ReplaceChild(v.Parent(), v, ast.NewString(v.URL(fonte)))
	}
}

func destinoSeguro(d string) bool {
	d = strings.TrimSpace(d)
	if d == "" || strings.HasPrefix(d, "#") || strings.HasPrefix(d, "/") && !strings.HasPrefix(d, "//") {
		return true
	}
	// O navegador ignora tabulação e quebra de linha no esquema ("java\tscript:").
	d = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, d)
	u, err := url.Parse(d)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return true
	case "":
		return !strings.HasPrefix(d, "//") && !strings.Contains(strings.SplitN(d, "/", 2)[0], ":")
	}
	return false
}

// htmlEscapado escreve o HTML bruto como texto.
type htmlEscapado struct{}

func (htmlEscapado) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindRawHTML, func(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			raw := n.(*ast.RawHTML)
			for i := 0; i < raw.Segments.Len(); i++ {
				seg := raw.Segments.At(i)
				_, _ = w.WriteString(html.EscapeString(string(seg.Value(source))))
			}
		}
		return ast.WalkSkipChildren, nil
	})
	reg.Register(ast.KindHTMLBlock, func(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			blk := n.(*ast.HTMLBlock)
			_, _ = w.WriteString("<pre class=\"bruto\">")
			for i := 0; i < blk.Lines().Len(); i++ {
				seg := blk.Lines().At(i)
				_, _ = w.WriteString(html.EscapeString(string(seg.Value(source))))
			}
			if blk.HasClosure() {
				seg := blk.ClosureLine
				_, _ = w.WriteString(html.EscapeString(string(seg.Value(source))))
			}
			_, _ = w.WriteString("</pre>\n")
		}
		return ast.WalkSkipChildren, nil
	})
}
