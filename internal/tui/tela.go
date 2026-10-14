// Package tui desenha a Central no terminal: a tela de um coordenador (pane no workspace
// Central) e a de todos (popup). Desenhar é puro, a partir do snapshot, para ser testado sem
// terminal; o modelo bubbletea (modelo.go) cuida do laço, do teclado e do mouse.
package tui

import (
	"fmt"
	"image/color"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
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
	// Selecao é o Item da linha selecionada: as linhas físicas dele saem com o fundo de seleção.
	Selecao string
}

// Linha é uma linha física da tela. Pane, quando não vazio, é o pane que Enter ou o clique
// focam; Item identifica a US ou a pendência a que a linha pertence (as várias linhas de uma
// US com MRs têm o mesmo Item). Bordas, regras e faixas não têm nem um nem outro.
type Linha struct {
	Texto string
	Pane  string
	Item  string
}

// Tela é o que Desenhar produz: as linhas, o rodapé com o estado das fontes e a legenda dos
// ícones do agente, um trecho por ícone (o modelo a encurta quando não cabe).
type Tela struct {
	Linhas  []Linha
	Rodape  string
	Legenda []string
}

// Estilos desenha com o lipgloss: as cores saem em truecolor e o programa do bubbletea as
// reduz ao perfil do terminal na saída. Claro escolhe os tons para terminal de fundo claro; o
// padrão, enquanto o terminal não informa a cor de fundo, é o escuro.
type Estilos struct {
	Claro bool
}

// paleta são as cores de um tema: as do texto e os fundos leves (faixa do coordenador, linha
// alternada, seleção e US blocked).
type paleta struct {
	vermelho, verde, amarelo, azul, cinza, apagado, laranja, roxo color.Color
	faixa, zebra, selecao, bloqueada, borda                       color.Color
}

var (
	escura = paleta{
		vermelho: lipgloss.Color("#e06c75"), verde: lipgloss.Color("#98c379"), amarelo: lipgloss.Color("#e5c07b"),
		azul: lipgloss.Color("#61afef"), cinza: lipgloss.Color("#7f848e"), apagado: lipgloss.Color("#5c6370"),
		laranja: lipgloss.Color("#ff8c00"), roxo: lipgloss.Color("#c678dd"),
		faixa: lipgloss.Color("#323844"), zebra: lipgloss.Color("#272b33"), selecao: lipgloss.Color("#3e4a5e"),
		bloqueada: lipgloss.Color("#4a2a30"), borda: lipgloss.Color("#4b5263"),
	}
	clara = paleta{
		vermelho: lipgloss.Color("#e45649"), verde: lipgloss.Color("#50a14f"), amarelo: lipgloss.Color("#b07d00"),
		azul: lipgloss.Color("#4078f2"), cinza: lipgloss.Color("#696c77"), apagado: lipgloss.Color("#a0a1a7"),
		laranja: lipgloss.Color("#d75f00"), roxo: lipgloss.Color("#a626a4"),
		faixa: lipgloss.Color("#e3e7ee"), zebra: lipgloss.Color("#f2f3f5"), selecao: lipgloss.Color("#cfd8e6"),
		bloqueada: lipgloss.Color("#fbe1e3"), borda: lipgloss.Color("#c5c8cf"),
	}
)

func (e Estilos) p() paleta {
	if e.Claro {
		return clara
	}
	return escura
}

func (e Estilos) novo() lipgloss.Style { return lipgloss.NewStyle() }

func (e Estilos) cor(c color.Color, s string) string { return e.novo().Foreground(c).Render(s) }
func (e Estilos) apagado(s string) string            { return e.cor(e.p().apagado, s) }
func (e Estilos) negrito(s string) string            { return e.novo().Bold(true).Render(s) }

// Ícones do status Herdr do pane da US, na ordem da legenda do rodapé. Pane sem agente
// (unknown), pane que sumiu (ausente) e US sem pane ou Herdr que nunca respondeu (–) têm marcas
// próprias.
var icones = []struct{ status, icone, legenda string }{
	{"working", "●", "trabalhando"},
	{"blocked", "⚠", "bloqueado"},
	{"idle", "·", "ocioso"},
	{"done", "✓", "concluiu"},
	{"unknown", "○", "desconhecido"},
	{"ausente", "✗", "pane fechado"},
	{"", "–", "sem pane"},
}

