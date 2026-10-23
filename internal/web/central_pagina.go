package web

import (
	"net/http"
	"net/url"
	"regexp"
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

var hashValido = regexp.MustCompile(`^[0-9a-f]{1,32}$`)

// vivo atende a atualização de 30 s (spec §6). A resposta é só hx-swap-oob; o elemento que a
// pede tem hx-swap="none" e nada nele é trocado. Troca as barras das ondas e as linhas das US
// (só o <summary>, nunca o <details>), a situação, as contagens das abas e a idade dos dados.
// Nunca devolve formulário, campo de texto nem corpo de pendência.
//
// A pessoa lê uma lista que não pode mudar embaixo da mão: se o hash (v) que a página tem não é
// o do servidor, a composição mudou. Aí a resposta não toca em nada que dependa dela (barras,
// US, abas, que podem nem existir na página) e traz o aviso "A lista mudou".
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
	c := vistaCentral(snap, s.agora(), Filtro{Projeto: abaDe(r.Header.Get("HX-Current-URL"))})
	v := r.URL.Query().Get("v")
	igual := v == c.Hash
	if !hashValido.MatchString(v) {
		v = c.Hash
	}
	c.Situacao.Oob, c.FontesOob = true, true
	for i := range c.Abas {
		c.Abas[i].Oob = true
	}
	for i := range c.Ondas {
		c.Ondas[i].Oob = true
		for j := range c.Ondas[i].US {
			c.Ondas[i].US[j].Oob = true
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = s.modelos.ExecuteTemplate(w, "vivo", struct {
		Central
		Igual bool
		V     string
	}{c, igual, v})
	if err != nil && s.Log != nil {
		s.Log("web: modelo vivo: %v", err)
	}
}
