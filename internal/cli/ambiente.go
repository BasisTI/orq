package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ArquivoAmbientePlugin é o arquivo, no diretório de configuração do plugin
// ($HERDR_PLUGIN_CONFIG_DIR, o de `herdr plugin config-dir orq`), com variáveis ORQ_* para o
// daemon, as ações e os panes do plugin e para os comandos do terminal. As ações, o startup e
// os panes rodam com o ambiente do servidor do Herdr, não com o do shell; sem este arquivo, não
// há como apontar ORQ_STATE_DIR ou ORQ_TAIGA_BIN para eles.
const ArquivoAmbientePlugin = "orq.env"

// DirConfigPlugin é o diretório de configuração do plugin orq. Com HERDR_PLUGIN_CONFIG_DIR
// (o Herdr o define para o que dispara), vale ele. Sem ele, a regra do Herdr 0.9.3 para
// `herdr plugin config-dir orq`, sem chamar o Herdr: $XDG_CONFIG_HOME/herdr/plugins/config/orq
// e, sem XDG_CONFIG_HOME, $HOME/.config/herdr/plugins/config/orq. HERDR_CONFIG_PATH não conta:
// ele só move o config.toml. O Herdr aceita um XDG_CONFIG_HOME relativo ou vazio e então
// devolve um caminho relativo ao diretório corrente; aqui eles são ignorados (vale o HOME),
// porque o resultado dependeria de onde o comando roda. Sem HOME, devolve "".
func DirConfigPlugin(env func(string) string) string {
	if dir := env("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		return dir
	}
	if x := env("XDG_CONFIG_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "herdr", "plugins", "config", IDPlugin)
	}
	if home := env("HOME"); home != "" {
		return filepath.Join(home, ".config", "herdr", "plugins", "config", IDPlugin)
	}
	return ""
}

// comAmbientePlugin devolve env completado pelo orq.env de DirConfigPlugin. O arquivo só
// fornece variáveis ORQ_* e não sobrescreve o que o processo já tem: o ambiente explícito vale
// mais que o arquivo.
func comAmbientePlugin(env func(string) string) func(string) string {
	e, _ := ambientePlugin(env)
	return e
}

// ambientePlugin é comAmbientePlugin que também devolve as chaves lidas do arquivo.
func ambientePlugin(env func(string) string) (func(string) string, map[string]string) {
	dir := DirConfigPlugin(env)
	if dir == "" {
		return env, nil
	}
	extra := lerAmbientePlugin(filepath.Join(dir, ArquivoAmbientePlugin))
	if len(extra) == 0 {
		return env, nil
	}
	return func(k string) string {
		if v := env(k); v != "" {
			return v
		}
		return extra[k]
	}, extra
}

// lerAmbientePlugin lê linhas CHAVE=valor; linha vazia e # são ignoradas, como chave que não
// começa com ORQ_. Aspas simples ou duplas em volta do valor são tiradas. Não há expansão.
// Arquivo ausente ou ilegível é o mesmo que vazio.
func lerAmbientePlugin(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		l = strings.TrimPrefix(l, "export ")
		k, v, ok := strings.Cut(l, "=")
		k = strings.TrimSpace(k)
		if !ok || !strings.HasPrefix(k, "ORQ_") {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		m[k] = v
	}
	return m
}
