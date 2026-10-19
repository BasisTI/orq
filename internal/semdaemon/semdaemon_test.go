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

func vivo(pid int) bool { return syscall.Kill(pid, 0) == nil }

// O teste que deixa um daemon vivo é achado, nomeado e encerrado.
func TestAcharEEncerrarDaemonDeixadoDeProposito(t *testing.T) {
	raiz := t.TempDir()
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
	raiz := t.TempDir()
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
