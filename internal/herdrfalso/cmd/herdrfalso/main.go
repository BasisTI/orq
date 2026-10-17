// Command herdrfalso é o herdr falso dos testes; o estado e o log ficam em $ORQ_TEST_HERDR_DIR.
package main

import (
	"os"

	"github.com/BasisTI/orq/internal/herdrfalso"
)

func main() { os.Exit(herdrfalso.Main(os.Args[1:], os.Getenv("ORQ_TEST_HERDR_DIR"))) }
