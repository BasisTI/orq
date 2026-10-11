package enriquecer

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
)

func TestNormalizarMR(t *testing.T) {
	gitlab := []struct {
		state, detalhe string
		draft, merged  bool
		want           string
	}{
		{"opened", "mergeable", false, false, "pronta"},
		{"opened", "mergeable", true, false, "draft"},
		{"opened", "draft_status", true, false, "draft"},
		{"opened", "conflict", false, false, "aberta"},
		{"opened", "unchecked", false, false, "aberta"},
		{"opened", "ci_still_running", false, false, "aberta"},
		{"merged", "not_open", false, true, "mesclada"},
		{"closed", "not_open", false, false, "fechada"},
		{"locked", "mergeable", false, true, "mesclada"},
	}
	for _, c := range gitlab {
		if got := NormalizarGitLab(c.state, c.draft, c.detalhe, c.merged); got != c.want {
			t.Errorf("gitlab %+v = %s, queria %s", c, got, c.want)
		}
	}
	github := []struct {
		state, mergeable string
		draft, merged    bool
		want             string
	}{
		{"open", "clean", false, false, "pronta"},
		{"open", "has_hooks", false, false, "pronta"},
		{"open", "clean", true, false, "draft"},
		{"open", "dirty", false, false, "aberta"},
		{"open", "blocked", false, false, "aberta"},
		{"open", "unstable", false, false, "aberta"},
		{"open", "unknown", false, false, "aberta"},
		{"closed", "unknown", false, true, "mesclada"},
		{"closed", "unknown", false, false, "fechada"},
	}
	for _, c := range github {
		if got := NormalizarGitHub(c.state, c.draft, c.merged, c.mergeable); got != c.want {
			t.Errorf("github %+v = %s, queria %s", c, got, c.want)
		}
	}
}

const (
	urlGL = "https://gitlab.basis.com.br/basis/iac/plataforma/-/merge_requests/41"
	urlGH = "https://github.com/BasisTI/orq/pull/1"
)

var argsGL = []string{"api", "--hostname", "gitlab.basis.com.br", "projects/basis%2Fiac%2Fplataforma/merge_requests/41"}
var argsGH = []string{"api", "repos/BasisTI/orq/pulls/1"}

func TestConsultarMRPelasForjas(t *testing.T) {
	c := novoCenario(t)
	c.responder("glab", `{"state":"opened","draft":false,"detailed_merge_status":"mergeable","merged_at":null,"web_url":"`+urlGL+`"}`, false, argsGL...)
	c.responder("gh", `{"state":"closed","draft":false,"merged":true,"mergeable_state":"unknown","html_url":"`+urlGH+`"}`, false, argsGH...)
	v, err := c.e.F.ConsultarMR(context.Background(), urlGL)
	if err != nil || v.Estado != "pronta" {
		t.Fatalf("gitlab = %+v, %v", v, err)
	}
	v, err = c.e.F.ConsultarMR(context.Background(), urlGH)
	if err != nil || v.Estado != "mesclada" {
		t.Fatalf("github = %+v, %v", v, err)
	}
	if got := c.chamadas("glab"); len(got) != 1 || strings.Join(got[0], " ") != strings.Join(argsGL, " ") {
		t.Fatalf("argv do glab = %q", got)
	}
}

func TestClassificarForja(t *testing.T) {
	casos := []struct{ stderr, want string }{
		{"HTTP 404: Not Found (https://api.github.com/repos/BasisTI/orq/pulls/401)", ErrNaoEncontrado},
		{"HTTP 401: Bad credentials (https://api.github.com/repos/BasisTI/orq/pulls/404)", ErrAutenticacao},
		{"glab: 401 Unauthorized", ErrAutenticacao},
		{"glab: 404 Not Found", ErrNaoEncontrado},
		{"To get started with GitHub CLI, please run:  gh auth login", ErrAutenticacao},
		{"HTTP 502: Bad Gateway (https://gitlab/merge_requests/401)", ErrFalhaComando},
	}
	for _, c := range casos {
		f := comoFalha(classificarForja(&Falha{Codigo: ErrFalhaComando, Erro: []byte(c.stderr)}))
		if f.Codigo != c.want {
			t.Errorf("%q = %s, queria %s", c.stderr, f.Codigo, c.want)
		}
	}
}