func (e Estilos) iconeDe(st string) string {
	p := e.p()
	switch st {
	case "working":
		return e.cor(p.amarelo, "●")
	case "blocked":
		return e.novo().Foreground(p.vermelho).Bold(true).Render("⚠")
	case "idle":
		return e.cor(p.cinza, "·")
	case "done":
		return e.cor(p.verde, "✓")
	case "unknown":
		return e.apagado("○")
	case "ausente":
		return e.apagado("✗")
	default:
		return e.apagado("–")
	}
}

func (e Estilos) icone(u central.US) string {
	st := ""
	if u.Herdr != nil && u.Herdr.Status != nil {
		st = *u.Herdr.Status
	}
	return e.iconeDe(st)
}

// mr desenha a referência da MR com o link para ela, na cor do estado: draft vermelho, pronta
// verde, aberta neutra, mesclada cinza tachado, fechada roxo itálico, sem estado apagada.
func (e Estilos) mr(m central.MR) string {
	p := e.p()
	st := e.novo()
	estado := "?"
	if m.Estado != nil {
		estado = *m.Estado
	}
	switch estado {
	case "draft":
		st = st.Foreground(p.vermelho)
	case "pronta":
		st = st.Foreground(p.verde)
	case "mesclada":
		st = st.Foreground(p.cinza).Strikethrough(true)
	case "fechada":
		st = st.Foreground(p.roxo).Italic(true)
	case "aberta":
	default:
		st = st.Foreground(p.apagado)
	}
	s := link(m.URL, st.Render(limpar(m.RefCurta())))
	if m.IaC {
		s += " " + e.cor(p.laranja, "[IaC]")
	}
	if m.Erro != nil {
		s += e.apagado("*")
	}
	return s
}

// larguraMR é a largura de Estilos.mr sem os escapes.
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

// pintar põe o fundo (e o esmaecido) de uma linha sob um texto já estilizado: cada trecho do
// lipgloss termina num reset, que apagaria o fundo no resto da célula, então ele é reaplicado
// depois de cada reset.
func pintar(s, sgr string) string {
	if sgr == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\x1b[m", "\x1b[m"+sgr)
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+sgr)
	return sgr + s
}

// fundo é o SGR do fundo c, esmaecido ou não; vazio sem nenhum dos dois.
func fundo(c color.Color, esmaecer bool) string {
	var st ansi.Style
	if esmaecer {
		st = st.Faint()
	}
	if c != nil {
		st = st.BackgroundColor(c)
	}
	if len(st) == 0 {
		return ""
	}
	return st.String()
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

// corStatus: Wait cinza, Plan roxo, Exec amarelo, Rev azul, Done verde.
func (e Estilos) corStatus(st string) color.Color {
	p := e.p()
	switch st {
	case "plan":
		return p.roxo
	case "exec":
		return p.amarelo
	case "rev":
		return p.azul
	case "done":
		return p.verde
	default:
		return p.cinza
	}
}

// corTipo: dúvida azul, autorização amarelo, decisão roxo.
func (e Estilos) corTipo(tipo string) color.Color {
	p := e.p()
	switch tipo {
	case "duvida":
		return p.azul
	case "autorizacao":
		return p.amarelo
	case "decisao":
		return p.roxo
	default:
		return p.cinza
	}
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
			pend = e.novo().Foreground(e.p().amarelo).Bold(true).Render(pend)
		}
		t.Linhas = append(t.Linhas, Linha{Texto: cortar(e.negrito(cab)+" · "+pend, o.Largura)})
	}
	nomes := map[string]bool{}
	for _, c := range coords {
		nomes[c.Nome] = true
	}
	var abertas []central.Pendencia
	for _, p := range s.PendenciasAbertas {
		if nomes[p.Coordenador] {
			abertas = append(abertas, p)
		}
	}
	cu := colunasUS(coords, o.Largura)
	cp := colunasPend(abertas, o.Largura, o.Agora)
	// A faixa vai de ponta a ponta do bloco: a largura da tabela mais larga, não a do terminal,
	// para os selos não ficarem longe do nome num pane muito largo.
	bloco := molduraTabela + cu.ag + cu.ref + cu.titulo + cu.status + cu.mr
	if len(abertas) > 0 {
		bloco = max(bloco, molduraTabela+cp.id+cp.tipo+cp.texto+cp.idade+cp.ir)
	}
	for i, c := range coords {
		if o.Todos || o.Coord == "" || i > 0 {
			t.Linhas = append(t.Linhas, Linha{})
		}
		t.Linhas = append(t.Linhas, e.coordenador(c, abertas, cu, cp, bloco, o)...)
	}
	t.Rodape = e.rodape(s, o.Agora)
	for _, ic := range icones {
		t.Legenda = append(t.Legenda, e.iconeDe(ic.status)+" "+e.apagado(ic.legenda))
	}
	return t
}

