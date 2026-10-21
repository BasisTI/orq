package cli

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/herdrfalso/falsotest"
	"github.com/BasisTI/orq/internal/pendencias"
)

const arquivoDecisao = `---
tipo: decisao
titulo: Qual biblioteca de YAML?
autor: tg-15_executor
opcoes:
  - id: 1
    titulo: yaml.v3
    descricao: estável
  - id: 2
    titulo: go.yaml.in/yaml/v3
    descricao: fork mantido
    recomendada: true
---
## Contexto
O frontmatter precisa de YAML.
## Pergunta
Qual biblioteca?
## Recomendação
A segunda.
## O que fica bloqueado
A importação.
## Evidências
- /tmp/x
`

// pendEnv prepara um coordenador com a US 15 numa onda e a pasta da rodada do worker.
func pendEnv(t *testing.T) (env map[string]string, pasta string) {
	t.Helper()
	env = ambiente(t)
	env["HERDR_PANE_ID"] = "wA:p1"
	ok(t, env, "coord", "registrar", "coord", "--projeto", "orq")
	ok(t, env, "onda", "criar", "o", "--us", "15,16")
	pasta = filepath.Join(env["ORQ_TRABALHO_DIR"], "orq", "TG-15", "implementacao", "r1", "pendencias")
	if err := os.MkdirAll(pasta, 0o755); err != nil {
		t.Fatal(err)
	}
	return env, pasta
}

func TestPendImportarVerResponderResolver(t *testing.T) {
	env, pasta := pendEnv(t)
	arq := filepath.Join(pasta, "decisao-yaml.md")
	escrever(t, arq, arquivoDecisao)
	env["ORQ_WEB_URL"] = "https://pend.exemplo"

	var r pendencias.Resultado
	if err := json.Unmarshal([]byte(ok(t, env, "pend", "importar", arq)), &r); err != nil {
		t.Fatal(err)
	}
	if r.Estado != "registrada" || r.ID != "TG-15#P1" || r.Chave != "orq/15#P1" || r.URL != "https://pend.exemplo/p/orq/15/1" {
		t.Fatalf("importar = %+v", r)
	}
	if b, err := os.ReadFile(filepath.Join(pasta, "decisao-yaml.registrada.json")); err != nil || !strings.Contains(string(b), `"chave": "orq/15#P1"`) {
		t.Fatalf("registrada.json = %q, %v", b, err)
	}
	if texto := ok(t, env, "--output", "text", "pend", "importar", arq); !strings.Contains(texto, "TG-15#P1  ja_registrada  https://pend.exemplo/p/orq/15/1") {
		t.Fatalf("texto = %q", texto)
	}

	// pend ver: detalhe completo, opções e origem.
	ver := ok(t, env, "--output", "text", "pend", "ver", "orq/15#P1")
	for _, quer := range []string{"Qual biblioteca de YAML?", "tg-15_executor", "1. yaml.v3", "2. go.yaml.in/yaml/v3  [recomendada]", "--- detalhe ---", "## Evidências", "decisao-yaml.md", "https://pend.exemplo/p/orq/15/1", "aberta"} {
		if !strings.Contains(ver, quer) {
			t.Errorf("pend ver sem %q:\n%s", quer, ver)
		}
	}
	var pv PendVer
	if err := json.Unmarshal([]byte(ok(t, env, "pend", "ver", "TG-15#P1")), &pv); err != nil || pv.Detalhe == nil || !strings.Contains(*pv.Detalhe, "## Contexto") || len(pv.Historico) != 0 || pv.URL == "" {
		t.Fatalf("ver json = %+v, %v", pv, err)
	}

	// responder pelo chat: respondida_por = chat; sem opção numa decisão é recusado.
	if r := rodar(t, env, "pend", "responder", "TG-15#P1", "só texto"); r.code != 2 || !strings.Contains(r.err, `"uso"`) {
		t.Fatalf("responder sem opção: %d %s", r.code, r.err)
	}
	if r := rodar(t, env, "pend", "responder", "TG-15#P1", "--opcao", "7"); r.code != 2 {
		t.Fatalf("opção inexistente: %d %s", r.code, r.err)
	}
	var p central.Pendencia
	if err := json.Unmarshal([]byte(ok(t, env, "pend", "responder", "TG-15#P1", "--opcao", "2", "vai com o fork")), &p); err != nil {
		t.Fatal(err)
	}
	if p.Estado != "respondida" || *p.RespondidaPor != "chat" || *p.RespostaOpcao != 2 || *p.Resposta != "vai com o fork" || p.AvisadaEm == nil {
		t.Fatalf("responder = %+v", p)
	}
	// list --respondidas e --abertas (a respondida ainda é aberta).
	var resp, abertas []central.Pendencia
	_ = json.Unmarshal([]byte(ok(t, env, "pend", "list", "--respondidas")), &resp)
	_ = json.Unmarshal([]byte(ok(t, env, "pend", "list", "--abertas")), &abertas)
	if len(resp) != 1 || len(abertas) != 1 {
		t.Fatalf("respondidas %d, abertas %d", len(resp), len(abertas))
	}
	if texto := ok(t, env, "--output", "text", "pend", "list", "--respondidas"); !strings.Contains(texto, "respondida: opção 2 (go.yaml.in/yaml/v3)") {
		t.Fatalf("list texto = %q", texto)
	}
	// Trocar a resposta enquanto não resolvida.
	ok(t, env, "pend", "responder", "TG-15#P1", "--opcao", "1")
	// resolver sem --resposta usa a registrada.
	var r2 central.Pendencia
	if err := json.Unmarshal([]byte(ok(t, env, "pend", "resolver", "TG-15#P1")), &r2); err != nil || r2.Aberta || *r2.Resposta != "opção 1 (yaml.v3)" {
		t.Fatalf("resolver = %+v, %v", r2, err)
	}
	var vh PendVer
	_ = json.Unmarshal([]byte(ok(t, env, "pend", "ver", "orq/15#P1")), &vh)
	if len(vh.Historico) != 2 || vh.Estado != "resolvida" {
		t.Fatalf("histórico = %+v", vh.Historico)
	}
	if r := rodar(t, env, "pend", "responder", "TG-15#P1", "--opcao", "1"); r.code != 4 || !strings.Contains(r.err, "pendencia_resolvida") {
		t.Fatalf("responder resolvida: %d %s", r.code, r.err)
	}
}