// O TTL segura a consulta; o erro fica no cache sem apagar o último valor bom.
func TestTTLEErroPreservaValorBom(t *testing.T) {
	c := novoCenario(t)
	co := c.coord("a", "w1:p1", "")
	c.onda(co, "1")
	c.mr(co, "1", urlGH)
	c.responder("gh", `{"state":"open","draft":false,"merged":false,"mergeable_state":"clean"}`, false, argsGH...)

	r := c.atualizar(Opcoes{Fontes: []string{"mr"}})
	if r.Fontes["mr"].Consultadas != 1 {
		t.Fatalf("1ª = %+v", r.Fontes["mr"])
	}
	c.rel.passar(30 * time.Second)
	if r := c.atualizar(Opcoes{Fontes: []string{"mr"}}); r.Fontes["mr"].EmDia != 1 || r.Fontes["mr"].Consultadas != 0 {
		t.Fatalf("dentro do TTL = %+v", r.Fontes["mr"])
	}
	if r := c.atualizar(Opcoes{Fontes: []string{"mr"}, Forcar: true}); r.Fontes["mr"].Consultadas != 1 {
		t.Fatalf("--forcar = %+v", r.Fontes["mr"])
	}
	if n := len(c.chamadas("gh")); n != 2 {
		t.Fatalf("gh chamado %d vezes, queria 2", n)
	}
	c.responder("gh", "gh: Could not resolve host: api.github.com", true, argsGH...)
	c.rel.passar(61 * time.Second)
	r = c.atualizar(Opcoes{Fontes: []string{"mr"}})
	if len(r.Fontes["mr"].Erros) != 1 || r.Fontes["mr"].Erros[0].Erro.Codigo != ErrFalhaComando {
		t.Fatalf("erro = %+v", r.Fontes["mr"])
	}
	m := c.snapshot().Coordenadores[0].Ondas[0].US[0].MRs[0]
	if m.Estado == nil || *m.Estado != "pronta" || m.Erro == nil || !strings.Contains(m.Erro.Mensagem, "Could not resolve host") {
		t.Fatalf("mr = %+v / %+v", m, m.Erro)
	}
}

// Erro de autenticação suspende a ferramenta: nada de login, só para de chamar.
func TestAutenticacaoSuspende(t *testing.T) {
	c := novoCenario(t)
	co := c.coord("a", "", "")
	c.onda(co, "1", "2")
	c.mr(co, "1", urlGH)
	c.mr(co, "2", "https://github.com/BasisTI/skills/pull/29")
	c.responder("gh", "HTTP 401: Bad credentials (https://api.github.com/repos/BasisTI/orq/pulls/1)", true, argsGH...)
	c.responder("taiga", `{"error":{"code":"session_expired","source":"auth","cause":"sessão expirada"}}`, true, "story", "get", "1", "--project", "orq", "--output", "json")

	r := c.atualizar(Opcoes{})
	if e := r.Fontes["mr"]; e.Consultadas != 1 || e.Suspensas != 1 || e.Erros[0].Erro.Codigo != ErrAutenticacao {
		t.Fatalf("mr = %+v", e)
	}
	if e := r.Fontes["taiga"]; e.Consultadas != 1 || e.Suspensas != 1 || e.Erros[0].Erro.Codigo != ErrAutenticacao {
		t.Fatalf("taiga = %+v", e)
	}
	if n := len(c.chamadas("gh")); n != 1 {
		t.Fatalf("gh chamado %d vezes depois do 401", n)
	}
	_ = c.chamadas("taiga")
	f := c.snapshot().Fontes
	if f["mr"].Erro == nil || !strings.HasPrefix(f["mr"].Erro.Mensagem, "gh suspenso") || f["taiga"].Erro == nil {
		t.Fatalf("fontes = %+v %+v", f["mr"].Erro, f["taiga"].Erro)
	}
	c.rel.passar(10 * time.Minute)
	if r := c.atualizar(Opcoes{}); r.Fontes["mr"].Suspensas != 2 || len(c.chamadas("gh"))+len(c.chamadas("taiga")) != 0 {
		t.Fatalf("suspensa ainda chamou: %+v", r.Fontes["mr"])
	}
	// --forcar tenta de novo; sucesso tira a suspensão.
	c.responder("gh", `{"state":"open","merged":false,"mergeable_state":"blocked"}`, false, argsGH...)
	c.atualizar(Opcoes{Fontes: []string{"mr"}, Forcar: true})
	if sus, _ := c.st.Suspensoes(context.Background()); sus["gh"].Codigo != "" {
		t.Fatalf("gh continua suspenso: %+v", sus)
	}
	// Passada a pausa, a ferramenta é tentada de novo sozinha; o 401 de novo a suspende já na
	// primeira chave.
	c.rel.passar(31 * time.Minute)
	_ = c.chamadas("taiga")
	if r := c.atualizar(Opcoes{Fontes: []string{"taiga"}}); r.Fontes["taiga"].Consultadas != 1 || r.Fontes["taiga"].Suspensas != 1 {
		t.Fatalf("depois da pausa = %+v", r.Fontes["taiga"])
	}
}

