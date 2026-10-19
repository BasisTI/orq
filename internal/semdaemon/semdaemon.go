// Package semdaemon é a rede de segurança dos testes que sobem um `orq daemon`, ou qualquer
// processo falso, direta ou indiretamente (upgrade, --detach, subprocesso): no fim da suíte do
// pacote, procura os processos ainda vivos presos aos temporários dele, encerra e falha a suíte
// nomeando o teste.
//
// A raiz é privada: Main cria, a cada execução, um diretório novo (os.MkdirTemp) e o põe no
// TMPDIR, nunca varre o TMPDIR herdado. Tudo o que está sob ela foi criado por esta execução,
// então qualquer processo do mesmo UID preso a ela (pela linha de comando, pelo executável ou
// pelo ORQ_STATE_DIR) é alvo legítimo, seja ou não um orq. Processo de outro UID nunca é
// sinalizado.
//
// Só os testes importam este pacote. Ele varre /proc, então só funciona em Linux; em outro
// sistema a varredura não acha nada.
package semdaemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Vazado é um daemon que sobrou vivo debaixo da raiz de temporários do pacote.
type Vazado struct {
	PID     int
	Teste   string // primeiro componente sob a raiz, que o t.TempDir() deriva do nome do teste
	Caminho string // o executável ou o ORQ_STATE_DIR que prendeu o daemon à raiz
}

// Main roda a suíte do pacote com uma raiz de temporários própria (TMPDIR), para que a varredura
// só enxergue o que este pacote subiu e não o de outro pacote rodando em paralelo. Devolve o
// código de saída para o os.Exit do TestMain: 1 se a suíte passou mas deixou daemon vivo.
func Main(m *testing.M) int {
	raiz, err := novaRaiz()
	if err != nil {
		fmt.Fprintln(os.Stderr, "semdaemon:", err)
		return 1
	}
	os.Setenv("TMPDIR", raiz)
	code := m.Run()
	vazados := Encerrar(raiz)
	if len(vazados) > 0 {
		fmt.Fprint(os.Stderr, Relatorio(vazados))
		if code == 0 {
			code = 1
		}
	}
	os.RemoveAll(raiz)
	return code
}

const prefixoRaiz = "orq-teste-"

// novaRaiz cria o diretório privado desta execução, sempre novo, com o caminho real (sem links).
func novaRaiz() (string, error) {
	raiz, err := os.MkdirTemp("", prefixoRaiz)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(raiz)
}

// Relatorio descreve os daemons vazados, um por linha, com o teste que os deixou.
func Relatorio(vazados []Vazado) string {
	var b strings.Builder
	b.WriteString("FAIL: daemon do orq deixado vivo pelos testes (encerrado pela suíte):\n")
	for _, v := range vazados {
		fmt.Fprintf(&b, "  pid %d, teste %q, preso a %s\n", v.PID, v.Teste, v.Caminho)
	}
	return b.String()
}

// Procurar acha os processos do mesmo UID vivos cuja linha de comando, executável ou
// ORQ_STATE_DIR estão sob raiz. Recusa (panic) uma raiz que não seja a privada de novaRaiz,
// para que um TMPDIR herdado nunca vire alvo.
func Procurar(raiz string) []Vazado {
	if !strings.HasPrefix(filepath.Base(raiz), prefixoRaiz) {
		panic("semdaemon: a raiz não é privada: " + raiz)
	}
	entradas, _ := os.ReadDir("/proc")
	var out []Vazado
	for _, e := range entradas {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		if caminho, ok := daemonSob(pid, raiz); ok {
			out = append(out, Vazado{PID: pid, Teste: testeDe(raiz, caminho), Caminho: caminho})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// Encerrar procura, manda SIGTERM aos daemons achados (o daemon limpa o que publicou ao sair),
// espera um pouco, usa SIGKILL nos que ficaram e devolve o que achou.
func Encerrar(raiz string) []Vazado {
	vazados := Procurar(raiz)
	for _, v := range vazados {
		_ = syscall.Kill(v.PID, syscall.SIGTERM)
	}
	limite := time.Now().Add(3 * time.Second)
	for time.Now().Before(limite) && len(Procurar(raiz)) > 0 {
		time.Sleep(50 * time.Millisecond)
	}
	for _, v := range Procurar(raiz) {
		_ = syscall.Kill(v.PID, syscall.SIGKILL)
	}
	return vazados
}

// daemonSob diz se o pid é um processo do mesmo UID preso a raiz, e por qual caminho.
func daemonSob(pid int, raiz string) (string, bool) {
	dir := filepath.Join("/proc", strconv.Itoa(pid))
	st, err := os.Stat(dir)
	if err != nil {
		return "", false
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); !ok || int(sys.Uid) != os.Getuid() {
		return "", false
	}
	if exe, err := os.Readlink(filepath.Join(dir, "exe")); err == nil && sob(raiz, strings.TrimSuffix(exe, " (deleted)")) {
		return strings.TrimSuffix(exe, " (deleted)"), true
	}
	if b, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
		// Casa o caminho dentro de qualquer argumento, inclusive o texto de um `sh -c`.
		if cmd := strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " "); strings.Contains(cmd, raiz+string(filepath.Separator)) {
			return emTorno(cmd, raiz), true
		}
	}
	if env, err := os.ReadFile(filepath.Join(dir, "environ")); err == nil {
		for _, kv := range strings.Split(string(env), "\x00") {
			if v, ok := strings.CutPrefix(kv, "ORQ_STATE_DIR="); ok && sob(raiz, v) {
				return v, true
			}
		}
	}
	return "", false
}

// emTorno devolve do texto o caminho que começa na raiz, até o próximo espaço ou aspa.
func emTorno(texto, raiz string) string {
	i := strings.Index(texto, raiz)
	fim := strings.IndexAny(texto[i:], " '\"")
	if fim < 0 {
		return texto[i:]
	}
	return texto[i : i+fim]
}

func sob(raiz, caminho string) bool {
	rel, err := filepath.Rel(raiz, filepath.Clean(caminho))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// testeDe tira do caminho o primeiro componente sob a raiz: o t.TempDir() cria
// <raiz>/<NomeDoTeste><número>/001, então o componente carrega o nome do teste.
func testeDe(raiz, caminho string) string {
	rel, err := filepath.Rel(raiz, filepath.Clean(caminho))
	if err != nil {
		return "?"
	}
	nome, _, _ := strings.Cut(rel, string(filepath.Separator))
	return strings.TrimRight(nome, "0123456789")
}
