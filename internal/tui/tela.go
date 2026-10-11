// Package tui desenha a Central no terminal: a tela de um coordenador (pane no workspace
// Central) e a de todos (popup). Desenhar é puro, a partir do snapshot, para ser testado sem
// terminal; o modelo bubbletea (modelo.go) cuida do laço, do teclado e do mouse.
package tui

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
	"github.com/charmbracelet/x/ansi"
)

// URLPane é o prefixo dos links que focam um pane: o link handler do plugin casa
// ^https://orq\.local/pane/ e chama a ação focar-pane com a URL clicada.
const URLPane = "https://orq.local/pane/"

// Opcoes escolhem o que Desenhar mostra.
type Opcoes struct {
	Coord   string    // um coordenador; vazio com Todos falso mostra todos também
	Todos   bool      // um bloco por coordenador, com o total de pendências abertas
	Largura int       // colunas disponíveis
	Agora   time.Time // relógio de "há 12 min"
}

// Linha é uma linha da tela. Pane, quando não vazio, é o pane que Enter ou o clique focam.
type Linha struct {
	Texto string
	Pane  string
}

// Tela é o que Desenhar produz: as linhas e o rodapé com o estado das fontes.
type Tela struct {
	Linhas []Linha
	Rodape string
}

// Estilos desenha com o lipgloss: as cores saem em truecolor e o programa do bubbletea as
// reduz ao perfil do terminal na saída.
type Estilos struct{}

var (
	corVermelho = lipgloss.Color("#e06c75")
	corVerde    = lipgloss.Color("#98c379")
	corAmarelo  = lipgloss.Color("#e5c07b")
	corCinza    = lipgloss.Color("#7f848e")
	corApagado  = lipgloss.Color("#5c6370")
	corLaranja  = lipgloss.Color("#ff8c00")
	corRoxo     = lipgloss.Color("#c678dd")
)

func (e Estilos) novo() lipgloss.Style { return lipgloss.NewStyle() }

func (e Estilos) apagado(s string) string { return e.novo().Foreground(corApagado).Render(s) }
func (e Estilos) negrito(s string) string { return e.novo().Bold(true).Render(s) }

// Ícone do status Herdr do pane da US. Pane sem agente (unknown), pane que sumiu (ausente) e
// US sem pane ou Herdr que nunca respondeu (–) têm marcas próprias.
func (e Estilos) icone(u central.US) string {
	st := ""
	if u.Herdr != nil && u.Herdr.Status != nil {
		st = *u.Herdr.Status
	}
	switch st {
	case "working":
		return e.novo().Foreground(corAmarelo).Render("●")
	case "blocked":
		return e.novo().Foreground(corVermelho).Bold(true).Render("⚠")
	case "idle":
		return e.novo().Foreground(corCinza).Render("·")
	case "done":
		return e.novo().Foreground(corVerde).Render("✓")
	case "unknown":
		return e.apagado("○")
	case "ausente":
		return e.apagado("✗")
	default:
		return e.apagado("–")
	}
}

// mr desenha a referência da MR com o link para ela, na cor do estado: draft vermelho, pronta
// verde, aberta neutra, mesclada cinza tachado, fechada roxo itálico, sem estado apagada.
func (e Estilos) mr(m central.MR, comEstado bool) string {
	st := e.novo()
	estado := "?"
	if m.Estado != nil {
		estado = *m.Estado
	}
	switch estado {
	case "draft":
		st = st.Foreground(corVermelho)
	case "pronta":
		st = st.Foreground(corVerde)
	case "mesclada":
		st = st.Foreground(corCinza).Strikethrough(true)
	case "fechada":
		st = st.Foreground(corRoxo).Italic(true)
	case "aberta":
	default:
		st = st.Foreground(corApagado)
	}
	s := link(m.URL, st.Render(limpar(m.RefCurta())))
	if m.IaC {
		s += " " + e.novo().Foreground(corLaranja).Render("[IaC]")
	}
	if comEstado {
		s += " " + e.apagado(estado)
	}
	if m.Erro != nil {
		s += e.apagado("*")
	}
	return s
}

// link envolve texto num hyperlink OSC 8. URL vazia devolve o texto puro.
func link(u, texto string) string {
	u = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || output.UnsafeRune(r) {
			return -1
		}
		return r
	}, u)
	if u == "" {
		return texto
	}
	return "\x1b]8;;" + u + "\x1b\\" + texto + "\x1b]8;;\x1b\\"
}

