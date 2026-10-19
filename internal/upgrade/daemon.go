package upgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/output"
)

// Daemon descreve o daemon em execução, lido da trava e de /proc; nil sem daemon. A versão sai
// de /proc/<pid>/exe, que continua apontando para o arquivo antigo depois que o go install ou o
// build do plugin o trocam.
func (a Ambiente) Daemon() *Binario {
	ativo, pid, err := enriquecer.DaemonAtivo(a.Dir)
	if err != nil || !ativo {
		return nil
	}
	b := &Binario{Rotulos: []string{RotuloDaemon}, PID: pid, Caminho: filepath.Join("/proc", pid, "exe")}
	if alvo, err := os.Readlink(b.Caminho); err == nil {
		b.real = alvo
	}
	return b
}

// argsDoDaemon lê do processo o --tick e o --intervalo-remoto com que ele sobe, para o novo
// daemon repetir.
func argsDoDaemon(pid string) []string {
	b, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil {
		return nil
	}
	args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	var out []string
	for i := 1; i < len(args); i++ {
		for _, flag := range []string{"--tick", "--intervalo-remoto"} {
			var v string
			switch {
			case args[i] == flag && i+1 < len(args):
				v = args[i+1]
				i++
			case strings.HasPrefix(args[i], flag+"="):
				v = strings.TrimPrefix(args[i], flag+"=")
			default:
				continue
			}
			if _, err := time.ParseDuration(v); err == nil {
				out = append(out, flag, v)
			}
		}
	}
	return out
}

// confirmarDaemon confere que o pid da trava é o daemon do orq deste diretório de estado, antes
// de qualquer sinal: processo do mesmo usuário, executável chamado orq, `daemon` como
// subcomando, ORQ_STATE_DIR (quando definido) igual ao alvo e dono da flock de daemon.lock
// segundo /proc/locks. Na dúvida, recusa: um pid reaproveitado ou um arquivo de trava adulterado
// não pode levar um SIGTERM a outro programa.
func (a Ambiente) confirmarDaemon(pid string) error {
	recusa := func(motivo string) error {
		return &output.Error{
			Code:     "daemon_alheio",
			Cause:    fmt.Sprintf("o pid %q da trava de %s não é o daemon do orq deste diretório de estado: %s", pid, a.Dir, motivo),
			Recovery: "nada foi sinalizado; confira com `ps` quem é esse processo e, se o daemon do orq morreu, apague o daemon.lock órfão e rode `orq upgrade` de novo",
			Exit:     output.ExitConflict,
		}
	}
	n, err := strconv.Atoi(pid)
	if err != nil || n <= 1 {
		return recusa("pid inválido")
	}
	raiz := filepath.Join("/proc", pid)
	st, err := os.Stat(raiz)
	if err != nil {
		return recusa("o processo não existe")
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); !ok || int(sys.Uid) != os.Getuid() {
		return recusa("pertence a outro usuário")
	}
	exe, err := os.Readlink(filepath.Join(raiz, "exe"))
	if err != nil {
		return recusa("não consegui ler o executável")
	}
	if nome := filepath.Base(strings.TrimSuffix(exe, " (deleted)")); !strings.HasPrefix(nome, "orq") {
		return recusa("o executável é " + exe)
	}
	b, err := os.ReadFile(filepath.Join(raiz, "cmdline"))
	if err != nil {
		return recusa("não consegui ler a linha de comando")
	}
	args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	if len(args) < 2 || args[1] != "daemon" {
		return recusa("a linha de comando não é `orq daemon`")
	}
	for _, kv := range ambienteDoProcesso(pid) {
		if v, ok := strings.CutPrefix(kv, "ORQ_STATE_DIR="); ok && filepath.Clean(v) != filepath.Clean(a.Dir) {
			return recusa("ele usa o diretório de estado " + v)
		}
	}
	if ativo, dono, _ := enriquecer.DaemonAtivo(a.Dir); !ativo || dono != pid {
		return recusa("a trava não aponta para ele")
	}
	if !seguraTrava(n, filepath.Join(a.Dir, enriquecer.ArquivoTravaDaemon)) {
		return recusa("ele não é o dono da flock de " + enriquecer.ArquivoTravaDaemon)
	}
	return nil
}

// ConfirmarDaemon é confirmarDaemon para quem só quer saber se o pid da trava de dir é o daemon
// do orq daquele diretório (o orq plugin doctor, antes de ler o ambiente dele). Não manda sinal.
func ConfirmarDaemon(dir, pid string) error { return Ambiente{Dir: dir}.confirmarDaemon(pid) }

