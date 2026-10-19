package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A regra de `herdr plugin config-dir orq` no Herdr 0.9.3, sem chamar o Herdr.
func TestDirConfigPlugin(t *testing.T) {
	cases := []struct {
		nome string
		env  map[string]string
		quer string
	}{
		{"definido pelo Herdr", map[string]string{"HERDR_PLUGIN_CONFIG_DIR": "/c/orq", "XDG_CONFIG_HOME": "/xdg", "HOME": "/h"}, "/c/orq"},
		{"XDG", map[string]string{"XDG_CONFIG_HOME": "/xdg", "HOME": "/h"}, "/xdg/herdr/plugins/config/orq"},
		{"HOME", map[string]string{"HOME": "/h"}, "/h/.config/herdr/plugins/config/orq"},
		{"HERDR_CONFIG_PATH não conta", map[string]string{"HERDR_CONFIG_PATH": "/x/c.toml", "HOME": "/h"}, "/h/.config/herdr/plugins/config/orq"},
		{"XDG vazio", map[string]string{"XDG_CONFIG_HOME": "", "HOME": "/h"}, "/h/.config/herdr/plugins/config/orq"},
		{"XDG relativo", map[string]string{"XDG_CONFIG_HOME": "rel", "HOME": "/h"}, "/h/.config/herdr/plugins/config/orq"},
		{"sem nada", map[string]string{}, ""},
	}
	for _, c := range cases {
		if got := DirConfigPlugin(func(k string) string { return c.env[k] }); got != c.quer {
			t.Errorf("%s: %q; quer %q", c.nome, got, c.quer)
		}
	}
}

// O orq.env é lido sem HERDR_PLUGIN_CONFIG_DIR, pelo HOME e pelo XDG_CONFIG_HOME, e o ambiente
// do processo vence o arquivo.
func TestAmbientePluginSemHerdrPluginConfigDir(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	escreverMkdir(t, filepath.Join(home, ".config", "herdr", "plugins", "config", "orq", ArquivoAmbientePlugin), "ORQ_GLAB_BIN=/do/home\nORQ_GH_BIN=/gh/do/home\nHOME=/nao\n")
	escreverMkdir(t, filepath.Join(xdg, "herdr", "plugins", "config", "orq", ArquivoAmbientePlugin), "ORQ_GLAB_BIN=/do/xdg\n")

	porHome := comAmbientePlugin(func(k string) string { return map[string]string{"HOME": home}[k] })
	if porHome("ORQ_GLAB_BIN") != "/do/home" || porHome("HOME") != home {
		t.Fatalf("HOME: GLAB=%q HOME=%q", porHome("ORQ_GLAB_BIN"), porHome("HOME"))
	}
	porXDG := comAmbientePlugin(func(k string) string { return map[string]string{"HOME": home, "XDG_CONFIG_HOME": xdg}[k] })
	if porXDG("ORQ_GLAB_BIN") != "/do/xdg" || porXDG("ORQ_GH_BIN") != "" {
		t.Fatalf("XDG: GLAB=%q GH=%q", porXDG("ORQ_GLAB_BIN"), porXDG("ORQ_GH_BIN"))
	}
	venceProcesso := comAmbientePlugin(func(k string) string {
		return map[string]string{"HOME": home, "ORQ_GLAB_BIN": "/do/processo"}[k]
	})
	if venceProcesso("ORQ_GLAB_BIN") != "/do/processo" || venceProcesso("ORQ_GH_BIN") != "/gh/do/home" {
		t.Fatalf("processo: GLAB=%q GH=%q", venceProcesso("ORQ_GLAB_BIN"), venceProcesso("ORQ_GH_BIN"))
	}
	// HERDR_PLUGIN_CONFIG_DIR continua mandando.
	outro := t.TempDir()
	escrever(t, filepath.Join(outro, ArquivoAmbientePlugin), "ORQ_GLAB_BIN=/do/herdr\n")
	if got := comAmbientePlugin(func(k string) string { return map[string]string{"HOME": home, "HERDR_PLUGIN_CONFIG_DIR": outro}[k] })("ORQ_GLAB_BIN"); got != "/do/herdr" {
		t.Fatalf("HERDR_PLUGIN_CONFIG_DIR: %q", got)
	}
}

func escreverMkdir(t *testing.T, caminho, conteudo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err != nil {
		t.Fatal(err)
	}
	escrever(t, caminho, conteudo)
}

