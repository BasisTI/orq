package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	_ "modernc.org/sqlite"
)

// TestMain também serve de processo auxiliar do teste de concorrência: com ORQ_TEST_HELPER,
// o binário de teste vira um orq que repete os argumentos ORQ_TEST_REPETIR vezes.
func TestMain(m *testing.M) {
	if os.Getenv("ORQ_TEST_HELPER") == "1" {
		os.Exit(processoAuxiliar())
	}
	// Com ORQ_TEST_MAIN, o binário de teste é o orq (o filho de orq daemon --detach).
	if os.Getenv("ORQ_TEST_MAIN") == "1" {
		os.Exit(Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, false))
	}
	code := m.Run()
	if binFalsoDir != "" {
		os.RemoveAll(binFalsoDir)
	}
	os.Exit(code)
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

// orq onda proxima diz o próximo passo da onda no limite, em JSON e em texto; não encerra.
func TestProximaEncerramentoCLI(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a", "--projeto", "p")
	ok(t, env, "onda", "criar", "o", "--us", "1")
	url := "https://gitlab.basis.com.br/basis/x/-/merge_requests/7"
	ok(t, env, "mr", "add", "1", url)
	type saida struct {
		Motivo       string
		Encerramento *struct {
			Pronta  bool
			Comando *string
			Faltam  []string
			Passo   string
		}
	}
	ler := func() saida {
		t.Helper()
		var s saida
		if out := ok(t, env, "onda", "proxima", "1"); json.Unmarshal([]byte(out), &s) != nil {
			t.Fatalf("proxima = %s", out)
		}
		return s
	}
	if s := ler(); s.Encerramento != nil {
		t.Fatalf("em curso: %+v", s.Encerramento)
	}
	ok(t, env, "us", "status", "1", "done")
	ok(t, env, "pend", "add", "1", "--tipo", "duvida", "e agora?")
	if s := ler(); s.Encerramento == nil || s.Encerramento.Pronta || len(s.Encerramento.Faltam) != 2 || s.Encerramento.Comando != nil {
		t.Fatalf("MR sem estado e pendência: %+v", s.Encerramento)
	}
	txt := ok(t, env, "--output", "text", "onda", "proxima", "1")
	for _, quer := range []string{"próximo passo: falta concluir", "falta: p/1: MR " + url + " sem estado conhecido (falta conferir)", "falta: p/1#P1: pendência aberta"} {
		if !strings.Contains(txt, quer) {
			t.Errorf("texto sem %q:\n%s", quer, txt)
		}
	}
	ok(t, env, "pend", "resolver", "TG-1#P1", "--resposta", "levada para o Taiga")
	st, err := central.Open(context.Background(), env["ORQ_STATE_DIR"])
	if err != nil {
		t.Fatal(err)
	}
	err = st.GravarMR(context.Background(), url, central.ValorMR{Estado: "mesclada"}, nil)
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	if s := ler(); s.Encerramento == nil || !s.Encerramento.Pronta || *s.Encerramento.Comando != "orq onda encerrar 1" {
		t.Fatalf("pronta: %+v", s.Encerramento)
	}
	if txt := ok(t, env, "--output", "text", "onda", "proxima", "1"); !strings.Contains(txt, "próximo passo: encerre a onda: orq onda encerrar 1\n") || strings.Contains(txt, "falta:") {
		t.Fatalf("texto pronta:\n%s", txt)
	}
	if out := ok(t, env, "onda", "list", "--abertas"); !strings.Contains(out, `"id": 1`) {
		t.Fatalf("a onda foi encerrada sozinha: %s", out)
	}
}

