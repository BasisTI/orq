package web

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"html/template"

	"github.com/BasisTI/orq/internal/central"
)

// Estes testes alimentam a vista com Snapshot montado à mão: sem banco e sem httptest (spec §2.1).

var agoraVista = time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)

func sp(s string) *string { return &s }

func mrV(estado, pipeline string) central.MR {
	m := central.MR{URL: "https://github.com/Exemplo/r/pull/1", ProjetoPath: "Exemplo/r", IID: 1, Estado: sp(estado), Pipeline: sp(pipeline)}
	return m
}

func usV(projeto string, ref int, status string, mrs []central.MR, pends ...central.Pendencia) central.US {
	return central.US{ID: "TG-" + string(rune('0'+ref%10)), Chave: projeto + "/" + string(rune('0'+ref%10)), Projeto: projeto, Ref: ref, Status: status, MRs: mrs, Pendencias: pends}
}

func ondaV(id int64, coord, ate string, us ...central.US) central.Onda {
	return central.Onda{ID: id, Coordenador: coord, Titulo: "onda", AutorizacaoAte: ate, Ordem: "livre", US: us}
}

func pendV(estado string, aberta bool, onda int64) central.Pendencia {
	return central.Pendencia{ID: "TG-1#P1", US: "p/1", Numero: 1, Tipo: "duvida", Estado: estado, Aberta: aberta, OndaID: onda}
}

func snapV(pend []central.Pendencia, ondas ...central.Onda) central.Snapshot {
	return central.Snapshot{
		Coordenadores:     []central.CoordSnapshot{{Coordenador: central.Coordenador{Nome: "c"}, Ondas: ondas}},
		PendenciasAbertas: pend,
		Fontes:            map[string]central.Fonte{},
	}
}

func idsDasOndas(c Central) []int64 {
	var ids []int64
	for _, o := range c.Ondas {
		ids = append(ids, o.ID)
	}
	return ids
}

func TestVistaOrdemPorAtencao(t *testing.T) {
	snap := snapV([]central.Pendencia{pendV("aberta", true, 4)},
		ondaV(1, "c", "merge", usV("p", 1, "exec", nil)),
		ondaV(2, "c", "merge", usV("p", 2, "exec", []central.MR{mrV("pronta", "sucesso")})),
		ondaV(3, "c", "merge", usV("p", 3, "exec", []central.MR{mrV("aberta", "falhou")})),
		ondaV(4, "c", "merge", usV("p", 4, "exec", nil)),
		ondaV(5, "c", "merge", usV("p", 5, "exec", nil)),
	)
	got := idsDasOndas(vistaCentral(snap, agoraVista, Filtro{}))
	if want := []int64{4, 3, 2, 1, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("ordem = %v, quer %v (pendência, pipeline falhou, MR pronta, o resto, por id)", got, want)
	}
}

func TestVistaSelosDeMRDependemDoLimiteDaOnda(t *testing.T) {
	pronta := []central.MR{mrV("pronta", "sucesso")}
	em := func(ate string) OndaVista {
		return vistaCentral(snapV(nil, ondaV(1, "c", ate, usV("p", 1, "exec", pronta))), agoraVista, Filtro{}).Ondas[0]
	}
	if o := em("merge"); o.US[0].MRs[0].Estado.Texto != "espera merge" || o.Sinais[0].Texto != "1 MR espera merge" {
		t.Errorf("onda até merge: %+v %+v", o.US[0].MRs[0].Estado, o.Sinais)
	}
	for _, ate := range []string{"mr-pronta", "plano"} {
		if o := em(ate); o.US[0].MRs[0].Estado.Texto != "pronta" || o.Sinais[0].Texto != "1 MR pronta" {
			t.Errorf("onda até %s: %+v %+v", ate, o.US[0].MRs[0].Estado, o.Sinais)
		}
	}
	// MR mesclada ou fechada não dá sinal.
	m := []central.MR{mrV("mesclada", "falhou")}
	if o := vistaCentral(snapV(nil, ondaV(1, "c", "merge", usV("p", 1, "exec", m))), agoraVista, Filtro{}).Ondas[0]; len(o.Sinais) != 0 {
		t.Errorf("sinais para MR mesclada: %+v", o.Sinais)
	}
}

func TestVistaEsperamVoceNaoContaARespondidaMasElaSegura(t *testing.T) {
	respondida := pendV("respondida", true, 1)
	us := usV("p", 1, "done", nil, respondida)
	snap := snapV([]central.Pendencia{respondida}, ondaV(1, "c", "plano", us))
	o := vistaCentral(snap, agoraVista, Filtro{}).Ondas[0]
	if o.Esperam != 0 || o.US[0].Esperam != 0 {
		t.Errorf("a respondida contou como espera: onda %d, US %d", o.Esperam, o.US[0].Esperam)
	}
	if o.Concluida {
		t.Error("onda com pendência respondida (ainda aberta) ficou concluída")
	}
	// Resolvida: sai de tudo e a onda conclui.
	us.Pendencias = []central.Pendencia{pendV("resolvida", false, 1)}
	o = vistaCentral(snapV(nil, ondaV(1, "c", "plano", us)), agoraVista, Filtro{}).Ondas[0]
	if !o.Concluida {
		t.Error("onda sem pendência aberta nem respondida deveria estar concluída")
	}
	// Aberta: conta.
	aberta := pendV("aberta", true, 1)
	us.Pendencias = []central.Pendencia{aberta}
	c := vistaCentral(snapV([]central.Pendencia{aberta}, ondaV(1, "c", "plano", us)), agoraVista, Filtro{})
	if c.Ondas[0].Esperam != 1 || c.Ondas[0].US[0].Esperam != 1 || c.Abas[len(c.Abas)-1].N != 1 || !c.Abas[len(c.Abas)-1].Aviso {
		t.Errorf("aberta não contou em todo lugar: %+v", c)
	}
}

