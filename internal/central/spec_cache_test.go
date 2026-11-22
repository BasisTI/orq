package central

import (
	"context"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/refs"
)

func arquivosCache(conteudo string) []ArquivoSpec {
	return []ArquivoSpec{
		{Caminho: "docs/specs/x/tg-59-y/spec.md", Papel: PapelPasta, Ordem: 0, Conteudo: conteudo, Tamanho: int64(len(conteudo))},
		{Caminho: "docs/specs/x/README.md", Papel: PapelEpico, Ordem: 1, Conteudo: "épico", Tamanho: 6},
		{Caminho: "docs/specs/x/tg-59-y/plano.md", Papel: PapelPasta, Ordem: 2, GrandeDemais: true, Tamanho: LimiteArquivoSpec + 1},
	}
}

func TestSpecCacheGravaELeVersaoAtual(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	u := refs.US{Projeto: "proj", Ref: 59} // a US já resolvida, como o daemon a recebe
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/specs/x/tg-59-y", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	sc, err := s.LerSpecCache(ctx, u, "")
	if err != nil || sc.Versao != nil || sc.Leitura != nil || sc.Spec.Caminho == "" {
		t.Fatalf("antes da leitura: %+v, %v", sc, err)
	}
	if err := s.GravarVersaoSpec(ctx, u, "docs/specs/x/tg-59-y", mrSpec, "aaaa", true, arquivosCache("v1")); err != nil {
		t.Fatal(err)
	}
	sc, err = s.LerSpecCache(ctx, u, "")
	if err != nil || sc.Versao == nil || sc.Versao.SHA != "aaaa" || sc.Versao.LidaEm == "" || *sc.Leitura.SHA != "aaaa" {
		t.Fatalf("sc = %+v, %v", sc, err)
	}
	as := sc.Versao.Arquivos
	if len(as) != 3 || as[0].Conteudo != "v1" || as[1].Papel != PapelEpico || !as[2].GrandeDemais || as[2].Conteudo != "" || as[2].Tamanho != LimiteArquivoSpec+1 {
		t.Fatalf("arquivos = %+v", as)
	}
	if eventos := eventosDoTipo(t, s, "spec.cache"); len(eventos) != 0 {
		t.Fatal("o cache não gera evento no diário")
	}
	// Spec sem registro: naoEncontrado.
	if _, err := s.LerSpecCache(ctx, us(t, "60"), ""); err == nil {
		t.Fatal("US sem spec devia dar erro")
	}
}

