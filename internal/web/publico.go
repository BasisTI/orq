package web

import (
	"context"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"path"
	"strconv"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/hora"
	"github.com/BasisTI/orq/internal/output"
)

// A página pública da spec (US #61): o que o usuário final vê pelo link. Roda num socket próprio
// (orq daemon --web-publico), que só responde a GET /s/{codigo} e aos estáticos da página; qualquer
// outra rota dá o mesmo 404. O handler só recebe a FontePublica: sem Snapshot, sem Store, sem
// escrita além do contador de acessos.

// FontePublica é tudo o que o handler público pode pedir à Central: a spec do link pelo hash do
// código e o registro de um acesso.
type FontePublica interface {
	SpecPorHash(ctx context.Context, hash string) (central.SpecPublica, error)
	MarcarAcesso(ctx context.Context, hash string) error
}

var _ FontePublica = (*central.Store)(nil)

// Publico atende o socket público.
type Publico struct {
	Fonte FontePublica
	// Log recebe uma linha por requisição, só com a rota (o padrão do mux, nunca o caminho: o código
	// vai na URL) e o status.
	Log func(format string, args ...any)

	modelos  *template.Template
	estatico *estaticos
}

// SecaoPublica é um arquivo da spec na página: a âncora, o título e o conteúdo renderizado.
type SecaoPublica struct {
	ID, Rotulo string
	Conteudo   template.HTML
	// Grande: o arquivo passou do limite e o cache não guardou o conteúdo.
	Grande bool
	KiB    int64
}

// paginaPublica é o que o modelo "publico" recebe.
type paginaPublica struct {
	Titulo      string
	Estado      string
	EstadoCls   string
	LidaEm      string
	AvisoVersao string
	// Secoes vazio: o daemon ainda não leu a spec.
	Secoes []SecaoPublica
}

var selosPublicos = map[string]string{
	central.PublicoEmValidacao: "aviso",
	central.PublicoAprovada:    "ok",
	central.PublicoEntregue:    "ok",
}

// ancorasPublicas dá a âncora de cada arquivo da página. As âncoras não saem do nome do arquivo
// (que pode citar ref ou projeto): spec, plano e decisões da pasta da US têm nome fixo, e o resto
// é numerado. O prefixo separa a pasta da US (`us-`) do épico (`epico-`): um README.md extra da
// pasta da US (`us-extra-N`) não colide com o README do épico (`epico-readme`).
func ancorasPublicas(arqs []central.ArquivoSpec) map[string]string {
	out := map[string]string{}
	extra, comp := 0, 0
	for _, a := range arqs {
		nome := path.Base(a.Caminho)
		switch {
		case a.Papel == central.PapelEpico && nome == "README.md":
			out[a.Caminho] = "epico-readme"
		case a.Papel == central.PapelEpico:
			comp++
			out[a.Caminho] = "epico-compartilhada-" + strconv.Itoa(comp)
		case nome == "spec.md":
			out[a.Caminho] = "us-spec"
		case nome == "plano.md":
			out[a.Caminho] = "us-plano"
		case nome == central.ArquivoDecisoesSpec:
			out[a.Caminho] = "us-decisoes"
		default:
			extra++
			out[a.Caminho] = "us-extra-" + strconv.Itoa(extra)
		}
	}
	return out
}

