package central

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

const mrCodigo = "https://github.com/BasisTI/orq/pull/61"

// ambienteLink é um ambiente com a spec da TG-59 registrada.
func ambienteLink(t *testing.T) (*Store, Coordenador, refs.US) {
	t.Helper()
	s, co := ambienteSpec(t)
	if _, err := s.RegistrarSpec(context.Background(), &co, us(t, "59"), "docs/specs/x/tg-59-y", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	return s, co, us(t, "59")
}

func gerar(t *testing.T, s *Store, co Coordenador, rotulo string, validade time.Duration) LinkGerado {
	t.Helper()
	l, err := s.GerarLink(context.Background(), &co, us(t, "59"), rotulo, validade)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestGerarLinkGuardaSoOHashESemCodigoNoDiario(t *testing.T) {
	s, co, _ := ambienteLink(t)
	a := gerar(t, s, co, "cliente A", 0)
	b := gerar(t, s, co, "  cliente   B ", 24*time.Hour)
	if len(a.Codigo) != TamanhoDoCodigo || !CodigoLinkValido(a.Codigo) || a.Codigo == b.Codigo || a.Estado != LinkAtivo || a.ExpiraEm != nil || b.ExpiraEm == nil || b.Rotulo != "cliente B" {
		t.Fatalf("a = %+v, b = %+v", a, b)
	}
	// Varredura: nada do banco (tabela de links e diário) tem o código em claro.
	for _, c := range []string{a.Codigo, b.Codigo} {
		for _, q := range []string{"SELECT group_concat(hash || rotulo || criado_em, '|') FROM spec_link", "SELECT group_concat(detalhe || tipo, '|') FROM evento"} {
			var todo string
			if err := s.db.QueryRow(q).Scan(&todo); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(todo, c) || strings.Contains(todo, HashCodigoLink(c)) && strings.Contains(q, "evento") {
				t.Fatalf("o código ou o hash vazou em %q", q)
			}
		}
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM spec_link WHERE hash = ?`, HashCodigoLink(c)).Scan(&n); err != nil || n != 1 {
			t.Fatalf("hash de %q: %d, %v", c, n, err)
		}
	}
	es := eventosDoTipo(t, s, "spec.link.gerar")
	if len(es) != 2 {
		t.Fatalf("eventos = %+v", es)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(es[0].Detalhe), &d); err != nil || d["rotulo"] != "cliente A" {
		t.Fatalf("detalhe = %s, %v", es[0].Detalhe, err)
	}
}

func TestGerarLinkRecusaEntradaInvalida(t *testing.T) {
	ctx := context.Background()
	s, co, _ := ambienteLink(t)
	for nome, f := range map[string]func() error{
		"rótulo vazio":    func() error { _, e := s.GerarLink(ctx, &co, us(t, "59"), "  ", 0); return e },
		"rótulo longo":    func() error { _, e := s.GerarLink(ctx, &co, us(t, "59"), strings.Repeat("a", 81), 0); return e },
		"rótulo controle": func() error { _, e := s.GerarLink(ctx, &co, us(t, "59"), "a\x1bb", 0); return e },
		"validade neg":    func() error { _, e := s.GerarLink(ctx, &co, us(t, "59"), "a", -time.Hour); return e },
		"validade enorme": func() error { _, e := s.GerarLink(ctx, &co, us(t, "59"), "a", MaxValidadeLink+time.Hour); return e },
	} {
		if err := f(); output.AsError(err).Exit != output.ExitUsage {
			t.Errorf("%s: err = %v", nome, err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM spec_link`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("links = %d, %v", n, err)
	}
	// Sem spec registrada, não há o que compartilhar.
	onda(t, s, co, NovaOnda{Titulo: "o2", US: []refs.US{us(t, "60")}})
	if _, err := s.GerarLink(ctx, &co, us(t, "60"), "a", 0); output.AsError(err).Exit != output.ExitUsage {
		t.Fatalf("sem spec: %v", err)
	}
}

// O mesmo erro, byte a byte, para inexistente, revogado e expirado.
func TestSpecPorHashDa404IdenticaParaInexistenteRevogadoEExpirado(t *testing.T) {
	ctx := context.Background()
	s, co, u := ambienteLink(t)
	base := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return base }
	ativo := gerar(t, s, co, "ativo", 0)
	revogado := gerar(t, s, co, "revogado", 0)
	expira := gerar(t, s, co, "expira", time.Hour)
	if _, err := s.RevogarLink(ctx, &co, u, revogado.ID); err != nil {
		t.Fatal(err)
	}
	if sp, err := s.SpecPorHash(ctx, HashCodigoLink(ativo.Codigo)); err != nil || sp.Estado != PublicoEmValidacao {
		t.Fatalf("ativo: %+v, %v", sp, err)
	}
	if _, err := s.SpecPorHash(ctx, HashCodigoLink(expira.Codigo)); err != nil {
		t.Fatalf("antes de expirar: %v", err)
	}
	s.Now = func() time.Time { return base.Add(time.Hour) } // no instante da validade já expirou
	var erros []*output.Error
	for _, c := range []string{"nunca existiu", revogado.Codigo, expira.Codigo} {
		_, err := s.SpecPorHash(ctx, HashCodigoLink(c))
		e := output.AsError(err)
		if err == nil || e.Exit != output.ExitNotFound {
			t.Fatalf("%q: %v", c, err)
		}
		erros = append(erros, e)
	}
	if *erros[0] != *erros[1] || *erros[1] != *erros[2] {
		t.Fatalf("os três 404 diferem: %+v", erros)
	}
	ls, _ := s.ListarLinks(ctx, &co, u)
	if len(ls) != 3 || ls[0].Estado != LinkAtivo || ls[1].Estado != LinkRevogado || ls[2].Estado != LinkExpirado {
		t.Fatalf("listar: %+v", ls)
	}
}

func TestRevogarLinkEDefinitivoEGerarDeNovoCriaCodigoNovo(t *testing.T) {
	ctx := context.Background()
	s, co, u := ambienteLink(t)
	a := gerar(t, s, co, "a", 0)
	r, err := s.RevogarLink(ctx, &co, u, a.ID)
	if err != nil || r.Estado != LinkRevogado || r.RevogadoEm == nil {
		t.Fatalf("r = %+v, %v", r, err)
	}
	if _, err := s.RevogarLink(ctx, &co, u, a.ID); output.AsError(err).Code != "link_ja_revogado" {
		t.Fatalf("revogar de novo: %v", err)
	}
	if _, err := s.RevogarLink(ctx, &co, u, 9999); output.AsError(err).Exit != output.ExitNotFound {
		t.Fatalf("id inexistente: %v", err)
	}
	b := gerar(t, s, co, "a", 0)
	if b.Codigo == a.Codigo || b.ID == a.ID {
		t.Fatal("gerar depois de revogar devia criar código novo")
	}
	if _, err := s.SpecPorHash(ctx, HashCodigoLink(a.Codigo)); err == nil {
		t.Fatal("o código revogado voltou a abrir")
	}
	if _, err := s.SpecPorHash(ctx, HashCodigoLink(b.Codigo)); err != nil {
		t.Fatal(err)
	}
	if len(eventosDoTipo(t, s, "spec.link.revogar")) != 1 {
		t.Fatal("a revogação gera um evento")
	}
}

func TestRevogarLinkDeOutraUSENaoEncontrado(t *testing.T) {
	ctx := context.Background()
	s, co, _ := ambienteLink(t)
	onda(t, s, co, NovaOnda{Titulo: "o2", US: []refs.US{us(t, "60")}})
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "60"), "docs/specs/x/tg-60-z", "https://github.com/BasisTI/orq/pull/45", false); err != nil {
		t.Fatal(err)
	}
	a := gerar(t, s, co, "a", 0)
	if _, err := s.RevogarLink(ctx, &co, us(t, "60"), a.ID); output.AsError(err).Exit != output.ExitNotFound {
		t.Fatalf("link de outra US: %v", err)
	}
	if _, err := s.SpecPorHash(ctx, HashCodigoLink(a.Codigo)); err != nil {
		t.Fatalf("o link seguiu ativo: %v", err)
	}
}

