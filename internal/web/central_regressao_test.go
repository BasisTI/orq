package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// Usa o formato produzido por limpeza.eventos e propagar, sem recursos reais.
func TestRegressaoDiarioNaoExpoePaneDaLimpeza(t *testing.T) {
	c := vazia(t, false)
	co, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	c.ok(c.st.RegistrarExternos(c.ctx, &co, refs.US{Projeto: "proj", Ref: 7}, []central.EventoExterno{
		{Tipo: "limpar.removido", Detalhe: map[string]any{"tipo": "pane", "alvo": "wA:p9 (worker-secreto)"}},
		{Tipo: "limpar.preservado", Detalhe: map[string]any{"tipo": "worktree", "alvo": "/tmp/repo", "motivo": "pane wA:p9 (worker-secreto) ficou preservado"}},
	}))
	code, html := c.get(fmt.Sprintf("/onda/%d", o.ID))
	if code != 200 {
		t.Fatal(code)
	}
	if strings.Contains(html, "wA:p9") {
		t.Error("id de pane da limpeza exposto no diário")
	}
}

func TestRegressaoArquivoDaPendenciaJaEstaNoSnapshot(t *testing.T) {
	c := vazia(t, false)
	_, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	_, err := c.st.RegistrarPendencia(c.ctx, central.NovaPendencia{US: refs.US{Projeto: "proj", Ref: 7}, Tipo: "duvida", Titulo: "Pergunta", Autor: "worker", Detalhe: "Contexto", Slug: "s", Hash: "h", Arquivo: "/tmp/somente-diario-segredo/contexto.md"})
	c.ok(err)
	snap, err := c.st.Snapshot(c.ctx, true)
	c.ok(err)
	b, err := json.Marshal(snap)
	c.ok(err)
	if !strings.Contains(string(b), "somente-diario-segredo") {
		t.Fatal("arquivo não encontrado no snapshot")
	}
	_, html := c.get(fmt.Sprintf("/onda/%d", o.ID))
	if !strings.Contains(html, "somente-diario-segredo") {
		t.Error("arquivo não encontrado no diário")
	}
}

// A story pede a situação de pipeline e aprovação por MR, além de estado e link.
func TestRegressaoMRMostraPipelineEAprovacao(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	url := "https://github.com/Exemplo/repo/pull/20"
	_, _, err := c.st.AdicionarMR(c.ctx, &co, refs.US{Projeto: "proj", Ref: 7}, url)
	c.ok(err)
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: "aberta"}, nil))
	_, html := c.get("/central")
	row := linhaMR(t, html)
	// A legenda geral na coluna lateral não informa a situação desta MR.
	if !strings.Contains(row, "pipeline") || !strings.Contains(row, "aprovação") || strings.Count(row, "desconhecido") < 2 {
		t.Errorf("linha da MR não informa pipeline e aprovação individualmente, nem indica desconhecido: %s", row)
	}
}

func TestRegressaoEscapeDeTodasAsFontes(t *testing.T) {
	c := vazia(t, false)
	co, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	payload := `<img src=x onerror=alert(1)>`
	c.taiga(u, payload, payload)
	url := "https://github.com/Exemplo/repo/pull/20"
	_, _, err := c.st.AdicionarMR(c.ctx, &co, u, url)
	c.ok(err)
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: payload}, nil))
	_, err = c.st.RegistrarPendencia(c.ctx, central.NovaPendencia{US: u, Tipo: "duvida", Titulo: payload, Autor: "worker", Detalhe: payload, Slug: "s", Hash: "h"})
	c.ok(err)
	c.ok(c.st.RegistrarExternos(c.ctx, &co, u, []central.EventoExterno{{Tipo: "limpar.falhou", Detalhe: map[string]any{"motivo": payload}}}))
	for _, path := range []string{"/central", fmt.Sprintf("/onda/%d", o.ID), "/", "/p/proj/7/1"} {
		code, html := c.get(path)
		if code != 200 {
			t.Errorf("%s: %d", path, code)
		}
		if strings.Contains(html, payload) {
			t.Errorf("%s: HTML não escapado", path)
		}
	}
}

func TestRegressaoMetodosCabecalhosEIdentidade(t *testing.T) {
	c := vazia(t, true)
	_, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	before, err := c.st.Diario(c.ctx, nil, central.FiltroDiario{})
	c.ok(err)
	for _, path := range []string{"/central", fmt.Sprintf("/onda/%d", o.ID)} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			w := c.f.faz(req{metodo: method, caminho: path})
			want := http.StatusMethodNotAllowed
			if method == "GET" || method == "HEAD" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Errorf("%s %s: %d", method, path, w.Code)
			}
			for k, v := range map[string]string{"Content-Security-Policy": csp, "X-Content-Type-Options": "nosniff", "Cache-Control": "no-store", "Referrer-Policy": "no-referrer"} {
				if w.Header().Get(k) != v {
					t.Errorf("%s %s: %s ausente", method, path, k)
				}
			}
			w = c.f.faz(req{metodo: method, caminho: path, cab: map[string]string{"X-Orq-User": ""}})
			if w.Code != 401 {
				t.Errorf("%s %s sem usuário: %d", method, path, w.Code)
			}
		}
	}
	after, err := c.st.Diario(c.ctx, nil, central.FiltroDiario{})
	c.ok(err)
	if len(before) != len(after) {
		t.Fatal("rota escreveu evento")
	}
}

