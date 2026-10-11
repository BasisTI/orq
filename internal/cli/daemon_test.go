package cli

import (
	"encoding/json"
	"strings"
	"testing"

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
