package central

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/refs"
)

const mrSpec = "https://github.com/BasisTI/orq/pull/44"

func ambienteSpec(t *testing.T) (*Store, Coordenador) {
	t.Helper()
	s := novoStore(t)
	co := coord(t, s, "a", "p1", "proj")
	onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{us(t, "59")}})
	return s, co
}

func eventosDoTipo(t *testing.T, s *Store, tipo string) []Evento {
	t.Helper()
	var out []Evento
	for _, e := range eventos(t, s) {
		if e.Tipo == tipo {
			out = append(out, e)
		}
	}
	return out
}

func TestRegistrarSpecLigaAMRNaMesmaTransacao(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	r, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/specs/x/tg-59-y/", mrSpec+"/files", false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Spec.Caminho != "docs/specs/x/tg-59-y" || r.Spec.MRURL != mrSpec || r.Spec.Estado != SpecRegistrada || !r.MRLigada || r.Substituiu {
		t.Fatalf("r = %+v", r)
	}
	if es := eventosDoTipo(t, s, "mr.add"); len(es) != 1 {
		t.Fatalf("mr.add = %d eventos", len(es))
	}
	es := eventosDoTipo(t, s, "spec.registrar")
	if len(es) != 1 || !strings.Contains(string(es[0].Detalhe), `"caminho":"docs/specs/x/tg-59-y"`) {
		t.Fatalf("spec.registrar = %+v", es)
	}
	// A MR já ligada por orq mr add não é ligada de novo.
	s2, co2 := ambienteSpec(t)
	if _, _, err := s2.AdicionarMR(ctx, &co2, us(t, "59"), mrSpec); err != nil {
		t.Fatal(err)
	}
	r2, err := s2.RegistrarSpec(ctx, &co2, us(t, "59"), "docs/x", mrSpec, false)
	if err != nil || r2.MRLigada {
		t.Fatalf("r2 = %+v, %v", r2, err)
	}
	if es := eventosDoTipo(t, s2, "mr.add"); len(es) != 1 {
		t.Fatalf("mr.add = %d eventos, queria 1", len(es))
	}
}

func TestRegistrarSpecDuplicidadeESubstituir(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/a", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	antes := len(eventos(t, s))
	_, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/b", "https://github.com/BasisTI/orq/pull/50", false)
	if codigo(err) != "spec_ja_registrada" || !strings.Contains(err.Error(), "docs/a") || !strings.Contains(err.Error(), mrSpec) {
		t.Fatalf("duplicidade = %v", err)
	}
	if len(eventos(t, s)) != antes {
		t.Fatal("a recusa não pode gravar nada (nem ligar a MR nova)")
	}
	if sp, _ := s.LerSpec(ctx, &co, us(t, "59")); sp.Caminho != "docs/a" {
		t.Fatalf("spec mudou na recusa: %+v", sp)
	}
	r, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/b", "https://github.com/BasisTI/orq/pull/50", true)
	if err != nil || !r.Substituiu || r.Anterior.Caminho != "docs/a" || r.Spec.Caminho != "docs/b" {
		t.Fatalf("substituir = %+v, %v", r, err)
	}
	es := eventosDoTipo(t, s, "spec.registrar")
	var d struct {
		Caminho  string
		Anterior map[string]string
	}
	if len(es) != 2 || json.Unmarshal(es[1].Detalhe, &d) != nil || d.Caminho != "docs/b" || d.Anterior["caminho"] != "docs/a" || d.Anterior["mr_url"] != mrSpec {
		t.Fatalf("evento da substituição = %s", es[len(es)-1].Detalhe)
	}
}

func TestRegistrarSpecValida(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	for _, c := range []struct{ caminho, mr string }{
		{"docs/x", "https://github.com/BasisTI/orq/issues/4"}, // não é MR
		{"", mrSpec}, {"/docs/x", mrSpec}, {"docs/../x", mrSpec}, {".", mrSpec}, {"docs\\x", mrSpec},
	} {
		if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), c.caminho, c.mr, false); codigo(err) != "uso" {
			t.Errorf("%q %q: %v", c.caminho, c.mr, err)
		}
	}
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "99"), "docs/x", mrSpec, false); codigo(err) != "nao_encontrado" {
		t.Errorf("US fora da onda: %v", err)
	}
	if _, err := s.LerSpec(ctx, &co, us(t, "59")); codigo(err) != "nao_encontrado" {
		t.Errorf("sem spec: %v", err)
	}
}