func TestTaigaEBinarioAusente(t *testing.T) {
	c := novoCenario(t)
	co := c.coord("a", "", "")
	c.onda(co, "4", "99999")
	c.responder("taiga", `{"subject":"Enriquecedores","url":"https://agile.basis.com.br/project/orq/us/4","status_extra_info":{"name":"In progress"}}`, false, "story", "get", "4", "--project", "orq", "--output", "json")
	c.responder("taiga", `{"error":{"code":"not_found","cause":"No UserStory"}}`, true, "story", "get", "99999", "--project", "orq", "--output", "json")
	c.env["ORQ_HERDR_BIN"] = "/nao/existe/herdr"
	r := c.atualizar(Opcoes{Fontes: []string{"taiga", "herdr"}})
	if e := r.Fontes["taiga"].Erros; len(e) != 1 || e[0].Chave != "orq/99999" || e[0].Erro.Codigo != ErrNaoEncontrado {
		t.Fatalf("taiga = %+v", r.Fontes["taiga"])
	}
	if e := r.Fontes["herdr"].Erros; len(e) != 1 || e[0].Erro.Codigo != ErrBinarioAusente {
		t.Fatalf("herdr = %+v", r.Fontes["herdr"])
	}
	s := c.snapshot()
	u := s.Coordenadores[0].Ondas[0].US
	if *u[0].Taiga.Titulo != "Enriquecedores" || *u[0].Taiga.Status != "In progress" || u[1].Taiga.Erro.Codigo != ErrNaoEncontrado {
		t.Fatalf("taiga no snapshot = %+v / %+v", u[0].Taiga, u[1].Taiga)
	}
	if s.Fontes["herdr"].Erro == nil || s.Fontes["herdr"].Erro.Codigo != ErrBinarioAusente {
		t.Fatalf("fonte herdr = %+v", s.Fontes["herdr"])
	}
	// Variável relativa também é rejeitada: o PATH nunca entra.
	c.env["ORQ_HERDR_BIN"] = "herdr"
	if _, err := c.e.F.Caminho("herdr"); err == nil {
		t.Fatal("caminho relativo devia falhar")
	}
	delete(c.env, "ORQ_HERDR_BIN")
	c.env["HERDR_BIN_PATH"] = c.dir + "/bin/herdr"
	if p, err := c.e.F.Caminho("herdr"); err != nil || p != c.dir+"/bin/herdr" {
		t.Fatalf("HERDR_BIN_PATH = %q, %v", p, err)
	}
}

