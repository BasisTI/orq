package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// herdrCentral é um herdr falso com o mínimo de estado para orq central abrir: o workspace
// passa a existir depois do create, pane list devolve panes.json, split numera os panes.
// Cada chamada vai para log, uma por linha, com os argumentos separados por |.
const herdrCentral = `#!/bin/sh
d='%DIR%'
printf '%s|' "$@" >> "$d/log"; echo >> "$d/log"
case "$1 $2" in
"workspace list")
	if [ -f "$d/criado" ]; then echo '{"result":{"workspaces":[{"workspace_id":"w1","label":"outro"},{"workspace_id":"wT","label":"Central teste"}]}}'
	else echo '{"result":{"workspaces":[{"workspace_id":"w1","label":"outro"}]}}'; fi ;;
"workspace create") touch "$d/criado"; echo '{"result":{"workspace":{"workspace_id":"wT"},"tab":{"tab_id":"wT:t1"},"root_pane":{"pane_id":"wT:p1"}}}' ;;
"pane list") cat "$d/panes.json" ;;
"pane split") n=$(cat "$d/n" 2>/dev/null || echo 1); n=$((n+1)); echo $n > "$d/n"; echo "{\"result\":{\"pane\":{\"pane_id\":\"wT:p$n\"}}}" ;;
"plugin list") cat "$d/plugins.json" 2>/dev/null || echo '{"result":{"plugins":[]}}' ;;
"config check") if grep -q REPROVAR "$HERDR_CONFIG_PATH" 2>/dev/null; then echo 'config: issues found'; echo 'unknown config key x' >&2; exit 1; fi; echo 'config: ok' ;;
"agent focus") if [ "$3" = "wX:p0" ]; then echo '{"error":{"code":"agent_not_found","message":"no agent"}}'; exit 1; fi; echo '{"result":{}}' ;;
*) echo '{"result":{}}' ;;
esac
`

