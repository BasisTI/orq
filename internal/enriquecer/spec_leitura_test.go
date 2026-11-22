package enriquecer

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

const epicoSpec = "docs/specs/epico"

// arq é um arquivo de uma pasta da forja falsa. Modo vazio é regular; Tamanho zero vale o do conteúdo.
type arq struct {
	Nome, Conteudo, Modo string
	Tamanho              int64
}

func (a arq) id(sha string) string {
	return "b-" + sha[:4] + "-" + strings.ReplaceAll(a.Nome, "/", "-")
}

// arvoreGH registra, para o commit sha, a cadeia de árvores até docs/specs/epico (uma por nível), a
// pasta do épico com os arquivos dele e a subpasta tg-59-x, e a pasta da spec com os dela, mais os
// blobs. Conta como arquivo da pasta tudo o que está em spec; "sub" e "tg-60-y" são subpastas.
func arvoreGH(f *falsos, sha string, spec, epico []arq) {
	id := func(segs ...string) string { return "t-" + sha + "-" + strings.Join(segs, "-") }
	arvore := func(ref string, es []map[string]any) {
		f.responder("gh", js(map[string]any{"tree": es, "truncated": false}), false, "api", "repos/BasisTI/orq/git/trees/"+ref)
	}
	arvore(sha, []map[string]any{{"path": "docs", "type": "tree", "sha": id("docs")}})
	arvore(id("docs"), []map[string]any{{"path": "specs", "type": "tree", "sha": id("docs", "specs")}})
	arvore(id("docs", "specs"), []map[string]any{{"path": "epico", "type": "tree", "sha": id("docs", "specs", "epico")}})
	entradas := func(as []arq) []map[string]any {
		var es []map[string]any
		for _, a := range as {
			modo := a.Modo
			if modo == "" {
				modo = "100644"
			}
			tam := a.Tamanho
			if tam == 0 {
				tam = int64(len(a.Conteudo))
			}
			es = append(es, map[string]any{"path": a.Nome, "type": "blob", "mode": modo, "sha": a.id(sha), "size": tam})
			f.responder("gh", js(map[string]any{"content": base64.StdEncoding.EncodeToString([]byte(a.Conteudo)), "encoding": "base64"}), false,
				"api", "repos/BasisTI/orq/git/blobs/"+a.id(sha))
		}
		return es
	}
	es := entradas(epico)
	es = append(es, map[string]any{"path": "tg-59-x", "type": "tree", "sha": id("docs", "specs", "epico", "tg-59-x")}, map[string]any{"path": "tg-60-y", "type": "tree", "sha": "outra"})
	arvore(id("docs", "specs", "epico"), es)
	es = entradas(spec)
	es = append(es, map[string]any{"path": "sub", "type": "tree", "sha": "z"})
	arvore(id("docs", "specs", "epico", "tg-59-x"), es)
}

func nomes(as []central.ArquivoSpec) string {
	var n []string
	for _, a := range as {
		n = append(n, a.Papel+":"+strings.TrimPrefix(a.Caminho, epicoSpec+"/"))
	}
	return strings.Join(n, ",")
}

