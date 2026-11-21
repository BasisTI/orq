package enriquecer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/herdrfalso/falsotest"
	"github.com/BasisTI/orq/internal/refs"
)

func nomesCom(t *testing.T, bin string) (*Nomes, *central.Store, *[]string) {
	t.Helper()
	st, err := central.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	var logs []string
	n := &Nomes{St: st, F: Ferramentas{Env: func(k string) string {
		if k == "ORQ_HERDR_BIN" {
			return bin
		}
		return ""
	}}, Log: func(f string, a ...any) { logs = append(logs, strings.TrimSpace(f)) }}
	return n, st, &logs
}

func criarOnda(t *testing.T, st *central.Store, c central.Coordenador, nome string, ref int) central.Onda {
	t.Helper()
	o, err := st.CriarOnda(context.Background(), c, central.NovaOnda{Titulo: nome + " título", Nome: nome, US: []refs.US{{Projeto: "p", Ref: ref}}})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func diarioTipo(t *testing.T, st *central.Store, tipo string) int {
	t.Helper()
	es, err := st.Diario(context.Background(), nil, central.FiltroDiario{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range es {
		if e.Tipo == tipo {
			n++
		}
	}
	return n
}

// Divergência criada à mão (rótulo e nome do agente) é detectada e corrigida, uma vez só.
func TestNomesCorrigeADivergencia(t *testing.T) {
	ctx := context.Background()
	h := falsotest.Novo(t)
	h.AgenteFixo("wA:p2", "agente-qualquer", "idle")
	h.Rotulo("wA:p2", "rótulo qualquer")
	n, st, logs := nomesCom(t, h.Bin)
	c, err := st.RegistrarCoord(ctx, central.Registro{Nome: "coord", Pane: "wA:p2", Projeto: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Ciclo(ctx); err != nil || len(h.Chamadas()) != 0 {
		t.Fatalf("sem onda aberta não toca no Herdr: %v %v", err, h.Chamadas())
	}
	criarOnda(t, st, c, "primeira", 1)
	criarOnda(t, st, c, "segunda", 2)
	if err := n.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if r, a := h.RotuloENome("wA:p2"); r != "segunda" || a != "segunda" {
		t.Fatalf("rótulo %q, agente %q", r, a)
	}
	if diarioTipo(t, st, "onda.pane_nome") != 1 || len(*logs) != 1 {
		t.Fatalf("diário %d, log %v", diarioTipo(t, st, "onda.pane_nome"), *logs)
	}
	// Em dia: nada a fazer, nada gravado.
	antes := len(h.Chamadas())
	if err := n.Ciclo(ctx); err != nil || diarioTipo(t, st, "onda.pane_nome") != 1 {
		t.Fatalf("ciclo em dia: %v", err)
	}
	for _, c := range h.Chamadas()[antes:] {
		if strings.Contains(c, "rename") {
			t.Fatalf("renomeou de novo: %s", c)
		}
	}
	// Só o nome do agente diverge: só ele é renomeado.
	h.Agente("wA:p2", "outro", "idle")
	if err := n.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if r, a := h.RotuloENome("wA:p2"); r != "segunda" || a != "segunda" {
		t.Fatalf("rótulo %q, agente %q", r, a)
	}
}

// Pane que o Herdr não mostra mais, onda sem nome e Herdr fora do ar não derrubam a conferência.
func TestNomesIgnoraPaneAusenteEOndaSemNome(t *testing.T) {
	ctx := context.Background()
	h := falsotest.Novo(t)
	n, st, _ := nomesCom(t, h.Bin)
	c, err := st.RegistrarCoord(ctx, central.Registro{Nome: "coord", Pane: "wA:p9", Projeto: "p"})
	if err != nil {
		t.Fatal(err)
	}
	criarOnda(t, st, c, "uma", 1)
	if err := n.Ciclo(ctx); err != nil || diarioTipo(t, st, "onda.pane_nome") != 0 {
		t.Fatalf("pane ausente: %v", err)
	}
	// Onda antiga sem nome nem título válido: sem nome esperado, sem consulta ao Herdr.
	h2 := falsotest.Novo(t)
	n2, st2, _ := nomesCom(t, h2.Bin)
	c2, _ := st2.RegistrarCoord(ctx, central.Registro{Nome: "coord", Pane: "wA:p2", Projeto: "p"})
	if _, err := st2.CriarOnda(ctx, c2, central.NovaOnda{Titulo: "Título Sem Nome", US: []refs.US{{Projeto: "p", Ref: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := n2.Ciclo(ctx); err != nil || len(h2.Chamadas()) != 0 {
		t.Fatalf("onda sem nome: %v %v", err, h2.Chamadas())
	}
}

// A mesma recusa do Herdr não enche o diário a cada tick.
func TestNomesNaoRepeteAFalha(t *testing.T) {
	ctx := context.Background()
	h := falsotest.Novo(t)
	h.Agente("wA:p2", "x", "idle")
	n, st, _ := nomesCom(t, h.Bin)
	c, _ := st.RegistrarCoord(ctx, central.Registro{Nome: "coord", Pane: "wA:p2", Projeto: "p"})
	criarOnda(t, st, c, "uma", 1)
	// Herdr que responde ao snapshot mas recusa o rename: troca o binário por um que falha só no rename.
	n.F = Ferramentas{Env: func(k string) string {
		if k == "ORQ_HERDR_BIN" {
			return scriptHerdrQueRecusa(t, h.Bin)
		}
		return ""
	}}
	for range 3 {
		if err := n.Ciclo(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := diarioTipo(t, st, "onda.pane_nome"); got != 1 {
		t.Fatalf("falha repetida gravou %d eventos", got)
	}
}

// scriptHerdrQueRecusa devolve um herdr que repassa tudo ao falso, menos `agent rename` e
// `pane rename`, que ele recusa com um erro do Herdr.
func scriptHerdrQueRecusa(t *testing.T, falso string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "herdr")
	script := "#!/bin/sh\ncase \"$2\" in rename) echo '{\"error\":{\"code\":\"name_taken\",\"message\":\"x\"}}' >&2; exit 1 ;; esac\nexec '" + falso + "' \"$@\"\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
