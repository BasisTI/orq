package semdaemon

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Com ORQ_SEMDAEMON_FILHO, o binário de teste faz o papel de um `orq daemon` que não sai.
func TestMain(m *testing.M) {
	switch os.Getenv("ORQ_SEMDAEMON_FILHO") {
	case "1":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "ignora":
		// Ignora SIGTERM sem gerar filho (o runtime do Go não faz fork) e só avisa que está pronto
		// depois de instalar o Ignore, para o teste não mandar o SIGTERM antes.
		signal.Ignore(syscall.SIGTERM)
		if err := os.WriteFile(os.Getenv("ORQ_SEMDAEMON_PRONTO"), nil, 0o600); err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Cada processo filho tem um único Wait, numa goroutine do helper: dois Wait no mesmo exec.Cmd
// são uma corrida de dados. O teste espera pelo canal devolvido; o Cleanup mata e espera o mesmo
// canal, sem chamar Wait de novo.
func esperar(t *testing.T, cmd *exec.Cmd) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	return done
}

func falso(t *testing.T, estado string) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "daemon")
	cmd.Env = append(os.Environ(), "ORQ_SEMDAEMON_FILHO=1", "ORQ_STATE_DIR="+estado)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd, esperar(t, cmd)
}

// ignoraTerm é o binário de teste reexecutado como um processo que ignora SIGTERM, não gera
// filhos e só devolve depois de avisar que está pronto. O estado vai como argumento, que é onde
// a varredura o acha, como no processo falso do teste do upgrade.
func ignoraTerm(t *testing.T, estado string) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	pronto := filepath.Join(t.TempDir(), "pronto")
	cmd := exec.Command(os.Args[0], "daemon", estado)
	cmd.Env = append(os.Environ(), "ORQ_SEMDAEMON_FILHO=ignora", "ORQ_SEMDAEMON_PRONTO="+pronto)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := esperar(t, cmd)
	for i := 0; i < 500; i++ {
		if _, err := os.Stat(pronto); err == nil {
			return cmd, done
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("o processo que ignora SIGTERM não ficou pronto")
	return nil, nil
}

func raizPrivada(t *testing.T) string {
	t.Helper()
	raiz, err := novaRaiz()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(raiz) })
	return raiz
}

func vivo(pid int) bool { return syscall.Kill(pid, 0) == nil }

// O teste que deixa um daemon vivo é achado, nomeado e encerrado.
func TestAcharEEncerrarDaemonDeixadoDeProposito(t *testing.T) {
	raiz := raizPrivada(t)
	estado := filepath.Join(raiz, "TestQueVaza123", "001", "estado")
	cmd, done := falso(t, estado)

	vazados := Encerrar(raiz)
	if len(vazados) != 1 || vazados[0].PID != cmd.Process.Pid || vazados[0].Teste != "TestQueVaza" || vazados[0].Caminho != estado {
		t.Fatalf("vazados = %+v", vazados)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("o daemon continuou vivo")
	}
	if rel := Relatorio(vazados); !strings.Contains(rel, "TestQueVaza") || !strings.Contains(rel, "FAIL") {
		t.Fatalf("relatório = %q", rel)
	}
	if again := Procurar(raiz); len(again) != 0 {
		t.Fatalf("sobrou %+v", again)
	}
}

// Daemon fora da raiz (o real, o de outro pacote) e processo que não é `daemon` ficam em paz.
func TestIgnoraProcessoForaDaRaiz(t *testing.T) {
	raiz := raizPrivada(t)
	fora, _ := falso(t, filepath.Join(t.TempDir(), "estado"))
	if v := Procurar(raiz); len(v) != 0 {
		t.Fatalf("achou %+v", v)
	}
	if !vivo(fora.Process.Pid) {
		t.Fatal("o daemon de fora foi encerrado")
	}
}

// Um estado cujo nome só começa igual à raiz não está sob ela.
func TestSobNaoCasaPrefixoDeNome(t *testing.T) {
	if sob("/tmp/a", "/tmp/ab/x") || sob("/tmp/a", "/tmp/a") || !sob("/tmp/a", "/tmp/a/x") {
		t.Fatal("sob errado")
	}
}

// Qualquer processo do mesmo UID preso à raiz é achado, e o que ignora SIGTERM leva SIGKILL.
// O processo não tem filhos: o shell com `sleep` expunha, na varredura, o filho entre fork e exec.
func TestEncerraShellQueIgnoraTerm(t *testing.T) {
	raiz, err := novaRaiz()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(raiz)
	cmd, done := ignoraTerm(t, filepath.Join(raiz, "TestDoShell42", "001", "estado"))

	vazados := Encerrar(raiz)
	if len(vazados) != 1 || vazados[0].PID != cmd.Process.Pid || vazados[0].Teste != "TestDoShell" {
		t.Fatalf("vazados = %+v", vazados)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("o shell sobreviveu")
	}
}

