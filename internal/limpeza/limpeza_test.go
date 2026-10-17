package limpeza

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/refs"
)

// Ferramentas falsas: herdr, gh e docker gravam o argv em log/<nome> e respondem com
// resp/<nome>/<chave>.out (stdout) ou .err (stderr, saída 1), onde a chave é o argv juntado por
// "_". O git é o de verdade, em repositórios temporários. Nenhum recurso real é tocado.
const scriptFalso = `#!/bin/sh
d='%DIR%'; t='%NOME%'
printf '%s\037' "$@" >> "$d/log/$t"; echo >> "$d/log/$t"
key=$(printf '%s_' "$@" | LC_ALL=C tr -c 'A-Za-z0-9_.-' '_')
if [ -f "$d/resp/$t/$key.err" ]; then cat "$d/resp/$t/$key.err" >&2; exit 1; fi
if [ -f "$d/resp/$t/$key.out" ]; then cat "$d/resp/$t/$key.out"; exit 0; fi
exit 0
`

var naoChave = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

func chave(args ...string) string { return naoChave.ReplaceAllString(strings.Join(args, "_")+"_", "_") }

type cen struct {
	t      *testing.T
	dir    string
	st     *central.Store
	l      *Limpador
	u      refs.US
	url    string
	origin string
	repo   string
	wt     string
	tip    string
	env    map[string]string
	coord  central.Coordenador
	ondaID int64
	// extras são panes do Herdr que não pertencem à US (outro board).
	extras []map[string]any
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v em %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func novo(t *testing.T) *cen {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	dir := t.TempDir()
	c := &cen{t: t, dir: dir, u: refs.US{Projeto: "orq", Ref: 13}, url: "https://github.com/BasisTI/orq/pull/9", env: map[string]string{"HOME": dir}}
	for _, n := range []string{"herdr", "gh", "docker"} {
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
		c.env[enriquecer.VariavelBin[n]] = p
	}
	// Repositórios: origin (bare), repo (checkout principal) e o worktree da US.
	c.origin = filepath.Join(dir, "origin.git")
	c.repo = filepath.Join(dir, "repos", "orq")
	c.wt = filepath.Join(dir, "repos", "orq.TG-13")
	git(t, dir, "init", "-q", "--bare", "-b", "main", c.origin)
	git(t, dir, "init", "-q", "-b", "main", c.repo)
	git(t, c.repo, "remote", "add", "origin", c.origin)
	escrever(t, filepath.Join(c.repo, "a.txt"), "a\n")
	git(t, c.repo, "add", ".")
	git(t, c.repo, "commit", "-q", "-m", "base")
	git(t, c.repo, "push", "-q", "-u", "origin", "main")
	git(t, c.repo, "worktree", "add", "-q", "-b", "TG-13", c.wt)
	escrever(t, filepath.Join(c.wt, "b.txt"), "b\n")
	git(t, c.wt, "add", ".")
	git(t, c.wt, "commit", "-q", "-m", "feature")
	git(t, c.wt, "push", "-q", "-u", "origin", "TG-13")
	c.tip = git(t, c.wt, "rev-parse", "HEAD")

	st, err := central.Open(context.Background(), filepath.Join(dir, "estado"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	c.st = st
	c.coord, err = st.RegistrarCoord(context.Background(), central.Registro{Nome: "c", Pane: "wB:p1", Projeto: "orq"})
	if err != nil {
		t.Fatal(err)
	}
	o, err := st.CriarOnda(context.Background(), c.coord, central.NovaOnda{Titulo: "onda", Ate: "merge", US: []refs.US{c.u}})
	if err != nil {
		t.Fatal(err)
	}
	c.ondaID = o.ID
	if _, _, err := st.AdicionarMR(context.Background(), &c.coord, c.u, c.url); err != nil {
		t.Fatal(err)
	}
	raizes := Raizes{Trabalho: filepath.Join(dir, "trabalho"), Tmp: filepath.Join(dir, "tmp")}
	c.l = &Limpador{St: st, F: enriquecer.Ferramentas{Env: func(k string) string { return c.env[k] }, Timeout: 20 * time.Second}, Raizes: raizes, PaneProprio: "wA:p99", Cwd: dir}
	rec := &Recursos{Repos: []Repo{{Repo: c.repo, Worktree: c.wt, Branch: "TG-13"}}, Panes: []string{"tg-13_executor"}, Aprovados: map[string]string{c.url: c.tip}}
	if _, err := rec.Gravar(raizes, c.u); err != nil {
		t.Fatal(err)
	}
	escrever(t, filepath.Join(raizes.PastaTrabalho(c.u), "pacote.md"), "x")
	escrever(t, filepath.Join(raizes.PastaTmp(c.u), "lixo", "f"), "x")
	c.mr("closed", true, c.tip)
	c.herdr(map[string]string{"wA:p2": "idle"})
	return c
}

func escrever(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (c *cen) responder(nome, saida string, args ...string) {
	c.t.Helper()
	p := filepath.Join(c.dir, "resp", nome, chave(args...)+".out")
	if err := os.WriteFile(p, []byte(saida), 0o644); err != nil {
		c.t.Fatal(err)
	}
}

// mr responde a consulta da MR na forja: mesclada (state closed + merged) ou aberta.
func (c *cen) mr(state string, merged bool, head string) {
	c.t.Helper()
	b, _ := json.Marshal(map[string]any{"state": state, "merged": merged, "draft": false, "mergeable_state": "clean", "html_url": c.url,
		"head": map[string]string{"sha": head, "ref": "TG-13"}, "base": map[string]string{"ref": "main"}})
	c.responder("gh", string(b), "api", "repos/BasisTI/orq/pulls/9")
}

// herdr responde o snapshot: o pane do coordenador e os panes dados (id → status), todos
// chamados tg-13_executor, mais um pane de outra US que nunca pode ser fechado.
func (c *cen) herdr(panes map[string]string) {
	c.t.Helper()
	ps := []map[string]any{
		{"pane_id": "wB:p1", "workspace_id": "wB", "agent": "claude", "agent_status": "idle", "name": "orq-coordenador"},
		{"pane_id": "wA:p7", "workspace_id": "wA", "agent": "claude", "agent_status": "idle", "name": "tg-130_executor"},
	}
	ps = append(ps, c.extras...)
	ids := make([]string, 0, len(panes))
	for id := range panes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ps = append(ps, map[string]any{"pane_id": id, "workspace_id": "wA", "agent": "claude", "agent_status": panes[id], "name": "tg-13_executor"})
	}
	b, _ := json.Marshal(map[string]any{"result": map[string]any{"type": "session_snapshot", "snapshot": map[string]any{"panes": ps, "agents": ps, "workspaces": []any{}}}})
	c.responder("herdr", string(b), "api", "snapshot")
}

// docker responde a listagem de cada tipo (contêiner, rede, volume, imagem) para o rótulo.
func (c *cen) docker(chaveRotulo, valor string, ids ...string) {
	c.t.Helper()
	for i, rd := range recursosDocker {
		args := append(append([]string(nil), rd.listar...), "--filter", "label="+chaveRotulo+"="+valor)
		c.responder("docker", ids[i]+"\n", args...)
		if ids[i] != "" {
			id := strings.Fields(ids[i])[0]
			c.responder("docker", valor+"\n", rd.inspect, "inspect", "--format", fmt.Sprintf(rd.rotulo, chaveRotulo), id)
		}
	}
}

// dockerIds é o de uma US com um recurso de cada tipo.
func (c *cen) dockerUS() {
	c.docker("orq.us", "orq/13", "abcdef123456 orq-13-pg", "112233445566 orq-13_default", "orq-13_dados", "sha256:aabbccddeeff00112233")
}

func (c *cen) chamadas(nome string) [][]string {
	c.t.Helper()
	p := filepath.Join(c.dir, "log", nome)
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		c.t.Fatal(err)
	}
	_ = os.Remove(p)
	var out [][]string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		out = append(out, strings.Split(strings.TrimSuffix(l, "\x1f"), "\x1f"))
	}
	return out
}

// escritas filtra as chamadas que mudam algo (fechar pane, remover no Docker).
func (c *cen) escritas(nome string) []string {
	var out []string
	for _, ch := range c.chamadas(nome) {
		j := strings.Join(ch, " ")
		switch nome {
		case "herdr":
			if strings.HasPrefix(j, "pane close") {
				out = append(out, j)
			}
		case "docker":
			if !strings.Contains(j, " ls") && !strings.Contains(j, "inspect") && !strings.HasPrefix(j, "ps") && !strings.Contains(j, "--filter") {
				out = append(out, j)
			}
		}
	}
	return out
}

// mergeNaMain integra a branch em main no origin, por merge ou por squash, e apaga a remota.
func (c *cen) mergeNaMain(squash, apagarRemota bool) {
	c.t.Helper()
	if squash {
		git(c.t, c.repo, "merge", "-q", "--squash", "TG-13")
		git(c.t, c.repo, "commit", "-q", "-m", "squash")
	} else {
		git(c.t, c.repo, "merge", "-q", "--no-ff", "-m", "merge", "TG-13")
	}
	git(c.t, c.repo, "push", "-q", "origin", "main")
	if apagarRemota {
		git(c.t, c.repo, "push", "-q", "origin", "--delete", "TG-13")
	}
}

// aprovar regrava o head aprovado da MR no recursos.json.
func (c *cen) aprovar(sha string) {
	c.t.Helper()
	rec, err := LerRecursos(c.l.Raizes.Arquivo(c.u), c.u)
	if err != nil {
		c.t.Fatal(err)
	}
	if sha == "" {
		rec.Aprovados = nil
	} else {
		rec.Aprovados = map[string]string{c.url: sha}
	}
	if _, err := rec.Gravar(c.l.Raizes, c.u); err != nil {
		c.t.Fatal(err)
	}
}

func existe(p string) bool { _, err := os.Lstat(p); return err == nil }

func (c *cen) limpar(dry bool) Resultado {
	c.t.Helper()
	r, err := c.l.Limpar(context.Background(), &c.coord, c.u, Opcoes{DryRun: dry})
	if err != nil {
		c.t.Fatal(err)
	}
	return r
}

func estados(r Resultado) map[string]string {
	m := map[string]string{}
	for _, i := range r.Itens {
		m[i.Tipo+" "+i.Alvo] = i.Estado
	}
	return m
}

func (c *cen) estadoDe(r Resultado, tipo string) []*Item {
	var out []*Item
	for _, i := range r.Itens {
		if i.Tipo == tipo {
			out = append(out, i)
		}
	}
	return out
}

func (c *cen) tipos() []string {
	es, err := c.st.Diario(context.Background(), nil, central.FiltroDiario{US: &c.u})
	if err != nil {
		c.t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Tipo+" "+string(e.Detalhe))
	}
	return out
}

