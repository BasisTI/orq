package cli

import (
	"encoding/json"
	"fmt"
	"github.com/BasisTI/orq/internal/herdrfalso"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const nomeCentral = "Central teste"

var nomesCoord = []string{"a", "b", "c", "d", "e"}

// coordenadores registra n coordenadores, cada um com uma onda aberta (a US i+1).
func coordenadores(t *testing.T, env map[string]string, de, ate int) {
	t.Helper()
	for i := de; i < ate; i++ {
		c := nomesCoord[i]
		ok(t, env, "coord", "registrar", c, "--pane", "wB:p"+c, "--projeto", "orq")
		ok(t, env, "--coord", c, "onda", "criar", "o"+c, "--us", fmt.Sprint(i+1))
	}
}

func abrir(t *testing.T, env map[string]string) ResultadoCentral {
	t.Helper()
	var r ResultadoCentral
	if err := json.Unmarshal([]byte(ok(t, env, "central", "abrir", "--nome", nomeCentral, "--sem-foco")), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// escritas devolve as chamadas ao herdr que mudam alguma coisa.
func escritas(h *herdrGrade) []string {
	var out []string
	for _, l := range h.log() {
		for _, w := range []string{"pane|split|", "pane|move|", "pane|resize|", "pane|close|", "pane|run|", "pane|rename|", "workspace|create|"} {
			if strings.HasPrefix(l, w) {
				out = append(out, l)
			}
		}
	}
	return out
}

// alturasIguais confere que as linhas da grade têm a mesma altura (a menos de arredondamento).
func alturasIguais(t *testing.T, h *herdrGrade, aba string, linhas int) {
	t.Helper()
	rs := h.retangulos(aba)
	alt := map[int]int{} // y → altura
	for _, r := range rs {
		alt[r.Y] = r.H
	}
	if len(alt) != linhas {
		t.Fatalf("linhas = %d, quer %d: %v", len(alt), linhas, rs)
	}
	min, max := areaAltura, 0
	for _, a := range alt {
		if a < min {
			min = a
		}
		if a > max {
			max = a
		}
	}
	if max-min > 1 {
		t.Fatalf("alturas desiguais: %v", alt)
	}
}

// Os casos da story: 1 a 5 panes, criados um a um e de uma vez, com a árvore de divisões
// esperada, a ordem de leitura igual à de criação e as linhas com a mesma altura.
func TestCentralGrade1a5Panes(t *testing.T) {
	formas := []string{
		"p1",
		"R(p1,p2)",
		"D(R(p1,p2),p3)",
		"D(R(p1,p2),R(p3,p4))",
		"D(R(p1,p2),D(R(p3,p4),p5))",
	}
	for _, umAUm := range []bool{true, false} {
		t.Run(fmt.Sprintf("um_a_um=%v", umAUm), func(t *testing.T) {
			h := novoHerdrGrade(t)
			if umAUm {
				for n := 1; n <= 5; n++ {
					coordenadores(t, h.env, n-1, n)
					antes := h.leitura("wT:t1")
					r := abrir(t, h.env)
					conferirGrade(t, h, r, n, formas[n-1])
					// Os panes que já existiam não mudam de lugar nem de processo.
					if depois := h.leitura("wT:t1"); n > 1 && strings.Join(depois[:n-1], ",") != strings.Join(antes, ",") {
						t.Fatalf("n=%d: panes mudaram de lugar: %v -> %v", n, antes, depois)
					}
					// De novo: nada é escrito.
					h.limparLog()
					abrir(t, h.env)
					if w := escritas(h); len(w) != 0 {
						t.Fatalf("n=%d: segunda chamada escreveu: %q", n, w)
					}
				}
				return
			}
			for n := 1; n <= 5; n++ {
				h := novoHerdrGrade(t)
				coordenadores(t, h.env, 0, n)
				conferirGrade(t, h, abrir(t, h.env), n, formas[n-1])
			}
		})
	}
}

func conferirGrade(t *testing.T, h *herdrGrade, r ResultadoCentral, n int, forma string) {
	t.Helper()
	if got := h.forma("wT:t1"); got != forma {
		t.Fatalf("n=%d: árvore %s, quer %s", n, got, forma)
	}
	var quer []string
	for i := 1; i <= n; i++ {
		quer = append(quer, fmt.Sprintf("wT:p%d", i))
	}
	if got := h.leitura("wT:t1"); strings.Join(got, ",") != strings.Join(quer, ",") {
		t.Fatalf("n=%d: ordem %v, quer %v", n, got, quer)
	}
	alturasIguais(t, h, "wT:t1", (n+1)/2)
	e := h.estado()
	for i, c := range nomesCoord[:n] {
		p := e.Panes[fmt.Sprintf("wT:p%d", i+1)]
		if p == nil || p.Rotulo != RotuloPaneCentral+c || len(p.Argv) < 4 || p.Argv[1] != "tui" || p.Argv[3] != c {
			t.Fatalf("n=%d: pane %d = %+v", n, i+1, p)
		}
	}
	if len(r.Panes) != n {
		t.Fatalf("n=%d: r = %+v", n, r)
	}
	// Todo pane move dentro da aba diz o alvo; sem --target-pane o herdr divide o pane focado.
	for _, l := range h.log() {
		if strings.HasPrefix(l, "pane|move|") && strings.Contains(l, "--tab|") && !strings.Contains(l, "--target-pane|") {
			t.Fatalf("pane move sem alvo: %s", l)
		}
	}
	if len(h.estado().Abas) != 2 { // a da Central e a do outro workspace: nenhuma aba temporária sobra
		t.Fatalf("n=%d: abas = %v", n, h.estado().Abas)
	}
}

// Fechar o pane do meio (o coordenador ficou sem onda): os seguintes sobem uma posição, sem
// buraco, sem relançar o TUI de ninguém.
func TestCentralGradeFechaPaneDoMeio(t *testing.T) {
	casos := []struct {
		fecha int // 1-based
		forma string
		ordem string
	}{
		{1, "D(R(p2,p3),R(p4,p5))", "p2,p3,p4,p5"},
		{2, "D(R(p1,p3),R(p4,p5))", "p1,p3,p4,p5"},
		{3, "D(R(p1,p2),R(p4,p5))", "p1,p2,p4,p5"},
		{4, "D(R(p1,p2),R(p3,p5))", "p1,p2,p3,p5"},
		{5, "D(R(p1,p2),R(p3,p4))", "p1,p2,p3,p4"},
	}
	for _, c := range casos {
		t.Run(fmt.Sprintf("fecha_%d", c.fecha), func(t *testing.T) {
			h := novoHerdrGrade(t)
			coordenadores(t, h.env, 0, 5)
			abrir(t, h.env)
			// Um pane de usuário em outro workspace e um sem rótulo na Central não entram na conta.
			h.limparLog()
			ok(t, h.env, "onda", "encerrar", fmt.Sprint(c.fecha))
			r := abrir(t, h.env)
			if got := h.forma("wT:t1"); got != c.forma {
				t.Fatalf("árvore %s, quer %s", got, c.forma)
			}
			var ordem []string
			for _, id := range h.leitura("wT:t1") {
				ordem = append(ordem, strings.TrimPrefix(id, "wT:"))
			}
			if strings.Join(ordem, ",") != c.ordem {
				t.Fatalf("ordem %v, quer %s", ordem, c.ordem)
			}
			alturasIguais(t, h, "wT:t1", 2)
			fechado := nomesCoord[c.fecha-1]
			var achou bool
			for _, p := range r.Panes {
				if p.Coordenador == fechado {
					achou = p.Acao == "fechado"
				} else if p.Acao != "mantido" {
					t.Errorf("pane %+v, quer mantido", p)
				}
			}
			if !achou {
				t.Fatalf("r = %+v", r)
			}
			// Os TUIs seguem onde estavam: ninguém recebeu comando.
			for _, l := range h.log() {
				if strings.HasPrefix(l, "pane|run|") || strings.HasPrefix(l, "pane|split|") {
					t.Fatalf("mexeu em processo: %s", l)
				}
			}
			for id, p := range h.estado().Panes {
				if strings.HasPrefix(id, "wT:") && len(p.Argv) == 0 {
					t.Errorf("pane %s perdeu o TUI", id)
				}
			}
			if len(h.estado().Abas) != 2 {
				t.Fatalf("abas = %v", h.estado().Abas)
			}
		})
	}
}

// Fechar dois panes de uma vez, e o pane que o usuário fechou à mão (o coordenador segue com
// onda): a grade se refaz e o pane perdido é recriado no fim.
func TestCentralGradePaneFechadoAMao(t *testing.T) {
	h := novoHerdrGrade(t)
	coordenadores(t, h.env, 0, 4)
	abrir(t, h.env)
	h.fechar("wT:p2")
	r := abrir(t, h.env)
	if got := h.forma("wT:t1"); got != "D(R(p1,p3),R(p4,p5))" {
		t.Fatalf("árvore %s", got)
	}
	if r.Panes[1] != (PaneCentral{"b", "wT:p5", "criado", nil}) {
		t.Fatalf("r = %+v", r)
	}
	alturasIguais(t, h, "wT:t1", 2)
}

// Quem arrumou a Central à mão em colunas ganha a grade por linha, sem reiniciar os TUIs.
func TestCentralGradeRefazColunas(t *testing.T) {
	h := novoHerdrGrade(t)
	coordenadores(t, h.env, 0, 4)
	abrir(t, h.env)
	e := h.estado()
	e.Abas["wT:t1"] = &noFalso{Direcao: "right", Ratio: 0.5,
		A: &noFalso{Direcao: "down", Ratio: 0.5, A: &noFalso{Pane: "wT:p1"}, B: &noFalso{Pane: "wT:p3"}},
		B: &noFalso{Direcao: "down", Ratio: 0.5, A: &noFalso{Pane: "wT:p2"}, B: &noFalso{Pane: "wT:p4"}}}
	h.salvar(e)
	h.limparLog()
	abrir(t, h.env)
	if got := h.forma("wT:t1"); got != "D(R(p1,p2),R(p3,p4))" {
		t.Fatalf("árvore %s", got)
	}
	for _, l := range h.log() {
		if strings.HasPrefix(l, "pane|run|") {
			t.Fatalf("relançou: %s", l)
		}
	}
	// A grade certa (duas colunas) não é mexida: segunda chamada sem escrita.
	h.limparLog()
	abrir(t, h.env)
	if w := escritas(h); len(w) != 0 {
		t.Fatalf("escreveu: %q", w)
	}
}

// Pane sem o rótulo orq · não é tocado nem contado.
func TestCentralGradePaneDeFora(t *testing.T) {
	h := novoHerdrGrade(t)
	coordenadores(t, h.env, 0, 2)
	abrir(t, h.env)
	e := h.estado()
	e.Panes["wT:p9"] = &paneFalso{Rotulo: "meu shell"}
	herdrfalso.Dividir(e.Abas["wT:t1"], "wT:p2", "wT:p9", "down", 0.5)
	h.salvar(e)
	coordenadores(t, h.env, 2, 3)
	h.limparLog()
	abrir(t, h.env)
	for _, l := range h.log() {
		if strings.Contains(l, "wT:p9") && !strings.HasPrefix(l, "pane|list") {
			t.Fatalf("tocou no pane de fora: %s", l)
		}
	}
	if p := h.estado().Panes["wT:p9"]; p == nil || p.Rotulo != "meu shell" {
		t.Fatalf("pane de fora = %+v", p)
	}
	// O coordenador do pane de fora na conta: a, b, c ficam em ordem de leitura.
	var ordem []string
	for _, id := range h.leitura("wT:t1") {
		if id != "wT:p9" {
			ordem = append(ordem, id)
		}
	}
	if len(ordem) != 3 {
		t.Fatalf("panes = %v", ordem)
	}
	// Idempotente com o pane de fora.
	h.limparLog()
	abrir(t, h.env)
	if w := escritas(h); len(w) != 0 {
		t.Fatalf("segunda chamada escreveu: %q", w)
	}
}

func TestCentralAbrirIdempotente(t *testing.T) {
	h := novoHerdrGrade(t)
	env := h.env
	for _, c := range []string{"a", "b", "c"} {
		ok(t, env, "coord", "registrar", c, "--pane", "wB:p"+c, "--projeto", "orq")
	}
	ok(t, env, "--coord", "a", "onda", "criar", "oa", "--us", "1")
	ok(t, env, "--coord", "b", "onda", "criar", "ob", "--us", "2")

	r := abrir(t, env)
	if !r.Criado || r.WorkspaceID == nil || *r.WorkspaceID != "wT" || r.Focado ||
		len(r.Panes) != 2 || r.Panes[0] != (PaneCentral{"a", "wT:p1", "criado", nil}) || r.Panes[1] != (PaneCentral{"b", "wT:p2", "criado", nil}) {
		t.Fatalf("r = %+v", r)
	}
	log := strings.Join(h.log(), "\n")
	exe, _ := os.Executable()
	for _, quer := range []string{
		"workspace|create|--label|Central teste|--no-focus|--env|ORQ_STATE_DIR=" + env["ORQ_STATE_DIR"] + "|--env|ORQ_HERDR_BIN=" + env["ORQ_HERDR_BIN"] + "|--cwd|" + h.dir + "|",
		"pane|rename|wT:p1|orq · a|",
		"pane|run|wT:p1|'" + exe + "' 'tui' '--coord' 'a'|",
		"pane|split|wT:p1|--direction|right|--no-focus|--env|ORQ_STATE_DIR=",
		"pane|rename|wT:p2|orq · b|",
	} {
		if !strings.Contains(log, quer) {
			t.Errorf("falta %q em:\n%s", quer, log)
		}
	}
	if strings.Contains(log, "workspace|focus") {
		t.Error("focou com --sem-foco")
	}

	// Segunda chamada: b fica, a perdeu a onda e o pane dele fecha, c ganhou onda; o pane do
	// usuário não é tocado.
	ok(t, env, "onda", "encerrar", "1")
	ok(t, env, "--coord", "c", "onda", "criar", "oc", "--us", "3")
	e := h.estado()
	e.Panes["wT:p9"] = &paneFalso{Rotulo: "meu shell"}
	herdrfalso.Dividir(e.Abas["wT:t1"], "wT:p2", "wT:p9", "right", 0.5)
	h.salvar(e)
	h.limparLog()
	r = ResultadoCentral{}
	if err := json.Unmarshal([]byte(ok(t, env, "central", "abrir", "--nome", nomeCentral)), &r); err != nil {
		t.Fatal(err)
	}
	quer := []PaneCentral{{"b", "wT:p2", "mantido", nil}, {"c", "wT:p3", "criado", nil}, {"a", "wT:p1", "fechado", nil}}
	if r.Criado || !r.Focado || len(r.Panes) != 3 {
		t.Fatalf("r = %+v", r)
	}
	for i := range quer {
		if r.Panes[i] != quer[i] {
			t.Errorf("pane %d = %+v, quer %+v", i, r.Panes[i], quer[i])
		}
	}
	log = strings.Join(h.log(), "\n")
	for _, quer := range []string{"pane|split|wT:p2|--direction|right|", "pane|close|wT:p1|", "workspace|focus|wT|"} {
		if !strings.Contains(log, quer) {
			t.Errorf("falta %q em:\n%s", quer, log)
		}
	}
	if strings.Contains(log, "wT:p9") && strings.Contains(log, "pane|close|wT:p9") || strings.Contains(log, "workspace|create") {
		t.Errorf("tocou no pane do usuário ou recriou o workspace:\n%s", log)
	}
}

// S1 da revisão r1: o TUI saiu com q (ou caiu) e o pane voltou ao shell; abrir de novo relança
// o TUI no mesmo pane. Pane com outro processo não recebe comando.
func TestCentralAbrirRelancaTUI(t *testing.T) {
	h := novoHerdrGrade(t)
	env := h.env
	coordenadores(t, env, 0, 3)
	abrir(t, env)
	exe, _ := os.Executable()
	// a: TUI vivo; b: voltou ao shell; c: o usuário rodou vim ali.
	e := h.estado()
	e.Panes["wT:p2"].Argv = nil
	e.Panes["wT:p3"].Argv = []string{"vim", "notas.md"}
	h.salvar(e)
	h.limparLog()
	r := abrir(t, env)
	if len(r.Panes) != 3 || r.Panes[0] != (PaneCentral{"a", "wT:p1", "mantido", nil}) || r.Panes[1] != (PaneCentral{"b", "wT:p2", "relancado", nil}) ||
		r.Panes[2].Acao != "ocupado" || r.Panes[2].Detalhe == nil || !strings.Contains(*r.Panes[2].Detalhe, "vim notas.md") {
		t.Fatalf("r = %+v", r)
	}
	log := strings.Join(h.log(), "\n")
	if strings.Count(log, "pane|run|") != 1 || !strings.Contains(log, "pane|run|wT:p2|'"+exe+"' 'tui' '--coord' 'b'|") || strings.Contains(log, "split") {
		t.Fatalf("run só no pane do shell, sem pane novo:\n%s", log)
	}
	// De novo: o b agora tem o TUI, e nada é relançado.
	h.limparLog()
	r = abrir(t, env)
	if r.Panes[1] != (PaneCentral{"b", "wT:p2", "mantido", nil}) || strings.Contains(strings.Join(h.log(), "\n"), "pane|run|") {
		t.Fatalf("segunda: %+v", r)
	}
	// O pane ocupado de um coordenador sem onda não é fechado.
	ok(t, env, "onda", "encerrar", "3")
	r = abrir(t, env)
	if last := r.Panes[len(r.Panes)-1]; last.Coordenador != "c" || last.Acao != "ocupado" || strings.Contains(strings.Join(h.log(), "\n"), "pane|close|wT:p3") {
		t.Fatalf("ocupado sem onda: %+v", r)
	}
}

// Lançamento que falha (o TUI não sobe): a ação diz no detalhe, e a próxima chamada relança.
func TestCentralAbrirTUINaoSobe(t *testing.T) {
	h := novoHerdrGrade(t)
	antes := esperaTUI
	esperaTUI = 300 * time.Millisecond
	t.Cleanup(func() { esperaTUI = antes })
	coordenadores(t, h.env, 0, 1)
	e := h.estado()
	e.TUINaoSobe = true
	h.salvar(e)
	r := abrir(t, h.env)
	if r.Panes[0].Acao != "criado" || r.Panes[0].Detalhe == nil || !strings.Contains(*r.Panes[0].Detalhe, "não apareceu") {
		t.Fatalf("r = %+v", r)
	}
	e = h.estado()
	e.TUINaoSobe = false
	h.salvar(e)
	r = abrir(t, h.env)
	if r.Panes[0] != (PaneCentral{"a", "wT:p1", "relancado", nil}) {
		t.Fatalf("relançado: %+v", r.Panes[0])
	}
}

func TestGradeCerta(t *testing.T) {
	r := func(x, y, w, h int) retangulo { return retangulo{x, y, w, h} }
	casos := []struct {
		nome     string
		rs       []retangulo
		completo bool
		quer     bool
	}{
		{"um", []retangulo{r(0, 0, 200, 120)}, true, true},
		{"dois", []retangulo{r(0, 0, 100, 120), r(100, 0, 100, 120)}, true, true},
		{"três", []retangulo{r(0, 0, 100, 60), r(100, 0, 100, 60), r(0, 60, 200, 60)}, true, true},
		{"três com o terceiro estreito", []retangulo{r(0, 0, 100, 60), r(100, 0, 100, 60), r(0, 60, 100, 60)}, true, false},
		{"três, prefixo de uma grade maior", []retangulo{r(0, 0, 100, 60), r(100, 0, 100, 60), r(0, 60, 100, 60)}, false, true},
		{"o segundo embaixo do primeiro", []retangulo{r(0, 0, 200, 60), r(0, 60, 100, 60), r(100, 60, 100, 60)}, true, false},
		{"quatro", []retangulo{r(0, 0, 100, 60), r(100, 0, 100, 60), r(0, 60, 100, 60), r(100, 60, 100, 60)}, true, true},
		{"quatro com a segunda linha deslocada", []retangulo{r(0, 0, 100, 60), r(100, 0, 100, 60), r(0, 60, 60, 60), r(60, 60, 140, 60)}, true, false},
	}
	for _, c := range casos {
		if got := gradeCerta(c.rs, c.completo); got != c.quer {
			t.Errorf("%s: %v, quer %v", c.nome, got, c.quer)
		}
	}
}

// O estado do fake de outro teste não vaza: cada herdrGrade tem o seu diretório.
func TestHerdrGradeIsolado(t *testing.T) {
	a, b := novoHerdrGrade(t), novoHerdrGrade(t)
	if a.dir == b.dir {
		t.Fatal("mesmo diretório")
	}
	if _, err := os.Stat(filepath.Join(a.dir, "estado.json")); err != nil {
		t.Fatal(err)
	}
}
