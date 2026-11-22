package enriquecer

import (
	"context"
	"encoding/json"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/central"
)

// limitePaginas e porPagina limitam a leitura paginada das forjas (só leitura).
const (
	porPagina    = 100
	limitePagina = 50
)

// CabecaMR devolve o SHA da cabeça da MR da spec (o último commit da branch de origem).
func (f Ferramentas) CabecaMR(ctx context.Context, rawURL string) (string, error) {
	d, err := f.ConsultarMRDetalhe(ctx, rawURL)
	if err != nil {
		return "", err
	}
	if d.Head == "" {
		return "", &Falha{Codigo: ErrRespostaInvalida, Mensagem: "a forja não disse o SHA da cabeça da MR"}
	}
	return d.Head, nil
}

// ErrPastaAusente é o código da falha da conferência quando a pasta registrada não existe no
// commit, não tem nenhum arquivo ou é um arquivo: sem pasta não há o que comparar, e uma pasta
// digitada errada nunca pode virar "pode mesclar".
const ErrPastaAusente = "pasta_da_spec_ausente"

// PastaNoSHA lista os arquivos da pasta no commit sha, com o id do conteúdo de cada um (o blob):
// caminho relativo à pasta → id. Pasta que não existe no commit, sem nenhum arquivo ou que é um
// arquivo é a falha ErrPastaAusente. É o conteúdo da pasta, não a diferença de um compare: um
// rebase ou o merge da base na branch não mexe nele.
func (f Ferramentas) PastaNoSHA(ctx context.Context, rawURL, pasta, sha string) (map[string]string, error) {
	fj, err := forjaDe(rawURL)
	if err != nil {
		return nil, err
	}
	pasta = strings.Trim(pasta, "/")
	var m map[string]string
	if fj.ferramenta == "glab" {
		m, err = f.pastaGitLab(ctx, fj, pasta, sha)
	} else {
		m, err = f.pastaGitHub(ctx, fj, pasta, sha)
	}
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, &Falha{Codigo: ErrPastaAusente, Mensagem: "a pasta " + pasta + " não existe, não tem arquivos ou é um arquivo em " + sha + ": confira o --caminho da spec"}
	}
	return m, nil
}

func (fj forja) argsAPI(caminho string) []string {
	args := []string{"api"}
	if fj.host != "github.com" || fj.ferramenta == "glab" {
		args = append(args, "--hostname", fj.host)
	}
	return append(args, caminho)
}

// entradaDir é uma entrada de uma árvore da forja: o nome (caminho relativo à pasta listada), o
// tipo (blob ou tree), o modo (120000 é symlink), o id do conteúdo e, quando a forja diz, o tamanho.
type entradaDir struct {
	Nome, Tipo, Modo, ID string
	Tamanho              int64
}

// regular diz se a entrada é arquivo regular (não symlink nem submódulo).
func (e entradaDir) regular() bool {
	return e.Tipo == "blob" && (e.Modo == "" || e.Modo == "100644" || e.Modo == "100755")
}

