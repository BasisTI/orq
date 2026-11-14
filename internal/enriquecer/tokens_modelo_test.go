package enriquecer

import (
	"context"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

func TestModeloToken(t *testing.T) {
	st := func(s string) *string { return &s }
	for _, c := range []struct {
		modelo *string
		quer   string
	}{
		{st("claude-fable-5-2"), "[C] fable-5-2"},
		{st("gpt-5.5"), "[O] gpt-5.5"},
		{st("kimi-k3"), "[K] kimi-k3"},
		{st("llama-4"), "llama-4"},
		{st(""), ""},
		{nil, ""},
	} {
		if got := ModeloToken(central.US{Modelo: c.modelo, Esforco: st("medium")}); got != c.quer {
			t.Errorf("ModeloToken(%v) = %q, quer %q", c.modelo, got, c.quer)
		}
	}
}

// O pane de cada US com modelo recebe o token orq com o selo; o do coordenador segue com o resumo.
func TestTokensPublicaOModeloNoPaneDaUS(t *testing.T) {
	c := novoCenario(t)
	ctx := context.Background()
	co := c.coord("a", "w1:p1", "")
	c.onda(co, "1", "2", "3")
	for ref, pane := range map[string]string{"1": "w1:p2", "2": "w1:p3", "3": "w1:p4"} {
		c.status(co, ref, "exec", pane)
	}
	for ref, modelo := range map[string]string{"1": "claude-fable-5-2", "2": "llama-4"} { // a 3 fica sem modelo
		r, _ := refs.ParseUS(ref)
		m := modelo
		if _, _, err := c.st.AtualizarStatus(ctx, &co, r, central.MudancaStatus{Status: "exec", Modelo: &m}); err != nil {
			t.Fatal(err)
		}
	}
	c.sessao([]map[string]any{
		{"pane_id": "w1:p1", "workspace_id": "w1", "agent": "claude", "agent_status": "working"},
		{"pane_id": "w1:p2", "workspace_id": "w1", "agent": "claude", "agent_status": "working"},
		{"pane_id": "w1:p3", "workspace_id": "w1", "agent": "claude", "agent_status": "working"},
		{"pane_id": "w1:p4", "workspace_id": "w1", "agent": "claude", "agent_status": "working"},
	}, []map[string]any{{"workspace_id": "w1"}})
	c.atualizar(Opcoes{Fontes: []string{"herdr"}, Forcar: true})
	_ = c.chamadas("herdr")
	r, err := c.e.PublicarTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range c.chamadas("herdr") {
		got = append(got, strings.Join(a, " "))
	}
	for _, quer := range []string{
		"pane report-metadata w1:p1 --source orq --token orq=3 Exec",
		"pane report-metadata w1:p2 --source orq --token orq=[C] fable-5-2",
		"pane report-metadata w1:p3 --source orq --token orq=llama-4",
	} {
		if !contem(got, quer) {
			t.Errorf("falta %q em %q", quer, got)
		}
	}
	if len(got) != 3 || len(r.Publicados) != 3 {
		t.Errorf("chamadas = %q, resultado %+v (o pane sem modelo não recebe token)", got, r)
	}
	// US done libera o pane: o token do modelo sai.
	c.status(co, "1", "done", "")
	c.sessao([]map[string]any{
		{"pane_id": "w1:p1", "workspace_id": "w1", "agent": "claude", "agent_status": "working", "tokens": map[string]string{"orq": "3 Exec"}},
		{"pane_id": "w1:p2", "workspace_id": "w1", "agent": "claude", "agent_status": "working", "tokens": map[string]string{"orq": "[C] fable-5-2"}},
		{"pane_id": "w1:p3", "workspace_id": "w1", "agent": "claude", "agent_status": "working", "tokens": map[string]string{"orq": "llama-4"}},
		{"pane_id": "w1:p4", "workspace_id": "w1", "agent": "claude", "agent_status": "working"},
	}, []map[string]any{{"workspace_id": "w1"}})
	c.atualizar(Opcoes{Fontes: []string{"herdr"}, Forcar: true})
	_ = c.chamadas("herdr")
	if _, err := c.e.PublicarTokens(ctx); err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, a := range c.chamadas("herdr") {
		got = append(got, strings.Join(a, " "))
	}
	if len(got) < 1 || !contem(got, "pane report-metadata w1:p2 --source orq --clear-token orq") {
		t.Errorf("chamadas depois do done = %q", got)
	}
}
