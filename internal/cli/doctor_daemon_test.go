package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/enriquecer"
)

// cenarioDoctor monta um doctor com um daemon "vivo": a trava de daemon.lock é segurada pelo
// próprio teste (com o pid gravado nela) e o /proc é uma árvore falsa com o ambiente e a hora de
// partida desse pid. Nada é sinalizado e nenhum /proc real é lido.
type cenarioDoctor struct {
	t      *testing.T
	home   string
	estado string
	proc   string
	cfg    string
	shell  map[string]string // ambiente do processo do doctor
	conf   func(dir, pid string) error
}

func novoCenarioDoctor(t *testing.T) *cenarioDoctor {
	t.Helper()
	dir, env := novoHerdrCentral(t)
	c := &cenarioDoctor{t: t, home: t.TempDir(), estado: t.TempDir(), proc: t.TempDir(), cfg: filepath.Join(dir, "config.toml"), shell: env}
	c.shell["HOME"], c.shell["USER"], c.shell["ORQ_STATE_DIR"] = c.home, "ana", c.estado
	delete(c.shell, "HERDR_PLUGIN_CONFIG_DIR")
	escrever(t, filepath.Join(dir, "plugins.json"), `{"result":{"plugins":[{"plugin_id":"orq","enabled":true,"plugin_root":"/x/orq"}]}}`)
	ok(t, c.shell, "plugin", "configurar", "--config", c.cfg)
	// Todas as ferramentas em ~/.local/bin falso, para o teste não depender das da máquina; o
	// herdr é o falso do teste, e ORQ_HERDR_BIN sai do ambiente.
	bin := filepath.Join(c.home, ".local", "bin")
	for _, n := range ferramentasDoctor {
		scriptFalso(t, filepath.Join(bin, n), "exit 0\n")
	}
	herdr, err := os.ReadFile(c.shell["ORQ_HERDR_BIN"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "herdr"), herdr, 0o755); err != nil {
		t.Fatal(err)
	}
	delete(c.shell, "ORQ_HERDR_BIN")
	c.conf = func(string, string) error { return nil }
	return c
}

func (c *cenarioDoctor) orqEnv() string {
	return filepath.Join(c.home, ".config", "herdr", "plugins", "config", "orq", ArquivoAmbientePlugin)
}

// daemon segura a trava como o daemon faz e cria o /proc falso dele. btime e ticks decidem a hora
// em que ele "subiu"; environ é o ambiente do processo dele.
func (c *cenarioDoctor) daemon(environ map[string]string, subiuEm time.Time) {
	c.t.Helper()
	pid := strconv.Itoa(os.Getpid())
	lock, err := os.OpenFile(filepath.Join(c.estado, enriquecer.ArquivoTravaDaemon), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		c.t.Fatal(err)
	}
	c.t.Cleanup(func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); lock.Close() })
	if _, err := lock.WriteString(pid); err != nil {
		c.t.Fatal(err)
	}
	var env bytes.Buffer
	for k, v := range environ {
		fmt.Fprintf(&env, "%s=%s\x00", k, v)
	}
	// Uma variável que o doctor não pode guardar nem mostrar.
	env.WriteString("SEGREDO_DO_TESTE=nao-mostrar\x00")
	escreverMkdir(c.t, filepath.Join(c.proc, pid, "environ"), env.String())
	boot := subiuEm.Add(-time.Hour)
	escreverMkdir(c.t, filepath.Join(c.proc, "stat"), fmt.Sprintf("cpu 1 2 3\nbtime %d\n", boot.Unix()))
	escreverMkdir(c.t, filepath.Join(c.proc, pid, "stat"), pid+" (orq daemon) S "+strings.Repeat("0 ", 19)+strconv.FormatInt(int64(time.Hour/(10*time.Millisecond)), 10)+" 0 0\n")
}

