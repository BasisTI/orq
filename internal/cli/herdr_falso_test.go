package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/BasisTI/orq/internal/herdrfalso"
)

type (
	estadoFalso = herdrfalso.Estado
	noFalso     = herdrfalso.No
	paneFalso   = herdrfalso.Pane
	retFalso    = herdrfalso.Ret
)

const areaAltura = herdrfalso.AreaAltura

var (
	binFalso     string
	binFalsoOnce sync.Once
	binFalsoErr  error
	binFalsoDir  string
)

// compilarHerdrFalso compila o herdr falso uma vez por execução dos testes: reexecutar o binário
// de teste a cada chamada custa segundos sob -race.
func compilarHerdrFalso(t *testing.T) string {
	t.Helper()
	binFalsoOnce.Do(func() {
		dir, err := os.MkdirTemp("", "herdrfalso")
		if err != nil {
			binFalsoErr = err
			return
		}
		binFalsoDir = dir
		binFalso = filepath.Join(dir, "herdrfalso")
		out, err := exec.Command("go", "build", "-o", binFalso, "github.com/BasisTI/orq/internal/herdrfalso/cmd/herdrfalso").CombinedOutput()
		if err != nil {
			binFalsoErr = errString(err.Error() + ": " + string(out))
		}
	})
	if binFalsoErr != nil {
		t.Fatalf("compilar o herdr falso: %v", binFalsoErr)
	}
	return binFalso
}

type errString string

func (e errString) Error() string { return string(e) }

// herdrGrade é o herdr falso de um teste.
type herdrGrade struct {
	t   *testing.T
	dir string
	env map[string]string
}

func novoHerdrGrade(t *testing.T) *herdrGrade {
	t.Helper()
	dir := t.TempDir()
	fake := compilarHerdrFalso(t)
	bin := filepath.Join(dir, "herdr")
	script := "#!/bin/sh\nORQ_TEST_HERDR_DIR='" + dir + "' exec '" + fake + "' \"$@\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := ambiente(t)
	env["ORQ_HERDR_BIN"] = bin
	env["HOME"] = dir
	h := &herdrGrade{t: t, dir: dir, env: env}
	// Um workspace de outro projeto, que a Central não pode tocar.
	e := h.estado()
	e.Workspaces["w1"] = "outro"
	e.Abas["w1:t1"] = &noFalso{Pane: "w1:p1"}
	e.AbaWS["w1:t1"] = "w1"
	e.Panes["w1:p1"] = &paneFalso{Rotulo: "meu pane"}
	h.salvar(e)
	return h
}

func (h *herdrGrade) estado() *estadoFalso {
	h.t.Helper()
	e, err := herdrfalso.Ler(h.dir)
	if err != nil {
		h.t.Fatal(err)
	}
	return e
}

func (h *herdrGrade) salvar(e *estadoFalso) {
	h.t.Helper()
	if err := e.Salvar(h.dir); err != nil {
		h.t.Fatal(err)
	}
}

// log devolve as chamadas ao herdr, uma por linha, com os argumentos separados por |.
func (h *herdrGrade) log() []string { return chamadas(h.t, h.dir) }

func (h *herdrGrade) limparLog() { escrever(h.t, filepath.Join(h.dir, "log"), "") }

// fechar simula o usuário fechando o pane (sem passar pelo orq).
func (h *herdrGrade) fechar(pane string) {
	e := h.estado()
	aba := e.AbaDe(pane)
	e.Abas[aba] = herdrfalso.Retirar(e.Abas[aba], pane)
	delete(e.Panes, pane)
	h.salvar(e)
}

// forma descreve a árvore da aba da Central como D(...) para divisão para baixo e R(...) para a
// direita, com os ids dos panes nas folhas.
func (h *herdrGrade) forma(aba string) string {
	e := h.estado()
	var f func(n *noFalso) string
	f = func(n *noFalso) string {
		if n.Pane != "" {
			return n.Pane[strings.Index(n.Pane, ":")+1:]
		}
		l := "R"
		if n.Direcao == "down" {
			l = "D"
		}
		return l + "(" + f(n.A) + "," + f(n.B) + ")"
	}
	if e.Abas[aba] == nil {
		return "<sem aba>"
	}
	return f(e.Abas[aba])
}

// retangulos devolve a posição de cada pane da aba.
func (h *herdrGrade) retangulos(aba string) map[string]retFalso {
	e := h.estado()
	panes := map[string]retFalso{}
	if e.Abas[aba] == nil {
		return panes
	}
	var splits []map[string]any
	e.Rects(e.Abas[aba], retFalso{W: herdrfalso.AreaLargura, H: herdrfalso.AreaAltura}, panes, &splits)
	return panes
}

// leitura devolve os panes da aba na ordem de leitura (linha, coluna).
func (h *herdrGrade) leitura(aba string) []string {
	rs := h.retangulos(aba)
	var ids []string
	for id := range rs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool {
		ra, rb := rs[ids[a]], rs[ids[b]]
		if ra.Y != rb.Y {
			return ra.Y < rb.Y
		}
		return ra.X < rb.X
	})
	return ids
}
