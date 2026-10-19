package upgrade

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/output"
	_ "modernc.org/sqlite"
)

const (
	commitAntigo = "aaaaaaaaaaaa1111111111111111111111111111"
	commitNovo   = "bbbbbbbbbbbb2222222222222222222222222222"
	versaoAntiga = "0.3.0-1-gaaaaaaa"
	versaoNova   = "0.3.0-2-gbbbbbbb"
)

var (
	binsOnce sync.Once
	binsDir  string
	binsErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binsDir != "" {
		os.RemoveAll(binsDir)
	}
	os.Exit(code)
}

// binarios compila o orq duas vezes, como se fossem duas versões, uma vez por execução dos testes.
func binarios(t *testing.T) (antigo, novo string) {
	t.Helper()
	binsOnce.Do(func() {
		binsDir, binsErr = os.MkdirTemp("", "orq-upgrade")
		if binsErr != nil {
			return
		}
		for nome, v := range map[string][2]string{"antigo": {versaoAntiga, commitAntigo}, "novo": {versaoNova, commitNovo}} {
			ld := "-X github.com/BasisTI/orq/internal/cli.Version=" + v[0] + " -X github.com/BasisTI/orq/internal/cli.Commit=" + v[1]
			out, err := exec.Command("go", "build", "-ldflags", ld, "-o", filepath.Join(binsDir, nome), "github.com/BasisTI/orq/cmd/orq").CombinedOutput()
			if err != nil {
				binsErr = &os.PathError{Op: "go build", Path: nome, Err: errString(err.Error() + ": " + string(out))}
				return
			}
		}
	})
	if binsErr != nil {
		t.Fatal(binsErr)
	}
	return filepath.Join(binsDir, "antigo"), filepath.Join(binsDir, "novo")
}

type errString string

func (e errString) Error() string { return string(e) }

// casa é um ambiente inteiro de mentira: um go install, um plugin, um herdr e um go falsos, tudo
// debaixo de um diretório temporário.
type casa struct {
	t                   *testing.T
	raiz, estado        string
	goBin, plugin, path string // arquivos orq
	antigo, novo        string
	env                 map[string]string
}

func escrever(t *testing.T, caminho, conteudo string, modo os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caminho, []byte(conteudo), modo); err != nil {
		t.Fatal(err)
	}
}

func copiar(t *testing.T, de, para string) {
	t.Helper()
	b, err := os.ReadFile(de)
	if err != nil {
		t.Fatal(err)
	}
	escrever(t, para, string(b), 0o755)
}

// novaCasa monta o ambiente com os dois binários na versão antiga: o do go install, o do plugin
// e o do PATH, que é um link para o do plugin, como no ambiente real.
func novaCasa(t *testing.T) *casa {
	t.Helper()
	antigo, novo := binarios(t)
	raiz := t.TempDir()
	c := &casa{t: t, raiz: raiz, estado: filepath.Join(raiz, "estado"), antigo: antigo, novo: novo}
	c.goBin = filepath.Join(raiz, "go", "bin", "orq")
	c.plugin = filepath.Join(raiz, "plugin", "bin", "orq")
	c.path = filepath.Join(raiz, ".local", "bin", "orq")
	copiar(t, antigo, c.goBin)
	copiar(t, antigo, c.plugin)
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(c.plugin, c.path); err != nil {
		t.Fatal(err)
	}
	// O que o "go install" e o "build do plugin" instalam: o binário em c.novo, ou o que estiver
	// em <raiz>/novo-no-lugar-do-novo.
	escrever(t, filepath.Join(raiz, "bin", "go"), `#!/bin/sh
echo "go $*" >> '`+raiz+`/chamadas.log'
case "$1" in
env) echo '`+filepath.Join(raiz, "go", "bin")+`'; echo '`+filepath.Join(raiz, "go")+`' ;;
install)
	[ -e '`+raiz+`/falha-go' ] && { echo "proxy: sem rede" >&2; exit 1; }
	cp '`+c.fonteNova()+`' '`+c.goBin+`.tmp' && mv '`+c.goBin+`.tmp' '`+c.goBin+`' ;;
esac
`, 0o755)
	escrever(t, filepath.Join(raiz, "bin", "herdr"), `#!/bin/sh
echo "herdr $*" >> '`+raiz+`/chamadas.log'
case "$1 $2" in
"plugin list") echo '{"id":"cli:plugin","result":{"plugins":[{"plugin_id":"orq","plugin_root":"`+filepath.Join(raiz, "plugin")+`","enabled":true}],"type":"plugin_list"}}' ;;
"plugin install")
	[ -e '`+raiz+`/falha-plugin' ] && { echo "build: go não encontrado" >&2; exit 1; }
	cp '`+c.fonteNova()+`' '`+c.plugin+`.tmp' && mv '`+c.plugin+`.tmp' '`+c.plugin+`' ;;
esac
`, 0o755)
	c.env = map[string]string{
		"HOME":          raiz,
		"PATH":          filepath.Join(raiz, ".local", "bin"),
		"ORQ_GO_BIN":    filepath.Join(raiz, "bin", "go"),
		"ORQ_HERDR_BIN": filepath.Join(raiz, "bin", "herdr"),
	}
	return c
}

