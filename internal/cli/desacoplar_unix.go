//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package cli

import (
	"os/exec"
	"syscall"
)

// desacoplar põe o filho numa sessão própria: ele sobrevive ao fim de quem o iniciou (o
// startup do plugin) e não recebe o SIGHUP do terminal.
func desacoplar(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