// Larguras do conteúdo das colunas (sem o respiro de um espaço de cada lado), iguais em toda
// a tela para que as tabelas dos coordenadores se alinhem.
type colsUS struct{ ag, ref, titulo, status, mr int }

type colsPend struct{ id, tipo, texto, idade, ir int }

// Cabeçalhos das tabelas.
var (
	cabUS   = []string{"", "US", "título", "status", "MRs"}
	cabPend = []string{"id", "tipo", "texto", "idade", ""}
)

// Cada tabela de 5 colunas gasta 16 colunas com bordas (6) e respiros (2 por coluna).
const molduraTabela = 6 + 2*5

// Abaixo disto o título deixa de encolher sozinho e as MRs passam a ceder (`!390…`).
const (
	tituloConfortavel = 16
	mrMinima          = 5
	textoMaximo       = 96
)

func colunasUS(cs []central.CoordSnapshot, largura int) colsUS {
	l := colsUS{ag: 1, ref: len(cabUS[1]), titulo: len(cabUS[2]), status: len(cabUS[3]), mr: len(cabUS[4])}
	for _, c := range cs {
		for _, o := range c.Ondas {
			for _, u := range o.US {
				l.ref = max(l.ref, ansi.StringWidth(refTexto(u)))
				w := ansi.StringWidth(tituloTexto(u))
				if u.Taiga.Erro != nil {
					w++ // a marca de erro
				}
				l.titulo = max(l.titulo, w)
				l.status = max(l.status, ansi.StringWidth(statusTexto(u)))
				for _, m := range u.MRs {
					l.mr = max(l.mr, larguraMR(m))
				}
			}
		}
	}
	// O título ocupa o que sobra, até o mais longo da tela: num pane largo as colunas não
	// esticam além do conteúdo. Num estreito ele encolhe primeiro; depois as MRs.
	resto := largura - molduraTabela - l.ag - l.ref - l.status
	if resto-l.mr < tituloConfortavel {
		l.mr = max(min(l.mr, resto-tituloConfortavel), min(l.mr, mrMinima))
	}
	l.titulo = max(min(l.titulo, resto-l.mr), 1)
	return l
}

