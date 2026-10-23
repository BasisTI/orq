package web

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// cena é uma Central sintética: nada aqui vem de uma Central real.
type cena struct {
	t   *testing.T
	ctx context.Context
	st  *central.Store
	f   *fixture
}

func vazia(t *testing.T, unix bool) *cena {
	t.Helper()
	ctx := context.Background()
	st, err := central.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f := &fixture{t: t, st: st}
	f.srv = &Servidor{St: st, Unix: unix}
	if !unix {
		f.srv.Porta = 8080
	}
	return &cena{t: t, ctx: ctx, st: st, f: f}
}

func (c *cena) ok(err error) {
	c.t.Helper()
	if err != nil {
		c.t.Fatal(err)
	}
}

// coord registra o coordenador com uma onda de uma US e o devolve.
func (c *cena) coord(nome, pane, projeto string, ref int, ate string) (central.Coordenador, central.Onda) {
	c.t.Helper()
	co, err := c.st.RegistrarCoord(c.ctx, central.Registro{Nome: nome, Pane: pane, Projeto: projeto})
	c.ok(err)
	o, err := c.st.CriarOnda(c.ctx, co, central.NovaOnda{Titulo: "Onda de " + nome, Ate: ate, US: []refs.US{{Projeto: projeto, Ref: ref}}})
	c.ok(err)
	return co, o
}

func (c *cena) herdr(panes map[string]central.PaneHerdr) {
	c.t.Helper()
	c.ok(c.st.GravarCache(c.ctx, central.FonteHerdr, "", central.SessaoHerdr{Panes: panes, Workspaces: map[string]central.WorkspaceHerdr{}}, nil))
}

func (c *cena) taiga(u refs.US, titulo, status string) {
	c.t.Helper()
	c.ok(c.st.GravarCache(c.ctx, central.FonteTaiga, u.Chave(), central.ValorTaiga{Titulo: titulo, Status: status, URL: "https://taiga.exemplo/project/" + u.Projeto + "/us/" + fmt.Sprint(u.Ref)}, nil))
}

func (c *cena) get(caminho string) (int, string) {
	c.t.Helper()
	w := c.f.faz(req{metodo: "GET", caminho: caminho})
	return w.Code, corpo(w)
}

func contem(t *testing.T, html string, quer ...string) {
	t.Helper()
	for _, q := range quer {
		if !strings.Contains(html, q) {
			t.Errorf("a página não tem %q:\n%s", q, html)
		}
	}
}

func naoContem(t *testing.T, html string, nao ...string) {
	t.Helper()
	for _, q := range nao {
		if strings.Contains(html, q) {
			t.Errorf("a página tem %q", q)
		}
	}
}

func TestCentralSemCoordenadores(t *testing.T) {
	c := vazia(t, false)
	code, html := c.get("/central")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	contem(t, html, "Nenhuma onda aberta", "banner-cartao", `id="vivo"`, `hx-swap="none"`, "Idade dos dados", "sem dado", "<b>0</b> ondas abertas")
	naoContem(t, html, "Dados vencidos", `title="o cache passou do prazo da fonte"`, `http-equiv="refresh"`)
}

func TestCentralUmCoordenador(t *testing.T) {
	c := vazia(t, false)
	co, o := c.coord("alfa", "wA:p1", "proj", 7, "merge")
	u := refs.US{Projeto: "proj", Ref: 7}
	_, _, err := c.st.AdicionarMR(c.ctx, &co, u, "https://github.com/Exemplo/repo/pull/20")
	c.ok(err)
	c.ok(c.st.GravarMR(c.ctx, "https://github.com/Exemplo/repo/pull/20", central.ValorMR{Estado: "pronta"}, nil))
	c.taiga(u, "Exportar a folha", "In progress")
	_, _, err = c.st.AtualizarStatus(c.ctx, &co, u, central.MudancaStatus{Status: "exec", Pane: ptr("wA:p9")})
	c.ok(err)
	c.herdr(map[string]central.PaneHerdr{"wA:p9": {Status: "working", Agente: "claude", Nome: "nome-secreto", WorkspaceID: "wA"}})
	for i, tipo := range []string{"duvida", "autorizacao"} {
		_, err := c.st.RegistrarPendencia(c.ctx, central.NovaPendencia{US: u, Tipo: tipo, Titulo: fmt.Sprintf("pergunta %d", i+1), Autor: "w", Detalhe: "d", Slug: fmt.Sprintf("s%d", i), Hash: fmt.Sprintf("h%d", i)})
		c.ok(err)
	}
	_, err = c.st.ResponderPendencia(c.ctx, nil, refs.Pend{US: u, Numero: 2}, central.Resposta{Opcao: new(1), Texto: "sim", Por: "ana"})
	c.ok(err)

	code, html := c.get("/central")
	if code != 200 {
		t.Fatalf("status %d: %s", code, html)
	}
	contem(t, html,
		"alfa", "Onda de alfa", fmt.Sprintf(`href="/onda/%d"`, o.ID), "até merge · livre",
		"TG-7", "Exportar a folha", "In progress", ">execução<",
		"repo#20", ">espera merge<", "1 MR espera merge", "trabalhando", "estado do agente: working",
		`href="/p/proj/7/1"`, `href="/p/proj/7/2"`, "respondida",
		// A respondida sai de "esperam você": sobra uma, na onda, na US, na situação e na aba.
		"1 espera você", "1 pendência espera você",
	)
	naoContem(t, html, "wA:p", "nome-secreto", `"versao"`, "sem estado", "2 esperam você")
}