func (c *cenarioDoctor) doctor() map[string]Item {
	c.t.Helper()
	var out, errOut bytes.Buffer
	env := func(k string) string { return c.shell[k] }
	comArquivo, arq := ambientePlugin(env)
	a := &App{Out: &out, Err: &errOut, Env: comArquivo, envProcesso: env, arquivoEnv: arq, procRaiz: c.proc, confirmarDaemon: c.conf}
	if code := a.Run([]string{"plugin", "doctor", "--config", c.cfg}); code != 0 {
		c.t.Fatalf("exit %d %s", code, errOut.String())
	}
	if strings.Contains(out.String(), "nao-mostrar") {
		c.t.Fatalf("o doctor mostrou uma variável que não é da lista: %s", out.String())
	}
	var d Doctor
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		c.t.Fatal(err)
	}
	m := map[string]Item{}
	for _, it := range d.Itens {
		m[it.Nome] = it
	}
	return m
}

// Daemon vivo: o doctor compara com o ambiente dele, lido de /proc, e não com o do shell.
func TestDoctorComparaComODaemonEmExecucao(t *testing.T) {
	c := novoCenarioDoctor(t)
	gh := filepath.Join(t.TempDir(), "gh")
	scriptFalso(t, gh, "exit 0\n")
	antigo := filepath.Join(t.TempDir(), "gh-antigo")
	scriptFalso(t, antigo, "exit 0\n")
	// O orq.env de agora aponta o gh novo; o daemon subiu com o antigo no ambiente.
	escreverMkdir(t, c.orqEnv(), "ORQ_GH_BIN="+gh+"\n")
	c.daemon(map[string]string{"HOME": c.home, "USER": "ana", "ORQ_GH_BIN": antigo, "ORQ_STATE_DIR": c.estado}, time.Now().Add(2*time.Hour))
	m := c.doctor()
	it := m["daemon e shell"]
	if it.Estado != "aviso" || !strings.Contains(it.Detalhe, "gh: o daemon usa "+antigo+", o shell usa ") || !strings.Contains(it.Detalhe, "conferido em /proc") {
		t.Fatalf("daemon e shell = %+v", it)
	}
	if strings.Contains(it.Detalhe, "simulado") {
		t.Errorf("com daemon legível não é simulação: %s", it.Detalhe)
	}
	// O orq.env é mais velho que o daemon (que "subiu" no futuro): nada a avisar.
	if _, aviso := m["orq.env e daemon"]; aviso {
		t.Errorf("avisou mudança do orq.env sem ela: %+v", m["orq.env e daemon"])
	}
}

// O orq.env mudou depois da subida do daemon: avisa que é preciso reiniciar; com tudo igual,
// o item "daemon e shell" fica ok.
func TestDoctorAvisaOrqEnvMaisNovoQueODaemon(t *testing.T) {
	c := novoCenarioDoctor(t)
	gh := filepath.Join(t.TempDir(), "gh")
	scriptFalso(t, gh, "exit 0\n")
	escreverMkdir(t, c.orqEnv(), "ORQ_GH_BIN="+gh+"\n")
	// Daemon que subiu há duas horas, com o mesmo ambiente do arquivo.
	c.daemon(map[string]string{"HOME": c.home, "USER": "ana", "ORQ_STATE_DIR": c.estado}, time.Now().Add(-2*time.Hour))
	m := c.doctor()
	if it := m["daemon e shell"]; it.Estado != "ok" || !strings.Contains(it.Detalhe, "git, docker, go") {
		t.Errorf("daemon e shell = %+v", it)
	}
	if it := m["orq.env e daemon"]; it.Estado != "aviso" || !strings.Contains(it.Detalhe, "reinicie o daemon") {
		t.Errorf("orq.env e daemon = %+v", it)
	}
}

// Sem ambiente legível ou sem daemon, é simulação e o item diz isso; nunca "mesmo caminho" seco.
func TestDoctorSimulacaoQuandoNaoConfereODaemon(t *testing.T) {
	c := novoCenarioDoctor(t)
	if it := c.doctor()["daemon e shell"]; it.Estado != "aviso" || !strings.Contains(it.Detalhe, "não há daemon ativo") || !strings.Contains(it.Detalhe, "o daemon em execução não foi conferido") {
		t.Errorf("sem daemon: %+v", it)
	}
	c.daemon(map[string]string{"HOME": c.home}, time.Now())
	if err := os.Remove(filepath.Join(c.proc, strconv.Itoa(os.Getpid()), "environ")); err != nil {
		t.Fatal(err)
	}
	if it := c.doctor()["daemon e shell"]; it.Estado != "aviso" || !strings.Contains(it.Detalhe, "não é legível") || !strings.Contains(it.Detalhe, "não foi conferido") {
		t.Errorf("ambiente ilegível: %+v", it)
	}
	c.conf = func(string, string) error { return fmt.Errorf("outro programa") }
	if it := c.doctor()["daemon e shell"]; it.Estado != "aviso" || !strings.Contains(it.Detalhe, "não foi confirmado") {
		t.Errorf("pid não confirmado: %+v", it)
	}
}

