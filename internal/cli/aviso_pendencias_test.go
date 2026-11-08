package cli

import (
	"strings"
	"testing"
)

// Pendência aberta em US concluída ou em onda encerrada gera aviso no stderr, sem texto da
// pendência, e o comando não resolve nada nem muda o exit.
func TestAvisoPendenciaAbertaEmUSConcluida(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a", "--projeto", "p")
	ok(t, env, "onda", "criar", "o", "--us", "1,2")
	ok(t, env, "pend", "add", "1", "--tipo", "duvida", "segredo-do-texto")
	ok(t, env, "pend", "add", "2", "--tipo", "duvida", "outra")

	r := rodar(t, env, "us", "status", "1", "done")
	if r.code != 0 || !strings.Contains(r.err, "aviso: p/1#P1 aberta") || strings.Contains(r.err, "p/2#P1") || strings.Contains(r.err, "segredo-do-texto") {
		t.Fatalf("us status done: exit %d\nout=%s\nerr=%s", r.code, r.out, r.err)
	}
	if r := rodar(t, env, "us", "status", "2", "exec"); r.err != "" {
		t.Fatalf("fora de done não avisa: %s", r.err)
	}

	r = rodar(t, env, "onda", "encerrar", "1")
	if r.code != 0 || !strings.Contains(r.err, "p/1#P1 aberta") || !strings.Contains(r.err, "p/2#P1 aberta") {
		t.Fatalf("onda encerrar: exit %d\nerr=%s", r.code, r.err)
	}
	// o aviso não resolve nada
	if out := ok(t, env, "pend", "list", "--abertas"); !strings.Contains(out, "p/1#P1") || !strings.Contains(out, "p/2#P1") {
		t.Fatalf("pendências mudaram: %s", out)
	}

	ok(t, env, "pend", "resolver", "p/1#P1", "--resposta", "levada para o Taiga")
	ok(t, env, "pend", "resolver", "p/2#P1", "--resposta", "levada para o Taiga")
	env2 := ambiente(t)
	ok(t, env2, "coord", "registrar", "a", "--projeto", "p")
	ok(t, env2, "onda", "criar", "o", "--us", "1")
	if r := rodar(t, env2, "onda", "encerrar", "1"); r.err != "" {
		t.Fatalf("sem pendência não avisa: %s", r.err)
	}
}

// Pendência respondida: o aviso manda ler com orq pend ver, não sugere trocar a resposta, e o
// aviso não muda o stdout do comando.
func TestAvisoPendenciaRespondida(t *testing.T) {
	env := ambiente(t)
	ok(t, env, "coord", "registrar", "a", "--projeto", "p")
	ok(t, env, "onda", "criar", "o", "--us", "1,2")
	ok(t, env, "pend", "add", "1", "--tipo", "duvida", "e agora?")
	ok(t, env, "pend", "responder", "TG-1#P1", "texto-da-pessoa")

	// Sem pendência na US 2, o stdout de referência vem do mesmo comando sem aviso.
	ref := rodar(t, env, "us", "status", "2", "done")
	if ref.err != "" {
		t.Fatalf("sem pendência não avisa: %s", ref.err)
	}
	r := rodar(t, env, "us", "status", "1", "done")
	if r.code != 0 || !strings.Contains(r.err, "p/1#P1 respondida") || !strings.Contains(r.err, "orq pend ver p/1#P1") || strings.Contains(r.err, "--resposta") || strings.Contains(r.err, "texto-da-pessoa") {
		t.Fatalf("us status done: exit %d\nerr=%s", r.code, r.err)
	}
	if strings.Contains(r.out, "aviso:") {
		t.Fatalf("o aviso vazou para o stdout:\n%s", r.out)
	}
	r = rodar(t, env, "onda", "encerrar", "1")
	if r.code != 0 || !strings.Contains(r.err, "orq pend ver p/1#P1") || strings.Contains(r.out, "aviso:") {
		t.Fatalf("onda encerrar: exit %d\nout=%s\nerr=%s", r.code, r.out, r.err)
	}
	if out := ok(t, env, "pend", "list", "--respondidas"); !strings.Contains(out, "p/1#P1") {
		t.Fatalf("a pendência mudou: %s", out)
	}
}