func TestCentralTresCoordenadores(t *testing.T) {
	c := vazia(t, false)
	for i, n := range []string{"um", "dois", "tres"} {
		c.coord(n, fmt.Sprintf("wA:p%d", i+1), "proj", 10+i, "")
	}
	_, html := c.get("/central")
	if got := strings.Count(html, `<details class="onda"`); got != 3 {
		t.Errorf("%d ondas na página, quer 3", got)
	}
	// Sem nada que peça atenção, a ordem é a do id (a do snapshot).
	i1, i2, i3 := strings.Index(html, "Onda de um"), strings.Index(html, "Onda de dois"), strings.Index(html, "Onda de tres")
	if i1 < 0 || i1 > i2 || i2 > i3 {
		t.Error("ondas fora da ordem do snapshot")
	}
	contem(t, html, "<b>3</b> ondas abertas")
}

func TestCentralOndaConcluida(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "plano")
	_, _, err := c.st.AtualizarStatus(c.ctx, &co, refs.US{Projeto: "proj", Ref: 7}, central.MudancaStatus{Status: "done"})
	c.ok(err)
	_, html := c.get("/central")
	contem(t, html, "1/1", "1 onda concluída")
	if n := strings.Count(html, "concluída</span>"); n != 2 {
		t.Errorf("selo concluída ×%d, quer o da onda e o da legenda", n)
	}

	// Uma onda com US pendente não ganha o selo.
	c2 := vazia(t, false)
	c2.coord("beta", "wA:p2", "proj", 8, "plano")
	_, html = c2.get("/central")
	naoContem(t, html, "1 onda concluída")
	// A legenda da coluna lateral tem um "concluída"; a onda concluída teria dois.
	if n := strings.Count(html, "concluída</span>"); n != 1 {
		t.Errorf("selo concluída ×%d, quer só o da legenda", n)
	}
}

func TestCentralMRSemEstado(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	_, _, err := c.st.AdicionarMR(c.ctx, &co, refs.US{Projeto: "proj", Ref: 7}, "https://github.com/Exemplo/repo/pull/21")
	c.ok(err)
	code, html := c.get("/central")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	contem(t, html, "repo#21", "selo-tracejado", ">sem estado<", "sem dado")
}

func TestCentralCacheVencido(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	_, _, err := c.st.AdicionarMR(c.ctx, &co, u, "https://github.com/Exemplo/repo/pull/20")
	c.ok(err)
	_, _, err = c.st.AtualizarStatus(c.ctx, &co, u, central.MudancaStatus{Status: "exec", Pane: ptr("wA:p9")})
	c.ok(err)
	c.ok(c.st.GravarMR(c.ctx, "https://github.com/Exemplo/repo/pull/20", central.ValorMR{Estado: "aberta"}, nil))
	c.taiga(u, "Titulo", "New")
	c.herdr(map[string]central.PaneHerdr{"wA:p9": {Status: "idle", WorkspaceID: "wA"}})

	_, html := c.get("/central")
	naoContem(t, html, "Dados vencidos", `title="o cache passou do prazo da fonte"`)
	contem(t, html, "agora", "ocioso")

	// Uma hora depois, sem o daemon, tudo passou do prazo e aparece como vencido.
	depois := time.Now().Add(time.Hour)
	c.f.srv.Agora = func() time.Time { return depois }
	_, html = c.get("/central")
	contem(t, html, "Dados vencidos: herdr (1 h), mr (1 h), taiga (1 h)", "há 1 h")
	// 3 fontes na coluna lateral, o agente, o Taiga da US e a MR (a MR duas vezes: estado e valores).
	if n := strings.Count(html, `title="o cache passou do prazo da fonte"`); n < 6 {
		t.Errorf("%d marcas de vencido, quer ao menos 6", n)
	}
}

