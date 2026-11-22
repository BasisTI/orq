package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	specPR    = "https://github.com/BasisTI/orq/pull/44"
	specPasta = "docs/specs/e/tg-15-x"
	specAprov = "aaaaaaaa11111111"
	specHead  = "bbbbbbbb22222222"
)

// ghFalso instala um gh que responde com o arquivo resp/<argv juntado por _> (stdout, saída 0) e
// sai com 1 sem ele. Devolve a pasta resp.
func ghDaSpec(t *testing.T, env map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	resp := filepath.Join(dir, "resp")
	if err := os.MkdirAll(resp, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nkey=$(printf '%s_' \"$@\" | LC_ALL=C tr -c 'A-Za-z0-9_.-' '_')\nif [ -f '" + resp + "'/\"$key\" ]; then cat '" + resp + "'/\"$key\"; exit 0; fi\necho 'HTTP 500' >&2\nexit 1\n"
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env["ORQ_GH_BIN"] = bin
	return resp
}

func respGH(t *testing.T, resp, saida string, args ...string) {
	t.Helper()
	k := strings.Join(args, "_") + "_"
	k = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-' {
			return r
		}
		return '_'
	}, k)
	if err := os.WriteFile(filepath.Join(resp, k), []byte(saida), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSpecRegistrar(t *testing.T) {
	env, _ := pendEnv(t)
	r := rodar(t, env, "spec", "registrar", "15", "--caminho", specPasta, "--mr", specPR)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	var res struct {
		Spec     struct{ Caminho, MRURL, Estado string }
		MRLigada bool `json:"mr_ligada"`
	}
	_ = json.Unmarshal([]byte(strings.ReplaceAll(strings.ReplaceAll(r.out, `"mr_url"`, `"MRURL"`), `"caminho"`, `"Caminho"`)), &res)
	if res.Spec.Caminho != specPasta || !res.MRLigada {
		t.Fatalf("out = %s", r.out)
	}
	// A MR ficou ligada à US.
	if out := ok(t, env, "onda", "ver", "1"); !strings.Contains(out, "pull/44") {
		t.Fatalf("a MR não foi ligada: %s", out)
	}
	// Duplicidade: recusa, mostra o registro atual; --substituir troca.
	r = rodar(t, env, "spec", "registrar", "15", "--caminho", "docs/outra", "--mr", specPR)
	if r.code != 4 || !strings.Contains(r.err, "spec_ja_registrada") || !strings.Contains(r.err, specPasta) {
		t.Fatalf("duplicidade: exit %d: %s", r.code, r.err)
	}
	if r = rodar(t, env, "spec", "registrar", "15", "--caminho", "docs/outra", "--mr", specPR, "--substituir"); r.code != 0 {
		t.Fatalf("substituir: exit %d: %s", r.code, r.err)
	}
	// Sem --mr, ou com URL que não é MR: erro de uso.
	for _, args := range [][]string{{"spec", "registrar", "15", "--caminho", "x"}, {"spec", "registrar", "15", "--caminho", "x", "--mr", "https://x/issues/1"}} {
		if r := rodar(t, env, args...); r.code != 2 {
			t.Errorf("%v: exit %d, queria 2: %s", args, r.code, r.err)
		}
	}
}

func TestPendAddValidacaoPelaCLI(t *testing.T) {
	env, _ := pendEnv(t)
	resp := ghDaSpec(t, env)
	// Sem spec: erro de uso, e a forja nem é consultada.
	if r := rodar(t, env, "pend", "add", "15", "--tipo", "validacao"); r.code != 2 || !strings.Contains(r.err, "spec registrada") {
		t.Fatalf("sem spec: exit %d: %s", r.code, r.err)
	}
	ok(t, env, "spec", "registrar", "15", "--caminho", specPasta, "--mr", specPR)
	// Forja fora do ar: erro na hora, nada aberto.
	if r := rodar(t, env, "pend", "add", "15", "--tipo", "validacao"); r.code == 0 || !strings.Contains(r.err, "forja_indisponivel") {
		t.Fatalf("forja fora: exit %d: %s", r.code, r.err)
	}
	if out := ok(t, env, "pend", "list"); strings.Contains(out, "validacao") {
		t.Fatalf("nada devia ter sido aberto: %s", out)
	}
	respGH(t, resp, `{"state":"open","head":{"sha":"`+specHead+`","ref":"tg-15"},"base":{"ref":"main"}}`, "api", "repos/BasisTI/orq/pulls/44")
	out := ok(t, env, "pend", "add", "15", "--tipo", "validacao", "texto ignorado")
	if !strings.Contains(out, "TG-15#P1") {
		t.Fatalf("out = %s", out)
	}
	ver := ok(t, env, "pend", "ver", "TG-15#P1")
	if !strings.Contains(ver, "Spec da TG-15 pronta para validar") || !strings.Contains(ver, "Aprovar") || !strings.Contains(ver, "Pedir ajustes") {
		t.Fatalf("ver = %s", ver)
	}
	// Dúvida continua pedindo o texto.
	if r := rodar(t, env, "pend", "add", "15", "--tipo", "duvida"); r.code != 2 {
		t.Fatalf("dúvida sem texto: exit %d", r.code)
	}
}

func TestSpecConferirPelaCLI(t *testing.T) {
	env, _ := pendEnv(t)
	resp := ghDaSpec(t, env)
	// Sem spec: não encontrado.
	if r := rodar(t, env, "spec", "conferir", "15"); r.code != 5 {
		t.Fatalf("sem spec: exit %d: %s", r.code, r.err)
	}
	ok(t, env, "spec", "registrar", "15", "--caminho", specPasta, "--mr", specPR)
	// Sem validação aprovada: diz isso, sem consultar a forja.
	out := ok(t, env, "--output", "text", "spec", "conferir", "15")
	if !strings.Contains(out, "não pode mesclar") || !strings.Contains(out, "validação aprovada") {
		t.Fatalf("sem aprovação: %s", out)
	}
	respGH(t, resp, `{"state":"open","head":{"sha":"`+specAprov+`","ref":"tg-15"},"base":{"ref":"main"}}`, "api", "repos/BasisTI/orq/pulls/44")
	ok(t, env, "pend", "add", "15", "--tipo", "validacao")
	// Em validação ainda não é aprovada.
	if out := ok(t, env, "--output", "text", "spec", "conferir", "15"); !strings.Contains(out, "validação aprovada") {
		t.Fatalf("em validação: %s", out)
	}
	ok(t, env, "pend", "responder", "TG-15#P1", "--opcao", "1")
	// A cabeça é a aprovada: pode mesclar.
	if out := ok(t, env, "--output", "text", "spec", "conferir", "15"); !strings.HasPrefix(out, "pode mesclar") {
		t.Fatalf("cabeça igual: %s", out)
	}
	// A cabeça andou e só o decisoes.md mudou, com um rebase trazendo arquivos de fora.
	respGH(t, resp, `{"state":"open","head":{"sha":"`+specHead+`","ref":"tg-15"},"base":{"ref":"main"}}`, "api", "repos/BasisTI/orq/pulls/44")
	arvore := func(sha, blobDecisoes string) {
		segs := strings.Split(specPasta, "/")
		atual := sha
		for i, seg := range segs {
			prox := "t-" + sha + "-" + strings.Join(segs[:i+1], "-")
			respGH(t, resp, `{"tree":[{"path":"`+seg+`","type":"tree","sha":"`+prox+`"}]}`, "api", "repos/BasisTI/orq/git/trees/"+atual)
			atual = prox
		}
		respGH(t, resp, `{"tree":[{"path":"spec.md","type":"blob","sha":"s1"},{"path":"decisoes.md","type":"blob","sha":"`+blobDecisoes+`"}],"truncated":false}`,
			"api", "repos/BasisTI/orq/git/trees/"+atual+"?recursive=1")
	}
	arvore(specAprov, "d1")
	arvore(specHead, "d2")
	respGH(t, resp, `{"files":[{"filename":"internal/x.go"},{"filename":"`+specPasta+`/decisoes.md"}]}`, "api", "repos/BasisTI/orq/compare/"+specAprov+"..."+specHead+"?per_page=100&page=1")
	out = ok(t, env, "--output", "text", "spec", "conferir", "15")
	if !strings.HasPrefix(out, "pode mesclar") || !strings.Contains(out, "a MR não é só da spec") || !strings.Contains(out, "internal/x.go") {
		t.Fatalf("rebase: %s", out)
	}
	// Mudou o spec.md: lista o arquivo.
	arvore(specHead, "d2")
	respGH(t, resp, `{"tree":[{"path":"spec.md","type":"blob","sha":"s2"},{"path":"decisoes.md","type":"blob","sha":"d2"}],"truncated":false}`,
		"api", "repos/BasisTI/orq/git/trees/t-"+specHead+"-"+strings.ReplaceAll(specPasta, "/", "-")+"?recursive=1")
	out = ok(t, env, "--output", "text", "spec", "conferir", "15")
	if !strings.HasPrefix(out, "não pode mesclar") || !strings.Contains(out, "spec.md") {
		t.Fatalf("mudou: %s", out)
	}
	// JSON.
	var res SpecConferida
	if err := json.Unmarshal([]byte(ok(t, env, "--output", "json", "spec", "conferir", "15")), &res); err != nil || res.PodeMesclar || !res.Aprovada || res.Conferencia.MudouNaPasta[0] != "spec.md" {
		t.Fatalf("json: %+v, %v", res, err)
	}
	// Forja fora do ar: erro.
	respGH(t, resp, "", "api", "repos/BasisTI/orq/pulls/44")
	os.Remove(filepath.Join(resp, "api_repos_BasisTI_orq_pulls_44_"))
	if r := rodar(t, env, "spec", "conferir", "15"); r.code == 0 || !strings.Contains(r.err, "forja_indisponivel") {
		t.Fatalf("forja fora: exit %d: %s", r.code, r.err)
	}
}