func (c *cen) trabalho() string { return c.l.Raizes.PastaTrabalho(c.u) }
func (c *cen) tmp() string      { return c.l.Raizes.PastaTmp(c.u) }

func TestLimparTudoNoMerge(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.dockerUS()
	r := c.limpar(false)
	if r.Recusa != "" || !r.Completa || !r.Agiu {
		t.Fatalf("resultado = %+v %s", r, estados(r))
	}
	for _, i := range r.Itens {
		if i.Estado != Removido {
			t.Errorf("%s %s: %s %s", i.Tipo, i.Alvo, i.Estado, i.Motivo)
		}
	}
	for _, p := range []string{c.wt, c.tmp(), c.trabalho()} {
		if existe(p) {
			t.Errorf("%s ainda existe", p)
		}
	}
	if git(t, c.repo, "branch", "--list", "TG-13") != "" {
		t.Error("branch local ficou")
	}
	if git(t, c.repo, "ls-remote", "origin", "refs/heads/TG-13") != "" {
		t.Error("branch remota ficou")
	}
	if got := c.escritas("herdr"); len(got) != 1 || got[0] != "pane close wA:p2" {
		t.Errorf("herdr = %v", got)
	}
	want := []string{"rm -f -v abcdef123456", "network rm 112233445566", "volume rm orq-13_dados", "rmi sha256:aabbccddeeff00112233"}
	if got := c.escritas("docker"); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("docker = %v", got)
	}
	tipos := strings.Join(c.tipos(), "\n")
	for _, w := range []string{"limpar.removido", `"tipo":"worktree"`, `"tipo":"docker_volume"`} {
		if !strings.Contains(tipos, w) {
			t.Errorf("diário sem %s:\n%s", w, tipos)
		}
	}
}