func TestRegressaoPaginacaoEIDsExtremos(t *testing.T) {
	c := vazia(t, false)
	_, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	for _, id := range []string{"-9223372036854775808", "9223372036854775808", "+1", "1e0", "01", "abc", "0", "-1"} {
		if code, _ := c.get("/onda/" + id); code != 404 {
			t.Errorf("%s: %d", id, code)
		}
	}
	for _, page := range []string{"-9223372036854775808", "9223372036854775808", "-1", "0", "abc", "99999999999999999999999999"} {
		if code, _ := c.get(fmt.Sprintf("/onda/%d?pagina=%s", o.ID, page)); code != 200 {
			t.Errorf("%s: %d", page, code)
		}
	}
}

func TestRegressaoCacheFalhouEAposPrazo(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	c.taiga(u, "Título", "New")
	_, _, err := c.st.AtualizarStatus(c.ctx, &co, u, central.MudancaStatus{Status: "exec", Pane: ptr("wA:p9")})
	c.ok(err)
	url := "https://github.com/Exemplo/repo/pull/20"
	_, _, err = c.st.AdicionarMR(c.ctx, &co, u, url)
	c.ok(err)
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: "pronta"}, nil))
	c.herdr(map[string]central.PaneHerdr{"wA:p9": {Status: "working"}})
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{}, &central.Erro{Codigo: "falha_comando"}))
	future := time.Now().Add(time.Hour)
	c.f.srv.Agora = func() time.Time { return future }
	_, html := c.get("/central")
	contem(t, html, ">pronta<", "falhou: falha_comando", "trabalhando", "Dados vencidos")
	if n := strings.Count(html, `title="o cache passou do prazo da fonte"`); n < 5 {
		t.Errorf("só %d fontes/valores marcados", n)
	}
}

func TestMRMostraPipelineEAprovacaoDoCache(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	casos := []struct {
		pr           int
		valor        central.ValorMR
		quer, naoTem []string
	}{
		{20, central.ValorMR{Estado: "aberta", Pipeline: "falhou", Aprovacao: "pendente"}, []string{"selo-erro", ">falhou<", "selo-aviso", ">pendente<"}, []string{"desconhecido"}},
		{21, central.ValorMR{Estado: "pronta", Pipeline: "sucesso", Aprovacao: "nao_exigida"}, []string{"selo-ok", ">sucesso<", "não exigida"}, []string{"desconhecido"}},
		{22, central.ValorMR{Estado: "aberta", Pipeline: "sem", Aprovacao: "aprovada"}, []string{"sem pipeline", ">aprovada<"}, []string{"desconhecido"}},
		{23, central.ValorMR{Estado: "aberta"}, []string{"selo-tracejado", "desconhecido"}, nil}, // forja antiga: campos ausentes
	}
	for _, k := range casos {
		url := fmt.Sprintf("https://github.com/Exemplo/repo/pull/%d", k.pr)
		_, _, err := c.st.AdicionarMR(c.ctx, &co, u, url)
		c.ok(err)
		c.ok(c.st.GravarMR(c.ctx, url, k.valor, nil))
	}
	_, html := c.get("/central")
	for _, k := range casos {
		_, resto, _ := strings.Cut(html, fmt.Sprintf("repo#%d</b>", k.pr))
		linha, _, _ := strings.Cut(resto, "</li>")
		contem(t, linha, k.quer...)
		naoContem(t, linha, k.naoTem...)
	}
	// A legenda não promete aprovação para "pronta".
	naoContem(t, html, "pipeline e aprovação em dia")
}

