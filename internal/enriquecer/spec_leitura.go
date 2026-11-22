package enriquecer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/BasisTI/orq/internal/central"
)

// SHADaBranch devolve o SHA da cabeça da branch na forja da MR dada (só leitura). É a ref de
// leitura da spec depois que a MR foi mesclada: a branch alvo.
func (f Ferramentas) SHADaBranch(ctx context.Context, rawURL, branch string) (string, error) {
	fj, err := forjaDe(rawURL)
	if err != nil {
		return "", err
	}
	if branch == "" {
		return "", &Falha{Codigo: ErrRespostaInvalida, Mensagem: "a forja não disse a branch alvo da MR"}
	}
	var sha string
	if fj.ferramenta == "glab" {
		out, err := f.Rodar(ctx, "glab", fj.argsAPI("projects/"+url.QueryEscape(fj.projeto)+"/repository/branches/"+url.PathEscape(branch))...)
		if err != nil {
			return "", classificarForja(err)
		}
		var r struct {
			Commit struct {
				ID string `json:"id"`
			} `json:"commit"`
		}
		if err := json.Unmarshal(out, &r); err != nil {
			return "", &Falha{Codigo: ErrRespostaInvalida, Mensagem: "glab api devolveu algo que não é uma branch: " + resumir(out)}
		}
		sha = r.Commit.ID
	} else {
		out, err := f.Rodar(ctx, "gh", fj.argsAPI("repos/"+fj.projeto+"/branches/"+url.PathEscape(branch))...)
		if err != nil {
			return "", classificarForja(err)
		}
		var r struct {
			Commit struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		if err := json.Unmarshal(out, &r); err != nil {
			return "", &Falha{Codigo: ErrRespostaInvalida, Mensagem: "gh api devolveu algo que não é uma branch: " + resumir(out)}
		}
		sha = r.Commit.SHA
	}
	if sha == "" {
		return "", &Falha{Codigo: ErrRespostaInvalida, Mensagem: "a forja não disse o SHA da branch " + branch}
	}
	return sha, nil
}

// listarDir lista os arquivos e subpastas diretamente na pasta, no commit sha (sem descer).
// Pasta que não existe no commit devolve lista vazia.
func (f Ferramentas) listarDir(ctx context.Context, fj forja, sha, pasta string) ([]entradaDir, error) {
	if fj.ferramenta == "glab" {
		return f.arvoreGitLab(ctx, fj, pasta, sha, false)
	}
	id, ok, err := f.descerGitHub(ctx, fj, sha, pasta)
	if err != nil || !ok {
		return nil, err
	}
	return f.arvoreGitHub(ctx, fj, id, false)
}

// lerBlob lê o conteúdo de um blob pelo id que a listagem da árvore deu (não pelo caminho: o que
// se lê é exatamente o que foi listado, regular e no commit pedido).
func (f Ferramentas) lerBlob(ctx context.Context, fj forja, id string) ([]byte, error) {
	if fj.ferramenta == "glab" {
		out, err := f.Rodar(ctx, "glab", fj.argsAPI("projects/"+url.QueryEscape(fj.projeto)+"/repository/blobs/"+url.PathEscape(id)+"/raw")...)
		if err != nil {
			return nil, classificarForja(err)
		}
		return out, nil
	}
	out, err := f.Rodar(ctx, "gh", fj.argsAPI("repos/"+fj.projeto+"/git/blobs/"+url.PathEscape(id))...)
	if err != nil {
		return nil, classificarForja(err)
	}
	var r struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "gh api devolveu algo que não é um blob: " + resumir(out)}
	}
	switch r.Encoding {
	case "base64":
		b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(r.Content), ""))
		if err != nil {
			return nil, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "gh api devolveu um blob em base64 inválido"}
		}
		return b, nil
	case "utf-8":
		return []byte(r.Content), nil
	}
	return nil, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "gh api devolveu um blob com codificação " + r.Encoding}
}

// LinksRelativos devolve o destino de cada link do markdown, na ordem em que aparecem (o que o
// parser resolveu: links em linha e por referência). Quem chama decide o que é relativo.
func LinksRelativos(src []byte) []string {
	doc := goldmark.New().Parser().Parse(text.NewReader(src))
	var out []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if l, ok := n.(*ast.Link); ok && entering {
			out = append(out, string(l.Destination))
		}
		return ast.WalkContinue, nil
	})
	return out
}

// ordemDaPasta põe spec.md, plano.md e decisoes.md na frente, nessa ordem; o resto vem depois, por nome.
func ordemDaPasta(nome string) int {
	switch nome {
	case "spec.md":
		return 0
	case "plano.md":
		return 1
	case central.ArquivoDecisoesSpec:
		return 2
	}
	return 3
}

