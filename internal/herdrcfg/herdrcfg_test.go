package herdrcfg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// O config real do Cedric em 2026-10-11, com os tokens da skill manual.
const configReal = `onboarding = false

[terminal]
# Panes novos nascem em zsh.
default_shell = "/usr/bin/zsh"

[ui.sidebar.agents]
rows = [["state_icon", "machine", "workspace", "tab"], ["agent"], ["$story", "$fase"]]
[ui.sidebar.spaces]
rows = [["state_icon", "workspace"], ["branch", "git_status"], ["$mrs_prontas"]]
[ui]
sidebar_width = 44        # largura inicial
sidebar_max_width = 120    # limite do arrasto
`

var padrao = Opcoes{Tecla: TeclaPadrao, TeclaCentral: TeclaCentralPadrao}

func configurar(t *testing.T, texto string, o Opcoes) (string, []string) {
	t.Helper()
	novo, mud, err := Configurar([]byte(texto), o)
	if err != nil {
		t.Fatalf("Configurar: %v", err)
	}
	return string(novo), mud
}

func TestConfigurarConfigReal(t *testing.T) {
	novo, mud := configurar(t, configReal, padrao)
	if len(mud) != 4 {
		t.Fatalf("mudanças = %v", mud)
	}
	for _, quer := range []string{
		`rows = [["state_icon", "machine", "workspace", "tab"], ["agent"], ["$story", "$fase"], [{ token = "$orq" }]]`,
		`rows = [["state_icon", "workspace"], ["branch", "git_status"], ["$mrs_prontas"], [{ token = "$orq_mrs" }]]`,
		"# Panes novos nascem em zsh.\n",
		"sidebar_width = 44        # largura inicial\n",
		"[[keys.command]]\nkey = \"prefix+u\"\ntype = \"plugin_action\"\ncommand = \"orq.popup-todos\"\n",
		"key = \"prefix+shift+u\"\ntype = \"plugin_action\"\ncommand = \"orq.abrir-central\"\n",
	} {
		if !strings.Contains(novo, quer) {
			t.Errorf("falta %q em:\n%s", quer, novo)
		}
	}
	s, err := Ler([]byte(novo))
	if err != nil || !s.LinhaAgentes || !s.LinhaEspacos || s.Teclas[AcaoPopup] != "prefix+u" || s.Teclas[AcaoCentral] != "prefix+shift+u" {
		t.Fatalf("situação = %+v, %v", s, err)
	}
	// Idempotente: de novo, nada muda.
	if deNovo, mud := configurar(t, novo, padrao); len(mud) != 0 || deNovo != novo {
		t.Fatalf("segunda vez mudou: %v", mud)
	}
	// Desconfigurar volta exatamente ao original.
	volta, mud, err := Desconfigurar([]byte(novo))
	if err != nil {
		t.Fatal(err)
	}
	if string(volta) != configReal {
		t.Fatalf("desconfigurar não voltou ao original (%v):\n%s", mud, volta)
	}
	if _, mud, _ := Desconfigurar(volta); len(mud) != 0 {
		t.Fatalf("desconfigurar de novo mudou: %v", mud)
	}
}

func TestConfigurarSemRows(t *testing.T) {
	// Arquivo vazio: rows padrão do Herdr mais a linha do orq.
	novo, _ := configurar(t, "", Opcoes{Tecla: "prefix+u"})
	if !strings.Contains(novo, "[ui.sidebar.agents]\nrows = [[\"state_icon\", \"machine\", \"workspace\", \"tab\"], [\"agent\"], [{ token = \"$orq\" }]]\n") ||
		strings.Contains(novo, "abrir-central") {
		t.Fatalf("novo =\n%s", novo)
	}
	// Tabela sem rows: rows entra logo abaixo do cabeçalho.
	novo, _ = configurar(t, "[ui.sidebar.spaces]\nrow_gap = 1\n", Opcoes{})
	if !strings.HasPrefix(novo, "[ui.sidebar.spaces]\nrows = [[\"state_icon\", \"workspace\"], [\"branch\", \"git_status\"], [{ token = \"$orq_mrs\" }]]\nrow_gap = 1\n") {
		t.Fatalf("novo =\n%s", novo)
	}
}

