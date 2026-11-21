package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
)

func TestCentralSecaoMensagensEEnvio(t *testing.T) {
	c := vazia(t, false)
	co, o := c.coord("alfa", "wA:p1", "proj", 7, "merge")

	// Na Central (/) e em /projeto/{slug}, a seção <details> existe fechada por padrão,
	// acima de .us-lista, com título "Mensagens ao coordenador (0)".
	for _, rota := range []string{"/", "/projeto/proj"} {
		code, html := c.get(rota)
		if code != 200 {
			t.Fatalf("GET %s = %d", rota, code)
		}
		sec := fmt.Sprintf(`<details class="onda-msgs" id="onda-%d-msgs-sec">`, o.ID)
		contem(t, html, sec, "Mensagens ao coordenador", fmt.Sprintf(`id="onda-%d-msgs-n">(0)</span>`, o.ID),
			fmt.Sprintf(`action="/onda/%d/mensagens"`, o.ID), `name="texto"`)
		if strings.Index(html, sec) > strings.Index(html, `<div class="us-lista">`) {
			t.Errorf("%s: seção de mensagens não está acima de .us-lista", rota)
		}
		if temInline(html) {
			t.Errorf("%s: tem script ou style inline", rota)
		}
	}

	// Envio sem HX-Request -> 303 para /#onda-<id>.
	rotaPost := fmt.Sprintf("/onda/%d/mensagens", o.ID)
	w := c.f.faz(req{
		metodo:  "POST",
		caminho: rotaPost,
		corpo:   url.Values{"texto": {"Primeira mensagem\nlinha 2"}}.Encode(),
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST sem HX = %d, quer 303: %s", w.Code, corpo(w))
	}
	if loc := w.Header().Get("Location"); loc != fmt.Sprintf("/#onda-%d", o.ID) {
		t.Fatalf("Location = %q", loc)
	}

	// Envio com HX-Request -> 200 com fragmento da lista, OOB (2) e #aviso.
	w = c.f.faz(req{
		metodo:  "POST",
		caminho: rotaPost,
		corpo:   url.Values{"texto": {"Segunda <b>mensagem</b>"}}.Encode(),
		cab:     map[string]string{"HX-Request": "true"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("POST com HX = %d: %s", w.Code, corpo(w))
	}
	frag := corpo(w)
	contem(t, frag,
		fmt.Sprintf(`id="onda-%d-msgs"`, o.ID),
		fmt.Sprintf(`id="onda-%d-msgs-n" hx-swap-oob="true">(2)</span>`, o.ID),
		`id="aviso"`, "Mensagem enviada ao coordenador.",
		"Segunda &lt;b&gt;mensagem&lt;/b&gt;",
		"aguardando envio",
	)
	naoContem(t, frag, "<b>mensagem</b>")
	// Ordem decrescente: Segunda antes de Primeira.
	if strings.Index(frag, "Segunda") > strings.Index(frag, "Primeira") {
		t.Errorf("mensagens fora de ordem decrescente:\n%s", frag)
	}

	// Resposta do coordenador em markdown (goldmark sem WithUnsafe) e escape de XSS/links.
	ms, err := c.st.ListarMensagens(c.ctx, central.FiltroMensagens{OndaID: o.ID})
	c.ok(err)
	_, err = c.st.ResponderMensagem(c.ctx, &co, ms[0].ID, "**Tudo certo**\n\n<script>alert(1)</script>\n\n[link](javascript:alert(1)) e [seguro](https://example.com)")
	c.ok(err)

	_, html := c.get("/")
	contem(t, html, "<strong>Tudo certo</strong>", "&lt;script&gt;alert(1)&lt;/script&gt;", `href="#"`, `href="https://example.com"`, "Resposta de <b>alfa</b>")
	naoContem(t, html, "<script>alert(1)</script>", "javascript:alert(1)")
}

func TestMensagensSegurancaEErros(t *testing.T) {
	// Socket Unix sem cabeçalho de usuário -> 401.
	cu := vazia(t, true)
	_, ou := cu.coord("alfa", "wA:p1", "proj", 7, "")
	w := cu.f.faz(req{
		metodo:  "POST",
		caminho: fmt.Sprintf("/onda/%d/mensagens", ou.ID),
		corpo:   "texto=oi",
		cab:     map[string]string{"X-Orq-User": ""},
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Unix sem usuário = %d, quer 401", w.Code)
	}

	// Cross-origin -> 403.
	w = cu.f.faz(req{
		metodo:  "POST",
		caminho: fmt.Sprintf("/onda/%d/mensagens", ou.ID),
		corpo:   "texto=oi",
		cab:     map[string]string{"Origin": "https://evil.exemplo", "Sec-Fetch-Site": "cross-site"},
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin = %d, quer 403", w.Code)
	}

	// Erros diversos no modo HX: retarget para find .erro.
	c := vazia(t, false)
	_, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	rota := fmt.Sprintf("/onda/%d/mensagens", o.ID)

	// 404 onda inexistente / inválida.
	for _, p := range []string{"/onda/999/mensagens", "/onda/abc/mensagens", "/onda/0/mensagens"} {
		w := c.f.faz(req{metodo: "POST", caminho: p, corpo: "texto=oi", cab: map[string]string{"HX-Request": "true"}})
		if w.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, quer 404", p, w.Code)
		}
	}

	// 400 vazio e caractere de controle (inclusive \r solto).
	for _, body := range []string{"texto=", "texto=%20%20", "texto=oi%00x", "texto=antes%0ddepois"} {
		w := c.f.faz(req{metodo: "POST", caminho: rota, corpo: body, cab: map[string]string{"HX-Request": "true"}})
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %q = %d, quer 400", body, w.Code)
		}
		if w.Header().Get("HX-Retarget") != "find .erro" || w.Header().Get("HX-Reswap") != "innerHTML" {
			t.Errorf("body %q sem retarget: %v", body, w.Header())
		}
	}

	// 400 sem HX-Request traz título coerente ("Mensagem recusada", não "Resposta recusada").
	w = c.f.faz(req{metodo: "POST", caminho: rota, corpo: "texto="})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("400 sem HX = %d", w.Code)
	}
	contem(t, corpo(w), "Mensagem recusada")
	naoContem(t, corpo(w), "Resposta recusada")

	// 8.190 bytes com 30 quebras de linha enviadas como CRLF (8.220 bytes brutos) -> 200 após normalização.
	textoCRLF := strings.Repeat("a\r\n", 30) + strings.Repeat("b", 8190-60)
	if len(textoCRLF) != 8220 {
		t.Fatalf("len(textoCRLF) = %d, quer 8220", len(textoCRLF))
	}
	w = c.f.faz(req{metodo: "POST", caminho: rota, corpo: url.Values{"texto": {textoCRLF}}.Encode(), cab: map[string]string{"HX-Request": "true"}})
	if w.Code != http.StatusOK {
		t.Fatalf("CRLF 8190 LF (8220 bruto) = %d, quer 200: %s", w.Code, corpo(w))
	}

	// 413 texto > 8 KiB (mesmo após normalização) e corpo > 16 KiB.
	grande8k := url.Values{"texto": {strings.Repeat("a", LimiteTexto+1)}}.Encode()
	w = c.f.faz(req{metodo: "POST", caminho: rota, corpo: grande8k, cab: map[string]string{"HX-Request": "true"}})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("> 8 KiB = %d, quer 413", w.Code)
	}
	grande16k := "texto=" + strings.Repeat("a", LimiteCorpo+10)
	w = c.f.faz(req{metodo: "POST", caminho: rota, corpo: grande16k, cab: map[string]string{"HX-Request": "true"}})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("> 16 KiB = %d, quer 413", w.Code)
	}

	// 409 onda encerrada.
	_, err := c.st.EncerrarOnda(c.ctx, "alfa", o.ID)
	c.ok(err)
	w = c.f.faz(req{metodo: "POST", caminho: rota, corpo: "texto=oi", cab: map[string]string{"HX-Request": "true"}})
	if w.Code != http.StatusConflict {
		t.Fatalf("onda encerrada = %d, quer 409", w.Code)
	}
	if w.Header().Get("HX-Retarget") != "find .erro" {
		t.Errorf("409 HX sem retarget: %v", w.Header())
	}
}

