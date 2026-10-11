package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/enriquecer"
)

// Sem as ferramentas, orq atualizar não falha: registra binario_ausente e o snapshot mostra.
func TestAtualizarSemFerramentas(t *testing.T) {
	env := ambiente(t)
	for _, v := range enriquecer.VariavelBin {
		env[v] = "/nao/existe/bin"
	}
	ok(t, env, "coord", "registrar", "a", "--projeto", "orq")
	ok(t, env, "onda", "criar", "o", "--us", "4")
	ok(t, env, "mr", "add", "4", "https://github.com/BasisTI/orq/pull/1")
	var r enriquecer.Resultado
	if err := json.Unmarshal([]byte(ok(t, env, "atualizar")), &r); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"herdr", "mr", "taiga"} {
		if e := r.Fontes[f].Erros; len(e) != 1 || e[0].Erro.Codigo != "binario_ausente" {
			t.Fatalf("%s = %+v", f, r.Fontes[f])
		}
	}
	if r.Tokens != nil {
		t.Fatalf("tokens conferidos com o Herdr fora: %+v", r.Tokens)
	}
	snap := ok(t, env, "snapshot")
	if !strings.Contains(snap, `"codigo": "binario_ausente"`) {
		t.Fatalf("snapshot sem o erro: %s", snap)
	}
	// Dentro do TTL e sem --forcar, nada é consultado de novo.
	r = enriquecer.Resultado{}
	if err := json.Unmarshal([]byte(ok(t, env, "atualizar", "--fonte", "mr,taiga")), &r); err != nil {
		t.Fatal(err)
	}
	if r.Fontes["mr"].EmDia != 1 || r.Fontes["taiga"].EmDia != 1 || r.Fontes["herdr"] != nil {
		t.Fatalf("segunda = %+v", r.Fontes)
	}
	texto := ok(t, env, "--output", "text", "atualizar", "--fonte", "herdr", "--forcar")
	if !strings.Contains(texto, "herdr  consultadas 1") || !strings.Contains(texto, "binario_ausente") {
		t.Fatalf("texto = %q", texto)
	}
	if r := rodar(t, env, "atualizar", "--fonte", "jira"); r.code != 2 || !strings.Contains(r.err, `"uso"`) {
		t.Fatalf("fonte inválida: %d %s", r.code, r.err)
	}
}

func TestDaemonSegundoSai(t *testing.T) {
	env := ambiente(t)
	soltar, err := enriquecer.TravarDaemon(env["ORQ_STATE_DIR"])
	if err != nil {
		t.Fatal(err)
	}
	defer soltar()
	r := rodar(t, env, "daemon")
	if r.code != 4 || !strings.Contains(r.err, `"daemon_ativo"`) {
		t.Fatalf("segundo daemon: exit %d %s", r.code, r.err)
	}
}

// herdrFalso grava um herdr que responde `api snapshot` com resposta e registra o argv em
// <dir>/herdr.log; outros comandos saem 0 sem nada.
func herdrFalso(t *testing.T, resposta string) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "herdr.log")
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), []byte(resposta), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s ' \"$@\" >> '" + log + "'; echo >> '" + log + "'\n" +
		"if [ \"$1 $2\" = 'api snapshot' ]; then cat '" + filepath.Join(dir, "snapshot.json") + "'; fi\n"
	bin = filepath.Join(dir, "herdr")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

// O modo texto de orq atualizar mostra os avisos dos tokens, como o JSON e o log do daemon.
func TestAtualizarTextoMostraAvisos(t *testing.T) {
	env := ambiente(t)
	bin, _ := herdrFalso(t, `{"result":{"snapshot":{"panes":[{"pane_id":"wB:p1","workspace_id":"wB","agent_status":"idle","tokens":{"orq":"de outro"}}],"agents":[],"workspaces":[{"workspace_id":"wB","tokens":{}}]}}}`)
	env["ORQ_HERDR_BIN"] = bin
	ok(t, env, "coord", "registrar", "a", "--projeto", "orq")
	ok(t, env, "onda", "criar", "o", "--us", "4")
	texto := ok(t, env, "--output", "text", "atualizar", "--fonte", "herdr")
	if !strings.Contains(texto, `aviso: pane wB:p1: o token orq tem "de outro"`) {
		t.Fatalf("texto sem o aviso: %q", texto)
	}
}

// orq daemon --detach sobe o daemon noutro processo, que segura a trava; um segundo --detach
// não sobe outro.
func TestDaemonDetach(t *testing.T) {
	if testing.Short() {
		t.Skip("sobe processo")
	}
	t.Setenv("ORQ_TEST_MAIN", "1")
	env := ambiente(t)
	for _, v := range enriquecer.VariavelBin {
		env[v] = "/nao/existe/bin"
	}
	env["HERDR_SOCKET_PATH"] = filepath.Join(t.TempDir(), "sem.sock")
	var r ResultadoDetach
	if err := json.Unmarshal([]byte(ok(t, env, "daemon", "--detach", "--tick", "1h")), &r); err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(r.PID)
	if r.Estado != "iniciado" || err != nil || r.Log != filepath.Join(env["ORQ_STATE_DIR"], "daemon.log") {
		t.Fatalf("r = %+v", r)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		for i := 0; i < 100; i++ {
			if ativo, _, _ := enriquecer.DaemonAtivo(env["ORQ_STATE_DIR"]); !ativo {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Errorf("daemon %d não saiu com SIGTERM", pid)
	})
	var r2 ResultadoDetach
	if err := json.Unmarshal([]byte(ok(t, env, "daemon", "--detach")), &r2); err != nil {
		t.Fatal(err)
	}
	if r2.Estado != "ja_ativo" || r2.PID != r.PID {
		t.Fatalf("segundo = %+v", r2)
	}
}
