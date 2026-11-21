package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/herdrfalso/falsotest"
)

func ambienteOndaPane(t *testing.T) (map[string]string, *falsotest.Falso) {
	t.Helper()
	env := ambiente(t)
	env["HERDR_PANE_ID"] = "wA:p2"
	h := falsotest.Novo(t)
	h.AgenteFixo("wA:p2", "antigo", "idle")
	h.Rotulo("wA:p2", "rótulo antigo")
	env["ORQ_HERDR_BIN"] = h.Bin
	ok(t, env, "coord", "registrar", "coord", "--pane", "wA:p2", "--projeto", "orq")
	return env, h
}

func rotuloENome(t *testing.T, h *falsotest.Falso) string {
	t.Helper()
	r, n := h.RotuloENome("wA:p2")
	return r + "/" + n
}

func TestOndaCriarExigeONomeValido(t *testing.T) {
	env, h := ambienteOndaPane(t)
	r := rodar(t, env, "onda", "criar", "titulo", "--us", "1")
	if r.code != 2 || !strings.Contains(r.err+r.out, "--nome") {
		t.Fatalf("sem --nome: exit %d\n%s%s", r.code, r.out, r.err)
	}
	r = rodar(t, env, "onda", "criar", "titulo", "--nome", "Nome Ruim", "--us", "1")
	if r.code != 2 {
		t.Fatalf("nome inválido: exit %d\n%s%s", r.code, r.out, r.err)
	}
	if got := rotuloENome(t, h); got != "rótulo antigo/antigo" {
		t.Fatalf("o pane mudou sem onda criada: %s", got)
	}
}

// Criar, ampliar e encerrar: o rótulo e o nome do agente seguem a onda aberta mais recente.
func TestOndaRenomeiaOPaneDoCoordenador(t *testing.T) {
	env, h := ambienteOndaPane(t)
	ok(t, env, "onda", "criar", "Primeira onda", "--nome", "primeira", "--us", "1")
	if got := rotuloENome(t, h); got != "primeira/primeira" {
		t.Fatalf("depois de criar a primeira: %s", got)
	}
	var o2 struct {
		ID       int64  `json:"id"`
		Nome     string `json:"nome"`
		PaneNome struct {
			Pane   string `json:"pane"`
			Nome   string `json:"nome"`
			Rotulo bool   `json:"rotulo"`
			Agente bool   `json:"agente"`
		} `json:"pane_nome"`
	}
	if err := json.Unmarshal([]byte(ok(t, env, "--output", "json", "onda", "criar", "Segunda onda", "--nome", "segunda", "--us", "2")), &o2); err != nil {
		t.Fatal(err)
	}
	if o2.Nome != "segunda" || o2.PaneNome.Pane != "wA:p2" || !o2.PaneNome.Rotulo || !o2.PaneNome.Agente {
		t.Fatalf("saída = %+v", o2)
	}
	if got := rotuloENome(t, h); got != "segunda/segunda" {
		t.Fatalf("com duas ondas abertas vale a mais recente: %s", got)
	}
	// Divergência feita à mão; ampliar a onda corrige.
	h.Rotulo("wA:p2", "à mão")
	ok(t, env, "onda", "adicionar", "1", "3")
	if got := rotuloENome(t, h); got != "segunda/segunda" {
		t.Fatalf("depois de ampliar a onda 1: %s", got)
	}
	// Encerrar a mais recente devolve o nome da que restou; encerrar a última deixa o pane como está.
	ok(t, env, "onda", "encerrar", "2")
	if got := rotuloENome(t, h); got != "primeira/primeira" {
		t.Fatalf("depois de encerrar a segunda: %s", got)
	}
	ok(t, env, "onda", "encerrar", "1")
	if got := rotuloENome(t, h); got != "primeira/primeira" {
		t.Fatalf("depois de encerrar a última: %s", got)
	}
	diario := ok(t, env, "diario", "--output", "json")
	if !strings.Contains(diario, "onda.pane_nome") {
		t.Fatalf("o diário não registra o renomeio:\n%s", diario)
	}
}

// Falha do Herdr não impede criar a onda: aparece na saída e no diário.
func TestOndaCriarComHerdrQuebradoAvisaENaoFalha(t *testing.T) {
	env, _ := ambienteOndaPane(t)
	env["ORQ_HERDR_BIN"] = "/nao/existe/herdr"
	out := ok(t, env, "--output", "text", "onda", "criar", "Onda", "--nome", "onda-x", "--us", "1")
	if !strings.Contains(out, "onda 1") || !strings.Contains(out, "aviso: pane") {
		t.Fatalf("saída:\n%s", out)
	}
	diario := ok(t, env, "diario", "--output", "json")
	if !strings.Contains(diario, "onda.pane_nome") || !strings.Contains(diario, "erros") {
		t.Fatalf("diário sem a falha:\n%s", diario)
	}
}

// Pane sem agente registrado no Herdr: só o rótulo é renomeado, sem erro.
func TestOndaCriarEmPaneSemAgente(t *testing.T) {
	env := ambiente(t)
	env["HERDR_PANE_ID"] = "wA:p2"
	h := falsotest.Novo(t)
	h.Rotulo("wA:p2", "velho")
	env["ORQ_HERDR_BIN"] = h.Bin
	ok(t, env, "coord", "registrar", "coord", "--pane", "wA:p2", "--projeto", "orq")
	out := ok(t, env, "--output", "text", "onda", "criar", "Onda", "--nome", "so-rotulo", "--us", "1")
	if strings.Contains(out, "aviso: pane") || !strings.Contains(out, "nota:") {
		t.Fatalf("saída:\n%s", out)
	}
	if r, _ := h.RotuloENome("wA:p2"); r != "so-rotulo" {
		t.Fatalf("rótulo = %q", r)
	}
}
