package tui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
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

// Contrato da tela de um coordenador a 100 colunas: faixa de 2 linhas, tabela das US com
// bordas, MRs empilhadas na célula, tabela de pendências; e o alvo de cada linha física.
func TestTelaDoCoordenador(t *testing.T) {
	tela := desenhar(Opcoes{Coord: "convey", Largura: 100})
	quer := []struct{ texto, pane string }{
		{" convey · sgo-cli                                                   até MR pronta  1/4 ▰▱▱▱  1 pend ", ""},
		{" Comandos api e jql                                                                                 ", ""},
		{"╭───┬──────┬──────────────────────────────────────────────────────────────┬───────────┬────────────╮", ""},
		{"│   │ US   │ título                                                       │ status    │ MRs        │", ""},
		{"├───┼──────┼──────────────────────────────────────────────────────────────┼───────────┼────────────┤", ""},
		// A US blocked sobe para o topo; o resto segue a ordem da onda. As MRs ficam na mesma
		// célula, uma por linha física, e toda linha da US aponta para o pane dela.
		{"│ ⚠ │ #313 │ sgo api: chamada REST                                        │ Exec r1 ↗ │ !33        │", "wB:p4"},
		{"│   │      │                                                              │           │ !412 [IaC] │", "wB:p4"},
		{"│ ● │ #312 │ sgo jql: consulta avulsa por JQL com paginação e campos esc… │ Rev r2 ↗  │ !32        │", "wB:p3"},
		{"│ – │ #314 │ Menores da TG-312                                            │ Wait      │            │", ""},
		{"│ ✓ │ #225 │ Criação Suporte/OS                                           │ Done ↗    │ !29        │", "wB:p6"},
		{"│   │      │                                                              │           │ !31        │", "wB:p6"},
		{"╰───┴──────┴──────────────────────────────────────────────────────────────┴───────────┴────────────╯", ""},
		{"╭───────────┬─────────────┬───────────────────────────────────────────┬───────────┬───╮", ""},
		{"│ id        │ tipo        │ texto                                     │ idade     │   │", ""},
		{"├───────────┼─────────────┼───────────────────────────────────────────┼───────────┼───┤", ""},
		{"│ TG-313#P1 │ autorização │ instalar binário e aplicar config pessoal │ há 12 min │ ↗ │", "wB:p4"},
		{"╰───────────┴─────────────┴───────────────────────────────────────────┴───────────┴───╯", ""},
	}
	ls := planas(tela)
	if len(ls) != len(quer) {
		t.Fatalf("linhas:\n%s", strings.Join(ls, "\n"))
	}
	for i, q := range quer {
		if ls[i] != q.texto {
			t.Errorf("linha %d:\n tem %q\nquer %q", i, ls[i], q.texto)
		}
		if tela.Linhas[i].Pane != q.pane {
			t.Errorf("alvo da linha %d = %q, quer %q", i, tela.Linhas[i].Pane, q.pane)
		}
	}
	// As linhas de uma US têm o mesmo item; bordas e faixas, nenhum.
	if tela.Linhas[5].Item != "us:sgo-cli/313" || tela.Linhas[6].Item != tela.Linhas[5].Item || tela.Linhas[2].Item != "" || tela.Linhas[0].Item != "" {
		t.Errorf("itens: %q %q %q %q", tela.Linhas[5].Item, tela.Linhas[6].Item, tela.Linhas[2].Item, tela.Linhas[0].Item)
	}
	// Nenhuma linha passa da largura.
	for i, l := range tela.Linhas {
		if w := ansi.StringWidth(l.Texto); w > 100 {
			t.Errorf("linha %d com %d colunas", i, w)
		}
	}
}

// Num pane largo as colunas não esticam além do conteúdo: o título mais longo cabe inteiro e a
// tabela fica mais estreita que a tela. A faixa vai de ponta a ponta do bloco, não da tela:
// os selos ficam perto do nome.
func TestPaneLargo(t *testing.T) {
	tela := desenhar(Opcoes{Coord: "convey", Largura: 300})
	ls := planas(tela)
	if ls[7] != "│ ● │ #312 │ sgo jql: consulta avulsa por JQL com paginação e campos escolhidos │ Rev r2 ↗  │ !32        │" {
		t.Fatalf("%q", ls[7])
	}
	if w, tw := ansi.StringWidth(tela.Linhas[0].Texto), ansi.StringWidth(tela.Linhas[2].Texto); w != tw || ansi.StringWidth(tela.Linhas[1].Texto) != tw {
		t.Errorf("faixa com %d colunas, tabela com %d", w, tw)
	}
}

