package cli

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var isoVisivel = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// As saídas em texto não mostram data ISO nem instante UTC: o JSON fora de
// terminal é que guarda RFC 3339.
func TestSaidasEmTextoSemDataISO(t *testing.T) {
	env, pasta := pendEnv(t)
	arq := filepath.Join(pasta, "decisao-yaml.md")
	escrever(t, arq, arquivoDecisao)
	ok(t, env, "pend", "importar", arq)
	ok(t, env, "pend", "responder", "TG-15#P1", "--opcao", "2")
	ok(t, env, "pend", "responder", "TG-15#P1", "--opcao", "1") // gera anterior.em no diário
	ok(t, env, "pend", "resolver", "TG-15#P1")

	telas := map[string][]string{
		"pend ver":     {"pend", "ver", "orq/15#P1"},
		"pend list":    {"pend", "list"},
		"onda list":    {"onda", "list"},
		"onda ver":     {"onda", "ver", "1"},
		"diario":       {"diario"},
		"coord list":   {"coord", "list"},
		"onda proxima": {"onda", "proxima"},
	}
	for nome, args := range telas {
		r := rodar(t, env, append([]string{"--output", "text"}, args...)...)
		if loc := isoVisivel.FindString(r.out); loc != "" {
			t.Errorf("%s mostra data ISO %q:\n%s", nome, loc, r.out)
		}
	}
	// O diário em texto tem o instante no formato de leitura, inclusive o anterior.em.
	d := ok(t, env, "--output", "text", "diario")
	if !regexp.MustCompile(`\d{2}/\d{2}/\d{4} \d{2}:\d{2}`).MatchString(d) || !strings.Contains(d, "pend.responder") {
		t.Fatalf("diario sem DD/MM/YYYY HH:MM:\n%s", d)
	}
}
