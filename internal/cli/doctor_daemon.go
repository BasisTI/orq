package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/limpeza"
	"github.com/BasisTI/orq/internal/upgrade"
)

// ferramentasDoctor são as ferramentas que o doctor resolve, na ordem em que as mostra. O herdr
// é o único cuja falta é erro.
var ferramentasDoctor = []string{"herdr", "glab", "gh", "taiga", "git", "docker", "go"}

// raizesDoctor são as opções que o daemon e o coordenador precisam ver iguais.
var raizesDoctor = []string{"ORQ_STATE_DIR", "ORQ_TRABALHO_DIR", "ORQ_TMP_DIR", "ORQ_LIMPEZA_AUTOMATICA"}

// Origens de um valor, no doctor.
const (
	origemProcesso = "processo"
	origemArquivo  = "orq.env"
	origemPadrao   = "padrão"
)

// visao é o ambiente com que um processo (o shell atual ou o daemon) resolve as ferramentas e
// as raízes: o do processo completado pelo orq.env.
type visao struct {
	env     func(string) string
	arquivo map[string]string // chaves do orq.env que valem nessa visão
	proc    func(string) string
}

func novaVisao(proc func(string) string) visao {
	env, arq := ambientePlugin(proc)
	return visao{env: env, arquivo: arq, proc: proc}
}

func (v visao) origemVar(k string) string {
	if v.proc(k) == "" && v.arquivo[k] != "" {
		return origemArquivo
	}
	return ""
}

func (v visao) ferramentas() enriquecer.Ferramentas {
	return enriquecer.Ferramentas{Env: v.env, OrigemVar: v.origemVar, Timeout: 10 * time.Second}
}

// valor devolve o valor efetivo de uma das raízes e de onde veio.
func (v visao) valor(chave string) (valor, origem string) {
	origem = origemProcesso
	if v.proc(chave) == "" {
		origem = origemArquivo
		if v.arquivo[chave] == "" {
			origem = origemPadrao
		}
	}
	switch chave {
	case "ORQ_STATE_DIR":
		d, err := central.DirPadrao(v.env)
		if err != nil {
			return "indisponível", origem
		}
		return d, origem
	case "ORQ_TRABALHO_DIR", "ORQ_TMP_DIR":
		r, err := limpeza.RaizesDe(v.env)
		if err != nil {
			return "inválida", origem
		}
		if chave == "ORQ_TMP_DIR" {
			return r.Tmp, origem
		}
		return r.Trabalho, origem
	}
	if limpeza.LigadaPeloAmbiente(v.env) {
		return "ligada", origem
	}
	return "desligada", origem
}

// daemonVivo é o daemon do orq que segura a trava do diretório de estado, conferido como o
// upgrade confere antes de sinalizar: mesmo usuário, executável orq, `daemon` como subcomando,
// dono da flock.
type daemonVivo struct {
	pid    string
	inicio time.Time
	visao  visao
	// motivo, quando visao não existe, diz por que o daemon não foi conferido.
	motivo string
}

// chavesDoDaemon são as únicas variáveis que o doctor lê de /proc/<pid>/environ. Nenhuma outra
// é guardada nem mostrada.
func chaveDoDaemon(k string) bool {
	switch k {
	case "HOME", "USER", "HERDR_BIN_PATH", "HERDR_PLUGIN_CONFIG_DIR", "XDG_CONFIG_HOME", "XDG_STATE_HOME":
		return true
	}
	return strings.HasPrefix(k, "ORQ_")
}

// localizarDaemon acha o daemon do diretório de estado dir e lê, de /proc, o ambiente dele.
func (a *App) localizarDaemon(dir string) daemonVivo {
	ativo, pid, err := enriquecer.DaemonAtivo(dir)
	if err != nil || !ativo {
		return daemonVivo{motivo: "não há daemon ativo com o estado " + dir}
	}
	d := daemonVivo{pid: pid}
	confirmar := a.confirmarDaemon
	if confirmar == nil {
		confirmar = upgrade.ConfirmarDaemon
	}
	if err := confirmar(dir, pid); err != nil {
		d.motivo = fmt.Sprintf("o pid %s da trava não foi confirmado como o daemon deste estado", pid)
		return d
	}
	raiz := a.procRaiz
	if raiz == "" {
		raiz = "/proc"
	}
	b, err := os.ReadFile(filepath.Join(raiz, pid, "environ"))
	if err != nil || len(b) == 0 {
		d.motivo = fmt.Sprintf("o ambiente do daemon (pid %s) não é legível", pid)
		return d
	}
	m := map[string]string{}
	for _, kv := range strings.Split(strings.TrimRight(string(b), "\x00"), "\x00") {
		if k, v, ok := strings.Cut(kv, "="); ok && chaveDoDaemon(k) {
			m[k] = v
		}
	}
	d.inicio, _ = inicioDoProcesso(raiz, pid)
	d.visao = novaVisao(func(k string) string { return m[k] })
	return d
}

