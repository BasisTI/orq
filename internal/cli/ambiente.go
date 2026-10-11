package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ArquivoAmbientePlugin é o arquivo, no diretório de configuração do plugin
// ($HERDR_PLUGIN_CONFIG_DIR, o de `herdr plugin config-dir orq`), com variáveis ORQ_* para os
// comandos que o Herdr dispara. As ações, o startup e os panes do plugin rodam com o ambiente
// do servidor do Herdr, não com o do shell; sem este arquivo, não há como apontar
// ORQ_STATE_DIR ou ORQ_TAIGA_BIN para eles.
const ArquivoAmbientePlugin = "orq.env"

// comAmbientePlugin devolve env completado pelo arquivo do plugin. O arquivo só entra quando
// HERDR_PLUGIN_CONFIG_DIR está definido, só fornece variáveis ORQ_* e não sobrescreve o que o
// processo já tem: o ambiente explícito vale mais que o arquivo.
func comAmbientePlugin(env func(string) string) func(string) string {
	dir := env("HERDR_PLUGIN_CONFIG_DIR")
	if dir == "" {
		return env
	}
	extra := lerAmbientePlugin(filepath.Join(dir, ArquivoAmbientePlugin))
	if len(extra) == 0 {
		return env
	}
	return func(k string) string {
		if v := env(k); v != "" {
			return v
		}
		return extra[k]
	}
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
