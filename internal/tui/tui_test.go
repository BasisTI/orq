package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/BasisTI/orq/internal/central"
	"github.com/charmbracelet/x/ansi"
)

var agora = time.Date(2026, 10, 11, 14, 30, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func herdr(st string) *central.Herdr { return &central.Herdr{Status: ptr(st)} }

func mr(url, path string, iid int, estado string, iac bool) central.MR {
	m := central.MR{URL: url, ProjetoPath: path, IID: iid, IaC: iac}
	if estado != "" {
		m.Estado = ptr(estado)
	}
	return m
}

func us(ref int, status, pane, hst, titulo string, rodada int, mrs ...central.MR) central.US {
	u := central.US{ID: "TG-" + itoa(ref), Chave: "sgo-cli/" + itoa(ref), Projeto: "sgo-cli", Ref: ref, Status: status, MRs: mrs}
	if pane != "" {
		u.PaneID = ptr(pane)
		u.Herdr = herdr(hst)
	}
	if rodada > 0 {
		u.Rodada = ptr(rodada)
	}
	if titulo != "" {
		u.Taiga.Titulo = ptr(titulo)
	}
	u.Taiga.URL = ptr("https://agile.basis.com.br/project/sgo-cli/us/" + itoa(ref))
	return u
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(n)) }

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// snapshot é a tela do desenho: convey · sgo-cli, com uma US blocked, MRs de todos os estados
// e uma pendência aberta.
func snapshot() central.Snapshot {
	gl := "https://gitlab.basis.com.br/basis/sgo-cli/-/merge_requests/"
	onda := central.Onda{ID: 1, Coordenador: "convey", Titulo: "Comandos api e jql", AutorizacaoAte: "mr-pronta", Ordem: "livre", US: []central.US{
		us(312, "rev", "wB:p3", "working", "sgo jql: consulta avulsa por JQL com paginação e campos escolhidos", 2, mr(gl+"32", "basis/sgo-cli", 32, "pronta", false)),
		us(313, "exec", "wB:p4", "blocked", "sgo api: chamada REST", 1,
			mr(gl+"33", "basis/sgo-cli", 33, "draft", false),
			mr("https://gitlab.basis.com.br/basis/iac/plataforma/-/merge_requests/412", "basis/iac/plataforma", 412, "aberta", true)),
		us(314, "wait", "", "", "Menores da TG-312", 0),
		us(225, "done", "wB:p6", "done", "Criação Suporte/OS", 0,
			mr(gl+"29", "basis/sgo-cli", 29, "mesclada", false),
			mr(gl+"31", "basis/sgo-cli", 31, "fechada", false)),
	}}
	p := central.Pendencia{ID: "TG-313#P1", US: "sgo-cli/313", Numero: 1, Tipo: "autorizacao", Texto: "instalar binário e aplicar config pessoal", PaneOrigem: ptr("wB:p4"), Aberta: true, AbertaEm: agora.Add(-12 * time.Minute).Format(time.RFC3339), OndaID: 1, Coordenador: "convey"}
	onda.US[1].Pendencias = []central.Pendencia{p}
	return central.Snapshot{
		Versao: 1,
		Coordenadores: []central.CoordSnapshot{
			{Coordenador: central.Coordenador{Nome: "convey", PaneID: ptr("wB:p1")}, Ondas: []central.Onda{onda}},
			{Coordenador: central.Coordenador{Nome: "liven", PaneID: ptr("w7:p1")}, Ondas: []central.Onda{}},
		},
		PendenciasAbertas: []central.Pendencia{p},
		Fontes: map[string]central.Fonte{
			"herdr": {AtualizadoEm: ptr(agora.Add(-3 * time.Second).Format(time.RFC3339))},
			"mr":    {AtualizadoEm: ptr(agora.Add(-40 * time.Second).Format(time.RFC3339))},
			"taiga": {AtualizadoEm: ptr(agora.Add(-2 * time.Minute).Format(time.RFC3339)), Erro: &central.Erro{Codigo: "autenticacao", Mensagem: "taiga suspenso"}},
		},
	}
}

var cores = Estilos{}

func desenhar(o Opcoes) Tela {
	if o.Agora.IsZero() {
		o.Agora = agora
	}
	return cores.Desenhar(snapshot(), o)
}

func planas(t Tela) []string {
	var ls []string
	for _, l := range t.Linhas {
		ls = append(ls, ansi.Strip(l.Texto))
	}
	return ls
}

