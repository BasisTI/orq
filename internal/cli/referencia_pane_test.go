package cli

import (
	"bytes"
	"context"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/herdrfalso/falsotest"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
)

func TestTextoCoordCitaOPanePeloNome(t *testing.T) {
	pane := "w12:p4"
	c := central.Coordenador{Nome: "orq-comunicacao", PaneID: &pane}
	var b bytes.Buffer
	if err := textoCoord(&b, c, "colaboradados/ci-templates-v1231 (w12:p4)"); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), "orq-comunicacao  pane colaboradados/ci-templates-v1231 (w12:p4)  workspace -  projeto -\n"; got != want {
		t.Fatalf("saída = %q, quer %q", got, want)
	}
}

func TestRefCentralNomeiaWorkspaceEPane(t *testing.T) {
	got := refCentral("Central", PaneCentral{Coordenador: "orq-comunicacao", PaneID: "w3:p2"})
	if want := "Central/orq · orq-comunicacao (w3:p2)"; got != want {
		t.Fatalf("refCentral = %q, quer %q", got, want)
	}
}

// Ponta a ponta com o herdr falso: as saídas em texto citam o pane pelo nome do workspace e do
// pane (ou da tab, sem nome no pane), com o id entre parênteses, nunca o id sozinho.
func TestSaidasCitamOPanePeloNomeComOHerdrFalso(t *testing.T) {
	ctx := context.Background()
	f := falsotest.Novo(t)
	f.Local("w12:p4", "w12", "colaboradados", "w12:t1", "ci")
	f.Rotulo("w12:p4", "ci-templates-v1231")
	f.Local("w12:p6", "w12", "colaboradados", "w12:t2", "ci")
	f.Agente("w12:p6", "exec", "working")
	f.Local("w12:p7", "w12", "colaboradados", "w12:t3", "ci") // sem rótulo nem agente
	env := ambiente(t)
	env["ORQ_HERDR_BIN"] = f.Bin

	// O que o daemon faria: a sessão do Herdr no cache.
	sessao, err := enriquecer.Ferramentas{Env: func(k string) string { return env[k] }}.ConsultarHerdr(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st, err := central.Open(ctx, env["ORQ_STATE_DIR"])
	if err != nil {
		t.Fatal(err)
	}
	if err := st.GravarCache(ctx, central.FonteHerdr, "", sessao, nil); err != nil {
		t.Fatal(err)
	}
	st.Close()

	texto := func(args ...string) string { return ok(t, env, append(args, "--output", "text")...) }
	quer := func(nome, out, trecho string) {
		t.Helper()
		if !strings.Contains(out, trecho) {
			t.Errorf("%s sem %q:\n%s", nome, trecho, out)
		}
	}

	quer("coord registrar", texto("coord", "registrar", "coord-x", "--pane", "w12:p4", "--projeto", "proj"), "pane colaboradados/ci-templates-v1231 (w12:p4)")
	quer("coord list", texto("coord", "list"), "pane colaboradados/ci-templates-v1231 (w12:p4)")

	ok(t, env, "onda", "criar", "o", "--nome", "onda-x", "--us", "proj/1", "--coord", "coord-x")
	quer("onda ver", texto("us", "status", "proj/1", "exec", "--pane", "w12:p6"), "pane colaboradados/exec (w12:p6) (working)") // sem rótulo: o nome do agente

	if g := texto("pend", "add", "proj/1", "--tipo", "duvida", "Qual tela?", "--pane", "w12:p7"); g == "" {
		t.Fatal("pend add sem saída")
	}
	quer("pend ver", texto("pend", "ver", "proj/1#P1"), "pane de origem:  colaboradados/ci (w12:p7)")
	if j := ok(t, env, "pend", "ver", "proj/1#P1", "--output", "json"); !strings.Contains(j, `"pane_origem_ref": "colaboradados/ci (w12:p7)"`) {
		t.Errorf("pend ver json sem pane_origem_ref:\n%s", j)
	}

	// Renomear o pane do coordenador diz o nome lido do Herdr agora, não o id.
	out := ok(t, env, "onda", "criar", "o2", "--nome", "onda-y", "--us", "proj/2", "--coord", "coord-x", "--output", "text")
	quer("onda criar", out, "pane colaboradados/onda-y (w12:p4)")
}
