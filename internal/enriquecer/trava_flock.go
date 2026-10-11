//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package enriquecer

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/BasisTI/orq/internal/output"
)

// TravarDaemon segura o flock de daemon.lock no diretório de estado enquanto o daemon roda e
// grava o pid nele. Um segundo daemon não espera: sai com daemon_ativo. O kernel solta a trava
// se o processo morrer, então não há trava velha para limpar.
func TravarDaemon(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, ArquivoTravaDaemon)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, err
		}
		pid := "?"
		if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
			pid = strings.TrimSpace(string(b))
		}
		return nil, &output.Error{Code: "daemon_ativo", Cause: "outro orq daemon já roda com este diretório de estado (pid " + pid + ", trava " + path + ")",
			Recovery: "só roda um daemon por máquina; use o que está no ar ou pare-o (SIGTERM) antes de subir outro", Exit: output.ExitConflict}
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return func() {
		_ = f.Truncate(0)
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// DaemonAtivo diz se algum processo segura a trava do daemon em dir e, se sim, o pid gravado
// nela. Testa a trava sem esperar e a solta na hora.
func DaemonAtivo(dir string) (ativo bool, pid string, err error) {
	path := filepath.Join(dir, ArquivoTravaDaemon)
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return false, "", err
		}
		b, _ := os.ReadFile(path)
		return true, strings.TrimSpace(string(b)), nil
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, "", nil
}