func TestDiarioSoMostraCamposConhecidos(t *testing.T) {
	c := vazia(t, false)
	co, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	c.ok(c.st.RegistrarExternos(c.ctx, &co, u, []central.EventoExterno{
		// Tipo conhecido, campo desconhecido e pane no texto, em formatos variados.
		{Tipo: "limpar.preservado", Detalhe: map[string]any{"tipo": "worktree", "alvo": "/tmp/repo", "extra": "wA:p9", "motivo": "pane wA:p9 (worker-secreto) ficou preservado; tab w1:t2 também; wB:p10(sem espaço)"}},
		{Tipo: "limpar.removido", Detalhe: map[string]any{"tipo": "pane", "alvo": "wA:p9 (worker-secreto)"}},
		{Tipo: "limpar.falhou", Detalhe: map[string]any{"tipo": "docker", "alvo": "container abc123", "motivo": "erro em https://exemplo.com:8080/x e host:porta"}},
	}))
	code, html := c.get(fmt.Sprintf("/onda/%d", o.ID))
	if code != 200 {
		t.Fatal(code)
	}
	naoContem(t, html, "wA:p9", "w1:t2", "wB:p10", "worker-secreto", "extra")
	contem(t, html, "[pane]", "ficou preservado", "/tmp/repo", "container abc123", "https://exemplo.com:8080/x e host:porta")
	// O alvo do pane some inteiro: do tipo "pane" não sobra o JSON do alvo.
	if strings.Contains(html, `"tipo":"pane"`) && strings.Contains(html, `"alvo":"[pane]"`) {
		t.Error("alvo de pane aparece mesmo mascarado")
	}
}

func TestMascararIdsDeHerdr(t *testing.T) {
	for in, quer := range map[string]string{
		"wA:p9":                     "[pane]",
		"wA:p9 (worker-secreto)":    "[pane]",
		"pane wA:p1 e wB:t2 (x y)":  "pane [pane] e [pane]",
		"sem id aqui":               "sem id aqui",
		"https://w.exemplo.com:80/": "https://w.exemplo.com:80/",
		"hostw:pagina":              "hostw:pagina",
	} {
		if got := mascarar(in); got != quer {
			t.Errorf("mascarar(%q) = %q, quer %q", in, got, quer)
		}
	}
}

// Tipo de evento que esta versão não conhece (de um binário mais novo, por exemplo) não mostra
// detalhe: pane em campo arbitrário, aninhado ou não, não sai.
func TestDetalheDeTipoDesconhecidoEOmitido(t *testing.T) {
	raw := json.RawMessage(`{"qualquer":"wA:p9 (worker-secreto)","aninhado":{"x":"wB:t3"},"pane_id":"wA:p1"}`)
	if got, cortado := textoDetalhe("limpar.novo", raw); got != detalheOmitido || cortado {
		t.Errorf("tipo desconhecido = %q", got)
	}
	// Tipo conhecido: só os campos da lista, e o texto mascarado, inclusive em objeto aninhado.
	raw = json.RawMessage(`{"id":"TG-1#P1","por":"ana","anterior":{"texto":"wA:p9 (x)","pane_id":"wA:p9"},"pane_origem":"wA:p9","qualquer":1}`)
	got, _ := textoDetalhe("pend.responder", raw)
	for _, nao := range []string{"wA:p9", "pane_origem", "pane_id", "qualquer"} {
		if strings.Contains(got, nao) {
			t.Errorf("%q aparece em %s", nao, got)
		}
	}
	if !strings.Contains(got, "TG-1#P1") || !strings.Contains(got, "[pane]") {
		t.Errorf("detalhe = %s", got)
	}
	// Detalhe que não é objeto também é omitido.
	if got, _ := textoDetalhe("mr.rm", json.RawMessage(`"wA:p9"`)); got != detalheOmitido {
		t.Errorf("não-objeto = %q", got)
	}
	// Todo tipo gravado por esta versão tem entrada na lista (se não, o detalhe some sem ninguém ver).
	for _, tipo := range append([]string{"us.status", "mr.estado", "onda.criar", "pend.add"}, central.TiposExternos...) {
		if _, ok := camposDoDiario[tipo]; !ok {
			t.Errorf("tipo %s sem lista de campos", tipo)
		}
	}
}

// A consulta extra falhou depois de um sucesso: a página mantém pipeline e aprovação, com a
// falha e a idade dos valores ao lado.
func TestCentralMostraFalhaDaConsultaExtra(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	url := "https://github.com/Exemplo/repo/pull/20"
	_, _, err := c.st.AdicionarMR(c.ctx, &co, refs.US{Projeto: "proj", Ref: 7}, url)
	c.ok(err)
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: "aberta", Pipeline: "sucesso", Aprovacao: "aprovada"}, nil))
	c.ok(c.st.GravarMR(c.ctx, url, central.ValorMR{Estado: "pronta", SituacaoErro: &central.Erro{Codigo: "falha_comando", Mensagem: "rede"}}, nil))
	_, html := c.get("/central")
	row := linhaMR(t, html)
	for _, quer := range []string{">sucesso<", ">aprovada<", "consulta extra falhou: falha_comando"} {
		if !strings.Contains(row, quer) {
			t.Errorf("falta %q em %s", quer, row)
		}
	}
	if strings.Contains(row, "desconhecido") {
		t.Errorf("o último valor bom virou desconhecido: %s", row)
	}
}
