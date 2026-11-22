package pendencias

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/refs"
)

const validacao = `---
tipo: validacao
titulo: um título qualquer que o orq ignora
autor: coordenador
opcoes:
  - id: 1
    titulo: Outra coisa
  - id: 2
    titulo: Mais outra
    recomendada: true
---
## Contexto
A spec da US está pronta.
## Pergunta
Aprova?
## O que fica bloqueado
A implementação.
## Evidências
- a MR
`

const validacaoSemTitulo = `---
tipo: validacao
autor: coordenador
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

type forjaFalsa struct {
	sha     string
	err     error
	chamado []string
}

func (f *forjaFalsa) CabecaMR(_ context.Context, url string) (string, error) {
	f.chamado = append(f.chamado, url)
	return f.sha, f.err
}

const arqValidacao = "orq/TG-15/pendencias/validar-spec.md"

func registrarSpec(t *testing.T, a *ambiente) {
	t.Helper()
	if _, err := a.st.RegistrarSpec(context.Background(), &a.coord, refs.US{Projeto: "orq", Ref: 15}, "docs/specs/e/tg-15-x", "https://github.com/BasisTI/orq/pull/44", false); err != nil {
		t.Fatal(err)
	}
}

func TestValidacaoGravaOSHAEImpoeTituloEOpcoes(t *testing.T) {
	a := novoAmbiente(t)
	registrarSpec(t, a)
	f := &forjaFalsa{sha: "8c9d43de5bdf2e1f74cf0913319aff780fcceb88"}
	a.imp.Forja = f
	r, err := a.imp.Importar(context.Background(), a.grava(arqValidacao, validacao))
	if err != nil || r.Estado != EstadoRegistrada {
		t.Fatalf("r = %+v, %v", r, err)
	}
	if len(f.chamado) != 1 || f.chamado[0] != "https://github.com/BasisTI/orq/pull/44" {
		t.Fatalf("forja chamada com %v", f.chamado)
	}
	p, err := a.st.VerPendencia(context.Background(), &a.coord, refs.Pend{US: refs.US{Projeto: "orq", Ref: 15}, Numero: 1})
	if err != nil {
		t.Fatal(err)
	}
	if p.Tipo != "validacao" || p.Texto != "Spec da TG-15 pronta para validar" || *p.SHAValidacao != f.sha ||
		len(p.Opcoes) != 2 || p.Opcoes[0].Titulo != "Aprovar" || p.Opcoes[1].Titulo != "Pedir ajustes" || p.Opcoes[0].Recomendada || p.Opcoes[1].Recomendada {
		t.Fatalf("p = %+v", p)
	}
	// Importar de novo não pergunta nada à forja nem cria outra pendência.
	if r, err := a.imp.Importar(context.Background(), a.grava(arqValidacao, validacao)); err != nil || r.Estado != EstadoJaRegistrada || len(f.chamado) != 1 {
		t.Fatalf("de novo: %+v, %v, forja %v", r, err, f.chamado)
	}
	// A cabeça andou depois: a pendência continua presa ao SHA que gravou.
	f.sha = "outro"
	if _, err := a.imp.Varrer(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p, _ := a.st.VerPendencia(context.Background(), &a.coord, refs.Pend{US: refs.US{Projeto: "orq", Ref: 15}, Numero: 1}); *p.SHAValidacao != "8c9d43de5bdf2e1f74cf0913319aff780fcceb88" {
		t.Fatalf("sha = %s", *p.SHAValidacao)
	}
}

func TestValidacaoNaoPrecisaDeTituloNemDeOpcoes(t *testing.T) {
	a := novoAmbiente(t)
	registrarSpec(t, a)
	a.imp.Forja = &forjaFalsa{sha: "abc"}
	if r, err := a.imp.Importar(context.Background(), a.grava(arqValidacao, validacaoSemTitulo)); err != nil || r.Estado != EstadoRegistrada {
		t.Fatalf("r = %+v, %v", r, err)
	}
}

func TestValidacaoSemSpecEhRecusada(t *testing.T) {
	a := novoAmbiente(t)
	f := &forjaFalsa{sha: "abc"}
	a.imp.Forja = f
	r, err := a.imp.Importar(context.Background(), a.grava(arqValidacao, validacao))
	if err != nil || r.Estado != EstadoErro || !strings.Contains(r.Erro, "não tem spec registrada") {
		t.Fatalf("r = %+v, %v", r, err)
	}
	if !a.existe("orq/TG-15/pendencias/validar-spec.erro.txt") || !strings.Contains(a.lê("orq/TG-15/pendencias/validar-spec.erro.txt"), "spec registrada") {
		t.Fatal("o motivo tem de estar no .erro.txt")
	}
	if len(f.chamado) != 0 {
		t.Fatal("sem spec, a forja nem é consultada")
	}
	// Registrada a spec, o mesmo arquivo passa e o .erro.txt some.
	registrarSpec(t, a)
	if r, err := a.imp.Importar(context.Background(), a.grava(arqValidacao, validacao)); err != nil || r.Estado != EstadoRegistrada {
		t.Fatalf("depois da spec: %+v, %v", r, err)
	}
	if a.existe("orq/TG-15/pendencias/validar-spec.erro.txt") {
		t.Fatal(".erro.txt devia ter sumido")
	}
}

func TestValidacaoComAForjaFalhandoFicaRetida(t *testing.T) {
	ctx := context.Background()
	a := novoAmbiente(t)
	registrarSpec(t, a)
	f := &forjaFalsa{err: errors.New("rede fora")}
	a.imp.Forja = f
	var logs []string
	a.imp.Log = func(format string, args ...any) { logs = append(logs, format) }
	caminho := a.grava(arqValidacao, validacao)
	for i := 0; i < 3; i++ {
		rs, err := a.imp.Varrer(ctx)
		if err != nil || len(rs) != 0 {
			t.Fatalf("varredura %d: %+v, %v (retida não aparece nem vira erro)", i, rs, err)
		}
	}
	if a.existe("orq/TG-15/pendencias/validar-spec.erro.txt") || a.existe("orq/TG-15/pendencias/validar-spec.registrada.json") {
		t.Fatal("retida: nem .erro.txt nem .registrada.json")
	}
	if ps, _ := a.st.ListarPendencias(ctx, central.FiltroPendencias{}); len(ps) != 0 {
		t.Fatalf("retida não aparece na Central: %+v", ps)
	}
	if len(f.chamado) != 3 || len(logs) != 1 {
		t.Fatalf("tentou %d vezes e logou %d vezes; queria 3 e 1 (o mesmo erro não enche o log)", len(f.chamado), len(logs))
	}
	if r, err := a.imp.Importar(ctx, caminho); err != nil || r.Estado != EstadoRetida {
		t.Fatalf("Importar = %+v, %v", r, err)
	}
	// A forja volta: importada no ciclo seguinte.
	f.err, f.sha = nil, "sha-ok"
	rs, err := a.imp.Varrer(ctx)
	if err != nil || len(rs) != 1 || rs[0].Estado != EstadoRegistrada {
		t.Fatalf("depois da forja voltar: %+v, %v", rs, err)
	}
	if ps, _ := a.st.ListarPendencias(ctx, central.FiltroPendencias{}); len(ps) != 1 || *ps[0].SHAValidacao != "sha-ok" {
		t.Fatalf("ps = %+v", ps)
	}
	// Sem forja configurada, também fica retida.
	b := novoAmbiente(t)
	registrarSpec(t, b)
	if r, err := b.imp.Importar(ctx, b.grava(arqValidacao, validacao)); err != nil || r.Estado != EstadoRetida {
		t.Fatalf("sem forja: %+v, %v", r, err)
	}
}

// O tipo validacao só vale com o SHA do orq: arquivo de outro tipo não é afetado.
func TestOutrosTiposNaoConsultamAForja(t *testing.T) {
	a := novoAmbiente(t)
	f := &forjaFalsa{err: errors.New("fora")}
	a.imp.Forja = f
	if r, err := a.imp.Importar(context.Background(), a.grava("orq/TG-15/pendencias/d.md", duvida)); err != nil || r.Estado != EstadoRegistrada || len(f.chamado) != 0 {
		t.Fatalf("r = %+v, %v, forja %v", r, err, f.chamado)
	}
}

// M3: MR que a forja diz não existir (404) é recusa com .erro.txt; falha passageira segue retida.
func TestValidacaoComMRInexistenteViraErro(t *testing.T) {
	a := novoAmbiente(t)
	registrarSpec(t, a)
	a.imp.Forja = &forjaFalsa{err: &enriquecer.Falha{Codigo: enriquecer.ErrNaoEncontrado, Mensagem: "HTTP 404"}}
	r, err := a.imp.Importar(context.Background(), a.grava(arqValidacao, validacao))
	if err != nil || r.Estado != EstadoErro || !strings.Contains(r.Erro, "não existe na forja") {
		t.Fatalf("r = %+v, %v", r, err)
	}
	if !strings.Contains(a.lê("orq/TG-15/pendencias/validar-spec.erro.txt"), "orq spec registrar") {
		t.Fatal(".erro.txt sem o caminho de correção")
	}
	for _, cod := range []string{enriquecer.ErrAutenticacao, enriquecer.ErrFalhaComando, enriquecer.ErrTempoEsgotado} {
		a.imp.Forja = &forjaFalsa{err: &enriquecer.Falha{Codigo: cod, Mensagem: "x"}}
		if r, err := a.imp.Importar(context.Background(), a.grava(arqValidacao, validacao)); err != nil || r.Estado != EstadoRetida {
			t.Fatalf("%s: %+v, %v", cod, r, err)
		}
	}
}