func TestHerdrPaneAusente(t *testing.T) {
	c := novoCenario(t)
	co := c.coord("a", "w1:p1", "")
	c.onda(co, "1", "2")
	c.status(co, "1", "exec", "w1:p2")
	c.status(co, "2", "exec", "w1:p3")
	c.sessao([]map[string]any{
		{"pane_id": "w1:p1", "workspace_id": "w1", "agent_status": "unknown"},
		{"pane_id": "w1:p2", "workspace_id": "w1", "agent": "claude", "agent_status": "working", "name": "tg1-exec"},
	}, []map[string]any{{"workspace_id": "w1"}})
	if r := c.atualizar(Opcoes{Fontes: []string{"herdr"}}); r.Fontes["herdr"].Consultadas != 1 || len(r.Fontes["herdr"].Erros) != 0 {
		t.Fatalf("herdr = %+v", r.Fontes["herdr"])
	}
	s := c.snapshot()
	u := s.Coordenadores[0].Ondas[0].US
	if *s.Coordenadores[0].Herdr.Status != "unknown" || s.Coordenadores[0].Herdr.Agente != nil {
		t.Fatalf("coord = %+v", s.Coordenadores[0].Herdr)
	}
	if *u[0].Herdr.Status != "working" || *u[0].Herdr.Nome != "tg1-exec" || *u[1].Herdr.Status != "ausente" {
		t.Fatalf("us = %+v / %+v", u[0].Herdr, u[1].Herdr)
	}
}

func TestResumoEJuntarRefs(t *testing.T) {
	st := func(s string) *string { return &s }
	c := central.CoordSnapshot{Ondas: []central.Onda{{US: []central.US{
		{Status: "exec", Herdr: &central.Herdr{Status: st("working")}},
		{Status: "exec"},
		{Status: "rev", Pendencias: []central.Pendencia{{Aberta: true}, {Aberta: false}}},
		{Status: "wait"},
	}}}}
	if got := Resumo(c); got != "1 Wait·2 Exec·1 Rev·1 pend" {
		t.Fatalf("resumo = %q", got)
	}
	c.Ondas[0].US[1].Herdr = &central.Herdr{Status: st("blocked")}
	if got := Resumo(c); got != "⚠ 1 Wait·2 Exec·1 Rev·1 pend" {
		t.Fatalf("resumo com blocked = %q", got)
	}
	if got := Resumo(central.CoordSnapshot{}); got != "" {
		t.Fatalf("sem onda = %q", got)
	}
	var muitas []central.US
	for i := 0; i < 2000; i++ {
		muitas = append(muitas, central.US{Status: []string{"wait", "plan", "exec", "rev", "done"}[i%5], Herdr: &central.Herdr{Status: st("blocked")},
			Pendencias: []central.Pendencia{{Aberta: true}}})
	}
	if got := Resumo(central.CoordSnapshot{Ondas: []central.Onda{{US: muitas}}}); utf8.RuneCountInString(got) > LimiteToken {
		t.Fatalf("resumo com %d runas: %q", utf8.RuneCountInString(got), got)
	}
	var rs []string
	for i := 100; i < 130; i++ {
		rs = append(rs, "!"+strconv.Itoa(i*7))
	}
	j := JuntarRefs(append(rs, rs...))
	if utf8.RuneCountInString(j) > LimiteToken || !strings.Contains(j, " +") {
		t.Fatalf("refs = %q", j)
	}
	if got := JuntarRefs([]string{"!32", "skills#29", "!32"}); got != "!32 skills#29" {
		t.Fatalf("refs = %q", got)
	}
	if got := JuntarRefs(nil); got != "" {
		t.Fatalf("sem refs = %q", got)
	}
}

