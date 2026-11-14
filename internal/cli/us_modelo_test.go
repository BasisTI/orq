package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUsStatusModeloEEsforco(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a", "--projeto", "p")
	ok(t, env, "onda", "criar", "o", "--us", "1,2")
	var res struct {
		Alterada bool
		US       struct{ Modelo, Esforco *string }
	}
	out := ok(t, env, "us", "status", "1", "exec", "--pane", "wB:p2", "--modelo", "claude-fable-5-2", "--esforco", "medium")
	if err := json.Unmarshal([]byte(out), &res); err != nil || !res.Alterada || res.US.Modelo == nil || *res.US.Modelo != "claude-fable-5-2" || *res.US.Esforco != "medium" {
		t.Fatalf("saída = %s (%v)", out, err)
	}
	// A saída em texto traz o modelo na linha de status.
	txt := ok(t, env, "--output", "text", "us", "status", "1", "exec", "--modelo", "claude-sonnet-5-5")
	if !strings.Contains(txt, "claude-sonnet-5-5") || !strings.Contains(txt, "medium") {
		t.Errorf("texto = %q", txt)
	}
	// Sem --modelo, o gravado fica.
	out = ok(t, env, "us", "status", "1", "rev")
	if err := json.Unmarshal([]byte(out), &res); err != nil || *res.US.Modelo != "claude-sonnet-5-5" {
		t.Fatalf("manter = %s", out)
	}
	// O diário registra a troca.
	d := ok(t, env, "diario")
	for _, quer := range []string{`"modelo":"claude-fable-5-2"`, `"modelo":"claude-sonnet-5-5"`, `"modelo_antes":"claude-fable-5-2"`} {
		if !strings.Contains(strings.ReplaceAll(d, `": "`, `":"`), quer) {
			t.Errorf("diário sem %s:\n%s", quer, d)
		}
	}
	// Valor inválido é erro de uso e não grava.
	r := rodar(t, env, "us", "status", "2", "exec", "--modelo", "com espaço")
	if r.code != 2 || !strings.Contains(r.err, `"uso"`) {
		t.Errorf("modelo inválido: exit %d %s", r.code, r.err)
	}
}
