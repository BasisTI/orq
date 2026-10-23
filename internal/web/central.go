package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

const (
	eventosPorPagina = 25
	// detalheMaximo corta o detalhe de um evento na página; o diário inteiro está no orq diario.
	detalheMaximo = 600
)

// frescor é a idade de um valor em cache, como o rodapé do TUI a escreve, e se ela passou do
// prazo da fonte.
type Frescor struct {
	Idade   string
	Vencido bool
	Nunca   bool
	Erro    string
}

func (s *Servidor) frescor(fonte string, atualizadoEm *string, erro *central.Erro) Frescor {
	f := Frescor{}
	if atualizadoEm == nil {
		f.Nunca = true
	} else {
		f.Idade = central.Idade(*atualizadoEm, s.agora())
		f.Vencido = central.Vencido(fonte, atualizadoEm, s.agora())
		// "agora" ao lado de "vencido" se contradiz (o prazo do Herdr é de 15 s): dá os segundos.
		if t, err := time.Parse(time.RFC3339, *atualizadoEm); err == nil && f.Vencido && s.agora().Sub(t) < time.Minute {
			f.Idade = fmt.Sprintf("há %d s", int(s.agora().Sub(t)/time.Second))
		}
	}
	if erro != nil {
		f.Erro = erro.Codigo
	}
	return f
}

type fonteVista struct {
	Nome string
	Frescor
}

type pendLink struct {
	ID, Href, Tipo, Estado string
}

type mrVista struct {
	URL, Rotulo string
	Estado      string // vazio: o cache ainda não tem estado
	// Pipeline e Aprovacao são sempre preenchidos: "desconhecido" quando o cache não tem o dado.
	Pipeline, Aprovacao       string
	PipelineCls, AprovacaoCls string
	// Situacao é a idade de Pipeline e Aprovacao e a falha da consulta extra, no padrão das fontes.
	Situacao Frescor
	Frescor
}

type usVista struct {
	ID, Titulo, Status, StatusTaiga, TaigaURL string
	Dependencias                              []string
	Agente                                    string // estado do agente, nunca o pane
	AgenteVencido                             bool
	Taiga                                     Frescor
	MRs                                       []mrVista
	Pendencias                                []pendLink
}

type ondaVista struct {
	ID                   int64
	Href, Titulo, Ate    string
	Ordem                string
	Concluida, Encerrada bool
	Feitas               int
	US                   []usVista
}

type coordVista struct {
	Nome, Agente         string
	AgenteVencido        bool
	Abertas, Respondidas int
	Ondas                []ondaVista
}

var (
	rotuloPipeline  = map[string]string{"sucesso": "sucesso", "falhou": "falhou", "rodando": "rodando", "pendente": "pendente", "sem": "sem pipeline"}
	rotuloAprovacao = map[string]string{"aprovada": "aprovada", "pendente": "pendente", "nao_exigida": "não exigida"}
)

// rotuloSituacao escreve o pipeline ou a aprovação da MR e a classe do selo; valor ausente ou
// que esta versão não conhece é "desconhecido", dito por extenso.
func rotuloSituacao(v *string, rotulos map[string]string) (texto, cls string) {
	if v != nil {
		if t, ok := rotulos[*v]; ok {
			return t, *v
		}
	}
	return "desconhecido", "desconhecido"
}

var rotuloAte = map[string]string{"plano": "plano", "mr-pronta": "MR pronta", "merge": "merge"}