func TestValidacaoSemSpecEDoCLI(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	if _, err := s.AbrirValidacao(ctx, &co, us(t, "59"), "abc", ""); codigo(err) != "uso" {
		t.Fatalf("sem spec: %v", err)
	}
	if _, err := s.AbrirPendencia(ctx, &co, us(t, "59"), "validacao", "x", ""); codigo(err) != "uso" {
		t.Fatalf("AbrirPendencia(validacao): %v", err)
	}
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/a", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AbrirValidacao(ctx, &co, us(t, "59"), "", ""); codigo(err) != "uso" {
		t.Fatalf("sem SHA: %v", err)
	}
	p, err := s.AbrirValidacao(ctx, &co, us(t, "59"), "8c9d43de", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Tipo != "validacao" || p.Texto != "Spec da TG-59 pronta para validar" || p.SHAValidacao == nil || *p.SHAValidacao != "8c9d43de" ||
		len(p.Opcoes) != 2 || p.Opcoes[0].Titulo != "Aprovar" || p.Opcoes[1].Titulo != "Pedir ajustes" || p.Opcoes[0].Recomendada || p.Opcoes[1].Recomendada {
		t.Fatalf("p = %+v", p)
	}
}

func TestRegistrarPendenciaValidacaoImpoeTituloEOpcoes(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	n := NovaPendencia{US: us(t, "59"), Tipo: "validacao", Titulo: "título do arquivo", Autor: "plan", Detalhe: "## Contexto\nc\n", Slug: "validar-spec", SHA: "abc123",
		Opcoes: []Opcao{{ID: 1, Titulo: "Qualquer"}, {ID: 2, Titulo: "Outra", Recomendada: true}}}
	if _, err := s.RegistrarPendencia(ctx, n); codigo(err) != "uso" {
		t.Fatalf("sem spec: %v", err)
	}
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/a", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	semSHA := n
	semSHA.SHA = ""
	if _, err := s.RegistrarPendencia(ctx, semSHA); codigo(err) != "uso" {
		t.Fatalf("sem SHA: %v", err)
	}
	r, err := s.RegistrarPendencia(ctx, n)
	if err != nil || !r.Criada {
		t.Fatal(r, err)
	}
	p := r.Pendencia
	if p.Texto != "Spec da TG-59 pronta para validar" || p.Opcoes[0].Titulo != "Aprovar" || p.Opcoes[1].Titulo != "Pedir ajustes" || p.Opcoes[1].Recomendada || *p.SHAValidacao != "abc123" {
		t.Fatalf("p = %+v", p)
	}
}

// answer responde a última validação com a opção.
func responderValidacao(t *testing.T, s *Store, co Coordenador, numero, opcao int) {
	t.Helper()
	if _, err := s.ResponderPendencia(context.Background(), &co, refs.Pend{US: us(t, "59"), Numero: numero}, Resposta{Opcao: &opcao, Por: "ana"}); err != nil {
		t.Fatal(err)
	}
}