func TestCentralVencidoRecenteDaSegundos(t *testing.T) {
	c := vazia(t, false)
	c.herdr(map[string]central.PaneHerdr{})
	depois := time.Now().Add(30 * time.Second)
	c.f.srv.Agora = func() time.Time { return depois }
	_, html := c.get("/central")
	if !regexp.MustCompile(`há \d+ s`).MatchString(html) {
		t.Error("o vencido de menos de um minuto deveria dar a idade em segundos")
	}
	contem(t, html, "Dados vencidos: herdr (")
	naoContem(t, html, "Dados vencidos: herdr (agora")
}

func TestCentralErroDeFonteNaoApagaOValor(t *testing.T) {
	c := vazia(t, false)
	c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	c.taiga(u, "Titulo", "New")
	c.ok(c.st.GravarCache(c.ctx, central.FonteTaiga, u.Chave(), nil, &central.Erro{Codigo: "autenticacao", Mensagem: "token <b>x</b>", Em: time.Now().UTC().Format(time.RFC3339)}))
	_, html := c.get("/central")
	contem(t, html, "Titulo", "New", "falhou: autenticacao")
	naoContem(t, html, "token <b>")
}

func TestCentralHerdrSoMostraEstado(t *testing.T) {
	c := vazia(t, false)
	co, _ := c.coord("alfa", "wA:p1", "proj", 7, "")
	_, _, err := c.st.AtualizarStatus(c.ctx, &co, refs.US{Projeto: "proj", Ref: 7}, central.MudancaStatus{Status: "exec", Pane: ptr("wA:p9")})
	c.ok(err)
	c.herdr(map[string]central.PaneHerdr{
		"wA:p1": {Status: "done", Nome: "coord-nome", WorkspaceID: "wA-ws"},
		"wA:p9": {Status: "blocked", Agente: "claude", Nome: "worker-nome", WorkspaceID: "wA-ws"},
	})
	_, html := c.get("/central")
	contem(t, html, "bloqueado", "estado do agente: blocked")
	naoContem(t, html, "wA:p", "coord-nome", "worker-nome", "wA-ws")
}

func ptr(s string) *string { return &s }

// linhaMR devolve o primeiro <li class="mr"> da página.
func linhaMR(t *testing.T, html string) string {
	t.Helper()
	_, row, ok := strings.Cut(html, `<li class="mr">`)
	if !ok {
		t.Fatal("MR ausente")
	}
	row, _, _ = strings.Cut(row, "</li>")
	return row
}

func TestOndaDiarioEscapadoEPaginado(t *testing.T) {
	c := vazia(t, false)
	co, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	u := refs.US{Projeto: "proj", Ref: 7}
	var es []central.EventoExterno
	for i := 0; i < 30; i++ {
		es = append(es, central.EventoExterno{Tipo: "limpar.removido", Detalhe: map[string]any{"n": i}})
	}
	es = append(es, central.EventoExterno{Tipo: "limpar.falhou", Detalhe: map[string]any{
		"motivo": `<script>alert("x")</script><img src=x onerror=alert(1)> wA:p9 (nome-x)`, "pane": "wA:p9", "workspace_id": "wA-ws", "tipo": "worktree", "alvo": "/tmp/visível",
	}})
	c.ok(c.st.RegistrarExternos(c.ctx, &co, u, es))

	href := fmt.Sprintf("/onda/%d", o.ID)
	code, html := c.get(href)
	if code != 200 {
		t.Fatalf("status %d: %s", code, html)
	}
	html = doMain(html)
	contem(t, html, "Onda de alfa", "limpar.falhou", "&lt;script&gt;", "visível", "[pane]", "página 1 de 2", href+"?pagina=2", "mais antigas")
	naoContem(t, html, "<script>alert", "<img src", "wA:p9", "wA-ws", "nome-x", "mais recentes")
	// O mais recente vem primeiro.
	if strings.Index(html, "limpar.falhou") > strings.Index(html, "limpar.removido") {
		t.Error("o evento mais recente não está no topo")
	}

	_, html = c.get(href + "?pagina=2")
	contem(t, html, "página 2 de 2", "mais recentes")
	naoContem(t, html, "mais antigas", "limpar.falhou")

	// Página fora do intervalo é encostada na última; lixo vira a primeira.
	if _, h := c.get(href + "?pagina=99"); !strings.Contains(h, "página 2 de 2") {
		t.Error("pagina=99 não foi limitada")
	}
	if _, h := c.get(href + "?pagina=abc"); !strings.Contains(h, "página 1 de 2") {
		t.Error("pagina=abc não caiu na primeira")
	}
}

