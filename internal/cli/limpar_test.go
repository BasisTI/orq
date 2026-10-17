package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/limpeza"
)

// ghFalso devolve, para qualquer chamada, a MR do JSON dado.
func ghFalso(t *testing.T, json string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\ncat <<'X'\n"+json+"\nX\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRecursosGravarELimparRecusaComMRAberta(t *testing.T) {
	env := ambiente(t)
	env["ORQ_TRABALHO_DIR"] = filepath.Join(t.TempDir(), "trabalho")
	env["ORQ_TMP_DIR"] = filepath.Join(t.TempDir(), "tmp")
	env["HOME"] = t.TempDir()
	env["TESTCONTAINERS_RYUK_DISABLED"] = "true"
	env["ORQ_GH_BIN"] = ghFalso(t, `{"state":"open","merged":false,"draft":false,"mergeable_state":"clean","head":{"sha":"abc1234","ref":"TG-13"},"base":{"ref":"main"}}`)
	ok(t, env, "coord", "registrar", "c", "--projeto", "orq")
	ok(t, env, "onda", "criar", "o", "--us", "13", "--ate", "merge")
	ok(t, env, "mr", "add", "13", "https://github.com/BasisTI/orq/pull/9")

	repo, wt := t.TempDir(), filepath.Join(t.TempDir(), "wt")
	var g RecursosGravados
	saida := ok(t, env, "recursos", "gravar", "13", "--repo", repo, "--worktree", wt, "--branch", "TG-13", "--pane", "tg-13_executor", "--pane", "tg-13_revisor")
	if err := json.Unmarshal([]byte(saida), &g); err != nil {
		t.Fatal(err)
	}
	if g.Env["TMPDIR"] != filepath.Join(env["ORQ_TMP_DIR"], "orq", "TG-13") || g.Rotulos["orq.us"] != "orq/13" || g.Rotulos["orq.onda"] != "1" {
		t.Fatalf("saída = %+v", g)
	}
	if len(g.Avisos) != 1 || !strings.Contains(g.Avisos[0], "RYUK") {
		t.Fatalf("avisos = %v", g.Avisos)
	}
	if _, err := os.Stat(filepath.Join(env["ORQ_TRABALHO_DIR"], "orq", "TG-13", limpeza.ArquivoRecursos)); err != nil {
		t.Fatal(err)
	}
	// Completar o arquivo não perde o que já estava.
	ok(t, env, "recursos", "gravar", "13", "--pane", "tg-13_extra", "--aprovado", "https://github.com/BasisTI/orq/pull/9=abc1234")
	if err := json.Unmarshal([]byte(ok(t, env, "recursos", "ver", "13")), &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Recursos.Repos) != 1 || len(g.Recursos.Panes) != 3 || g.Recursos.Aprovados["https://github.com/BasisTI/orq/pull/9"] != "abc1234" {
		t.Fatalf("recursos = %+v", g.Recursos)
	}
	// Reabrir sobe a geração e guarda as MRs de agora como encerradas.
	if err := json.Unmarshal([]byte(ok(t, env, "recursos", "reabrir", "13")), &g); err != nil {
		t.Fatal(err)
	}
	if g.Recursos.Geracao != 1 || len(g.Recursos.MRsEncerradas) != 1 || len(g.Recursos.Panes) != 3 {
		t.Fatalf("reabrir = %+v", g.Recursos)
	}
	if r := rodar(t, env, "recursos", "gravar", "13", "--repo", repo, "--worktree", wt, "--branch", "TG-13-r2"); r.code != 0 || !strings.Contains(r.out, "caminho novo") {
		t.Fatalf("reuso do caminho do worktree: %d %s", r.code, r.out)
	}
	if r := rodar(t, env, "recursos", "gravar", "13", "--repo", repo); r.code != 2 {
		t.Fatalf("--repo sem --worktree: exit %d", r.code)
	}

	// MR aberta: recusa com exit 4, nada removido, e o dry-run também.
	for _, args := range [][]string{{"limpar", "13"}, {"limpar", "13", "--dry-run"}} {
		r := rodar(t, env, args...)
		if r.code != 4 || !strings.Contains(r.err, "limpeza_recusada") || !strings.Contains(r.out, "reaberta") {
			t.Fatalf("%v: exit %d\nout %s\nerr %s", args, r.code, r.out, r.err)
		}
	}
	if _, err := os.Stat(filepath.Join(env["ORQ_TRABALHO_DIR"], "orq", "TG-13", limpeza.ArquivoRecursos)); err != nil {
		t.Fatal("a recusa mexeu na pasta de trabalho")
	}
	diario := ok(t, env, "diario", "--us", "13")
	for _, w := range []string{"recursos.gravar", "limpar.recusada"} {
		if !strings.Contains(diario, w) {
			t.Errorf("diário sem %s", w)
		}
	}
	if r := rodar(t, env, "limpar", "13", "--onda", "1"); r.code != 2 {
		t.Fatalf("ref e --onda juntos: exit %d", r.code)
	}
	if r := rodar(t, env, "limpar", "--onda", "1"); r.code != 4 || !strings.Contains(r.err, "onda_aberta") {
		t.Fatalf("--onda aberta: exit %d %s", r.code, r.err)
	}
}