func TestPendImportarInvalidoSaiComErroEGravaErroTxt(t *testing.T) {
	env, pasta := pendEnv(t)
	arq := filepath.Join(pasta, "ruim.md")
	escrever(t, arq, strings.Replace(arquivoDecisao, "## Evidências", "## Provas", 1))
	r := rodar(t, env, "pend", "importar", arq)
	if r.code != 2 || !strings.Contains(r.err, "pendencia_invalida") || !strings.Contains(r.err, "Evidências") {
		t.Fatalf("importar inválido: %d %s", r.code, r.err)
	}
	if b, err := os.ReadFile(filepath.Join(pasta, "ruim.erro.txt")); err != nil || !strings.Contains(string(b), "`## Evidências` ausente") {
		t.Fatalf("erro.txt = %q, %v", b, err)
	}
	// Caminho relativo vira absoluto; fora da raiz é recusado.
	if r := rodar(t, env, "pend", "importar", filepath.Join(t.TempDir(), "orq", "TG-15", "pendencias", "x.md")); r.code != 2 {
		t.Fatalf("fora da raiz: %d %s", r.code, r.err)
	}
}

func TestPendVerAntigaSemDetalhe(t *testing.T) {
	env, _ := pendEnv(t)
	ok(t, env, "pend", "add", "TG-15", "--tipo", "duvida", "resumo antigo")
	ver := ok(t, env, "--output", "text", "pend", "ver", "TG-15#P1")
	if !strings.Contains(ver, "sem detalhe") || !strings.Contains(ver, "resumo antigo") {
		t.Fatalf("ver = %q", ver)
	}
	if list := ok(t, env, "--output", "text", "pend", "list"); !strings.Contains(list, "[sem detalhe]") {
		t.Fatalf("list = %q", list)
	}
}

func TestDaemonWebForaDoAr(t *testing.T) {
	env := ambiente(t)
	// Socket numa pasta curta (o caminho de um socket Unix tem limite) e arquivo comum no lugar.
	dir, err := os.MkdirTemp("", "w")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	comum := filepath.Join(dir, "x")
	escrever(t, comum, "x")
	if r := rodar(t, env, "daemon", "--web", "unix:"+comum); r.code != 4 || !strings.Contains(r.err, "web_nao_abriu") {
		t.Fatalf("socket ocupado por arquivo: %d %s", r.code, r.err)
	}
	if r := rodar(t, env, "daemon", "--web", "0.0.0.0:8080"); r.code != 2 || !strings.Contains(r.err, "loopback") {
		t.Fatalf("0.0.0.0: %d %s", r.code, r.err)
	}
	// A trava do daemon foi solta: nada ficou preso.
	if ativo, _, _ := enriquecer.DaemonAtivo(env["ORQ_STATE_DIR"]); ativo {
		t.Fatal("daemon ficou ativo depois da falha de partida")
	}
}