// Em 80 colunas o título encolhe; mais estreito, as MRs cedem (`!412…`). Nenhuma linha passa
// da largura, nem em largura mínima.
func TestTruncamento(t *testing.T) {
	for _, w := range []int{80, 60, 50, 30, 5} {
		for _, o := range []Opcoes{{Coord: "convey", Largura: w}, {Todos: true, Largura: w}} {
			for i, l := range desenhar(o).Linhas {
				if lw := ansi.StringWidth(l.Texto); lw > w {
					t.Errorf("largura %d, linha %d com %d colunas: %q", w, i, lw, ansi.Strip(l.Texto))
				}
			}
		}
	}
	ls := planas(desenhar(Opcoes{Coord: "convey", Largura: 80}))
	if ls[7] != "│ ● │ #312 │ sgo jql: consulta avulsa por JQL com pa… │ Rev r2 ↗  │ !32        │" {
		t.Errorf("título em 80: %q", ls[7])
	}
	if ls[0] != " convey · sgo-cli                               até MR pronta  1/4 ▰▱▱▱  1 pend " {
		t.Errorf("faixa em 80: %q", ls[0])
	}
	ls = planas(desenhar(Opcoes{Coord: "convey", Largura: 50}))
	if ls[5] != "│ ⚠ │ #313 │ sgo api: chama… │ Exec r1 ↗ │ !33   │" || ls[6] != "│   │      │                 │           │ !412… │" {
		t.Errorf("MRs não cederam em 50:\n%s\n%s", ls[5], ls[6])
	}
	// O corte da MR mantém o link fechado: nada de hyperlink vazando para a borda.
	if l := desenhar(Opcoes{Coord: "convey", Largura: 50}).Linhas[6].Texto; !strings.Contains(l, "!412\x1b]8;;\x1b\\…") {
		t.Errorf("link aberto depois do corte: %q", l)
	}
	// Sem lugar, a autorização sai da faixa antes do resto.
	if ls[0] != " convey · sgo-cli                1/4 ▰▱▱▱  1 pend " {
		t.Errorf("faixa em 50: %q", ls[0])
	}
}

// A pendência longa quebra em palavras e para na 2ª linha com …; as duas linhas apontam para o
// pane de origem.
func TestPendenciaEmDuasLinhas(t *testing.T) {
	s := snapshot()
	s.PendenciasAbertas[0].Texto = "escolher entre alternativa A que reescreve o importador, alternativa B que mantém o formato antigo e alternativa C que adia tudo (recomendado C)"
	tela := cores.Desenhar(s, Opcoes{Coord: "convey", Largura: 80, Agora: agora})
	ls := planas(tela)
	n := len(ls)
	l1, l2 := ls[n-3], ls[n-2]
	if l1 != "│ TG-313#P1 │ autorização │ escolher entre alternativa A que   │ há 12 min │ ↗ │" {
		t.Errorf("1ª linha: %q", l1)
	}
	if !strings.Contains(l2, "reescreve o importador") || !strings.Contains(l2, "… │") {
		t.Errorf("2ª linha: %q", l2)
	}
	if tela.Linhas[n-3].Pane != "wB:p4" || tela.Linhas[n-2].Pane != "wB:p4" || tela.Linhas[n-1].Pane != "" {
		t.Errorf("alvos: %q %q %q", tela.Linhas[n-3].Pane, tela.Linhas[n-2].Pane, tela.Linhas[n-1].Pane)
	}
	if got := duasLinhas("curto", 20); len(got) != 1 || got[0] != "curto" {
		t.Errorf("curto: %q", got)
	}
	// A quebra é no espaço, não no hífen de uma opção.
	if got := duasLinhas("apagar o recurso --cascade=orphan depois", 22); got[0] != "apagar o recurso" || got[1] != "--cascade=orphan depo…" {
		t.Errorf("hífen: %q", got)
	}
	if got := duasLinhas("abcdefghij", 4); fmt.Sprint(got) != "[abcd efg…]" {
		t.Errorf("palavra longa: %q", got)
	}
}

