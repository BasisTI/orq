package upgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BasisTI/orq/internal/enriquecer"
)

// Rótulos dos binários conhecidos.
const (
	RotuloPATH      = "PATH"
	RotuloGoInstall = "go install"
	RotuloPlugin    = "plugin"
	RotuloDaemon    = "daemon"
)

// Binario é um executável do orq que o upgrade conhece, ou o processo do daemon.
type Binario struct {
	Rotulos []string `json:"rotulos"`
	Caminho string   `json:"caminho"`
	Versao  string   `json:"versao,omitempty"`
	Commit  string   `json:"commit,omitempty"`
	Schema  int      `json:"schema,omitempty"`
	// PID só no daemon.
	PID  string `json:"pid,omitempty"`
	Erro string `json:"erro,omitempty"`

	real string
}

type listaBinarios []*Binario

// tem diz se algum binário da lista é o arquivo real dado.
func (l listaBinarios) tem(real string) bool {
	for _, b := range l {
		if b.real == real {
			return true
		}
	}
	return false
}

func (b *Binario) tem(r string) bool {
	for _, x := range b.Rotulos {
		if x == r {
			return true
		}
	}
	return false
}

// Ambiente reúne o que o upgrade precisa para achar as ferramentas e os binários.
type Ambiente struct {
	Env func(string) string
	// Dir é o diretório de estado (onde ficam o banco e a trava do daemon).
	Dir string
}

func (a Ambiente) ferramentas() enriquecer.Ferramentas {
	return enriquecer.Ferramentas{Env: a.Env, Timeout: 20 * time.Second}
}

// pluginInstalado pergunta ao Herdr onde o plugin orq está instalado. Vazio quando o Herdr não
// responde ou o plugin não está instalado; o erro diz qual dos casos.
func (a Ambiente) pluginRaiz(ctx context.Context) (string, error) {
	out, err := a.ferramentas().Rodar(ctx, "herdr", "plugin", "list", "--json")
	if err != nil {
		return "", err
	}
	var r struct {
		Result struct {
			Plugins []struct {
				ID   string `json:"plugin_id"`
				Root string `json:"plugin_root"`
			} `json:"plugins"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &r); err != nil {
		return "", errors.New("herdr plugin list: resposta inválida")
	}
	for _, p := range r.Result.Plugins {
		if p.ID == "orq" && p.Root != "" {
			return p.Root, nil
		}
	}
	return "", errNaoInstalado
}

var errNaoInstalado = errors.New("plugin orq não instalado no Herdr")

// binDoGo é o diretório em que `go install` põe o orq: $GOBIN ou $GOPATH/bin.
func (a Ambiente) binDoGo(ctx context.Context) string {
	if out, err := a.ferramentas().Rodar(ctx, "go", "env", "GOBIN", "GOPATH"); err == nil {
		l := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(l) > 0 && strings.TrimSpace(l[0]) != "" {
			return strings.TrimSpace(l[0])
		}
		if len(l) > 1 && strings.TrimSpace(l[1]) != "" {
			return filepath.Join(filepath.SplitList(strings.TrimSpace(l[1]))[0], "bin")
		}
	}
	if d := a.Env("GOBIN"); d != "" {
		return d
	}
	if d := a.Env("GOPATH"); d != "" {
		return filepath.Join(filepath.SplitList(d)[0], "bin")
	}
	if h := a.Env("HOME"); h != "" {
		return filepath.Join(h, "go", "bin")
	}
	return ""
}

func executavel(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0
}

func resolver(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// doPATH acha os `orq` do PATH (e de ~/.local/bin, que o PATH de um pane pode não ter).
func (a Ambiente) doPATH() []string {
	var dirs []string
	dirs = append(dirs, filepath.SplitList(a.Env("PATH"))...)
	if h := a.Env("HOME"); h != "" {
		dirs = append(dirs, filepath.Join(h, ".local", "bin"))
	}
	var out []string
	for _, d := range dirs {
		if d == "" || !filepath.IsAbs(d) {
			continue
		}
		if p := filepath.Join(d, "orq"); executavel(p) {
			out = append(out, p)
		}
	}
	return out
}

// Descobrir lista os binários distintos do orq: o do go install (quando existe e não é o mesmo
// arquivo que o do plugin), o do plugin e os demais do PATH. Dois caminhos que resolvem para o
// mesmo arquivo viram um binário só, com os dois rótulos.
func (a Ambiente) Descobrir(ctx context.Context) []*Binario {
	return a.descobrir(ctx)
}

func (a Ambiente) descobrir(ctx context.Context) listaBinarios {
	var lista listaBinarios
	add := func(rotulo, caminho string) {
		r := resolver(caminho)
		for _, b := range lista {
			if b.real == r {
				if !b.tem(rotulo) {
					b.Rotulos = append(b.Rotulos, rotulo)
				}
				return
			}
		}
		lista = append(lista, &Binario{Rotulos: []string{rotulo}, Caminho: caminho, real: r})
	}
	if raiz, err := a.pluginRaiz(ctx); err == nil {
		if p := filepath.Join(raiz, "bin", "orq"); executavel(p) {
			add(RotuloPlugin, p)
		}
	}
	if d := a.binDoGo(ctx); d != "" {
		// Link para o binário do plugin é o mesmo arquivo, que o plugin já atualiza: não é um
		// binário do go install.
		if p := filepath.Join(d, "orq"); executavel(p) && !lista.tem(resolver(p)) {
			add(RotuloGoInstall, p)
		}
	}
	for _, p := range a.doPATH() {
		add(RotuloPATH, p)
	}
	return lista
}

// Versoes consulta a versão de cada binário, em paralelo, e preenche os campos.
func Versoes(ctx context.Context, bs []*Binario) {
	var wg sync.WaitGroup
	for _, b := range bs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.consultar(ctx)
		}()
	}
	wg.Wait()
}

func (b *Binario) consultar(ctx context.Context) {
	i, err := Consultar(ctx, b.Caminho)
	b.Versao, b.Commit, b.Schema, b.Erro = i.Versao, i.Commit, i.Schema, ""
	if err != nil {
		b.Erro = err.Error()
	}
}

// Conhecidos devolve os binários do orq e o processo do daemon, já com as versões.
func (a Ambiente) Conhecidos(ctx context.Context) []*Binario {
	bs := a.Descobrir(ctx)
	if d := a.Daemon(); d != nil {
		bs = append(bs, d)
	}
	Versoes(ctx, bs)
	return bs
}

// rodarEm roda bin com args, com stdout e stderr juntos, e devolve a saída.
func rodarEm(ctx context.Context, dir string, env []string, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	if env != nil {
		cmd.Env = env
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}
