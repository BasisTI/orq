package enriquecer

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

const (
	prSpec     = "https://github.com/BasisTI/orq/pull/44"
	pastaSpec  = "docs/specs/epico/tg-59-x"
	apiPR      = "repos/BasisTI/orq/pulls/44"
	shaAprov   = "aaaaaaaa11111111"
	shaCabeca  = "bbbbbbbb22222222"
	shaRebased = "cccccccc33333333"
)

func js(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// cabecaGH responde a leitura da MR com a cabeça dada.
func cabecaGH(f *falsos, sha string) {
	f.responder("gh", js(map[string]any{"state": "open", "head": map[string]any{"sha": sha, "ref": "tg-59"}, "base": map[string]any{"ref": "main"}}), false, "api", apiPR)
}

// pastaGH registra a árvore do commit sha: a cadeia docs/specs/epico/tg-59-x, uma árvore por nível,
// e a árvore recursiva da pasta com os arquivos (caminho relativo → blob).
func pastaGH(f *falsos, sha string, arquivos map[string]string) {
	segs := strings.Split(pastaSpec, "/")
	atual := sha
	for i, seg := range segs {
		prox := "tree-" + sha + "-" + strings.Join(segs[:i+1], "-")
		f.responder("gh", js(map[string]any{"tree": []map[string]any{{"path": "outra", "type": "tree", "sha": "x"}, {"path": seg, "type": "tree", "sha": prox}}}), false,
			"api", "repos/BasisTI/orq/git/trees/"+atual)
		atual = prox
	}
	var es []map[string]any
	for p, id := range arquivos {
		es = append(es, map[string]any{"path": p, "type": "blob", "sha": id})
	}
	es = append(es, map[string]any{"path": "sub", "type": "tree", "sha": "z"})
	f.responder("gh", js(map[string]any{"tree": es, "truncated": false}), false, "api", "repos/BasisTI/orq/git/trees/"+atual+"?recursive=1")
}

func compareGH(f *falsos, de, para string, arquivos ...string) {
	var fs []map[string]any
	for _, a := range arquivos {
		fs = append(fs, map[string]any{"filename": a})
	}
	f.responder("gh", js(map[string]any{"files": fs}), false, "api", "repos/BasisTI/orq/compare/"+de+"..."+para+"?per_page=100&page=1")
}

var spec3 = map[string]string{"spec.md": "b1", "plano.md": "b2", "decisoes.md": "b3"}

func com(m map[string]string, kv ...string) map[string]string {
	o := map[string]string{}
	for k, v := range m {
		o[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		if kv[i+1] == "" {
			delete(o, kv[i])
		} else {
			o[kv[i]] = kv[i+1]
		}
	}
	return o
}

func TestCompararPastas(t *testing.T) {
	for _, c := range []struct {
		nome      string
		atual     map[string]string
		quer      string
		descricao string
	}{
		{"igual", spec3, "", ""},
		{"só decisoes.md muda", com(spec3, "decisoes.md", "novo"), "", ""},
		{"decisoes.md some", com(spec3, "decisoes.md", ""), "", ""},
		{"spec.md muda", com(spec3, "spec.md", "novo"), "spec.md", ""},
		{"arquivo novo", com(spec3, "extra.md", "e"), "extra.md", ""},
		{"arquivo removido", com(spec3, "plano.md", ""), "plano.md", ""},
		{"vários", com(spec3, "plano.md", "", "spec.md", "x", "decisoes.md", "y"), "plano.md,spec.md", ""},
	} {
		if got := strings.Join(CompararPastas(spec3, c.atual), ","); got != c.quer {
			t.Errorf("%s: %q, queria %q", c.nome, got, c.quer)
		}
	}
}

func TestForaDaPasta(t *testing.T) {
	got := ForaDaPasta([]string{pastaSpec + "/spec.md", "internal/x.go", pastaSpec, pastaSpec + "-outra/a.md", "docs/specs/epico/README.md"}, pastaSpec+"/")
	if strings.Join(got, ",") != "internal/x.go,"+pastaSpec+"-outra/a.md,docs/specs/epico/README.md" {
		t.Fatalf("fora = %v", got)
	}
}

func TestConferirSpecSoODecisoesMudou(t *testing.T) {
	f := novosFalsos(t)
	cabecaGH(f, shaCabeca)
	pastaGH(f, shaAprov, spec3)
	pastaGH(f, shaCabeca, com(spec3, "decisoes.md", "b3-novo"))
	compareGH(f, shaAprov, shaCabeca, pastaSpec+"/decisoes.md")
	c, err := f.ferramentas().ConferirSpec(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil {
		t.Fatal(err)
	}
	if !c.PodeMesclar || len(c.MudouNaPasta) != 0 || len(c.ForaDaPasta) != 0 || c.SHACabeca != shaCabeca || c.SHAAprovado != shaAprov {
		t.Fatalf("c = %+v", c)
	}
}

func TestConferirSpecOutroArquivoDaPasta(t *testing.T) {
	f := novosFalsos(t)
	cabecaGH(f, shaCabeca)
	pastaGH(f, shaAprov, spec3)
	pastaGH(f, shaCabeca, com(spec3, "spec.md", "b1-novo", "decisoes.md", "b3-novo"))
	compareGH(f, shaAprov, shaCabeca, pastaSpec+"/spec.md", pastaSpec+"/decisoes.md")
	c, err := f.ferramentas().ConferirSpec(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil {
		t.Fatal(err)
	}
	if c.PodeMesclar || strings.Join(c.MudouNaPasta, ",") != "spec.md" {
		t.Fatalf("c = %+v", c)
	}
}

// D17: depois de um rebase (ou do merge da base na branch da spec), o compare da forja traz
// arquivos de fora da pasta. O conteúdo da pasta é o mesmo: a spec não é reprovada, e o que veio
// de fora é só o outro sinal.
func TestConferirSpecRebaseNaoReprovaAEspec(t *testing.T) {
	f := novosFalsos(t)
	cabecaGH(f, shaRebased)
	pastaGH(f, shaAprov, spec3)
	// Os commits são outros; o conteúdo da pasta (os blobs) é idêntico, e o decisoes.md mudou.
	pastaGH(f, shaRebased, com(spec3, "decisoes.md", "b3-novo"))
	compareGH(f, shaAprov, shaRebased, "internal/central/mr.go", "internal/web/tmpl/central.html", pastaSpec+"/decisoes.md", "go.mod")
	c, err := f.ferramentas().ConferirSpec(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil {
		t.Fatal(err)
	}
	if !c.PodeMesclar || len(c.MudouNaPasta) != 0 {
		t.Fatalf("rebase reprovou a spec: %+v", c)
	}
	if strings.Join(c.ForaDaPasta, ",") != "go.mod,internal/central/mr.go,internal/web/tmpl/central.html" {
		t.Fatalf("fora da pasta = %v", c.ForaDaPasta)
	}
}

// O merge da base na branch traz arquivos novos de fora e a mesma pasta; e, com o merge, um arquivo
// da pasta que mudou na base (impossível sem querer, mas conta como mudança na pasta).
func TestConferirSpecMergeDaBaseMexendoNaPasta(t *testing.T) {
	f := novosFalsos(t)
	cabecaGH(f, shaRebased)
	pastaGH(f, shaAprov, spec3)
	pastaGH(f, shaRebased, com(spec3, "plano.md", "b2-da-base"))
	compareGH(f, shaAprov, shaRebased, "README.md", pastaSpec+"/plano.md")
	c, err := f.ferramentas().ConferirSpec(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil {
		t.Fatal(err)
	}
	if c.PodeMesclar || strings.Join(c.MudouNaPasta, ",") != "plano.md" || strings.Join(c.ForaDaPasta, ",") != "README.md" {
		t.Fatalf("c = %+v", c)
	}
}

func TestConferirSpecArquivoNovoERemovidoNaPasta(t *testing.T) {
	f := novosFalsos(t)
	cabecaGH(f, shaCabeca)
	pastaGH(f, shaAprov, spec3)
	pastaGH(f, shaCabeca, com(spec3, "plano.md", "", "extra.md", "e1"))
	compareGH(f, shaAprov, shaCabeca)
	c, err := f.ferramentas().ConferirSpec(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil || c.PodeMesclar || strings.Join(c.MudouNaPasta, ",") != "extra.md,plano.md" {
		t.Fatalf("c = %+v, %v", c, err)
	}
}

func TestConferirSpecCabecaIgualNaoLeArvores(t *testing.T) {
	f := novosFalsos(t)
	cabecaGH(f, shaAprov)
	c, err := f.ferramentas().ConferirSpec(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil || !c.PodeMesclar {
		t.Fatalf("c = %+v, %v", c, err)
	}
	if n := len(f.chamadas("gh")); n != 1 {
		t.Fatalf("gh chamado %d vezes, queria só a leitura da MR", n)
	}
}

func TestConferirSpecCompareFalhandoNaoDerrubaAConferenciaDaPasta(t *testing.T) {
	f := novosFalsos(t)
	cabecaGH(f, shaCabeca)
	pastaGH(f, shaAprov, spec3)
	pastaGH(f, shaCabeca, com(spec3, "decisoes.md", "x"))
	f.responder("gh", "HTTP 500", true, "api", "repos/BasisTI/orq/compare/"+shaAprov+"..."+shaCabeca+"?per_page=100&page=1")
	c, err := f.ferramentas().ConferirSpec(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil || !c.PodeMesclar || c.ForaDaPastaErro == "" {
		t.Fatalf("c = %+v, %v", c, err)
	}
}

func TestConferirSpecFalhaDaForjaNaPasta(t *testing.T) {
	f := novosFalsos(t)
	cabecaGH(f, shaCabeca)
	f.responder("gh", "HTTP 502", true, "api", "repos/BasisTI/orq/git/trees/"+shaAprov)
	if _, err := f.ferramentas().ConferirSpec(context.Background(), prSpec, pastaSpec, shaAprov); err == nil {
		t.Fatal("a falha da forja na árvore tem de voltar como erro")
	}
	// Pasta que não existe no commit aprovado: lista vazia, e o que há agora é arquivo novo.
	f = novosFalsos(t)
	cabecaGH(f, shaCabeca)
	f.responder("gh", js(map[string]any{"tree": []map[string]any{{"path": "outra", "type": "tree", "sha": "x"}}}), false, "api", "repos/BasisTI/orq/git/trees/"+shaAprov)
	pastaGH(f, shaCabeca, spec3)
	compareGH(f, shaAprov, shaCabeca)
	c, err := f.ferramentas().ConferirSpec(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil || c.PodeMesclar || len(c.MudouNaPasta) != 2 {
		t.Fatalf("c = %+v, %v", c, err)
	}
}

func TestConferirSpecGitLab(t *testing.T) {
	f := novosFalsos(t)
	mr := "https://gitlab.basis.com.br/basis/orq/-/merge_requests/9"
	proj := url.QueryEscape("basis/orq")
	f.responder("glab", js(map[string]any{"state": "opened", "sha": shaRebased, "source_branch": "tg-59", "target_branch": "main"}), false,
		"api", "--hostname", "gitlab.basis.com.br", "projects/"+proj+"/merge_requests/9")
	arvore := func(sha string, arquivos map[string]string) {
		var es []map[string]any
		for p, id := range arquivos {
			es = append(es, map[string]any{"id": id, "path": pastaSpec + "/" + p, "type": "blob"})
		}
		es = append(es, map[string]any{"id": "t", "path": pastaSpec + "/sub", "type": "tree"})
		f.responder("glab", js(es), false, "api", "--hostname", "gitlab.basis.com.br",
			"projects/"+proj+"/repository/tree?path="+url.QueryEscape(pastaSpec)+"&ref="+sha+"&recursive=true&per_page=100&page=1")
	}
	arvore(shaAprov, spec3)
	arvore(shaRebased, com(spec3, "decisoes.md", "novo"))
	f.responder("glab", js(map[string]any{"diffs": []map[string]any{{"old_path": "go.mod", "new_path": "go.mod"}, {"old_path": pastaSpec + "/decisoes.md", "new_path": pastaSpec + "/decisoes.md"}}}), false,
		"api", "--hostname", "gitlab.basis.com.br", "projects/"+proj+"/repository/compare?from="+shaAprov+"&to="+shaRebased+"&straight=true")
	c, err := f.ferramentas().ConferirSpec(context.Background(), mr, pastaSpec, shaAprov)
	if err != nil || !c.PodeMesclar || strings.Join(c.ForaDaPasta, ",") != "go.mod" {
		t.Fatalf("c = %+v, %v", c, err)
	}
	// Pasta ausente no GitLab (404): vazia.
	f.responder("glab", "404 Not Found", true, "api", "--hostname", "gitlab.basis.com.br",
		"projects/"+proj+"/repository/tree?path="+url.QueryEscape(pastaSpec)+"&ref="+shaAprov+"&recursive=true&per_page=100&page=1")
	c, err = f.ferramentas().ConferirSpec(context.Background(), mr, pastaSpec, shaAprov)
	if err != nil || c.PodeMesclar {
		t.Fatalf("pasta ausente: %+v, %v", c, err)
	}
}

func TestCicloDaConferenciaDasSpecs(t *testing.T) {
	ctx := context.Background()
	c := novoCenario(t)
	co := c.coord("alfa", "wA:p1", "")
	if _, err := c.st.CriarOnda(ctx, co, central.NovaOnda{Titulo: "o", US: []refs.US{{Projeto: "orq", Ref: 59}}}); err != nil {
		t.Fatal(err)
	}
	u := refs.US{Projeto: "orq", Ref: 59}
	if _, err := c.st.RegistrarSpec(ctx, &co, u, pastaSpec, prSpec, false); err != nil {
		t.Fatal(err)
	}
	var logs []string
	svc := &ConferenciaSpecs{E: c.e, Log: func(f string, a ...any) { logs = append(logs, f) }}
	if err := svc.Ciclo(ctx); err != nil || len(c.chamadas("gh")) != 0 {
		t.Fatal("spec sem aprovação não é conferida", err)
	}
	p, err := c.st.AbrirValidacao(ctx, &co, u, shaAprov, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Ciclo(ctx); err != nil || len(c.chamadas("gh")) != 0 {
		t.Fatal("spec em validação não é conferida", err)
	}
	opcao := central.OpcaoAprovar
	if _, err := c.st.ResponderPendencia(ctx, &co, refs.Pend{US: u, Numero: p.Numero}, central.Resposta{Opcao: &opcao, Por: "ana"}); err != nil {
		t.Fatal(err)
	}
	estado := func() *central.SpecUS {
		sp, err := c.st.LerSpec(ctx, &co, u)
		if err != nil {
			t.Fatal(err)
		}
		return sp
	}
	// Só o decisoes.md mudou: aprovada, com a conferência gravada.
	cabecaGH(c.falsos, shaCabeca)
	pastaGH(c.falsos, shaAprov, spec3)
	pastaGH(c.falsos, shaCabeca, com(spec3, "decisoes.md", "novo"))
	compareGH(c.falsos, shaAprov, shaCabeca, pastaSpec+"/decisoes.md")
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if sp := estado(); sp.Estado != central.SpecAprovada || sp.Conferencia == nil || !sp.Conferencia.PodeMesclar || *sp.SHACabeca != shaCabeca {
		t.Fatalf("sp = %+v", sp)
	}
	// A cabeça não andou: só a leitura da MR, sem reler as árvores.
	c.chamadas("gh")
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(c.chamadas("gh")); n != 1 {
		t.Fatalf("gh chamado %d vezes, queria 1 (só a cabeça)", n)
	}
	// A cabeça andou e a pasta mudou: "mudou além do decisoes.md".
	cabecaGH(c.falsos, shaRebased)
	pastaGH(c.falsos, shaRebased, com(spec3, "spec.md", "novo"))
	compareGH(c.falsos, shaAprov, shaRebased, pastaSpec+"/spec.md", "go.mod")
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if sp := estado(); sp.Estado != central.SpecMudou || len(sp.Conferencia.MudouNaPasta) != 1 || len(sp.Conferencia.ForaDaPasta) != 1 {
		t.Fatalf("sp = %+v", sp)
	}
	// A forja falha: o último valor bom fica, a falha vai ao lado, e o log registra uma vez.
	c.responder("gh", "HTTP 500", true, "api", apiPR)
	for i := 0; i < 2; i++ {
		if err := svc.Ciclo(ctx); err != nil {
			t.Fatal(err)
		}
	}
	sp := estado()
	if sp.ConferenciaErro == nil || sp.Estado != central.SpecMudou {
		t.Fatalf("sp = %+v", sp)
	}
	falhas := 0
	for _, l := range logs {
		if strings.Contains(l, "conferência") {
			falhas++
		}
	}
	if falhas != 1 {
		t.Fatalf("falhas no log = %d, queria 1", falhas)
	}
}
