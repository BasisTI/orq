//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package cli

import "os/exec"

func desacoplar(*exec.Cmd) {}