// leitorSpec acumula os arquivos lidos e o orçamento do cache (1 MiB por arquivo, 4 MiB por spec).
type leitorSpec struct {
	f      Ferramentas
	fj     forja
	gasto  int64
	saida  []central.ArquivoSpec
	vistos map[string]bool
}

// ler lê o arquivo e o junta à saída. Acima do limite o conteúdo não é guardado: o arquivo sai
// marcado como grande demais, com o tamanho. O conteúdo volta (para a busca de links) só se foi guardado.
func (l *leitorSpec) ler(ctx context.Context, e entradaDir, caminho, papel string) (string, error) {
	a := central.ArquivoSpec{Caminho: caminho, Papel: papel, Ordem: len(l.saida)}
	var conteudo string
	switch {
	case e.Tamanho > central.LimiteArquivoSpec:
		a.Tamanho, a.GrandeDemais = e.Tamanho, true
	default:
		b, err := l.f.lerBlob(ctx, l.fj, e.ID)
		if err != nil {
			return "", err
		}
		a.Tamanho = int64(len(b))
		if a.Tamanho > central.LimiteArquivoSpec || l.gasto+a.Tamanho > central.LimiteSpec {
			a.GrandeDemais = true
		} else {
			conteudo = strings.ToValidUTF8(string(b), "�")
			a.Conteudo = conteudo
			l.gasto += a.Tamanho
		}
	}
	l.vistos[caminho] = true
	l.saida = append(l.saida, a)
	return conteudo, nil
}

// LerSpecNoSHA lê a spec no commit sha pela API da forja, sem tocar o disco: os `.md` diretamente
// na pasta da US, o README.md do épico (a pasta acima) e os `.md` diretamente na pasta do épico que
// algum arquivo da US referencia por link relativo. Arquivo que não é regular (symlink), `.md` em
// subpasta e o que um link normalizado leva para fora da pasta do épico ficam de fora. Pasta da
// spec sem arquivo é a falha ErrPastaAusente.
func (f Ferramentas) LerSpecNoSHA(ctx context.Context, rawURL, pasta, sha string) ([]central.ArquivoSpec, error) {
	fj, err := forjaDe(rawURL)
	if err != nil {
		return nil, err
	}
	pasta = path.Clean(strings.Trim(pasta, "/"))
	es, err := f.listarDir(ctx, fj, sha, pasta)
	if err != nil {
		return nil, err
	}
	var doc []entradaDir
	for _, e := range es {
		if e.regular() && strings.HasSuffix(e.Nome, ".md") && !strings.Contains(e.Nome, "/") {
			doc = append(doc, e)
		}
	}
	if len(doc) == 0 {
		return nil, &Falha{Codigo: ErrPastaAusente, Mensagem: "a pasta " + pasta + " não existe, não tem arquivo .md ou é um arquivo em " + sha + ": confira o --caminho da spec"}
	}
	sort.SliceStable(doc, func(i, j int) bool {
		a, b := ordemDaPasta(doc[i].Nome), ordemDaPasta(doc[j].Nome)
		if a != b {
			return a < b
		}
		return doc[i].Nome < doc[j].Nome
	})
	l := &leitorSpec{f: f, fj: fj, vistos: map[string]bool{}}
	var links []string
	for _, e := range doc {
		conteudo, err := l.ler(ctx, e, pasta+"/"+e.Nome, central.PapelPasta)
		if err != nil {
			return nil, err
		}
		for _, d := range LinksRelativos([]byte(conteudo)) {
			if p, ok := central.ResolverLinkSpec(pasta, d); ok {
				links = append(links, p)
			}
		}
	}
	epico := path.Dir(pasta)
	if epico == "." {
		return l.saida, nil
	}
	ees, err := f.listarDir(ctx, fj, sha, epico)
	if err != nil {
		return nil, err
	}
	regulares := map[string]entradaDir{}
	for _, e := range ees {
		if e.regular() && strings.HasSuffix(e.Nome, ".md") && !strings.Contains(e.Nome, "/") {
			regulares[epico+"/"+e.Nome] = e
		}
	}
	// O README do épico sempre; depois, na ordem em que os links aparecem, os .md da pasta do épico.
	alvos := append([]string{epico + "/README.md"}, links...)
	for _, p := range alvos {
		e, ok := regulares[p]
		if !ok || l.vistos[p] || !central.MdDiretoNaPasta(p, epico) {
			continue
		}
		if _, err := l.ler(ctx, e, p, central.PapelEpico); err != nil {
			return nil, err
		}
	}
	return l.saida, nil
}
