package enriquecer

import (
	"context"
	"testing"
	"time"
)

// A consulta extra (gh pr view) falha depois de um sucesso: o enriquecedor e a Store mantêm
// pipeline e aprovação do último valor bom, com a falha e a idade à vista, e o estado da MR
// segue atualizado. A ferramenta não é suspensa.
func TestFalhaDaConsultaExtraPreservaUltimoBom(t *testing.T) {
	c := novoCenario(t)
	co := c.coord("a", "w1:p1", "")
	c.onda(co, "1")
	c.mr(co, "1", urlGH)
	c.responder("gh", `{"state":"open","mergeable_state":"clean"}`, false, argsGH...)
	c.responder("gh", `{"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}],"reviewDecision":"APPROVED"}`, false, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})
	antes := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]
	if antes.Pipeline == nil || antes.Aprovacao == nil || antes.SituacaoErro != nil || antes.SituacaoEm == nil {
		t.Fatalf("primeira consulta: %+v", antes)
	}

	c.rel.passar(61 * time.Second)
	c.responder("gh", `{"state":"open","draft":true}`, false, argsGH...)
	c.responder("gh", "falha de rede", true, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})
	depois := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]
	if depois.Pipeline == nil || *depois.Pipeline != "sucesso" || depois.Aprovacao == nil || *depois.Aprovacao != "aprovada" {
		t.Errorf("a falha da consulta extra apagou o último bom: pipeline=%v aprovação=%v", depois.Pipeline, depois.Aprovacao)
	}
	if depois.Estado == nil || *depois.Estado != "draft" {
		t.Errorf("o estado principal não foi atualizado: %v", depois.Estado)
	}
	if depois.SituacaoErro == nil || depois.SituacaoErro.Codigo == "" || depois.SituacaoErro.Em == "" {
		t.Errorf("falha não visível: %+v", depois.SituacaoErro)
	}
	if depois.SituacaoEm == nil || *depois.SituacaoEm != *antes.SituacaoEm {
		t.Errorf("a idade dos valores mudou sem leitura nova: antes=%v depois=%v", antes.SituacaoEm, depois.SituacaoEm)
	}
	if depois.Erro != nil {
		t.Errorf("a consulta principal não falhou: %+v", depois.Erro)
	}
	sus, err := c.st.Suspensoes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sus) != 0 {
		t.Errorf("consulta extra suspendeu a ferramenta: %v", sus)
	}

	// Voltou: valores novos, idade nova e a falha some.
	c.rel.passar(61 * time.Second)
	c.responder("gh", `{"statusCheckRollup":[{"status":"COMPLETED","conclusion":"FAILURE"}],"reviewDecision":"REVIEW_REQUIRED"}`, false, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})
	volta := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]
	if volta.SituacaoErro != nil || volta.Pipeline == nil || *volta.Pipeline != "falhou" || *volta.Aprovacao != "pendente" {
		t.Errorf("depois da recuperação: %+v", volta)
	}
}

// Sem último valor bom, a falha deixa os dois desconhecidos (nulos), mas visível.
func TestFalhaDaConsultaExtraSemValorAnterior(t *testing.T) {
	c := novoCenario(t)
	co := c.coord("a", "w1:p1", "")
	c.onda(co, "1")
	c.mr(co, "1", urlGH)
	c.responder("gh", `{"state":"open","mergeable_state":"clean"}`, false, argsGH...)
	c.responder("gh", "falha de rede", true, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})
	m := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]
	if m.Pipeline != nil || m.Aprovacao != nil || m.SituacaoEm != nil || m.SituacaoErro == nil {
		t.Errorf("%+v", m)
	}
	if m.Estado == nil || *m.Estado != "pronta" {
		t.Errorf("estado: %v", m.Estado)
	}
}

// Só SUCCESS, NEUTRAL e SKIPPED são sucesso; STALE não é.
func TestCheckStaleNaoESucesso(t *testing.T) {
	p, _ := ParseSituacaoGitHub([]byte(`{"statusCheckRollup":[{"status":"COMPLETED","conclusion":"STALE"}],"reviewDecision":""}`))
	if p == "sucesso" {
		t.Fatal("check STALE foi apresentado como sucesso")
	}
	if p != "pendente" {
		t.Errorf("STALE = %q, quer pendente", p)
	}
}

// Conclusão que esta versão não conhece é conservadora (pendente), com ou sem status; falha
// conhecida e sucesso conhecido seguem iguais, e um check ruim não deixa o conjunto verde.
func TestCheckComConclusaoDesconhecidaNaoESucesso(t *testing.T) {
	for _, k := range []struct{ nome, raw, quer string }{
		{"conclusão futura", `{"statusCheckRollup":[{"status":"COMPLETED","conclusion":"ALGO_NOVO"}]}`, "pendente"},
		{"estado futuro", `{"statusCheckRollup":[{"state":"ALGO_NOVO"}]}`, "pendente"},
		{"concluído sem conclusão", `{"statusCheckRollup":[{"status":"COMPLETED","conclusion":""}]}`, "pendente"},
		{"desconhecida entre sucessos", `{"statusCheckRollup":[{"state":"SUCCESS"},{"status":"COMPLETED","conclusion":"ALGO_NOVO"}]}`, "pendente"},
		{"falha vence desconhecida", `{"statusCheckRollup":[{"status":"COMPLETED","conclusion":"ALGO_NOVO"},{"status":"COMPLETED","conclusion":"FAILURE"}]}`, "falhou"},
		{"minúsculas", `{"statusCheckRollup":[{"status":"completed","conclusion":"stale"}]}`, "pendente"},
		{"sucesso continua", `{"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"NEUTRAL"},{"status":"COMPLETED","conclusion":"SKIPPED"}]}`, "sucesso"},
	} {
		if p, _ := ParseSituacaoGitHub([]byte(k.raw)); p != k.quer {
			t.Errorf("%s: %q, quer %q", k.nome, p, k.quer)
		}
	}
}
