package pendencias

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/herdrfalso/falsotest"
	"github.com/BasisTI/orq/internal/refs"
)

func TestMain(m *testing.M) {
	code := m.Run()
	falsotest.Limpar()
	os.Exit(code)
}

const decisao = `---
tipo: decisao
titulo: Qual biblioteca de YAML?
autor: tg-15_executor
opcoes:
  - id: 1
    titulo: yaml.v3
    descricao: estável, sem manutenção
  - id: 2
    titulo: go.yaml.in/yaml/v3
    descricao: fork mantido
    recomendada: true
---
## Contexto
O frontmatter precisa de YAML.

## Pergunta
Qual biblioteca?

## Recomendação (obrigatória em decisão)
A segunda.

## O que fica bloqueado
A importação.

## Evidências
- /tmp/x
`

const duvida = `---
tipo: duvida
titulo: O daemon reinicia?
autor: tg-15_executor
---
## Contexto
c
## Pergunta
p
## O que fica bloqueado
b
## Evidências
e
`

type ambiente struct {
	t     *testing.T
	st    *central.Store
	raiz  string
	imp   *Importador
	coord central.Coordenador
}

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	ctx := context.Background()
	st, err := central.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	co, err := st.RegistrarCoord(ctx, central.Registro{Nome: "coord", Pane: "wA:p1", Projeto: "orq"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CriarOnda(ctx, co, central.NovaOnda{Titulo: "o", US: []refs.US{{Projeto: "orq", Ref: 15}, {Projeto: "orq", Ref: 16}}}); err != nil {
		t.Fatal(err)
	}
	raiz := t.TempDir()
	return &ambiente{t: t, st: st, raiz: raiz, coord: co, imp: &Importador{St: st, Raiz: raiz, BaseURL: "https://pend.exemplo"}}
}

// grava escreve um arquivo (criando as pastas) e devolve o caminho.
func (a *ambiente) grava(rel, conteudo string) string {
	a.t.Helper()
	p := filepath.Join(a.raiz, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(conteudo), 0o644); err != nil {
		a.t.Fatal(err)
	}
	return p
}

func (a *ambiente) existe(rel string) bool {
	_, err := os.Stat(filepath.Join(a.raiz, rel))
	return err == nil
}

func (a *ambiente) lê(rel string) string {
	a.t.Helper()
	b, err := os.ReadFile(filepath.Join(a.raiz, rel))
	if err != nil {
		a.t.Fatal(err)
	}
	return string(b)
}

const worker = "orq/TG-15/implementacao/r1/pendencias/"

func TestLerArquivoValido(t *testing.T) {
	a, err := Ler([]byte(decisao))
	if err != nil {
		t.Fatal(err)
	}
	if a.Tipo != "decisao" || a.Titulo != "Qual biblioteca de YAML?" || a.Autor != "tg-15_executor" || len(a.Opcoes) != 2 || !a.Opcoes[1].Recomendada || a.Opcoes[0].Recomendada {
		t.Fatalf("a = %+v", a)
	}
	if !strings.HasPrefix(a.Detalhe, "## Contexto") || !strings.Contains(a.Detalhe, "## Evidências") {
		t.Fatalf("detalhe = %q", a.Detalhe)
	}
	// CRLF vale como LF.
	if _, err := Ler([]byte(strings.ReplaceAll(decisao, "\n", "\r\n"))); err != nil {
		t.Fatalf("CRLF: %v", err)
	}
}

func TestLerSecoesObrigatorias(t *testing.T) {
	for _, sec := range []string{"Contexto", "Pergunta", "Recomendação (obrigatória em decisão)", "O que fica bloqueado", "Evidências"} {
		t.Run(sec, func(t *testing.T) {
			sem := strings.Replace(decisao, "## "+sec, "## Outra coisa", 1)
			_, err := Ler([]byte(sem))
			nome, _, _ := strings.Cut(sec, " (")
			if err == nil || !strings.Contains(err.Error(), "`## "+nome+"` ausente") {
				t.Fatalf("err = %v", err)
			}
			if ea := err.(*ErroArquivo); ea.Problemas[0].Linha == 0 {
				t.Fatalf("sem linha: %+v", ea)
			}
		})
	}
	// Seção vazia é seção ausente.
	vazia := strings.Replace(duvida, "## Pergunta\np\n", "## Pergunta\n\n", 1)
	if _, err := Ler([]byte(vazia)); err == nil || !strings.Contains(err.Error(), "`## Pergunta` vazia") {
		t.Fatalf("seção vazia: %v", err)
	}
	// Recomendação só é obrigatória na decisão; dúvida passa sem ela.
	if _, err := Ler([]byte(duvida)); err != nil {
		t.Fatalf("dúvida: %v", err)
	}
	// Título dentro de bloco de código não conta.
	cerca := strings.Replace(duvida, "## Pergunta\np\n", "```\n## Pergunta\np\n```\n", 1)
	if _, err := Ler([]byte(cerca)); err == nil || !strings.Contains(err.Error(), "`## Pergunta` ausente") {
		t.Fatalf("título em cerca: %v", err)
	}
	// Vários problemas voltam juntos.
	_, err := Ler([]byte("---\ntipo: duvida\ntitulo: t\nautor: a\n---\nnada\n"))
	if ea, ok := err.(*ErroArquivo); !ok || len(ea.Problemas) != 4 {
		t.Fatalf("problemas = %v", err)
	}
}

func TestLerFrontmatterInvalido(t *testing.T) {
	casos := map[string]string{
		"sem frontmatter":     "## Contexto\nx\n",
		"sem fechamento":      "---\ntipo: duvida\n",
		"yaml quebrado":       "---\ntipo: [duvida\ntitulo: t\n---\n",
		"campo desconhecido":  strings.Replace(duvida, "autor:", "dono: x\nautor:", 1),
		"tipo inválido":       strings.Replace(duvida, "tipo: duvida", "tipo: pedido", 1),
		"sem título":          strings.Replace(duvida, "titulo: O daemon reinicia?", "titulo: ''", 1),
		"sem autor":           strings.Replace(duvida, "autor: tg-15_executor\n", "", 1),
		"título longo demais": strings.Replace(duvida, "O daemon reinicia?", strings.Repeat("x", 201), 1),
		"id não numérico":     strings.Replace(decisao, "id: 1", "id: um", 1),
		"recomendada texto":   strings.Replace(decisao, "recomendada: true", "recomendada: talvez", 1),
	}
	for nome, c := range casos {
		if _, err := Ler([]byte(c)); err == nil {
			t.Errorf("%s: aceitou", nome)
		} else if ea, ok := err.(*ErroArquivo); !ok || ea.Problemas[0].Linha < 1 {
			t.Errorf("%s: sem linha: %v", nome, err)
		}
	}
}

func TestLerOpcoesPorTipo(t *testing.T) {
	trocar := func(base, de, para string) string { return strings.Replace(base, de, para, 1) }
	casos := []struct {
		nome, conteudo, quer string
	}{
		{"decisão sem opções", strings.Replace(decisao, decisao[strings.Index(decisao, "opcoes:"):strings.Index(decisao, "---\n## Contexto")], "", 1), "pelo menos 2 opções"},
		{"decisão com duas recomendadas", trocar(decisao, "descricao: estável, sem manutenção", "descricao: x\n    recomendada: true"), "mais de uma opção recomendada"},
		{"id repetido", trocar(decisao, "id: 2", "id: 1"), "repetido"},
		{"autorização com opções", trocar(decisao, "tipo: decisao", "tipo: autorizacao"), "autorização não tem opções"},
		{"dúvida com uma opção", trocar(duvida, "---\n## Contexto", "opcoes:\n  - id: 1\n    titulo: a\n---\n## Contexto"), "2 ou nenhuma"},
	}
	for _, c := range casos {
		if _, err := Ler([]byte(c.conteudo)); err == nil || !strings.Contains(err.Error(), c.quer) {
			t.Errorf("%s: err = %v, queria %q", c.nome, err, c.quer)
		}
	}
	// Autorização sem opções no arquivo é válida (a Central cria as duas).
	aut := trocar(duvida, "tipo: duvida", "tipo: autorizacao")
	if _, err := Ler([]byte(aut)); err != nil {
		t.Fatalf("autorização: %v", err)
	}
}

func TestLerLimites(t *testing.T) {
	grande := duvida + strings.Repeat("x", central.LimiteDetalhe)
	if _, err := Ler([]byte(grande)); err == nil || !strings.Contains(err.Error(), "detalhe com") {
		t.Fatalf("detalhe grande: %v", err)
	}
	enorme := duvida + strings.Repeat("x", LimiteArquivo)
	if _, err := Ler([]byte(enorme)); err == nil || !strings.Contains(err.Error(), "arquivo com") {
		t.Fatalf("arquivo enorme: %v", err)
	}
	// Exatamente no limite passa.
	corpo := strings.Index(duvida, "## Contexto")
	pad := central.LimiteDetalhe - len(duvida[corpo:]) - 1
	limite := strings.Replace(duvida, "## Evidências\ne\n", "## Evidências\n"+strings.Repeat("e", pad+1)+"\n", 1)
	if a, err := Ler([]byte(limite)); err != nil {
		t.Fatalf("no limite: %v (%d)", err, len(limite[corpo:]))
	} else if len(a.Detalhe) > central.LimiteDetalhe+1 {
		t.Fatalf("detalhe = %d", len(a.Detalhe))
	}
	if _, err := Ler([]byte(strings.Replace(duvida, "## Contexto\nc", "## Contexto\nc\x01", 1))); err == nil {
		t.Fatal("controle no detalhe aceito")
	}
}

func TestImportarRegistraEGravaRegistrada(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	p := a.grava(worker+"decisao-yaml.md", decisao)
	r, err := a.imp.Importar(ctx, p)
	if err != nil || r.Estado != EstadoRegistrada || r.ID != "TG-15#P1" || r.Chave != "orq/15#P1" || r.URL != "https://pend.exemplo/p/orq/15/1" {
		t.Fatalf("r = %+v, %v", r, err)
	}
	var reg Registrada
	if err := json.Unmarshal([]byte(a.lê(worker+"decisao-yaml.registrada.json")), &reg); err != nil || reg != (Registrada{ID: "TG-15#P1", Chave: "orq/15#P1", URL: "https://pend.exemplo/p/orq/15/1"}) {
		t.Fatalf("registrada = %+v, %v", reg, err)
	}
	if a.existe(worker + "decisao-yaml.erro.txt") {
		t.Fatal("erro.txt sem erro")
	}
	pend, err := a.st.VerPendencia(ctx, nil, refs.Pend{US: refs.US{Projeto: "orq", Ref: 15}, Numero: 1})
	if err != nil || pend.Tipo != "decisao" || *pend.Autor != "tg-15_executor" || len(pend.Opcoes) != 2 || *pend.Slug != "decisao-yaml" || !strings.Contains(*pend.Detalhe, "## Evidências") {
		t.Fatalf("pendência = %+v, %v", pend, err)
	}
	// Idempotente por slug + US: importar de novo não cria outra.
	r2, err := a.imp.Importar(ctx, p)
	if err != nil || r2.Estado != EstadoJaRegistrada || r2.ID != "TG-15#P1" {
		t.Fatalf("de novo = %+v, %v", r2, err)
	}
	todas, _ := a.st.ListarPendencias(ctx, central.FiltroPendencias{})
	if len(todas) != 1 {
		t.Fatalf("%d pendências", len(todas))
	}
	// O coordenador grava na pasta da US, sem papel nem rodada.
	c := a.grava("orq/TG-16/pendencias/duvida.md", duvida)
	if r, err := a.imp.Importar(ctx, c); err != nil || r.ID != "TG-16#P1" {
		t.Fatalf("coordenador = %+v, %v", r, err)
	}
	// registrada.json apagado volta no próximo olhar.
	_ = os.Remove(filepath.Join(a.raiz, worker+"decisao-yaml.registrada.json"))
	if r, err := a.imp.Importar(ctx, p); err != nil || r.ID != "TG-15#P1" || !a.existe(worker+"decisao-yaml.registrada.json") {
		t.Fatalf("regravar = %+v, %v", r, err)
	}
}

func TestImportarArquivoInvalidoGravaErroEDepoisLimpa(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	p := a.grava(worker+"ruim.md", strings.Replace(decisao, "## Pergunta", "## Perguntas", 1))
	r, err := a.imp.Importar(ctx, p)
	if err != nil || r.Estado != EstadoErro || !strings.Contains(r.Erro, "`## Pergunta` ausente") {
		t.Fatalf("r = %+v, %v", r, err)
	}
	txt := a.lê(worker + "ruim.erro.txt")
	if !strings.Contains(txt, "linha ") || !strings.Contains(txt, "`## Pergunta` ausente") || a.existe(worker+"ruim.registrada.json") {
		t.Fatalf("erro.txt = %q", txt)
	}
	if l, _ := a.st.ListarPendencias(ctx, central.FiltroPendencias{}); len(l) != 0 {
		t.Fatalf("registrou pendência inválida: %+v", l)
	}
	// Corrigido o arquivo, o erro some.
	a.grava(worker+"ruim.md", decisao)
	if r, err := a.imp.Importar(ctx, p); err != nil || r.Estado != EstadoRegistrada {
		t.Fatalf("corrigido = %+v, %v", r, err)
	}
	if a.existe(worker + "ruim.erro.txt") {
		t.Fatal("erro.txt ficou depois do acerto")
	}
}

func TestImportarCaminhoForaDeUSDeOndaAberta(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	casos := []struct{ rel, quer string }{
		{"orq/TG-99/pendencias/x.md", "não está em nenhuma onda"},
	}
	for _, c := range casos {
		p := a.grava(c.rel, duvida)
		r, err := a.imp.Importar(ctx, p)
		if err != nil || r.Estado != EstadoErro || !strings.Contains(r.Erro, c.quer) {
			t.Errorf("%s: %+v, %v", c.rel, r, err)
		}
		if !strings.Contains(a.lê(strings.TrimSuffix(c.rel, ".md")+".erro.txt"), c.quer) {
			t.Errorf("%s: erro.txt sem o motivo", c.rel)
		}
	}
	// Onda encerrada.
	if _, err := a.st.EncerrarOnda(ctx, "coord", 1); err != nil {
		t.Fatal(err)
	}
	p := a.grava("orq/TG-16/pendencias/tarde.md", duvida)
	if r, err := a.imp.Importar(ctx, p); err != nil || r.Estado != EstadoErro || !strings.Contains(r.Erro, "onda encerrada") {
		t.Fatalf("onda encerrada: %+v, %v", r, err)
	}
	// Caminhos que não são do formato nem chegam a erro.txt: viram erro de uso.
	for _, rel := range []string{"orq/TG-15/pendencias/sub/x.md", "orq/15/pendencias/x.md", "orq/TG-15/pendencias/Maiúscula.md", "orq/TG-15/outro/x.md", "../x.md"} {
		if _, err := a.imp.Importar(ctx, filepath.Join(a.raiz, rel)); err == nil {
			t.Errorf("%s aceito", rel)
		}
	}
	if _, err := a.imp.Importar(ctx, filepath.Join(t.TempDir(), "orq/TG-15/pendencias/x.md")); err == nil {
		t.Error("caminho fora da raiz aceito")
	}
	if _, err := a.imp.Importar(ctx, "relativo/orq/TG-15/pendencias/x.md"); err == nil {
		t.Error("caminho relativo aceito")
	}
}

func TestImportarArquivoAlteradoDepoisDoRegistro(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	p := a.grava(worker+"d.md", decisao)
	if _, err := a.imp.Importar(ctx, p); err != nil {
		t.Fatal(err)
	}
	a.grava(worker+"d.md", strings.Replace(decisao, "Qual biblioteca de YAML?", "Outro título", 1))
	r, err := a.imp.Importar(ctx, p)
	if err != nil || r.Estado != EstadoJaRegistrada || !r.Alterado {
		t.Fatalf("r = %+v, %v", r, err)
	}
	pend, _ := a.st.VerPendencia(ctx, nil, refs.Pend{US: refs.US{Projeto: "orq", Ref: 15}, Numero: 1})
	if pend.Texto != "Qual biblioteca de YAML?" || !pend.ArquivoAlterado {
		t.Fatalf("a pendência mudou ou não marcou: %+v", pend)
	}
	// Segundo olhar sobre o mesmo conteúdo não repete o evento.
	if r, _ := a.imp.Importar(ctx, p); r.Alterado {
		t.Fatal("evento repetido")
	}
	es, _ := a.st.Diario(ctx, nil, central.FiltroDiario{})
	n := 0
	for _, e := range es {
		if e.Tipo == "pend.arquivo_alterado" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d eventos pend.arquivo_alterado", n)
	}
}

func TestVarrerIgnoraTmpEOcultoESymlinkParaFora(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	a.grava(worker+".d.md.tmp", decisao)
	a.grava(worker+"d.md.tmp", decisao)
	a.grava(worker+".oculto.md", decisao)
	a.grava(worker+"bom.md", decisao)
	// Symlink para um arquivo fora do ORQ_TRABALHO_DIR.
	fora := filepath.Join(t.TempDir(), "segredo.md")
	if err := os.WriteFile(fora, []byte(decisao), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fora, filepath.Join(a.raiz, worker, "link.md")); err != nil {
		t.Fatal(err)
	}
	// Symlink para dentro da raiz é aceito.
	if err := os.Symlink(filepath.Join(a.raiz, worker, "bom.md"), filepath.Join(a.raiz, worker, "atalho.md")); err != nil {
		t.Fatal(err)
	}
	// Pasta que é symlink para fora: nem lê, nem escreve do outro lado.
	foraDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(foraDir, "x.md"), []byte(decisao), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(a.raiz, "orq/TG-16"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foraDir, filepath.Join(a.raiz, "orq/TG-16/pendencias")); err != nil {
		t.Fatal(err)
	}
	rs, err := a.imp.Varrer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	estados := map[string]string{}
	for _, r := range rs {
		estados[filepath.Base(r.Arquivo)] = r.Estado
	}
	if estados["bom.md"] != EstadoRegistrada || estados["link.md"] != EstadoErro || estados["x.md"] != EstadoErro {
		t.Fatalf("estados = %v", estados)
	}
	for _, ig := range []string{".d.md.tmp", "d.md.tmp", ".oculto.md"} {
		if _, ok := estados[ig]; ok {
			t.Errorf("%s foi olhado", ig)
		}
	}
	if !strings.Contains(a.lê(worker+"link.erro.txt"), "fora de ORQ_TRABALHO_DIR") {
		t.Fatal("link sem erro.txt")
	}
	if a.existe(worker + "link.registrada.json") {
		t.Fatal("registrou o link")
	}
	if entries, _ := os.ReadDir(foraDir); len(entries) != 1 {
		t.Fatalf("escreveu fora da raiz: %v", entries)
	}
	// O atalho para dentro é o mesmo slug? Não: slug atalho, outro arquivo, mesma pendência nova.
	pend, _ := a.st.ListarPendencias(ctx, central.FiltroPendencias{})
	slugs := map[string]bool{}
	for _, p := range pend {
		slugs[*p.Slug] = true
	}
	if !slugs["bom"] || slugs["link"] || slugs["d"] || slugs[".oculto"] {
		t.Fatalf("slugs = %v", slugs)
	}
	// Nada novo no segundo ciclo.
	if rs, err := a.imp.Varrer(ctx); err != nil || len(rs) != 2 {
		t.Fatalf("segundo ciclo = %+v, %v", rs, err)
	}
}

func TestImportarLimiteDe64KiB(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	p := a.grava(worker+"grande.md", duvida+strings.Repeat("y", central.LimiteDetalhe))
	r, err := a.imp.Importar(ctx, p)
	if err != nil || r.Estado != EstadoErro || !strings.Contains(r.Erro, "detalhe com") {
		t.Fatalf("r = %+v, %v", r, err)
	}
	// Um arquivo gigante não é lido inteiro.
	g := a.grava(worker+"gigante.md", strings.Repeat("z", 5<<20))
	if r, err := a.imp.Importar(ctx, g); err != nil || r.Estado != EstadoErro || !strings.Contains(r.Erro, "arquivo com") {
		t.Fatalf("gigante = %+v, %v", r, err)
	}
}

// O daemon (Varrer) e orq pend importar (Importar) produzem a mesma pendência e o mesmo registro.
func TestVarrerEImportarDaoOMesmoResultado(t *testing.T) {
	porVarredura, porComando := novoAmbiente(t), novoAmbiente(t)
	ctx := context.Background()
	for _, a := range []*ambiente{porVarredura, porComando} {
		a.grava(worker+"decisao-yaml.md", decisao)
		a.grava(worker+"ruim.md", "sem frontmatter")
	}
	if _, err := porVarredura.imp.Varrer(ctx); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"decisao-yaml", "ruim"} {
		if _, err := porComando.imp.Importar(ctx, filepath.Join(porComando.raiz, worker+rel+".md")); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"decisao-yaml.registrada.json", "ruim.erro.txt"} {
		if porVarredura.existe(worker+rel) != porComando.existe(worker+rel) || porVarredura.lê(worker+rel) != porComando.lê(worker+rel) {
			t.Errorf("%s difere:\n%s\n---\n%s", rel, porVarredura.lê(worker+rel), porComando.lê(worker+rel))
		}
	}
	a, _ := porVarredura.st.VerPendencia(ctx, nil, refs.Pend{US: refs.US{Projeto: "orq", Ref: 15}, Numero: 1})
	b, _ := porComando.st.VerPendencia(ctx, nil, refs.Pend{US: refs.US{Projeto: "orq", Ref: 15}, Numero: 1})
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	// O caminho absoluto do arquivo é de cada ambiente.
	norm := func(s string, amb *ambiente) string { return strings.ReplaceAll(s, amb.raiz, "RAIZ") }
	if norm(string(ja), porVarredura) != norm(string(jb), porComando) {
		t.Fatalf("pendências diferem:\n%s\n%s", ja, jb)
	}
}