func TestVivoAtualizaMensagensSemTocarNoFormulario(t *testing.T) {
	c := vazia(t, false)
	co, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	q := queryDaPagina(t, c, "/")

	// Envia mensagem e registra falha (sem coordenador).
	m, err := c.st.EnviarMensagem(c.ctx, o.ID, "Pergunta ao vivo", "cedric")
	c.ok(err)
	c.ok(c.st.RegistrarFalhaAvisoMensagem(c.ctx, m.ID, "agent_not_found"))

	w := c.f.faz(req{metodo: "GET", caminho: "/vivo?" + q.Encode(), cab: map[string]string{"HX-Request": "true", "HX-Current-URL": "http://127.0.0.1:8080/"}})
	if w.Code != http.StatusOK {
		t.Fatalf("/vivo = %d", w.Code)
	}
	body := corpo(w)
	contem(t, body,
		fmt.Sprintf(`id="onda-%d-msgs-n" hx-swap-oob="true">(1)</span>`, o.ID),
		fmt.Sprintf(`id="onda-%d-msgs" hx-swap-oob="true"`, o.ID),
		"Pergunta ao vivo", "sem coordenador",
	)
	naoContem(t, body, "<textarea", "<form")

	// Marca avisada -> estado vira "enviada ao coordenador".
	c.ok(c.st.MarcarMensagemAvisada(c.ctx, m.ID))
	w = c.f.faz(req{metodo: "GET", caminho: "/vivo?" + q.Encode(), cab: map[string]string{"HX-Request": "true", "HX-Current-URL": "http://127.0.0.1:8080/"}})
	contem(t, corpo(w), "enviada ao coordenador")
	naoContem(t, corpo(w), "sem coordenador", "<textarea")

	// Coordenador responde -> /vivo traz a resposta renderizada sem tocar na textarea.
	_, err = c.st.ResponderMensagem(c.ctx, &co, m.ID, "Respondido com *ênfase*")
	c.ok(err)
	w = c.f.faz(req{metodo: "GET", caminho: "/vivo?" + q.Encode(), cab: map[string]string{"HX-Request": "true", "HX-Current-URL": "http://127.0.0.1:8080/"}})
	contem(t, corpo(w), "Respondido com <em>ênfase</em>")
	naoContem(t, corpo(w), "enviada ao coordenador", "<textarea")
}
