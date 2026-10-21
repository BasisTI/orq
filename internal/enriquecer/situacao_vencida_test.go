package enriquecer

// Testes adversariais da orq #19: a mescla do GravarMR sob falhas que não são
// erro de subprocesso, falha com cara de autenticação, falhas seguidas e recuperação.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A consulta extra responde com saída 0 mas conteúdo que o parser não entende (lixo, ou JSON
// sem os campos): é uma falha da consulta extra tanto quanto um erro de rede. O último valor
// bom tem de ficar, com a falha visível — como no caso do subprocesso.
func TestSituacaoExtraComSaidaInvalidaPreservaUltimoBom(t *testing.T) {
	for _, k := range []struct{ nome, saida string }{
		{"lixo no stdout", "não é json"},
		{"json sem os campos", `{}`},
		{"campos nulos", `{"statusCheckRollup":null,"reviewDecision":null}`},
	} {
		t.Run(k.nome, func(t *testing.T) {
			c := novoCenario(t)
			co := c.coord("a", "w1:p1", "")
			c.onda(co, "1")
			c.mr(co, "1", urlGH)
			c.responder("gh", `{"state":"open","mergeable_state":"clean"}`, false, argsGH...)
			c.responder("gh", `{"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}],"reviewDecision":"APPROVED"}`, false, argsGHView...)
			c.atualizar(Opcoes{Fontes: []string{"mr"}})
			antes := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]
			if antes.Pipeline == nil || antes.Aprovacao == nil {
				t.Fatal("primeira consulta não populou")
			}

			c.rel.passar(61 * time.Second)
			c.responder("gh", k.saida, false, argsGHView...) // saída 0, conteúdo inútil
			c.atualizar(Opcoes{Fontes: []string{"mr"}})
			depois := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]
			if depois.Pipeline == nil || depois.Aprovacao == nil {
				t.Errorf("consulta extra inválida apagou o último bom: pipeline=%v aprovação=%v", depois.Pipeline, depois.Aprovacao)
			}
			if depois.SituacaoErro == nil {
				t.Errorf("consulta extra inválida não deixou falha visível: %+v", depois)
			}
		})
	}
}

// A consulta extra falha com cara de autenticação: mesmo código visível, estado da MR segue
// atualizado e a ferramenta NÃO é suspensa (decisão da story, "como já era").
func TestSituacaoFalhaExtraComCaraDeAutenticacaoNaoSuspende(t *testing.T) {
	c := novoCenario(t)
	co := c.coord("a", "w1:p1", "")
	c.onda(co, "1")
	c.mr(co, "1", urlGH)
	c.responder("gh", `{"state":"open","mergeable_state":"clean"}`, false, argsGH...)
	c.responder("gh", `{"statusCheckRollup":[],"reviewDecision":""}`, false, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})

	c.rel.passar(61 * time.Second)
	c.responder("gh", `{"state":"open","draft":true}`, false, argsGH...)
	c.responder("gh", "HTTP 401: Bad credentials (https://api.github.com/repos/BasisTI/orq/pulls/1)", true, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})
	m := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]
	if m.Estado == nil || *m.Estado != "draft" {
		t.Errorf("estado não avançou: %v", m.Estado)
	}
	if m.SituacaoErro == nil || m.SituacaoErro.Codigo == "" {
		t.Errorf("falha não visível: %+v", m.SituacaoErro)
	}
	sus, err := c.st.Suspensoes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sus) != 0 {
		t.Errorf("falha da consulta extra suspendeu a ferramenta: %v", sus)
	}
	// E a próxima consulta acontece (não houve suspensão escondida no Resultado).
	c.rel.passar(61 * time.Second)
	c.responder("gh", `{"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}],"reviewDecision":"APPROVED"}`, false, argsGHView...)
	r := c.atualizar(Opcoes{Fontes: []string{"mr"}})
	if r.Fontes["mr"].Suspensas != 0 {
		t.Errorf("consulta seguinte foi suspensa: %+v", r.Fontes["mr"])
	}
}

