package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// PendItem é uma pendência como a Central a desenha: um <details> fechado, só com o resumo. O
// corpo (contexto, opções, formulário) é buscado em CorpoHref na primeira abertura. O mesmo item
// serve à US e à aba Pendências (spec §2.2 e §5.3).
type PendItem struct {
	Anchor, ID, Href, CorpoHref string
	Tipo, Icone, Titulo, Idade  string
	Estado                      string
	Respondida, SemDetalhe      bool
	SemCoordenador              bool
	// Nova marca o item que acabou de ser respondido: ele pisca uma vez.
	Nova bool
}

var iconesTipo = map[string]string{"decisao": "decisao", "autorizacao": "autoriza", "duvida": "duvida"}

// pendItemDe monta o item do resumo de uma pendência do snapshot. Não lê relógio: recebe agora.
func pendItemDe(p central.Pendencia, agora time.Time) PendItem {
	u := p.RefUS()
	href := refs.RotaPendencia(u.Projeto, u.Ref, p.Numero)
	return PendItem{
		Anchor: "pend-" + u.Projeto + "-" + strconv.Itoa(u.Ref) + "-" + strconv.Itoa(p.Numero),
		ID:     p.ID, Href: href, CorpoHref: href + "/corpo",
		Tipo: p.Tipo, Icone: iconesTipo[p.Tipo], Titulo: p.Texto, Idade: central.Idade(p.AbertaEm, agora),
		Estado: p.Estado, Respondida: p.Estado == central.EstadoRespondida,
		SemDetalhe: !p.TemDetalhe, SemCoordenador: p.SemCoordenador,
	}
}

// FiltroPend são os três filtros da aba Pendências, como vieram da URL ou, depois de
// opcoesCascata, já normalizados.
type FiltroPend struct {
	Projeto, Coord, Onda string
}

// Query é a query canônica do filtro (só o que não está em "todos", sempre na mesma ordem).
func (f FiltroPend) Query() string {
	v := url.Values{}
	if f.Projeto != "" {
		v.Set("projeto", f.Projeto)
	}
	if f.Coord != "" {
		v.Set("coord", f.Coord)
	}
	if f.Onda != "" {
		v.Set("onda", f.Onda)
	}
	return v.Encode()
}

// URL é o endereço canônico da aba com o filtro.
func (f FiltroPend) URL() string {
	if q := f.Query(); q != "" {
		return "/pendencias?" + q
	}
	return "/pendencias"
}

// OpcaoFiltro é uma <option> de um dos selects da cascata.
type OpcaoFiltro struct {
	Valor, Rotulo string
	Sel           bool
}

// Cascata são as opções dos três selects e o filtro normalizado (spec §7).
type Cascata struct {
	Projetos, Coords, Ondas []OpcaoFiltro
	Filtro                  FiltroPend
	// Normalizado: o filtro pedido tinha um valor impossível, que foi descartado com os de baixo.
	Normalizado bool
}

type ondaAberta struct {
	id       int64
	titulo   string
	coord    string
	projetos map[string]bool
}