func TestLimparSquashConferePorConteudo(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(true, true)
	// O head da forja não é o da branch local: a conferência cai no conteúdo.
	c.aprovar(strings.Repeat("0", 40))
	c.mr("closed", true, strings.Repeat("0", 40))
	r := c.limpar(false)
	if r.Recusa != "" || !r.Completa {
		t.Fatalf("squash: %+v %s", r, estados(r))
	}
	if existe(c.wt) || git(t, c.repo, "branch", "--list", "TG-13") != "" {
		t.Error("worktree ou branch local ficou")
	}
}

func TestLimparSquashComMudancaDepoisNaMainPreserva(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(true, true)
	// Alguém mexeu no mesmo arquivo na main depois do squash: não dá para provar o conteúdo.
	escrever(t, filepath.Join(c.repo, "b.txt"), "b mudou\n")
	git(t, c.repo, "commit", "-q", "-am", "depois")
	git(t, c.repo, "push", "-q", "origin", "main")
	c.aprovar(strings.Repeat("0", 40))
	c.mr("closed", true, strings.Repeat("0", 40))
	r := c.limpar(false)
	if r.Completa || !existe(c.wt) {
		t.Fatalf("devia preservar: %s", estados(r))
	}
}

func TestLimparMRAbertaRecusa(t *testing.T) {
	c := novo(t)
	c.mr("open", false, c.tip)
	c.dockerUS()
	r := c.limpar(false)
	if !strings.Contains(r.Recusa, "MR não mesclada") || r.Agiu || len(r.Itens) != 0 {
		t.Fatalf("resultado = %+v", r)
	}
	if !existe(c.wt) || !existe(c.tmp()) || !existe(c.trabalho()) {
		t.Error("algo foi removido")
	}
	if len(c.escritas("herdr"))+len(c.escritas("docker")) != 0 {
		t.Error("escreveu no Herdr ou no Docker")
	}
	if !strings.Contains(strings.Join(c.tipos(), "\n"), "limpar.recusada") {
		t.Error("sem evento limpar.recusada")
	}
}

func TestLimparHeadDiferenteDoAprovado(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	rec, _ := LerRecursos(c.l.Raizes.Arquivo(c.u), c.u)
	rec.Aprovados = map[string]string{c.url: "1111111"}
	if _, err := rec.Gravar(c.l.Raizes, c.u); err != nil {
		t.Fatal(err)
	}
	r := c.limpar(false)
	if !strings.Contains(r.Recusa, "não é o aprovado") || existe(c.trabalho()) == false {
		t.Fatalf("resultado = %+v", r)
	}
	// Com o head aprovado certo (prefixo), segue.
	rec.Aprovados[c.url] = c.tip[:10]
	if _, err := rec.Gravar(c.l.Raizes, c.u); err != nil {
		t.Fatal(err)
	}
	if r := c.limpar(false); r.Recusa != "" || !r.Completa {
		t.Fatalf("com o aprovado certo: %+v", r)
	}
}

func TestLimparSemRecursosOuSemMR(t *testing.T) {
	c := novo(t)
	if err := os.Remove(c.l.Raizes.Arquivo(c.u)); err != nil {
		t.Fatal(err)
	}
	if r := c.limpar(false); !strings.Contains(r.Recusa, "sem recursos gravados") {
		t.Fatalf("sem recursos: %+v", r)
	}
}

func TestLimparArvoreSuja(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	escrever(t, filepath.Join(c.wt, "novo.txt"), "não commitado")
	r := c.limpar(false)
	wt := c.estadoDe(r, "worktree")[0]
	if wt.Estado != Preservado || !strings.Contains(wt.Motivo, "árvore suja") {
		t.Fatalf("worktree = %+v", wt)
	}
	if bl := c.estadoDe(r, "branch_local")[0]; bl.Estado != Preservado {
		t.Errorf("branch local = %+v", bl)
	}
	if tr := c.estadoDe(r, "pasta_trabalho")[0]; tr.Estado != Preservado || !existe(c.trabalho()) {
		t.Errorf("a pasta de trabalho some com recursos por limpar: %+v", tr)
	}
	// O que não depende do worktree segue: o TMPDIR e o pane.
	if existe(c.tmp()) || len(c.escritas("herdr")) != 1 {
		t.Error("o TMPDIR e o pane eram para sair")
	}
	if !existe(filepath.Join(c.wt, "novo.txt")) || r.Completa {
		t.Error("o worktree sujo foi tocado")
	}
	if !strings.Contains(strings.Join(c.tipos(), "\n"), "limpar.preservado") {
		t.Error("sem evento limpar.preservado")
	}
}

func TestLimparCommitNaoEnviado(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	escrever(t, filepath.Join(c.wt, "so-local.txt"), "só aqui")
	git(t, c.wt, "add", ".")
	git(t, c.wt, "commit", "-q", "-m", "local")
	r := c.limpar(false)
	wt := c.estadoDe(r, "worktree")[0]
	if wt.Estado != Preservado || !strings.Contains(wt.Motivo, "não enviado") {
		t.Fatalf("worktree = %+v", wt)
	}
	if bl := c.estadoDe(r, "branch_local")[0]; bl.Estado != Preservado || git(t, c.repo, "branch", "--list", "TG-13") == "" {
		t.Errorf("branch local = %+v", bl)
	}
	if !existe(filepath.Join(c.wt, "so-local.txt")) {
		t.Error("o commit local sumiu")
	}
}