func colunasPend(ps []central.Pendencia, largura int, agora time.Time) colsPend {
	l := colsPend{id: len(cabPend[0]), tipo: len(cabPend[1]), texto: len(cabPend[2]), idade: len(cabPend[3]), ir: 1}
	for _, p := range ps {
		l.id = max(l.id, ansi.StringWidth(limpar(p.ID)))
		l.tipo = max(l.tipo, ansi.StringWidth(rotulo(rotuloTipo, p.Tipo)))
		l.texto = max(l.texto, ansi.StringWidth(limpar(p.Texto)))
		l.idade = max(l.idade, ansi.StringWidth(desde(p.AbertaEm, agora)))
	}
	// O texto tem teto: mais longo, quebra em 2 linhas em vez de esticar a tabela.
	l.texto = max(min(l.texto, textoMaximo, largura-molduraTabela-l.id-l.tipo-l.idade-l.ir), 1)
	return l
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

func (e Estilos) coordenador(c central.CoordSnapshot, abertas []central.Pendencia, cu colsUS, cp colsPend, bloco int, o Opcoes) []Linha {
	var ls []Linha
	pane := ""
	if c.PaneID != nil {
		pane = *c.PaneID
	}
	nome := linkPane(pane, e.negrito(limpar(c.Nome)))
	if len(c.Ondas) == 0 {
		ls = append(ls, Linha{Texto: cortar(nome+e.apagado(" — sem onda aberta"), o.Largura)})
	}
	for _, onda := range c.Ondas {
		ls = append(ls, e.faixa(c, onda, nome, bloco, o)...)
		us := append([]central.US(nil), onda.US...)
		// A US com o pane blocked sobe; o resto fica na ordem da onda.
		sort.SliceStable(us, func(i, j int) bool { return bloqueada(us[i]) && !bloqueada(us[j]) })
		if len(us) > 0 {
			ls = append(ls, e.tabelaUS(us, cu, o)...)
		}
	}
	var minhas []central.Pendencia
	for _, p := range abertas {
		if p.Coordenador == c.Nome {
			minhas = append(minhas, p)
		}
	}
	if len(minhas) > 0 {
		ls = append(ls, e.tabelaPend(minhas, cp, o)...)
	}
	return ls
}

// faixa é o cabeçalho da onda, com fundo leve na largura do bloco (a das tabelas, ou a que os
// selos pedem, até a do terminal): na 1ª linha o coordenador e os
// projetos à esquerda e os selos (`até MR pronta`, `1/4 ▰▱▱▱`, `1 pend`, `concluída`) à direita; na 2ª o
// título. O nome leva ao pane do coordenador pelo link; a faixa em si não é alvo de foco.
func (e Estilos) faixa(c central.CoordSnapshot, onda central.Onda, nome string, bloco int, o Opcoes) []Linha {
	p := e.p()
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
		for _, pe := range u.Pendencias {
			if pe.Aberta {
				pend++
			}
		}
	}
	esq := " " + nome
	if len(projetos) > 0 {
		esq += e.cor(p.cinza, " · "+strings.Join(projetos, ", "))
	}
	ate := e.apagado("até " + rotulo(rotuloAte, onda.AutorizacaoAte))
	dir := fmt.Sprintf("%d/%d", feitas, len(onda.US))
	if b := e.barra(feitas, len(onda.US)); b != "" {
		dir += " " + b
	}
	if pend > 0 {
		dir += "  " + e.novo().Foreground(p.amarelo).Bold(true).Render(fmt.Sprintf("%d pend", pend))
	}
	// A regra é a do `orq onda proxima`: a onda está pronta para o coordenador encerrar.
	if enc := central.EncerramentoDe(onda); enc != nil && enc.Pronta {
		dir += "  " + e.cor(p.verde, "concluída")
	}
	dir += " "
	titulo := limpar(onda.Titulo)
	natural := max(ansi.StringWidth(esq)+2+ansi.StringWidth(ate)+2+ansi.StringWidth(dir), ansi.StringWidth(titulo)+2)
	w := min(o.Largura, max(bloco, natural))
	// Sem lugar, a autorização sai antes; depois os projetos e o nome encolhem.
	if ansi.StringWidth(esq)+2+ansi.StringWidth(ate)+2+ansi.StringWidth(dir) <= w {
		dir = ate + "  " + dir
	}
	esq = cortar(esq, max(w-ansi.StringWidth(dir)-2, 1))
	l1 := esq + strings.Repeat(" ", max(w-ansi.StringWidth(esq)-ansi.StringWidth(dir), 1)) + dir
	ls := []Linha{{Texto: e.linhaFaixa(l1, w)}}
	if titulo != "" {
		ls = append(ls, Linha{Texto: e.linhaFaixa(" "+cortar(titulo, w-2), w)})
	}
	return ls
}

func (e Estilos) linhaFaixa(s string, largura int) string {
	return pintar(preencher(cortar(s, largura), largura), fundo(e.p().faixa, false)) + "\x1b[m"
}

