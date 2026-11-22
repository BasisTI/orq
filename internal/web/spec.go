package web

import (
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// A spec na Central (US #60): a página da spec da US e as abas dentro da pendência de validação. O
// conteúdo vem do cache que o daemon gravou da leitura pela forja (central.SpecCache); a web não
// lê a forja nem o disco.

// paramAba é o parâmetro da URL que escolhe a aba. A aba é um link comum (sem script): a página
// inteira é pedida de novo com a aba na URL.
const paramAba = "aba"

// SpecAba é uma aba da spec: um arquivo lido.
type SpecAba struct {
	Chave, Rotulo, Href string
	Atual               bool
	// Grande: o arquivo passou do limite e o cache não guardou o conteúdo.
	Grande bool
}

// SpecPainel é o que o modelo "spec-painel" desenha: as abas, o arquivo da aba atual renderizado e
// os avisos da leitura. Tem é falso quando não há versão para mostrar (só o Aviso).
type SpecPainel struct {
	Tem bool
	// Abas vazio com Tem verdadeiro não acontece: a leitura sem arquivo é falha, não versão.
	Abas     []SpecAba
	Titulo   string
	Conteudo template.HTML
	// Grande: o arquivo da aba atual não foi guardado; Tamanho e MRURL montam o aviso.
	Grande  bool
	KiB     int64
	MRURL   string
	Caminho string
	// Faixa do topo: estado da spec, ref, SHA e hora da leitura.
	Estado                     Selo
	Ref, SHA, SHACurto, LidaEm string
	Versao                     string // "atual" ou "da validação"
	// Aviso explica o que falta (leitura ainda não feita, versão não guardada); Erro, a última falha
	// da leitura, com a hora (o conteúdo mostrado é o último bom).
	Aviso, Erro, ErroEm string
	// Atual é o link para a spec atual, no aviso de versão que não está guardada.
	LinkAtual string
}

// chaveAba é o identificador do arquivo na URL: o nome do arquivo na pasta da US, e "epico:" mais o
// nome para os da pasta do épico.
func chaveAba(a central.ArquivoSpec) string {
	if a.Papel == central.PapelEpico {
		return "epico:" + path.Base(a.Caminho)
	}
	return path.Base(a.Caminho)
}

func rotuloAba(a central.ArquivoSpec) string {
	nome := path.Base(a.Caminho)
	if a.Papel == central.PapelEpico {
		if nome == "README.md" {
			return "README do épico"
		}
		return "Épico: " + strings.TrimSuffix(nome, ".md")
	}
	switch nome {
	case "spec.md":
		return "Spec"
	case "plano.md":
		return "Plano"
	case central.ArquivoDecisoesSpec:
		return "Decisões"
	}
	return strings.TrimSuffix(nome, ".md")
}

// painelSpec monta o painel da versão pedida. href dá o endereço de cada aba (a página da spec e a
// da pendência têm o seu); sha é o que foi pedido ("" é a atual) e serve ao aviso de versão ausente.
func (s *Servidor) painelSpec(sc central.SpecCache, sha, aba string, href func(chave string) string) SpecPainel {
	p := SpecPainel{MRURL: sc.Spec.MRURL, Caminho: sc.Spec.Caminho}
	if e, ok := selosSpec[sc.Spec.Estado]; ok {
		p.Estado = e
	} else {
		p.Estado = Selo{Cls: "neutro", Texto: "spec " + sc.Spec.Estado}
	}
	if l := sc.Leitura; l != nil && l.Erro != nil {
		p.Erro = l.Erro.Codigo + ": " + l.Erro.Mensagem
		if l.Erro.Em != "" {
			p.ErroEm = s.quando(l.Erro.Em)
		}
	}
	v := sc.Versao
	if v == nil {
		switch {
		case sha != "":
			p.Aviso = "A versão da validação (SHA " + shaCurto(sha) + ") não está guardada: ela é descartada depois que a pendência é respondida, ou o daemon ainda não a leu."
		case sc.Leitura == nil || sc.Leitura.SHA == nil:
			p.Aviso = "O daemon ainda não leu a spec na forja."
		default:
			p.Aviso = "A versão atual da spec não está guardada."
		}
		return p
	}
	p.Tem = true
	p.SHA, p.SHACurto, p.LidaEm = v.SHA, shaCurto(v.SHA), s.quando(v.LidaEm)
	p.Versao = "atual"
	if sha != "" {
		p.Versao = "da validação"
	}
	porCaminho := map[string]string{}
	for _, a := range v.Arquivos {
		porCaminho[a.Caminho] = chaveAba(a)
	}
	atual := v.Arquivos[0]
	for _, a := range v.Arquivos {
		if chaveAba(a) == aba {
			atual = a
		}
	}
	for _, a := range v.Arquivos {
		k := chaveAba(a)
		p.Abas = append(p.Abas, SpecAba{Chave: k, Rotulo: rotuloAba(a), Href: href(k), Atual: a.Caminho == atual.Caminho, Grande: a.GrandeDemais})
	}
	p.Titulo = rotuloAba(atual)
	if atual.GrandeDemais {
		p.Grande, p.KiB = true, (atual.Tamanho+1023)/1024
		return p
	}
	p.Conteudo = RenderizarSpec(atual.Conteudo, LinksSpec{
		Base: path.Dir(atual.Caminho),
		Destino: func(caminho string) (string, bool) {
			k, ok := porCaminho[caminho]
			if !ok {
				return "", false
			}
			return href(k), true
		},
	})
	return p
}

// hrefAbaSpec monta o endereço de uma aba na página da spec.
func hrefAbaSpec(u refs.US) func(string) string {
	base := refs.RotaSpec(u.Projeto, u.Ref)
	return func(chave string) string { return base + "?" + paramAba + "=" + url.QueryEscape(chave) }
}

// specDe lê a US do caminho da página da spec (/p/{projeto}/{ref}/spec).
func specDe(r *http.Request) (refs.US, bool) {
	projeto, err := refs.ValidarProjeto(r.PathValue("projeto"))
	ref, e := strconv.Atoi(r.PathValue("ref"))
	if err != nil || e != nil || ref <= 0 || projeto != r.PathValue("projeto") {
		return refs.US{}, false
	}
	return refs.US{Projeto: projeto, Ref: ref}, true
}

// specPagina é o que o modelo "spec" recebe.
type specPagina struct {
	base
	US      string
	Voltar  string
	Painel  SpecPainel
	Titulo2 string
}

// spec atende GET /p/{projeto}/{ref}/spec: a spec da US, com uma aba por arquivo.
func (s *Servidor) spec(w http.ResponseWriter, r *http.Request) {
	u, ok := specDe(r)
	if !ok {
		s.erro(w, r, http.StatusNotFound, "Página não encontrada", "Endereço de spec inválido.", "/")
		return
	}
	sc, err := s.fonte().LerSpecCache(r.Context(), u, "")
	if err != nil {
		s.falha(w, r, err, "/")
		return
	}
	p := s.painelSpec(sc, "", r.URL.Query().Get(paramAba), hrefAbaSpec(u))
	p.Ref = refs.Exibir(u.Ref)
	s.pagina(w, http.StatusOK, "spec", specPagina{
		base:   s.comAbas(r, base{Titulo: "Spec da " + refs.Exibir(u.Ref), Usuario: usuarioDe(r), Larga: true}, abaTodos),
		US:     refs.Exibir(u.Ref),
		Voltar: "/",
		Painel: p,
	})
}

// specDaValidacao monta o painel da spec dentro da pendência de validação: a versão do SHA que a
// pendência registrou, não a cabeça. Sem spec registrada ou sem leitura devolve o painel só com o aviso.
func (s *Servidor) specDaValidacao(r *http.Request, ref refs.Pend, p central.Pendencia) (SpecPainel, bool) {
	if p.Tipo != "validacao" || p.SHAValidacao == nil {
		return SpecPainel{}, false
	}
	sc, err := s.fonte().LerSpecCache(r.Context(), ref.US, *p.SHAValidacao)
	if err != nil {
		return SpecPainel{Aviso: "Não foi possível ler a spec desta US: " + output.AsError(err).Cause}, true
	}
	base := refs.RotaPendencia(ref.US.Projeto, ref.US.Ref, ref.Numero)
	href := func(chave string) string { return base + "?" + paramAba + "=" + url.QueryEscape(chave) + "#spec" }
	pn := s.painelSpec(sc, *p.SHAValidacao, r.URL.Query().Get(paramAba), href)
	pn.Ref = refs.Exibir(ref.US.Ref)
	pn.LinkAtual = refs.RotaSpec(ref.US.Projeto, ref.US.Ref)
	return pn, true
}