// seguraTrava diz se o pid é o dono de uma flock sobre o arquivo, lendo /proc/locks.
func seguraTrava(pid int, arquivo string) bool {
	st, err := os.Stat(arquivo)
	if err != nil {
		return false
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	b, err := os.ReadFile("/proc/locks")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		if travaNaLinha(l, pid, uint64(sys.Dev), sys.Ino) {
			return true
		}
	}
	return false
}

// ambienteDoProcesso lê o ambiente de um processo; nil quando não dá.
func ambienteDoProcesso(pid string) []string {
	b, err := os.ReadFile(filepath.Join("/proc", pid, "environ"))
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
}

// copiarExecutavel guarda uma cópia do binário do daemon antigo, para subir de novo o que
// estava rodando se nenhum dos novos subir.
func copiarExecutavel(origem, destino string) error {
	if err := os.MkdirAll(filepath.Dir(destino), 0o700); err != nil {
		return err
	}
	in, err := os.Open(origem)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destino, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Parar manda SIGTERM ao daemon e espera a trava soltar. Não usa SIGKILL: o daemon limpa os
// tokens de sidebar ao sair, e se ele não sai o upgrade o deixa rodando.
func (a Ambiente) parar(ctx context.Context, pid string, espera time.Duration) error {
	if err := a.confirmarDaemon(pid); err != nil {
		return err
	}
	n, _ := strconv.Atoi(pid)
	p, err := os.FindProcess(n)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("SIGTERM ao pid %d: %w", n, err)
	}
	limite := time.NewTimer(espera)
	defer limite.Stop()
	tique := time.NewTicker(50 * time.Millisecond)
	defer tique.Stop()
	for {
		if ativo, _, _ := enriquecer.DaemonAtivo(a.Dir); !ativo {
			return nil
		}
		select {
		case <-tique.C:
		case <-limite.C:
			return fmt.Errorf("o daemon (pid %d) não soltou a trava em %s", n, espera)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// subir roda `<bin> daemon --detach` com o ambiente e os argumentos dados. Devolve o pid.
func (a Ambiente) subir(ctx context.Context, bin string, env []string, args []string) (string, error) {
	argv := append([]string{"daemon", "--detach", "--output", "json"}, args...)
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	out, err := rodarEm(ctx, "", append(env, "ORQ_STATE_DIR="+a.Dir), bin, argv...)
	if err != nil {
		return "", fmt.Errorf("%s daemon --detach: %w: %s", bin, err, causaDe(out))
	}
	var r struct{ Estado, PID string }
	if err := json.NewDecoder(bytes.NewReader([]byte(out))).Decode(&r); err != nil || r.PID == "" {
		return "", fmt.Errorf("%s daemon --detach: resposta inesperada %s", bin, resumo(out))
	}
	return r.PID, nil
}

// causaDe tira da saída o `cause` do envelope de erro do filho (JSON), quando há; senão resume.
func causaDe(out string) string {
	var e struct{ Error struct{ Cause string } }
	if json.Unmarshal([]byte(out), &e) == nil && e.Error.Cause != "" {
		return resumo(e.Error.Cause)
	}
	return resumo(out)
}

func resumo(s string) string {
	t := strings.Join(strings.Fields(s), " ")
	if r := []rune(t); len(r) > 300 {
		t = string(r[:300]) + "…"
	}
	if t == "" {
		return "sem saída"
	}
	return t
}

// travaNaLinha diz se uma linha de /proc/locks é a flock do pid sobre o arquivo de dispositivo
// dev (st_dev) e inode ino: "1: FLOCK  ADVISORY  WRITE 12345 fc:00:131 0 EOF". Compara
// major:minor (em hexadecimal) e inode, porque o mesmo inode existe em outros sistemas de arquivos.
func travaNaLinha(l string, pid int, dev, ino uint64) bool {
	f := strings.Fields(l)
	if len(f) < 6 || f[1] != "FLOCK" || f[4] != strconv.Itoa(pid) {
		return false
	}
	partes := strings.Split(f[5], ":")
	if len(partes) != 3 {
		return false
	}
	maior, err1 := strconv.ParseUint(partes[0], 16, 64)
	menor, err2 := strconv.ParseUint(partes[1], 16, 64)
	if err1 != nil || err2 != nil || partes[2] != strconv.FormatUint(ino, 10) {
		return false
	}
	// st_dev no Linux: major em dev[8..19] e dev[32..], minor em dev[0..7] e dev[12..31].
	dmaior := (dev>>8)&0xfff | (dev>>32)&^uint64(0xfff)
	dmenor := dev&0xff | (dev>>12)&^uint64(0xff)
	return maior == dmaior && menor == dmenor
}