// montarPagina transforma a spec pública na página: uma seção por arquivo, na ordem do cache (spec,
// plano, decisões, os outros .md da pasta da US, o README do épico e as specs compartilhadas).
func montarPagina(sp central.SpecPublica) paginaPublica {
	p := paginaPublica{Titulo: sp.Titulo, Estado: sp.Estado, EstadoCls: selosPublicos[sp.Estado]}
	if p.Titulo == "" {
		p.Titulo = "Especificação"
	}
	if p.EstadoCls == "" {
		p.EstadoCls = "neutro"
	}
	if sp.Versao >= 2 && sp.SubstituidaEm != "" {
		p.AvisoVersao = fmt.Sprintf("Versão %d — substituída em %s", sp.Versao, hora.DataHoraRFC(sp.SubstituidaEm))
	}
	if len(sp.Arquivos) == 0 {
		return p
	}
	p.LidaEm = hora.DataHoraRFC(sp.LidaEm)
	anc := ancorasPublicas(sp.Arquivos)
	for _, a := range sp.Arquivos {
		sec := SecaoPublica{ID: anc[a.Caminho], Rotulo: rotuloAba(a)}
		if a.GrandeDemais {
			sec.Grande, sec.KiB = true, (a.Tamanho+1023)/1024
		} else {
			sec.Conteudo = RenderizarSpec(a.Conteudo, LinksSpec{
				Base: path.Dir(a.Caminho),
				Destino: func(caminho string) (string, bool) {
					id, ok := anc[caminho]
					return "#" + id, ok
				},
			})
		}
		p.Secoes = append(p.Secoes, sec)
	}
	return p
}

func (p *Publico) preparar() {
	if p.modelos == nil {
		p.estatico = carregarEstaticos()
		p.modelos = template.Must(template.New("").Funcs(template.FuncMap{"ver": p.estatico.versao}).ParseFS(modelosFS, "tmpl/publico.html"))
	}
}

// Handler monta as rotas do socket público. Nada além de GET /s/{codigo} e dos estáticos responde.
func (p *Publico) Handler() http.Handler {
	p.preparar()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /s/{codigo}", p.rota("/s/{codigo}", p.spec))
	mux.HandleFunc("GET /estatico/{caminho...}", p.rota("/estatico/{caminho...}", func(w http.ResponseWriter, r *http.Request) { servirEstaticoDe(p.estatico, w, r) }))
	// Todo o resto, de qualquer método, dá o mesmo 404 do código que não abre.
	mux.HandleFunc("/", p.rota("/", func(w http.ResponseWriter, _ *http.Request) { p.naoEncontrado(w) }))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Robots-Tag", "noindex")
		mux.ServeHTTP(w, r)
	})
}

// rota registra o status da resposta no log pelo nome da rota, nunca pelo caminho pedido.
func (p *Publico) rota(nome string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rs := &respostaStatus{ResponseWriter: w}
		h(rs, r)
		if p.Log != nil {
			p.Log("web público %s %d", nome, rs.status)
		}
	}
}

func (p *Publico) spec(w http.ResponseWriter, r *http.Request) {
	codigo := r.PathValue("codigo")
	if !central.CodigoLinkValido(codigo) {
		p.naoEncontrado(w)
		return
	}
	hash := central.HashCodigoLink(codigo)
	sp, err := p.Fonte.SpecPorHash(r.Context(), hash)
	if err != nil {
		if output.AsError(err).Exit == output.ExitNotFound {
			p.naoEncontrado(w)
			return
		}
		if p.Log != nil {
			p.Log("web público: erro interno: %s", output.AsError(err).Code)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintln(w, "Indisponível. Tente de novo em instantes.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := p.modelos.ExecuteTemplate(w, "publico", montarPagina(sp)); err != nil {
		if p.Log != nil {
			p.Log("web público: modelo: %v", err)
		}
		return
	}
	if err := p.Fonte.MarcarAcesso(r.Context(), hash); err != nil && p.Log != nil {
		p.Log("web público: acesso não registrado: %s", output.AsError(err).Code)
	}
}

// naoEncontrado é a única resposta para código inexistente, revogado, expirado e rota desconhecida.
func (p *Publico) naoEncontrado(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_ = p.modelos.ExecuteTemplate(w, "publico-404", struct{}{})
}

// Servir atende l até ctx acabar e então encerra com prazo.
func (p *Publico) Servir(ctx context.Context, l net.Listener) error {
	return servirHandler(ctx, p.Handler(), l)
}