// Numa onda com dois projetos e o mesmo número, o id que sai em faltam, colado no
// pend resolver, resolve a pendência certa (o id curto TG-1#P1 iria ao projeto padrão).
func TestFaltamComDoisProjetosCLI(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a", "--projeto", "demo-a")
	ok(t, env, "onda", "criar", "o", "--us", "demo-a/1,demo-b/1")
	ok(t, env, "us", "status", "demo-a/1", "done")
	ok(t, env, "us", "status", "demo-b/1", "done")
	ok(t, env, "pend", "add", "demo-b/1", "--tipo", "duvida", "fictícia")
	var s struct{ Encerramento struct{ Faltam []string } }
	if out := ok(t, env, "onda", "proxima", "1"); json.Unmarshal([]byte(out), &s) != nil || len(s.Encerramento.Faltam) != 1 {
		t.Fatalf("proxima = %s", out)
	}
	id, _, _ := strings.Cut(s.Encerramento.Faltam[0], ":")
	if id != "demo-b/1#P1" {
		t.Fatalf("id em faltam = %q", id)
	}
	if txt := ok(t, env, "--output", "text", "onda", "proxima", "1"); !strings.Contains(txt, "falta: demo-b/1#P1: pendência aberta") {
		t.Fatalf("texto:\n%s", txt)
	}
	ok(t, env, "pend", "resolver", id, "--resposta", "levada para o Taiga")
	var p struct{ Encerramento struct{ Pronta bool } }
	if out := ok(t, env, "onda", "proxima", "1"); json.Unmarshal([]byte(out), &p) != nil || !p.Encerramento.Pronta {
		t.Fatalf("depois de resolver: %s", out)
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
	if out := ok(t, env, "snapshot", "--json"); !strings.Contains(out, `"versao": 1`) {
		t.Fatalf("snapshot --json = %q", out)
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

// Vários processos abrem ao mesmo tempo um banco que ainda não existe: a criação, a ativação
// do WAL e a migration não podem derrubar nenhum deles.
func TestConcorrenciaBancoNovo(t *testing.T) {
	if testing.Short() {
		t.Skip("sobe processos")
	}
	const rodadas, procs = 8, 4
	for r := 0; r < rodadas; r++ {
		dir := t.TempDir()
		var wg sync.WaitGroup
		saidas := make([][]byte, procs)
		erros := make([]error, procs)
		for i := 0; i < procs; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				args, _ := json.Marshal([]string{"coord", "registrar", fmt.Sprintf("c%d", i), "--pane", ""})
				cmd := exec.Command(os.Args[0], "-test.run=^$")
				cmd.Env = append(os.Environ(), "ORQ_TEST_HELPER=1", "ORQ_TEST_REPETIR=1", "ORQ_TEST_ARGS="+string(args), "ORQ_STATE_DIR="+dir, "ORQ_COORD=", "HERDR_PANE_ID=")
				saidas[i], erros[i] = cmd.CombinedOutput()
			}()
		}
		wg.Wait()
		for i, err := range erros {
			if err != nil {
				t.Fatalf("rodada %d, processo %d: %v\n%s", r, i, err, saidas[i])
			}
		}
		var cs []struct{ Nome string }
		if err := json.Unmarshal([]byte(ok(t, map[string]string{"ORQ_STATE_DIR": dir}, "coord", "list")), &cs); err != nil || len(cs) != procs {
			t.Fatalf("rodada %d: %d coordenadores, %v", r, len(cs), err)
		}
	}
}

// Três processos introduzem projetos ao mesmo tempo, dois deles os mesmos: cada projeto fica com
// um índice só, os índices são 0..n-1 sem repetir nem pular, e cada atribuição deixou o seu evento.
func TestConcorrenciaTomDoProjeto(t *testing.T) {
	if testing.Short() {
		t.Skip("sobe processos")
	}
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "base")
	const n = 25
	procs := [][]string{
		{"coord", "registrar", "a{i}", "--pane", "", "--projeto", "pa{i}"},
		{"coord", "registrar", "b{i}", "--pane", "", "--projeto", "pb{i}"},
		{"coord", "registrar", "c{i}", "--pane", "", "--projeto", "pa{i}"},
	}
	var wg sync.WaitGroup
	erros := make([]error, len(procs))
	saidas := make([][]byte, len(procs))
	for i, p := range procs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			args, _ := json.Marshal(p)
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), "ORQ_TEST_HELPER=1", "ORQ_TEST_REPETIR="+strconv.Itoa(n), "ORQ_TEST_ARGS="+string(args),
				"ORQ_STATE_DIR="+env["ORQ_STATE_DIR"], "ORQ_COORD=", "HERDR_PANE_ID=")
			saidas[i], erros[i] = cmd.CombinedOutput()
		}()
	}
	wg.Wait()
	for i, err := range erros {
		if err != nil {
			t.Fatalf("processo %d: %v\n%s", i, err, saidas[i])
		}
	}
	var snap struct {
		Projetos []struct {
			Projeto string
			Indice  int
		}
	}
	if err := json.Unmarshal([]byte(ok(t, env, "snapshot")), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Projetos) != 2*n {
		t.Fatalf("%d projetos, queria %d", len(snap.Projetos), 2*n)
	}
	nomes, indices := map[string]bool{}, map[int]bool{}
	for _, p := range snap.Projetos {
		if nomes[p.Projeto] || indices[p.Indice] || p.Indice < 0 || p.Indice >= 2*n {
			t.Fatalf("projeto ou índice repetido ou fora de 0..%d: %+v", 2*n-1, p)
		}
		nomes[p.Projeto], indices[p.Indice] = true, true
	}
	var es []struct{ Tipo string }
	if err := json.Unmarshal([]byte(ok(t, env, "diario", "--limite", "0")), &es); err != nil {
		t.Fatal(err)
	}
	conta := 0
	for _, e := range es {
		if e.Tipo == "projeto.tom" {
			conta++
		}
	}
	if conta != 2*n {
		t.Fatalf("%d eventos projeto.tom, queria %d", conta, 2*n)
	}
}

// Cada atribuição de tom identifica o projeto no diário, em JSON e em texto.
func TestDiarioTomIdentificaOProjeto(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a")
	ok(t, env, "onda", "criar", "o", "--us", "projeto-a/1,projeto-b/1")
	var es []struct {
		Tipo    string
		Detalhe struct {
			Projeto string
			Indice  int
		}
	}
	if err := json.Unmarshal([]byte(ok(t, env, "diario", "--limite", "0")), &es); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range es {
		if e.Tipo == "projeto.tom" {
			got = append(got, e.Detalhe.Projeto+"="+strconv.Itoa(e.Detalhe.Indice))
		}
	}
	if strings.Join(got, ",") != "projeto-a=0,projeto-b=1" {
		t.Fatalf("projeto.tom em JSON = %v", got)
	}
	texto := ok(t, env, "--output", "text", "diario", "--limite", "0")
	for _, want := range []string{`"projeto":"projeto-a"`, `"projeto":"projeto-b"`} {
		if !strings.Contains(texto, want) {
			t.Errorf("diário em texto sem %s:\n%s", want, texto)
		}
	}
}

// O orq que encontra um banco mais novo que ele manda rodar o orq upgrade.
func TestBancoMaisNovoApontaOUpgrade(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a")
	db, err := sql.Open("sqlite", "file:"+filepath.Join(env["ORQ_STATE_DIR"], "central.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Sem banco_meta, o banco mais novo é recusado como sempre.
	if _, err := db.Exec("DROP TABLE banco_meta"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	r := rodar(t, env, "snapshot")
	if r.code != 4 || !strings.Contains(r.err, "versao_do_banco") {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	for _, want := range []string{"orq upgrade", "daemon"} {
		if !strings.Contains(r.err, want) {
			t.Errorf("recovery sem %q: %s", want, r.err)
		}
	}
}