func TestTelaDoCoordenador(t *testing.T) {
	tela := desenhar(Opcoes{Coord: "convey", Largura: 100})
	ls := planas(tela)
	quer := []string{
		"convey · sgo-cli — Comandos api e jql  [até: MR pronta]  1/4 done · 1 pend",
		// A US blocked sobe para o topo; o resto segue a ordem da onda.
		"⚠ #313 sgo api: chamada REST                                                Exec r1 ↗",
		"       !33 draft",
		"       !412 [IaC] aberta",
		"● #312 sgo jql: consulta avulsa por JQL com paginação e campos escolhidos   Rev r2 ↗   !32",
		"– #314 Menores da TG-312                                                    Wait",
		"✓ #225 Criação Suporte/OS                                                   Done ↗",
		"       !29 mesclada",
		"       !31 fechada",
		"Pendências",
		" TG-313#P1 autorização  instalar binário e aplicar config pessoal  (há 12 min) ↗",
	}
	if len(ls) != len(quer) {
		t.Fatalf("linhas:\n%s", strings.Join(ls, "\n"))
	}
	for i := range quer {
		if strings.TrimRight(ls[i], " ") != quer[i] {
			t.Errorf("linha %d:\n tem %q\nquer %q", i, strings.TrimRight(ls[i], " "), quer[i])
		}
	}
	// Nenhuma linha passa da largura.
	for i, l := range tela.Linhas {
		if w := ansi.StringWidth(l.Texto); w > 100 {
			t.Errorf("linha %d com %d colunas", i, w)
		}
	}
	// Alvos de foco: cabeçalho → pane do coordenador; US e as linhas de MR → pane da US;
	// US sem pane, sem alvo; pendência → pane de origem.
	alvos := []string{"wB:p1", "wB:p4", "wB:p4", "wB:p4", "wB:p3", "", "wB:p6", "wB:p6", "wB:p6", "", "wB:p4"}
	for i, a := range alvos {
		if tela.Linhas[i].Pane != a {
			t.Errorf("alvo da linha %d = %q, quer %q", i, tela.Linhas[i].Pane, a)
		}
	}
}

// Num pane largo, a coluna do título não passa do título mais longo: o status fica perto.
func TestPaneLargo(t *testing.T) {
	ls := planas(desenhar(Opcoes{Coord: "convey", Largura: 300}))
	if strings.TrimRight(ls[4], " ") != "● #312 sgo jql: consulta avulsa por JQL com paginação e campos escolhidos   Rev r2 ↗   !32" {
		t.Fatalf("%q", ls[4])
	}
}

func TestTruncamento(t *testing.T) {
	tela := desenhar(Opcoes{Coord: "convey", Largura: 50})
	for i, l := range tela.Linhas {
		if w := ansi.StringWidth(l.Texto); w > 50 {
			t.Errorf("linha %d com %d colunas: %q", i, w, ansi.Strip(l.Texto))
		}
	}
	ls := planas(tela)
	if ls[4] != "● #312 sgo jql: consulta avulsa p…  Rev r2 ↗   !32" {
		t.Errorf("título não truncado: %q", ls[4])
	}
	if ls[10] != " TG-313#P1 autorização  instalar b…  (há 12 min) ↗" {
		t.Errorf("pendência não truncada: %q", ls[10])
	}
	// Sem lugar para o título da onda, a autorização sai primeiro.
	if ls[0] != "convey · sgo-cli — Comandos ap…  1/4 done · 1 pend" {
		t.Errorf("cabeçalho estreito: %q", ls[0])
	}
	// Largura mínima não quebra.
	_ = desenhar(Opcoes{Coord: "convey", Largura: 5})
}

func TestEstilosEOSC8(t *testing.T) {
	tela := desenhar(Opcoes{Coord: "convey", Largura: 100})
	tudo := ""
	for _, l := range tela.Linhas {
		tudo += l.Texto + "\n"
	}
	vermelho, verde, cinza, laranja := "38;2;224;108;117", "38;2;152;195;121", "38;2;127;132;142", "38;2;255;140;0"
	casos := []struct{ nome, quer string }{
		{"draft vermelho com link", "\x1b]8;;https://gitlab.basis.com.br/basis/sgo-cli/-/merge_requests/33\x1b\\\x1b[" + vermelho + "m!33"},
		{"pronta verde", "\x1b[" + verde + "m!32"},
		{"mesclada cinza tachado", "merge_requests/29\x1b\\\x1b[" + cinza + ";9m!"},
		{"IaC laranja", "\x1b[" + laranja + "m[IaC]"},
		{"fechada roxo itálico", "\x1b[3;38;2;198;120;221m!31"},
		{"#ref com link do Taiga", "\x1b]8;;https://agile.basis.com.br/project/sgo-cli/us/313\x1b\\#313\x1b]8;;\x1b\\"},
		{"status com link de pane", "\x1b]8;;https://orq.local/pane/wB:p4\x1b\\"},
		{"blocked em negrito vermelho", "\x1b[1;" + vermelho + "m⚠"},
		{"pendência ↗ foca a origem", "\x1b]8;;https://orq.local/pane/wB:p4\x1b\\↗\x1b]8;;\x1b\\"},
	}
	for _, c := range casos {
		if !strings.Contains(tudo, c.quer) {
			t.Errorf("%s: falta %q", c.nome, c.quer)
		}
	}
	// Aberta é neutra: sem cor.
	if !strings.Contains(tudo, "merge_requests/412\x1b\\!412\x1b]8;;") {
		t.Errorf("aberta não é neutra:\n%q", tudo)
	}
	// O título da US blocked fica em negrito.
	if !strings.Contains(tudo, "\x1b[1msgo api: chamada REST") {
		t.Errorf("título da blocked sem destaque")
	}
}