func TestLerSpecNoSHAAllowlist(t *testing.T) {
	f := novosFalsos(t)
	arvoreGH(f, shaAprov,
		[]arq{
			{Nome: "zeta.md", Conteudo: "z"},
			{Nome: "decisoes.md", Conteudo: "d"},
			{Nome: "plano.md", Conteudo: "p"},
			{Nome: "spec.md", Conteudo: "Veja o [plano](plano.md), o [épico](../README.md), a [ref](../outro.md#x), a [fora](../../fora.md), " +
				"[outra US](../tg-60-y/spec.md), [fundo](../sub/x.md), [abs](/docs/specs/epico/ref2.md), [web](https://x.y/z.md) e [sem arquivo](../nao-existe.md)."},
			{Nome: "dados.csv", Conteudo: "a,b"},
			{Nome: "atalho.md", Conteudo: "../README.md", Modo: "120000"},
		},
		[]arq{{Nome: "README.md", Conteudo: "épico"}, {Nome: "outro.md", Conteudo: "o"}, {Nome: "ref2.md", Conteudo: "r2"}, {Nome: "naoref.md", Conteudo: "n"}, {Nome: "link.md", Conteudo: "l", Modo: "120000"}})
	as, err := f.ferramentas().LerSpecNoSHA(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil {
		t.Fatal(err)
	}
	// Pasta: spec, plano, decisões e o resto por nome; sem csv, sem symlink. Épico: README e os referenciados, na ordem dos links.
	if got, want := nomes(as), "pasta:tg-59-x/spec.md,pasta:tg-59-x/plano.md,pasta:tg-59-x/decisoes.md,pasta:tg-59-x/zeta.md,epico:README.md,epico:outro.md,epico:ref2.md"; got != want {
		t.Fatalf("arquivos\n got %s\nwant %s", got, want)
	}
	if as[0].Conteudo == "" || as[0].GrandeDemais || as[3].Conteudo != "z" {
		t.Fatalf("conteúdo: %+v", as[0])
	}
}

func TestLerSpecNoSHALimites(t *testing.T) {
	f := novosFalsos(t)
	grande := strings.Repeat("a", central.LimiteArquivoSpec/2+1)
	arvoreGH(f, shaAprov,
		[]arq{
			{Nome: "spec.md", Conteudo: grande}, {Nome: "plano.md", Conteudo: grande}, {Nome: "decisoes.md", Conteudo: grande},
			{Nome: "enorme.md", Tamanho: central.LimiteArquivoSpec + 1}, {Nome: "pequeno.md", Conteudo: "ok"},
		}, nil)
	as, err := f.ferramentas().LerSpecNoSHA(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil {
		t.Fatal(err)
	}
	por := map[string]central.ArquivoSpec{}
	for _, a := range as {
		por[a.Caminho[strings.LastIndex(a.Caminho, "/")+1:]] = a
	}
	if e := por["enorme.md"]; !e.GrandeDemais || e.Conteudo != "" || e.Tamanho != central.LimiteArquivoSpec+1 {
		t.Fatalf("enorme = %+v", e)
	}
	if por["spec.md"].GrandeDemais || por["pequeno.md"].Conteudo != "ok" {
		t.Fatalf("os que cabem ficam: %+v", por)
	}
	// O arquivo grande demais pelo tamanho que a forja disse não é baixado.
	for _, c := range f.chamadas("gh") {
		if strings.HasSuffix(c[len(c)-1], "-enorme.md") {
			t.Fatalf("baixou o arquivo grande: %v", c)
		}
	}
}

func TestLerSpecNoSHAOrcamentoDaSpec(t *testing.T) {
	f := novosFalsos(t)
	b := strings.Repeat("a", central.LimiteArquivoSpec-10)
	arvoreGH(f, shaAprov, []arq{{Nome: "spec.md", Conteudo: b}, {Nome: "plano.md", Conteudo: b}, {Nome: "decisoes.md", Conteudo: b}, {Nome: "x.md", Conteudo: b}, {Nome: "y.md", Conteudo: b}}, nil)
	as, err := f.ferramentas().LerSpecNoSHA(context.Background(), prSpec, pastaSpec, shaAprov)
	if err != nil {
		t.Fatal(err)
	}
	guardados, grandes := 0, 0
	var soma int64
	for _, a := range as {
		if a.GrandeDemais {
			grandes++
			if a.Conteudo != "" {
				t.Fatalf("grande demais com conteúdo: %s", a.Caminho)
			}
		} else {
			guardados++
			soma += int64(len(a.Conteudo))
		}
	}
	if guardados != 4 || grandes != 1 || soma > central.LimiteSpec {
		t.Fatalf("guardados=%d grandes=%d soma=%d", guardados, grandes, soma)
	}
}

func TestLerSpecNoSHAGitLab(t *testing.T) {
	f := novosFalsos(t)
	mr := "https://gitlab.basis.com.br/basis/orq/-/merge_requests/9"
	proj := url.QueryEscape("basis/orq")
	arvore := func(dir string, es []map[string]any) {
		f.responder("glab", js(es), false, "api", "--hostname", "gitlab.basis.com.br",
			"projects/"+proj+"/repository/tree?path="+url.QueryEscape(dir)+"&ref="+shaAprov+"&per_page=100&page=1")
	}
	blob := func(id, conteudo string) {
		f.responder("glab", conteudo, false, "api", "--hostname", "gitlab.basis.com.br", "projects/"+proj+"/repository/blobs/"+id+"/raw")
	}
	arvore(pastaSpec, []map[string]any{
		{"id": "b1", "path": pastaSpec + "/spec.md", "type": "blob", "mode": "100644"},
		{"id": "b2", "path": pastaSpec + "/ln.md", "type": "blob", "mode": "120000"},
		{"id": "t", "path": pastaSpec + "/sub", "type": "tree", "mode": "040000"}})
	arvore(epicoSpec, []map[string]any{{"id": "b3", "path": epicoSpec + "/README.md", "type": "blob", "mode": "100644"}})
	blob("b1", "spec [épico](../README.md)")
	blob("b3", "épico")
	as, err := f.ferramentas().LerSpecNoSHA(context.Background(), mr, pastaSpec, shaAprov)
	if err != nil || nomes(as) != "pasta:tg-59-x/spec.md,epico:README.md" || as[1].Conteudo != "épico" {
		t.Fatalf("as = %+v, err = %v", as, err)
	}
	// Pasta que não existe (404).
	f.responder("glab", "404 Not Found", true, "api", "--hostname", "gitlab.basis.com.br", "projects/"+proj+"/repository/tree?path="+url.QueryEscape(pastaSpec)+"&ref="+shaAprov+"&per_page=100&page=1")
	if _, err := f.ferramentas().LerSpecNoSHA(context.Background(), mr, pastaSpec, shaAprov); !pastaAusente(err) {
		t.Fatalf("pasta ausente: %v", err)
	}
}

func TestSHADaBranch(t *testing.T) {
	f := novosFalsos(t)
	f.responder("gh", js(map[string]any{"commit": map[string]any{"sha": shaRebased}}), false, "api", "repos/BasisTI/orq/branches/feature%2Fx")
	if sha, err := f.ferramentas().SHADaBranch(context.Background(), prSpec, "feature/x"); err != nil || sha != shaRebased {
		t.Fatalf("sha = %q, %v", sha, err)
	}
	proj := url.QueryEscape("basis/orq")
	f.responder("glab", js(map[string]any{"commit": map[string]any{"id": shaAprov}}), false, "api", "--hostname", "gitlab.basis.com.br", "projects/"+proj+"/repository/branches/main")
	if sha, err := f.ferramentas().SHADaBranch(context.Background(), "https://gitlab.basis.com.br/basis/orq/-/merge_requests/9", "main"); err != nil || sha != shaAprov {
		t.Fatalf("sha = %q, %v", sha, err)
	}
}

// cenarioLeitura registra a US, a spec e a MR aberta com a cabeça shaCabeca, já com a árvore dela.
func cenarioLeitura(t *testing.T) (*cenario, central.Coordenador, refs.US) {
	t.Helper()
	ctx := context.Background()
	c := novoCenario(t)
	co := c.coord("alfa", "wA:p1", "")
	u := refs.US{Projeto: "orq", Ref: 59}
	if _, err := c.st.CriarOnda(ctx, co, central.NovaOnda{Titulo: "o", US: []refs.US{u}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.st.RegistrarSpec(ctx, &co, u, pastaSpec, prSpec, false); err != nil {
		t.Fatal(err)
	}
	return c, co, u
}

func arquivosSpec(spec string) ([]arq, []arq) {
	return []arq{{Nome: "spec.md", Conteudo: spec}, {Nome: "plano.md", Conteudo: "plano"}, {Nome: "decisoes.md", Conteudo: "decisões"}}, []arq{{Nome: "README.md", Conteudo: "épico"}}
}

func TestCicloDaLeituraDasSpecs(t *testing.T) {
	ctx := context.Background()
	c, co, u := cenarioLeitura(t)
	var logs []string
	svc := &LeituraSpecs{E: c.e, Log: func(f string, a ...any) { logs = append(logs, f) }}
	cache := func(sha string) central.SpecCache {
		sc, err := c.st.LerSpecCache(ctx, u, sha)
		if err != nil {
			t.Fatal(err)
		}
		return sc
	}
	// Primeira leitura: guarda a versão da cabeça e a marca como atual.
	cabecaGH(c.falsos, shaCabeca)
	sp, ep := arquivosSpec("v1")
	arvoreGH(c.falsos, shaCabeca, sp, ep)
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	sc := cache("")
	if sc.Versao == nil || sc.Versao.SHA != shaCabeca || len(sc.Versao.Arquivos) != 4 || sc.Leitura == nil || *sc.Leitura.SHA != shaCabeca || sc.Leitura.Erro != nil {
		t.Fatalf("cache = %+v", sc)
	}
	// Sem mudança de SHA: só a consulta da MR, nenhum arquivo relido.
	c.chamadas("gh")
	for i := 0; i < 3; i++ {
		if err := svc.Ciclo(ctx); err != nil {
			t.Fatal(err)
		}
	}
	chamadas := c.chamadas("gh")
	if len(chamadas) != 3 {
		t.Fatalf("gh chamado %d vezes em 3 ciclos parados, queria 3 (a MR): %v", len(chamadas), chamadas)
	}
	for _, ch := range chamadas {
		if ch[1] != apiPR {
			t.Fatalf("chamada que não é a da MR: %v", ch)
		}
	}
	// A cabeça andou: lê de novo, e a versão antiga é descartada.
	cabecaGH(c.falsos, shaRebased)
	sp, ep = arquivosSpec("v2")
	arvoreGH(c.falsos, shaRebased, sp, ep)
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if sc = cache(""); sc.Versao.SHA != shaRebased || sc.Versao.Arquivos[0].Conteudo != "v2" {
		t.Fatalf("cache = %+v", sc.Versao)
	}
	if v := cache(shaCabeca).Versao; v != nil {
		t.Fatalf("a versão antiga devia ter sido descartada: %+v", v)
	}
	_ = co
}

func TestLeituraDaValidacaoPeloSHADelaEPreservada(t *testing.T) {
	ctx := context.Background()
	c, co, u := cenarioLeitura(t)
	svc := &LeituraSpecs{E: c.e}
	p, err := c.st.AbrirValidacao(ctx, &co, u, shaAprov, "")
	if err != nil {
		t.Fatal(err)
	}
	// A cabeça já andou desde a abertura: lê a cabeça (atual) e o SHA da validação.
	cabecaGH(c.falsos, shaCabeca)
	sp, ep := arquivosSpec("da cabeça")
	arvoreGH(c.falsos, shaCabeca, sp, ep)
	sp, ep = arquivosSpec("da validação")
	arvoreGH(c.falsos, shaAprov, sp, ep)
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	val, err := c.st.LerSpecCache(ctx, u, shaAprov)
	if err != nil || val.Versao == nil || val.Versao.Arquivos[0].Conteudo != "da validação" {
		t.Fatalf("validação = %+v, %v", val.Versao, err)
	}
	// A cabeça anda de novo: a versão da validação aberta fica; a da cabeça velha sai.
	cabecaGH(c.falsos, shaRebased)
	sp, ep = arquivosSpec("terceira")
	arvoreGH(c.falsos, shaRebased, sp, ep)
	c.chamadas("gh")
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.st.LerSpecCache(ctx, u, shaAprov); v.Versao == nil || v.Versao.Arquivos[0].Conteudo != "da validação" {
		t.Fatalf("a versão da validação aberta foi substituída: %+v", v.Versao)
	}
	if v, _ := c.st.LerSpecCache(ctx, u, shaCabeca); v.Versao != nil {
		t.Fatal("a versão da cabeça antiga devia ter saído")
	}
	for _, ch := range c.chamadas("gh") {
		if strings.Contains(strings.Join(ch, " "), shaAprov[:4]+"-") {
			t.Fatalf("releu a versão da validação, que já estava no cache: %v", ch)
		}
	}
	// Respondida a pendência, a próxima passada descarta a versão dela.
	opcao := central.OpcaoPedirAjustes
	if _, err := c.st.ResponderPendencia(ctx, &co, refs.Pend{US: u, Numero: p.Numero}, central.Resposta{Opcao: &opcao, Por: "ana"}); err != nil {
		t.Fatal(err)
	}
	// Respondida mas aberta, a versão fica (aberta = 1 até o coordenador resolver).
	if _, err := c.st.ResolverPendencia(ctx, &co, refs.Pend{US: u, Numero: p.Numero}, ""); err != nil {
		t.Fatal(err)
	}
	c.rel.passar(time.Hour)
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	// A poda roda quando o daemon grava; sem mudança de SHA ela espera a próxima leitura.
	cabecaGH(c.falsos, "dddddddd44444444")
	sp, ep = arquivosSpec("quarta")
	arvoreGH(c.falsos, "dddddddd44444444", sp, ep)
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.st.LerSpecCache(ctx, u, shaAprov); v.Versao != nil {
		t.Fatal("validação respondida: a versão devia ter sido descartada")
	}
}

func TestLeituraComFalhaDaForjaMantemOUltimoConteudo(t *testing.T) {
	ctx := context.Background()
	c, _, u := cenarioLeitura(t)
	var logs []string
	svc := &LeituraSpecs{E: c.e, Log: func(f string, a ...any) { logs = append(logs, f) }}
	cabecaGH(c.falsos, shaCabeca)
	sp, ep := arquivosSpec("v1")
	arvoreGH(c.falsos, shaCabeca, sp, ep)
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	c.responder("gh", "HTTP 500", true, "api", apiPR)
	c.rel.passar(time.Hour)
	for i := 0; i < 2; i++ {
		if err := svc.Ciclo(ctx); err != nil {
			t.Fatal(err)
		}
		c.rel.passar(time.Hour)
	}
	sc, err := c.st.LerSpecCache(ctx, u, "")
	if err != nil || sc.Versao == nil || sc.Versao.Arquivos[0].Conteudo != "v1" || sc.Leitura.Erro == nil {
		t.Fatalf("sc = %+v, %v", sc, err)
	}
	if n := len(logs); n != 2 { // a leitura inicial e a falha, uma vez só
		t.Fatalf("logs = %v", logs)
	}
	// A forja volta: a falha some e o conteúdo é o mesmo.
	cabecaGH(c.falsos, shaCabeca)
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	if sc, _ = c.st.LerSpecCache(ctx, u, ""); sc.Leitura.Erro != nil || sc.Versao.Arquivos[0].Conteudo != "v1" {
		t.Fatalf("sc = %+v", sc)
	}
}

func TestLeituraDeMRMescladaUsaABranchAlvo(t *testing.T) {
	ctx := context.Background()
	c, _, u := cenarioLeitura(t)
	svc := &LeituraSpecs{E: c.e}
	c.responder("gh", js(map[string]any{"state": "closed", "merged": true, "head": map[string]any{"sha": shaCabeca, "ref": "tg-59"}, "base": map[string]any{"ref": "release/1"}}), false, "api", apiPR)
	c.responder("gh", js(map[string]any{"commit": map[string]any{"sha": shaRebased}}), false, "api", "repos/BasisTI/orq/branches/release%2F1")
	sp, ep := arquivosSpec("mesclada")
	arvoreGH(c.falsos, shaRebased, sp, ep)
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	sc, err := c.st.LerSpecCache(ctx, u, "")
	if err != nil || sc.Versao == nil || sc.Versao.SHA != shaRebased {
		t.Fatalf("sc = %+v, %v", sc, err)
	}
	for _, ch := range c.chamadas("gh") {
		if strings.Contains(strings.Join(ch, " "), shaCabeca) {
			t.Fatalf("leu pela origem depois de mesclada: %v", ch)
		}
	}
}

func TestLeituraPastaErradaViraFalhaNoCache(t *testing.T) {
	ctx := context.Background()
	c, _, u := cenarioLeitura(t)
	svc := &LeituraSpecs{E: c.e}
	cabecaGH(c.falsos, shaCabeca)
	c.responder("gh", js(map[string]any{"tree": []map[string]any{{"path": "outra", "type": "tree", "sha": "x"}}, "truncated": false}), false, "api", "repos/BasisTI/orq/git/trees/"+shaCabeca)
	if err := svc.Ciclo(ctx); err != nil {
		t.Fatal(err)
	}
	sc, err := c.st.LerSpecCache(ctx, u, "")
	if err != nil || sc.Versao != nil || sc.Leitura == nil || sc.Leitura.Erro == nil || sc.Leitura.Erro.Codigo != ErrPastaAusente {
		t.Fatalf("sc = %+v, %v", sc, err)
	}
}
