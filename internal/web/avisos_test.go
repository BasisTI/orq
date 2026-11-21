package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

func lerAvisos(t *testing.T, c *cena) (string, Avisos) {
	t.Helper()
	w := c.f.faz(req{metodo: "GET", caminho: "/avisos"})
	if w.Code != 200 {
		t.Fatalf("GET /avisos = %d: %s", w.Code, corpo(w))
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, quer no-store", cc)
	}
	raw := corpo(w)
	var a Avisos
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, raw)
	}
	return raw, a
}

func TestAvisosPendenciaNova(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "merge")
	_, a := lerAvisos(t, c)
	if len(a.Itens) != 0 {
		t.Fatalf("sem pendência: %+v", a.Itens)
	}
	u := refs.US{Projeto: "proj", Ref: 7}
	_, err := c.st.AbrirPendencia(c.ctx, &co, u, "decisao", "Qual\n biblioteca <b>usar</b>?", "")
	c.ok(err)
	raw, a := lerAvisos(t, c)
	if len(a.Itens) != 1 {
		t.Fatalf("itens = %+v", a.Itens)
	}
	it := a.Itens[0]
	if it.Tipo != "pendencia" || it.Chave != "proj/7#P1" || it.Projeto != "proj" || it.Href != "/p/proj/7/1" || it.Titulo != "Qual biblioteca <b>usar</b>?" {
		t.Errorf("item = %+v", it)
	}
	if !strings.HasPrefix(it.ID, "p:proj/7#P1@") {
		t.Errorf("id = %q", it.ID)
	}
	// O id é estável entre consultas (é o que as abas usam para não repetir).
	if _, b := lerAvisos(t, c); b.Itens[0].ID != it.ID {
		t.Errorf("id mudou: %q -> %q", it.ID, b.Itens[0].ID)
	}
	// Só os campos combinados vão no JSON.
	var m map[string][]map[string]any
	_ = json.Unmarshal([]byte(raw), &m)
	for k := range m["itens"][0] {
		switch k {
		case "id", "tipo", "chave", "titulo", "projeto", "href":
		default:
			t.Errorf("campo inesperado no JSON: %q", k)
		}
	}
	// Uma segunda pendência é um segundo item; a respondida pela pessoa sai (já foi vista).
	_, err = c.st.AbrirPendencia(c.ctx, &co, u, "duvida", "Outra", "")
	c.ok(err)
	_, a = lerAvisos(t, c)
	if len(a.Itens) != 2 {
		t.Fatalf("itens = %+v", a.Itens)
	}
	_, err = c.st.ResponderPendencia(c.ctx, nil, refs.Pend{US: u, Numero: 1}, central.Resposta{Texto: "segredo-da-pessoa", Por: "ana"})
	c.ok(err)
	raw, a = lerAvisos(t, c)
	if len(a.Itens) != 1 || a.Itens[0].Chave != "proj/7#P2" {
		t.Fatalf("depois de responder: %+v", a.Itens)
	}
	if strings.Contains(raw, "segredo-da-pessoa") {
		t.Errorf("texto da pessoa vazou em /avisos: %s", raw)
	}
}

func TestAvisosRespostaDoCoordenadorSemTextoLivre(t *testing.T) {
	c := vazia(t, false)
	co, o := c.coord("alfa", "wA:p1", "proj", 7, "merge")
	m, err := c.st.EnviarMensagem(c.ctx, o.ID, "pergunta-sigilosa da pessoa", "ana")
	c.ok(err)
	if _, a := lerAvisos(t, c); len(a.Itens) != 0 {
		t.Fatalf("mensagem sem resposta não avisa: %+v", a.Itens)
	}
	_, err = c.st.ResponderMensagem(c.ctx, &co, m.ID, "resposta-sigilosa do coordenador")
	c.ok(err)
	raw, a := lerAvisos(t, c)
	if len(a.Itens) != 1 {
		t.Fatalf("itens = %+v", a.Itens)
	}
	it := a.Itens[0]
	if it.Tipo != "resposta" || !strings.HasPrefix(it.ID, "m:") || !strings.HasPrefix(it.Href, "/#msg-") || it.Titulo != "Onda de alfa" {
		t.Errorf("item = %+v", it)
	}
	for _, s := range []string{"pergunta-sigilosa", "resposta-sigilosa"} {
		if strings.Contains(raw, s) {
			t.Errorf("%q vazou em /avisos: %s", s, raw)
		}
	}
	// O destino do clique existe na Central: o item da mensagem tem esse id.
	_, html := c.get("/")
	contem(t, html, `id="`+strings.TrimPrefix(it.Href, "/#")+`"`)
}

func TestAvisosTituloLimitadoELimpo(t *testing.T) {
	if got := tituloAviso("a\x1b[31m  b\t\nc"); got != "a [31m b c" {
		t.Errorf("tituloAviso = %q", got)
	}
	long := strings.Repeat("ã", 300)
	if got := []rune(tituloAviso(long)); len(got) != limiteTituloAviso || got[len(got)-1] != '…' {
		t.Errorf("título longo: %d runas", len(got))
	}
}

func TestAvisosExigeUsuarioNoSocketUnix(t *testing.T) {
	c := vazia(t, true)
	w := c.f.faz(req{metodo: "GET", caminho: "/avisos", cab: map[string]string{"X-Orq-User": ""}})
	if w.Code != 401 {
		t.Errorf("sem X-Orq-User = %d, quer 401", w.Code)
	}
	if w = c.f.faz(req{metodo: "GET", caminho: "/avisos"}); w.Code != 200 {
		t.Errorf("com usuário = %d", w.Code)
	}
}

func TestPaginaTemBotaoDeAvisosSemPedirPermissao(t *testing.T) {
	c := vazia(t, false)
	c.coord("alfa", "wA:p1", "proj", 7, "merge")
	_, html := c.get("/")
	contem(t, html, `id="avisos-btn"`, `/estatico/avisos.js?v=`)
	// O botão nasce escondido: só o JS o mostra, e o pedido de permissão é do clique.
	if !strings.Contains(html, `id="avisos-btn" class="btn btn-sm cab-avisos" hidden`) {
		t.Errorf("o botão deveria nascer hidden")
	}
	js, err := os.ReadFile("estatico/avisos.js")
	if err != nil {
		t.Fatal(err)
	}
	// requestPermission só aparece dentro do handler do clique.
	src := string(js)
	i := strings.Index(src, "requestPermission")
	j := strings.Index(src, "btn.addEventListener('click'")
	if i < 0 || j < 0 || i < j {
		t.Errorf("requestPermission deve ficar dentro do handler de clique (i=%d, j=%d)", i, j)
	}
	if strings.Count(src, "requestPermission") != 1 {
		t.Errorf("requestPermission aparece %d vezes", strings.Count(src, "requestPermission"))
	}
}

// TestAvisosLogicaDoNavegador roda testdata/avisos_logica.mjs em Node: seleção do que é novo,
// deduplicação entre abas (Web Lock e registro compartilhado simulados), texto e href. Sem Node
// no PATH o teste é pulado.
func TestAvisosLogicaDoNavegador(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node fora do PATH")
	}
	out, err := exec.Command(node, "testdata/avisos_logica.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("avisos_logica.mjs: %v\n%s", err, out)
	}
	t.Logf("%s", strings.TrimSpace(string(out)))
}
