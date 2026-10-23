package web

import (
	"net/http"
	"net/url"
	"regexp"
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
// ondas, as linhas das US e as contagens das abas (só o <summary>, nunca o <details>). Nunca
// devolve formulário, campo de texto nem corpo de pendência.
//
// A lista não muda embaixo da mão de quem lê: se o hash (v) da página difere do do servidor
// (onda nova ou encerrada, ordem por atenção, pendência nova), nada é inserido, removido nem
// reordenado; os alvos que a página tem continuam sendo atualizados no lugar, e a resposta traz
// o aviso "A lista mudou. Atualizar".
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