func TestSpecCacheMantemSoAAtualEAsDeValidacaoAberta(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	u := refs.US{Projeto: "proj", Ref: 59} // a US já resolvida, como o daemon a recebe
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/specs/x/tg-59-y", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	p, err := s.AbrirValidacao(ctx, &co, us(t, "59"), "val1", "")
	if err != nil {
		t.Fatal(err)
	}
	ha := func(sha string) bool {
		sc, err := s.LerSpecCache(ctx, u, sha)
		if err != nil {
			t.Fatal(err)
		}
		return sc.Versao != nil
	}
	for _, sha := range []string{"cab1", "val1"} {
		if err := s.GravarVersaoSpec(ctx, u, "docs/specs/x/tg-59-y", mrSpec, sha, sha == "cab1", arquivosCache("de "+sha)); err != nil {
			t.Fatal(err)
		}
	}
	// A cabeça anda: a versão da validação aberta fica, a cabeça velha sai.
	if err := s.GravarVersaoSpec(ctx, u, "docs/specs/x/tg-59-y", mrSpec, "cab2", true, arquivosCache("de cab2")); err != nil {
		t.Fatal(err)
	}
	if ha("cab1") || !ha("val1") || !ha("cab2") {
		t.Fatalf("cab1=%v val1=%v cab2=%v", ha("cab1"), ha("val1"), ha("cab2"))
	}
	if sc, _ := s.LerSpecCache(ctx, u, "val1"); sc.Versao.Arquivos[0].Conteudo != "de val1" {
		t.Fatal("a versão da validação foi substituída")
	}
	// Reescrever a versão atual com o mesmo SHA não duplica arquivos.
	if err := s.GravarVersaoSpec(ctx, u, "docs/specs/x/tg-59-y", mrSpec, "cab2", true, arquivosCache("de cab2 de novo")); err != nil {
		t.Fatal(err)
	}
	if sc, _ := s.LerSpecCache(ctx, u, ""); len(sc.Versao.Arquivos) != 3 || sc.Versao.Arquivos[0].Conteudo != "de cab2 de novo" {
		t.Fatalf("arquivos = %+v", sc.Versao.Arquivos)
	}
	// Respondida mas ainda aberta (aberta = 1 até o coordenador resolver), a validação segue
	// mostrando a spec na versão dela: a versão fica (D7 da orq#60).
	op := OpcaoPedirAjustes
	if _, err := s.ResponderPendencia(ctx, &co, refs.Pend{US: u, Numero: p.Numero}, Resposta{Opcao: &op, Por: "ana"}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarcarLeituraSpec(ctx, u, ""); err != nil {
		t.Fatal(err)
	}
	if !ha("val1") {
		t.Fatal("validação respondida e ainda aberta: a versão devia ter ficado")
	}
	if ls, _ := s.SpecsParaLer(ctx); len(ls) != 1 || len(ls[0].Validacoes) != 1 || ls[0].Validacoes[0] != "val1" {
		t.Fatalf("a validação respondida e aberta segue pedindo a versão: %+v", ls)
	}
	// Resolvida (aberta = 0), a versão dela sai na próxima gravação.
	if _, err := s.ResolverPendencia(ctx, &co, refs.Pend{US: u, Numero: p.Numero}, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.MarcarLeituraSpec(ctx, u, ""); err != nil {
		t.Fatal(err)
	}
	if ha("val1") {
		t.Fatal("validação resolvida: a versão devia ter sido descartada")
	}
	// A atual nunca é descartada.
	if !ha("cab2") {
		t.Fatal("a atual saiu")
	}
}

func TestSpecCacheErroMantemOConteudoEMarcarLimpa(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	u := refs.US{Projeto: "proj", Ref: 59} // a US já resolvida, como o daemon a recebe
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/specs/x/tg-59-y", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	if err := s.GravarVersaoSpec(ctx, u, "docs/specs/x/tg-59-y", mrSpec, "aaaa", true, arquivosCache("v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.GravarErroLeituraSpec(ctx, u, Erro{Codigo: "falha_comando", Mensagem: strings.Repeat("x", 900)}); err != nil {
		t.Fatal(err)
	}
	sc, _ := s.LerSpecCache(ctx, u, "")
	if sc.Versao == nil || sc.Versao.Arquivos[0].Conteudo != "v1" || sc.Leitura.Erro == nil || sc.Leitura.Erro.Codigo != "falha_comando" || len(sc.Leitura.Erro.Mensagem) > 600 || *sc.Leitura.SHA != "aaaa" {
		t.Fatalf("sc = %+v", sc)
	}
	if err := s.MarcarLeituraSpec(ctx, u, ""); err != nil {
		t.Fatal(err)
	}
	if sc, _ = s.LerSpecCache(ctx, u, ""); sc.Leitura.Erro != nil || sc.Versao == nil {
		t.Fatalf("sc = %+v", sc)
	}
	// Apontar a atual para uma versão que não existe não faz nada.
	if err := s.MarcarLeituraSpec(ctx, u, "inexistente"); err != nil {
		t.Fatal(err)
	}
	if sc, _ = s.LerSpecCache(ctx, u, ""); *sc.Leitura.SHA != "aaaa" {
		t.Fatalf("sc = %+v", sc.Leitura)
	}
}

func TestSubstituirASpecApagaOCache(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	u := refs.US{Projeto: "proj", Ref: 59} // a US já resolvida, como o daemon a recebe
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/specs/x/tg-59-y", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	if err := s.GravarVersaoSpec(ctx, u, "docs/specs/x/tg-59-y", mrSpec, "aaaa", true, arquivosCache("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/specs/x/tg-59-z", mrSpec, true); err != nil {
		t.Fatal(err)
	}
	if sc, _ := s.LerSpecCache(ctx, u, ""); sc.Versao != nil || sc.Leitura != nil {
		t.Fatalf("o cache era da spec velha: %+v", sc)
	}
}

func TestSpecsParaLer(t *testing.T) {
	ctx := context.Background()
	s, co := ambienteSpec(t)
	u := refs.US{Projeto: "proj", Ref: 59} // a US já resolvida, como o daemon a recebe
	if _, err := s.RegistrarSpec(ctx, &co, us(t, "59"), "docs/specs/x/tg-59-y", mrSpec, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AbrirValidacao(ctx, &co, us(t, "59"), "val1", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.GravarVersaoSpec(ctx, u, "docs/specs/x/tg-59-y", mrSpec, "cab1", true, arquivosCache("v")); err != nil {
		t.Fatal(err)
	}
	ls, err := s.SpecsParaLer(ctx)
	if err != nil || len(ls) != 1 {
		t.Fatalf("ls = %+v, %v", ls, err)
	}
	l := ls[0]
	if l.SHAAtual != "cab1" || !l.Versoes["cab1"] || len(l.Validacoes) != 1 || l.Validacoes[0] != "val1" || l.Mesclada || l.Caminho != "docs/specs/x/tg-59-y" {
		t.Fatalf("l = %+v", l)
	}
}