func TestVistaOndaComUSDeDoisProjetosApareceNasDuasAbas(t *testing.T) {
	snap := snapV(nil,
		ondaV(1, "c", "merge", usV("a", 1, "exec", nil), usV("b", 2, "exec", nil)),
		ondaV(2, "c", "merge", usV("a", 3, "exec", nil)),
	)
	for proj, want := range map[string][]int64{"a": {1, 2}, "b": {1}} {
		c := vistaCentral(snap, agoraVista, Filtro{Projeto: proj})
		if got := idsDasOndas(c); !reflect.DeepEqual(got, want) {
			t.Errorf("aba %s: ondas %v, quer %v", proj, got, want)
		}
		if len(c.Ondas[0].US) != 2 && proj == "a" {
			t.Errorf("a onda perdeu US ao filtrar: %d", len(c.Ondas[0].US))
		}
	}
	todos := vistaCentral(snap, agoraVista, Filtro{})
	var contagens []string
	for _, a := range todos.Abas {
		contagens = append(contagens, a.Rotulo+":"+string(rune('0'+a.N)))
	}
	if got := strings.Join(contagens, " "); got != "Todos:2 a:2 b:1 Pendências:0" {
		t.Errorf("abas = %s", got)
	}
	if c := vistaCentral(snap, agoraVista, Filtro{Projeto: "zzz"}); !c.ProjetoSemOnda {
		t.Error("projeto sem onda deveria marcar ProjetoSemOnda")
	}
}

func TestVistaProjetoPadraoDiferenteDoProjetoDasUS(t *testing.T) {
	// A aba é a do projeto das US, não a do projeto padrão do coordenador.
	snap := snapV(nil, ondaV(1, "c", "merge", usV("nuvem", 1, "exec", nil)))
	snap.Coordenadores[0].ProjetoPadrao = sp("catalogo")
	c := vistaCentral(snap, agoraVista, Filtro{})
	if len(c.Abas) != 3 || c.Abas[1].Rotulo != "nuvem" {
		t.Errorf("abas = %+v", c.Abas)
	}
}

func TestVistaTituloLongoETextoHostilNaoViramHTML(t *testing.T) {
	longo := strings.Repeat("título ", 40)[:200]
	o := ondaV(1, "c", "merge", usV("p", 1, "exec", nil))
	o.Titulo = longo + "<script>alert(1)</script>"
	c := vistaCentral(snapV(nil, o), agoraVista, Filtro{})
	if c.Ondas[0].Titulo != o.Titulo {
		t.Error("a vista não deve cortar nem alterar o título (o corte é do CSS)")
	}
	// Nenhum campo da vista é template.HTML: o escape é sempre do html/template.
	var ve func(reflect.Type) bool
	ve = func(ty reflect.Type) bool {
		switch ty.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			return ve(ty.Elem())
		case reflect.Struct:
			if ty == reflect.TypeOf(template.HTML("")) {
				return true
			}
			for i := 0; i < ty.NumField(); i++ {
				if ve(ty.Field(i).Type) {
					return true
				}
			}
		case reflect.String:
			return ty == reflect.TypeOf(template.HTML(""))
		}
		return false
	}
	if ve(reflect.TypeOf(Central{})) {
		t.Error("a vista tem campo template.HTML")
	}
}

func TestVistaHashDaListaMudaComComposicao(t *testing.T) {
	a := snapV(nil, ondaV(1, "c", "merge", usV("p", 1, "exec", nil)))
	b := snapV(nil, ondaV(1, "c", "merge", usV("p", 1, "exec", nil)), ondaV(2, "c", "merge", usV("p", 2, "exec", nil)))
	ha, ha2, hb := vistaCentral(a, agoraVista, Filtro{}).Hash, vistaCentral(a, agoraVista, Filtro{}).Hash, vistaCentral(b, agoraVista, Filtro{}).Hash
	if ha != ha2 || ha == hb {
		t.Errorf("hash: %s %s %s", ha, ha2, hb)
	}
	// Mudar só um estado (sem mudar ordem nem ids) não muda o hash.
	c := snapV(nil, ondaV(1, "c", "merge", usV("p", 1, "done", nil)))
	if vistaCentral(c, agoraVista, Filtro{}).Hash != ha {
		t.Error("o hash mudou sem a composição mudar")
	}
}

func TestVistaVencidoEFontes(t *testing.T) {
	velho := agoraVista.Add(-time.Hour).Format(time.RFC3339)
	snap := snapV(nil, ondaV(1, "c", "merge", usV("p", 1, "exec", nil)))
	snap.Fontes = map[string]central.Fonte{"herdr": {AtualizadoEm: &velho}, "mr": {}, "taiga": {AtualizadoEm: sp(agoraVista.Format(time.RFC3339))}}
	c := vistaCentral(snap, agoraVista, Filtro{})
	if c.Situacao.Vencidas != "herdr (1 h)" {
		t.Errorf("vencidas = %q", c.Situacao.Vencidas)
	}
	if !c.Fontes[0].Vencido || !c.Fontes[1].Nunca || c.Fontes[2].Vencido {
		t.Errorf("fontes = %+v", c.Fontes)
	}
}