func TestConfigurarRowsMultilinhaEFormaDoUsuario(t *testing.T) {
	texto := `[ui.sidebar]
agents.rows = [
  ["state_icon", "agent"],  # comentário dentro do array
  [{ token = "$story", fg = "#89b4fa" }],
]
spaces.rows = [["workspace"], ["$orq_mrs"]]
`
	novo, mud := configurar(t, texto, Opcoes{})
	quer := `[ui.sidebar]
agents.rows = [
  ["state_icon", "agent"],  # comentário dentro do array
  [{ token = "$story", fg = "#89b4fa" }],
  [{ token = "$orq" }],
]
spaces.rows = [["workspace"], ["$orq_mrs"]]
`
	// "$orq_mrs" em string pura já conta: spaces.rows não muda.
	if len(mud) != 1 || novo != quer {
		t.Fatalf("mud %v novo =\n%s", mud, novo)
	}
	// desconfigurar tira só a linha do orq: volta byte a byte, e o "$orq_mrs" do usuário fica.
	volta, mud, err := Desconfigurar([]byte(novo))
	if err != nil || string(volta) != texto || len(mud) != 1 {
		t.Fatalf("mud %v volta =\n%s (%v)", mud, volta, err)
	}
}

// Os casos de rows do relatório de revisão r1 (M2) e as variantes de vírgula e de fechamento.
func TestRowsPreservaTexto(t *testing.T) {
	cases := []struct{ nome, antes, depois string }{
		{"comentário e vírgula final (M2)",
			"[ui.sidebar.agents]\nrows = [\n [\"agent\"], # meu comentário de layout\n [\"$story\", \"$fase\"],\n]\n",
			"[ui.sidebar.agents]\nrows = [\n [\"agent\"], # meu comentário de layout\n [\"$story\", \"$fase\"],\n [{ token = \"$orq\" }],\n]\n"},
		{"sem vírgula final",
			"[ui.sidebar.agents]\nrows = [\n    [\"agent\"],\n    [\"$story\"]  # fim\n]\n",
			"[ui.sidebar.agents]\nrows = [\n    [\"agent\"],\n    [\"$story\"],  # fim\n    [{ token = \"$orq\" }]\n]\n"},
		{"fecha na linha do último",
			"[ui.sidebar.agents]\nrows = [\n  [\"agent\"],\n  [\"$story\"]]\n",
			"[ui.sidebar.agents]\nrows = [\n  [\"agent\"],\n  [\"$story\"], [{ token = \"$orq\" }]]\n"},
		{"vazia",
			"[ui.sidebar.agents]\nrows = []\n",
			"[ui.sidebar.agents]\nrows = [[{ token = \"$orq\" }]]\n"},
	}
	for _, c := range cases {
		novo, _ := configurar(t, c.antes, Opcoes{})
		// spaces.rows ausente vira tabela nova no fim; só a parte de agents interessa aqui.
		agents := novo[:strings.Index(novo, "\n\n[ui.sidebar.spaces]")+1]
		if agents != c.depois {
			t.Errorf("%s: configurar =\n%s\nquer\n%s", c.nome, agents, c.depois)
			continue
		}
		volta, _, err := Desconfigurar([]byte(novo))
		if err != nil || !strings.HasPrefix(string(volta), c.antes) {
			t.Errorf("%s: desconfigurar =\n%s (%v)", c.nome, volta, err)
		}
	}
}

// Um token do orq que o usuário pôs numa linha dele (ou com estilo) não é do configurar.
func TestDesconfigurarDeixaTokenDoUsuario(t *testing.T) {
	texto := "[ui.sidebar.agents]\nrows = [[\"agent\", \"$orq\"], [{ token = \"$orq\", fg = \"#ff0000\" }]]\n" +
		"[ui.sidebar.spaces]\nrows = [[\"workspace\"], [{ token = \"$orq_mrs\" }]]\n"
	volta, mud, err := Desconfigurar([]byte(texto))
	if err != nil {
		t.Fatal(err)
	}
	quer := "[ui.sidebar.agents]\nrows = [[\"agent\", \"$orq\"], [{ token = \"$orq\", fg = \"#ff0000\" }]]\n" +
		"[ui.sidebar.spaces]\nrows = [[\"workspace\"]]\n"
	if string(volta) != quer || len(mud) != 1 {
		t.Fatalf("mud %v volta =\n%s", mud, volta)
	}
}

