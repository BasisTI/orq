package cli

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/herdrfalso/falsotest"
	"github.com/BasisTI/orq/internal/semdaemon"
)

const basePublica = "https://specs.exemplo.com.br"

func specComLink(t *testing.T) map[string]string {
	t.Helper()
	env, _ := pendEnv(t)
	ok(t, env, "spec", "registrar", "15", "--caminho", specPasta, "--mr", specPR)
	return env
}

func gerarLink(t *testing.T, env map[string]string, args ...string) LinkGeradoSaida {
	t.Helper()
	var l LinkGeradoSaida
	if err := json.Unmarshal([]byte(ok(t, env, append([]string{"spec", "link", "gerar", "15"}, args...)...)), &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestSpecLinkGerarRecusaSemBaseEUsaSoAPublica(t *testing.T) {
	env := specComLink(t)
	for nome, e := range map[string]map[string]string{"sem nada": env, "só ORQ_WEB_URL": withEnv(env, "ORQ_WEB_URL", "https://central.exemplo")} {
		r := rodar(t, e, "spec", "link", "gerar", "15", "--para", "cliente")
		if r.code != 2 || !strings.Contains(r.err, "url_publica_ausente") {
			t.Errorf("%s: exit %d: %s", nome, r.code, r.err)
		}
		if strings.Contains(r.out, "/s/") {
			t.Errorf("%s: imprimiu URL: %s", nome, r.out)
		}
	}
	// Nenhum link foi criado.
	var ls LinksSaida
	if err := json.Unmarshal([]byte(ok(t, env, "spec", "link", "listar", "15")), &ls); err != nil || len(ls.Links) != 0 {
		t.Fatalf("listar = %+v, %v", ls, err)
	}
	// Base inválida.
	for _, b := range []string{"ftp://x.y", "specs.exemplo.com.br", "https://", "https://x.y/?q=1", "https://u:p@x.y"} {
		if r := rodar(t, env, "spec", "link", "gerar", "15", "--para", "c", "--url-publica", b); r.code != 2 {
			t.Errorf("base %q: exit %d", b, r.code)
		}
	}
}

func withEnv(env map[string]string, k, v string) map[string]string {
	c := map[string]string{}
	for a, b := range env {
		c[a] = b
	}
	c[k] = v
	return c
}

func TestSpecLinkGerarImprimeAURLUmaVezEGuardaSoOHash(t *testing.T) {
	env := specComLink(t)
	l := gerarLink(t, env, "--para", "cliente A", "--url-publica", basePublica+"/")
	codigo := strings.TrimPrefix(l.URL, basePublica+"/s/")
	if len(codigo) != 43 || strings.Contains(codigo, "/") || l.ID == 0 || l.Rotulo != "cliente A" || l.ExpiraEm != nil || l.Aviso == "" {
		t.Fatalf("l = %+v", l)
	}
	// A base também vem de ORQ_WEB_PUBLICO_URL; a flag ganha.
	env["ORQ_WEB_PUBLICO_URL"] = "https://outra.exemplo"
	if l2 := gerarLink(t, env, "--para", "B"); !strings.HasPrefix(l2.URL, "https://outra.exemplo/s/") {
		t.Fatalf("l2 = %+v", l2)
	}
	if l3 := gerarLink(t, env, "--para", "C", "--url-publica", basePublica); !strings.HasPrefix(l3.URL, basePublica+"/s/") {
		t.Fatalf("l3 = %+v", l3)
	}
	// Em texto: a URL e o aviso.
	r := rodar(t, env, "--output", "text", "spec", "link", "gerar", "15", "--para", "texto", "--expira", "30d")
	if r.code != 0 || !strings.Contains(r.out, "https://outra.exemplo/s/") || !strings.Contains(r.out, "não aparece de novo") || !strings.Contains(r.out, "vale até") {
		t.Fatalf("texto: %d %s", r.code, r.out)
	}
	// Nem a listagem nem o banco nem o diário têm o código.
	listar := ok(t, env, "spec", "link", "listar", "15")
	diario := ok(t, env, "diario")
	if strings.Contains(listar, codigo) || strings.Contains(diario, codigo) {
		t.Fatal("o código apareceu na listagem ou no diário")
	}
	if !strings.Contains(diario, "spec.link.gerar") {
		t.Fatalf("o gerar devia ter evento no diário: %s", diario)
	}
	filepath.WalkDir(env["ORQ_STATE_DIR"], func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, _ := os.ReadFile(p); bytes.Contains(b, []byte(codigo)) {
			t.Errorf("o código está em %s", p)
		}
		return nil
	})
}

func TestSpecLinkExpiraValidaAEntrada(t *testing.T) {
	env := specComLink(t)
	l := gerarLink(t, env, "--para", "c", "--url-publica", basePublica, "--expira", "12h")
	if l.ExpiraEm == nil {
		t.Fatalf("l = %+v", l)
	}
	for _, e := range []string{"0d", "-1d", "d", "abc", "30", "30x", "1.5d", "99999d"} {
		if r := rodar(t, env, "spec", "link", "gerar", "15", "--para", "c", "--url-publica", basePublica, "--expira", e); r.code != 2 {
			t.Errorf("--expira %q: exit %d", e, r.code)
		}
	}
	if r := rodar(t, env, "spec", "link", "gerar", "15", "--url-publica", basePublica); r.code != 2 {
		t.Errorf("sem --para: exit %d", r.code)
	}
}

func TestSpecLinkListarERevogar(t *testing.T) {
	env := specComLink(t)
	a := gerarLink(t, env, "--para", "A", "--url-publica", basePublica)
	b := gerarLink(t, env, "--para", "B", "--url-publica", basePublica, "--expira", "1h")
	var ls LinksSaida
	if err := json.Unmarshal([]byte(ok(t, env, "spec", "link", "listar", "15")), &ls); err != nil || len(ls.Links) != 2 {
		t.Fatalf("listar = %+v, %v", ls, err)
	}
	if ls.Links[0].Rotulo != "A" || ls.Links[0].Estado != "ativo" || ls.Links[0].Acessos != 0 || ls.Links[1].ExpiraEm == nil {
		t.Fatalf("links = %+v", ls.Links)
	}
	texto := ok(t, env, "--output", "text", "spec", "link", "listar", "15")
	if !strings.Contains(texto, "ativo") || !strings.Contains(texto, "nunca acessado") || !strings.Contains(texto, "0 acessos") {
		t.Fatalf("texto = %s", texto)
	}
	if out := ok(t, env, "--output", "text", "spec", "link", "revogar", "15", itoa64(a.ID)); !strings.Contains(out, "revogado") {
		t.Fatalf("revogar = %s", out)
	}
	if r := rodar(t, env, "spec", "link", "revogar", "15", itoa64(a.ID)); r.code != 4 || !strings.Contains(r.err, "link_ja_revogado") {
		t.Fatalf("de novo: %d %s", r.code, r.err)
	}
	for _, id := range []string{"abc", "0", "-1"} {
		if r := rodar(t, env, "spec", "link", "revogar", "15", id); r.code != 2 {
			t.Errorf("id %q: exit %d", id, r.code)
		}
	}
	if r := rodar(t, env, "spec", "link", "revogar", "15", "999"); r.code != 5 {
		t.Errorf("inexistente: exit %d: %s", r.code, r.err)
	}
	if err := json.Unmarshal([]byte(ok(t, env, "spec", "link", "listar", "15")), &ls); err != nil || ls.Links[0].Estado != "revogado" || ls.Links[1].Estado != "ativo" {
		t.Fatalf("depois: %+v, %v", ls, err)
	}
	// Gerar de novo depois de revogar cria outro link, com outro código.
	c := gerarLink(t, env, "--para", "A", "--url-publica", basePublica)
	if c.ID == a.ID || c.URL == a.URL || c.URL == b.URL {
		t.Fatalf("c = %+v", c)
	}
	// US sem spec: nem gerar nem listar.
	for _, args := range [][]string{{"spec", "link", "gerar", "16", "--para", "x", "--url-publica", basePublica}, {"spec", "link", "listar", "16"}} {
		if r := rodar(t, env, args...); r.code == 0 {
			t.Errorf("%v devia falhar sem spec", args)
		}
	}
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

func TestSpecSubstituirAvisaOsLinksAtivosEAVersao(t *testing.T) {
	env := specComLink(t)
	gerarLink(t, env, "--para", "A", "--url-publica", basePublica)
	gerarLink(t, env, "--para", "B", "--url-publica", basePublica)
	out := ok(t, env, "--output", "text", "spec", "registrar", "15", "--caminho", "docs/nova", "--mr", specPR, "--substituir")
	if !strings.Contains(out, "2 links ativos passam a mostrar a spec nova") || !strings.Contains(out, "versão 2 da spec") {
		t.Fatalf("out = %s", out)
	}
	var res struct {
		Spec        struct{ Versao int }
		LinksAtivos int `json:"links_ativos"`
	}
	if err := json.Unmarshal([]byte(ok(t, env, "spec", "registrar", "15", "--caminho", "docs/nova2", "--mr", specPR, "--substituir")), &res); err != nil || res.LinksAtivos != 2 || res.Spec.Versao != 3 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	// Os links seguem ativos.
	var ls LinksSaida
	if err := json.Unmarshal([]byte(ok(t, env, "spec", "link", "listar", "15")), &ls); err != nil || len(ls.Links) != 2 || ls.Links[0].Estado != "ativo" {
		t.Fatalf("listar = %+v, %v", ls, err)
	}
	// Sem links ativos, o aviso não aparece.
	ok(t, env, "spec", "link", "revogar", "15", "1")
	ok(t, env, "spec", "link", "revogar", "15", "2")
	if out := ok(t, env, "--output", "text", "spec", "registrar", "15", "--caminho", "docs/nova3", "--mr", specPR, "--substituir"); strings.Contains(out, "links ativos") {
		t.Fatalf("sem links ativos: %s", out)
	}
	if avisoLinksAtivos(1) != "1 link ativo passa a mostrar a spec nova" {
		t.Fatal("singular")
	}
}

func TestDaemonWebPublicoRecusaEnderecosInvalidos(t *testing.T) {
	env := ambiente(t)
	dir := semdaemon.DirCurto(t, "w")
	sock := filepath.Join(dir, "a.sock")
	for nome, args := range map[string][]string{
		"unix: sem caminho":    {"daemon", "--web-publico", "unix:"},
		"igual ao da Central":  {"daemon", "--web", "unix:" + sock, "--web-publico", sock},
		"igual, com unix:":     {"daemon", "--web", "unix:" + sock, "--web-publico", "unix:" + sock},
		"fora do loopback":     {"daemon", "--web-publico", "0.0.0.0:8081"},
		"caminho relativo":     {"daemon", "--web-publico", "unix:relativo.sock"},
		"mesma porta loopback": {"daemon", "--web", "127.0.0.1:18081", "--web-publico", "127.0.0.1:18081"},
	} {
		if r := rodar(t, env, args...); r.code == 0 || r.code == 4 && strings.Contains(r.err, "daemon_ativo") {
			t.Errorf("%s: exit %d: %s", nome, r.code, r.err)
		}
	}
	if ativo, _, _ := enriquecer.DaemonAtivo(env["ORQ_STATE_DIR"]); ativo {
		t.Fatal("daemon ficou ativo depois da falha de partida")
	}
}

// Um daemon de verdade com os dois sockets: o público só responde ao link; a Central não responde a ele.
func TestDaemonWebPublicoServeSoOLink(t *testing.T) {
	if testing.Short() {
		t.Skip("sobe processo")
	}
	t.Setenv("ORQ_TEST_MAIN", "1")
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(t.TempDir(), "sem.sock"))
	env, _ := pendEnv(t)
	h := falsotest.Novo(t)
	h.AgenteFixo("wA:p1", "coord", "idle")
	env["ORQ_HERDR_BIN"] = h.Bin
	for _, v := range []string{"ORQ_GLAB_BIN", "ORQ_GH_BIN", "ORQ_TAIGA_BIN"} {
		env[v] = "/nao/existe/bin"
	}
	env["ORQ_LIMPEZA_AUTOMATICA"] = "0"
	ok(t, env, "spec", "registrar", "15", "--caminho", specPasta, "--mr", specPR)
	l := gerarLink(t, env, "--para", "cliente", "--url-publica", basePublica)
	codigo := strings.TrimPrefix(l.URL, basePublica+"/s/")

	runtime := semdaemon.DirCurto(t, "w")
	central, publico := filepath.Join(runtime, "web.sock"), filepath.Join(runtime, "pub.sock")
	var r ResultadoDetach
	if err := json.Unmarshal([]byte(ok(t, env, "daemon", "--detach", "--tick", "200ms", "--web", "unix:"+central, "--web-publico", publico)), &r); err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(r.PID)
	if err != nil || r.Estado != "iniciado" {
		t.Fatalf("detach = %+v, %v", r, err)
	}
	t.Cleanup(func() {
		defer semdaemon.Parar(env["ORQ_STATE_DIR"])
		_ = syscall.Kill(pid, syscall.SIGTERM)
		for i := 0; i < 100; i++ {
			if ativo, _, _ := enriquecer.DaemonAtivo(env["ORQ_STATE_DIR"]); !ativo {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Errorf("daemon %d não saiu com SIGTERM", pid)
	})
	// A prontidão do --detach já inclui os dois sockets.
	for _, s := range []string{central, publico} {
		if st, err := os.Stat(s); err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("socket %s = %v, %v", s, st, err)
		}
	}
	pub, cen := clienteUnix(publico), clienteUnix(central)
	code, corpo, cab := reqWeb(t, pub, "GET", "/s/"+codigo, nil)
	if code != 200 || !strings.Contains(corpo, "ainda não está disponível") || cab.Get("Referrer-Policy") != "no-referrer" || cab.Get("X-Robots-Tag") != "noindex" || cab.Get("Cache-Control") != "no-store" {
		t.Fatalf("página pública = %d %v\n%s", code, cab, corpo)
	}
	// O socket público não responde a nada da Central, nem ao /saude.
	_, ref, _ := reqWeb(t, pub, "GET", "/nada", nil)
	for _, caminho := range []string{"/", "/saude", "/pendencias", "/p/orq/15/spec", "/onda/1", "/historico", "/vivo", "/avisos"} {
		if code, corpo, _ := reqWeb(t, pub, "GET", caminho, nil); code != 404 || corpo != ref {
			t.Errorf("%s no socket público: %d %q", caminho, code, corpo)
		}
	}
	// E o socket da Central não serve o link.
	if code, corpo, _ := reqWeb(t, cen, "GET", "/s/"+codigo, nil); code == 200 || strings.Contains(corpo, "ainda não está disponível") {
		t.Fatalf("a Central serviu o link: %d", code)
	}
	if code, corpo, _ := reqWeb(t, cen, "GET", "/saude", nil); code != 200 || strings.TrimSpace(corpo) != "ok" {
		t.Fatalf("Central /saude = %d %q", code, corpo)
	}
	// O acesso foi contado; revogado, o link dá o mesmo 404.
	var ls LinksSaida
	esperarAte(t, "o acesso contado", func() bool {
		_ = json.Unmarshal([]byte(ok(t, env, "spec", "link", "listar", "15")), &ls)
		return len(ls.Links) == 1 && ls.Links[0].Acessos == 1
	})
	ok(t, env, "spec", "link", "revogar", "15", strconv.FormatInt(l.ID, 10))
	if code, corpo, _ := reqWeb(t, pub, "GET", "/s/"+codigo, nil); code != 404 || corpo != ref {
		t.Fatalf("revogado: %d %q", code, corpo)
	}
	// O log do daemon não tem o código.
	log, err := os.ReadFile(filepath.Join(env["ORQ_STATE_DIR"], ArquivoLogDaemon))
	if err != nil || strings.Contains(string(log), codigo) || !strings.Contains(string(log), "web público: escutando") {
		t.Fatalf("log = %v, %v", string(log), err)
	}
}
