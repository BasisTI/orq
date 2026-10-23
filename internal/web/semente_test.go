package web

import (
	"context"
	"fmt"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// Dados SINTÉTICOS no formato do snapshot (nada aqui vem de uma Central real): a Central do
// protótipo aprovado, com 9 ondas de 6 coordenadores e 4 projetos. Serve ao BenchmarkVivo e ao
// teste de navegador.

type mrSemente struct {
	n                    int
	estado               string
	pipeline, aprovacao  string
	extraFalhou, semInfo bool
}

type usSemente struct {
	projeto string
	ref     int
	titulo  string
	taiga   string
	status  string
	agente  string // estado do agente; vazio: sem pane
	mrs     []mrSemente
	pends   []string // tipo de cada pendência aberta
	respond int      // quantas das pendências ficam respondidas (as primeiras)
	dep     int      // ref da US de que esta depende (mesmo projeto); 0 nenhuma
}

type ondaSemente struct {
	coord, projeto, titulo, ate, ordem string
	us                                 []usSemente
}

var ondasSemente = []ondaSemente{
	{"aurora", "catalogo", "Busca facetada do catálogo com filtros por categoria, marca e faixa de preço, e ordenação por relevância", "merge", "estrita", []usSemente{
		{"catalogo", 401, "Índice de busca com campos facetáveis", "Done", "done", "done", []mrSemente{{n: 118, estado: "mesclada", pipeline: "sucesso", aprovacao: "aprovada"}}, nil, 0, 0},
		{"catalogo", 402, "Endpoint de busca com paginação por cursor", "Done", "done", "done", []mrSemente{{n: 121, estado: "mesclada", pipeline: "sucesso", aprovacao: "aprovada"}}, nil, 0, 401},
		{"catalogo", 404, "Filtro por faixa de preço com moeda e promoção ativa", "Ready for test", "done", "idle", []mrSemente{{n: 58, estado: "pronta", pipeline: "sucesso", aprovacao: "aprovada"}}, []string{"decisao", "duvida"}, 1, 402},
		{"catalogo", 405, "Ordenação por relevância com peso configurável por categoria", "In progress", "rev", "working", []mrSemente{{n: 59, estado: "aberta", pipeline: "falhou", aprovacao: "pendente"}, {n: 122, estado: "aberta", pipeline: "rodando", aprovacao: "pendente"}}, []string{"autorizacao"}, 0, 404},
		{"catalogo", 406, "Documentar os parâmetros de busca no portal de desenvolvedores", "In progress", "exec", "blocked", nil, nil, 0, 405},
	}},
	{"aurora", "catalogo", "Importação em lote de produtos por planilha", "mr-pronta", "livre", []usSemente{
		{"catalogo", 388, "Leitura e validação da planilha", "Done", "done", "done", []mrSemente{{n: 49, estado: "pronta", pipeline: "sucesso", aprovacao: "aprovada"}}, nil, 0, 0},
		{"catalogo", 389, "Relatório de linhas rejeitadas", "Ready for test", "done", "done", []mrSemente{{n: 50, estado: "pronta", pipeline: "sucesso", aprovacao: "nao_exigida"}}, nil, 0, 0},
	}},
	{"bussola", "faturamento", "Emissão de notas fiscais de serviço com retentativa automática e fila de reprocessamento", "merge", "livre", []usSemente{
		{"faturamento", 512, "Cliente do provedor de notas com retentativa exponencial", "Done", "done", "done", []mrSemente{{n: 203, estado: "mesclada", pipeline: "sucesso", aprovacao: "aprovada"}}, nil, 0, 0},
		{"faturamento", 513, "Fila de reprocessamento das notas rejeitadas", "In progress", "exec", "working", []mrSemente{{n: 207, estado: "aberta", pipeline: "rodando", aprovacao: "pendente"}}, []string{"duvida"}, 0, 512},
		{"faturamento", 514, "Tela de acompanhamento das notas em retentativa", "New", "wait", "", nil, nil, 0, 513},
	}},
	{"bussola", "agenda", "Lembretes de consulta por SMS e confirmação pelo paciente", "plano", "estrita", []usSemente{
		{"agenda", 221, "Modelo de mensagem e janela de envio", "In progress", "plan", "blocked", nil, nil, 0, 0},
		{"agenda", 222, "Confirmação pelo paciente por link único", "New", "wait", "", nil, nil, 0, 221},
	}},
	{"cais", "nuvem", "Migrar os clusters de homologação para a nova versão do Kubernetes e atualizar os complementos", "merge", "livre", []usSemente{
		{"nuvem", 77, "Atualizar o plano de controle do cluster de homologação", "Ready for test", "rev", "idle", []mrSemente{{n: 31, estado: "pronta", pipeline: "sucesso", aprovacao: "pendente"}}, nil, 0, 0},
		{"nuvem", 78, "Atualizar os grupos de nós e o rodízio", "In progress", "exec", "working", []mrSemente{{n: 32, estado: "draft", pipeline: "pendente", aprovacao: "pendente"}}, []string{"autorizacao", "decisao"}, 0, 77},
		{"nuvem", 79, "Conferir a compatibilidade dos complementos", "Done", "done", "done", []mrSemente{{n: 30, estado: "mesclada", pipeline: "sucesso", aprovacao: "aprovada"}}, nil, 0, 0},
	}},
	// A onda com US de dois projetos aparece nas duas abas.
	{"cais", "nuvem", "Painel único de custos por projeto", "mr-pronta", "livre", []usSemente{
		{"nuvem", 91, "Exportar o custo por namespace", "In progress", "exec", "working", []mrSemente{{n: 40, estado: "aberta", pipeline: "sucesso", aprovacao: "pendente", extraFalhou: true}}, nil, 0, 0},
		{"faturamento", 530, "Importar o custo no rateio mensal", "New", "wait", "", nil, nil, 0, 0},
	}},
	{"delta", "agenda", "Reagendamento em um toque", "mr-pronta", "livre", []usSemente{
		{"agenda", 240, "Botão de reagendar na confirmação", "Ready for test", "done", "done", []mrSemente{{n: 12, estado: "pronta", pipeline: "sucesso", aprovacao: "aprovada"}, {n: 13, semInfo: true}}, nil, 0, 0},
	}},
	{"eclipse", "catalogo", "Fotos de produto em várias resoluções", "merge", "livre", []usSemente{
		{"catalogo", 420, "Gerar miniaturas na carga", "In progress", "exec", "idle", []mrSemente{{n: 70, estado: "pronta", pipeline: "sucesso", aprovacao: "aprovada"}}, nil, 0, 0},
	}},
	{"farol", "nuvem", "Observabilidade dos serviços de homologação", "plano", "livre", []usSemente{
		{"nuvem", 100, "Painéis de latência e erro por serviço", "New", "wait", "", nil, nil, 0, 0},
	}},
}

// semear grava a Central sintética no banco. Devolve as pendências abertas (para os testes).
func semear(t testing.TB, ctx context.Context, st *central.Store) {
	t.Helper()
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	panes := map[string]central.PaneHerdr{}
	coords := map[string]central.Coordenador{}
	nPane := 1
	for _, os := range ondasSemente {
		co, tem := coords[os.coord]
		if !tem {
			pane := fmt.Sprintf("wA:p%d", nPane)
			nPane++
			var err error
			co, err = st.RegistrarCoord(ctx, central.Registro{Nome: os.coord, Pane: pane, Projeto: os.projeto})
			ok(err)
			coords[os.coord] = co
			panes[pane] = central.PaneHerdr{Status: "idle", Agente: "claude", Nome: "sintetico-" + os.coord, WorkspaceID: "wA"}
		}
		var us []refs.US
		var deps [][2]refs.US
		for _, u := range os.us {
			r := refs.US{Projeto: u.projeto, Ref: u.ref}
			us = append(us, r)
			if u.dep != 0 {
				deps = append(deps, [2]refs.US{r, {Projeto: u.projeto, Ref: u.dep}})
			}
		}
		onda, err := st.CriarOnda(ctx, co, central.NovaOnda{Titulo: os.titulo, Ate: os.ate, Ordem: os.ordem, US: us, Deps: deps})
		ok(err)
		_ = onda
		for _, u := range os.us {
			r := refs.US{Projeto: u.projeto, Ref: u.ref}
			mud := central.MudancaStatus{Status: u.status}
			if u.agente != "" {
				pane := fmt.Sprintf("wA:p%d", 100+u.ref)
				mud.Pane = &pane
				panes[pane] = central.PaneHerdr{Status: u.agente, Agente: "claude", Nome: "sintetico-" + u.projeto, WorkspaceID: "wA"}
			}
			if u.status != "wait" {
				_, _, err := st.AtualizarStatus(ctx, &co, r, mud)
				ok(err)
			}
			ok(st.GravarCache(ctx, central.FonteTaiga, r.Chave(), central.ValorTaiga{Titulo: u.titulo, Status: u.taiga, URL: fmt.Sprintf("https://taiga.exemplo.invalid/project/%s/us/%d", u.projeto, u.ref)}, nil))
			for _, m := range u.mrs {
				url := fmt.Sprintf("https://github.com/Exemplo/%s/pull/%d", u.projeto, m.n)
				_, _, err := st.AdicionarMR(ctx, &co, r, url)
				ok(err)
				if m.semInfo {
					continue
				}
				v := central.ValorMR{Estado: m.estado, Pipeline: m.pipeline, Aprovacao: m.aprovacao}
				ok(st.GravarMR(ctx, url, v, nil))
				if m.extraFalhou {
					ok(st.GravarMR(ctx, url, central.ValorMR{Estado: m.estado, SituacaoErro: &central.Erro{Codigo: "falha_comando", Mensagem: "rede"}}, nil))
				}
			}
			for i, tipo := range u.pends {
				p := central.NovaPendencia{US: r, Tipo: tipo, Titulo: fmt.Sprintf("Pergunta %d sobre %s", i+1, u.titulo), Autor: "sintetico", Detalhe: "## Contexto\nTexto sintético.\n", Slug: fmt.Sprintf("s%d-%d", u.ref, i), Hash: fmt.Sprintf("h%d-%d", u.ref, i)}
				if tipo == "decisao" {
					p.Opcoes = []central.Opcao{{ID: 1, Titulo: "A", Descricao: "primeira"}, {ID: 2, Titulo: "B", Descricao: "segunda", Recomendada: true}}
				}
				_, err := st.RegistrarPendencia(ctx, p)
				ok(err)
				if i < u.respond {
					resp := central.Resposta{Texto: "ok", Por: "ana"}
					if tipo == "decisao" {
						resp.Opcao = new(2)
					}
					_, err := st.ResponderPendencia(ctx, nil, refs.Pend{US: r, Numero: i + 1}, resp)
					ok(err)
				}
			}
		}
	}
	ok(st.GravarCache(ctx, central.FonteHerdr, "", central.SessaoHerdr{Panes: panes, Workspaces: map[string]central.WorkspaceHerdr{}}, nil))
}

func refsUS(projeto string, ref int) refs.US { return refs.US{Projeto: projeto, Ref: ref} }
