package web

import (
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/refs"
)

// centralPagina é o que o modelo "central" recebe.
type centralPagina struct {
	base
	Central
	// Abas desfaz a ambiguidade entre base.Abas e Central.Abas (a mesma faixa).
	Abas []Aba
}

// inicio atende /: a Central, aba Todos. Os filtros da página antiga (?coord=&projeto=&onda=)
// passam para /pendencias com a mesma query, em vez de serem ignorados em silêncio (spec §2.2).
func (s *Servidor) inicio(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Has("coord") || q.Has("projeto") || q.Has("onda") {
		http.Redirect(w, r, "/pendencias?"+r.URL.RawQuery, http.StatusPermanentRedirect)
		return
	}
	s.mostrarCentral(w, r, "")
}

// centralAntiga atende /central, que virou /: 308, para os links e a skill que ainda o usam.
func (s *Servidor) centralAntiga(w http.ResponseWriter, r *http.Request) {
	destino := "/"
	if r.URL.RawQuery != "" {
		destino += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, destino, http.StatusPermanentRedirect)
}

// projeto atende /projeto/{slug}: as ondas com ao menos uma US do projeto. O slug que não tem
// onda aberta é 404.
func (s *Servidor) projeto(w http.ResponseWriter, r *http.Request) {
	slug, err := refs.ValidarProjeto(r.PathValue("slug"))
	if err != nil || slug != r.PathValue("slug") {
		s.erro(w, r, http.StatusNotFound, "Não encontrada", "Não há projeto com esse nome nas ondas abertas.", "/")
		return
	}
	s.mostrarCentral(w, r, slug)
}

func (s *Servidor) mostrarCentral(w http.ResponseWriter, r *http.Request, slug string) {
	// O snapshot é lido uma vez e serve a página inteira (spec §2.1).
	snap, err := s.fonte().Snapshot(r.Context(), false)
	if err != nil {
		s.falha(w, r, err, "/")
		return
	}
	c := vistaCentral(snap, s.agora(), Filtro{Projeto: slug})
	if c.ProjetoSemOnda {
		s.erro(w, r, http.StatusNotFound, "Não encontrada", "Não há projeto com esse nome nas ondas abertas.", "/")
		return
	}
	s.pagina(w, http.StatusOK, "central", centralPagina{
		base:    base{Titulo: c.H1, Usuario: usuarioDe(r), Cheia: true},
		Central: c,
		Abas:    c.Abas,
	})
}

// Em que aba a pessoa está, lido do HX-Current-URL.
const (
	semAba       = ""
	abaDeTodos   = "todos"
	abaDePend    = "pendencias"
	abaDeProjeto = "projeto"
)

// AbaAtual é a aba lida do HX-Current-URL: o tipo, o slug do projeto (só na aba de projeto) e a
// query (o filtro, na aba Pendências).
type AbaAtual struct {
	Tipo, Projeto string
	Query         url.Values
}

// abaDe lê, do HX-Current-URL que o htmx envia em todo pedido, em que aba a pessoa está.
// Reconhece /, /central, /projeto/{slug} e /pendencias; o que não reconhece é semAba.
func abaDe(atual string) AbaAtual {
	u, err := url.Parse(atual)
	if err != nil {
		return AbaAtual{}
	}
	switch {
	case u.Path == "/" || u.Path == "/central":
		return AbaAtual{Tipo: abaDeTodos}
	case u.Path == "/pendencias":
		return AbaAtual{Tipo: abaDePend, Query: u.Query()}
	}
	if resto, ok := strings.CutPrefix(u.Path, "/projeto/"); ok && resto != "" && !strings.Contains(resto, "/") {
		return AbaAtual{Tipo: abaDeProjeto, Projeto: resto}
	}
	return AbaAtual{}
}

// filtroDe lê o filtro da aba Pendências de uma query.
func filtroDe(q url.Values) FiltroPend {
	return FiltroPend{Projeto: q.Get("projeto"), Coord: q.Get("coord"), Onda: q.Get("onda")}
}

var (
	hashValido  = regexp.MustCompile(`^[0-9a-f]{1,32}$`)
	listaValida = regexp.MustCompile(`^[a-z0-9_,-]*$`)
)

// conjunto lê uma lista separada por vírgula (vazia ou inválida: conjunto vazio).
func conjunto(s string) map[string]bool {
	m := map[string]bool{}
	if len(s) > 8192 || !listaValida.MatchString(s) {
		return m
	}
	for _, x := range strings.Split(s, ",") {
		if x != "" {
			m[x] = true
		}
	}
	return m
}