// vistaCoords converte o snapshot no que a página mostra. Do Herdr sai só o estado do agente:
// nome, pane e workspace ficam de fora.
func (s *Servidor) vistaCoords(snap central.Snapshot) []coordVista {
	out := make([]coordVista, 0, len(snap.Coordenadores))
	for _, c := range snap.Coordenadores {
		cv := coordVista{Nome: c.Nome, Ondas: []ondaVista{}}
		cv.Agente, cv.AgenteVencido = s.agente(c.Herdr)
		for _, o := range c.Ondas {
			ov := ondaVista{ID: o.ID, Href: "/onda/" + strconv.FormatInt(o.ID, 10), Titulo: o.Titulo, Ate: rotuloAte[o.AutorizacaoAte], Ordem: o.Ordem, Encerrada: o.EncerradaEm != nil}
			if ov.Ate == "" {
				ov.Ate = o.AutorizacaoAte
			}
			if enc := central.EncerramentoDe(o); enc != nil && enc.Pronta {
				ov.Concluida = true
			}
			for _, u := range o.US {
				if u.Status == "done" {
					ov.Feitas++
				}
				uv := usVista{ID: u.ID, Status: u.Status, Dependencias: u.DependeDe, Taiga: s.frescor(central.FonteTaiga, u.Taiga.AtualizadoEm, u.Taiga.Erro)}
				if u.Taiga.Titulo != nil {
					uv.Titulo = *u.Taiga.Titulo
				}
				if u.Taiga.Status != nil {
					uv.StatusTaiga = *u.Taiga.Status
				}
				if u.Taiga.URL != nil {
					uv.TaigaURL = *u.Taiga.URL
				}
				uv.Agente, uv.AgenteVencido = s.agente(u.Herdr)
				for _, m := range u.MRs {
					mv := mrVista{URL: m.URL, Rotulo: m.RefCurta(), Frescor: s.frescor(central.FonteMR, m.AtualizadoEm, m.Erro)}
					if m.Estado != nil {
						mv.Estado = *m.Estado
					}
					mv.Pipeline, mv.PipelineCls = rotuloSituacao(m.Pipeline, rotuloPipeline)
					mv.Aprovacao, mv.AprovacaoCls = rotuloSituacao(m.Aprovacao, rotuloAprovacao)
					mv.Situacao = s.frescor(central.FonteMR, m.SituacaoEm, m.SituacaoErro)
					uv.MRs = append(uv.MRs, mv)
				}
				for _, p := range u.Pendencias {
					switch p.Estado {
					case central.EstadoRespondida:
						cv.Respondidas++
					case central.EstadoAberta:
						cv.Abertas++
					default:
						continue
					}
					r := p.RefUS()
					uv.Pendencias = append(uv.Pendencias, pendLink{ID: p.ID, Href: refs.RotaPendencia(r.Projeto, r.Ref, p.Numero), Tipo: p.Tipo, Estado: p.Estado})
				}
				ov.US = append(ov.US, uv)
			}
			cv.Ondas = append(cv.Ondas, ov)
		}
		out = append(out, cv)
	}
	return out
}

// agente devolve o estado do agente e se o dado do Herdr venceu. Vazio sem pane ou sem resposta.
func (s *Servidor) agente(h *central.Herdr) (string, bool) {
	if h == nil || h.Status == nil {
		return "", false
	}
	return *h.Status, central.Vencido(central.FonteHerdr, h.AtualizadoEm, s.agora())
}

func (s *Servidor) central(w http.ResponseWriter, r *http.Request) {
	snap, err := s.St.Snapshot(r.Context(), false)
	if err != nil {
		s.falha(w, r, err, "/")
		return
	}
	var fontes []fonteVista
	for _, nome := range central.Fontes {
		if f, ok := snap.Fontes[nome]; ok {
			fontes = append(fontes, fonteVista{Nome: nome, Frescor: s.frescor(nome, f.AtualizadoEm, f.Erro)})
		}
	}
	s.pagina(w, http.StatusOK, "central", struct {
		base
		Coords []coordVista
		Fontes []fonteVista
		Gerado string
	}{base{Titulo: "Central", Usuario: usuarioDe(r), Recarregar: true, Ativa: "central", Larga: true}, s.vistaCoords(snap), fontes, s.quando(snap.GeradoEm)})
}

type eventoVista struct {
	Quando, Tipo, US, Detalhe string
	Cortado                   bool
}

func (s *Servidor) onda(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != r.PathValue("id") {
		s.erro(w, r, http.StatusNotFound, "Página não encontrada", "Endereço de onda inválido.", "/central")
		return
	}
	ctx := r.Context()
	snap, err := s.St.Snapshot(ctx, true)
	if err != nil {
		s.falha(w, r, err, "/central")
		return
	}
	var alvo *ondaVista
	var coord string
	for _, cv := range s.vistaCoords(snap) {
		for _, o := range cv.Ondas {
			if o.ID == id {
				o := o
				alvo, coord = &o, cv.Nome
			}
		}
	}
	if alvo == nil {
		s.erro(w, r, http.StatusNotFound, "Página não encontrada", "Não há onda com esse número.", "/central")
		return
	}
	pagina, _ := strconv.Atoi(r.FormValue("pagina"))
	pagina = max(pagina, 1)
	f := central.FiltroDiario{OndaID: id}
	total, err := s.St.ContarDiario(ctx, nil, f)
	if err != nil {
		s.falha(w, r, err, "/central")
		return
	}
	paginas := max((total+eventosPorPagina-1)/eventosPorPagina, 1)
	pagina = min(pagina, paginas)
	f.Limite, f.Deslocamento = eventosPorPagina, (pagina-1)*eventosPorPagina
	es, err := s.St.Diario(ctx, nil, f)
	if err != nil {
		s.falha(w, r, err, "/central")
		return
	}
	// O mais recente primeiro: é o que se quer ver no celular.
	evs := make([]eventoVista, 0, len(es))
	for i := len(es) - 1; i >= 0; i-- {
		e := es[i]
		ev := eventoVista{Quando: s.quando(e.TS), Tipo: e.Tipo}
		if e.US != nil {
			ev.US = *e.US
		}
		ev.Detalhe, ev.Cortado = textoDetalhe(e.Tipo, e.Detalhe)
		evs = append(evs, ev)
	}
	base0 := "/onda/" + strconv.FormatInt(id, 10)
	d := struct {
		base
		Onda              ondaVista
		Coordenador       string
		Eventos           []eventoVista
		Total             int
		Pagina, Paginas   int
		Anterior, Proxima string
	}{base{Titulo: "Onda " + strconv.FormatInt(id, 10), Usuario: usuarioDe(r), Recarregar: true, Ativa: "central", Larga: true}, *alvo, coord, evs, total, pagina, paginas, "", ""}
	if pagina > 1 {
		d.Anterior = base0 + "?pagina=" + strconv.Itoa(pagina-1)
	}
	if pagina < paginas {
		d.Proxima = base0 + "?pagina=" + strconv.Itoa(pagina+1)
	}
	s.pagina(w, http.StatusOK, "onda", d)
}