// A raiz é sempre um diretório novo desta execução, nunca o TMPDIR herdado, e Procurar recusa
// uma raiz que não seja assim.
func TestRaizEPrivadaENova(t *testing.T) {
	herdado := os.TempDir()
	base := herdado // com TMPDIR longo, a raiz vai para /tmp (o caminho do socket Unix tem limite)
	if len(herdado) > maxTmpdir {
		base = "/tmp"
	}
	a, err := novaRaiz()
	if err != nil {
		t.Fatal(err)
	}
	b, err := novaRaiz()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(a)
	defer os.RemoveAll(b)
	if a == b || a == herdado || !strings.HasPrefix(filepath.Base(a), prefixoRaiz) || !sob(base, a) {
		t.Fatalf("raízes = %q, %q (herdado %q)", a, b, herdado)
	}
	if ls, _ := os.ReadDir(a); len(ls) != 0 {
		t.Fatalf("a raiz nova não está vazia: %v", ls)
	}
	for _, ruim := range []string{herdado, "/", "/tmp"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Procurar(%q) não recusou", ruim)
				}
			}()
			Procurar(ruim)
		}()
	}
}

// O ambiente de teste não enxerga a máquina: HOME e XDG dentro da raiz privada (o orq.env real
// fica fora do alcance) e nenhuma ORQ_*/HERDR_* herdada, como ORQ_WEB do daemon real.
func TestIsolarAmbiente(t *testing.T) {
	for _, k := range []string{"HOME", "TMPDIR", "XDG_RUNTIME_DIR", "XDG_CONFIG_HOME", "ORQ_WEB", "HERDR_SOCKET_PATH", "GOCACHE", "GOPATH"} {
		t.Setenv(k, os.Getenv(k)) // o t.Setenv restaura o valor de agora no fim
	}
	t.Setenv("ORQ_WEB", "unix:/run/user/1000/orq.sock")
	t.Setenv("HERDR_SOCKET_PATH", "/real/herdr.sock")
	t.Setenv("XDG_CONFIG_HOME", "/real/config")
	t.Setenv("HOME", "/home/real")
	raiz, err := novaRaiz()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(raiz)
	if err := isolarAmbiente(raiz); err != nil {
		t.Fatal(err)
	}
	for k, quer := range map[string]string{"HOME": filepath.Join(raiz, "home"), "TMPDIR": raiz, "XDG_RUNTIME_DIR": filepath.Join(raiz, "run"),
		"ORQ_WEB": "", "HERDR_SOCKET_PATH": "", "XDG_CONFIG_HOME": ""} {
		if got := os.Getenv(k); got != quer {
			t.Errorf("%s = %q, queria %q", k, got, quer)
		}
	}
}

// Um TMPDIR longo não leva a raiz nem o DirCurto para um caminho que estoura o socket Unix.
func TestRaizEDirCurtoComTmpdirLongo(t *testing.T) {
	longo := filepath.Join(t.TempDir(), strings.Repeat("x", 80))
	if err := os.MkdirAll(longo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", longo)
	raiz, err := novaRaiz()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(raiz)
	if len(raiz) > maxTmpdir {
		t.Errorf("raiz longa (%d): %s", len(raiz), raiz)
	}
	dir := DirCurto(t, "orq")
	if len(filepath.Join(dir, "web.sock")) > 60 {
		t.Errorf("DirCurto longo: %s", dir)
	}
}

// Parar encerra o daemon de teste e o que mais estiver preso ao diretório, e espera sumir.
func TestPararEsperaOProcessoSumir(t *testing.T) {
	raiz, err := novaRaiz()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(raiz)
	t.Setenv("TMPDIR", raiz)
	estado := filepath.Join(raiz, "casa", "estado")
	if err := os.MkdirAll(estado, 0o755); err != nil {
		t.Fatal(err)
	}
	_, done := falso(t, estado)
	for i := 0; i < 100 && len(procurar(filepath.Join(raiz, "casa"))) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if v := Parar(filepath.Join(raiz, "casa")); len(v) != 1 {
		t.Fatalf("vazados = %+v", v)
	}
	if v := procurar(filepath.Join(raiz, "casa")); len(v) != 0 {
		t.Fatalf("ainda vivo: %+v", v)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("o daemon continuou vivo")
	}
}