// inicioDoProcesso lê o instante em que o processo nasceu: o starttime de /proc/<pid>/stat
// (em ticks de 1/100 s desde o boot) mais o btime de /proc/stat.
func inicioDoProcesso(raiz, pid string) (time.Time, bool) {
	b, err := os.ReadFile(filepath.Join(raiz, pid, "stat"))
	if err != nil {
		return time.Time{}, false
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return time.Time{}, false
	}
	campos := strings.Fields(s[i+1:]) // campos[0] é o 3º do stat (state)
	if len(campos) < 20 {
		return time.Time{}, false
	}
	ticks, err := strconv.ParseInt(campos[19], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	st, err := os.ReadFile(filepath.Join(raiz, "stat"))
	if err != nil {
		return time.Time{}, false
	}
	for _, l := range strings.Split(string(st), "\n") {
		if v, ok := strings.CutPrefix(l, "btime "); ok {
			bt, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, false
			}
			return time.Unix(bt, 0).Add(time.Duration(ticks) * 10 * time.Millisecond), true
		}
	}
	return time.Time{}, false
}

// doctorDaemonEShell compara o que o daemon e o shell atual resolvem: as sete ferramentas e as
// raízes. A comparação é com o daemon em execução, lendo o ambiente dele em /proc; sem daemon
// legível, é uma simulação (só o orq.env atual fornece ORQ_* ao daemon) e o item diz isso.
func (a *App) doctorDaemonEShell(add func(nome, estado, detalhe string), dir string) {
	shell := novaVisao(a.processo())
	d := a.localizarDaemon(dir)
	real := d.motivo == ""
	var daemon visao
	if real {
		daemon = d.visao
	} else {
		// Simulação: o servidor do Herdr não tem as ORQ_* do shell; só o orq.env as fornece.
		daemon = novaVisao(func(k string) string {
			if strings.HasPrefix(k, "ORQ_") {
				return ""
			}
			return shell.proc(k)
		})
	}
	var difere, semLado []string
	for _, n := range ferramentasDoctor {
		rd, errD := daemon.ferramentas().Resolver(n)
		rs, errS := shell.ferramentas().Resolver(n)
		switch {
		case errD != nil && errS != nil:
			semLado = append(semLado, n)
		case errD != nil:
			difere = append(difere, fmt.Sprintf("%s: o daemon não acha, o shell usa %s", n, rs.Caminho))
		case errS != nil:
			difere = append(difere, fmt.Sprintf("%s: o daemon usa %s, o shell não acha", n, rd.Caminho))
		case rd.Caminho != rs.Caminho:
			difere = append(difere, fmt.Sprintf("%s: o daemon usa %s, o shell usa %s", n, rd.Caminho, rs.Caminho))
		}
	}
	nota := "simulado pelo orq.env atual; o daemon em execução não foi conferido (" + d.motivo + ")"
	if real {
		nota = "daemon pid " + d.pid + " conferido em /proc"
	}
	switch {
	case len(difere) > 0:
		add("daemon e shell", "aviso", strings.Join(difere, "; ")+": ponha a variável ORQ_<NOME>_BIN no orq.env e reinicie o daemon ("+nota+")")
	case len(semLado) > 0:
		add("daemon e shell", "aviso", "nenhum dos dois resolve "+strings.Join(semLado, ", ")+"; o resto resolve o mesmo caminho ("+nota+")")
	case real:
		add("daemon e shell", "ok", "resolvem o mesmo caminho em "+strings.Join(ferramentasDoctor, ", ")+" ("+nota+")")
	default:
		add("daemon e shell", "aviso", "a simulação dá o mesmo caminho em "+strings.Join(ferramentasDoctor, ", ")+", mas "+nota)
	}

	var rdiv []string
	for _, k := range raizesDoctor {
		vs, ovs := shell.valor(k)
		vd, ovd := daemon.valor(k)
		linha := fmt.Sprintf("%s: shell %s (%s), daemon %s (%s)", k, vs, ovs, vd, ovd)
		if vs != vd {
			rdiv = append(rdiv, linha)
		}
		add("raiz "+k, map[bool]string{true: "ok", false: "aviso"}[vs == vd], linha)
	}
	if len(rdiv) > 0 {
		add("raízes", "aviso", "o daemon e o coordenador não veem as mesmas raízes ("+strings.Join(rdiv, "; ")+"); o README exige as mesmas; ponha-as no orq.env e reinicie o daemon")
	}
	if real {
		a.doctorOrqEnvMudou(add, d)
	}
}

// doctorOrqEnvMudou avisa quando o orq.env é mais novo que o daemon: ele só o lê ao subir.
func (a *App) doctorOrqEnvMudou(add func(nome, estado, detalhe string), d daemonVivo) {
	dir := DirConfigPlugin(d.visao.proc)
	if dir == "" || d.inicio.IsZero() {
		return
	}
	st, err := os.Stat(filepath.Join(dir, ArquivoAmbientePlugin))
	if err != nil || !st.ModTime().After(d.inicio) {
		return
	}
	add("orq.env e daemon", "aviso", fmt.Sprintf("o orq.env mudou em %s, depois que o daemon (pid %s) subiu em %s: reinicie o daemon para ele ler a mudança",
		st.ModTime().Format(time.RFC3339), d.pid, d.inicio.Format(time.RFC3339)))
}

// processo é o ambiente do processo, sem o orq.env.
func (a *App) processo() func(string) string {
	if a.envProcesso != nil {
		return a.envProcesso
	}
	return a.Env
}
