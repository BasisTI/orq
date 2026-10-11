//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package enriquecer

import "github.com/BasisTI/orq/internal/output"

// TravarDaemon não tem flock nesta plataforma, e o Herdr é só Linux: o daemon não sobe.
func TravarDaemon(string) (func(), error) {
	return nil, &output.Error{Code: "plataforma", Cause: "orq daemon precisa de flock (Linux)", Exit: output.ExitUnexpected}
}