func scriptFalso(t *testing.T, caminho, corpo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caminho, []byte("#!/bin/sh\n"+corpo), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Evidência da story: sem nenhuma variável *_BIN, `orq limpar --dry-run` acha o glab falso em
// ~/.nix-profile/bin, que vem antes de linuxbrew e /usr/bin (o glab real da máquina, se houver).
func TestLimparDryRunAchaGlabNoNixProfile(t *testing.T) {
	env := ambiente(t)
	env["ORQ_TRABALHO_DIR"] = filepath.Join(t.TempDir(), "trabalho")
	env["ORQ_TMP_DIR"] = filepath.Join(t.TempDir(), "tmp")
	env["HOME"] = t.TempDir()
	env["USER"] = "ana"
	mr := "https://gitlab.exemplo.test/basis/proj/-/merge_requests/5"
	ok(t, env, "coord", "registrar", "c", "--projeto", "orq")
	ok(t, env, "onda", "criar", "o", "--us", "13", "--ate", "merge")
	ok(t, env, "mr", "add", "13", mr)
	ok(t, env, "recursos", "gravar", "13", "--pane", "tg-13_executor")

	log := filepath.Join(t.TempDir(), "glab.log")
	scriptFalso(t, filepath.Join(env["HOME"], ".nix-profile", "bin", "glab"),
		"echo \"$@\" >> '"+log+"'\necho '{\"state\":\"merged\",\"merged_at\":\"2026-10-19T10:00:00Z\",\"web_url\":\""+mr+"\",\"sha\":\"abc1234\",\"source_branch\":\"TG-13\",\"target_branch\":\"main\"}'\n")
	r := rodar(t, env, "limpar", "13", "--dry-run")
	if strings.Contains(r.err+r.out, "binario_ausente") {
		t.Fatalf("não achou o glab: exit %d\nout %s\nerr %s", r.code, r.out, r.err)
	}
	b, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(b), "api --hostname gitlab.exemplo.test projects/basis%2Fproj/merge_requests/5") {
		t.Fatalf("glab falso não foi chamado como esperado: %q, %v\nexit %d\nout %s\nerr %s", b, err, r.code, r.out, r.err)
	}
}

// O doctor diz de onde veio cada caminho e se o daemon e o shell resolvem o mesmo.
func TestDoctorOrigemEDaemonEShell(t *testing.T) {
	dir, env := novoHerdrCentral(t)
	home := t.TempDir()
	env["HOME"] = home
	env["USER"] = "ana"
	delete(env, "HERDR_PLUGIN_CONFIG_DIR")
	cfg := filepath.Join(dir, "config.toml")
	escrever(t, filepath.Join(dir, "plugins.json"), `{"result":{"plugins":[{"plugin_id":"orq","enabled":true,"plugin_root":"/x/orq"}]}}`)
	ok(t, env, "plugin", "configurar", "--config", cfg)

	nix := filepath.Join(home, ".nix-profile", "bin", "glab")
	scriptFalso(t, nix, "exit 0\n")
	porVariavel := filepath.Join(t.TempDir(), "gh")
	scriptFalso(t, porVariavel, "exit 0\n")
	porArquivo := filepath.Join(t.TempDir(), "taiga")
	scriptFalso(t, porArquivo, "exit 0\n")
	escreverMkdir(t, filepath.Join(home, ".config", "herdr", "plugins", "config", "orq", ArquivoAmbientePlugin), "ORQ_TAIGA_BIN="+porArquivo+"\n")
	delete(env, "ORQ_GLAB_BIN")
	env["ORQ_GH_BIN"] = porVariavel

	lerDoctor := func() map[string]Item {
		var d Doctor
		if err := json.Unmarshal([]byte(ok(t, env, "plugin", "doctor", "--config", cfg)), &d); err != nil {
			t.Fatal(err)
		}
		m := map[string]Item{}
		for _, it := range d.Itens {
			m[it.Nome] = it
		}
		return m
	}
	m := lerDoctor()
	for nome, quer := range map[string]Item{
		"binario glab":  {Detalhe: nix, Origem: "diretorio"},
		"binario gh":    {Detalhe: porVariavel, Origem: "variavel"},
		"binario taiga": {Detalhe: porArquivo, Origem: "orq.env"},
	} {
		if got := m[nome]; got.Detalhe != quer.Detalhe || got.Origem != quer.Origem {
			t.Errorf("%s = %+v; quer %+v", nome, got, quer)
		}
	}
	if it := m["orq.env"]; it.Estado != "ok" || !strings.Contains(it.Detalhe, "1 chaves ORQ_") {
		t.Errorf("orq.env = %+v", it)
	}
	// O gh (e o herdr falso do teste) só existem em variáveis do shell: o daemon não as vê, e o doctor avisa.
	if it := m["daemon e shell"]; it.Estado != "aviso" || !strings.Contains(it.Detalhe, "gh: daemon") || strings.Contains(it.Detalhe, "taiga:") {
		t.Errorf("daemon e shell = %+v", it)
	}
	// Com a variável no orq.env em vez do shell, os dois concordam.
	herdrFalso := env["ORQ_HERDR_BIN"]
	delete(env, "ORQ_GH_BIN")
	delete(env, "ORQ_HERDR_BIN")
	escreverMkdir(t, filepath.Join(home, ".config", "herdr", "plugins", "config", "orq", ArquivoAmbientePlugin),
		"ORQ_TAIGA_BIN="+porArquivo+"\nORQ_GH_BIN="+porVariavel+"\nORQ_HERDR_BIN="+herdrFalso+"\n")
	if it := lerDoctor()["daemon e shell"]; it.Estado != "ok" {
		t.Errorf("daemon e shell = %+v", it)
	}
}
