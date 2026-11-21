package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUsEsperaRegistraTrocaELimpa(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a", "--projeto", "p")
	ok(t, env, "onda", "criar", "o", "--nome", "teste", "--us", "1")
	var res struct {
		Alterada bool
		US       struct {
			EsperaMotivo *string `json:"espera_motivo"`
		}
	}
	out := ok(t, env, "us", "espera", "1", "Espera o eSocial publicar o arquivo")
	if err := json.Unmarshal([]byte(out), &res); err != nil || !res.Alterada || res.US.EsperaMotivo == nil || *res.US.EsperaMotivo != "Espera o eSocial publicar o arquivo" {
		t.Fatalf("saída = %s (%v)", out, err)
	}
	txt := ok(t, env, "--output", "text", "us", "espera", "1", "Espera a carga corrigida")
	if !strings.Contains(txt, "Espera a carga corrigida") {
		t.Errorf("texto = %q", txt)
	}
	if txt = ok(t, env, "--output", "text", "us", "espera", "1", "Espera a carga corrigida"); !strings.Contains(txt, "(sem mudança)") {
		t.Errorf("repetição = %q", txt)
	}
	out = ok(t, env, "us", "espera", "1", "--limpar")
	if err := json.Unmarshal([]byte(out), &res); err != nil || !res.Alterada || res.US.EsperaMotivo != nil {
		t.Fatalf("limpar = %s (%v)", out, err)
	}
	d := strings.ReplaceAll(ok(t, env, "diario"), `": `, `":`)
	for _, quer := range []string{`us.espera`, `"limpou":true`, `"motivo_antes":"Espera a carga corrigida"`} {
		if !strings.Contains(d, quer) {
			t.Errorf("diário sem %s:\n%s", quer, d)
		}
	}
	// Sair de wait limpa o motivo.
	ok(t, env, "us", "espera", "1", "outro motivo")
	out = ok(t, env, "us", "status", "1", "exec")
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.US.EsperaMotivo != nil {
		t.Fatalf("status exec = %s (%v)", out, err)
	}
	// Sem motivo e sem --limpar, ou os dois, é erro de uso.
	for _, args := range [][]string{{"us", "espera", "1"}, {"us", "espera", "1", "x", "--limpar"}} {
		if r := rodar(t, env, args...); r.code != 2 || !strings.Contains(r.err, `"uso"`) {
			t.Errorf("%v: exit %d %s", args, r.code, r.err)
		}
	}
	if r := rodar(t, env, "us", "espera", "1", "a\x1bb"); r.code != 2 {
		t.Errorf("controle: exit %d %s", r.code, r.err)
	}
}