// fonteNova é o arquivo que o go install e o plugin copiam para o lugar do binário.
func (c *casa) fonteNova() string { return filepath.Join(c.raiz, "novo", "orq") }

// instalarNovo põe em c.fonteNova() o orq novo de verdade.
func (c *casa) instalarNovo() { copiar(c.t, c.novo, c.fonteNova()) }

// instalarNovoQuebrado põe um "orq novo" que informa a versão nova mas não sobe o daemon.
func (c *casa) instalarNovoQuebrado() {
	escrever(c.t, c.fonteNova(), `#!/bin/sh
case "$1" in
version) echo '{"versao":"`+versaoNova+`","commit":"`+commitNovo+`","schema":4}' ;;
*) echo "boom" >&2; exit 1 ;;
esac
`, 0o755)
}

func (c *casa) ambiente() Ambiente {
	return Ambiente{Env: func(k string) string { return c.env[k] }, Dir: c.estado}
}

func (c *casa) falha(nome string) { escrever(c.t, filepath.Join(c.raiz, "falha-"+nome), "", 0o644) }

// subirDaemon sobe um daemon do binário antigo, com um ambiente que o upgrade tem de preservar.
func (c *casa) subirDaemon(bin string) string {
	c.t.Helper()
	cmd := exec.Command(bin, "daemon", "--detach", "--output", "json", "--tick", "1h", "--intervalo-remoto", "2h")
	cmd.Env = append(os.Environ(), "ORQ_STATE_DIR="+c.estado, "ORQ_TEST_MARCA=preservada",
		"ORQ_HERDR_BIN=/nao/existe/herdr", "ORQ_GLAB_BIN=/nao/existe/glab", "ORQ_GH_BIN=/nao/existe/gh", "ORQ_TAIGA_BIN=/nao/existe/taiga",
		"HERDR_SOCKET_PATH="+filepath.Join(c.raiz, "sem.sock"))
	if out, err := cmd.CombinedOutput(); err != nil {
		c.t.Fatalf("subir daemon: %v: %s", err, out)
	}
	ativo, pid, err := enriquecer.DaemonAtivo(c.estado)
	if err != nil || !ativo {
		c.t.Fatalf("daemon não ativo: %v", err)
	}
	c.t.Cleanup(func() { c.pararDaemon() })
	return pid
}