func novoHerdrCentral(t *testing.T) (dir string, env map[string]string) {
	t.Helper()
	dir = t.TempDir()
	bin := filepath.Join(dir, "herdr")
	if err := os.WriteFile(bin, []byte(strings.ReplaceAll(herdrCentral, "%DIR%", dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	env = ambiente(t)
	env["ORQ_HERDR_BIN"] = bin
	env["HOME"] = dir
	return dir, env
}

func chamadas(t *testing.T, dir string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "log"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func escrever(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCentralAbrirIdempotente(t *testing.T) {
	dir, env := novoHerdrCentral(t)
	for _, c := range []string{"a", "b", "c"} {
		ok(t, env, "coord", "registrar", c, "--pane", "wB:p"+c, "--projeto", "orq")
	}
	ok(t, env, "--coord", "a", "onda", "criar", "oa", "--us", "1")
	ok(t, env, "--coord", "b", "onda", "criar", "ob", "--us", "2")
	escrever(t, filepath.Join(dir, "panes.json"), `{"result":{"panes":[{"pane_id":"wT:p1"}]}}`)

	var r ResultadoCentral
	if err := json.Unmarshal([]byte(ok(t, env, "central", "abrir", "--nome", "Central teste", "--sem-foco")), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Criado || r.WorkspaceID == nil || *r.WorkspaceID != "wT" || r.Focado ||
		len(r.Panes) != 2 || r.Panes[0] != (PaneCentral{"a", "wT:p1", "criado"}) || r.Panes[1] != (PaneCentral{"b", "wT:p2", "criado"}) {
		t.Fatalf("r = %+v", r)
	}
	log := strings.Join(chamadas(t, dir), "\n")
	exe, _ := os.Executable()
	for _, quer := range []string{
		"workspace|create|--label|Central teste|--no-focus|--env|ORQ_STATE_DIR=" + env["ORQ_STATE_DIR"] + "|--env|ORQ_HERDR_BIN=" + env["ORQ_HERDR_BIN"] + "|--cwd|" + dir + "|",
		"pane|rename|wT:p1|orq · a|",
		"pane|run|wT:p1|'" + exe + "' 'tui' '--coord' 'a'|",
		"pane|split|wT:p1|--direction|down|--no-focus|--env|ORQ_STATE_DIR=",
		"pane|rename|wT:p2|orq · b|",
	} {
		if !strings.Contains(log, quer) {
			t.Errorf("falta %q em:\n%s", quer, log)
		}
	}
	if strings.Contains(log, "workspace|focus") {
		t.Error("focou com --sem-foco")
	}

	// Segunda chamada: a fica, b ganhou pane perdido (recriado), c deixou de ter onda e o pane
	// dele fecha; o pane do usuário não é tocado.
	ok(t, env, "onda", "encerrar", "1")
	ok(t, env, "--coord", "c", "onda", "criar", "oc", "--us", "3")
	escrever(t, filepath.Join(dir, "panes.json"), `{"result":{"panes":[{"pane_id":"wT:p9","label":"meu shell"},{"pane_id":"wT:p1","label":"orq · a"},{"pane_id":"wT:p2","label":"orq · b"}]}}`)
	escrever(t, filepath.Join(dir, "log"), "")
	r = ResultadoCentral{}
	if err := json.Unmarshal([]byte(ok(t, env, "central", "abrir", "--nome", "Central teste")), &r); err != nil {
		t.Fatal(err)
	}
	quer := []PaneCentral{{"b", "wT:p2", "mantido"}, {"c", "wT:p3", "criado"}, {"a", "wT:p1", "fechado"}}
	if r.Criado || !r.Focado || len(r.Panes) != 3 {
		t.Fatalf("r = %+v", r)
	}
	for i := range quer {
		if r.Panes[i] != quer[i] {
			t.Errorf("pane %d = %+v, quer %+v", i, r.Panes[i], quer[i])
		}
	}
	log = strings.Join(chamadas(t, dir), "\n")
	for _, quer := range []string{"pane|split|wT:p2|", "pane|close|wT:p1|", "workspace|focus|wT|"} {
		if !strings.Contains(log, quer) {
			t.Errorf("falta %q em:\n%s", quer, log)
		}
	}
	if strings.Contains(log, "wT:p9") || strings.Contains(log, "workspace|create") {
		t.Errorf("tocou no pane do usuário ou recriou o workspace:\n%s", log)
	}
}

func TestCentralAbrirSemOnda(t *testing.T) {
	dir, env := novoHerdrCentral(t)
	ok(t, env, "coord", "registrar", "a")
	var r ResultadoCentral
	if err := json.Unmarshal([]byte(ok(t, env, "central", "abrir", "--nome", "Central teste")), &r); err != nil {
		t.Fatal(err)
	}
	if r.WorkspaceID != nil || r.Criado || len(r.Panes) != 0 {
		t.Fatalf("r = %+v", r)
	}
	if log := strings.Join(chamadas(t, dir), "\n"); strings.Contains(log, "create") {
		t.Fatalf("criou workspace sem coordenador com onda:\n%s", log)
	}
}

func TestCentralAbrirHerdrFora(t *testing.T) {
	env := ambiente(t)
	env["ORQ_HERDR_BIN"] = "/nao/existe/herdr"
	ok(t, env, "coord", "registrar", "a")
	ok(t, env, "onda", "criar", "o", "--us", "1", "--projeto", "orq")
	if r := rodar(t, env, "central", "abrir"); r.code != 1 || !strings.Contains(r.err, `"herdr_falhou"`) {
		t.Fatalf("exit %d %s", r.code, r.err)
	}
}

func TestComandoShell(t *testing.T) {
	if s := comandoShell("/x y/orq", "tui", "--coord", "o'brien"); s != `'/x y/orq' 'tui' '--coord' 'o'\''brien'` {
		t.Fatal(s)
	}
}

func TestPaneDaURL(t *testing.T) {
	bons := map[string]string{
		"https://orq.local/pane/w2A:p9":       "w2A:p9",
		"https://orq.local/pane/w2A%3Ap9":     "w2A:p9",
		"https://orq.local/pane/wB:p3/":       "wB:p3",
		"https://orq.local/pane/wB:p3?x=1#ab": "wB:p3",
	}
	for u, quer := range bons {
		if p, err := PaneDaURL(u); err != nil || p != quer {
			t.Errorf("%s = %q, %v", u, p, err)
		}
	}
	for _, u := range []string{"https://orq.local/pane/", "https://orq.local/pane/-rf", "https://orq.local/pane/a%20b", "https://evil.local/pane/wB:p3", "http://orq.local/pane/wB:p3", "https://orq.local/pane/a/b"} {
		if p, err := PaneDaURL(u); err == nil {
			t.Errorf("%s aceito como %q", u, p)
		}
	}
}

// focar roteia a URL do link handler ($HERDR_PLUGIN_CLICKED_URL) para herdr agent focus.
func TestFocarPelaURLDoLink(t *testing.T) {
	dir, env := novoHerdrCentral(t)
	env["HERDR_PLUGIN_CLICKED_URL"] = "https://orq.local/pane/w2A%3Ap9"
	if out := ok(t, env, "focar"); !strings.Contains(out, `"pane": "w2A:p9"`) {
		t.Fatalf("out = %s", out)
	}
	ok(t, env, "focar", "wB:p3")
	log := chamadas(t, dir)
	if len(log) != 2 || log[0] != "agent|focus|w2A:p9|" || log[1] != "agent|focus|wB:p3|" {
		t.Fatalf("chamadas = %q", log)
	}
	if r := rodar(t, env, "focar", "wX:p0"); r.code != 1 || !strings.Contains(r.err, `"foco_falhou"`) || !strings.Contains(r.err, "agent_not_found") {
		t.Fatalf("pane sem agente: %d %s", r.code, r.err)
	}
	if r := rodar(t, env, "focar", "--", "-rf"); r.code != 2 {
		t.Fatalf("pane inválido: %d %s", r.code, r.err)
	}
	if r := rodar(t, env, "focar", "https://outro/pane/x"); r.code != 2 {
		t.Fatalf("URL alheia: %d %s", r.code, r.err)
	}
	delete(env, "HERDR_PLUGIN_CLICKED_URL")
	env["HERDR_PLUGIN_CONTEXT_JSON"] = `{"invocation_source":"link_click","clicked_url":"https://orq.local/pane/wC:p2","link_handler_id":"pane"}`
	ok(t, env, "focar")
	if log := chamadas(t, dir); log[len(log)-1] != "agent|focus|wC:p2|" {
		t.Fatalf("contexto: %q", log)
	}
	delete(env, "HERDR_PLUGIN_CONTEXT_JSON")
	if r := rodar(t, env, "focar"); r.code != 2 {
		t.Fatalf("sem alvo: %d %s", r.code, r.err)
	}
}

func TestTUIRecusaTodosComCoord(t *testing.T) {
	env := ambiente(t)
	if r := rodar(t, env, "--coord", "a", "tui", "--todos"); r.code != 2 {
		t.Fatalf("exit %d %s", r.code, r.err)
	}
}

// O orq.env do diretório de configuração do plugin fornece ORQ_* sem passar por cima do
// ambiente do processo.
func TestAmbientePlugin(t *testing.T) {
	cfg := t.TempDir()
	estado := t.TempDir()
	escrever(t, filepath.Join(cfg, ArquivoAmbientePlugin), "# estado de teste\nexport ORQ_STATE_DIR='"+estado+"'\nHOME=/nao\nORQ_COORD = \"x\"\n")
	base := map[string]string{"HERDR_PLUGIN_CONFIG_DIR": cfg, "ORQ_COORD": "do-processo"}
	env := comAmbientePlugin(func(k string) string { return base[k] })
	if env("ORQ_STATE_DIR") != estado || env("ORQ_COORD") != "do-processo" || env("HOME") != "" {
		t.Fatalf("STATE=%q COORD=%q HOME=%q", env("ORQ_STATE_DIR"), env("ORQ_COORD"), env("HOME"))
	}
	// Pela CLI: o banco vai para o estado do arquivo.
	r := rodar(t, map[string]string{"HERDR_PLUGIN_CONFIG_DIR": cfg}, "coord", "registrar", "p")
	if r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.err)
	}
	if _, err := os.Stat(filepath.Join(estado, "central.db")); err != nil {
		t.Fatalf("banco fora do estado do orq.env: %v", err)
	}
}

func TestPluginConfigurarPelaCLI(t *testing.T) {
	dir, env := novoHerdrCentral(t)
	cfg := filepath.Join(dir, "config.toml")
	escrever(t, cfg, "[ui]\nsidebar_width = 44\n")
	out := ok(t, env, "plugin", "configurar", "--config", cfg)
	var r struct {
		Mudancas []string `json:"mudancas"`
		Backup   *string  `json:"backup"`
		Check    *string  `json:"check"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Mudancas) != 4 || r.Backup == nil || r.Check == nil || *r.Check != "config: ok" {
		t.Fatalf("r = %s", out)
	}
	if log := strings.Join(chamadas(t, dir), "\n"); strings.Count(log, "config|check|") != 2 {
		t.Fatalf("checks:\n%s", log)
	}
	// Idempotente.
	if out := ok(t, env, "--output", "text", "plugin", "configurar", "--config", cfg); !strings.Contains(out, "nada a mudar") {
		t.Fatalf("segunda: %s", out)
	}
	// desconfigurar volta ao original; tecla ocupada é conflito, sem escrever.
	if r := rodar(t, env, "plugin", "desconfigurar", "--config", cfg); r.code != 0 {
		t.Fatalf("desconfigurar: %d %s", r.code, r.err)
	}
	limpo, _ := os.ReadFile(cfg)
	// As tabelas que configurar criou ficam com as rows padrão do Herdr, sem os tokens.
	if !strings.HasPrefix(string(limpo), "[ui]\nsidebar_width = 44\n") || strings.Contains(string(limpo), "orq") {
		t.Fatalf("desconfigurar não voltou:\n%s", limpo)
	}
	if r := rodar(t, env, "plugin", "configurar", "--config", cfg, "--tecla", "prefix+z"); r.code != 4 || !strings.Contains(r.err, `"tecla_ocupada"`) {
		t.Fatalf("tecla ocupada: %d %s", r.code, r.err)
	}
	if b, _ := os.ReadFile(cfg); string(b) != string(limpo) {
		t.Fatalf("tecla ocupada escreveu:\n%s", b)
	}
	// Outra tecla e sem a de abrir-central.
	out = ok(t, env, "plugin", "configurar", "--config", cfg, "--tecla", "prefix+alt+u", "--tecla-central", "")
	if !strings.Contains(out, "prefix+alt+u → orq.popup-todos") || strings.Contains(out, "abrir-central") {
		t.Fatalf("out = %s", out)
	}
}

func TestPluginConfigurarCheckReprovaRestaura(t *testing.T) {
	dir, env := novoHerdrCentral(t)
	cfg := filepath.Join(dir, "config.toml")
	original := "[ui]\nsidebar_width = 44\n"
	escrever(t, cfg, original)
	// Um herdr que aprova o arquivo atual e reprova o editado (que tem orq.popup-todos).
	bin := filepath.Join(dir, "herdr-reprova")
	escrever(t, bin, "#!/bin/sh\nif grep -q popup-todos \"$HERDR_CONFIG_PATH\"; then echo 'config: issues found'; exit 1; fi; echo 'config: ok'\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	env["ORQ_HERDR_BIN"] = bin
	r := rodar(t, env, "plugin", "configurar", "--config", cfg)
	if r.code != 4 || !strings.Contains(r.err, `"config_check_falhou"`) || !strings.Contains(r.err, "restaurado") {
		t.Fatalf("exit %d %s", r.code, r.err)
	}
	if b, _ := os.ReadFile(cfg); string(b) != original {
		t.Fatalf("não restaurou:\n%s", b)
	}
	baks, _ := filepath.Glob(cfg + ".orq-*.bak")
	if len(baks) != 1 {
		t.Fatalf("backups = %v", baks)
	}
	// Arquivo que já reprova não é tocado.
	escrever(t, cfg, original+"# REPROVAR popup-todos\n")
	if r := rodar(t, env, "plugin", "configurar", "--config", cfg); r.code != 4 || !strings.Contains(r.err, `"config_invalida"`) {
		t.Fatalf("exit %d %s", r.code, r.err)
	}
}

func TestPluginDoctor(t *testing.T) {
	dir, env := novoHerdrCentral(t)
	cfg := filepath.Join(dir, "config.toml")
	escrever(t, filepath.Join(dir, "plugins.json"), `{"result":{"plugins":[{"plugin_id":"orq","enabled":true,"plugin_root":"/x/orq"}]}}`)
	ok(t, env, "plugin", "configurar", "--config", cfg)
	var d Doctor
	if err := json.Unmarshal([]byte(ok(t, env, "plugin", "doctor", "--config", cfg)), &d); err != nil {
		t.Fatal(err)
	}
	estado := map[string]string{}
	for _, it := range d.Itens {
		estado[it.Nome] = it.Estado
	}
	// Sem daemon: ok falso; o resto em dia.
	if d.OK || estado["daemon"] != "erro" || estado["plugin"] != "ok" || estado["sidebar agentes"] != "ok" || estado["sidebar espaços"] != "ok" ||
		estado["tecla popup-todos"] != "ok" || estado["tecla abrir-central"] != "ok" || estado["banco"] != "ok" || estado["binario herdr"] != "ok" {
		t.Fatalf("doctor = %+v", d)
	}
}
