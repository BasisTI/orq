//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package limpeza

import (
	"path/filepath"
	"time"

	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// ArquivoTrava: veja trava_flock.go.
func (r Raizes) ArquivoTrava(u refs.US) string {
	return filepath.Join(r.Trabalho, u.Projeto, "."+refs.Exibir(u.Ref)+".lock")
}

// TravarUS não tem flock nesta plataforma, e o Herdr é só Linux.
func (r Raizes) TravarUS(refs.US, time.Duration) (func(), error) {
	return nil, &output.Error{Code: "plataforma", Cause: "a limpeza precisa de flock (Linux)", Exit: output.ExitUnexpected}
}