func (c *casa) pararDaemon() {
	if ativo, pid, _ := enriquecer.DaemonAtivo(c.estado); ativo {
		n, _ := strconv.Atoi(pid)
		_ = syscall.Kill(n, syscall.SIGTERM)
		for i := 0; i < 100; i++ {
			if a, _, _ := enriquecer.DaemonAtivo(c.estado); !a {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func (c *casa) versao(bin string) Info {
	c.t.Helper()
	i, err := Consultar(context.Background(), bin)
	if err != nil {
		c.t.Fatal(err)
	}
	return i
}

func (c *casa) executar(o Opcoes) (*Resultado, error) {
	c.t.Helper()
	if o.Ref == "" {
		o.Ref = "main"
	}
	if o.EsperaParada == 0 {
		o.EsperaParada = 10 * time.Second
	}
	return Executar(context.Background(), c.ambiente(), o)
}

func codigoDe(err error) string {
	if err == nil {
		return ""
	}
	return output.AsError(err).Code
}

func etapa(r *Resultado, nome string) Etapa {
	for _, e := range r.Etapas {
		if e.Nome == nome {
			return e
		}
	}
	return Etapa{}
}

// O critério central: dois binários na versão anterior e um daemon antigo; um comando deixa os
// dois e o daemon na versão nova.
func TestUpgradeAtualizaOsDoisBinariosEODaemon(t *testing.T) {
	c := novaCasa(t)
	c.instalarNovo()
	pidAntigo := c.subirDaemon(c.plugin)
	if got := c.versao(c.goBin).Commit; got != commitAntigo {
		t.Fatalf("go install antes = %s", got)
	}

	r, err := c.executar(Opcoes{})
	if err != nil || !r.OK {
		t.Fatalf("upgrade: %v\n%+v", err, r)
	}
	for _, bin := range []string{c.goBin, c.plugin, c.path} {
		if got := c.versao(bin); got.Commit != commitNovo || got.Versao != versaoNova || got.Schema == 0 {
			t.Errorf("%s depois = %+v", bin, got)
		}
	}
	if r.Referencia != commitNovo || len(r.Atrasados) != 0 {
		t.Fatalf("referência %q, atrasados %+v", r.Referencia, r.Atrasados)
	}
	if e := etapa(r, "daemon"); e.Estado != "ok" {
		t.Fatalf("daemon = %+v", e)
	}
	ativo, pid, _ := enriquecer.DaemonAtivo(c.estado)
	if !ativo || pid == pidAntigo {
		t.Fatalf("daemon ativo %v pid %s (antes %s)", ativo, pid, pidAntigo)
	}
	// O daemon novo roda o binário novo, com os mesmos argumentos e o mesmo ambiente do antigo.
	if i, err := Consultar(context.Background(), filepath.Join("/proc", pid, "exe")); err != nil || i.Commit != commitNovo {
		t.Fatalf("exe do daemon novo = %+v, %v", i, err)
	}
	if cmd, _ := os.ReadFile(filepath.Join("/proc", pid, "cmdline")); !strings.Contains(string(cmd), "--tick\x001h") || !strings.Contains(string(cmd), "--intervalo-remoto\x002h") {
		t.Errorf("cmdline do daemon novo = %q", cmd)
	}
	if env, _ := os.ReadFile(filepath.Join("/proc", pid, "environ")); !strings.Contains(string(env), "ORQ_TEST_MARCA=preservada") {
		t.Error("o ambiente do daemon antigo não foi repetido")
	}
	if _, err := os.Stat(filepath.Join(c.estado, "upgrade")); !os.IsNotExist(err) {
		t.Errorf("sobrou a cópia do daemon antigo: %v", err)
	}
	// Segunda rodada: nada a reiniciar.
	r2, err := c.executar(Opcoes{})
	if err != nil || etapa(r2, "daemon").Estado != "pulado" {
		t.Fatalf("segunda rodada: %v %+v", err, r2.Etapas)
	}
	if _, pid2, _ := enriquecer.DaemonAtivo(c.estado); pid2 != pid {
		t.Errorf("a segunda rodada reiniciou o daemon: %s → %s", pid, pid2)
	}
}

func TestUpgradeFalhaNoGoInstallNaoParaODaemon(t *testing.T) {
	c := novaCasa(t)
	c.instalarNovo()
	c.falha("go")
	c.subirDaemon(c.plugin)

	r, err := c.executar(Opcoes{})
	if codigoDe(err) != "upgrade_incompleto" || output.AsError(err).Exit != output.ExitConflict {
		t.Fatalf("erro = %v", err)
	}
	msg := output.AsError(err).Cause
	if !strings.Contains(msg, "go install") || !strings.Contains(msg, c.goBin) || !strings.Contains(msg, "sem rede") {
		t.Errorf("a causa não diz o que ficou para trás: %s", msg)
	}
	if got := c.versao(c.goBin).Commit; got != commitAntigo {
		t.Errorf("go install mudou: %s", got)
	}
	if got := c.versao(c.plugin).Commit; got != commitNovo {
		t.Errorf("plugin não atualizou: %s", got)
	}
	if ativo, _, _ := enriquecer.DaemonAtivo(c.estado); !ativo {
		t.Fatal("o daemon ficou parado")
	}
	if len(r.Atrasados) != 1 || !strings.Contains(r.Atrasados[0].Quem, "go install") {
		t.Errorf("atrasados = %+v", r.Atrasados)
	}
}

func TestUpgradeFalhaNoPluginNaoParaODaemon(t *testing.T) {
	c := novaCasa(t)
	c.instalarNovo()
	c.falha("plugin")
	c.subirDaemon(c.plugin)

	r, err := c.executar(Opcoes{})
	msg := output.AsError(err).Cause
	if codigoDe(err) != "upgrade_incompleto" || !strings.Contains(msg, "plugin") || !strings.Contains(msg, "go não encontrado") {
		t.Fatalf("erro = %v", err)
	}
	if got := c.versao(c.goBin).Commit; got != commitNovo {
		t.Errorf("go install não atualizou: %s", got)
	}
	if got := c.versao(c.plugin).Commit; got != commitAntigo {
		t.Errorf("plugin mudou: %s", got)
	}
	if ativo, _, _ := enriquecer.DaemonAtivo(c.estado); !ativo {
		t.Fatal("o daemon ficou parado")
	}
	// Quem ficou para trás: o plugin, o PATH (que é o plugin) e o daemon, que subiu do go install?
	// O candidato é o binário que ficou novo; o daemon novo, portanto, está na versão nova.
	nomes := ""
	for _, a := range r.Atrasados {
		nomes += a.Quem + "\n"
	}
	if !strings.Contains(nomes, "plugin") || strings.Contains(nomes, "daemon") {
		t.Errorf("atrasados = %s", nomes)
	}
}

func TestUpgradeComAsDuasEtapasFalhandoDeixaODaemonComoEstava(t *testing.T) {
	c := novaCasa(t)
	c.instalarNovo()
	c.falha("go")
	c.falha("plugin")
	pid := c.subirDaemon(c.plugin)
	_, err := c.executar(Opcoes{})
	if codigoDe(err) != "upgrade_incompleto" {
		t.Fatalf("erro = %v", err)
	}
	if ativo, p, _ := enriquecer.DaemonAtivo(c.estado); !ativo || p != pid {
		t.Fatalf("o daemon mudou: ativo=%v pid=%s (era %s)", ativo, p, pid)
	}
}

// Daemon novo que não sobe: o upgrade volta ao daemon anterior, diz a falha e não deixa nada parado.
func TestUpgradeDaemonNovoNaoSobeVoltaAoAnterior(t *testing.T) {
	c := novaCasa(t)
	c.instalarNovoQuebrado()
	c.subirDaemon(c.plugin)

	r, err := c.executar(Opcoes{})
	if codigoDe(err) != "upgrade_incompleto" {
		t.Fatalf("erro = %v", err)
	}
	e := etapa(r, "daemon")
	if e.Estado != "falhou" || !strings.Contains(e.Detalhe, "voltei ao daemon anterior") || !strings.Contains(e.Detalhe, "boom") {
		t.Fatalf("etapa daemon = %+v", e)
	}
	ativo, pid, _ := enriquecer.DaemonAtivo(c.estado)
	if !ativo {
		t.Fatal("o daemon ficou parado")
	}
	if i, err := Consultar(context.Background(), filepath.Join("/proc", pid, "exe")); err != nil || i.Commit != commitAntigo {
		t.Fatalf("o daemon que voltou = %+v, %v", i, err)
	}
	if _, err := os.Stat(filepath.Join(c.estado, "upgrade")); !os.IsNotExist(err) {
		t.Errorf("sobrou a cópia do daemon antigo: %v", err)
	}
}

// Um daemon que não solta a trava a tempo fica como está; o upgrade não usa SIGKILL.
func TestUpgradeDaemonQueNaoParaContinuaRodando(t *testing.T) {
	c := novaCasa(t)
	c.instalarNovo()
	if err := os.MkdirAll(c.estado, 0o700); err != nil {
		t.Fatal(err)
	}
	trava := filepath.Join(c.estado, enriquecer.ArquivoTravaDaemon)
	// Um "daemon" que segura a trava e ignora o SIGTERM; o "daemon" no argv basta para ehDaemon.
	cmd := exec.Command("flock", "-x", trava, "sh", "-c", `echo $$ > '`+trava+`'; trap "" TERM; while :; do sleep 0.1; done`, "daemon")
	if err := cmd.Start(); err != nil {
		t.Skipf("sem flock(1): %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	for i := 0; i < 100; i++ {
		if ativo, _, _ := enriquecer.DaemonAtivo(c.estado); ativo {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if b, _ := os.ReadFile(trava); strings.TrimSpace(string(b)) == "" {
		time.Sleep(300 * time.Millisecond)
	}
	r, err := c.executar(Opcoes{EsperaParada: 600 * time.Millisecond})
	if codigoDe(err) != "upgrade_incompleto" {
		t.Fatalf("erro = %v", err)
	}
	if e := etapa(r, "daemon"); e.Estado != "falhou" || !strings.Contains(e.Detalhe, "segue rodando") {
		t.Fatalf("etapa daemon = %+v", e)
	}
	if ativo, _, _ := enriquecer.DaemonAtivo(c.estado); !ativo {
		t.Fatal("o daemon foi parado")
	}
}

// Um orq no PATH que não é o do go install nem o do plugin não é atualizado: o upgrade diz qual.
func TestUpgradeBinarioForaDoControleFicaParaTras(t *testing.T) {
	c := novaCasa(t)
	c.instalarNovo()
	solto := filepath.Join(c.raiz, "usr", "bin", "orq")
	copiar(t, c.antigo, solto)
	c.env["PATH"] = filepath.Dir(solto) + ":" + filepath.Dir(c.path)
	c.subirDaemon(c.plugin)
	r, err := c.executar(Opcoes{})
	if codigoDe(err) != "upgrade_incompleto" {
		t.Fatalf("erro = %v", err)
	}
	if len(r.Atrasados) != 1 || !strings.Contains(r.Atrasados[0].Quem, solto) || !strings.Contains(r.Atrasados[0].Motivo, "troque-o à mão") {
		t.Fatalf("atrasados = %+v", r.Atrasados)
	}
}

// O orq do go install que é link para o do plugin é o mesmo arquivo: não há go install a fazer.
func TestUpgradeGoInstallQueELinkParaOPlugin(t *testing.T) {
	c := novaCasa(t)
	c.instalarNovo()
	if err := os.Remove(c.goBin); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(c.plugin, c.goBin); err != nil {
		t.Fatal(err)
	}
	r, err := c.executar(Opcoes{})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if e := etapa(r, "go install"); e.Estado != "pulado" {
		t.Fatalf("go install = %+v", e)
	}
	if log, _ := os.ReadFile(filepath.Join(c.raiz, "chamadas.log")); strings.Contains(string(log), "go install") {
		t.Errorf("rodou go install: %s", log)
	}
}

func TestUpgradeSemNadaParaAtualizar(t *testing.T) {
	c := novaCasa(t)
	c.env["ORQ_HERDR_BIN"] = "/nao/existe/herdr"
	if err := os.Remove(c.goBin); err != nil {
		t.Fatal(err)
	}
	r, err := c.executar(Opcoes{})
	if codigoDe(err) != "upgrade_incompleto" || !strings.Contains(output.AsError(err).Cause, "nenhum binário foi atualizado") {
		t.Fatalf("erro = %v", err)
	}
	if len(r.Etapas) != 2 {
		t.Fatalf("etapas = %+v", r.Etapas)
	}
}

func TestPassaORefAoGoEAoHerdr(t *testing.T) {
	c := novaCasa(t)
	c.instalarNovo()
	if _, err := c.executar(Opcoes{Ref: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(filepath.Join(c.raiz, "chamadas.log"))
	for _, quer := range []string{"go install github.com/BasisTI/orq/cmd/orq@v1.2.3", "herdr plugin install BasisTI/orq --ref v1.2.3 --yes"} {
		if !strings.Contains(string(log), quer) {
			t.Errorf("sem %q:\n%s", quer, log)
		}
	}
}

func TestValidarRef(t *testing.T) {
	for _, ok := range []string{"main", "v1.2.3", "feature/x-1", "abcdef1234", "TG-11"} {
		if err := ValidarRef(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, ruim := range []string{"", "-x", "--ref", "a b", "a;b", "a$(b)", "main\n"} {
		if err := ValidarRef(ruim); err == nil {
			t.Errorf("%q aceito", ruim)
		}
	}
	if _, err := Executar(context.Background(), Ambiente{Env: func(string) string { return "" }}, Opcoes{Ref: "-x"}); codigoDe(err) != "uso" {
		t.Errorf("ref ruim = %v", err)
	}
}

func TestCommitDeEMesmoCommit(t *testing.T) {
	for versao, quer := range map[string]string{
		"0.0.0-20261019120000-abcdef123456": "abcdef123456",
		"0.3.0-5-gABCDEF1":                  "abcdef1",
		"0.3.0-5-gabcdef1-dirty":            "abcdef1",
		"abcdef1":                           "abcdef1",
		"dev":                               "",
		"0.3.0":                             "",
		"0.3.0-dirty":                       "",
	} {
		if got := CommitDe(versao); got != quer {
			t.Errorf("CommitDe(%q) = %q, queria %q", versao, got, quer)
		}
	}
	if !MesmoCommit("abcdef1", "abcdef1234567890") || MesmoCommit("abcdef1", "abcdef2") || MesmoCommit("abc", "abc") || MesmoCommit("", "") {
		t.Error("MesmoCommit")
	}
}

func TestResultadoSerializaEmJSON(t *testing.T) {
	r := &Resultado{Etapas: []Etapa{}, Atrasados: []Atraso{}, TUIs: []TUI{}}
	b, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(b), `"atrasados":[]`) || !strings.Contains(string(b), `"tuis_para_reabrir":[]`) {
		t.Fatalf("%s %v", b, err)
	}
}

// Um `orq tui` aberto com o binário antigo é listado para ser reaberto; o do binário novo, não.
func TestUpgradeListaOsTUIsAbertos(t *testing.T) {
	antigo, novo := binarios(t)
	proc := t.TempDir()
	tui := func(pid, exe string, args ...string) {
		if err := os.MkdirAll(filepath.Join(proc, pid), 0o755); err != nil {
			t.Fatal(err)
		}
		escrever(t, filepath.Join(proc, pid, "cmdline"), strings.Join(append([]string{"/x/orq"}, args...), "\x00")+"\x00", 0o644)
		if err := os.Symlink(exe, filepath.Join(proc, pid, "exe")); err != nil {
			t.Fatal(err)
		}
	}
	tui("101", antigo, "tui", "--todos")
	tui("102", novo, "tui", "--coord", "a")
	tui("103", antigo, "daemon", "--tick", "5s") // não é tui
	got := tuisEm(proc, commitNovo)
	if len(got) != 1 || got[0].PID != "101" || got[0].Versao != versaoAntiga {
		t.Fatalf("tuis = %+v", got)
	}
}

// A2: o pid da trava só leva SIGTERM se for o daemon do orq deste diretório de estado.
func TestPararRecusaProcessoQueNaoEODaemon(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("sem python3")
	}
	cmd := exec.Command(py, "-c", "import time; time.sleep(60)", "daemon")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := strconv.Itoa(cmd.Process.Pid)

	c := novaCasa(t)
	// Mesmo com a linha de comando `daemon` e uma trava que cita o pid, não é um orq.
	escrever(t, filepath.Join(c.estado, enriquecer.ArquivoTravaDaemon), pid+"\n", 0o600)
	for _, dir := range []string{t.TempDir(), c.estado} {
		err = Ambiente{Dir: dir}.parar(context.Background(), pid, time.Second)
		if codigoDe(err) != "daemon_alheio" {
			t.Fatalf("parar(%s) = %v, quero daemon_alheio", dir, err)
		}
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("o processo alheio foi sinalizado: %v", err)
	}
}

// Um orq daemon de outro diretório de estado também não leva o sinal.
func TestPararRecusaDaemonDeOutroEstado(t *testing.T) {
	c := novaCasa(t)
	pid := c.subirDaemon(c.plugin)
	outro := t.TempDir()
	escrever(t, filepath.Join(outro, enriquecer.ArquivoTravaDaemon), pid+"\n", 0o600)
	if err := (Ambiente{Dir: outro}).parar(context.Background(), pid, time.Second); codigoDe(err) != "daemon_alheio" {
		t.Fatalf("parar = %v, quero daemon_alheio", err)
	}
	if ativo, _, _ := enriquecer.DaemonAtivo(c.estado); !ativo {
		t.Fatal("o daemon foi sinalizado")
	}
	// O do próprio diretório é aceito.
	if err := c.ambiente().parar(context.Background(), pid, 10*time.Second); err != nil {
		t.Fatalf("parar o daemon certo: %v", err)
	}
}

// A3: o upgrade só anuncia sucesso com o banco aberto. Um daemon novo que sai ao abrir o banco
// (aqui, de uma versão que nenhum binário conhece) faz o upgrade falhar com o erro do filho e
// dizer que o daemon ficou parado.
func TestUpgradeNaoConfirmaDaemonQueNaoAbriuOBanco(t *testing.T) {
	c := novaCasa(t)
	c.instalarNovo()
	c.subirDaemon(c.plugin)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(c.estado, "central.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99; UPDATE banco_meta SET valor = '99'"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	// A trava de início presa por 1 s: o filho tem de esperar por ela antes de abrir o banco, e
	// a trava de daemon que ele já segura não pode valer como "pronto".
	f, err := os.OpenFile(filepath.Join(c.estado, "central.db.init.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(time.Second)
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}()

	r, err := c.executar(Opcoes{})
	if codigoDe(err) != "upgrade_incompleto" || r.OK {
		t.Fatalf("OK=%v erro=%v", r.OK, err)
	}
	e := etapa(r, "daemon")
	if e.Estado != "falhou" || !strings.Contains(e.Detalhe, "versao_do_banco") || !strings.Contains(e.Detalhe, "PARADO") {
		t.Fatalf("etapa daemon = %+v", e)
	}
	if ativo, _, _ := enriquecer.DaemonAtivo(c.estado); ativo {
		t.Fatal("sobrou um daemon vivo que não abre o banco")
	}
}

// Um daemon já no ar, com o banco migrado por outro processo para além do que ele escreve,
// registra o erro, solta a trava e sai com 4, em vez de ficar vivo sem escrever.
func TestDaemonSaiQuandoOBancoPassaAExigirMais(t *testing.T) {
	for _, tc := range []struct{ nome, leitor, escritor, codigo string }{
		{"escritor", "4", "99", "banco_somente_leitura"},
		{"leitor", "99", "99", "versao_do_banco"},
	} {
		t.Run(tc.nome, func(t *testing.T) {
			c := novaCasa(t)
			var stderr strings.Builder
			cmd := exec.Command(c.plugin, "daemon", "--tick", "200ms", "--intervalo-remoto", "1h")
			cmd.Env = append(os.Environ(), "ORQ_STATE_DIR="+c.estado, "ORQ_HERDR_BIN=/nao/existe/herdr",
				"HERDR_SOCKET_PATH="+filepath.Join(c.raiz, "sem.sock"))
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			saiu := make(chan error, 1)
			go func() { saiu <- cmd.Wait() }()
			defer func() { _ = cmd.Process.Kill() }()
			for i := 0; i < 200; i++ {
				if ativo, _, _ := enriquecer.DaemonAtivo(c.estado); ativo {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			db, err := sql.Open("sqlite", "file:"+filepath.Join(c.estado, "central.db")+"?_pragma=busy_timeout(5000)")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("PRAGMA user_version = 99; UPDATE banco_meta SET valor = '" + tc.leitor + "' WHERE chave = 'leitor_minimo'; UPDATE banco_meta SET valor = '" + tc.escritor + "' WHERE chave = 'escritor_minimo'"); err != nil {
				t.Fatal(err)
			}
			db.Close()
			select {
			case err := <-saiu:
				ee, ok := err.(*exec.ExitError)
				if !ok || ee.ExitCode() != 4 {
					t.Fatalf("saída = %v, quero código 4", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("o daemon continuou vivo com o banco incompatível")
			}
			if !strings.Contains(stderr.String(), tc.codigo) || !strings.Contains(stderr.String(), "orq daemon sai por erro") {
				t.Fatalf("log do daemon: %s", stderr.String())
			}
			if ativo, _, _ := enriquecer.DaemonAtivo(c.estado); ativo {
				t.Fatal("a trava não soltou")
			}
		})
	}
}
