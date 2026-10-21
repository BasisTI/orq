package central

// Testes adversariais da orq #19: corrida da mescla do GravarMR com outro
// escritor e compatibilidade do valor em cache com o binário anterior.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/BasisTI/orq/internal/refs"
)

// Um escritor grava valores novos (consulta extra boa) enquanto outro grava a falha da
// consulta extra (que lê o valor anterior dentro da transação e mescla). BEGIN IMMEDIATE
// serializa as duas: no fim, ou vale o valor fresco, ou o último bom com a falha — nunca
// pipeline/aprovação perdidos sem falha visível.
func TestSituacaoGravarMRMesclaConcorrente(t *testing.T) {
	ctx := context.Background()
	s := novoStore(t)
	c := coord(t, s, "alfa", "wA:p1", "proj")
	onda(t, s, c, NovaOnda{Titulo: "o", US: []refs.US{us(t, "proj/1")}})
	const url = "https://github.com/BasisTI/orq/pull/1"
	if _, _, err := s.AdicionarMR(ctx, &c, us(t, "proj/1"), url); err != nil {
		t.Fatal(err)
	}
	if err := s.GravarMR(ctx, url, ValorMR{Estado: "aberta", Pipeline: "sucesso", Aprovacao: "aprovada"}, nil); err != nil {
		t.Fatal(err)
	}
	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			errs <- s.GravarMR(ctx, url, ValorMR{Estado: "aberta", Pipeline: "falhou", Aprovacao: "pendente"}, nil)
		}()
		go func() {
			defer wg.Done()
			errs <- s.GravarMR(ctx, url, ValorMR{Estado: "aberta", SituacaoErro: &Erro{Codigo: "rede", Mensagem: "x"}}, nil)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("GravarMR concorrente: %v", err)
		}
	}
	snap, err := s.Snapshot(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	m := snap.Coordenadores[0].Ondas[0].US[0].MRs[0]
	if m.Pipeline == nil || m.Aprovacao == nil {
		t.Fatalf("a corrida perdeu pipeline/aprovação: %+v", m)
	}
	if m.SituacaoErro == nil {
		// O último escritor foi o de sucesso: têm de ser os valores frescos dele.
		if *m.Pipeline != "falhou" || *m.Aprovacao != "pendente" {
			t.Errorf("sem falha visível, os valores têm de ser os frescos: %+v", m)
		}
	} else if m.SituacaoEm == nil {
		t.Errorf("com falha visível, a idade dos valores tem de estar lá: %+v", m)
	}
}

// O valor em cache com os campos novos é lido pelo formato do binário anterior (que ignora o
// que não conhece), e o valor antigo é lido pelo formato novo (campos ausentes ficam zerados).
func TestSituacaoCacheDaMRSeriaCompativelComBinarioAnterior(t *testing.T) {
	novo := ValorMR{Estado: "pronta", WebURL: "https://x", Pipeline: "sucesso", Aprovacao: "aprovada",
		SituacaoEm: "2026-10-21T12:00:00Z", SituacaoErro: &Erro{Codigo: "rede", Mensagem: "m", Em: "2026-10-21T12:01:00Z"}}
	raw, err := json.Marshal(novo)
	if err != nil {
		t.Fatal(err)
	}
	// Leitor do binário anterior: o struct da US #16, sem os dois campos.
	var antigo struct {
		Estado    string `json:"estado"`
		WebURL    string `json:"web_url"`
		Pipeline  string `json:"pipeline,omitempty"`
		Aprovacao string `json:"aprovacao,omitempty"`
	}
	if err := json.Unmarshal(raw, &antigo); err != nil {
		t.Fatalf("leitor anterior não leu o cache novo: %v", err)
	}
	if antigo.Estado != "pronta" || antigo.Pipeline != "sucesso" || antigo.Aprovacao != "aprovada" {
		t.Errorf("leitor anterior: %+v", antigo)
	}
	// Cache escrito pelo binário anterior lido por esta versão.
	var v ValorMR
	if err := json.Unmarshal([]byte(`{"estado":"pronta","web_url":"https://x","pipeline":"sucesso","aprovacao":"aprovada"}`), &v); err != nil {
		t.Fatalf("esta versão não leu o cache antigo: %v", err)
	}
	if v.Estado != "pronta" || v.Pipeline != "sucesso" || v.SituacaoEm != "" || v.SituacaoErro != nil {
		t.Errorf("cache antigo: %+v", v)
	}
}