// Com o --detach, a falha da web é falha de partida: o pai conta o que o filho escreveu no log.
func TestDaemonDetachComWebQueNaoAbre(t *testing.T) {
	if testing.Short() {
		t.Skip("sobe processo")
	}
	t.Setenv("ORQ_TEST_MAIN", "1")
	env := ambiente(t)
	for _, v := range enriquecer.VariavelBin {
		env[v] = "/nao/existe/bin"
	}
	dir, _ := os.MkdirTemp("", "w")
	defer os.RemoveAll(dir)
	comum := filepath.Join(dir, "x")
	escrever(t, comum, "x")
	r := rodar(t, env, "daemon", "--detach", "--web", "unix:"+comum)
	if r.code == 0 || !strings.Contains(r.err, "daemon_nao_subiu") || !strings.Contains(r.err, "web_nao_abriu") {
		t.Fatalf("detach: %d %s", r.code, r.err)
	}
}

func clienteUnix(sock string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       10 * time.Second,
	}
}

func reqWeb(t *testing.T, c *http.Client, metodo, caminho string, corpo url.Values) (int, string, http.Header) {
	t.Helper()
	var body io.Reader
	if corpo != nil {
		body = strings.NewReader(corpo.Encode())
	}
	req, err := http.NewRequest(metodo, "http://orq.local"+caminho, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Orq-User", "ana")
	if corpo != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func esperarAte(t *testing.T, o string, cond func() bool) {
	t.Helper()
	for i := 0; i < 250; i++ {
		if cond() {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("tempo esgotado esperando %s", o)
}

// O ciclo inteiro com um daemon de verdade (o binário de teste como orq): arquivo → importação →
// registrada.json → página → resposta → pend ver → aviso ao coordenador.
func TestDaemonCicloCompletoDaPendencia(t *testing.T) {
	if testing.Short() {
		t.Skip("sobe processo")
	}
	t.Setenv("ORQ_TEST_MAIN", "1")
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(t.TempDir(), "sem.sock"))
	env, pasta := pendEnv(t)
	h := falsotest.Novo(t)
	h.AgenteFixo("wA:p1", "coord", "idle")
	env["ORQ_HERDR_BIN"] = h.Bin
	for _, v := range []string{"ORQ_GLAB_BIN", "ORQ_GH_BIN", "ORQ_TAIGA_BIN"} {
		env[v] = "/nao/existe/bin"
	}
	env["ORQ_LIMPEZA_AUTOMATICA"] = "0"
	runtime, err := os.MkdirTemp("", "w")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	sock := filepath.Join(runtime, "web.sock")

	var r ResultadoDetach
	if err := json.Unmarshal([]byte(ok(t, env, "daemon", "--detach", "--tick", "200ms", "--web", "unix:"+sock, "--web-url", "https://pend.exemplo")), &r); err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(r.PID)
	if err != nil || r.Estado != "iniciado" {
		t.Fatalf("detach = %+v, %v", r, err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		for i := 0; i < 100; i++ {
			if ativo, _, _ := enriquecer.DaemonAtivo(env["ORQ_STATE_DIR"]); !ativo {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Errorf("daemon %d não saiu com SIGTERM", pid)
	})
	// A prontidão do --detach já inclui a web escutando.
	st, err := os.Stat(sock)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("socket = %v, %v", st, err)
	}
	c := clienteUnix(sock)
	if code, corpo, _ := reqWeb(t, c, "GET", "/saude", nil); code != 200 || strings.TrimSpace(corpo) != "ok" {
		t.Fatalf("/saude = %d %q", code, corpo)
	}

	// 1. O worker grava o arquivo (atômico) e o daemon importa.
	tmp := filepath.Join(pasta, ".decisao-yaml.md.tmp")
	escrever(t, tmp, arquivoDecisao)
	if err := os.Rename(tmp, filepath.Join(pasta, "decisao-yaml.md")); err != nil {
		t.Fatal(err)
	}
	registrada := filepath.Join(pasta, "decisao-yaml.registrada.json")
	esperarAte(t, "registrada.json", func() bool { _, err := os.Stat(registrada); return err == nil })
	var reg pendencias.Registrada
	b, _ := os.ReadFile(registrada)
	if err := json.Unmarshal(b, &reg); err != nil || reg.Chave != "orq/15#P1" || reg.URL != "https://pend.exemplo/p/orq/15/1" {
		t.Fatalf("registrada = %s, %v", b, err)
	}

	// 2. A página: lista e detalhe.
	if code, corpo, hd := reqWeb(t, c, "GET", "/", nil); code != 200 || !strings.Contains(corpo, "TG-15#P1") || hd.Get("Content-Security-Policy") == "" {
		t.Fatalf("/ = %d %s", code, corpo)
	}
	code, corpo, _ := reqWeb(t, c, "GET", "/p/orq/15/1", nil)
	if code != 200 || !strings.Contains(corpo, "Qual biblioteca de YAML?") || strings.Contains(corpo, "checked") {
		t.Fatalf("detalhe = %d %s", code, corpo)
	}

	// 3. O aviso de abertura chegou ao coordenador.
	esperarAte(t, "aviso de abertura", func() bool { return len(h.Prompts("wA:p1")) >= 1 })
	if p := h.Prompts("wA:p1")[0]; p != "[orq] orq TG-15 pendência P1 aberta: Qual biblioteca de YAML? — https://pend.exemplo/p/orq/15/1" {
		t.Fatalf("aviso = %q", p)
	}

	// 4. A pessoa responde pela web (POST, redirect).
	code, _, hd := reqWeb(t, c, "POST", "/p/orq/15/1/responder", url.Values{"opcao": {"2"}, "texto": {"vai com o fork, mas confira o licenciamento"}})
	if code != http.StatusSeeOther || hd.Get("Location") != "/p/orq/15/1?registrada=1" {
		t.Fatalf("POST = %d %v", code, hd)
	}

	// 5. orq pend ver mostra a resposta de ana; o coordenador recebe o aviso, sem o texto livre.
	var pv PendVer
	if err := json.Unmarshal([]byte(ok(t, env, "pend", "ver", "orq/15#P1")), &pv); err != nil || pv.Estado != "respondida" || *pv.RespondidaPor != "ana" || *pv.RespostaOpcao != 2 {
		t.Fatalf("ver = %+v, %v", pv, err)
	}
	esperarAte(t, "aviso de resposta", func() bool { return len(h.Prompts("wA:p1")) >= 2 })
	want := "[orq] orq TG-15 pendência P1 respondida: opção 2 (go.yaml.in/yaml/v3) — leia com orq pend ver orq/15#P1"
	if ps := h.Prompts("wA:p1"); ps[1] != want || strings.Contains(strings.Join(ps, "\n"), "licenciamento") {
		t.Fatalf("prompts = %q", ps)
	}
	// Nenhum aviso duplicado nos ciclos seguintes.
	time.Sleep(800 * time.Millisecond)
	if n := len(h.Prompts("wA:p1")); n != 2 {
		t.Fatalf("%d avisos, queria 2", n)
	}
	// 6. Sem o cabeçalho de usuário, a web recusa.
	req, _ := http.NewRequest("GET", "http://orq.local/", nil)
	if resp, err := c.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sem usuário: %v %v", resp, err)
	}
	// 7. SIGTERM encerra o servidor e remove o socket.
	_ = syscall.Kill(pid, syscall.SIGTERM)
	esperarAte(t, "daemon sair", func() bool { ativo, _, _ := enriquecer.DaemonAtivo(env["ORQ_STATE_DIR"]); return !ativo })
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("socket ficou: %v", err)
	}
}

// Regressão da revisão r1: pend ver mostra a resposta inteira (a listagem é que abrevia, com …).
func TestPendVerMostraARespostaInteira(t *testing.T) {
	env, pasta := pendEnv(t)
	escrever(t, filepath.Join(pasta, "d.md"), arquivoDecisao)
	ok(t, env, "pend", "importar", filepath.Join(pasta, "d.md"))
	texto := "Resposta curta no começo. " + strings.Repeat("contexto ", 12) + "NÃO EXECUTE SEM BACKUP"
	ok(t, env, "pend", "responder", "TG-15#P1", "--opcao", "2", texto)
	ver := ok(t, env, "--output", "text", "pend", "ver", "orq/15#P1")
	if !strings.Contains(ver, "NÃO EXECUTE SEM BACKUP") || !strings.Contains(ver, "opção 2 (go.yaml.in/yaml/v3)") {
		t.Fatalf("pend ver truncou a resposta:\n%s", ver)
	}
	lista := ok(t, env, "--output", "text", "pend", "list", "--respondidas")
	if strings.Contains(lista, "NÃO EXECUTE") || !strings.Contains(lista, "…") {
		t.Fatalf("a listagem devia abreviar com reticências:\n%s", lista)
	}
}