func TestEstilosEOSC8(t *testing.T) {
	tela := desenhar(Opcoes{Coord: "convey", Largura: 100})
	tudo := ""
	for _, l := range tela.Linhas {
		tudo += l.Texto + "\n"
	}
	vermelho, verde, cinza, laranja := "38;2;224;108;117", "38;2;152;195;121", "38;2;127;132;142", "38;2;255;140;0"
	amarelo, azul := "38;2;229;192;123", "38;2;97;175;239"
	casos := []struct{ nome, quer string }{
		{"draft vermelho com link", "\x1b]8;;https://gitlab.basis.com.br/basis/sgo-cli/-/merge_requests/33\x1b\\\x1b[" + vermelho + "m!33"},
		{"pronta verde", "\x1b[" + verde + "m!32"},
		{"mesclada cinza tachado", "merge_requests/29\x1b\\\x1b[" + cinza + ";9m!"},
		{"IaC laranja", "\x1b[" + laranja + "m[IaC]"},
		{"fechada roxo itálico", "\x1b[3;38;2;198;120;221m!31"},
		{"#ref com link do Taiga", "\x1b]8;;https://agile.basis.com.br/project/sgo-cli/us/313\x1b\\#313\x1b]8;;\x1b\\"},
		{"status com link de pane", "\x1b]8;;https://orq.local/pane/wB:p4\x1b\\"},
		{"blocked em negrito vermelho", "\x1b[1;" + vermelho + "m⚠"},
		{"status blocked em negrito vermelho", "\x1b[1;" + vermelho + "mExec r1 ↗"},
		{"Rev azul", "\x1b[" + azul + "mRev r2 ↗"},
		{"Done verde", "\x1b[" + verde + "mDone ↗"},
		{"Wait cinza", "\x1b[" + cinza + "mWait"},
		{"pendência ↗ foca a origem", "\x1b]8;;https://orq.local/pane/wB:p4\x1b\\↗\x1b]8;;\x1b\\"},
		{"tipo autorização amarelo", "\x1b[" + amarelo + "mautorização"},
		{"nome do coordenador leva ao pane dele", "\x1b]8;;https://orq.local/pane/wB:p1\x1b\\\x1b[1mconvey"},
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
	// Plan roxo e Exec amarelo (sem blocked).
	s := snapshot()
	s.Coordenadores[0].Ondas[0].US[1].Herdr = herdr("working")
	s.Coordenadores[0].Ondas[0].US[2].Status = "plan"
	tudo = ""
	for _, l := range cores.Desenhar(s, Opcoes{Coord: "convey", Largura: 100, Agora: agora}).Linhas {
		tudo += l.Texto
	}
	for nome, quer := range map[string]string{"Plan roxo": "\x1b[38;2;198;120;221mPlan", "Exec amarelo": "\x1b[" + amarelo + "mExec r1 ↗"} {
		if !strings.Contains(tudo, quer) {
			t.Errorf("%s: falta %q", nome, quer)
		}
	}
}

// Os escapes OSC 8 e SGR não contam na largura: todas as linhas da tabela têm a mesma largura
// visível, e as bordas verticais caem nas mesmas colunas com e sem links.
func TestOSC8NaoAfetaALargura(t *testing.T) {
	tela := desenhar(Opcoes{Coord: "convey", Largura: 100})
	var w0 int
	for i := 2; i <= 11; i++ {
		l := tela.Linhas[i].Texto
		if !strings.Contains(l, "\x1b]8;;") && i >= 5 && tela.Linhas[i].Pane != "" && i != 6 && i != 10 {
			t.Errorf("linha %d sem link: %q", i, l)
		}
		w := ansi.StringWidth(l)
		if i == 2 {
			w0 = w
		}
		if w != w0 || w != ansi.StringWidth(ansi.Strip(l)) {
			t.Errorf("linha %d com largura %d (plana %d), quer %d", i, w, ansi.StringWidth(ansi.Strip(l)), w0)
		}
	}
	// As bordas verticais caem nas mesmas colunas em todas as linhas, com ou sem link.
	bordas := func(l string) []int {
		var cs []int
		col := 0
		for _, r := range ansi.Strip(l) {
			if r == '│' || r == '┼' || r == '┬' {
				cs = append(cs, col)
			}
			col += ansi.StringWidth(string(r))
		}
		return cs
	}
	ref := fmt.Sprint(bordas(tela.Linhas[3].Texto))
	for i := 5; i <= 10; i++ {
		if b := fmt.Sprint(bordas(tela.Linhas[i].Texto)); b != ref {
			t.Errorf("linha %d com bordas em %s, quer %s", i, b, ref)
		}
	}
}

// Fundos: faixa na largura toda, linhas alternadas, a blocked em destaque, a selecionada com o
// fundo de seleção em todas as linhas físicas e a Done esmaecida. Claro e escuro têm tons
// próprios.
func TestFundosETom(t *testing.T) {
	bg := func(c string) string { return "48;2;" + c + "m" }
	for _, caso := range []struct {
		e                                Estilos
		faixa, zebra, selecao, bloqueada string
	}{
		{Estilos{}, bg("50;56;68"), bg("39;43;51"), bg("62;74;94"), bg("74;42;48")},
		{Estilos{Claro: true}, bg("227;231;238"), bg("242;243;245"), bg("207;216;230"), bg("251;225;227")},
	} {
		tela := caso.e.Desenhar(snapshot(), Opcoes{Coord: "convey", Largura: 100, Agora: agora, Selecao: "us:sgo-cli/225"})
		l := func(i int) string { return tela.Linhas[i].Texto }
		if !strings.HasPrefix(l(0), "\x1b["+caso.faixa) || !strings.HasPrefix(l(1), "\x1b["+caso.faixa) {
			t.Errorf("claro=%v faixa: %q", caso.e.Claro, l(0))
		}
		if !strings.Contains(l(5), caso.bloqueada) || !strings.Contains(l(6), caso.bloqueada) {
			t.Errorf("claro=%v blocked sem destaque: %q", caso.e.Claro, l(5))
		}
		if !strings.Contains(l(7), caso.zebra) || strings.Contains(l(8), "48;2;") {
			t.Errorf("claro=%v zebra: %q / %q", caso.e.Claro, l(7), l(8))
		}
		// A #225 (Done, selecionada): as duas linhas com o fundo de seleção e esmaecidas.
		for _, i := range []int{9, 10} {
			if !strings.Contains(l(i), caso.selecao) || !strings.Contains(l(i), "\x1b[2;") {
				t.Errorf("claro=%v linha %d sem seleção ou esmaecido: %q", caso.e.Claro, i, l(i))
			}
		}
		// O fundo volta depois de cada trecho colorido: nenhum reset deixa a célula sem fundo.
		cel := strings.Split(l(9), "│")[3]
		if strings.Contains(cel, "\x1b[m") && !strings.Contains(cel, "\x1b[m\x1b[") {
			t.Errorf("reset sem fundo: %q", cel)
		}
	}
	// Bordas não têm fundo.
	if strings.Contains(desenhar(Opcoes{Coord: "convey", Largura: 100}).Linhas[2].Texto, "48;2;") {
		t.Error("borda com fundo")
	}
}

func TestErroDeFonteDiscretoMantemValor(t *testing.T) {
	s := snapshot()
	u := &s.Coordenadores[0].Ondas[0].US[0]
	u.Taiga.Erro = &central.Erro{Codigo: "tempo_esgotado", Mensagem: "taiga não respondeu"}
	u.MRs[0].Erro = &central.Erro{Codigo: "falha_comando", Mensagem: "glab saiu com 1"}
	tela := cores.Desenhar(s, Opcoes{Coord: "convey", Largura: 100, Agora: agora})
	ls := planas(tela)
	if !strings.Contains(ls[7], "sgo jql: consulta avulsa") || !strings.Contains(ls[7], "…* │ Rev r2 ↗") || !strings.Contains(ls[7], "!32*") {
		t.Errorf("linha com erro: %q", ls[7])
	}
	rod := ansi.Strip(tela.Rodape)
	for _, quer := range []string{"herdr agora", "taiga há 2 min ✗ autenticacao", "* última consulta falhou"} {
		if !strings.Contains(rod, quer) {
			t.Errorf("rodapé sem %q: %q", quer, rod)
		}
	}
}

func TestLegenda(t *testing.T) {
	leg := make([]string, 0, 7)
	for _, l := range desenhar(Opcoes{Coord: "convey", Largura: 100}).Legenda {
		leg = append(leg, ansi.Strip(l))
	}
	quer := "● trabalhando ⚠ bloqueado · ocioso ✓ concluiu ○ desconhecido ✗ pane fechado – sem pane"
	if strings.Join(leg, " ") != quer {
		t.Errorf("legenda: %q", strings.Join(leg, " "))
	}
}

func TestTodos(t *testing.T) {
	tela := desenhar(Opcoes{Todos: true, Largura: 100})
	ls := planas(tela)
	if ls[0] != "Central · 2 coordenadores · 1 pendência aberta" {
		t.Errorf("cabeçalho: %q", ls[0])
	}
	tudo := strings.Join(ls, "\n")
	if !strings.Contains(tudo, " convey · sgo-cli ") || !strings.Contains(tudo, "liven — sem onda aberta") {
		t.Errorf("blocos:\n%s", tudo)
	}
	// O coordenador sem onda é uma linha discreta, sem faixa.
	ult := tela.Linhas[len(tela.Linhas)-1].Texto
	if strings.Contains(ult, "48;2;") || !strings.Contains(ult, "orq.local/pane/w7:p1") {
		t.Errorf("sem onda: %q", ult)
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
	// ↓ pula a faixa e as bordas e seleciona a US blocked; Enter foca.
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
	// A seleção é o fundo da linha, nas duas linhas físicas da #313.
	ls := strings.Split(m.View().Content, "\n")
	if !strings.HasPrefix(ansi.Strip(ls[5]), "│ ⚠ │ #313") || !strings.Contains(ls[5], "48;2;62;74;94m") || !strings.Contains(ls[6], "48;2;62;74;94m") {
		t.Fatalf("seleção: %q", ls[5])
	}
	// Clique na 2ª linha da #312... na linha da #312 (a 8ª: faixa, bordas, #313 e a 2ª MR
	// antes): pânico vira erro.
	_, cmd = m.Update(tea.MouseClickMsg{X: 3, Y: 7, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("clique sem foco")
	}
	m.Update(cmd())
	if !strings.Contains(ansi.Strip(m.View().Content), "foco em wB:p3 falhou: pânico: herdr sumiu") {
		t.Fatalf("pânico não virou erro:\n%s", ansi.Strip(m.View().Content))
	}
	// Clique em linha sem pane não faz nada: US sem pane, borda e faixa.
	for _, y := range []int{8, 2, 0} {
		if _, cmd = m.Update(tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft}); cmd != nil {
			t.Fatalf("clique na linha %d (sem pane) focou", y)
		}
	}
	// Clique na 2ª linha de MR da #313 foca o pane dela.
	if _, cmd = m.Update(tea.MouseClickMsg{X: 90, Y: 6, Button: tea.MouseLeft}); cmd == nil {
		t.Fatal("clique na 2ª MR não focou")
	}
	m.Update(cmd())
	if focos[len(focos)-1] != "wB:p4" {
		t.Fatalf("focos = %v", focos)
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
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // #312, a primeira sem ninguém blocked
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

func modeloCarregado(t *testing.T, largura int) *Modelo {
	t.Helper()
	m := Novo(context.Background(), Config{
		Carregar: func(context.Context) (central.Snapshot, error) { return snapshot(), nil },
		Focar:    func(context.Context, string) error { return nil },
		Opcoes:   Opcoes{Coord: "convey"},
		Estilos:  cores,
		Agora:    func() time.Time { return agora },
	})
	m.Update(tea.WindowSizeMsg{Width: largura, Height: 40})
	m.Update(m.carregar())
	return m
}

// ↑↓ andam de item em item: pulam faixa, bordas, regras, a US sem pane e as outras linhas
// físicas da US selecionada.
func TestNavegacaoPorItem(t *testing.T) {
	m := modeloCarregado(t, 100)
	var vistos []string
	for range 6 {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		vistos = append(vistos, m.tela.Linhas[m.sel].Item)
	}
	quer := "[us:sgo-cli/313 us:sgo-cli/312 us:sgo-cli/225 pend:convey/TG-313#P1 pend:convey/TG-313#P1 pend:convey/TG-313#P1]"
	if fmt.Sprint(vistos) != quer {
		t.Errorf("↓: %v", vistos)
	}
	vistos = nil
	for range 4 {
		m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		vistos = append(vistos, m.tela.Linhas[m.sel].Item)
	}
	// ↑ volta pelos mesmos itens e para na #313, a primeira com pane.
	if fmt.Sprint(vistos) != "[us:sgo-cli/225 us:sgo-cli/312 us:sgo-cli/313 us:sgo-cli/313]" {
		t.Errorf("↑: %v", vistos)
	}
	// A pendência e a US #313 têm o mesmo pane, mas só a selecionada ganha o fundo.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // pendência
	sel := "48;2;62;74;94m"
	for i, l := range m.tela.Linhas {
		if strings.Contains(l.Texto, sel) != (l.Item == "pend:convey/TG-313#P1") {
			t.Errorf("linha %d (%s) com seleção errada", i, l.Item)
		}
	}
}

// A cor de fundo chega como mensagem e troca os tons; antes dela vale o escuro. Init não espera
// a resposta: só devolve comandos.
func TestCorDeFundoDoTerminal(t *testing.T) {
	m := modeloCarregado(t, 100)
	if m.Init() == nil {
		t.Fatal("Init sem comandos")
	}
	escuro, claro := "48;2;50;56;68m", "48;2;227;231;238m"
	if !strings.Contains(m.tela.Linhas[0].Texto, escuro) {
		t.Fatalf("padrão não é o escuro: %q", m.tela.Linhas[0].Texto)
	}
	m.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 0xfa, G: 0xfa, B: 0xfa, A: 0xff}})
	if !m.cfg.Estilos.Claro || !strings.Contains(m.tela.Linhas[0].Texto, claro) {
		t.Fatalf("fundo claro não trocou os tons: %q", m.tela.Linhas[0].Texto)
	}
	m.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}})
	if m.cfg.Estilos.Claro || !strings.Contains(m.tela.Linhas[0].Texto, escuro) {
		t.Fatal("fundo escuro não voltou")
	}
}

