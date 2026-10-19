package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// tuisAbertos acha os `orq tui` em execução que não estão na versão de referência. Nem o Herdr
// 0.9.3 tem como reabrir um pane com outro comando, então o upgrade só avisa.
func tuisAbertos(referencia string) []TUI { return tuisEm("/proc", referencia) }

// tuisEm é tuisAbertos sobre uma árvore /proc em raiz.
func tuisEm(raiz, referencia string) []TUI {
	tuis := []TUI{}
	entradas, err := filepath.Glob(filepath.Join(raiz, "[0-9]*", "cmdline"))
	if err != nil {
		return tuis
	}
	versoes := map[string]Info{}
	for _, e := range entradas {
		b, err := os.ReadFile(e)
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		if len(args) < 2 || filepath.Base(args[0]) != "orq" {
			continue
		}
		tui := false
		for _, a := range args[1:] {
			if a == "tui" {
				tui = true
				break
			}
		}
		if !tui {
			continue
		}
		pid := filepath.Base(filepath.Dir(e))
		if n, err := strconv.Atoi(pid); err != nil || n == os.Getpid() && raiz == "/proc" {
			continue
		}
		exe := filepath.Join(raiz, pid, "exe")
		alvo, err := os.Readlink(exe)
		if err != nil {
			continue
		}
		i, ok := versoes[alvo]
		if !ok {
			i, _ = Consultar(context.Background(), exe)
			versoes[alvo] = i
		}
		if i.Commit != "" && MesmoCommit(i.Commit, referencia) {
			continue
		}
		tuis = append(tuis, TUI{PID: pid, Versao: i.Versao})
	}
	return tuis
}