// vivo atende a atualização de 30 s (spec §6). A resposta é só hx-swap-oob; o elemento que a
// pede tem hx-swap="none" e nada nele é trocado. Troca a situação, a idade dos dados e, só para
// os ids que a página diz ter (o="ondas", u="US", a="abas", no hx-vals do #vivo), as barras das
// ondas e das US, as contagens das abas e, no corpo de cada US, o status do Taiga, as MRs e a lista
// de pendências. Nunca devolve o formulário nem o corpo de uma pendência.
//
// A lista de pendências da US é atualizada por diferença contra o que a página diz ter (pi, os
// "anchor:estado" do hx-include): o item novo entra no fim, o que mudou de estado é trocado e o
// que saiu é removido; o que não mudou não vai na resposta. Item com formulário aberto ou texto
// digitado não é trocado nem removido: o app.js o retém (htmx:oobBeforeSwap), marca como
// desatualizado e aplica a mudança quando a pessoa o fecha ou envia (spec §6).
//
// A lista de ondas e US não muda embaixo da mão de quem lê: se o hash (v) da página difere do do
// servidor (onda nova ou encerrada, ordem por atenção), nada é inserido, removido nem reordenado;
// os alvos que a página tem continuam sendo atualizados no lugar, e a resposta traz o aviso "A
// lista mudou. Atualizar".
func (s *Servidor) vivo(w http.ResponseWriter, r *http.Request) {
	snap, err := s.fonte().Snapshot(r.Context(), false)
	if err != nil {
		// hx-swap="none": o erro não troca nada na página; o próximo ciclo tenta de novo.
		if s.Log != nil {
			s.Log("web: vivo: %v", err)
		}
		http.Error(w, "indisponível", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	aba := abaDe(r.Header.Get("HX-Current-URL"))
	if aba.Tipo == abaDePend {
		s.vivoPendencias(w, snap, q, aba)
		return
	}
	c := vistaCentral(snap, s.agora(), Filtro{Projeto: aba.Projeto})
	igual := q.Get("v") == c.Hash
	ondas, us, abas := conjunto(q.Get("o")), conjunto(q.Get("u")), conjunto(q.Get("a"))
	tem := pendDaPagina(q["pi"])
	c.Situacao.Oob, c.FontesOob = true, true
	var as []Aba
	for _, a := range c.Abas {
		if abas[a.ID] {
			a.Oob = true
			as = append(as, a)
		}
	}
	c.Abas = as
	var os []OndaVista
	for _, o := range c.Ondas {
		if !ondas[strconv.FormatInt(o.ID, 10)] {
			continue
		}
		o.Oob = true
		var uu []USVista
		for _, u := range o.US {
			if us[u.Anchor] {
				u.Oob = true
				u.PendMuda = pendMuda(u, tem)
				uu = append(uu, u)
			}
		}
		o.US = uu
		os = append(os, o)
	}
	c.Ondas = os
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = s.modelos.ExecuteTemplate(w, "vivo", struct {
		Central
		Igual bool
	}{c, igual})
	if err != nil && s.Log != nil {
		s.Log("web: modelo vivo: %v", err)
	}
}

// pendDaPagina lê os "pi" (anchor:estado) das pendências que a página tem. Valor que não tem a
// forma esperada é ignorado.
func pendDaPagina(pi []string) map[string]string {
	m := map[string]string{}
	for i, v := range pi {
		if i >= 2000 {
			break
		}
		if a, e, ok := strings.Cut(v, ":"); ok && len(v) < 200 && pendAnchorValido.MatchString(a) && listaValida.MatchString(e) {
			m[a] = e
		}
	}
	return m
}

var pendAnchorValido = regexp.MustCompile(`^pend-[a-z0-9_-]+-[0-9]+-[0-9]+$`)

// pendMuda compara a lista de pendências da US com a que a página tem (tem: anchor -> estado).
// Só mexe no que mudou: insere a nova, troca a que mudou de estado e remove a que saiu da lista;
// o que não mudou (aberto, com texto digitado) não vai na resposta.
func pendMuda(u USVista, tem map[string]string) []PendMuda {
	var out []PendMuda
	alvo := u.Anchor + "-pend"
	prefixo := "pend-" + strings.TrimPrefix(u.Anchor, "us-") + "-"
	vistas := map[string]bool{}
	for _, p := range u.Pendencias {
		vistas[p.Anchor] = true
		estado, na := tem[p.Anchor]
		switch {
		case !na:
			out = append(out, PendMuda{Acao: "inserir", Alvo: alvo, Item: p})
		case estado != p.Estado:
			p.Oob = true
			out = append(out, PendMuda{Acao: "trocar", Item: p})
		}
	}
	for a := range tem {
		if resto, ok := strings.CutPrefix(a, prefixo); ok && numeroPuro.MatchString(resto) && !vistas[a] {
			out = append(out, PendMuda{Acao: "remover", Item: PendItem{Anchor: a}})
		}
	}
	// Na ordem do número da pendência (P2 antes de P10), que é a da carga nova.
	sort.Slice(out, func(i, j int) bool {
		a, b := numeroDe(out[i].Item.Anchor), numeroDe(out[j].Item.Anchor)
		if a != b {
			return a < b
		}
		return out[i].Item.Anchor < out[j].Item.Anchor
	})
	return out
}

var numeroPuro = regexp.MustCompile(`^[0-9]+$`)

// numeroDe é o número da pendência no fim do anchor ("pend-proj-12-3" é 3).
func numeroDe(anchor string) int {
	i := strings.LastIndexByte(anchor, '-')
	n, _ := strconv.Atoi(anchor[i+1:])
	return n
}
