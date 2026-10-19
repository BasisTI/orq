package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/BasisTI/orq/internal/central"
)

func TestVersionTrazCommitESchema(t *testing.T) {
	env := ambiente(t)
	var v struct {
		Versao string
		Commit string
		Schema int
	}
	if err := json.Unmarshal([]byte(ok(t, env, "version")), &v); err != nil || v.Versao == "" || v.Schema != central.Schema() {
		t.Fatalf("version = %+v, %v", v, err)
	}
	if txt := ok(t, env, "--output", "text", "version"); !strings.HasPrefix(txt, "orq ") {
		t.Fatalf("texto = %q", txt)
	}
}

func TestUpgradeRefInvalido(t *testing.T) {
	env := ambiente(t)
	if r := rodar(t, env, "upgrade", "--ref", "--yes"); r.code != 2 || !strings.Contains(r.err, `"uso"`) {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

// Sem go install nem plugin, o upgrade não tem o que atualizar: diz isso e não abre o banco.
func TestUpgradeSemNadaNaoAbreOBanco(t *testing.T) {
	env := ambiente(t)
	env["HOME"] = t.TempDir()
	env["ORQ_GO_BIN"] = "/nao/existe/go"
	env["ORQ_HERDR_BIN"] = "/nao/existe/herdr"
	r := rodar(t, env, "upgrade")
	if r.code != 4 || !strings.Contains(r.err, `"upgrade_incompleto"`) {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	var res struct {
		OK     bool
		Etapas []struct{ Nome, Estado string }
	}
	if err := json.Unmarshal([]byte(r.out), &res); err != nil || res.OK || len(res.Etapas) != 2 {
		t.Fatalf("saída = %q, %v", r.out, err)
	}
	if _, err := os.Stat(filepath.Join(env["ORQ_STATE_DIR"], central.Arquivo)); !os.IsNotExist(err) {
		t.Fatalf("o upgrade criou o banco: %v", err)
	}
	if txt := rodar(t, env, "--output", "text", "upgrade"); !strings.Contains(txt.out, "pulado") {
		t.Fatalf("texto = %q", txt.out)
	}
}

// bancoV3 prepara um banco anterior à versão mínima de leitor, como o de quem ainda roda o orq
// anterior; as migrations seguintes ainda estão por aplicar.
func bancoV3(t *testing.T, dir string) {
	t.Helper()
	st, err := central.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, central.Arquivo)+"?_pragma=busy_timeout(15000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"DROP TABLE banco_meta", "PRAGMA user_version = 3"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// outroOrq grava um orq falso no PATH que responde a `version` com a saída dada.
func outroOrq(t *testing.T, saida string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orq"), []byte("#!/bin/sh\ncase \"$1\" in version) echo '"+saida+"' ;; *) exit 1 ;; esac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func ambienteSemFerramentas(t *testing.T, path string) map[string]string {
	env := ambiente(t)
	env["HOME"] = t.TempDir()
	env["PATH"] = path
	env["ORQ_GO_BIN"] = "/nao/existe/go"
	env["ORQ_HERDR_BIN"] = "/nao/existe/herdr"
	return env
}

// Antes de migrar, o orq novo avisa de cada orq conhecido que deixa de abrir o banco.
func TestAvisaAntesDeMigrarQuemNaoAbreOBanco(t *testing.T) {
	// Um orq anterior ao leitor mínimo: a saída de `version` não traz schema.
	env := ambienteSemFerramentas(t, outroOrq(t, `{"versao":"0.3.0-1-gaaaaaaa"}`))
	bancoV3(t, env["ORQ_STATE_DIR"])
	r := rodar(t, env, "coord", "list")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	for _, quer := range []string{"aviso: ao migrar o banco da versão 3 para a 4", "deixa de abrir o banco (versao_do_banco)", "0.3.0-1-gaaaaaaa", "orq upgrade"} {
		if !strings.Contains(r.err, quer) {
			t.Errorf("stderr sem %q:\n%s", quer, r.err)
		}
	}
	// Banco em dia: nenhuma migração, nenhum aviso.
	if r := rodar(t, env, "coord", "list"); r.err != "" {
		t.Fatalf("segunda abertura avisou: %s", r.err)
	}
}

func TestNaoAvisaDeQuemAindaAbreOBanco(t *testing.T) {
	env := ambienteSemFerramentas(t, outroOrq(t, `{"versao":"0.4.0","commit":"cccccccccccc","schema":4}`))
	bancoV3(t, env["ORQ_STATE_DIR"])
	if r := rodar(t, env, "coord", "list"); r.code != 0 || r.err != "" {
		t.Fatalf("exit %d, stderr %q", r.code, r.err)
	}
}

// Banco novo (nenhum outro orq o usa ainda) não consulta ninguém.
func TestBancoNovoNaoConsultaOutrosBinarios(t *testing.T) {
	env := ambienteSemFerramentas(t, outroOrq(t, `{"versao":"0.3.0-1-gaaaaaaa"}`))
	if r := rodar(t, env, "coord", "list"); r.code != 0 || r.err != "" {
		t.Fatalf("exit %d, stderr %q", r.code, r.err)
	}
}

// Vários processos abrem juntos um banco v3: a migração para a versão atual acontece uma vez,
// sob a trava de início, e nenhum deles falha.
func TestConcorrenciaMigracaoDoBancoV3(t *testing.T) {
	if testing.Short() {
		t.Skip("sobe processos")
	}
	for r := 0; r < 4; r++ {
		dir := t.TempDir()
		bancoV3(t, dir)
		var wg sync.WaitGroup
		saidas := make([][]byte, 4)
		erros := make([]error, 4)
		for i := range erros {
			wg.Add(1)
			go func() {
				defer wg.Done()
				args, _ := json.Marshal([]string{"coord", "registrar", fmt.Sprintf("c%d", i), "--pane", ""})
				cmd := exec.Command(os.Args[0], "-test.run=^$")
				cmd.Env = append(os.Environ(), "ORQ_TEST_HELPER=1", "ORQ_TEST_REPETIR=1", "ORQ_TEST_ARGS="+string(args), "ORQ_STATE_DIR="+dir,
					"ORQ_COORD=", "HERDR_PANE_ID=", "ORQ_HERDR_BIN=/nao/existe/herdr", "ORQ_GO_BIN=/nao/existe/go", "PATH=", "HOME="+dir)
				saidas[i], erros[i] = cmd.CombinedOutput()
			}()
		}
		wg.Wait()
		for i, err := range erros {
			if err != nil {
				t.Fatalf("rodada %d, processo %d: %v\n%s", r, i, err, saidas[i])
			}
		}
		e, existe, err := central.LerEstado(context.Background(), dir)
		if err != nil || !existe || e.Banco != central.Schema() || e.LeitorMinimo != 4 {
			t.Fatalf("rodada %d: estado %+v, %v", r, e, err)
		}
		var cs []struct{ Nome string }
		if err := json.Unmarshal([]byte(ok(t, map[string]string{"ORQ_STATE_DIR": dir}, "coord", "list")), &cs); err != nil || len(cs) != 4 {
			t.Fatalf("rodada %d: %d coordenadores, %v", r, len(cs), err)
		}
	}
}