func TestErroDeFonteDiscretoMantemValor(t *testing.T) {
	s := snapshot()
	u := &s.Coordenadores[0].Ondas[0].US[0]
	u.Taiga.Erro = &central.Erro{Codigo: "tempo_esgotado", Mensagem: "taiga não respondeu"}
	u.MRs[0].Erro = &central.Erro{Codigo: "falha_comando", Mensagem: "glab saiu com 1"}
	tela := cores.Desenhar(s, Opcoes{Coord: "convey", Largura: 100, Agora: agora})
	ls := planas(tela)
	if !strings.Contains(ls[4], "sgo jql: consulta avulsa") || !strings.Contains(ls[4], "*  Rev r2 ↗") || !strings.Contains(ls[4], "!32*") {
		t.Errorf("linha com erro: %q", ls[4])
	}
	rod := ansi.Strip(tela.Rodape)
	for _, quer := range []string{"herdr há 0 min", "taiga há 2 min ✗ autenticacao", "* última consulta falhou"} {
		if quer == "herdr há 0 min" {
			quer = "herdr agora"
		}
		if !strings.Contains(rod, quer) {
			t.Errorf("rodapé sem %q: %q", quer, rod)
		}
	}
}

func TestTodos(t *testing.T) {
	ls := planas(desenhar(Opcoes{Todos: true, Largura: 100}))
	if ls[0] != "Central · 2 coordenadores · 1 pendência aberta" {
		t.Errorf("cabeçalho: %q", ls[0])
	}
	tudo := strings.Join(ls, "\n")
	if !strings.Contains(tudo, "convey · sgo-cli — Comandos api e jql") || !strings.Contains(tudo, "liven — sem onda aberta") {
		t.Errorf("blocos:\n%s", tudo)
	}
}

func TestCoordenadorDesconhecido(t *testing.T) {
	ls := planas(desenhar(Opcoes{Coord: "ninguem", Largura: 80}))
	if len(ls) != 1 || ls[0] != "coordenador ninguem não registrado" {
		t.Fatalf("%q", ls)
	}
}

func TestTextoDeAgenteNaoMexeNoTerminal(t *testing.T) {
	s := snapshot()
	s.Coordenadores[0].Ondas[0].US[2].Taiga.Titulo = ptr("mau\x1b[2Jtítulo\x07")
	s.PendenciasAbertas[0].Texto = "linha\nquebrada\x1b]8;;http://x\x1b\\"
	tela := cores.Desenhar(s, Opcoes{Coord: "convey", Largura: 100, Agora: agora})
	for _, l := range tela.Linhas {
		if strings.Contains(l.Texto, "\x1b[2J") || strings.Contains(l.Texto, "\x07") || strings.Contains(l.Texto, "\n") || strings.Contains(l.Texto, "http://x") && strings.Contains(l.Texto, "\x1b]8;;http://x") {
			t.Fatalf("controle passou: %q", l.Texto)
		}
	}
}