func TestOndaInexistenteEInvalida(t *testing.T) {
	c := vazia(t, false)
	for _, p := range []string{"/onda/999", "/onda/abc", "/onda/0", "/onda/-1", "/onda/01"} {
		if code, _ := c.get(p); code != 404 {
			t.Errorf("GET %s = %d, quer 404", p, code)
		}
	}
}

func TestOndaEncerradaAindaTemDiario(t *testing.T) {
	c := vazia(t, false)
	co, o := c.coord("alfa", "wA:p1", "proj", 7, "plano")
	_, _, err := c.st.AtualizarStatus(c.ctx, &co, refs.US{Projeto: "proj", Ref: 7}, central.MudancaStatus{Status: "done"})
	c.ok(err)
	_, err = c.st.EncerrarOnda(c.ctx, "alfa", o.ID)
	c.ok(err)
	_, html := c.get("/central")
	naoContem(t, html, "Onda de alfa") // /central traz só as abertas, como o snapshot
	code, html := c.get(fmt.Sprintf("/onda/%d", o.ID))
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	contem(t, html, "encerrada", "onda.encerrar")
}

func TestCentralEOndaSoLeitura(t *testing.T) {
	c := vazia(t, false)
	_, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	for _, p := range []string{"/central", fmt.Sprintf("/onda/%d", o.ID)} {
		w := c.f.faz(req{metodo: "POST", caminho: p, corpo: "x=1"})
		if w.Code != 405 {
			t.Errorf("POST %s = %d, quer 405", p, w.Code)
		}
	}
}

func TestCentralMesmosCabecalhosEAutenticacao(t *testing.T) {
	c := vazia(t, true)
	_, o := c.coord("alfa", "wA:p1", "proj", 7, "")
	for _, p := range []string{"/central", fmt.Sprintf("/onda/%d", o.ID), "/"} {
		w := c.f.faz(req{metodo: "GET", caminho: p})
		if w.Code != 200 {
			t.Fatalf("GET %s = %d", p, w.Code)
		}
		for k, v := range map[string]string{"Content-Security-Policy": csp, "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "Cache-Control": "no-store"} {
			if got := w.Header().Get(k); got != v {
				t.Errorf("%s %s = %q", p, k, got)
			}
		}
		if h := corpo(w); temInline(h) {
			t.Errorf("%s tem script ou estilo inline", p)
		}
		sem := c.f.faz(req{metodo: "GET", caminho: p, cab: map[string]string{"X-Orq-User": ""}})
		if sem.Code != 401 {
			t.Errorf("GET %s sem usuário = %d, quer 401", p, sem.Code)
		}
	}
}

// A tela vazia é a ausência global de ondas abertas (spec §10), mesmo com coordenador registrado.
func TestCentralVaziaComCoordenadorRegistrado(t *testing.T) {
	c := vazia(t, false)
	_, err := c.st.RegistrarCoord(c.ctx, central.Registro{Nome: "alfa", Pane: "wA:p1", Projeto: "proj"})
	c.ok(err)
	code, html := c.get("/central")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	contem(t, html, `class="banner-cartao"`, "orq-banner-light.png", "Nenhuma onda aberta", "Coordenadores registrados, sem onda aberta: <strong>alfa</strong>", `id="vivo"`)
	naoContem(t, html, "Nenhum coordenador registrado")
	// Com uma onda, o banner some.
	c.coord("beta", "wA:p2", "proj", 7, "")
	_, html = c.get("/central")
	naoContem(t, html, "banner-cartao")
}