// Falhas seguidas: a idade dos valores (SituacaoEm) não anda sem leitura nova; a falha nova
// substitui a anterior; o estado principal continua avançando; sem suspensão.
func TestSituacaoFalhasSeguidasMantemIdadeEAtualizamFalha(t *testing.T) {
	c := novoCenario(t)
	co := c.coord("a", "w1:p1", "")
	c.onda(co, "1")
	c.mr(co, "1", urlGH)
	c.responder("gh", `{"state":"open","mergeable_state":"clean"}`, false, argsGH...)
	c.responder("gh", `{"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}],"reviewDecision":"APPROVED"}`, false, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})
	antes := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]

	c.rel.passar(61 * time.Second)
	c.responder("gh", "falha de rede um", true, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})
	meio := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]

	c.rel.passar(61 * time.Second)
	c.responder("gh", `{"state":"open","draft":true}`, false, argsGH...)
	c.responder("gh", "falha de rede dois", true, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})
	depois := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]

	if depois.Pipeline == nil || *depois.Pipeline != "sucesso" || depois.Aprovacao == nil || *depois.Aprovacao != "aprovada" {
		t.Errorf("valores não preservados: %+v", depois)
	}
	if depois.SituacaoEm == nil || *depois.SituacaoEm != *antes.SituacaoEm {
		t.Errorf("idade andou sem leitura nova: antes=%v depois=%v", antes.SituacaoEm, depois.SituacaoEm)
	}
	if meio.SituacaoEm == nil || *meio.SituacaoEm != *antes.SituacaoEm {
		t.Errorf("idade andou na 1ª falha: antes=%v meio=%v", antes.SituacaoEm, meio.SituacaoEm)
	}
	if depois.SituacaoErro == nil || !strings.Contains(depois.SituacaoErro.Mensagem, "dois") {
		t.Errorf("a falha nova não substituiu a anterior: %+v", depois.SituacaoErro)
	}
	if meio.SituacaoErro == nil || depois.SituacaoErro.Em == meio.SituacaoErro.Em {
		t.Errorf("o instante da falha não avançou: meio=%+v depois=%+v", meio.SituacaoErro, depois.SituacaoErro)
	}
	if depois.Estado == nil || *depois.Estado != "draft" {
		t.Errorf("estado principal não avançou: %v", depois.Estado)
	}
	sus, _ := c.st.Suspensoes(context.Background())
	if len(sus) != 0 {
		t.Errorf("suspensão indevida: %v", sus)
	}
}

// Recuperação depois da falha com resultado verdadeiro diferente do valor guardado: o PR ficou
// sem checks; o valor novo (sem/nao_exigida) substitui o último bom e a falha some.
func TestSituacaoRecuperacaoSubstituiUltimoBomPorValorVerdadeiro(t *testing.T) {
	c := novoCenario(t)
	co := c.coord("a", "w1:p1", "")
	c.onda(co, "1")
	c.mr(co, "1", urlGH)
	c.responder("gh", `{"state":"open","mergeable_state":"clean"}`, false, argsGH...)
	c.responder("gh", `{"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}],"reviewDecision":"APPROVED"}`, false, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})

	c.rel.passar(61 * time.Second)
	c.responder("gh", "falha de rede", true, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})

	c.rel.passar(61 * time.Second)
	c.responder("gh", `{"statusCheckRollup":[],"reviewDecision":""}`, false, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})
	m := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]
	if m.SituacaoErro != nil {
		t.Errorf("a falha ficou depois da recuperação: %+v", m.SituacaoErro)
	}
	if m.Pipeline == nil || *m.Pipeline != "sem" || m.Aprovacao == nil || *m.Aprovacao != "nao_exigida" {
		t.Errorf("valor novo não substituiu o último bom: %+v", m)
	}
	if m.SituacaoEm == nil {
		t.Errorf("recuperação sem idade: %+v", m)
	}
}

// Cache gravado pelo binário anterior (sem situacao_em, como o da US #16): a mescla preserva
// pipeline e aprovação, mas a idade dos valores não pode virar "nunca lidos" — eles existem.
func TestSituacaoMesclaComCacheDeBinarioAnterior(t *testing.T) {
	c := novoCenario(t)
	co := c.coord("a", "w1:p1", "")
	c.onda(co, "1")
	c.mr(co, "1", urlGH)
	// O binário da #16 gravava o valor sem situacao_em nem situacao_erro; GravarCache direto
	// reproduz exatamente essa linha (GravarMR desta versão já poria situacao_em).
	if err := c.st.GravarCache(context.Background(), "mr", urlGH, map[string]string{
		"estado": "aberta", "web_url": urlGH, "pipeline": "sucesso", "aprovacao": "aprovada",
	}, nil); err != nil {
		t.Fatal(err)
	}
	c.rel.passar(61 * time.Second) // fora do TTL: a consulta acontece
	c.responder("gh", `{"state":"open","mergeable_state":"clean"}`, false, argsGH...)
	c.responder("gh", "falha de rede", true, argsGHView...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}})
	m := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]
	if m.Pipeline == nil || *m.Pipeline != "sucesso" || m.Aprovacao == nil || *m.Aprovacao != "aprovada" {
		t.Errorf("a mescla não preservou os valores do cache antigo: %+v", m)
	}
	if m.SituacaoErro == nil {
		t.Errorf("falha não visível: %+v", m)
	}
	if m.SituacaoEm == nil {
		t.Errorf("valores existem mas a idade diz \"nunca lidos\": %+v", m)
	}
}