func TestEstadosDaSpec(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	estado := func() *SpecUS {
		t.Helper()
		sp, err := s.LerSpec(ctx, &co, us(t, "59"))
		if err != nil {
			t.Fatal(err)
		}
		return sp
	}
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/a", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	if sp := estado(); sp.Estado != SpecRegistrada || sp.SHAValidacao != nil {
		t.Fatalf("registrada: %+v", sp)
	}
	p, err := s.AbrirValidacao(ctx, &co, us(t, "59"), "sha-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if sp := estado(); sp.Estado != SpecEmValidacao || *sp.SHAValidacao != "sha-1" || sp.SHAAprovado != nil || *sp.Validacao != "TG-59#P1" {
		t.Fatalf("em validação: %+v", sp)
	}
	// Pedir ajustes volta a "registrada": não há validação vigente.
	responderValidacao(t, s, co, p.Numero, OpcaoPedirAjustes)
	if sp := estado(); sp.Estado != SpecRegistrada {
		t.Fatalf("pediu ajustes: %+v", sp)
	}
	// Trocar a resposta por Aprovar (a pendência segue aberta até alguém resolver).
	responderValidacao(t, s, co, p.Numero, OpcaoAprovar)
	if sp := estado(); sp.Estado != SpecAprovada || *sp.SHAAprovado != "sha-1" {
		t.Fatalf("aprovada: %+v", sp)
	}
	// Conferência do daemon: do SHA aprovado, mudou além do decisoes.md.
	ur := refs.US{Projeto: "proj", Ref: 59}
	conf := ConferenciaSpec{SHAAprovado: "sha-1", SHACabeca: "sha-2", PodeMesclar: false, MudouNaPasta: []string{"spec.md"}}
	if err := s.GravarConferenciaSpec(ctx, ur, &conf, nil); err != nil {
		t.Fatal(err)
	}
	if sp := estado(); sp.Estado != SpecMudou || *sp.SHACabeca != "sha-2" || sp.Conferencia == nil {
		t.Fatalf("mudou: %+v", sp)
	}
	// A conferência de outro SHA aprovado não vale.
	conf.SHAAprovado = "outro"
	if err := s.GravarConferenciaSpec(ctx, ur, &conf, nil); err != nil {
		t.Fatal(err)
	}
	if sp := estado(); sp.Estado != SpecAprovada || sp.Conferencia != nil {
		t.Fatalf("conferência de outro SHA: %+v", sp)
	}
	// Falha da forja: o último valor bom fica e a falha vai ao lado.
	if err := s.GravarConferenciaSpec(ctx, ur, nil, &Erro{Codigo: "falha_comando", Mensagem: "rede"}); err != nil {
		t.Fatal(err)
	}
	if sp := estado(); sp.ConferenciaErro == nil || sp.ConferenciaErro.Codigo != "falha_comando" {
		t.Fatalf("erro: %+v", sp)
	}
	// Nova validação aberta: em validação de novo, mesmo com a antiga aprovada.
	p2, err := s.AbrirValidacao(ctx, &co, us(t, "59"), "sha-3", "")
	if err != nil {
		t.Fatal(err)
	}
	if sp := estado(); sp.Estado != SpecEmValidacao || *sp.SHAValidacao != "sha-3" {
		t.Fatalf("nova validação: %+v", sp)
	}
	responderValidacao(t, s, co, p2.Numero, OpcaoAprovar)
	// MR mesclada vence os outros estados.
	if err := s.GravarMR(ctx, mrSpec, ValorMR{Estado: "mesclada"}, nil); err != nil {
		t.Fatal(err)
	}
	if sp := estado(); sp.Estado != SpecMesclada {
		t.Fatalf("mesclada: %+v", sp)
	}
	// Mesclada não é mais conferida.
	if ls, err := s.SpecsParaConferir(ctx); err != nil || len(ls) != 0 {
		t.Fatalf("SpecsParaConferir = %+v, %v", ls, err)
	}
}

func TestSnapshotTrazASpecSemSubirAVersao(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	snap, err := s.Snapshot(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Versao != 1 {
		t.Fatalf("versao = %d, queria 1 (campo novo não sobe a versão)", snap.Versao)
	}
	if u := snap.Coordenadores[0].Ondas[0].US[0]; u.Spec != nil {
		t.Fatalf("sem registro, spec = %+v, queria nula", u.Spec)
	}
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/a", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	snap, _ = s.Snapshot(ctx, false)
	b, err := json.Marshal(snap.Coordenadores[0].Ondas[0].US[0])
	if err != nil {
		t.Fatal(err)
	}
	var u struct{ Spec map[string]any }
	if err := json.Unmarshal(b, &u); err != nil {
		t.Fatal(err)
	}
	if u.Spec["caminho"] != "docs/a" || u.Spec["mr_url"] != mrSpec || u.Spec["estado"] != "registrada" {
		t.Fatalf("spec = %v", u.Spec)
	}
	for _, k := range []string{"sha_validacao", "sha_aprovado", "sha_cabeca", "conferencia", "validacao"} {
		if _, ok := u.Spec[k]; !ok {
			t.Errorf("campo %s ausente do snapshot", k)
		}
	}
}

func TestMigracaoDaTabelaPendenciaPreservaOsDados(t *testing.T) {
	// A 0011 refaz a tabela pendencia (o CHECK do tipo); o que já existia continua lá, com o mesmo id.
	ctx := context.Background()
	s, co := ambienteSpec(t)
	p, err := s.AbrirPendencia(ctx, &co, us(t, "59"), "duvida", "antes", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "TG-59#P1" || p.Estado != EstadoAberta {
		t.Fatalf("p = %+v", p)
	}
	var idx int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN ('pendencia_aberta', 'pendencia_slug')`).Scan(&idx); err != nil || idx != 2 {
		t.Fatalf("índices = %d, %v", idx, err)
	}
}
