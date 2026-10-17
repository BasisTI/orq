package limpeza

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/BasisTI/orq/internal/refs"
)

// planejarPanes acha os panes da US pelo nome do agente: os listados no recursos.json e os que
// seguem `tg-<ref>_*` ou `<projeto>-tg-<ref>_*`. O nome sem projeto só vale se nenhum outro
// projeto da Central tem uma US com esse número. Só fecha o pane cujo agente está idle ou done.
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
	semProjeto := false
	if ps, err := l.St.ProjetosDaRef(ctx, u.Ref); err == nil && len(ps) == 1 {
		semProjeto = true
	}
	tg := strings.ToLower(refs.Exibir(u.Ref))
	prefixos := []string{u.Projeto + "-" + tg + "_"}
	if semProjeto {
		prefixos = append(prefixos, tg+"_")
	}
	exatos := map[string]bool{}
	for _, n := range rec.Panes {
		exatos[strings.ToLower(n)] = true
	}
	casa := func(nome string) bool {
		nome = strings.ToLower(nome)
		if nome == "" {
			return false
		}
		if exatos[nome] {
			return true
		}
		for _, p := range prefixos {
			if strings.HasPrefix(nome, p) {
				return true
			}
		}
		return false
	}
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
	return itens
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
