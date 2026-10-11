package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestMain também serve de processo auxiliar do teste de concorrência: com ORQ_TEST_HELPER,
// o binário de teste vira um orq que repete os argumentos ORQ_TEST_REPETIR vezes.
func TestMain(m *testing.M) {
	if os.Getenv("ORQ_TEST_HELPER") == "1" {
		os.Exit(processoAuxiliar())
	}
	os.Exit(m.Run())
}

func processoAuxiliar() int {
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("ORQ_TEST_ARGS")), &args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 99
	}
	n, _ := strconv.Atoi(os.Getenv("ORQ_TEST_REPETIR"))
	for i := 0; i < n; i++ {
		var out, errOut bytes.Buffer
		call := make([]string, len(args))
		for j, a := range args {
			call[j] = strings.ReplaceAll(a, "{i}", strconv.Itoa(i))
		}
		if code := Main(call, nil, &out, &errOut, os.Getenv, false); code != 0 {
			fmt.Fprintf(os.Stderr, "iteração %d: exit %d: %s", i, code, errOut.String())
			return code
		}
	}
	return 0
}

type resultado struct {
	code     int
	out, err string
}

func rodar(t *testing.T, env map[string]string, args ...string) resultado {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Main(args, nil, &out, &errOut, func(k string) string { return env[k] }, false)
	return resultado{code, out.String(), errOut.String()}
}

func ok(t *testing.T, env map[string]string, args ...string) string {
	t.Helper()
	r := rodar(t, env, args...)
	if r.code != 0 {
		t.Fatalf("orq %v: exit %d\n%s", args, r.code, r.err)
	}
	return r.out
}

func ambiente(t *testing.T) map[string]string {
	return map[string]string{"ORQ_STATE_DIR": t.TempDir(), "HERDR_PANE_ID": "wB:p1"}
}

func TestErroJSONTipado(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a", "--projeto", "colaboradados")
	ok(t, env, "onda", "criar", "o", "--us", "215,216")
	ok(t, env, "us", "status", "215", "done")
	cases := []struct {
		args []string
		exit int
		code string
	}{
		{[]string{"us", "status", "215", "exec"}, 4, "transicao_invalida"},
		{[]string{"us", "status", "999", "exec"}, 5, "nao_encontrado"},
		{[]string{"us", "status", "TG-x", "exec"}, 2, "uso"},
		{[]string{"us", "status", "215"}, 2, "uso"},
		{[]string{"onda", "ver", "abc"}, 2, "uso"},
		{[]string{"pend", "add", "215", "texto"}, 2, "uso"},
		{[]string{"pend", "resolver", "TG-215#P1"}, 5, "nao_encontrado"},
		{[]string{"mr", "add", "215", "https://example.com/x"}, 2, "uso"},
		{[]string{"--flag-inexistente"}, 2, "uso"},
	}
	for _, c := range cases {
		r := rodar(t, env, c.args...)
		var e struct {
			Error struct{ Code, Cause, Recovery string }
		}
		if err := json.Unmarshal([]byte(r.err), &e); err != nil {
			t.Fatalf("%v: stderr não é JSON: %q", c.args, r.err)
		}
		if r.code != c.exit || e.Error.Code != c.code || e.Error.Cause == "" {
			t.Errorf("%v: exit %d code %q, queria %d %q", c.args, r.code, e.Error.Code, c.exit, c.code)
		}
	}
	r := rodar(t, env, "--output", "text", "us", "status", "215", "exec")
	if !strings.HasPrefix(r.err, "erro [transicao_invalida]: ") || !strings.Contains(r.err, "como resolver: ") {
		t.Fatalf("erro em texto = %q", r.err)
	}
}

func TestCoordenadorPeloPane(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a", "--projeto", "p")
	out := ok(t, env, "onda", "criar", "Primeira", "--us", "1,2", "--ordem", "estrita", "--depende", "2:1")
	var o struct {
		ID          int64
		Coordenador string
		US          []struct{ Chave string }
	}
	if err := json.Unmarshal([]byte(out), &o); err != nil || o.Coordenador != "a" || len(o.US) != 2 {
		t.Fatalf("onda = %s", out)
	}
	// Outro pane, sem coordenador registrado: criar onda falha, mas ler e mudar status não.
	outro := map[string]string{"ORQ_STATE_DIR": env["ORQ_STATE_DIR"], "HERDR_PANE_ID": "wB:p9"}
	if r := rodar(t, outro, "onda", "criar", "x", "--us", "3"); r.code != 2 || !strings.Contains(r.err, "coord_nao_resolvido") {
		t.Fatalf("criar sem coordenador = %+v", r)
	}
	ok(t, outro, "us", "status", "p/1", "exec")
	if r := rodar(t, outro, "--coord", "zz", "us", "status", "p/1", "rev"); r.code != 5 {
		t.Fatalf("--coord inexistente = %+v", r)
	}
	ok(t, map[string]string{"ORQ_STATE_DIR": env["ORQ_STATE_DIR"], "ORQ_COORD": "a"}, "onda", "ver")
	prox := ok(t, env, "--output", "text", "onda", "proxima", fmt.Sprint(o.ID))
	if !strings.HasPrefix(prox, "nada elegível: ordem estrita: TG-1 ainda em exec") {
		t.Fatalf("proxima = %q", prox)
	}
}