func TestLimparBranchRemotaComCommitDepoisDoMerge(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	// Mais um commit na branch remota depois do merge: a remota não pode ser apagada.
	escrever(t, filepath.Join(c.wt, "depois.txt"), "d")
	git(t, c.wt, "add", ".")
	git(t, c.wt, "commit", "-q", "-m", "depois")
	git(t, c.wt, "push", "-q", "origin", "TG-13")
	r := c.limpar(false)
	if br := c.estadoDe(r, "branch_remota")[0]; br.Estado != Preservado {
		t.Fatalf("branch remota = %+v", br)
	}
	if git(t, c.repo, "ls-remote", "origin", "refs/heads/TG-13") == "" {
		t.Error("a remota foi apagada")
	}
}

func TestLimparAgenteWorking(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.herdr(map[string]string{"wA:p2": "working"})
	c.dockerUS()
	r := c.limpar(false)
	if r.Agiu || r.Completa {
		t.Fatalf("resultado = %s", estados(r))
	}
	p := c.estadoDe(r, "pane")[0]
	if p.Estado != Preservado || !strings.Contains(p.Motivo, "working") {
		t.Fatalf("pane = %+v", p)
	}
	for _, p := range []string{c.wt, c.tmp(), c.trabalho()} {
		if !existe(p) {
			t.Errorf("%s foi removido com o agente trabalhando", p)
		}
	}
	if len(c.escritas("herdr"))+len(c.escritas("docker")) != 0 {
		t.Error("escreveu no Herdr ou no Docker")
	}
	// Só os panes da US entram: o tg-130 e o do coordenador nunca aparecem como alvo de fechar.
	if n := len(c.estadoDe(r, "pane")); n != 1 {
		t.Errorf("%d panes no plano, queria 1", n)
	}
}

func TestLimparNuncaFechaOPaneProprioNemOCoordenador(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.l.PaneProprio = "wA:p2"
	r := c.limpar(false)
	if p := c.estadoDe(r, "pane")[0]; p.Estado != Preservado || !strings.Contains(p.Motivo, "roda este comando") {
		t.Fatalf("pane = %+v", p)
	}
	if len(c.escritas("herdr")) != 0 {
		t.Error("fechou o próprio pane")
	}
}

func TestLimparDockerDeOutraUS(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.dockerUS()
	// O contêiner listado carrega, na hora da conferência, o rótulo de outra US.
	rd := recursosDocker[0]
	c.responder("docker", "orq/99\n", rd.inspect, "inspect", "--format", fmt.Sprintf(rd.rotulo, "orq.us"), "abcdef123456")
	r := c.limpar(false)
	d := c.estadoDe(r, "docker_container")[0]
	if d.Estado != Preservado || !strings.Contains(d.Motivo, "orq/99") {
		t.Fatalf("contêiner = %+v", d)
	}
	for _, e := range c.escritas("docker") {
		if strings.Contains(e, "abcdef123456") {
			t.Errorf("removeu o contêiner de outra US: %s", e)
		}
	}
	if r.Completa || !existe(c.trabalho()) {
		t.Error("a pasta de trabalho some com recurso preservado")
	}
}

func TestLimparNuncaUsaPrune(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.dockerUS()
	c.limpar(false)
	for _, ch := range c.chamadas("docker") {
		if strings.Contains(strings.Join(ch, " "), "prune") {
			t.Fatalf("prune: %v", ch)
		}
	}
}

func TestLimparCaminhosForaDasRaizes(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	vitima := filepath.Join(c.dir, "vitima")
	escrever(t, filepath.Join(vitima, "importante"), "x")

	// 1. recursos.json declara uma pasta temporária fora da raiz.
	arq := c.l.Raizes.Arquivo(c.u)
	var rec map[string]any
	b, _ := os.ReadFile(arq)
	_ = json.Unmarshal(b, &rec)
	rec["tmp"] = vitima
	b, _ = json.Marshal(rec)
	if err := os.WriteFile(arq, b, 0o644); err != nil {
		t.Fatal(err)
	}
	r := c.limpar(false)
	if tmp := c.estadoDe(r, "pasta_tmp")[0]; tmp.Estado != Preservado || !strings.Contains(tmp.Motivo, "fora do caminho") {
		t.Fatalf("tmp declarado fora = %+v", tmp)
	}
	if !existe(filepath.Join(vitima, "importante")) {
		t.Fatal("apagou fora da raiz")
	}
}

func TestLimparSymlinks(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	vitima := filepath.Join(c.dir, "vitima")
	escrever(t, filepath.Join(vitima, "importante"), "x")

	// O TMPDIR da US é um link para fora.
	if err := os.RemoveAll(c.tmp()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vitima, c.tmp()); err != nil {
		t.Fatal(err)
	}
	r := c.limpar(false)
	if tmp := c.estadoDe(r, "pasta_tmp")[0]; tmp.Estado != Preservado || !strings.Contains(tmp.Motivo, "link simbólico") {
		t.Fatalf("tmp symlink = %+v", tmp)
	}
	if !existe(filepath.Join(vitima, "importante")) {
		t.Fatal("seguiu o link")
	}

	// O pai (<tmp>/<projeto>) é um link: o caminho resolve para fora de <raiz>/<projeto>/TG-ref.
	if err := os.Remove(c.tmp()); err != nil {
		t.Fatal(err)
	}
	pai := filepath.Dir(c.tmp())
	if err := os.RemoveAll(pai); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vitima, "TG-13"), 0o755); err != nil {
		t.Fatal(err)
	}
	escrever(t, filepath.Join(vitima, "TG-13", "dentro"), "x")
	if err := os.Symlink(vitima, pai); err != nil {
		t.Fatal(err)
	}
	r = c.limpar(false)
	if tmp := c.estadoDe(r, "pasta_tmp")[0]; tmp.Estado != Preservado || !strings.Contains(tmp.Motivo, "fora de") {
		t.Fatalf("tmp com pai symlink = %+v", tmp)
	}
	if !existe(filepath.Join(vitima, "TG-13", "dentro")) {
		t.Fatal("apagou através do link do pai")
	}
}

