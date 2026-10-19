package upgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BasisTI/orq/internal/enriquecer"
)

// Daemon descreve o daemon em execução, lido da trava e de /proc; nil sem daemon. A versão sai
// de /proc/<pid>/exe, que continua apontando para o arquivo antigo depois que o go install ou o
// build do plugin o trocam.
func (a Ambiente) Daemon() *Binario {
	ativo, pid, err := enriquecer.DaemonAtivo(a.Dir)
	if err != nil || !ativo {
		return nil
	}
	b := &Binario{Rotulos: []string{RotuloDaemon}, PID: pid, Caminho: filepath.Join("/proc", pid, "exe")}
	if alvo, err := os.Readlink(b.Caminho); err == nil {
		b.real = alvo
	}
	return b
}

// argsDoDaemon lê do processo o --tick e o --intervalo-remoto com que ele sobe, para o novo
// daemon repetir.
func argsDoDaemon(pid string) []string {
	b, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil {
		return nil
	}
	args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	var out []string
	for i := 1; i < len(args); i++ {
		for _, flag := range []string{"--tick", "--intervalo-remoto"} {
			var v string
			switch {
			case args[i] == flag && i+1 < len(args):
				v = args[i+1]
				i++
			case strings.HasPrefix(args[i], flag+"="):
				v = strings.TrimPrefix(args[i], flag+"=")
			default:
				continue
			}
			if _, err := time.ParseDuration(v); err == nil {
				out = append(out, flag, v)
			}
		}
	}
	return out
}

// ehDaemon confere que o pid da trava é mesmo um `orq daemon`, para não mandar sinal a um pid
// reaproveitado.
func ehDaemon(pid string) bool {
	b, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil {
		return false
	}
	args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	for _, a := range args[1:] {
		if a == "daemon" {
			return true
		}
	}
	return false
}

// ambienteDoProcesso lê o ambiente de um processo; nil quando não dá.
func ambienteDoProcesso(pid string) []string {
	b, err := os.ReadFile(filepath.Join("/proc", pid, "environ"))
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
}

// copiarExecutavel guarda uma cópia do binário do daemon antigo, para subir de novo o que
// estava rodando se nenhum dos novos subir.
func copiarExecutavel(origem, destino string) error {
	if err := os.MkdirAll(filepath.Dir(destino), 0o700); err != nil {
		return err
	}
	in, err := os.Open(origem)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destino, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Parar manda SIGTERM ao daemon e espera a trava soltar. Não usa SIGKILL: o daemon limpa os
// tokens de sidebar ao sair, e se ele não sai o upgrade o deixa rodando.
func (a Ambiente) parar(ctx context.Context, pid string, espera time.Duration) error {
	n, err := strconv.Atoi(pid)
	if err != nil || n <= 1 || !ehDaemon(pid) {
		return fmt.Errorf("o pid %q da trava do daemon não é de um orq daemon", pid)
	}
	p, err := os.FindProcess(n)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("SIGTERM ao pid %d: %w", n, err)
	}
	limite := time.NewTimer(espera)
	defer limite.Stop()
	tique := time.NewTicker(50 * time.Millisecond)
	defer tique.Stop()
	for {
		if ativo, _, _ := enriquecer.DaemonAtivo(a.Dir); !ativo {
			return nil
		}
		select {
		case <-tique.C:
		case <-limite.C:
			return fmt.Errorf("o daemon (pid %d) não soltou a trava em %s", n, espera)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// subir roda `<bin> daemon --detach` com o ambiente e os argumentos dados. Devolve o pid.
func (a Ambiente) subir(ctx context.Context, bin string, env []string, args []string) (string, error) {
	argv := append([]string{"daemon", "--detach", "--output", "json"}, args...)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := rodarEm(ctx, "", append(env, "ORQ_STATE_DIR="+a.Dir), bin, argv...)
	if err != nil {
		return "", fmt.Errorf("%s daemon --detach: %w: %s", bin, err, resumo(out))
	}
	var r struct{ Estado, PID string }
	if err := json.NewDecoder(bytes.NewReader([]byte(out))).Decode(&r); err != nil || r.PID == "" {
		return "", fmt.Errorf("%s daemon --detach: resposta inesperada %s", bin, resumo(out))
	}
	return r.PID, nil
}

func resumo(s string) string {
	t := strings.Join(strings.Fields(s), " ")
	if r := []rune(t); len(r) > 300 {
		t = string(r[:300]) + "…"
	}
	if t == "" {
		return "sem saída"
	}
	return t
}