func (f Ferramentas) arvoreGitHub(ctx context.Context, fj forja, ref string, recursivo bool) ([]entradaDir, error) {
	caminho := "repos/" + fj.projeto + "/git/trees/" + url.PathEscape(ref)
	if recursivo {
		caminho += "?recursive=1"
	}
	out, err := f.Rodar(ctx, "gh", fj.argsAPI(caminho)...)
	if err != nil {
		return nil, classificarForja(err)
	}
	var r struct {
		Tree *[]struct {
			Path string `json:"path"`
			Type string `json:"type"`
			Mode string `json:"mode"`
			SHA  string `json:"sha"`
			Size int64  `json:"size"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(out, &r); err != nil || r.Tree == nil {
		return nil, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "gh api devolveu algo que não é uma árvore: " + resumir(out)}
	}
	if r.Truncated {
		return nil, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "a árvore de " + ref + " veio truncada pela forja"}
	}
	es := make([]entradaDir, len(*r.Tree))
	for i, e := range *r.Tree {
		es[i] = entradaDir{Nome: e.Path, Tipo: e.Type, Modo: e.Mode, ID: e.SHA, Tamanho: e.Size}
	}
	return es, nil
}

// descerGitHub desce a pasta um nível por vez (a árvore do repositório inteiro pode vir truncada)
// e devolve o id da árvore dela; false se a pasta não existe no commit.
func (f Ferramentas) descerGitHub(ctx context.Context, fj forja, sha, pasta string) (string, bool, error) {
	atual := sha
	if pasta == "" {
		return atual, true, nil
	}
	for _, seg := range strings.Split(pasta, "/") {
		es, err := f.arvoreGitHub(ctx, fj, atual, false)
		if err != nil {
			return "", false, err
		}
		prox := ""
		for _, e := range es {
			if e.Nome == seg && e.Tipo == "tree" {
				prox = e.ID
			}
		}
		if prox == "" {
			return "", false, nil
		}
		atual = prox
	}
	return atual, true, nil
}

func (f Ferramentas) pastaGitHub(ctx context.Context, fj forja, pasta, sha string) (map[string]string, error) {
	atual, ok, err := f.descerGitHub(ctx, fj, sha, pasta)
	if err != nil || !ok {
		return map[string]string{}, err
	}
	es, err := f.arvoreGitHub(ctx, fj, atual, true)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, e := range es {
		if e.Tipo == "blob" {
			m[e.Nome] = e.ID
		}
	}
	return m, nil
}

// arvoreGitLab lista a pasta no commit (recursiva ou só o primeiro nível), com os nomes relativos
// a ela. Pasta que não existe no commit devolve lista vazia (o GitLab responde 404).
func (f Ferramentas) arvoreGitLab(ctx context.Context, fj forja, pasta, sha string, recursivo bool) ([]entradaDir, error) {
	var m []entradaDir
	for pagina := 1; pagina <= limitePagina; pagina++ {
		caminho := "projects/" + url.QueryEscape(fj.projeto) + "/repository/tree?path=" + url.QueryEscape(pasta) + "&ref=" + url.QueryEscape(sha)
		if recursivo {
			caminho += "&recursive=true"
		}
		caminho += "&per_page=" + strconv.Itoa(porPagina) + "&page=" + strconv.Itoa(pagina)
		out, err := f.Rodar(ctx, "glab", fj.argsAPI(caminho)...)
		if err != nil {
			err = classificarForja(err)
			if f, ok := err.(*Falha); ok && f.Codigo == ErrNaoEncontrado && pagina == 1 {
				return m, nil // o GitLab responde 404 para uma pasta que não existe no commit
			}
			return nil, err
		}
		var es []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
			Type string `json:"type"`
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(out, &es); err != nil {
			return nil, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "glab api devolveu algo que não é uma árvore: " + resumir(out)}
		}
		for _, e := range es {
			m = append(m, entradaDir{Nome: strings.TrimPrefix(strings.TrimPrefix(e.Path, pasta), "/"), Tipo: e.Type, Modo: e.Mode, ID: e.ID})
		}
		if len(es) < porPagina {
			return m, nil
		}
	}
	return nil, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "a pasta da spec tem arquivos demais para conferir"}
}

func (f Ferramentas) pastaGitLab(ctx context.Context, fj forja, pasta, sha string) (map[string]string, error) {
	es, err := f.arvoreGitLab(ctx, fj, pasta, sha, true)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, e := range es {
		if e.Tipo == "blob" {
			m[e.Nome] = e.ID
		}
	}
	return m, nil
}

// ArquivosAlterados lista os arquivos que a forja diz alterados entre os dois SHAs (o compare
// dela). Serve só de sinal sobre o que a MR mexe fora da pasta da spec: o compare do GitHub é de
// três pontos e, depois de um rebase, traz o que veio da base.
func (f Ferramentas) ArquivosAlterados(ctx context.Context, rawURL, de, para string) ([]string, error) {
	fj, err := forjaDe(rawURL)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	if fj.ferramenta == "glab" {
		caminho := "projects/" + url.QueryEscape(fj.projeto) + "/repository/compare?from=" + url.QueryEscape(de) + "&to=" + url.QueryEscape(para) + "&straight=true"
		out, err := f.Rodar(ctx, "glab", fj.argsAPI(caminho)...)
		if err != nil {
			return nil, classificarForja(err)
		}
		var r struct {
			Diffs []struct {
				Old string `json:"old_path"`
				New string `json:"new_path"`
			} `json:"diffs"`
		}
		if err := json.Unmarshal(out, &r); err != nil {
			return nil, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "glab api devolveu algo que não é um compare: " + resumir(out)}
		}
		for _, d := range r.Diffs {
			set[d.Old], set[d.New] = true, true
		}
	} else {
		for pagina := 1; pagina <= limitePagina; pagina++ {
			caminho := "repos/" + fj.projeto + "/compare/" + url.PathEscape(de) + "..." + url.PathEscape(para) + "?per_page=" + strconv.Itoa(porPagina) + "&page=" + strconv.Itoa(pagina)
			out, err := f.Rodar(ctx, "gh", fj.argsAPI(caminho)...)
			if err != nil {
				return nil, classificarForja(err)
			}
			var r struct {
				Files []struct {
					Nome     string `json:"filename"`
					Anterior string `json:"previous_filename"`
				} `json:"files"`
			}
			if err := json.Unmarshal(out, &r); err != nil {
				return nil, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "gh api devolveu algo que não é um compare: " + resumir(out)}
			}
			for _, x := range r.Files {
				set[x.Nome], set[x.Anterior] = true, true
			}
			if len(r.Files) < porPagina {
				break
			}
		}
	}
	delete(set, "")
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// CompararPastas compara o conteúdo da pasta nos dois commits e devolve os arquivos (caminhos
// relativos à pasta) que mudaram, entraram ou saíram, sem o decisoes.md, o único que pode mudar
// depois da aprovação.
func CompararPastas(aprovada, atual map[string]string) []string {
	mudou := []string{}
	for p, id := range atual {
		if p == central.ArquivoDecisoesSpec {
			continue
		}
		if antes, ok := aprovada[p]; !ok || antes != id {
			mudou = append(mudou, p)
		}
	}
	for p := range aprovada {
		if _, ok := atual[p]; !ok && p != central.ArquivoDecisoesSpec {
			mudou = append(mudou, p)
		}
	}
	sort.Strings(mudou)
	return mudou
}

// ForaDaPasta tira da lista o que está dentro da pasta (e o próprio caminho dela).
func ForaDaPasta(arquivos []string, pasta string) []string {
	pasta = strings.Trim(path.Clean(pasta), "/")
	fora := []string{}
	for _, a := range arquivos {
		if a != pasta && !strings.HasPrefix(a, pasta+"/") {
			fora = append(fora, a)
		}
	}
	return fora
}

// ConferirSpec compara a pasta da spec entre o SHA aprovado e a cabeça da MR: pode mesclar quando
// só o decisoes.md mudou nela. Arquivo de fora da pasta é outro sinal (ForaDaPasta) e não reprova a
// spec; se a consulta desse sinal falha, a conferência da pasta vale e o motivo vai junto.
func (f Ferramentas) ConferirSpec(ctx context.Context, rawURL, pasta, shaAprovado string) (central.ConferenciaSpec, error) {
	cabeca, err := f.CabecaMR(ctx, rawURL)
	if err != nil {
		return central.ConferenciaSpec{}, err
	}
	return f.ConferirSpecCabeca(ctx, rawURL, pasta, shaAprovado, cabeca)
}

// ConferirSpecCabeca é o ConferirSpec com a cabeça da MR já lida.
func (f Ferramentas) ConferirSpecCabeca(ctx context.Context, rawURL, pasta, shaAprovado, cabeca string) (central.ConferenciaSpec, error) {
	c := central.ConferenciaSpec{SHAAprovado: shaAprovado, SHACabeca: cabeca, MudouNaPasta: []string{}, ForaDaPasta: []string{}}
	if cabeca == shaAprovado {
		// Nada mudou, mas a pasta precisa existir: um --caminho errado nunca vira "pode mesclar".
		if _, err := f.PastaNoSHA(ctx, rawURL, pasta, cabeca); err != nil {
			return central.ConferenciaSpec{}, err
		}
		c.PodeMesclar = true
		return c, nil
	}
	aprovada, err := f.PastaNoSHA(ctx, rawURL, pasta, shaAprovado)
	if err != nil {
		return central.ConferenciaSpec{}, err
	}
	atual, err := f.PastaNoSHA(ctx, rawURL, pasta, cabeca)
	if err != nil {
		return central.ConferenciaSpec{}, err
	}
	c.MudouNaPasta = CompararPastas(aprovada, atual)
	c.PodeMesclar = len(c.MudouNaPasta) == 0
	if arquivos, err := f.ArquivosAlterados(ctx, rawURL, shaAprovado, cabeca); err != nil {
		c.ForaDaPastaErro = err.Error()
	} else {
		c.ForaDaPasta = ForaDaPasta(arquivos, pasta)
	}
	return c, nil
}