func TestTokensPublicaConfereELimpa(t *testing.T) {
	c := novoCenario(t)
	ctx := context.Background()
	co := c.coord("a", "w1:p1", "")
	c.coord("fantasma", "w9:p9", "w9") // pane e workspace que não existem
	c.onda(co, "1", "2")
	c.status(co, "1", "exec", "w1:p2")
	c.mr(co, "1", urlGH)
	c.mr(co, "2", urlGL)
	c.responder("gh", `{"state":"open","merged":false,"mergeable_state":"clean"}`, false, argsGH...)
	c.responder("glab", `{"state":"opened","detailed_merge_status":"mergeable"}`, false, argsGL...)
	painel := func(tokPane, tokWS map[string]string) {
		c.sessao([]map[string]any{
			{"pane_id": "w1:p1", "workspace_id": "w1", "agent": "claude", "agent_status": "working", "tokens": tokPane},
			{"pane_id": "w1:p2", "workspace_id": "w1", "agent": "claude", "agent_status": "blocked"},
		}, []map[string]any{{"workspace_id": "w1", "tokens": tokWS}})
	}
	painel(nil, nil)
	c.atualizar(Opcoes{})
	_ = c.chamadas("herdr")
	r, err := c.e.PublicarTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := c.chamadas("herdr")
	want := []string{
		"pane report-metadata w1:p1 --source orq --token orq=⚠ 1 Wait·1 Exec",
		"workspace report-metadata w1 --source orq --token mrs_prontas=orq#1 !41",
	}
	if len(got) != 2 || strings.Join(got[0], " ") != want[0] || strings.Join(got[1], " ") != want[1] || len(r.Publicados) != 2 {
		t.Fatalf("argv = %q, resultado %+v", got, r)
	}
	// O Herdr já mostra os tokens: nada a republicar.
	painel(map[string]string{"orq": "⚠ 1 Wait·1 Exec"}, map[string]string{"mrs_prontas": "orq#1 !41"})
	c.atualizar(Opcoes{Fontes: []string{"herdr"}, Forcar: true})
	_ = c.chamadas("herdr")
	if r, _ := c.e.PublicarTokens(ctx); len(r.Publicados)+len(r.Limpos) != 0 || len(c.chamadas("herdr")) != 0 {
		t.Fatalf("republicou sem mudança: %+v", r)
	}
	// O servidor do Herdr reiniciou e os tokens sumiram: republica.
	painel(nil, nil)
	c.atualizar(Opcoes{Fontes: []string{"herdr"}, Forcar: true})
	_ = c.chamadas("herdr")
	if r, _ := c.e.PublicarTokens(ctx); len(r.Publicados) != 2 {
		t.Fatalf("não republicou: %+v", r)
	}
	_ = c.chamadas("herdr")
	// As MRs saem de pronta: mrs_prontas é limpo.
	painel(map[string]string{"orq": "⚠ 1 Wait·1 Exec"}, map[string]string{"mrs_prontas": "orq#1 !41"})
	c.responder("gh", `{"state":"closed","merged":true}`, false, argsGH...)
	c.responder("glab", `{"state":"opened","draft":true,"detailed_merge_status":"draft_status"}`, false, argsGL...)
	c.atualizar(Opcoes{Forcar: true})
	_ = c.chamadas("herdr")
	r, _ = c.e.PublicarTokens(ctx)
	got = c.chamadas("herdr")
	if len(got) != 1 || strings.Join(got[0], " ") != "workspace report-metadata w1 --source orq --clear-token mrs_prontas" || len(r.Limpos) != 1 {
		t.Fatalf("limpeza = %q, %+v", got, r)
	}
	// O pane do coordenador some: o registro é esquecido sem chamar o Herdr.
	c.sessao([]map[string]any{{"pane_id": "w1:p2", "workspace_id": "w1", "agent_status": "idle"}}, []map[string]any{{"workspace_id": "w1"}})
	c.atualizar(Opcoes{Fontes: []string{"herdr"}, Forcar: true})
	_ = c.chamadas("herdr")
	if r, _ := c.e.PublicarTokens(ctx); len(r.Publicados)+len(r.Limpos) != 0 || len(c.chamadas("herdr")) != 0 {
		t.Fatalf("pane sumido: %+v", r)
	}
	if ts, _ := c.st.TokensPublicados(ctx); len(ts) != 0 {
		t.Fatalf("tokens publicados = %+v", ts)
	}
	// Herdr com erro: não confere nada.
	c.responder("herdr", "server not running", true, "api", "snapshot")
	c.atualizar(Opcoes{Fontes: []string{"herdr"}, Forcar: true})
	if r, _ := c.e.PublicarTokens(ctx); r.Pulado == nil {
		t.Fatal("devia pular com o Herdr fora")
	}
}

func TestDaemonUnico(t *testing.T) {
	dir := t.TempDir()
	soltar, err := TravarDaemon(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = TravarDaemon(dir)
	if e := output.AsError(err); err == nil || e.Code != "daemon_ativo" || e.Exit != output.ExitConflict || !strings.Contains(e.Cause, "pid ") {
		t.Fatalf("segundo daemon = %v", err)
	}
	soltar()
	soltar2, err := TravarDaemon(dir)
	if err != nil {
		t.Fatalf("depois de soltar: %v", err)
	}
	soltar2()
}
