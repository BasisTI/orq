// Package falsotest compila o herdr falso e monta um por teste, para os pacotes que precisam de
// um herdr com agentes (o Avisador de pendências, o daemon). Só os testes o importam.
package falsotest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/BasisTI/orq/internal/herdrfalso"
)

var (
	once sync.Once
	bin  string
	dir  string
	erro error
)

// Limpar apaga o binário compilado; chame no fim do TestMain do pacote.
func Limpar() {
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

func compilar(t *testing.T) string {
	t.Helper()
	once.Do(func() {
		d, err := os.MkdirTemp("", "herdrfalso")
		if err != nil {
			erro = err
			return
		}
		dir = d
		bin = filepath.Join(d, "herdrfalso")
		out, err := exec.Command("go", "build", "-o", bin, "github.com/BasisTI/orq/internal/herdrfalso/cmd/herdrfalso").CombinedOutput()
		if err != nil {
			erro = &compilacao{err.Error() + ": " + string(out)}
		}
	})
	if erro != nil {
		t.Fatalf("compilar o herdr falso: %v", erro)
	}
	return bin
}

type compilacao struct{ msg string }

func (c *compilacao) Error() string { return c.msg }

// Falso é um herdr falso de um teste, com estado e log num diretório temporário.
type Falso struct {
	t *testing.T
	// Dir guarda o estado e o log; Bin é o executável para ORQ_HERDR_BIN.
	Dir, Bin string
}

// Novo cria o herdr falso do teste.
func Novo(t *testing.T) *Falso {
	t.Helper()
	fake := compilar(t)
	d := t.TempDir()
	b := filepath.Join(d, "herdr")
	script := "#!/bin/sh\nORQ_TEST_HERDR_DIR='" + d + "' exec '" + fake + "' \"$@\"\n"
	if err := os.WriteFile(b, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Falso{t: t, Dir: d, Bin: b}
}

// Agente põe (ou troca) um agente no pane, com o status dado.
func (f *Falso) Agente(pane, nome, status string) {
	f.t.Helper()
	e, err := herdrfalso.Ler(f.Dir)
	if err != nil {
		f.t.Fatal(err)
	}
	p := e.Panes[pane]
	if p == nil {
		p = &herdrfalso.Pane{}
		e.Panes[pane] = p
	}
	p.Nome, p.Status = nome, status
	if err := e.Salvar(f.Dir); err != nil {
		f.t.Fatal(err)
	}
}

// AgenteFixo é Agente, mas o status não muda quando o agente recebe um prompt.
func (f *Falso) AgenteFixo(pane, nome, status string) {
	f.t.Helper()
	f.Agente(pane, nome, status)
	e, err := herdrfalso.Ler(f.Dir)
	if err != nil {
		f.t.Fatal(err)
	}
	e.Panes[pane].Fixo = true
	if err := e.Salvar(f.Dir); err != nil {
		f.t.Fatal(err)
	}
}

// Remover tira o agente do pane (o coordenador some).
func (f *Falso) Remover(pane string) {
	f.t.Helper()
	e, err := herdrfalso.Ler(f.Dir)
	if err != nil {
		f.t.Fatal(err)
	}
	delete(e.Panes, pane)
	if err := e.Salvar(f.Dir); err != nil {
		f.t.Fatal(err)
	}
}

// Prompts devolve os textos entregues ao agente do pane, na ordem.
func (f *Falso) Prompts(pane string) []string {
	f.t.Helper()
	e, err := herdrfalso.Ler(f.Dir)
	if err != nil {
		f.t.Fatal(err)
	}
	if p := e.Panes[pane]; p != nil {
		return p.Prompts
	}
	return nil
}

// Chamadas devolve o log: um argv por linha, com os argumentos separados por |.
func (f *Falso) Chamadas() []string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.Dir, "log"))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}