func TestMarcarAcessoContaSemEventoNoDiario(t *testing.T) {
	ctx := context.Background()
	s, co, u := ambienteLink(t)
	a := gerar(t, s, co, "a", 0)
	h := HashCodigoLink(a.Codigo)
	antes := len(eventos(t, s))
	for i := 0; i < 3; i++ {
		if err := s.MarcarAcesso(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MarcarAcesso(ctx, HashCodigoLink("inexistente")); err != nil {
		t.Fatal(err)
	}
	ls, _ := s.ListarLinks(ctx, &co, u)
	if len(ls) != 1 || ls[0].Acessos != 3 || ls[0].UltimoAcesso == nil {
		t.Fatalf("ls = %+v", ls)
	}
	if len(eventos(t, s)) != antes {
		t.Fatal("o acesso não gera evento no diário")
	}
	// Revogado não conta.
	if _, err := s.RevogarLink(ctx, &co, u, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarcarAcesso(ctx, h); err != nil {
		t.Fatal(err)
	}
	if ls, _ = s.ListarLinks(ctx, &co, u); ls[0].Acessos != 3 {
		t.Fatalf("link revogado contou acesso: %+v", ls)
	}
}

func TestSpecPublicaNaoTrazNadaAlemDoConteudo(t *testing.T) {
	ctx := context.Background()
	s, co, _ := ambienteLink(t)
	a := gerar(t, s, co, "a", 0)
	h := HashCodigoLink(a.Codigo)
	if sp, err := s.SpecPorHash(ctx, h); err != nil || sp.Arquivos != nil || sp.Versao != 1 || sp.SubstituidaEm != "" || sp.Titulo != "" {
		t.Fatalf("antes da leitura: %+v, %v", sp, err)
	}
	if err := s.GravarVersaoSpec(ctx, refs.US{Projeto: "proj", Ref: 59}, "docs/specs/x/tg-59-y", mrSpec, "aaaa", true, arquivosCache("v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.GravarCache(ctx, FonteTaiga, "proj/59", ValorTaiga{Titulo: "Título da US", Status: "In progress"}, nil); err != nil {
		t.Fatal(err)
	}
	sp, err := s.SpecPorHash(ctx, h)
	if err != nil || len(sp.Arquivos) != 3 || sp.LidaEm == "" || sp.Titulo != "Título da US" {
		t.Fatalf("sp = %+v, %v", sp, err)
	}
	// O tipo não tem campo para ref, SHA, MR nem pasta: a conferência é estrutural.
	b, _ := json.Marshal(sp)
	for _, proibido := range []string{"aaaa", mrSpec, "proj", "pull/44"} {
		if strings.Contains(string(b), proibido) {
			t.Fatalf("SpecPublica traz %q: %s", proibido, b)
		}
	}
}

func TestEstadoPublicoEntregueExigeMRDoCodigoMesclada(t *testing.T) {
	ctx := context.Background()
	s, co, _ := ambienteLink(t)
	a := gerar(t, s, co, "a", 0)
	h := HashCodigoLink(a.Codigo)
	estado := func() string {
		t.Helper()
		sp, err := s.SpecPorHash(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		return sp.Estado
	}
	if e := estado(); e != PublicoEmValidacao {
		t.Fatalf("registrada: %s", e)
	}
	p, err := s.AbrirValidacao(ctx, &co, us(t, "59"), "sha-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if e := estado(); e != PublicoEmValidacao {
		t.Fatalf("em validação: %s", e)
	}
	responderValidacao(t, s, co, p.Numero, OpcaoAprovar)
	if e := estado(); e != PublicoAprovada {
		t.Fatalf("aprovada: %s", e)
	}
	// Mudança além do decisoes.md depois da aprovação volta a "em validação".
	conf := ConferenciaSpec{SHAAprovado: "sha-1", SHACabeca: "sha-2", MudouNaPasta: []string{"spec.md"}}
	if err := s.GravarConferenciaSpec(ctx, refs.US{Projeto: "proj", Ref: 59}, &conf, nil); err != nil {
		t.Fatal(err)
	}
	if e := estado(); e != PublicoEmValidacao {
		t.Fatalf("mudou: %s", e)
	}
	conf.PodeMesclar, conf.MudouNaPasta = true, nil
	if err := s.GravarConferenciaSpec(ctx, refs.US{Projeto: "proj", Ref: 59}, &conf, nil); err != nil {
		t.Fatal(err)
	}
	// A MR da spec mesclada: aprovada, não entregue.
	if err := s.GravarMR(ctx, mrSpec, ValorMR{Estado: "mesclada"}, nil); err != nil {
		t.Fatal(err)
	}
	if e := estado(); e != PublicoAprovada {
		t.Fatalf("MR da spec mesclada: %s", e)
	}
	// A onda "até plano" encerrada e a US concluída, sem código mesclado: segue "aprovada".
	if _, _, err := s.AtualizarStatus(ctx, &co, us(t, "59"), MudancaStatus{Status: "done"}); err != nil {
		t.Fatal(err)
	}
	var ondaID int64
	if err := s.db.QueryRow(`SELECT onda_id FROM us WHERE ref = 59`).Scan(&ondaID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EncerrarOnda(ctx, co.Nome, ondaID); err != nil {
		t.Fatal(err)
	}
	if e := estado(); e != PublicoAprovada {
		t.Fatalf("US done e onda encerrada sem código mesclado: %s", e)
	}
	// MR do código ligada, ainda aberta: aprovada. Mesclada: entregue.
	if _, _, err := s.AdicionarMR(ctx, &co, us(t, "59"), mrCodigo); err != nil {
		t.Fatal(err)
	}
	if err := s.GravarMR(ctx, mrCodigo, ValorMR{Estado: "pronta"}, nil); err != nil {
		t.Fatal(err)
	}
	if e := estado(); e != PublicoAprovada {
		t.Fatalf("código aberto: %s", e)
	}
	if err := s.GravarMR(ctx, mrCodigo, ValorMR{Estado: "mesclada"}, nil); err != nil {
		t.Fatal(err)
	}
	if e := estado(); e != PublicoEntregue {
		t.Fatalf("código mesclado: %s", e)
	}
}

func TestEstadoPublicoTabela(t *testing.T) {
	for _, c := range []struct {
		spec     string
		entregue bool
		quer     string
	}{
		{SpecRegistrada, false, PublicoEmValidacao}, {SpecEmValidacao, false, PublicoEmValidacao}, {SpecMudou, false, PublicoEmValidacao},
		{SpecAprovada, false, PublicoAprovada}, {SpecMesclada, false, PublicoAprovada},
		{SpecAprovada, true, PublicoEntregue}, {SpecMesclada, true, PublicoEntregue},
	} {
		if got := EstadoPublico(c.spec, c.entregue); got != c.quer {
			t.Errorf("%s/%v = %s, quero %s", c.spec, c.entregue, got, c.quer)
		}
	}
}

func TestSubstituirIncrementaAVersaoMantemOsLinksEContaOsAtivos(t *testing.T) {
	ctx := context.Background()
	s, co, u := ambienteLink(t)
	if sp, _ := s.LerSpec(ctx, &co, u); sp.Versao != 1 {
		t.Fatalf("versão inicial = %d", sp.Versao)
	}
	a := gerar(t, s, co, "a", 0)
	b := gerar(t, s, co, "b", 0)
	gerar(t, s, co, "c", time.Hour)
	if _, err := s.RevogarLink(ctx, &co, u, b.ID); err != nil {
		t.Fatal(err)
	}
	r, err := s.RegistrarSpec(ctx, &co, u, "docs/specs/x/tg-59-novo", mrSpec, true)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Substituiu || r.Spec.Versao != 2 || r.LinksAtivos != 2 {
		t.Fatalf("r = %+v", r)
	}
	if _, err := s.SpecPorHash(ctx, HashCodigoLink(a.Codigo)); err != nil {
		t.Fatalf("o link devia seguir: %v", err)
	}
	sp, err := s.SpecPorHash(ctx, HashCodigoLink(a.Codigo))
	if err != nil || sp.Versao != 2 || sp.SubstituidaEm != r.Spec.AtualizadaEm {
		t.Fatalf("sp = %+v, %v", sp, err)
	}
	es := eventosDoTipo(t, s, "spec.registrar")
	var d map[string]any
	if err := json.Unmarshal([]byte(es[len(es)-1].Detalhe), &d); err != nil || d["versao"] != float64(2) || d["links_ativos"] != float64(2) {
		t.Fatalf("detalhe = %s", es[len(es)-1].Detalhe)
	}
	if r, err = s.RegistrarSpec(ctx, &co, u, "docs/specs/x/tg-59-novo2", mrSpec, true); err != nil || r.Spec.Versao != 3 {
		t.Fatalf("r = %+v, %v", r, err)
	}
}

// A migration calcula a versão dos registros existentes pelas substituições do diário.
func TestMigracaoCalculaAVersaoPelasSubstituicoes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	n := 0
	s.Now = func() time.Time { n++; return base.Add(time.Duration(n) * time.Second) }
	co := coord(t, s, "a", "p1", "proj")
	onda(t, s, co, NovaOnda{Titulo: "o", US: []refs.US{us(t, "59"), us(t, "60")}})
	for i, c := range []string{"docs/a", "docs/b", "docs/c"} {
		if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), c, mrSpec, i > 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "60"), "docs/z", "https://github.com/BasisTI/orq/pull/45", false); err != nil {
		t.Fatal(err)
	}
	u := refs.US{Projeto: "proj", Ref: 59}
	if err := s.GravarVersaoSpec(ctx, u, "docs/c", mrSpec, "aaaa", true, arquivosCache("v")); err != nil {
		t.Fatal(err)
	}
	// Volta ao banco da 0012: sem a tabela de links e sem as colunas novas.
	for _, q := range []string{"DROP TABLE spec_link", "ALTER TABLE spec DROP COLUMN versao", "ALTER TABLE spec_versao DROP COLUMN caminho", "ALTER TABLE spec_versao DROP COLUMN mr_url", "DROP TABLE banco_meta", "PRAGMA user_version = 12"} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	_ = s.Close()
	var m Migracao
	s, err = OpenCom(ctx, dir, Opcoes{AntesDeMigrar: func(x Migracao) { m = x }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if m.De != 12 || m.Para != 13 || m.EscritorMinimo != 13 || m.LeitorMinimo != 4 {
		t.Fatalf("migração = %+v", m)
	}
	for ref, quer := range map[int]int{59: 3, 60: 1} {
		var v int
		if err := s.db.QueryRow(`SELECT versao FROM spec WHERE ref = ?`, ref).Scan(&v); err != nil || v != quer {
			t.Errorf("TG-%d: versão %d, quero %d (%v)", ref, v, quer, err)
		}
	}
	// A versão do cache existente passa a ser do registro atual.
	if sc, err := s.LerSpecCache(ctx, u, "aaaa"); err != nil || sc.Versao == nil {
		t.Fatalf("cache depois da migration: %+v, %v", sc, err)
	}
}

// Corrida entre o daemon e o --substituir: o daemon leu a spec velha e só grava depois da troca.
func TestGravarVersaoDoRegistroVelhoNaoGrava(t *testing.T) {
	ctx := context.Background()
	s, co, u := ambienteLink(t)
	ur := refs.US{Projeto: "proj", Ref: 59}
	velho := "docs/specs/x/tg-59-y"
	if _, err := s.RegistrarSpec(ctx, &co, u, "docs/specs/x/tg-59-novo", mrSpec, true); err != nil {
		t.Fatal(err)
	}
	if err := s.GravarVersaoSpec(ctx, ur, velho, mrSpec, "velha", true, arquivosCache("da spec velha")); err != nil {
		t.Fatal(err)
	}
	if sc, _ := s.LerSpecCache(ctx, ur, ""); sc.Versao != nil || sc.Leitura != nil {
		t.Fatalf("gravou conteúdo do registro velho: %+v", sc)
	}
	// Outra MR, mesma pasta: também é outro registro.
	if err := s.GravarVersaoSpec(ctx, ur, "docs/specs/x/tg-59-novo", "https://github.com/BasisTI/orq/pull/99", "outra-mr", true, arquivosCache("x")); err != nil {
		t.Fatal(err)
	}
	if sc, _ := s.LerSpecCache(ctx, ur, ""); sc.Versao != nil {
		t.Fatalf("gravou conteúdo de outra MR: %+v", sc)
	}
	if err := s.GravarVersaoSpec(ctx, ur, "docs/specs/x/tg-59-novo", mrSpec, "nova", true, arquivosCache("da spec nova")); err != nil {
		t.Fatal(err)
	}
	if sc, _ := s.LerSpecCache(ctx, ur, ""); sc.Versao == nil || sc.Versao.Arquivos[0].Conteudo != "da spec nova" {
		t.Fatalf("a versão nova devia gravar: %+v", sc)
	}
}

// Cache que sobrou de outro registro (binário que não limpa o cache): não é lido nem contado.
func TestVersaoDeOutroRegistroNaoContaNemAparece(t *testing.T) {
	ctx := context.Background()
	s, _, _ := ambienteLink(t)
	ur := refs.US{Projeto: "proj", Ref: 59}
	if err := s.GravarVersaoSpec(ctx, ur, "docs/specs/x/tg-59-y", mrSpec, "boa", true, arquivosCache("boa")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO spec_versao (projeto, ref, sha, lida_em, caminho, mr_url) VALUES ('proj', 59, 'velha', 'x', 'docs/outra', ?)`, mrSpec); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO spec_versao (projeto, ref, sha, lida_em) VALUES ('proj', 59, 'semregistro', 'x')`); err != nil {
		t.Fatal(err)
	}
	ls, err := s.SpecsParaLer(ctx)
	if err != nil || len(ls) != 1 || !ls[0].Versoes["boa"] || ls[0].Versoes["velha"] || ls[0].Versoes["semregistro"] {
		t.Fatalf("ls = %+v, %v", ls, err)
	}
	if sc, _ := s.LerSpecCache(ctx, ur, "velha"); sc.Versao != nil {
		t.Fatal("LerSpecCache devolveu versão de outro registro")
	}
	// A poda leva o que não é do registro atual.
	if err := s.MarcarLeituraSpec(ctx, ur, ""); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM spec_versao`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("versões depois da poda = %d, %v", n, err)
	}
}

func TestListarLinksSemSpec(t *testing.T) {
	s, co := ambienteSpec(t)
	if _, err := s.ListarLinks(context.Background(), &co, us(t, "59")); output.AsError(err).Exit != output.ExitNotFound {
		t.Fatalf("err = %v", err)
	}
}