func linkPane(pane, texto string) string {
	if pane == "" {
		return texto
	}
	return link(URLPane+url.PathEscape(pane), texto)
}

// limpar troca controles e formatação invisível por espaço: títulos e textos vêm de agentes e
// não podem mexer no terminal.
func limpar(s string) string {
	return strings.Map(func(r rune) rune {
		if output.UnsafeRune(r) {
			return ' '
		}
		return r
	}, s)
}

func cortar(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

func preencher(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

var rotuloStatus = map[string]string{"wait": "Wait", "plan": "Plan", "exec": "Exec", "rev": "Rev", "done": "Done"}

var rotuloAte = map[string]string{"plano": "plano", "mr-pronta": "MR pronta", "merge": "merge"}

var rotuloTipo = map[string]string{"duvida": "dúvida", "autorizacao": "autorização", "decisao": "decisão"}

func rotulo(m map[string]string, k string) string {
	if v, ok := m[k]; ok {
		return v
	}
	return limpar(k)
}

func bloqueada(u central.US) bool {
	return u.Herdr != nil && u.Herdr.Status != nil && *u.Herdr.Status == "blocked"
}

// Desenhar monta a tela a partir do snapshot.
func (e Estilos) Desenhar(s central.Snapshot, o Opcoes) Tela {
	if o.Largura <= 0 {
		o.Largura = 80
	}
	var t Tela
	coords := s.Coordenadores
	if o.Coord != "" && !o.Todos {
		coords = nil
		for _, c := range s.Coordenadores {
			if c.Nome == o.Coord {
				coords = append(coords, c)
			}
		}
		if len(coords) == 0 {
			t.Linhas = append(t.Linhas, Linha{Texto: e.apagado(cortar("coordenador "+limpar(o.Coord)+" não registrado", o.Largura))})
		}
	} else {
		cab := fmt.Sprintf("Central · %d coordenador", len(coords))
		if len(coords) != 1 {
			cab += "es"
		}
		n := len(s.PendenciasAbertas)
		pend := fmt.Sprintf("%d pendência aberta", n)
		if n != 1 {
			pend = fmt.Sprintf("%d pendências abertas", n)
		}
		if n > 0 {
			pend = e.novo().Foreground(corAmarelo).Bold(true).Render(pend)
		}
		t.Linhas = append(t.Linhas, Linha{Texto: e.negrito(cab) + " · " + pend})
	}
	larg := larguras(coords)
	for i, c := range coords {
		if o.Todos || o.Coord == "" || i > 0 {
			t.Linhas = append(t.Linhas, Linha{})
		}
		t.Linhas = append(t.Linhas, e.coordenador(c, s.PendenciasAbertas, larg, o)...)
	}
	t.Rodape = e.rodape(s, o.Agora)
	return t
}

// larg são as larguras das colunas, iguais em toda a tela para alinhar.
type larg struct{ ref, titulo, status, mr int }

func larguras(cs []central.CoordSnapshot) larg {
	var l larg
	for _, c := range cs {
		for _, o := range c.Ondas {
			for _, u := range o.US {
				l.ref = max(l.ref, ansi.StringWidth(refTexto(u)))
				l.titulo = max(l.titulo, ansi.StringWidth(tituloTexto(u))+1) // +1: a marca de erro
				l.status = max(l.status, ansi.StringWidth(statusTexto(u)))
				if len(u.MRs) == 1 {
					l.mr = max(l.mr, larguraMR(u.MRs[0]))
				}
			}
		}
	}
	return l
}

// larguraMR é a largura de Estilos.mr(m, false) sem os escapes.
func larguraMR(m central.MR) int {
	w := ansi.StringWidth(limpar(m.RefCurta()))
	if m.IaC {
		w += len(" [IaC]")
	}
	if m.Erro != nil {
		w++
	}
	return w
}

func tituloTexto(u central.US) string {
	if u.Taiga.Titulo != nil && *u.Taiga.Titulo != "" {
		return limpar(*u.Taiga.Titulo)
	}
	return u.Chave
}

func refTexto(u central.US) string { return "#" + strconv.Itoa(u.Ref) }

func statusTexto(u central.US) string {
	s := rotulo(rotuloStatus, u.Status)
	if u.Rodada != nil {
		s += " r" + strconv.Itoa(*u.Rodada)
	}
	if u.PaneID != nil {
		s += " ↗"
	}
	return s
}

func (e Estilos) coordenador(c central.CoordSnapshot, abertas []central.Pendencia, l larg, o Opcoes) []Linha {
	var ls []Linha
	pane := ""
	if c.PaneID != nil {
		pane = *c.PaneID
	}
	if len(c.Ondas) == 0 {
		ls = append(ls, Linha{Texto: e.negrito(cortar(limpar(c.Nome), o.Largura)) + e.apagado(cortar(" — sem onda aberta", o.Largura-ansi.StringWidth(c.Nome))), Pane: pane})
	}
	for _, onda := range c.Ondas {
		ls = append(ls, e.cabecalho(c, onda, o, pane))
		us := append([]central.US(nil), onda.US...)
		// A US com o pane blocked sobe; o resto fica na ordem da onda.
		sort.SliceStable(us, func(i, j int) bool { return bloqueada(us[i]) && !bloqueada(us[j]) })
		for _, u := range us {
			ls = append(ls, e.us(u, l, o)...)
		}
	}
	var minhas []central.Pendencia
	for _, p := range abertas {
		if p.Coordenador == c.Nome {
			minhas = append(minhas, p)
		}
	}
	if len(minhas) > 0 {
		ls = append(ls, Linha{Texto: e.negrito("Pendências")})
		for _, p := range minhas {
			ls = append(ls, e.pendencia(p, o))
		}
	}
	return ls
}

// cabecalho: `coordenador · projeto — título  [até: MR pronta]  n/m done · k pend`.
func (e Estilos) cabecalho(c central.CoordSnapshot, onda central.Onda, o Opcoes, pane string) Linha {
	var projetos []string
	visto := map[string]bool{}
	feitas, pend := 0, 0
	for _, u := range onda.US {
		if !visto[u.Projeto] {
			visto[u.Projeto] = true
			projetos = append(projetos, limpar(u.Projeto))
		}
		if u.Status == "done" {
			feitas++
		}
		for _, p := range u.Pendencias {
			if p.Aberta {
				pend++
			}
		}
	}
	esq := e.negrito(limpar(c.Nome))
	if len(projetos) > 0 {
		esq += " · " + strings.Join(projetos, ", ")
	}
	esq += " — "
	ate := "  " + e.apagado("[até: "+rotulo(rotuloAte, onda.AutorizacaoAte)+"]")
	dir := "  " + fmt.Sprintf("%d/%d done", feitas, len(onda.US))
	if pend > 0 {
		dir += " · " + e.novo().Foreground(corAmarelo).Render(fmt.Sprintf("%d pend", pend))
	}
	resto := o.Largura - ansi.StringWidth(esq) - ansi.StringWidth(dir)
	// Sem lugar para o título, a autorização sai antes dele.
	if resto-ansi.StringWidth(ate) >= 10 {
		dir = ate + dir
		resto -= ansi.StringWidth(ate)
	}
	titulo := cortar(limpar(onda.Titulo), max(resto, 1))
	return Linha{Texto: cortar(esq+titulo+dir, o.Largura), Pane: pane}
}

// us: ícone, #ref (link do Taiga), título truncado, status e rodada (link que foca o pane) e as
// MRs: inline quando há uma, uma por linha extra quando há mais.
func (e Estilos) us(u central.US, l larg, o Opcoes) []Linha {
	pane := ""
	if u.PaneID != nil {
		pane = *u.PaneID
	}
	urlTaiga := ""
	if u.Taiga.URL != nil {
		urlTaiga = *u.Taiga.URL
	}
	blk := bloqueada(u)
	ref := link(urlTaiga, preencher(refTexto(u), l.ref))
	stTexto := statusTexto(u)
	stEstilo := e.novo()
	if blk {
		stEstilo = stEstilo.Foreground(corVermelho).Bold(true)
	} else if u.Status == "done" {
		stEstilo = stEstilo.Foreground(corCinza)
	}
	status := linkPane(pane, stEstilo.Render(stTexto)) + strings.Repeat(" ", max(l.status-ansi.StringWidth(stTexto), 0))
	mrInline := ""
	if len(u.MRs) == 1 {
		mrInline = e.mr(u.MRs[0], false)
	}
	fixo := 2 + l.ref + 1 + 2 + l.status
	if l.mr > 0 {
		fixo += 2 + l.mr
	}
	// O título ocupa o que sobra, até o mais longo da tela: num pane largo o status fica perto.
	larguraTitulo := max(min(o.Largura-fixo, l.titulo), 1)
	titulo := ""
	if u.Taiga.Titulo != nil {
		titulo = limpar(*u.Taiga.Titulo)
	}
	// Com erro na última consulta ao Taiga, o título é o último bom e uma marca apagada no fim
	// da coluna diz que a consulta mais recente falhou.
	tw := larguraTitulo
	if u.Taiga.Erro != nil {
		tw = max(tw-1, 0)
	}
	var tituloR string
	if titulo == "" {
		tituloR = e.apagado(preencher(cortar(u.Chave, tw), tw))
	} else {
		tituloR = preencher(cortar(titulo, tw), tw)
		if blk {
			tituloR = e.negrito(tituloR)
		}
	}
	if u.Taiga.Erro != nil {
		tituloR += e.apagado("*")
	}
	texto := e.icone(u) + " " + ref + " " + tituloR + "  " + status
	if mrInline != "" {
		texto += "  " + mrInline
	}
	ls := []Linha{{Texto: cortar(texto, o.Largura), Pane: pane}}
	if len(u.MRs) > 1 {
		recuo := strings.Repeat(" ", 2+l.ref+1)
		for _, m := range u.MRs {
			ls = append(ls, Linha{Texto: cortar(recuo+e.mr(m, true), o.Largura), Pane: pane})
		}
	}
	return ls
}

// pendencia: `TG-313#P1 autorização  texto  (há 12 min) ↗`; o ↗ foca o pane que perguntou.
func (e Estilos) pendencia(p central.Pendencia, o Opcoes) Linha {
	pane := ""
	if p.PaneOrigem != nil {
		pane = *p.PaneOrigem
	}
	esq := " " + e.novo().Foreground(corAmarelo).Render(limpar(p.ID)) + " " + rotulo(rotuloTipo, p.Tipo) + "  "
	dir := "  " + e.apagado("("+desde(p.AbertaEm, o.Agora)+")")
	if pane != "" {
		dir += " " + linkPane(pane, "↗")
	}
	texto := cortar(limpar(p.Texto), max(o.Largura-ansi.StringWidth(esq)-ansi.StringWidth(dir), 1))
	return Linha{Texto: cortar(esq+texto+dir, o.Largura), Pane: pane}
}

// desde escreve há quanto tempo foi ts (RFC 3339): agora, há 12 min, há 3 h, há 2 d.
func desde(ts string, agora time.Time) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "?"
	}
	d := agora.Sub(t)
	switch {
	case d < time.Minute:
		return "agora"
	case d < time.Hour:
		return fmt.Sprintf("há %d min", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("há %d h", int(d/time.Hour))
	default:
		return fmt.Sprintf("há %d d", int(d/(24*time.Hour)))
	}
}

// rodape mostra, discreto, a idade de cada fonte e o erro que ela tiver; os valores da tela
// continuam os últimos bons.
func (e Estilos) rodape(s central.Snapshot, agora time.Time) string {
	var partes []string
	marca := false
	for _, f := range central.Fontes {
		fo, ok := s.Fontes[f]
		if !ok {
			continue
		}
		p := f + " "
		switch {
		case fo.AtualizadoEm != nil:
			p += desde(*fo.AtualizadoEm, agora)
		default:
			p += "nunca"
		}
		if fo.Erro != nil {
			p += " ✗ " + limpar(fo.Erro.Codigo)
		}
		partes = append(partes, p)
	}
	for _, c := range s.Coordenadores {
		for _, o := range c.Ondas {
			for _, u := range o.US {
				if u.Taiga.Erro != nil {
					marca = true
				}
				for _, m := range u.MRs {
					if m.Erro != nil {
						marca = true
					}
				}
			}
		}
	}
	if marca {
		partes = append(partes, "* última consulta falhou")
	}
	return e.apagado(strings.Join(partes, " · "))
}