func TestPastaSeguraRecusaVazioRelativoERaizInvalida(t *testing.T) {
	u := refs.US{Projeto: "orq", Ref: 13}
	for _, c := range []struct{ raiz, caminho string }{{"", "/x/orq/TG-13"}, {"/tmp", ""}, {"rel", "rel/orq/TG-13"}, {"/tmp", "tmp/orq/TG-13"}} {
		if existe, motivo := pastaSegura(c.raiz, c.caminho, u); existe || motivo == "" {
			t.Errorf("%q %q: existe=%v motivo=%q", c.raiz, c.caminho, existe, motivo)
		}
	}
	for _, v := range []string{"/", "", "relativo"} {
		_, err := RaizesDe(func(k string) string {
			return map[string]string{"ORQ_TMP_DIR": v, "ORQ_TRABALHO_DIR": "/t", "HOME": "/h"}[k]
		})
		if v == "" {
			continue // vazio usa o padrão
		}
		if err == nil {
			t.Errorf("raiz %q devia ser recusada", v)
		}
	}
}

func TestLimparWorktreeComCwdDentro(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.l.Cwd = filepath.Join(c.wt)
	r := c.limpar(false)
	if wt := c.estadoDe(r, "worktree")[0]; wt.Estado != Preservado || !strings.Contains(wt.Motivo, "dentro do worktree") {
		t.Fatalf("worktree = %+v", wt)
	}
}

func TestLimparNaoRemoveOCheckoutPrincipal(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	rec, _ := LerRecursos(c.l.Raizes.Arquivo(c.u), c.u)
	rec.Repos[0].Worktree = c.repo
	if _, err := rec.Gravar(c.l.Raizes, c.u); err != nil {
		t.Fatal(err)
	}
	r := c.limpar(false)
	if wt := c.estadoDe(r, "worktree")[0]; wt.Estado != Preservado || !existe(c.repo) {
		t.Fatalf("worktree = %+v", wt)
	}
}

func TestDryRunListaExatamenteOQueSeriaRemovido(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(true, false)
	c.dockerUS()
	antes := c.tipos()
	dry := c.limpar(true)
	if !dry.DryRun || dry.Agiu {
		t.Fatalf("dry = %+v", dry)
	}
	for _, p := range []string{c.wt, c.tmp(), c.trabalho()} {
		if !existe(p) {
			t.Fatalf("o dry-run removeu %s", p)
		}
	}
	if len(c.escritas("herdr"))+len(c.escritas("docker")) != 0 {
		t.Fatal("o dry-run escreveu no Herdr ou no Docker")
	}
	if git(t, c.repo, "branch", "--list", "TG-13") == "" || git(t, c.repo, "ls-remote", "origin", "refs/heads/TG-13") == "" {
		t.Fatal("o dry-run apagou branch")
	}
	if len(c.tipos()) != len(antes) {
		t.Fatal("o dry-run gravou no diário")
	}
	plano := map[string]bool{}
	for _, i := range dry.Itens {
		if i.Estado == Remover {
			plano[i.Tipo+" "+i.Alvo] = true
		}
	}
	if len(plano) < 8 {
		t.Fatalf("plano pequeno: %v", plano)
	}
	real := c.limpar(false)
	feito := map[string]bool{}
	for _, i := range real.Itens {
		if i.Estado == Removido {
			feito[i.Tipo+" "+i.Alvo] = true
		}
	}
	if fmt.Sprint(sortado(plano)) != fmt.Sprint(sortado(feito)) {
		t.Fatalf("dry-run != real\n dry: %v\nreal: %v", sortado(plano), sortado(feito))
	}
}

