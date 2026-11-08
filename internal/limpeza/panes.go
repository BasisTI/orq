package limpeza

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/refs"
)

// planejarPanes acha os panes da US pelo nome de agente listado no recursos.json, e só eles.
// Só fecha o pane cujo agente está idle ou done.
func (l *Limpador) planejarPanes(ctx context.Context, u refs.US, rec *Recursos) []*Item {
	sessao, err := l.F.ConsultarHerdr(ctx)
	if err != nil {
		return []*Item{{Tipo: "herdr", Alvo: "panes", Estado: Preservado, Motivo: "Herdr indisponível, não consegui conferir os panes: " + err.Error()}}
	}
	coords := map[string]bool{}
	cs, err := l.St.ListarCoords(ctx)
	if err != nil {
		return []*Item{{Tipo: "herdr", Alvo: "panes", Estado: Preservado, Motivo: "não consegui ler os coordenadores: " + err.Error()}}
	}
	for _, c := range cs {
		if c.PaneID != nil {
			coords[*c.PaneID] = true
		}
	}
	// Só os panes registrados em recursos.json: um nome parecido (tg-<ref>_*) não prova que o
	// pane é desta US, porque o Herdr é compartilhado com outros boards e coordenadores.
	exatos := map[string]bool{}
	for _, n := range rec.Panes {
		exatos[strings.ToLower(n)] = true
	}
	casa := func(nome string) bool { return nome != "" && exatos[strings.ToLower(nome)] }
	var ids []string
	for id, p := range sessao.Panes {
		if casa(p.Nome) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var itens []*Item
	for _, id := range ids {
		p := sessao.Panes[id]
		it := &Item{Tipo: "pane", Alvo: fmt.Sprintf("%s (%s)", id, p.Nome)}
		nome := p.Nome
		switch {
		case coords[id]:
			it.Estado, it.Motivo = Preservado, "é o pane de um coordenador"
		case id == l.PaneProprio:
			it.Estado, it.Motivo = Preservado, "é o pane que roda este comando"
		case p.Status != "idle" && p.Status != "done":
			it.Estado, it.Motivo = Preservado, "agente em "+p.Status
		default:
			it.Estado = Remover
			it.exec = func(ctx context.Context) error { return l.fecharPane(ctx, id, nome) }
		}
		itens = append(itens, it)
	}
	// Panes que parecem da US (tg-<ref>_*) mas não estão no recursos.json: só aparecem, nunca
	// são fechados. Também seguram a pasta de trabalho, que guarda o registro.
	var soltos []string
	for id, p := range sessao.Panes {
		if !casa(p.Nome) && !coords[id] && nomeDePaneDaUS(p.Nome, u) {
			soltos = append(soltos, id)
		}
	}
	sort.Strings(soltos)
	for _, id := range soltos {
		itens = append(itens, &Item{Tipo: "pane_nao_registrado", Alvo: fmt.Sprintf("%s (%s)", id, sessao.Panes[id].Nome),
			Estado: Preservado, Motivo: "não registrado no recursos.json, preservado (registre com orq recursos gravar --pane)"})
	}
	return itens
}

// nomeDePaneDaUS diz se o nome segue o padrão dos panes da US: tg-<ref>_* ou <projeto>-tg-<ref>_*.
func nomeDePaneDaUS(nome string, u refs.US) bool {
	n := strings.ToLower(nome)
	base := "tg-" + strconv.Itoa(u.Ref) + "_"
	return strings.HasPrefix(n, base) || strings.HasPrefix(n, strings.ToLower(u.Projeto)+"-"+base)
}

// fecharPane relê o Herdr logo antes de fechar: o agente pode ter voltado a trabalhar.
func (l *Limpador) fecharPane(ctx context.Context, id, nome string) error {
	sessao, err := l.F.ConsultarHerdr(ctx)
	if err != nil {
		return preservar("Herdr indisponível na hora de fechar: " + err.Error())
	}
	p, ok := sessao.Panes[id]
	if !ok {
		return nil
	}
	if p.Nome != nome {
		return preservar(fmt.Sprintf("o pane %s agora se chama %q", id, p.Nome))
	}
	if p.Status != "idle" && p.Status != "done" {
		return preservar("agente passou a " + p.Status)
	}
	_, err = l.F.Rodar(ctx, "herdr", "pane", "close", id)
	return err
}
