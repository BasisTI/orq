//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package limpeza

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// ArquivoTrava é a trava da US, ao lado da pasta de trabalho (<raiz>/<projeto>/.TG-<ref>.lock) e
// fora dela, porque a pasta de trabalho é o que a limpeza apaga.
func (r Raizes) ArquivoTrava(u refs.US) string {
	return filepath.Join(r.Trabalho, u.Projeto, "."+refs.Exibir(u.Ref)+".lock")
}

// TravarUS toma o flock exclusivo da US. É a exclusão mútua entre o `orq limpar` (que a segura do
// plano ao último item), o `orq recursos gravar` e o `orq recursos reabrir`: quem reabre só
// recria o worktree depois de pegar a trava, e então nenhuma limpeza velha está em andamento.
// espera 0 tenta uma vez; senão repete até esgotar. O kernel solta a trava se o processo morrer.
func (r Raizes) TravarUS(u refs.US, espera time.Duration) (func(), error) {
	path := r.ArquivoTrava(u)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	limite := time.Now().Add(espera)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		if !time.Now().Before(limite) {
			_ = f.Close()
			return nil, &output.Error{Code: "limpeza_em_andamento", Cause: fmt.Sprintf("outra limpeza, gravação ou reabertura de %s segura a trava %s", u.Chave(), path),
				Recovery: "espere terminar e repita", Exit: output.ExitConflict}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