// M1 da revisão r1: uma tecla manual para orq.popup-todos, sem a marca "# orq:", fica.
func TestDesconfigurarDeixaTeclaManual(t *testing.T) {
	manual := `[[keys.command]]
key = "prefix+alt+u"
type = "plugin_action"
command = "orq.popup-todos"
description = "Minha Central"
`
	texto := configReal + "\n" + manual
	novo, mud := configurar(t, texto, padrao)
	// A ação já tem tecla (a manual): só a de abrir-central entra.
	if len(mud) != 3 || strings.Count(novo, "orq.popup-todos") != 1 {
		t.Fatalf("mud %v novo =\n%s", mud, novo)
	}
	volta, mud, err := Desconfigurar([]byte(novo))
	if err != nil {
		t.Fatal(err)
	}
	if string(volta) != texto || len(mud) != 3 {
		t.Fatalf("mud %v volta =\n%s", mud, volta)
	}
	s, _ := Ler(volta)
	if s.Teclas[AcaoPopup] != "prefix+alt+u" || s.LinhaAgentes {
		t.Fatalf("situação = %+v", s)
	}
}

func TestTeclaOcupada(t *testing.T) {
	cases := map[string]string{
		"[[keys.command]]\nkey = \"prefix+u\"\ntype = \"popup\"\ncommand = \"lazygit\"\n": "[[keys.command]] lazygit",
		"[keys]\nzoom = \"prefix+u\"\n": "keys.zoom",
		"":                              "keys.zoom (padrão do Herdr)",
	}
	for texto, dono := range cases {
		o := Opcoes{Tecla: "prefix+u"}
		if texto == "" {
			o.Tecla = "prefix+z"
		}
		_, _, err := Configurar([]byte(texto), o)
		var e *Erro
		if !errors.As(err, &e) || e.Codigo != "tecla_ocupada" || !strings.Contains(e.Causa, dono) {
			t.Errorf("%q: err = %v", texto, err)
		}
	}
	// A tecla padrão trocada pelo usuário fica livre.
	if _, _, err := Configurar([]byte("[keys]\nzoom = \"prefix+alt+z\"\n"), Opcoes{Tecla: "prefix+z"}); err != nil {
		t.Fatal(err)
	}
}

func TestDesconfigurarSoTiraOQueEDoOrq(t *testing.T) {
	texto := configReal + `
[[keys.command]]
key = "prefix+alt+g"
type = "popup"
command = "lazygit"
`
	novo, _ := configurar(t, texto, padrao)
	novo = strings.Replace(novo, "[ui]\n", "[ui]\naccent = \"cyan\"\n", 1)
	volta, mud, err := Desconfigurar([]byte(novo))
	if err != nil {
		t.Fatal(err)
	}
	if len(mud) != 4 || !strings.Contains(string(volta), "command = \"lazygit\"") || strings.Contains(string(volta), "orq.") ||
		!strings.Contains(string(volta), `["$story", "$fase"]]`) || !strings.Contains(string(volta), "accent = \"cyan\"") {
		t.Fatalf("mud %v volta =\n%s", mud, volta)
	}
}

