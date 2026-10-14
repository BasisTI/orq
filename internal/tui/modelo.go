package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/BasisTI/orq/internal/central"
	"github.com/charmbracelet/x/ansi"
)

// Config liga o modelo ao mundo: de onde vem o snapshot e como focar um pane. As duas funções
// rodam fora do laço do bubbletea (num tea.Cmd); erro delas vira texto na linha de status e
// nunca derruba a tela.
type Config struct {
	Carregar  func(ctx context.Context) (central.Snapshot, error)
	Focar     func(ctx context.Context, pane string) error
	Opcoes    Opcoes
	Intervalo time.Duration // entre as releituras do snapshot; zero usa 2 s
	// SairAoFocar fecha a tela depois de focar um pane: no popup (--todos) o foco só aparece
	// quando o popup some.
	SairAoFocar bool
	Estilos     Estilos
	Agora       func() time.Time
}

// Modelo é o tea.Model da Central.
type Modelo struct {
	cfg     Config
	snap    *central.Snapshot
	erro    string // falha da última leitura do snapshot
	msg     string // resultado da última ação (foco)
	largura int
	altura  int
	tela    Tela
	sel     int // índice em tela.Linhas da linha selecionada (a 1ª do item); -1 sem seleção
	topo    int // primeira linha visível
	ctx     context.Context
	sair    bool
}

// Novo monta o modelo.
func Novo(ctx context.Context, cfg Config) *Modelo {
	if cfg.Intervalo <= 0 {
		cfg.Intervalo = 2 * time.Second
	}
	if cfg.Agora == nil {
		cfg.Agora = time.Now
	}
	return &Modelo{cfg: cfg, sel: -1, largura: 80, altura: 24, ctx: ctx}
}

type msgSnapshot struct {
	snap central.Snapshot
	err  error
}

type msgTick struct{}

type msgFoco struct {
	pane string
	err  error
}

func (m *Modelo) carregar() tea.Msg {
	s, err := protegido(func() (central.Snapshot, error) { return m.cfg.Carregar(m.ctx) })
	return msgSnapshot{s, err}
}

func (m *Modelo) agendar() tea.Cmd {
	return tea.Tick(m.cfg.Intervalo, func(time.Time) tea.Msg { return msgTick{} })
}

func (m *Modelo) focar(pane string) tea.Cmd {
	return func() tea.Msg {
		_, err := protegido(func() (struct{}, error) { return struct{}{}, m.cfg.Focar(m.ctx, pane) })
		return msgFoco{pane, err}
	}
}

// protegido transforma pânico em erro: nada que a leitura ou o foco façam derruba a tela.
func protegido[T any](f func() (T, error)) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pânico: %v", r)
		}
	}()
	return f()
}

// Init lê o snapshot, agenda a próxima leitura e pergunta a cor de fundo do terminal. A
// pergunta não espera: a resposta, se vier, chega como tea.BackgroundColorMsg e troca os tons;
// até lá (e num terminal que não responde) vale o escuro.
func (m *Modelo) Init() tea.Cmd {
	return tea.Batch(m.carregar, m.agendar(), tea.RequestBackgroundColor)
}

// Update trata teclado, mouse, tamanho, leituras e focos.
func (m *Modelo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.largura, m.altura = msg.Width, msg.Height
		m.redesenhar()
	case tea.BackgroundColorMsg:
		m.cfg.Estilos.Claro = !msg.IsDark()
		m.redesenhar()
	case msgTick:
		return m, tea.Batch(m.carregar, m.agendar())
	case msgSnapshot:
		if msg.err != nil {
			m.erro = msg.err.Error()
		} else {
			m.erro = ""
			s := msg.snap
			m.snap = &s
		}
		m.redesenhar()
	case msgFoco:
		if msg.err != nil {
			m.msg = "foco em " + limpar(msg.pane) + " falhou: " + limpar(msg.err.Error())
			return m, nil
		}
		m.msg = "foco em " + limpar(msg.pane)
		if m.cfg.SairAoFocar {
			m.sair = true
			return m, tea.Quit
		}
	case tea.KeyPressMsg:
		return m.tecla(msg)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			i := m.topo + msg.Y
			if msg.Y < m.visiveis() && i < len(m.tela.Linhas) && m.tela.Linhas[i].Pane != "" {
				m.selecionar(i)
				return m, m.focar(m.tela.Linhas[i].Pane)
			}
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.rolar(-3)
		case tea.MouseWheelDown:
			m.rolar(3)
		}
	}
	return m, nil
}