func ondasAbertasDe(snap central.Snapshot) []ondaAberta {
	var out []ondaAberta
	for _, co := range snap.Coordenadores {
		for _, o := range co.Ondas {
			if o.EncerradaEm != nil {
				continue
			}
			oa := ondaAberta{id: o.ID, titulo: o.Titulo, coord: co.Nome, projetos: map[string]bool{}}
			for _, u := range o.US {
				oa.projetos[u.Projeto] = true
			}
			out = append(out, oa)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// cortar abrevia o texto a n runas, com reticências.
func cortar(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

// opcoesCascata calcula as opções dos filtros projeto → coordenador → onda a partir do
// snapshot (as ondas abertas e o projeto das US), não das pendências: a onda sem pendência
// continua escolhível. Percorre de cima para baixo e, no primeiro valor que não existe naquela
// combinação, descarta esse e os de baixo. Nenhuma combinação impossível sai daqui.
func opcoesCascata(snap central.Snapshot, f FiltroPend) Cascata {
	ondas := ondasAbertasDe(snap)
	c := Cascata{}

	projetos := map[string]bool{}
	for _, o := range ondas {
		for p := range o.projetos {
			projetos[p] = true
		}
	}
	if f.Projeto != "" && !projetos[f.Projeto] {
		f.Projeto, f.Coord, f.Onda = "", "", ""
		c.Normalizado = true
	}

	coords := map[string]bool{}
	for _, o := range ondas {
		if f.Projeto == "" || o.projetos[f.Projeto] {
			coords[o.coord] = true
		}
	}
	if f.Coord != "" && !coords[f.Coord] {
		f.Coord, f.Onda = "", ""
		c.Normalizado = true
	}

	var daCombinacao []ondaAberta
	for _, o := range ondas {
		if (f.Projeto == "" || o.projetos[f.Projeto]) && (f.Coord == "" || o.coord == f.Coord) {
			daCombinacao = append(daCombinacao, o)
		}
	}
	if f.Onda != "" {
		ok := false
		for _, o := range daCombinacao {
			if strconv.FormatInt(o.id, 10) == f.Onda {
				ok = true
			}
		}
		if !ok {
			f.Onda = ""
			c.Normalizado = true
		}
	}

	for _, s := range chavesOrdenadas(projetos) {
		c.Projetos = append(c.Projetos, OpcaoFiltro{Valor: s, Rotulo: s, Sel: s == f.Projeto})
	}
	for _, s := range chavesOrdenadas(coords) {
		c.Coords = append(c.Coords, OpcaoFiltro{Valor: s, Rotulo: s, Sel: s == f.Coord})
	}
	for _, o := range daCombinacao {
		id := strconv.FormatInt(o.id, 10)
		rot := "onda " + id
		if o.titulo != "" {
			rot = "#" + id + " " + cortar(o.titulo, 44)
		}
		c.Ondas = append(c.Ondas, OpcaoFiltro{Valor: id, Rotulo: rot, Sel: id == f.Onda})
	}
	c.Filtro = f
	return c
}

func chavesOrdenadas(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Pendencias é a aba Pendências inteira, pronta para o modelo.
type Pendencias struct {
	Abas []Aba
	Cascata
	Esperam, Respondidas []PendItem
	// Hash identifica a lista exibida (filtro, opções e itens com o estado): o /vivo compara o que
	// a página tem com o do servidor e, se mudou, avisa em vez de mexer na lista.
	Hash string
	// Fontes e FontesOob alimentam a coluna lateral ("Idade dos dados").
	Fontes    []FonteVista
	FontesOob bool
	// Oob e Igual só valem na resposta do /vivo.
	Igual bool
}

// NEsp e NResp são as contagens dos dois grupos.
func (p Pendencias) NEsp() int  { return len(p.Esperam) }
func (p Pendencias) NResp() int { return len(p.Respondidas) }

// URL é o endereço canônico da aba com o filtro normalizado.
func (p Pendencias) URL() string { return p.Filtro.URL() }

// vistaPendencias monta a aba Pendências do snapshot. Função pura: não abre banco nem lê relógio.
func vistaPendencias(snap central.Snapshot, agora time.Time, f FiltroPend) Pendencias {
	p := Pendencias{Cascata: opcoesCascata(snap, f)}
	p.Abas = abasDe(snap, abaTabPendencias)
	f = p.Filtro
	var resp []central.Pendencia
	for _, pe := range snap.PendenciasAbertas {
		if pe.Estado != central.EstadoAberta && pe.Estado != central.EstadoRespondida {
			continue
		}
		if f.Projeto != "" && pe.RefUS().Projeto != f.Projeto || f.Coord != "" && pe.Coordenador != f.Coord || f.Onda != "" && strconv.FormatInt(pe.OndaID, 10) != f.Onda {
			continue
		}
		if pe.Estado == central.EstadoRespondida {
			resp = append(resp, pe)
			continue
		}
		p.Esperam = append(p.Esperam, pendItemDe(pe, agora))
	}
	// A respondida mais recente em cima: é onde a resposta nova entra (afterbegin) sem a lista
	// mudar de ordem embaixo da mão de quem lê.
	sort.SliceStable(resp, func(i, j int) bool { return derefStr(resp[i].RespondidaEm) > derefStr(resp[j].RespondidaEm) })
	for _, pe := range resp {
		p.Respondidas = append(p.Respondidas, pendItemDe(pe, agora))
	}
	p.Fontes = fontesDe(snap, agora)
	p.Hash = hashPendencias(p)
	return p
}

// hashPendencias é o começo do SHA-256 do filtro, das opções, das abas e dos itens com o estado,
// sem depender da ordem.
func hashPendencias(p Pendencias) string {
	h := sha256.New()
	fmt.Fprintf(h, "f%s\n", p.Filtro.Query())
	for _, grupo := range [][]OpcaoFiltro{p.Projetos, p.Coords, p.Ondas} {
		for _, o := range grupo {
			fmt.Fprintf(h, "%s,", o.Valor)
		}
		h.Write([]byte{'\n'})
	}
	var itens []string
	for _, it := range p.Esperam {
		itens = append(itens, it.Anchor+":a")
	}
	for _, it := range p.Respondidas {
		itens = append(itens, it.Anchor+":r")
	}
	sort.Strings(itens)
	for _, it := range itens {
		fmt.Fprintf(h, "%s\n", it)
	}
	for _, a := range p.Abas {
		fmt.Fprintf(h, "a%s\n", a.ID)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// Vals é o hx-vals estático do #vivo da aba Pendências: o hash da lista e as abas que a página
// tem. "g" diz que a página tem os contadores dos grupos.
func (p Pendencias) Vals() string {
	var abas []string
	for _, a := range p.Abas {
		abas = append(abas, a.ID)
	}
	b, _ := json.Marshal(map[string]string{"v": p.Hash, "a": strings.Join(abas, ","), "g": "1"})
	return string(b)
}

// fontesDe é a lista "Idade dos dados".
func fontesDe(snap central.Snapshot, agora time.Time) []FonteVista {
	var out []FonteVista
	for _, nome := range central.Fontes {
		if fs, ok := snap.Fontes[nome]; ok {
			fr := frescorEm(agora, nome, fs.AtualizadoEm, fs.Erro)
			out = append(out, FonteVista{Nome: nome, Idade: fr.Idade, Vencido: fr.Vencido, Nunca: fr.Nunca, Erro: fr.Erro})
		}
	}
	return out
}