func TestPendCLI(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a", "--projeto", "p")
	ok(t, env, "onda", "criar", "o", "--us", "312")
	if id := ok(t, env, "--output", "text", "pend", "add", "TG-312", "--tipo", "autorizacao", "instalar o binário", "--pane", "wB:p4"); id != "TG-312#P1\n" {
		t.Fatalf("id = %q", id)
	}
	ok(t, env, "pend", "add", "TG-312", "--tipo", "duvida", "linha\n\x1b[31mfalsa")
	ok(t, env, "pend", "resolver", "TG-312#P1", "--resposta", "pode")
	txt := ok(t, env, "--output", "text", "pend", "list")
	if strings.Count(txt, "\n") != 2 || strings.Contains(txt, "\x1b") {
		t.Fatalf("texto de outro agente quebrou a linha: %q", txt)
	}
	abertas := ok(t, env, "pend", "list", "--abertas", "--coord", "a")
	var ps []struct{ ID string }
	if err := json.Unmarshal([]byte(abertas), &ps); err != nil || len(ps) != 1 || ps[0].ID != "TG-312#P2" {
		t.Fatalf("abertas = %s", abertas)
	}
	if out := ok(t, env, "pend", "list", "--coord", "outro"); strings.TrimSpace(out) != "[]" {
		t.Fatalf("filtro por outro coordenador = %s", out)
	}
}

func TestSnapshotSempreJSON(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a")
	out := ok(t, env, "--output", "text", "snapshot")
	var s map[string]any
	if err := json.Unmarshal([]byte(out), &s); err != nil || s["versao"] != float64(1) {
		t.Fatalf("snapshot = %q", out)
	}
	v := ok(t, env, "version")
	if !strings.Contains(v, `"versao"`) {
		t.Fatalf("version = %q", v)
	}
}

// Dois processos escrevem ao mesmo tempo no mesmo banco: nenhuma escrita se perde, os números
// de pendência não se repetem e cada escrita deixou o seu evento.
func TestConcorrenciaDoisProcessos(t *testing.T) {
	if testing.Short() {
		t.Skip("sobe processos")
	}
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a", "--projeto", "p")
	ok(t, env, "onda", "criar", "o", "--us", "1,2")
	const n = 40
	procs := []struct {
		args []string
	}{
		{[]string{"pend", "add", "TG-1", "--tipo", "duvida", "do processo A, {i}"}},
		{[]string{"pend", "add", "TG-1", "--tipo", "decisao", "do processo B, {i}"}},
		{[]string{"us", "status", "TG-2", "exec", "--rodada", "{i}1"}},
	}
	var wg sync.WaitGroup
	erros := make([]error, len(procs))
	saidas := make([][]byte, len(procs))
	for i, p := range procs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			args, _ := json.Marshal(p.args)
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), "ORQ_TEST_HELPER=1", "ORQ_TEST_REPETIR="+strconv.Itoa(n), "ORQ_TEST_ARGS="+string(args),
				"ORQ_STATE_DIR="+env["ORQ_STATE_DIR"], "HERDR_PANE_ID=wB:p1")
			saidas[i], erros[i] = cmd.CombinedOutput()
		}()
	}
	wg.Wait()
	for i, err := range erros {
		if err != nil {
			t.Fatalf("processo %d: %v\n%s", i, err, saidas[i])
		}
	}
	var ps []struct {
		Numero int
		Texto  string
	}
	if err := json.Unmarshal([]byte(ok(t, env, "pend", "list")), &ps); err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2*n {
		t.Fatalf("%d pendências, queria %d", len(ps), 2*n)
	}
	vistos := map[int]bool{}
	for _, p := range ps {
		if vistos[p.Numero] || p.Numero < 1 || p.Numero > 2*n {
			t.Fatalf("número %d repetido ou fora de 1..%d", p.Numero, 2*n)
		}
		vistos[p.Numero] = true
	}
	var es []struct{ Tipo string }
	if err := json.Unmarshal([]byte(ok(t, env, "diario", "--limite", "0")), &es); err != nil {
		t.Fatal(err)
	}
	conta := map[string]int{}
	for _, e := range es {
		conta[e.Tipo]++
	}
	if conta["pend.add"] != 2*n || conta["us.status"] != n {
		t.Fatalf("eventos = %v", conta)
	}
}
