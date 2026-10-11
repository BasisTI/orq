package enriquecer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// falsos monta herdr, glab, gh e taiga falsos num diretório temporário. Cada um grava o argv
// em log/<nome> e responde com resp/<nome>/<chave>.out (stdout, saída 0) ou .err (stderr,
// saída 1), onde a chave é o argv juntado por "_"; sem arquivo, sai 0 sem nada.
type falsos struct {
	t   *testing.T
	dir string
	env map[string]string
}

const scriptFalso = `#!/bin/sh
d='%DIR%'; t='%NOME%'
printf '%s\037' "$@" >> "$d/log/$t"; echo >> "$d/log/$t"
key=$(printf '%s_' "$@" | LC_ALL=C tr -c 'A-Za-z0-9_.-' '_')
if [ -f "$d/resp/$t/$key.err" ]; then cat "$d/resp/$t/$key.err" >&2; exit $(cat "$d/resp/$t/$key.code" 2>/dev/null || echo 1); fi
if [ -f "$d/resp/$t/$key.out" ]; then cat "$d/resp/$t/$key.out"; exit 0; fi
exit 0
`

func novosFalsos(t *testing.T) *falsos {
	t.Helper()
	dir := t.TempDir()
	f := &falsos{t: t, dir: dir, env: map[string]string{"HOME": dir}}
	for _, n := range []string{"herdr", "glab", "gh", "taiga"} {
		for _, sub := range []string{"log", "resp/" + n, "bin"} {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		s := strings.NewReplacer("%DIR%", dir, "%NOME%", n).Replace(scriptFalso)
		p := filepath.Join(dir, "bin", n)
		if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
		f.env[VariavelBin[n]] = p
	}
	return f
}

var naoChave = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

func chaveArgs(args ...string) string {
	var b strings.Builder
	for _, a := range args {
		b.WriteString(a + "_")
	}
	return naoChave.ReplaceAllString(b.String(), "_")
}

// responder grava a resposta da ferramenta para o argv dado; erro vai para o stderr com saída 1.
func (f *falsos) responder(nome, saida string, erro bool, args ...string) {
	f.t.Helper()
	base := filepath.Join(f.dir, "resp", nome, chaveArgs(args...))
	_ = os.Remove(base + ".out")
	_ = os.Remove(base + ".err")
	_ = os.Remove(base + ".code")
	ext := ".out"
	if erro {
		ext = ".err"
	}
	if err := os.WriteFile(base+ext, []byte(saida), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// responderCodigo é o responder de erro com código de saída próprio.
func (f *falsos) responderCodigo(nome, saida string, codigo int, args ...string) {
	f.t.Helper()
	f.responder(nome, saida, true, args...)
	base := filepath.Join(f.dir, "resp", nome, chaveArgs(args...))
	if err := os.WriteFile(base+".code", []byte(strconv.Itoa(codigo)), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// chamadas devolve o argv de cada chamada à ferramenta, na ordem, e esvazia o log.
func (f *falsos) chamadas(nome string) [][]string {
	f.t.Helper()
	p := filepath.Join(f.dir, "log", nome)
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	_ = os.Remove(p)
	var out [][]string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		out = append(out, strings.Split(strings.TrimSuffix(l, "\x1f"), "\x1f"))
	}
	return out
}

func (f *falsos) ferramentas() Ferramentas {
	return Ferramentas{Env: func(k string) string { return f.env[k] }, Timeout: 10 * time.Second}
}

// sessao escreve a resposta de `herdr api snapshot`.
func (f *falsos) sessao(panes []map[string]any, workspaces []map[string]any) {
	f.t.Helper()
	var agentes []map[string]any
	for _, p := range panes {
		if _, ok := p["agent"]; ok {
			agentes = append(agentes, p)
		}
	}
	b, _ := json.Marshal(map[string]any{"id": "cli:api:snapshot", "result": map[string]any{"type": "session_snapshot", "snapshot": map[string]any{
		"panes": panes, "agents": agentes, "workspaces": workspaces}}})
	f.responder("herdr", string(b), false, "api", "snapshot")
}

// relogio é um relógio de teste compartilhado pelo store e pelo enriquecedor.
type relogio struct{ t time.Time }

func (r *relogio) agora() time.Time { return r.t }
func (r *relogio) passar(d time.Duration) {
	r.t = r.t.Add(d)
}

type cenario struct {
	*falsos
	st  *central.Store
	e   *Enriquecedor
	rel *relogio
}

func novoCenario(t *testing.T) *cenario {
	t.Helper()
	f := novosFalsos(t)
	st, err := central.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rel := &relogio{t: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}
	st.Now = rel.agora
	return &cenario{falsos: f, st: st, rel: rel, e: &Enriquecedor{St: st, F: f.ferramentas(), Now: rel.agora}}
}

func (c *cenario) coord(nome, pane, workspace string) central.Coordenador {
	c.t.Helper()
	co, err := c.st.RegistrarCoord(context.Background(), central.Registro{Nome: nome, Pane: pane, Workspace: workspace, Projeto: "orq"})
	if err != nil {
		c.t.Fatal(err)
	}
	return co
}

func (c *cenario) onda(co central.Coordenador, us ...string) {
	c.t.Helper()
	n := central.NovaOnda{Titulo: "onda"}
	for _, u := range us {
		r, err := refs.ParseUS(u)
		if err != nil {
			c.t.Fatal(err)
		}
		n.US = append(n.US, r)
	}
	if _, err := c.st.CriarOnda(context.Background(), co, n); err != nil {
		c.t.Fatal(err)
	}
}

func (c *cenario) status(co central.Coordenador, u, status, pane string) {
	c.t.Helper()
	r, _ := refs.ParseUS(u)
	m := central.MudancaStatus{Status: status}
	if pane != "" {
		m.Pane = &pane
	}
	if _, _, err := c.st.AtualizarStatus(context.Background(), &co, r, m); err != nil {
		c.t.Fatal(err)
	}
}

func (c *cenario) mr(co central.Coordenador, u, url string) {
	c.t.Helper()
	r, _ := refs.ParseUS(u)
	if _, _, err := c.st.AdicionarMR(context.Background(), &co, r, url); err != nil {
		c.t.Fatal(err)
	}
}

func (c *cenario) snapshot() central.Snapshot {
	c.t.Helper()
	s, err := c.st.Snapshot(context.Background(), false)
	if err != nil {
		c.t.Fatal(err)
	}
	return s
}

func (c *cenario) atualizar(op Opcoes) Resultado {
	c.t.Helper()
	r, err := c.e.Atualizar(context.Background(), op)
	if err != nil {
		c.t.Fatal(err)
	}
	return r
}