func sortado(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestDryRunMostraPreservadoPorDependencia(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	escrever(t, filepath.Join(c.wt, "sujo"), "x")
	r := c.limpar(true)
	for _, i := range r.Itens {
		if i.Tipo == "pasta_trabalho" && i.Estado != Preservado {
			t.Errorf("o dry-run promete remover a pasta de trabalho com recursos preservados: %+v", i)
		}
	}
}

func TestRecursosValidar(t *testing.T) {
	u := refs.US{Projeto: "orq", Ref: 13}
	ok := func() *Recursos {
		return &Recursos{Versao: 1, Projeto: "orq", Ref: 13, Repos: []Repo{{Repo: "/r", Worktree: "/w", Branch: "TG-13"}}, Panes: []string{"tg-13_executor"}}
	}
	if err := ok().Validar(u); err != nil {
		t.Fatal(err)
	}
	for nome, mut := range map[string]func(*Recursos){
		"worktree relativo":  func(r *Recursos) { r.Repos[0].Worktree = "w" },
		"branch com ..":      func(r *Recursos) { r.Repos[0].Branch = "a..b" },
		"branch com opção":   func(r *Recursos) { r.Repos[0].Branch = "--delete" },
		"pane com espaço":    func(r *Recursos) { r.Panes = []string{"a b"} },
		"outra US":           func(r *Recursos) { r.Ref = 14 },
		"versão":             func(r *Recursos) { r.Versao = 9 },
		"aprovado não é sha": func(r *Recursos) { r.Aprovados = map[string]string{"https://github.com/a/b/pull/1": "zz"} },
	} {
		r := ok()
		mut(r)
		if err := r.Validar(u); err == nil {
			t.Errorf("%s: aceitou", nome)
		}
	}
}

func TestLigadaPeloAmbiente(t *testing.T) {
	for v, want := range map[string]bool{"": true, "1": true, "sim": true, "0": false, "false": false, "OFF": false, "não": false, " nao ": false} {
		if got := LigadaPeloAmbiente(func(string) string { return v }); got != want {
			t.Errorf("%q = %v", v, got)
		}
	}
}

// --- limpeza automática no daemon ---

func (c *cen) mesclarNoBanco() {
	c.t.Helper()
	if err := c.st.GravarMR(context.Background(), c.url, central.ValorMR{Estado: "mesclada"}, nil); err != nil {
		c.t.Fatal(err)
	}
	if _, _, err := c.st.AtualizarStatus(context.Background(), &c.coord, c.u, central.MudancaStatus{Status: "done"}); err != nil {
		c.t.Fatal(err)
	}
}

func TestAutoLimpaNoMergeEEncerraAOnda(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.dockerUS()
	c.docker("orq.onda", fmt.Sprint(c.ondaID), "", "", "onda-vol", "")
	var linhas []string
	a := &Auto{L: c.l, Log: func(f string, args ...any) { linhas = append(linhas, fmt.Sprintf(f, args...)) }}

	// MR ainda não mesclada no cache: nada acontece.
	if err := a.Ciclo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !existe(c.wt) || len(c.escritas("herdr")) != 0 {
		t.Fatal("agiu sem MR mesclada")
	}

	c.mesclarNoBanco()
	if err := a.Ciclo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if existe(c.wt) || existe(c.trabalho()) || existe(c.tmp()) {
		t.Fatalf("a limpeza automática não terminou: %v", linhas)
	}
	o, err := c.st.VerOnda(context.Background(), c.ondaID)
	if err != nil {
		t.Fatal(err)
	}
	if o.EncerradaEm == nil {
		t.Fatalf("a onda não foi encerrada: %v", linhas)
	}
	docker := strings.Join(c.escritas("docker"), "|")
	if !strings.Contains(docker, "volume rm orq-13_dados") || !strings.Contains(docker, "volume rm onda-vol") {
		t.Errorf("docker = %s", docker)
	}
	es, _ := c.st.Diario(context.Background(), nil, central.FiltroDiario{OndaID: c.ondaID})
	tipos := ""
	for _, e := range es {
		tipos += e.Tipo + "\n"
	}
	for _, w := range []string{"onda.encerrar", "limpar.onda", "limpar.removido"} {
		if !strings.Contains(tipos, w) {
			t.Errorf("diário sem %s", w)
		}
	}
	// Ciclo de novo: nada a fazer, sem eventos novos.
	n := len(es)
	if err := a.Ciclo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if es2, _ := c.st.Diario(context.Background(), nil, central.FiltroDiario{OndaID: c.ondaID}); len(es2) != n {
		t.Errorf("eventos repetidos: %d → %d", n, len(es2))
	}
}

func TestAutoNaoRepeteEventoNemTentaAntesDoReintento(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.mesclarNoBanco()
	c.herdr(map[string]string{"wA:p2": "working"})
	agora := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a := &Auto{L: c.l, Agora: func() time.Time { return agora }}
	rodar := func() int {
		if err := a.Ciclo(context.Background()); err != nil {
			t.Fatal(err)
		}
		return len(c.tipos())
	}
	n1 := rodar()
	if n1 == 0 || !existe(c.wt) {
		t.Fatalf("1º ciclo: %d eventos", n1)
	}
	chamadasAntes := len(c.chamadas("herdr"))
	if n := rodar(); n != n1 || len(c.chamadas("herdr")) != 0 {
		t.Errorf("tentou de novo antes do reintento (eventos %d → %d, herdr %d→)", n1, n, chamadasAntes)
	}
	agora = agora.Add(6 * time.Minute)
	if n := rodar(); n != n1 {
		t.Errorf("repetiu o mesmo desfecho no diário: %d → %d", n1, n)
	}
	// O agente para: o próximo ciclo (depois do reintento) termina o serviço.
	c.herdr(map[string]string{"wA:p2": "idle"})
	agora = agora.Add(6 * time.Minute)
	rodar()
	if existe(c.wt) || existe(c.trabalho()) {
		t.Error("não limpou depois que o agente parou")
	}
}

func TestAutoIgnoraUSSemRecursos(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.mesclarNoBanco()
	if err := os.Remove(c.l.Raizes.Arquivo(c.u)); err != nil {
		t.Fatal(err)
	}
	a := &Auto{L: c.l}
	if err := a.Ciclo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !existe(c.wt) || len(c.tipos()) != 0 && strings.Contains(strings.Join(c.tipos(), ""), "limpar.") {
		t.Fatal("agiu numa US sem recursos gravados")
	}
}

func TestAutoNaoEncerraOndaDePlano(t *testing.T) {
	c := novo(t)
	o, err := c.st.CriarOnda(context.Background(), c.coord, central.NovaOnda{Titulo: "p", Ate: "plano", US: []refs.US{{Projeto: "orq", Ref: 77}}})
	if err == nil {
		_ = o
	}
	// A US 13 está na onda aberta de merge; criar outra com a 77 só falha se a 77 já estiver em onda aberta.
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.st.AtualizarStatus(context.Background(), &c.coord, refs.US{Projeto: "orq", Ref: 77}, central.MudancaStatus{Status: "done"}); err != nil {
		t.Fatal(err)
	}
	a := &Auto{L: c.l}
	if err := a.Ciclo(context.Background()); err != nil {
		t.Fatal(err)
	}
	o2, _ := c.st.VerOnda(context.Background(), o.ID)
	if o2.EncerradaEm != nil {
		t.Fatal("encerrou onda de plano sozinho")
	}
}

// --- achados da revisão r1 ---

// F1: o plano foi montado, a US foi reaberta (mesmo worktree em branch nova, MR nova, recursos
// regravados, evidência nova no TMPDIR) e só então a execução rodou: nada da geração nova some.
func TestReaberturaDepoisDoPlanoPreservaAGeracaoNova(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.l.aposPlano = func() {
		git(t, c.wt, "checkout", "-q", "-b", "TG-13-correcao")
		escrever(t, filepath.Join(c.wt, "correcao.txt"), "c")
		git(t, c.wt, "add", ".")
		git(t, c.wt, "commit", "-q", "-m", "correção")
		if _, _, err := c.st.AdicionarMR(context.Background(), &c.coord, c.u, "https://github.com/BasisTI/orq/pull/10"); err != nil {
			t.Fatal(err)
		}
		rec := &Recursos{Repos: []Repo{{Repo: c.repo, Worktree: c.wt, Branch: "TG-13-correcao"}}, Panes: []string{"tg-13_executor"}}
		if _, err := rec.Gravar(c.l.Raizes, c.u); err != nil {
			t.Fatal(err)
		}
		escrever(t, filepath.Join(c.tmp(), "evidencia-nova"), "e")
	}
	r := c.limpar(false)
	if r.Agiu {
		t.Fatalf("removeu algo de um plano velho: %s", estados(r))
	}
	for _, p := range []string{c.wt, c.l.Raizes.Arquivo(c.u), filepath.Join(c.tmp(), "evidencia-nova")} {
		if !existe(p) {
			t.Errorf("%s sumiu", p)
		}
	}
	if len(c.escritas("herdr")) != 0 {
		t.Error("fechou pane com o plano velho")
	}
	if b := git(t, c.wt, "symbolic-ref", "--short", "HEAD"); b != "TG-13-correcao" {
		t.Errorf("branch = %s", b)
	}
	for _, i := range r.Itens {
		if i.Estado == Preservado && !strings.Contains(i.Motivo, "mudou") && !strings.Contains(i.Motivo, "ficou") {
			t.Errorf("motivo estranho: %+v", i)
		}
	}
}

// F1, variante: só o worktree muda de branch (sem regravar o arquivo): a identidade é reconferida.
func TestWorktreeEmOutraBranchNaHoraDeRemover(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.l.aposPlano = func() { git(t, c.wt, "checkout", "-q", "-b", "outra") }
	r := c.limpar(false)
	if wt := c.estadoDe(r, "worktree")[0]; wt.Estado != Preservado || !existe(c.wt) {
		t.Fatalf("worktree = %+v", wt)
	}
}

// F2: um pane com nome parecido, de outro board e fora do recursos.json, nunca é fechado.
func TestPaneDeOutroBoardNaoEFechado(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.extras = []map[string]any{
		{"pane_id": "wOutro:p1", "workspace_id": "wOutro", "agent": "claude", "agent_status": "idle", "name": "tg-13_revisor"},
		{"pane_id": "wOutro:p2", "workspace_id": "wOutro", "agent": "claude", "agent_status": "idle", "name": "orq-tg-13_executor"},
	}
	c.herdr(map[string]string{"wA:p2": "idle"})
	r := c.limpar(false)
	if got := c.escritas("herdr"); len(got) != 1 || got[0] != "pane close wA:p2" {
		t.Fatalf("panes fechados = %v", got)
	}
	if n := len(c.estadoDe(r, "pane")); n != 1 {
		t.Errorf("%d panes no plano", n)
	}
}

// F3: o TMPDIR que o orq recursos gravar publica existe antes de o worker começar.
func TestTmpExisteDepoisDeGravar(t *testing.T) {
	raizes := Raizes{Trabalho: filepath.Join(t.TempDir(), "t"), Tmp: filepath.Join(t.TempDir(), "tmp")}
	u := refs.US{Projeto: "orq", Ref: 13}
	rec := &Recursos{}
	if _, err := rec.Gravar(raizes, u); err != nil {
		t.Fatal(err)
	}
	d, err := os.MkdirTemp(raizes.PastaTmp(u), "worker-")
	if err != nil {
		t.Fatalf("MkdirTemp no TMPDIR publicado: %v", err)
	}
	if st, _ := os.Stat(raizes.PastaTmp(u)); st.Mode().Perm() != 0o700 {
		t.Errorf("modo %v", st.Mode())
	}
	_ = d
}

// F4: sem head aprovado registrado, a limpeza recusa, mesmo com o conteúdo integrado.
func TestSemAprovadoRecusa(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.aprovar("")
	r := c.limpar(false)
	if !strings.Contains(r.Recusa, "sem head aprovado") || r.Agiu {
		t.Fatalf("resultado = %+v", r)
	}
	for _, p := range []string{c.wt, c.tmp(), c.trabalho()} {
		if !existe(p) {
			t.Errorf("%s removido sem aprovação", p)
		}
	}
	if len(c.escritas("herdr")) != 0 {
		t.Error("fechou pane sem aprovação")
	}
}

// F5: Docker da onda que falha não deixa a onda fechada sem nova tentativa.
func TestOndaSegueAbertaAteODockerLimpar(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	c.docker("orq.onda", fmt.Sprint(c.ondaID), "", "", "onda-volume", "")
	c.mesclarNoBanco()
	err := os.WriteFile(filepath.Join(c.dir, "resp", "docker", chave("volume", "rm", "onda-volume")+".err"), []byte("volume is in use"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	agora := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a := &Auto{L: c.l, Agora: func() time.Time { return agora }}
	if err := a.Ciclo(context.Background()); err != nil {
		t.Fatal(err)
	}
	o, _ := c.st.VerOnda(context.Background(), c.ondaID)
	if o.EncerradaEm != nil {
		t.Fatal("encerrou a onda com o Docker dela falhando")
	}
	if !strings.Contains(strings.Join(c.escritas("docker"), "|"), "volume rm onda-volume") {
		t.Fatal("não tentou remover")
	}
	// Antes do reintento, nada de nova tentativa; depois dele, com o erro resolvido, fecha.
	if err := a.Ciclo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.escritas("docker")) != 0 {
		t.Error("tentou antes do reintento")
	}
	if err := os.Remove(filepath.Join(c.dir, "resp", "docker", chave("volume", "rm", "onda-volume")+".err")); err != nil {
		t.Fatal(err)
	}
	agora = agora.Add(6 * time.Minute)
	if err := a.Ciclo(context.Background()); err != nil {
		t.Fatal(err)
	}
	o, _ = c.st.VerOnda(context.Background(), c.ondaID)
	if o.EncerradaEm == nil || !strings.Contains(strings.Join(c.escritas("docker"), "|"), "volume rm onda-volume") {
		t.Fatalf("onda = %+v", o)
	}
}

// --- achado F1 da revisão r2: exclusão mútua entre a limpeza e a reabertura ---

// reabrirDuranteItem segura a limpeza no momento de executar um item do tipo dado (ou, com
// espera != nil, dentro do próprio comando), reabre a US de outro lado e confere que a reabertura
// espera a limpeza terminar: o worktree novo só nasce depois, e nada da rodada nova é apagado.
func (c *cen) reabrirDuranteItem(tipo string, preparar func(), segurar func(liberar func())) {
	c.t.Helper()
	c.mergeNaMain(false, false)
	c.dockerUS()
	if preparar != nil {
		preparar()
	}
	parado, soltar := make(chan struct{}), make(chan struct{})
	var uma sync.Once
	if segurar == nil {
		c.l.antesDoItem = func(i *Item) {
			if i.Tipo == tipo {
				uma.Do(func() { close(parado); <-soltar })
			}
		}
	} else {
		segurar(func() { close(soltar) })
	}
	feito := make(chan Resultado, 1)
	var limparTerminou atomic.Bool
	go func() {
		r, err := c.l.Limpar(context.Background(), &c.coord, c.u, Opcoes{})
		if err != nil {
			c.t.Error(err)
		}
		limparTerminou.Store(true)
		feito <- r
	}()
	if segurar == nil {
		<-parado
	} else {
		esperarArquivo(c.t, filepath.Join(c.dir, "git.parado"))
	}
	reaberta := make(chan *Recursos, 1)
	var reabriuAntes atomic.Bool
	go func() {
		rec, _, err := c.l.Reabrir(context.Background(), &c.coord, c.u)
		if err != nil {
			c.t.Error(err)
		}
		reabriuAntes.Store(!limparTerminou.Load())
		reaberta <- rec
	}()
	select {
	case <-reaberta:
		c.t.Fatal("a reabertura não esperou a limpeza em andamento")
	case <-time.After(400 * time.Millisecond):
	}
	if segurar == nil {
		close(soltar)
	} else {
		segurar(nil)
	}
	r := <-feito
	rec := <-reaberta
	if reabriuAntes.Load() {
		c.t.Error("a reabertura terminou antes da limpeza")
	}
	if !r.Completa {
		c.t.Errorf("a limpeza da geração antiga devia terminar inteira: %s", estados(r))
	}
	if rec == nil || rec.Geracao != 1 || len(rec.MRsEncerradas) != 1 {
		c.t.Fatalf("recursos reabertos = %+v", rec)
	}
	// Só agora o coordenador recria o worktree (caminho novo) e grava a rodada nova.
	novoWt := c.wt + "-r2"
	git(c.t, c.repo, "worktree", "add", "-q", "-b", "TG-13-r2", novoWt)
	rec.Repos = []Repo{{Repo: c.repo, Worktree: novoWt, Branch: "TG-13-r2"}}
	rec.Panes = []string{"tg-13_executor"}
	if _, err := rec.Gravar(c.l.Raizes, c.u); err != nil {
		c.t.Fatal(err)
	}
	escrever(c.t, filepath.Join(c.tmp(), "evidencia-nova"), "e")
	// Reaberta e sem MR nova ligada: a limpeza recusa e não toca em nada.
	r2 := c.limpar(false)
	if !strings.Contains(r2.Recusa, "reaberta") || !existe(novoWt) || !existe(filepath.Join(c.tmp(), "evidencia-nova")) || !existe(c.l.Raizes.Arquivo(c.u)) {
		c.t.Fatalf("depois da reabertura: %+v", r2)
	}
}

func esperarArquivo(t *testing.T, p string) {
	t.Helper()
	limite := time.Now().Add(10 * time.Second)
	for !existe(p) {
		if time.Now().After(limite) {
			t.Fatalf("esperando %s", p)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReaberturaEsperaALimpezaEmCadaTipoDeItem(t *testing.T) {
	for _, tipo := range []string{"pane", "worktree", "pasta_tmp", "docker_volume", "pasta_trabalho"} {
		t.Run(tipo, func(t *testing.T) {
			c := novo(t)
			c.reabrirDuranteItem(tipo, nil, nil)
		})
	}
}

// A variante do revisor: o próprio `git worktree remove` fica parado num wrapper do git.
func TestReaberturaEsperaOGitWorktreeRemove(t *testing.T) {
	c := novo(t)
	wrapper := filepath.Join(c.dir, "bin", "git-lento")
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  if [ "$a" = "remove" ]; then
    touch '%[1]s/git.parado'
    while [ ! -f '%[1]s/git.liberar' ]; do sleep 0.02; done
  fi
done
exec /usr/bin/git "$@"
`, c.dir)
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c.env["ORQ_GIT_BIN"] = wrapper
	c.reabrirDuranteItem("", nil, func(liberar func()) {
		if liberar == nil {
			escrever(t, filepath.Join(c.dir, "git.liberar"), "")
		}
	})
}

// A trava ocupada faz o limpar recusar (o daemon tenta depois), e a gravação espera.
func TestLimparRecusaComATravaOcupadaEGravarEspera(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	soltar, err := c.l.Raizes.TravarUS(c.u, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r := c.limpar(false); !strings.Contains(r.Recusa, "em andamento") || !existe(c.wt) {
		t.Fatalf("resultado = %+v", r)
	}
	if _, err := c.l.Raizes.TravarUS(c.u, 100*time.Millisecond); err == nil {
		t.Fatal("pegou a trava duas vezes")
	}
	soltar()
	if r := c.limpar(false); r.Recusa != "" || !r.Completa {
		t.Fatalf("depois de soltar: %+v", r)
	}
}

// Reabrir com a geração antiga ainda no disco: a limpeza só segue com uma MR nova ligada,
// mesclada e aprovada.
func TestDepoisDeReabrirALimpezaExigeAMRNovaMesclada(t *testing.T) {
	c := novo(t)
	c.mergeNaMain(false, false)
	if _, _, err := c.l.Reabrir(context.Background(), &c.coord, c.u); err != nil {
		t.Fatal(err)
	}
	if r := c.limpar(false); !strings.Contains(r.Recusa, "reaberta") {
		t.Fatalf("sem MR nova: %+v", r)
	}
	nova := "https://github.com/BasisTI/orq/pull/10"
	if _, _, err := c.st.AdicionarMR(context.Background(), &c.coord, c.u, nova); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"state": "open", "merged": false, "mergeable_state": "clean", "head": map[string]string{"sha": c.tip, "ref": "TG-13-r2"}, "base": map[string]string{"ref": "main"}})
	c.responder("gh", string(b), "api", "repos/BasisTI/orq/pulls/10")
	if r := c.limpar(false); !strings.Contains(r.Recusa, "MR não mesclada") {
		t.Fatalf("MR nova aberta: %+v", r)
	}
}
