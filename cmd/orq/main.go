package main

import (
	"os"

	"github.com/BasisTI/orq/internal/cli"
	"golang.org/x/term"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, term.IsTerminal(int(os.Stdout.Fd()))))
}