// barra é o progresso da onda, uma marca por US até 10 e proporcional acima disso.
func (e Estilos) barra(feitas, total int) string {
	if total <= 0 {
		return ""
	}
	n := min(total, 10)
	cheias := (feitas*n + total/2) / total
	if feitas > 0 && cheias == 0 {
		cheias = 1
	}
	if feitas < total && cheias == n {
		cheias = n - 1
	}
	return e.cor(e.p().verde, strings.Repeat("▰", cheias)) + e.apagado(strings.Repeat("▱", n-cheias))
}

// celula é o conteúdo de uma linha da tabela: as células, já estilizadas e cortadas na
// largura da coluna, uma linha física por linha da célula.
type celula struct {
	cels     []string
	altura   int
	pane     string
	item     string
	fundo    color.Color
	esmaecer bool
}

// grade desenha a tabela: borda arredondada, separadores de coluna, regra só sob o cabeçalho
// e o fundo de cada linha. As larguras são fixas (calculadas antes), então o lipgloss não
// redimensiona nem quebra nada; o mapeamento linha física → pane sai da altura de cada linha.
func (e Estilos) grade(cab []string, larg []int, rows []celula, largura int) []Linha {
	p := e.p()
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(e.novo().Foreground(p.borda)).
		BorderRow(false).
		Wrap(true).
		Headers(cab...)
	for _, r := range rows {
		cels := make([]string, len(r.cels))
		for j, c := range r.cels {
			cels[j] = pintar(c, fundo(r.fundo, r.esmaecer))
		}
		t.Row(cels...)
	}
	t.StyleFunc(func(row, col int) lipgloss.Style {
		st := e.novo().Padding(0, 1).Width(larg[col] + 2)
		if row == table.HeaderRow {
			return st.Foreground(p.cinza).Bold(true)
		}
		r := rows[row]
		st = st.Height(r.altura)
		if r.fundo != nil {
			st = st.Background(r.fundo)
		}
		if r.esmaecer {
			st = st.Faint(true)
		}
		return st
	})
	fis := strings.Split(t.String(), "\n")
	ls := make([]Linha, 0, len(fis))
	add := func(texto, pane, item string) {
		ls = append(ls, Linha{Texto: cortar(texto, largura), Pane: pane, Item: item})
	}
	// borda de cima, cabeçalho e regra; depois as linhas, cada uma com a sua altura.
	i := 0
	for ; i < 3 && i < len(fis); i++ {
		add(fis[i], "", "")
	}
	for _, r := range rows {
		for k := 0; k < r.altura && i < len(fis); k, i = k+1, i+1 {
			add(fis[i], r.pane, r.item)
		}
	}
	for ; i < len(fis); i++ {
		add(fis[i], "", "")
	}
	return ls
}

// fundoLinha: a selecionada tem o fundo de seleção, a blocked o de destaque e as outras se
// alternam entre o fundo do terminal e o leve.
func (e Estilos) fundoLinha(i int, item string, blk bool, o Opcoes) color.Color {
	p := e.p()
	switch {
	case item != "" && item == o.Selecao:
		return p.selecao
	case blk:
		return p.bloqueada
	case i%2 == 1:
		return p.zebra
	}
	return nil
}