// camposDoDiario é a lista do que pode sair, por tipo de evento: só estes campos aparecem na
// página. Tipo fora da lista não mostra detalhe nenhum, e campo novo num tipo conhecido fica
// escondido até alguém conferir que ele é seguro e o incluir aqui. Ficam de fora, de propósito,
// pane_id, workspace_id, pane_origem e pane_liberado_de.
var camposDoDiario = map[string][]string{
	"coord.registrar":         {"projeto_padrao"},
	"projeto.tom":             {"projeto", "indice"},
	"onda.criar":              {"titulo", "autorizacao_ate", "ordem", "us", "depende_de", "movidas"},
	"onda.encerrar":           {},
	"us.status":               {"de", "para", "status", "papel", "rodada", "forcado", "motivo"},
	"mr.add":                  {"url", "projeto_path", "iid", "iac"},
	"mr.rm":                   {"url"},
	"mr.estado":               {"url", "de", "para"},
	"pend.add":                {"id", "tipo", "texto", "origem", "slug", "arquivo", "autor", "opcoes"},
	"pend.responder":          {"id", "por", "opcao", "texto", "titulo_opcao", "anterior"},
	"pend.resolver":           {"id", "resposta"},
	"pend.arquivo_alterado":   {"id", "slug", "arquivo", "hash_registrado", "hash_atual"},
	"pend.arquivo_restaurado": {"id", "slug", "arquivo"},
	"pend.avisada":            {"id", "aviso", "coordenador"},
	"pend.aviso_falhou":       {"id", "aviso", "coordenador", "motivo"},
	"limpar.removido":         {"tipo", "alvo", "motivo"},
	"limpar.preservado":       {"tipo", "alvo", "motivo"},
	"limpar.falhou":           {"tipo", "alvo", "motivo"},
	"limpar.recusada":         {"motivo"},
	"limpar.onda":             {"acao", "motivo"},
	"recursos.gravar":         {"arquivo", "repos", "panes", "aprovados"},
	"recursos.reabrir":        {"geracao", "mrs_encerradas"},
}

const detalheOmitido = "detalhe omitido"

// textoDetalhe escreve o detalhe do evento como JSON compacto, só com os campos da lista do
// tipo, com os ids de pane do Herdr (e o nome entre parênteses que os acompanha) mascarados nos
// textos, e cortado. O escape do HTML é do template.
func textoDetalhe(tipo string, raw json.RawMessage) (string, bool) {
	campos, ok := camposDoDiario[tipo]
	if !ok {
		return detalheOmitido, false
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return detalheOmitido, false
	}
	// O alvo de um recurso do tipo pane é o próprio pane: não há o que mostrar dele.
	if t, _ := m["tipo"].(string); t == "pane" {
		delete(m, "alvo")
	}
	out := map[string]any{}
	for _, k := range campos {
		if v, ok := m[k]; ok {
			out[k] = mascarar(v)
		}
	}
	if len(out) == 0 {
		return "", false
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return detalheOmitido, false
	}
	return corte(strings.TrimSpace(b.String()))
}

// idHerdr casa o id de pane (w…:p…) ou de tab (w…:t…) do Herdr, com o nome entre parênteses que
// vem junto no formato "id (nome)" e que o painel de limpeza escreve.
var idHerdr = regexp.MustCompile(`\bw[0-9A-Za-z_-]*:[pt][0-9A-Za-z_-]+(?:\s*\([^)]*\))?`)

// mascarar troca, em todo texto do valor (inclusive dentro de listas e objetos), o id de pane
// pelo marcador "[pane]".
func mascarar(v any) any {
	switch x := v.(type) {
	case string:
		return idHerdr.ReplaceAllString(x, "[pane]")
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if l := strings.ToLower(k); strings.Contains(l, "pane") || strings.Contains(l, "workspace") {
				continue
			}
			out[k] = mascarar(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = mascarar(e)
		}
		return out
	}
	return v
}

func corte(s string) (string, bool) {
	if len(s) <= detalheMaximo {
		return s, false
	}
	i := detalheMaximo
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i], true
}
