package central

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/BasisTI/orq/internal/refs"
)

func inteiro(n int) *int { return &n }

func novaDecisao(slug string) NovaPendencia {
	return NovaPendencia{US: refs.US{Projeto: "proj", Ref: 312}, Tipo: "decisao", Titulo: "Qual biblioteca de YAML?", Autor: "tg-312_executor",
		Detalhe: "## Contexto\nx\n", Slug: slug, Arquivo: "/t/proj/TG-312/pendencias/" + slug + ".md", Hash: "h1",
		Opcoes: []Opcao{{ID: 1, Titulo: "yaml.v3", Descricao: "estável"}, {ID: 2, Titulo: "goccy", Descricao: "rápida", Recomendada: true}}}
}

func storeComOnda(t *testing.T) (*Store, Coordenador) {
	t.Helper()
	s := novoStore(t)
	co := coord(t, s, "a", "wA:p1", "proj")
	onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{us(t, "312"), us(t, "313")}})
	return s, co
}

// Um banco v3 com pendências migra para a 0005: elas continuam abertas, sem detalhe e sem opções.
func TestMigracaoV3ComPendencias(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	v3 := fstest.MapFS{}
	es, _ := fs.ReadDir(migrationsFS, "migrations")
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "0004") || strings.HasPrefix(e.Name(), "0005") {
			continue
		}
		b, _ := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		v3["migrations/"+e.Name()] = &fstest.MapFile{Data: b}
	}
	antigo, err := OpenCom(ctx, dir, Opcoes{Migracoes: v3})
	if err != nil {
		t.Fatal(err)
	}
	_, err = antigo.RegistrarCoord(ctx, Registro{Nome: "a", Pane: "wA:p1", Projeto: "proj"})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO onda (id, coordenador, titulo, autorizacao_ate, ordem, criada_em) VALUES (1, 'a', 'o', 'merge', 'livre', '2026-10-11T12:00:00Z')`,
		`INSERT INTO us (projeto, ref, onda_id, posicao, status, proxima_pendencia, atualizado_em) VALUES ('proj', 312, 1, 1, 'wait', 3, '2026-10-11T12:00:00Z')`,
	} {
		if _, err := antigo.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	// O código novo não lê o esquema v3, então as pendências entram por SQL, como o orq 3 gravaria.
	for i, txt := range []string{"primeira", "segunda"} {
		if _, err := antigo.db.ExecContext(ctx, `INSERT INTO pendencia (projeto, ref, numero, tipo, texto, pane_origem, aberta, aberta_em) VALUES ('proj', 312, ?, 'duvida', ?, 'wB:p1', 1, '2026-10-11T12:00:00Z')`, i+1, txt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := antigo.db.ExecContext(ctx, `UPDATE pendencia SET aberta = 0, resposta = 'sim', resolvida_em = '2026-10-11T13:00:00Z' WHERE numero = 2`); err != nil {
		t.Fatal(err)
	}
	_ = antigo.Close()

	var m Migracao
	s, err := OpenCom(ctx, dir, Opcoes{AntesDeMigrar: func(x Migracao) { m = x }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if m.De != 3 || m.Para != 5 || m.LeitorMinimo != 4 || m.EscritorMinimo != 5 {
		t.Fatalf("migração = %+v", m)
	}
	ps, err := s.ListarPendencias(ctx, FiltroPendencias{})
	if err != nil || len(ps) != 2 {
		t.Fatalf("pendências = %+v, %v", ps, err)
	}
	p1, p2 := ps[0], ps[1]
	if p1.Estado != EstadoAberta || p1.TemDetalhe || len(p1.Opcoes) != 0 || p1.Slug != nil || p1.RespondidaEm != nil {
		t.Fatalf("P1 = %+v", p1)
	}
	if p2.Estado != EstadoResolvida || p2.Resposta == nil || *p2.Resposta != "sim" {
		t.Fatalf("P2 = %+v", p2)
	}
	// A pendência antiga responde pelo mesmo caminho.
	r, err := s.ResponderPendencia(ctx, nil, refs.Pend{US: us(t, "proj/312"), Numero: 1}, Resposta{Texto: "ok", Por: "ana"})
	if err != nil || r.Estado != EstadoRespondida {
		t.Fatalf("responder = %+v, %v", r, err)
	}
}

func TestEstadosRespostaETroca(t *testing.T) {
	s, co := storeComOnda(t)
	ctx := context.Background()
	res, err := s.RegistrarPendencia(ctx, novaDecisao("yaml"))
	if err != nil || !res.Criada || res.Pendencia.ID != "TG-312#P1" || res.Pendencia.Estado != EstadoAberta || !res.Pendencia.TemDetalhe {
		t.Fatalf("registrar = %+v, %v", res, err)
	}
	p := refs.Pend{US: us(t, "proj/312"), Numero: 1}
	// Decisão exige opção; opção inexistente e resposta vazia são recusadas.
	for _, c := range []struct {
		r    Resposta
		code string
	}{{Resposta{Texto: "só texto", Por: "ana"}, "uso"}, {Resposta{Opcao: inteiro(9), Por: "ana"}, "uso"}, {Resposta{Por: "ana"}, "uso"}, {Resposta{Opcao: inteiro(1)}, "uso"},
		{Resposta{Opcao: inteiro(1), Texto: "a\x00b", Por: "ana"}, "uso"}, {Resposta{Opcao: inteiro(1), Texto: strings.Repeat("x", LimiteResposta+1), Por: "ana"}, "uso"}} {
		if _, err := s.ResponderPendencia(ctx, &co, p, c.r); codigo(err) != c.code {
			t.Errorf("%+v: %v", c.r, err)
		}
	}
	r1, err := s.ResponderPendencia(ctx, &co, p, Resposta{Opcao: inteiro(1), Texto: "linha 1\r\nlinha 2", Por: "ana"})
	if err != nil || r1.Estado != EstadoRespondida || !r1.Aberta || *r1.RespostaOpcao != 1 || *r1.Resposta != "linha 1\nlinha 2" || *r1.RespondidaPor != "ana" || r1.AvisadaEm != nil {
		t.Fatalf("r1 = %+v, %v", r1, err)
	}
	// Respondida ainda conta como aberta para encerrar a onda e para --abertas.
	abertas, _ := s.ListarPendencias(ctx, FiltroPendencias{Abertas: true})
	respondidas, _ := s.ListarPendencias(ctx, FiltroPendencias{Respondidas: true})
	if len(abertas) != 1 || len(respondidas) != 1 {
		t.Fatalf("abertas %d, respondidas %d", len(abertas), len(respondidas))
	}
	us0, _ := s.LerUS(ctx, us(t, "proj/312"))
	enc := encerramentoDe(t, s, co)
	if enc == nil || len(enc.Faltam) == 0 || !strings.Contains(strings.Join(enc.Faltam, "|"), "#P1: pendência aberta") || us0.Pendencias[0].Estado != EstadoRespondida {
		t.Fatalf("encerramento = %+v", enc)
	}
	// Trocar a resposta zera o aviso e entra no histórico.
	if err := s.MarcarAvisada(ctx, Aviso{Tipo: AvisoResposta, Pendencia: r1}); err != nil {
		t.Fatal(err)
	}
	r2, err := s.ResponderPendencia(ctx, &co, p, Resposta{Opcao: inteiro(2), Por: "ana"})
	if err != nil || r2.AvisadaEm != nil || *r2.RespostaOpcao != 2 || r2.Resposta != nil {
		t.Fatalf("r2 = %+v, %v", r2, err)
	}
	h, err := s.HistoricoRespostas(ctx, p)
	if err != nil || len(h) != 2 || *h[0].Opcao != 1 || *h[1].Opcao != 2 || *h[1].TituloOpcao != "goccy" {
		t.Fatalf("histórico = %+v, %v", h, err)
	}
	n := 0
	for _, e := range eventos(t, s) {
		if e.Tipo == "pend.responder" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d eventos pend.responder, queria 2", n)
	}
	// resolver sem --resposta usa a registrada; sem texto, a opção escolhida.
	rr, err := s.ResolverPendencia(ctx, &co, p, "")
	if err != nil || rr.Aberta || rr.Estado != EstadoResolvida || *rr.Resposta != "opção 2 (goccy)" {
		t.Fatalf("resolver = %+v, %v", rr, err)
	}
	if _, err := s.ResponderPendencia(ctx, &co, p, Resposta{Opcao: inteiro(1), Por: "ana"}); codigo(err) != "pendencia_resolvida" {
		t.Fatalf("responder resolvida: %v", err)
	}
	// Com texto registrado, o texto vale; --resposta explícito vale mais.
	res, _ = s.RegistrarPendencia(ctx, NovaPendencia{US: refs.US{Projeto: "proj", Ref: 312}, Tipo: "duvida", Titulo: "d", Autor: "x", Detalhe: "d", Slug: "duv", Hash: "h"})
	p2 := refs.Pend{US: us(t, "proj/312"), Numero: res.Pendencia.Numero}
	if _, err := s.ResponderPendencia(ctx, &co, p2, Resposta{Opcao: inteiro(1), Por: "ana"}); codigo(err) != "uso" {
		t.Fatalf("opção numa dúvida sem opções: %v", err)
	}
	if _, err := s.ResponderPendencia(ctx, &co, p2, Resposta{Texto: "use a v3", Por: "ana"}); err != nil {
		t.Fatal(err)
	}
	rr, err = s.ResolverPendencia(ctx, &co, p2, "")
	if err != nil || *rr.Resposta != "use a v3" {
		t.Fatalf("resolver com texto = %+v, %v", rr, err)
	}
}

func encerramentoDe(t *testing.T, s *Store, co Coordenador) *Encerramento {
	t.Helper()
	ctx := context.Background()
	for _, ref := range []string{"proj/312", "proj/313"} {
		if _, _, err := s.AtualizarStatus(ctx, &co, us(t, ref), MudancaStatus{Status: "done"}); err != nil {
			t.Fatal(err)
		}
	}
	o, err := s.VerOnda(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	return EncerramentoDe(o)
}

// Resposta pelo chat já nasce avisada: quem a grava é o próprio coordenador.
func TestRespostaPeloChatNasceAvisada(t *testing.T) {
	s, co := storeComOnda(t)
	ctx := context.Background()
	if _, err := s.RegistrarPendencia(ctx, novaDecisao("yaml")); err != nil {
		t.Fatal(err)
	}
	r, err := s.ResponderPendencia(ctx, &co, refs.Pend{US: us(t, "proj/312"), Numero: 1}, Resposta{Opcao: inteiro(2), Por: PorChat})
	if err != nil || r.AvisadaEm == nil || *r.RespondidaPor != "chat" {
		t.Fatalf("r = %+v, %v", r, err)
	}
	avisos, _ := s.AvisosPendentes(ctx)
	for _, a := range avisos {
		if a.Tipo == AvisoResposta {
			t.Fatalf("aviso de resposta para a resposta do chat: %+v", a)
		}
	}
}

func TestRegistrarPendencia(t *testing.T) {
	s, co := storeComOnda(t)
	ctx := context.Background()
	res, err := s.RegistrarPendencia(ctx, novaDecisao("yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if p := res.Pendencia; p.Chave != "proj/312#P1" || *p.Slug != "yaml" || *p.Autor != "tg-312_executor" || len(p.Opcoes) != 2 || p.Tipo != "decisao" {
		t.Fatalf("p = %+v", p)
	}
	// Idempotente por slug + US: nada novo, mesmo número.
	again, err := s.RegistrarPendencia(ctx, novaDecisao("yaml"))
	if err != nil || again.Criada || again.Alterada || again.Pendencia.Numero != 1 {
		t.Fatalf("de novo = %+v, %v", again, err)
	}
	// Mesmo slug em outra US é outra pendência.
	n := novaDecisao("yaml")
	n.US.Ref = 313
	if o, err := s.RegistrarPendencia(ctx, n); err != nil || !o.Criada || o.Pendencia.ID != "TG-313#P1" {
		t.Fatalf("outra US = %+v, %v", o, err)
	}
	// Arquivo alterado depois do registro: a pendência não muda, o diário avisa uma vez.
	mudou := novaDecisao("yaml")
	mudou.Hash, mudou.Titulo = "h2", "OUTRO TÍTULO"
	for i := 0; i < 3; i++ {
		got, err := s.RegistrarPendencia(ctx, mudou)
		if err != nil || got.Criada || got.Alterada != (i == 0) || got.Pendencia.Texto != "Qual biblioteca de YAML?" || !got.Pendencia.ArquivoAlterado {
			t.Fatalf("alterado %d = %+v, %v", i, got, err)
		}
	}
	nAlt := 0
	for _, e := range eventos(t, s) {
		if e.Tipo == "pend.arquivo_alterado" {
			nAlt++
		}
	}
	if nAlt != 1 {
		t.Fatalf("%d eventos pend.arquivo_alterado, queria 1", nAlt)
	}
	// Voltar ao conteúdo registrado limpa a marca.
	if got, err := s.RegistrarPendencia(ctx, novaDecisao("yaml")); err != nil || got.Pendencia.ArquivoAlterado {
		t.Fatalf("restaurado = %+v, %v", got, err)
	}
	// Autorização sem opções ganha Autorizo / Não autorizo.
	a := NovaPendencia{US: refs.US{Projeto: "proj", Ref: 312}, Tipo: "autorizacao", Titulo: "migrar o banco?", Autor: "x", Detalhe: "d", Slug: "mig", Hash: "h"}
	if got, err := s.RegistrarPendencia(ctx, a); err != nil || len(got.Pendencia.Opcoes) != 2 || got.Pendencia.Opcoes[0].Titulo != "Autorizo" || got.Pendencia.Opcoes[1].Titulo != "Não autorizo" {
		t.Fatalf("autorização = %+v, %v", got, err)
	}
	// Erros: US fora de onda, onda encerrada, opções inválidas por tipo.
	fora := novaDecisao("f")
	fora.US.Ref = 999
	if _, err := s.RegistrarPendencia(ctx, fora); codigo(err) != "nao_encontrado" {
		t.Fatalf("US fora de onda: %v", err)
	}
	if _, err := s.EncerrarOnda(ctx, co.Nome, 1); err != nil {
		t.Fatal(err)
	}
	novo := novaDecisao("depois")
	if _, err := s.RegistrarPendencia(ctx, novo); codigo(err) != "onda_encerrada" {
		t.Fatalf("onda encerrada: %v", err)
	}
	// Slug já registrado segue idempotente com a onda encerrada.
	if got, err := s.RegistrarPendencia(ctx, novaDecisao("yaml")); err != nil || got.Criada {
		t.Fatalf("slug registrado em onda encerrada = %+v, %v", got, err)
	}
}

func TestValidarOpcoes(t *testing.T) {
	dois := []Opcao{{ID: 1, Titulo: "a"}, {ID: 2, Titulo: "b"}}
	rec := func(os ...int) []Opcao {
		out := []Opcao{{ID: 1, Titulo: "a"}, {ID: 2, Titulo: "b"}, {ID: 3, Titulo: "c"}}
		for _, i := range os {
			out[i].Recomendada = true
		}
		return out
	}
	casos := []struct {
		nome string
		tipo string
		os   []Opcao
		ok   bool
	}{
		{"decisão com 2", "decisao", dois, true},
		{"decisão sem opções", "decisao", nil, false},
		{"decisão com 1", "decisao", dois[:1], false},
		{"decisão com 2 recomendadas", "decisao", rec(0, 1), false},
		{"decisão com 1 recomendada", "decisao", rec(1), true},
		{"autorização com opções", "autorizacao", dois, false},
		{"autorização sem opções", "autorizacao", nil, true},
		{"dúvida sem opções", "duvida", nil, true},
		{"dúvida com 2", "duvida", dois, true},
		{"dúvida com 1", "duvida", dois[:1], false},
		{"id repetido", "decisao", []Opcao{{ID: 1, Titulo: "a"}, {ID: 1, Titulo: "b"}}, false},
		{"id zero", "decisao", []Opcao{{ID: 0, Titulo: "a"}, {ID: 1, Titulo: "b"}}, false},
		{"título vazio", "decisao", []Opcao{{ID: 1, Titulo: " "}, {ID: 2, Titulo: "b"}}, false},
		{"título com controle", "decisao", []Opcao{{ID: 1, Titulo: "a\nb"}, {ID: 2, Titulo: "b"}}, false},
	}
	for _, c := range casos {
		_, err := ValidarOpcoes(c.tipo, c.os)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.nome, err)
		}
	}
}

func TestAvisosPendentes(t *testing.T) {
	s, co := storeComOnda(t)
	ctx := context.Background()
	res, _ := s.RegistrarPendencia(ctx, novaDecisao("yaml"))
	avisos, err := s.AvisosPendentes(ctx)
	if err != nil || len(avisos) != 1 || avisos[0].Tipo != AvisoAbertura || avisos[0].Coordenador != "a" || *avisos[0].Pane != "wA:p1" {
		t.Fatalf("avisos = %+v, %v", avisos, err)
	}
	if err := s.MarcarAvisada(ctx, avisos[0]); err != nil {
		t.Fatal(err)
	}
	if avisos, _ = s.AvisosPendentes(ctx); len(avisos) != 0 {
		t.Fatalf("depois de avisar: %+v", avisos)
	}
	p := refs.Pend{US: us(t, "proj/312"), Numero: res.Pendencia.Numero}
	if _, err := s.ResponderPendencia(ctx, &co, p, Resposta{Opcao: inteiro(1), Por: "ana"}); err != nil {
		t.Fatal(err)
	}
	avisos, _ = s.AvisosPendentes(ctx)
	if len(avisos) != 1 || avisos[0].Tipo != AvisoResposta || avisos[0].Opcao == nil || avisos[0].Opcao.Titulo != "yaml.v3" {
		t.Fatalf("aviso de resposta = %+v", avisos)
	}
	velho := avisos[0]
	// A resposta troca entre a leitura do aviso e a marca: o aviso novo continua devido.
	if _, err := s.ResponderPendencia(ctx, &co, p, Resposta{Opcao: inteiro(2), Por: "ana"}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarcarAvisada(ctx, velho); err != nil {
		t.Fatal(err)
	}
	avisos, _ = s.AvisosPendentes(ctx)
	if len(avisos) != 1 || avisos[0].Opcao.ID != 2 {
		t.Fatalf("aviso da resposta trocada = %+v", avisos)
	}
	// Falha por coordenador inexistente: um evento só e a marca "sem coordenador".
	for i := 0; i < 3; i++ {
		if err := s.RegistrarFalhaAviso(ctx, avisos[0], "agent_not_found"); err != nil {
			t.Fatal(err)
		}
	}
	nFalhas := 0
	for _, e := range eventos(t, s) {
		if e.Tipo == "pend.aviso_falhou" {
			nFalhas++
		}
	}
	got, _ := s.VerPendencia(ctx, nil, p)
	if nFalhas != 1 || !got.SemCoordenador {
		t.Fatalf("falhas %d, pendência = %+v", nFalhas, got)
	}
	// Quando o aviso sai, a marca some.
	if err := s.MarcarAvisada(ctx, avisos[0]); err != nil {
		t.Fatal(err)
	}
	got, _ = s.VerPendencia(ctx, nil, p)
	if got.SemCoordenador || got.AvisadaEm == nil {
		t.Fatalf("depois do aviso = %+v", got)
	}
	if avisos, _ = s.AvisosPendentes(ctx); len(avisos) != 0 {
		t.Fatalf("sobrou aviso: %+v", avisos)
	}
}

func TestListarResolvidasPaginadas(t *testing.T) {
	s, co := storeComOnda(t)
	ctx := context.Background()
	for _, slug := range []string{"a", "b", "c"} {
		res, err := s.RegistrarPendencia(ctx, NovaPendencia{US: refs.US{Projeto: "proj", Ref: 312}, Tipo: "duvida", Titulo: slug, Autor: "x", Detalhe: "d", Slug: slug, Hash: slug})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.ResolverPendencia(ctx, &co, refs.Pend{US: us(t, "proj/312"), Numero: res.Pendencia.Numero}, slug); err != nil {
			t.Fatal(err)
		}
	}
	f := FiltroPendencias{Resolvidas: true, Limite: 2}
	p1, _ := s.ListarPendencias(ctx, f)
	f.Deslocamento = 2
	p2, _ := s.ListarPendencias(ctx, f)
	total, _ := s.ContarPendencias(ctx, FiltroPendencias{Resolvidas: true})
	if len(p1) != 2 || len(p2) != 1 || total != 3 || p1[0].Texto != "c" || p2[0].Texto != "a" {
		t.Fatalf("p1 %v p2 %v total %d", p1, p2, total)
	}
}