// O modelo sobrevive a leitura e foco que falham (binário ausente, pânico) e mostra o erro na
// linha de status.
func TestModeloSobreviveAFalhas(t *testing.T) {
	falhar := true
	focos := []string{}
	cfg := Config{
		Carregar: func(context.Context) (central.Snapshot, error) {
			if falhar {
				return central.Snapshot{}, errors.New("banco ocupado")
			}
			return snapshot(), nil
		},
		Focar: func(_ context.Context, pane string) error {
			focos = append(focos, pane)
			if pane == "wB:p3" {
				panic("herdr sumiu")
			}
			return errors.New("binario_ausente: herdr não encontrado")
		},
		Opcoes:  Opcoes{Coord: "convey"},
		Estilos: cores,
		Agora:   func() time.Time { return agora },
	}
	m := Novo(context.Background(), cfg)
	m.Update(tea.WindowSizeMsg{Width: 101, Height: 30})
	m.Update(m.carregar())
	if !strings.Contains(ansi.Strip(m.View().Content), "leitura falhou: banco ocupado") {
		t.Fatalf("sem erro de leitura:\n%s", ansi.Strip(m.View().Content))
	}
	falhar = false
	m.Update(m.carregar())
	if strings.Contains(m.View().Content, "leitura falhou") || !strings.Contains(ansi.Strip(m.View().Content), "#313") {
		t.Fatalf("não recuperou:\n%s", ansi.Strip(m.View().Content))
	}
	// Uma leitura que falha depois mantém a última tela boa.
	falhar = true
	m.Update(m.carregar())
	v := ansi.Strip(m.View().Content)
	if !strings.Contains(v, "#313") || !strings.Contains(v, "leitura falhou") {
		t.Fatalf("perdeu a tela boa:\n%s", v)
	}
	// ↓ seleciona a primeira linha com pane (o cabeçalho), ↓ de novo a US blocked; Enter foca.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter sem foco")
	}
	_, q := m.Update(cmd())
	if q != nil || len(focos) != 1 || focos[0] != "wB:p4" {
		t.Fatalf("focos = %v", focos)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "foco em wB:p4 falhou: binario_ausente") {
		t.Fatalf("erro do foco não aparece:\n%s", ansi.Strip(m.View().Content))
	}
	if !strings.HasPrefix(ansi.Strip(strings.Split(m.View().Content, "\n")[1]), "›⚠ #313") {
		t.Fatalf("seleção: %q", ansi.Strip(strings.Split(m.View().Content, "\n")[1]))
	}
	// Clique na linha da #312 (a 5ª: cabeçalho, #313 e duas MRs antes): pânico vira erro.
	_, cmd = m.Update(tea.MouseClickMsg{X: 3, Y: 4, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("clique sem foco")
	}
	m.Update(cmd())
	if !strings.Contains(ansi.Strip(m.View().Content), "foco em wB:p3 falhou: pânico: herdr sumiu") {
		t.Fatalf("pânico não virou erro:\n%s", ansi.Strip(m.View().Content))
	}
	// Clique em linha sem pane não faz nada.
	if _, cmd = m.Update(tea.MouseClickMsg{X: 3, Y: 5, Button: tea.MouseLeft}); cmd != nil {
		t.Fatal("clique na #314 (sem pane) focou")
	}
}

func TestModeloPopupSaiAoFocar(t *testing.T) {
	m := Novo(context.Background(), Config{
		Carregar:    func(context.Context) (central.Snapshot, error) { return snapshot(), nil },
		Focar:       func(context.Context, string) error { return nil },
		Opcoes:      Opcoes{Todos: true},
		SairAoFocar: true,
		Estilos:     cores,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(m.carregar())
	_, cmd := m.Update(msgFoco{pane: "wB:p4"})
	if cmd == nil || !m.sair {
		t.Fatal("popup não fechou depois do foco")
	}
}

func TestSelecaoSegueOPane(t *testing.T) {
	s := snapshot()
	s.Coordenadores[0].Ondas[0].US[1].Herdr = herdr("working") // ninguém blocked
	carregada := s
	m := Novo(context.Background(), Config{
		Carregar: func(context.Context) (central.Snapshot, error) { return carregada, nil },
		Focar:    func(context.Context, string) error { return nil },
		Opcoes:   Opcoes{Coord: "convey"},
		Estilos:  cores,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(m.carregar())
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // #312
	if m.tela.Linhas[m.sel].Pane != "wB:p3" {
		t.Fatalf("seleção em %q", m.tela.Linhas[m.sel].Pane)
	}
	carregada = snapshot() // #313 blocked sobe
	m.Update(m.carregar())
	if m.tela.Linhas[m.sel].Pane != "wB:p3" {
		t.Fatalf("seleção pulou para %q", m.tela.Linhas[m.sel].Pane)
	}
}

func TestDesde(t *testing.T) {
	for d, quer := range map[time.Duration]string{30 * time.Second: "agora", 12 * time.Minute: "há 12 min", 3 * time.Hour: "há 3 h", 72 * time.Hour: "há 3 d"} {
		if s := desde(agora.Add(-d).Format(time.RFC3339), agora); s != quer {
			t.Errorf("%v = %q", d, s)
		}
	}
}