func TestAvisoRowsByAgent(t *testing.T) {
	s, err := Ler([]byte("[ui.sidebar.agents.rows_by_agent]\nclaude = [[\"agent\"]]\n"))
	if err != nil || len(s.Avisos) != 1 || !strings.Contains(s.Avisos[0], "claude") {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestConfigInvalidaNaoEditada(t *testing.T) {
	_, _, err := Configurar([]byte("rows = [\n"), padrao)
	var e *Erro
	if !errors.As(err, &e) || e.Codigo != "config_invalida" {
		t.Fatalf("err = %v", err)
	}
}

// checador falso: reprova quando o arquivo contém reprovar.
func checador(reprovar string, chamadas *int) Checador {
	return func(_ context.Context, caminho string) (string, error) {
		*chamadas++
		b, err := os.ReadFile(caminho)
		if err != nil {
			return "", err
		}
		if reprovar != "" && strings.Contains(string(b), reprovar) {
			return "config: issues found", errors.New("exit 1")
		}
		return "config: ok", nil
	}
}

func TestAplicarBackupECheck(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(configReal), 0o600); err != nil {
		t.Fatal(err)
	}
	agora := time.Date(2026, 10, 11, 15, 4, 5, 0, time.UTC)
	n := 0
	ed := func(b []byte) ([]byte, []string, error) { return Configurar(b, padrao) }
	r, err := Aplicar(context.Background(), p, ed, checador("", &n), agora)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || r.Backup == nil || *r.Backup != p+".orq-20261011-150405.bak" || r.Check == nil || *r.Check != "config: ok" {
		t.Fatalf("r = %+v, checks %d", r, n)
	}
	if b, _ := os.ReadFile(*r.Backup); string(b) != configReal {
		t.Fatal("backup diferente do original")
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("modo = %v", st.Mode())
	}
	// Sem mudança: nem backup nem check.
	n = 0
	r, err = Aplicar(context.Background(), p, ed, checador("", &n), agora)
	if err != nil || len(r.Mudancas) != 0 || r.Backup != nil || n != 0 {
		t.Fatalf("idempotente: %+v %v checks %d", r, err, n)
	}
	// Desconfigurar no mesmo segundo: backup com sufixo.
	r, err = Aplicar(context.Background(), p, Desconfigurar, checador("", &n), agora)
	if err != nil || r.Backup == nil || !strings.HasSuffix(*r.Backup, ".orq-20261011-150405-2.bak") {
		t.Fatalf("desconfigurar: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(p); string(b) != configReal {
		t.Fatalf("depois de desconfigurar:\n%s", b)
	}
}

func TestAplicarCheckReprovadoRestaura(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(configReal), 0o644); err != nil {
		t.Fatal(err)
	}
	n := 0
	ed := func(b []byte) ([]byte, []string, error) { return Configurar(b, padrao) }
	r, err := Aplicar(context.Background(), p, ed, checador("orq.popup-todos", &n), time.Now())
	var e *Erro
	if !errors.As(err, &e) || e.Codigo != "config_check_falhou" || !strings.Contains(e.Recuperacao, "restaurado") || !strings.Contains(e.Causa, "issues found") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != configReal {
		t.Fatalf("não restaurou:\n%s", b)
	}
	if r.Backup == nil {
		t.Fatal("sem backup")
	}
	// Arquivo que já reprova não é tocado nem ganha backup.
	n = 0
	_, err = Aplicar(context.Background(), p, ed, checador("onboarding", &n), time.Now())
	if !errors.As(err, &e) || e.Codigo != "config_invalida" || n != 1 {
		t.Fatalf("err = %v, checks %d", err, n)
	}
	// Arquivo novo reprovado é apagado.
	novo := filepath.Join(dir, "sub", "config.toml")
	_, err = Aplicar(context.Background(), novo, ed, checador("orq", &n), time.Now())
	if !errors.As(err, &e) || e.Codigo != "config_check_falhou" {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(novo); !os.IsNotExist(err) {
		t.Fatalf("arquivo novo ficou: %v", err)
	}
}

func TestCaminhoPadrao(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		m    map[string]string
		quer string
	}{
		{map[string]string{"HERDR_CONFIG_PATH": "/x/c.toml", "HOME": "/h"}, "/x/c.toml"},
		{map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/h"}, "/xdg/herdr/config.toml"},
		{map[string]string{"XDG_CONFIG_HOME": "rel", "HOME": "/h"}, "/h/.config/herdr/config.toml"},
	}
	for _, c := range cases {
		if p, _ := CaminhoPadrao(env(c.m)); p != c.quer {
			t.Errorf("%v = %s", c.m, p)
		}
	}
}
