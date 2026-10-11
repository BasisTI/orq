//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package central

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// travarInicio segura um flock exclusivo em path enquanto o banco é aberto, posto em WAL e
// migrado. A troca do journal para WAL num banco novo devolve SQLITE_BUSY na hora, sem passar
// pelo busy_timeout, quando outro processo faz o mesmo; a trava serializa essa janela.
func travarInicio(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	inicio := time.Now()
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, err
		}
		if time.Since(inicio) >= busyTimeout {
			_ = f.Close()
			return nil, ocupado(errors.New("trava de inicialização "+path+" ocupada (SQLITE_BUSY)"), inicio)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}