func (m *Modelo) tecla(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c", "esc":
		m.sair = true
		return m, tea.Quit
	case "r", "f5":
		m.msg = "relendo…"
		return m, m.carregar
	case "down", "j":
		m.mover(1)
	case "up", "k":
		m.mover(-1)
	case "pgdown", "space":
		m.rolar(m.visiveis())
	case "pgup":
		m.rolar(-m.visiveis())
	case "enter":
		if m.sel >= 0 && m.sel < len(m.tela.Linhas) && m.tela.Linhas[m.sel].Pane != "" {
			return m, m.focar(m.tela.Linhas[m.sel].Pane)
		}
	}
	return m, nil
}

// mover leva a seleção para o próximo item com pane, na direção dada: pula bordas, regras e
// faixas (sem pane) e as outras linhas físicas do item selecionado.
func (m *Modelo) mover(d int) {
	atual := ""
	if m.sel >= 0 && m.sel < len(m.tela.Linhas) {
		atual = m.tela.Linhas[m.sel].Item
	}
	i := m.sel
	for {
		i += d
		if i < 0 || i >= len(m.tela.Linhas) {
			return
		}
		l := m.tela.Linhas[i]
		if l.Pane != "" && (l.Item == "" || l.Item != atual) {
			m.selecionar(i)
			m.mostrar(m.sel)
			return
		}
	}
}

// selecionar marca o item da linha i e redesenha para o fundo de seleção acompanhar.
func (m *Modelo) selecionar(i int) {
	m.sel = i
	m.redesenhar()
}

func (m *Modelo) rolar(d int) {
	m.topo = max(0, min(m.topo+d, len(m.tela.Linhas)-m.visiveis()))
}

func (m *Modelo) mostrar(i int) {
	if i < m.topo {
		m.topo = i
	}
	if v := m.visiveis(); i >= m.topo+v {
		m.topo = i - v + 1
	}
}

// visiveis é quantas linhas da tela cabem acima da linha de status.
func (m *Modelo) visiveis() int { return max(m.altura-1, 1) }

func (m *Modelo) redesenhar() {
	if m.snap == nil {
		m.tela = Tela{}
		return
	}
	o := m.cfg.Opcoes
	o.Largura = max(m.largura, 10)
	o.Agora = m.cfg.Agora()
	// A seleção segue o item (a US ou a pendência), não o índice: a ordem muda quando um pane
	// fica blocked. Sem item, segue o pane.
	var antes Linha
	if m.sel >= 0 && m.sel < len(m.tela.Linhas) {
		antes = m.tela.Linhas[m.sel]
	}
	o.Selecao = antes.Item
	m.tela = m.cfg.Estilos.Desenhar(*m.snap, o)
	m.sel = -1
	for i, l := range m.tela.Linhas {
		if antes.Item != "" && l.Item == antes.Item || antes.Item == "" && antes.Pane != "" && l.Pane == antes.Pane {
			m.sel = i
			break
		}
	}
	m.rolar(0)
}

// View desenha as linhas visíveis e a linha de status, em tela alternativa e com o mouse.
func (m *Modelo) View() tea.View {
	v := tea.NewView(m.conteudo())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "orq — Central"
	return v
}

func (m *Modelo) conteudo() string {
	if m.sair {
		return ""
	}
	var b strings.Builder
	v := m.visiveis()
	n := 0
	if m.snap == nil {
		b.WriteString(" " + m.cfg.Estilos.apagado("lendo a Central…") + "\n")
		n++
	}
	for i := m.topo; i < len(m.tela.Linhas) && n < v; i, n = i+1, n+1 {
		b.WriteString(m.tela.Linhas[i].Texto + "\n")
	}
	for ; n < v; n++ {
		b.WriteString("\n")
	}
	b.WriteString(m.status())
	return b.String()
}

// status é a última linha: erro de leitura, resultado da ação, o rodapé das fontes, a legenda
// dos ícones e as teclas. Sem lugar, a legenda cede primeiro, um ícone por vez a partir do fim;
// o resto é cortado.
func (m *Modelo) status() string {
	e := m.cfg.Estilos
	var antes []string
	if m.erro != "" {
		antes = append(antes, e.cor(e.p().vermelho, "leitura falhou: "+limpar(m.erro)))
	}
	if m.msg != "" {
		antes = append(antes, limpar(m.msg))
	}
	if m.tela.Rodape != "" {
		antes = append(antes, m.tela.Rodape)
	}
	teclas := e.apagado("↑↓ Enter foca · r relê · q sai")
	w := max(m.largura-1, 1)
	for n := len(m.tela.Legenda); n >= 0; n-- {
		partes := append([]string(nil), antes...)
		if n > 0 {
			partes = append(partes, strings.Join(m.tela.Legenda[:n], " "))
		}
		s := strings.Join(append(partes, teclas), "  ")
		if n == 0 || ansi.StringWidth(s) <= w {
			return " " + ansi.Truncate(s, w, "…")
		}
	}
	return ""
}