func TestVarrerSemRaiz(t *testing.T) {
	a := novoAmbiente(t)
	a.imp.Raiz = filepath.Join(t.TempDir(), "nao-existe")
	if rs, err := a.imp.Varrer(context.Background()); err != nil || len(rs) != 0 {
		t.Fatalf("raiz inexistente = %+v, %v", rs, err)
	}
}

func esperar(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("tempo esgotado")
}

// Um .tmp plantado como link, ou um FIFO no lugar do .md, não leva o daemon a escrever fora da
// raiz nem a travar.
func TestImportarNaoSegueTmpPlantadoNemTravaEmFIFO(t *testing.T) {
	a := novoAmbiente(t)
	ctx := context.Background()
	alvo := filepath.Join(t.TempDir(), "alvo")
	if err := os.WriteFile(alvo, []byte("intacto"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := a.grava(worker+"d.md", decisao)
	if err := os.Symlink(alvo, filepath.Join(a.raiz, worker, ".d.registrada.json.tmp")); err != nil {
		t.Fatal(err)
	}
	if r, err := a.imp.Importar(ctx, p); err != nil || r.Estado != EstadoRegistrada {
		t.Fatalf("importar = %+v, %v", r, err)
	}
	if b, _ := os.ReadFile(alvo); string(b) != "intacto" {
		t.Fatalf("o daemon escreveu pelo link: %q", b)
	}
	fifo := filepath.Join(a.raiz, worker, "fifo.md")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("sem mkfifo: %v", err)
	}
	feito := make(chan Resultado, 1)
	go func() { r, _ := a.imp.Importar(ctx, fifo); feito <- r }()
	select {
	case r := <-feito:
		if r.Estado != EstadoErro {
			t.Fatalf("FIFO = %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("o importador travou num FIFO")
	}
}
