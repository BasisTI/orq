package semdaemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Com ORQ_SEMDAEMON_FILHO, o binário de teste faz o papel de um `orq daemon` que não sai.
func TestMain(m *testing.M) {
	if os.Getenv("ORQ_SEMDAEMON_FILHO") == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func falso(t *testing.T, estado string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "daemon")
	cmd.Env = append(os.Environ(), "ORQ_SEMDAEMON_FILHO=1", "ORQ_STATE_DIR="+estado)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

// shellQueIgnoraTerm é um sh que ignora SIGTERM e só tem a raiz na linha de comando, como o
// processo falso do teste do upgrade.
func shellQueIgnoraTerm(t *testing.T, estado string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sh", "-c", `trap "" TERM; while :; do sleep 0.1; done`, estado)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
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
	cmd := falso(t, estado)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()

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
	fora := falso(t, filepath.Join(t.TempDir(), "estado"))
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
func TestEncerraShellQueIgnoraTerm(t *testing.T) {
	raiz, err := novaRaiz()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(raiz)
	cmd := shellQueIgnoraTerm(t, filepath.Join(raiz, "TestDoShell42", "001", "estado"))
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()

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
	if a == b || a == herdado || !strings.HasPrefix(filepath.Base(a), prefixoRaiz) || !sob(herdado, a) {
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
