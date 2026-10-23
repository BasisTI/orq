package web

import (
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// ehFragmento diz se a requisição pede só o fragmento (htmx). A restauração do histórico (botão
// Voltar) também chega com HX-Request, mas o htmx troca o <body> pela resposta: ali o servidor
// devolve a página inteira (spec §2.2, B3). A meta do htmx já tira o HX-Request desse pedido
// (historyRestoreAsHxRequest:false); esta é a segunda defesa, e as duas são obrigatórias.
func ehFragmento(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true"
}

// erroFragmento responde um 4xx/5xx de fragmento. Com alvo, os cabeçalhos HX-Retarget e HX-Reswap
// mandam a mensagem para o espaço reservado do formulário, que fica com o texto digitado (spec
// §5.3, B2): o responseHandling da meta troca o 4xx no alvo do pedido, e sem isso o erro ocuparia
// o lugar do item. Sem alvo (o /corpo), os dois cabeçalhos são omitidos: o pedido já declara o
// próprio alvo, e o erro entra nele sem apagar o <summary>.
func erroFragmento(w http.ResponseWriter, status int, alvo, corpo string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	if alvo != "" {
		h.Set("HX-Retarget", alvo)
		h.Set("HX-Reswap", "innerHTML")
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, corpo)
}

// erroHX é o erro de qualquer rota quando o pedido é de fragmento: o POST da resposta vai para o
// espaço reservado do formulário, o filtro da aba Pendências para o aviso da página, e o /corpo
// (e o /vivo, que não troca nada) ficam sem cabeçalho de retarget.
func (s *Servidor) erroHX(w http.ResponseWriter, r *http.Request, status int, msg string) {
	corpo := "<p>" + html.EscapeString(msg) + "</p>"
	switch {
	case r.Method == http.MethodPost:
		erroFragmento(w, status, "find .erro", corpo)
	case r.URL.Path == "/pendencias":
		erroFragmento(w, status, "#pend-aviso", corpo)
	default:
		corpo = `<p class="aviso erro" role="alert">` + html.EscapeString(msg)
		if ref, ok := pendDe(r); ok {
			corpo += ` <a href="` + html.EscapeString(refs.RotaPendencia(ref.US.Projeto, ref.US.Ref, ref.Numero)) + `">Abrir a pendência numa página</a>`
		}
		corpo += `</p>`
		erroFragmento(w, status, "", corpo)
	}
}

// pendenciasPagina é o que o modelo "pendencias" recebe.
type pendenciasPagina struct {
	base
	Pendencias
	// Abas desfaz a ambiguidade entre base.Abas e Pendencias.Abas (a mesma faixa).
	Abas []Aba
}

// pendencias atende /pendencias: a aba Pendências, com o filtro em cascata na URL. Com HX-Request
// devolve só o #pend-conteudo; com HX-History-Restore-Request, a página inteira.
func (s *Servidor) pendencias(w http.ResponseWriter, r *http.Request) {
	snap, err := s.fonte().Snapshot(r.Context(), false)
	if err != nil {
		s.falha(w, r, err, "/")
		return
	}
	v := vistaPendencias(snap, s.agora(), filtroDe(r.URL.Query()))
	w.Header().Add("Vary", "HX-Request, HX-History-Restore-Request")
	if !ehFragmento(r) {
		// Valor impossível na URL (onda encerrada, link velho, endereço à mão): a lista abre sem ele.
		if v.Normalizado {
			http.Redirect(w, r, v.URL(), http.StatusSeeOther)
			return
		}
		s.pagina(w, http.StatusOK, "pendencias", pendenciasPagina{
			base:       base{Titulo: "Pendências", Usuario: usuarioDe(r), Cheia: true},
			Pendencias: v,
			Abas:       v.Abas,
		})
		return
	}
	// O botão Voltar depende de cada filtro ser uma entrada do histórico: o endereço canônico
	// entra (push). Se o servidor normalizou, a entrada atual é corrigida no lugar (replace).
	if v.Normalizado {
		w.Header().Set("HX-Replace-Url", v.URL())
	} else {
		w.Header().Set("HX-Push-Url", v.URL())
	}
	s.pagina(w, http.StatusOK, "pend-conteudo", v)
}

// vivoPendencias é o /vivo da aba Pendências (spec §6): só as contagens das abas, a idade dos
// dados e, se a lista é a que a página tem, os contadores dos grupos. Se a lista mudou (pendência
// nova, resolvida, onda nova), nada é inserido nem movido: a resposta traz o aviso.
func (s *Servidor) vivoPendencias(w http.ResponseWriter, snap central.Snapshot, q url.Values, aba AbaAtual) {
	v := vistaPendencias(snap, s.agora(), filtroDe(aba.Query))
	abas := conjunto(q.Get("a"))
	var as []Aba
	for _, a := range v.Abas {
		if abas[a.ID] {
			a.Oob = true
			as = append(as, a)
		}
	}
	v.Abas, v.FontesOob = as, true
	v.Igual = q.Get("v") == v.Hash
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.modelos.ExecuteTemplate(w, "vivo-pend", struct {
		Pendencias
		Grupos bool
	}{v, q.Get("g") != ""}); err != nil && s.Log != nil {
		s.Log("web: modelo vivo-pend: %v", err)
	}
}

// RespostaAtual é a resposta já dada, como o corpo da pendência respondida a mostra.
type RespostaAtual struct {
	Opcao      int
	Titulo     string
	Por, Idade string
	Comentario string
}

// corpoPend é o que os modelos "pend-corpo" e "form-resp" recebem.
type corpoPend struct {
	P                           central.Pendencia
	Detalhe                     template.HTML
	SemDetalhe, ArquivoAlterado bool
	Href, CorpoHref, Acao       string
	LimiteTexto                 int
	// HX: o formulário envia por hx-post (no corpo do accordion); na página /p/… é um formulário comum.
	HX bool
	// Trocar: o formulário abre com a resposta atual indicada.
	Trocar      bool
	AtualOpcao  int
	TextoAtual  string
	Resposta    RespostaAtual
	ResolvidaEm string
}

func (s *Servidor) dadosCorpo(p central.Pendencia, ref refs.Pend, trocar bool) corpoPend {
	href := refs.RotaPendencia(ref.US.Projeto, ref.US.Ref, ref.Numero)
	d := corpoPend{P: p, SemDetalhe: !p.TemDetalhe, ArquivoAlterado: p.ArquivoAlterado, Href: href, CorpoHref: href + "/corpo", Acao: href + "/responder", LimiteTexto: LimiteTexto, HX: true,
		Trocar: trocar && p.Estado == central.EstadoRespondida}
	if p.Detalhe != nil {
		d.Detalhe = RenderizarMarkdown(*p.Detalhe)
	}
	if p.ResolvidaEm != nil {
		d.ResolvidaEm = s.quando(*p.ResolvidaEm)
	}
	if p.Estado == central.EstadoRespondida {
		r := RespostaAtual{Por: derefStr(p.RespondidaPor), Comentario: derefStr(p.Resposta)}
		if p.RespondidaEm != nil {
			r.Idade = central.Idade(*p.RespondidaEm, s.agora())
		}
		if p.RespostaOpcao != nil {
			r.Opcao = *p.RespostaOpcao
			for _, o := range p.Opcoes {
				if o.ID == r.Opcao {
					r.Titulo = o.Titulo
				}
			}
		}
		d.Resposta = r
		if d.Trocar {
			d.AtualOpcao, d.TextoAtual = r.Opcao, r.Comentario
		}
	}
	return d
}

// corpo atende /p/…/corpo: o fragmento do accordion da pendência (contexto, opções, formulário),
// carregado na primeira abertura. Sem HX-Request vai para a página da pendência.
func (s *Servidor) corpo(w http.ResponseWriter, r *http.Request) {
	ref, ok := pendDe(r)
	if !ok {
		s.erro(w, r, http.StatusNotFound, "Página não encontrada", "Endereço de pendência inválido.", "/")
		return
	}
	if !ehFragmento(r) {
		http.Redirect(w, r, refs.RotaPendencia(ref.US.Projeto, ref.US.Ref, ref.Numero), http.StatusSeeOther)
		return
	}
	p, err := s.fonte().VerPendencia(r.Context(), nil, ref)
	if err != nil {
		s.falha(w, r, err, "/")
		return
	}
	s.pagina(w, http.StatusOK, "pend-corpo", s.dadosCorpo(p, ref, r.URL.Query().Get("trocar") == "1"))
}

// respostaDados é o que o modelo "resposta" recebe: o item respondido e as contagens que mudaram
// (spec §5.3).
type respostaDados struct {
	Item PendItem
	// Principal: o item é a resposta principal (o outerHTML do closest .pend o troca no lugar);
	// senão a resposta principal é vazia, o item sai de cima e entra no grupo de baixo (Mover).
	Principal bool
	Mover     bool
	Pend      *Pendencias
	AbaPend   *Aba
	Situacao  *SituacaoVista
	Onda      *OndaVista
	US        *USVista
	Aviso     string
}

// respostaFragmento responde o POST com HX-Request: o item respondido e as contagens, escolhidas
// pela aba em que a pessoa está (HX-Current-URL). Trocar uma resposta que já existia não muda as
// contagens: ela já tinha saído de "esperam você".
func (s *Servidor) respostaFragmento(w http.ResponseWriter, r *http.Request, p central.Pendencia) {
	agora := s.agora()
	trocou := p.RespostaVersao > 1
	d := respostaDados{Item: pendItemDe(p, agora), Principal: true}
	d.Item.Nova = true
	d.Aviso = "Resposta enviada: " + p.ID + ". O coordenador é avisado."
	if trocou {
		d.Aviso = "Resposta trocada: " + p.ID + ". O coordenador é avisado."
	}
	aba := abaDe(r.Header.Get("HX-Current-URL"))
	if snap, err := s.fonte().Snapshot(r.Context(), false); err == nil && !trocou {
		switch aba.Tipo {
		case abaDeTodos, abaDeProjeto:
			c := vistaCentral(snap, agora, Filtro{Projeto: aba.Projeto})
			d.AbaPend = abaPendencias(c.Abas)
			c.Situacao.Oob = true
			d.Situacao = &c.Situacao
			for _, o := range c.Ondas {
				if o.ID != p.OndaID {
					continue
				}
				o.Oob = true
				d.Onda = &o
				anchor := "us-" + p.RefUS().Projeto + "-" + strconv.Itoa(p.RefUS().Ref)
				for _, u := range o.US {
					if u.Anchor == anchor {
						u.Oob = true
						d.US = &u
					}
				}
			}
		case abaDePend:
			v := vistaPendencias(snap, agora, filtroDe(aba.Query))
			d.AbaPend = abaPendencias(v.Abas)
			d.Principal, d.Mover, d.Pend = false, true, &v
		}
	}
	if d.AbaPend != nil {
		d.AbaPend.Oob = true
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.modelos.ExecuteTemplate(w, "resposta", d); err != nil && s.Log != nil {
		s.Log("web: modelo resposta: %v", err)
	}
}

// abaPendencias devolve a cópia da aba Pendências da faixa.
func abaPendencias(abas []Aba) *Aba {
	for _, a := range abas {
		if a.ID == "aba-n-pendencias" {
			return &a
		}
	}
	return nil
}