// Dois erros de resolução não são "mesmo caminho": o item avisa e nomeia a ferramenta.
func TestDoctorNenhumLadoResolve(t *testing.T) {
	c := novoCenarioDoctor(t)
	escreverMkdir(t, c.orqEnv(), "ORQ_GH_BIN=/nao/existe/gh\nORQ_DOCKER_BIN=/nao/existe/docker\n")
	m := c.doctor()
	it := m["daemon e shell"]
	if it.Estado != "aviso" || !strings.Contains(it.Detalhe, "nenhum dos dois resolve") || !strings.Contains(it.Detalhe, "gh") || !strings.Contains(it.Detalhe, "docker") || strings.Contains(it.Detalhe, "resolvem o mesmo caminho") {
		t.Errorf("daemon e shell = %+v", it)
	}
	for _, n := range []string{"git", "docker", "go"} {
		if _, ok := m["binario "+n]; !ok {
			t.Errorf("sem item binario %s", n)
		}
	}
}

// As raízes e a limpeza automática: valor efetivo e origem no shell e no daemon; divergência avisa.
func TestDoctorRaizesNoShellENoDaemon(t *testing.T) {
	c := novoCenarioDoctor(t)
	trab, tmp := t.TempDir(), t.TempDir()
	c.shell["ORQ_TRABALHO_DIR"] = trab
	escreverMkdir(t, c.orqEnv(), "ORQ_TMP_DIR="+tmp+"\nORQ_LIMPEZA_AUTOMATICA=0\n")
	outro := t.TempDir()
	c.daemon(map[string]string{"HOME": c.home, "ORQ_STATE_DIR": c.estado, "ORQ_TRABALHO_DIR": outro}, time.Now().Add(2*time.Hour))
	m := c.doctor()
	for chave, quer := range map[string][]string{
		"raiz ORQ_STATE_DIR":          {"shell " + c.estado + " (processo)", "daemon " + c.estado + " (processo)"},
		"raiz ORQ_TRABALHO_DIR":       {"shell " + trab + " (processo)", "daemon " + outro + " (processo)"},
		"raiz ORQ_TMP_DIR":            {"shell " + tmp + " (orq.env)", "daemon " + tmp + " (orq.env)"},
		"raiz ORQ_LIMPEZA_AUTOMATICA": {"shell desligada (orq.env)", "daemon desligada (orq.env)"},
	} {
		for _, w := range quer {
			if !strings.Contains(m[chave].Detalhe, w) {
				t.Errorf("%s = %q; falta %q", chave, m[chave].Detalhe, w)
			}
		}
	}
	if m["raiz ORQ_TRABALHO_DIR"].Estado != "aviso" || m["raiz ORQ_STATE_DIR"].Estado != "ok" {
		t.Errorf("estados: %+v %+v", m["raiz ORQ_TRABALHO_DIR"], m["raiz ORQ_STATE_DIR"])
	}
	if it := m["raízes"]; it.Estado != "aviso" || !strings.Contains(it.Detalhe, "ORQ_TRABALHO_DIR") || strings.Contains(it.Detalhe, "ORQ_TMP_DIR:") {
		t.Errorf("raízes = %+v", it)
	}
	// Padrão: nada definido em lugar nenhum.
	c2 := novoCenarioDoctor(t)
	if d := c2.doctor()["raiz ORQ_TMP_DIR"]; !strings.Contains(d.Detalhe, "shell /tmp/orq (padrão)") {
		t.Errorf("padrão = %+v", d)
	}
}
