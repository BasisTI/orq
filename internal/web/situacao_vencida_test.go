package web

// Testes adversariais da orq #19 na página /: o selo da consulta extra
// (escape e exposição), a primeira falha sem valor anterior, e o último valor bom velho.

import (
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// O selo mostra só o código do erro, escapado; a mensagem da ferramenta (que pode trazer
// qualquer texto) não aparece.
func TestSituacaoSeloDaConsultaExtraEscapadoENaoExpoeMensagem(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	url := "https://github.com/Exemplo/repo/pull/20"
	_, _, err := c.st.AdicionarMR(c.ctx, &co, u, url)
	c.ok(err)
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: "aberta", Pipeline: "sucesso", Aprovacao: "aprovada"}, nil))
	payload := `<img src=x onerror=alert(1)>`
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: "aberta",
		SituacaoErro: &central.Erro{Codigo: payload, Mensagem: "segredo-mensagem " + payload}}, nil))
	_, html := c.get("/")
	naoContem(t, html, payload, "segredo-mensagem")
	contem(t, html, "consulta extra falhou: ")
}

// A primeira consulta da MR já falha na parte extra: pipeline e aprovação desconhecidos, a
// falha à vista e a idade dita como "nunca lidos".
func TestSituacaoPrimeiraFalhaSemValorAnteriorNaPagina(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	url := "https://github.com/Exemplo/repo/pull/20"
	_, _, err := c.st.AdicionarMR(c.ctx, &co, refs.US{Projeto: "proj", Ref: 7}, url)
	c.ok(err)
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: "pronta", SituacaoErro: &central.Erro{Codigo: "falha_comando", Mensagem: "rede"}}, nil))
	_, html := c.get("/")
	row := linhaMR(t, html)
	contem(t, row, "valores lidos: nunca", "consulta extra falhou: falha_comando")
	if n := strings.Count(row, ">desconhecido<"); n != 2 {
		t.Errorf("desconhecido ×%d, quer 2: %s", n, row)
	}
}

// O último valor bom pode ficar eternamente velho: a idade aparece, mas a marca de vencido do
// projeto ("Dado vencido aparece como vencido", README) tem de acompanhar a idade.
func TestSituacaoUltimoBomVelhoApareceComoVencido(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	url := "https://github.com/Exemplo/repo/pull/20"
	_, _, err := c.st.AdicionarMR(c.ctx, &co, u, url)
	c.ok(err)
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: "aberta", Pipeline: "sucesso", Aprovacao: "aprovada"}, nil))
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: "aberta", SituacaoErro: &central.Erro{Codigo: "falha_comando", Mensagem: "rede"}}, nil))
	// Uma hora sem conseguir renovar pipeline e aprovação, com o estado da MR em dia.
	depois := time.Now().Add(time.Hour)
	c.f.srv.Agora = func() time.Time { return depois }
	_, html := c.get("/")
	row := linhaMR(t, html)
	contem(t, row, "valores lidos: há 1 h", "consulta extra falhou: falha_comando", "vencido")
}

// Cache no formato do binário anterior (com pipeline e aprovação, sem situacao_em): a página
// mantém os valores, mas não pode dizer "(nunca lidos)" para valores que estão à vista.
func TestSituacaoMesclaComCacheAntigoNaoDizNuncaLidos(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	url := "https://github.com/Exemplo/repo/pull/20"
	_, _, err := c.st.AdicionarMR(c.ctx, &co, u, url)
	c.ok(err)
	c.ok(c.st.GravarCache(c.ctx, central.FonteMR, url, map[string]string{
		"estado": "aberta", "web_url": url, "pipeline": "sucesso", "aprovacao": "aprovada",
	}, nil))
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: "aberta", SituacaoErro: &central.Erro{Codigo: "falha_comando", Mensagem: "rede"}}, nil))
	_, html := c.get("/")
	row := linhaMR(t, html)
	contem(t, row, ">sucesso<", ">aprovada<", "consulta extra falhou: falha_comando")
	naoContem(t, row, "valores lidos: nunca")
}
