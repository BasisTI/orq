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

// central atende /central: a Central nova, aba Todos. A lista de pendências segue em /.
func (s *Servidor) central(w http.ResponseWriter, r *http.Request) {
	s.mostrarCentral(w, r, "")
}

// projeto atende /projeto/{slug}: as ondas com ao menos uma US do projeto. O slug que não tem
// onda aberta é 404.
func (s *Servidor) projeto(w http.ResponseWriter, r *http.Request) {
	slug, err := refs.ValidarProjeto(r.PathValue("slug"))
	if err != nil || slug != r.PathValue("slug") {
		s.erro(w, r, http.StatusNotFound, "Não encontrada", "Não há projeto com esse nome nas ondas abertas.", "/central")
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
		s.erro(w, r, http.StatusNotFound, "Não encontrada", "Não há projeto com esse nome nas ondas abertas.", "/central")
		return
	}
	s.pagina(w, http.StatusOK, "central", centralPagina{
		base:    base{Titulo: c.H1, Usuario: usuarioDe(r), Cheia: true},
		Central: c,
		Abas:    c.Abas,
	})
}

// abaDe lê, do HX-Current-URL que o htmx envia em todo pedido, em que aba a pessoa está.
// Devolve o slug do projeto ("" para Todos, para Pendências e para o que não reconhece).
func abaDe(atual string) string {
	u, err := url.Parse(atual)
	if err != nil {
		return ""
	}
	if resto, ok := strings.CutPrefix(u.Path, "/projeto/"); ok && resto != "" && !strings.Contains(resto, "/") {
		return resto
	}
	return ""
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
	c := vistaCentral(snap, s.agora(), Filtro{Projeto: abaDe(r.Header.Get("HX-Current-URL"))})
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