// tabelaUS: agente | US (link do Taiga) | título | status (cor por valor, link que foca o
// pane) | MRs, uma por linha física da célula.
func (e Estilos) tabelaUS(us []central.US, l colsUS, o Opcoes) []Linha {
	p := e.p()
	rows := make([]celula, 0, len(us))
	for i, u := range us {
		pane := ""
		if u.PaneID != nil {
			pane = *u.PaneID
		}
		urlTaiga := ""
		if u.Taiga.URL != nil {
			urlTaiga = *u.Taiga.URL
		}
		blk := bloqueada(u)
		stEstilo := e.novo().Foreground(e.corStatus(u.Status))
		if blk {
			stEstilo = e.novo().Foreground(p.vermelho).Bold(true)
		}
		status := linkPane(pane, stEstilo.Render(cortar(statusTexto(u), l.status)))
		// Com erro na última consulta ao Taiga, o título é o último bom e uma marca apagada no
		// fim diz que a consulta mais recente falhou.
		tw := l.titulo
		if u.Taiga.Erro != nil {
			tw = max(tw-1, 0)
		}
		var titulo string
		if u.Taiga.Titulo == nil || *u.Taiga.Titulo == "" {
			titulo = e.apagado(cortar(u.Chave, tw))
		} else {
			titulo = cortar(limpar(*u.Taiga.Titulo), tw)
			if blk {
				titulo = e.negrito(titulo)
			}
		}
		if u.Taiga.Erro != nil {
			titulo += e.apagado("*")
		}
		var mrs []string
		for _, m := range u.MRs {
			mrs = append(mrs, cortar(e.mr(m), l.mr))
		}
		item := "us:" + u.Chave
		rows = append(rows, celula{
			cels:     []string{e.icone(u), link(urlTaiga, refTexto(u)), titulo, status, strings.Join(mrs, "\n")},
			altura:   max(len(mrs), 1),
			pane:     pane,
			item:     item,
			fundo:    e.fundoLinha(i, item, blk, o),
			esmaecer: u.Status == "done" && !blk,
		})
	}
	return e.grade(cabUS, []int{l.ag, l.ref, l.titulo, l.status, l.mr}, rows, o.Largura)
}

// tabelaPend: id | tipo | texto em até 2 linhas (corta com … depois) | idade | ↗, que foca o
// pane que perguntou.
func (e Estilos) tabelaPend(ps []central.Pendencia, l colsPend, o Opcoes) []Linha {
	rows := make([]celula, 0, len(ps))
	for i, p := range ps {
		pane := ""
		if p.PaneOrigem != nil {
			pane = *p.PaneOrigem
		}
		texto := duasLinhas(limpar(p.Texto), l.texto)
		item := "pend:" + p.Coordenador + "/" + p.ID
		ir := ""
		if pane != "" {
			ir = linkPane(pane, "↗")
		}
		rows = append(rows, celula{
			cels: []string{
				e.cor(e.p().amarelo, cortar(limpar(p.ID), l.id)),
				e.cor(e.corTipo(p.Tipo), cortar(rotulo(rotuloTipo, p.Tipo), l.tipo)),
				strings.Join(texto, "\n"),
				e.apagado(cortar(desde(p.AbertaEm, o.Agora), l.idade)),
				ir,
			},
			altura: len(texto),
			pane:   pane,
			item:   item,
			fundo:  e.fundoLinha(i, item, false, o),
		})
	}
	return e.grade(cabPend, []int{l.id, l.tipo, l.texto, l.idade, l.ir}, rows, o.Largura)
}

// duasLinhas quebra o texto nos espaços, na largura w, e guarda até duas linhas; o que sobra
// vira … no fim da segunda. Só o espaço quebra (o ansi.Wrap quebra também no hífen e partiria
// um `--cascade`); palavra maior que a linha é cortada no meio.
func duasLinhas(s string, w int) []string {
	w = max(w, 1)
	var ls []string
	atual := ""
	for _, p := range strings.Fields(s) {
		switch {
		case atual == "":
			atual = p
		case ansi.StringWidth(atual)+1+ansi.StringWidth(p) <= w:
			atual += " " + p
		default:
			ls = append(ls, atual)
			atual = p
		}
		for ansi.StringWidth(atual) > w {
			cab := ansi.Truncate(atual, w, "")
			if cab == "" {
				// Caractere largo numa coluna de 1: vira … e o texto anda, senão o laço não
				// termina.
				_, n := utf8.DecodeRuneInString(atual)
				ls = append(ls, "…")
				atual = atual[n:]
				continue
			}
			ls = append(ls, cab)
			atual = atual[len(cab):]
		}
	}
	if atual != "" || len(ls) == 0 {
		ls = append(ls, atual)
	}
	if len(ls) > 2 {
		ls = []string{ls[0], cortar(strings.Join(ls[1:], " "), w)}
		if !strings.HasSuffix(ls[1], "…") {
			ls[1] = cortar(ls[1]+" …", w)
		}
	}
	return ls
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