// Rodapé: num pane largo, fontes, legenda e teclas; no estreito a legenda cede primeiro, um
// ícone por vez, e as teclas continuam.
func TestRodapeComLegenda(t *testing.T) {
	ultima := func(m *Modelo) string {
		ls := strings.Split(m.View().Content, "\n")
		return ansi.Strip(ls[len(ls)-1])
	}
	largo := ultima(modeloCarregado(t, 200))
	for _, quer := range []string{"herdr agora", "● trabalhando", "– sem pane", "q sai"} {
		if !strings.Contains(largo, quer) {
			t.Errorf("rodapé largo sem %q: %q", quer, largo)
		}
	}
	medio := ultima(modeloCarregado(t, 140))
	if !strings.Contains(medio, "● trabalhando") || strings.Contains(medio, "sem pane") || !strings.HasSuffix(medio, "q sai") {
		t.Errorf("rodapé médio: %q", medio)
	}
	estreito := ultima(modeloCarregado(t, 80))
	if strings.Contains(estreito, "trabalhando") || !strings.Contains(estreito, "herdr agora") {
		t.Errorf("rodapé estreito: %q", estreito)
	}
	for _, w := range []int{200, 140, 80} {
		if l := ultima(modeloCarregado(t, w)); ansi.StringWidth(l) > w {
			t.Errorf("rodapé de %d com %d colunas", w, ansi.StringWidth(l))
		}
	}
}
