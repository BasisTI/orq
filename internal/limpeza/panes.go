package limpeza

import (
	"context"
	"fmt"
	"regexp"
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
	// O board manda: US reaberta ou ainda aberta no Taiga não perde pane. Sem resposta do
	// Taiga, preserva.
	motivoBoard := l.conferirBoard(ctx, u)
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
		outra := l.outraUS(ctx, u, id, nome)
		switch {
		case outra != "":
			it.Estado, it.Motivo = Preservado, outra
		case motivoBoard != "":
			it.Estado, it.Motivo = Preservado, motivoBoard
		case coords[id]:
			it.Estado, it.Motivo = Preservado, "é o pane de um coordenador"
		case id == l.PaneProprio:
			it.Estado, it.Motivo = Preservado, "é o pane que roda este comando"
		case p.Status != "idle" && p.Status != "done":
			it.Estado, it.Motivo = Preservado, "agente em "+p.Status
		default:
			it.Estado = Remover
			it.exec = func(ctx context.Context) error { return l.fecharPane(ctx, u, id, nome) }
		}
		itens = append(itens, it)
	}
	return itens
}

// listarPanesSoltos devolve, só para mostrar, os panes com cara de ser da US (tg-<ref>_* ou
// <projeto>-tg-<ref>_*) que o recursos.json não registra. O nome não prova a posse (o Herdr é
// compartilhado entre boards e o mesmo ref repete: tg-306_revisor de um board contra a US 306 de
// outro), então eles são Preservado e nunca dependência de nada: não seguram worktree, branches,
// TMPDIR, Docker nem a pasta de trabalho, e nunca são fechados.
func (l *Limpador) listarPanesSoltos(ctx context.Context, u refs.US, rec *Recursos) []*Item {
	sessao, err := l.F.ConsultarHerdr(ctx)
	if err != nil {
		return nil
	}
	registrados := map[string]bool{}
	for _, n := range rec.Panes {
		registrados[strings.ToLower(n)] = true
	}
	var ids []string
	for id, p := range sessao.Panes {
		if p.Nome != "" && !registrados[strings.ToLower(p.Nome)] && nomeDePaneDaUS(p.Nome, u) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var itens []*Item
	for _, id := range ids {
		itens = append(itens, &Item{Tipo: "pane_nao_registrado", Alvo: fmt.Sprintf("%s (%s)", id, sessao.Panes[id].Nome),
			Estado: Preservado, Motivo: "não registrado no recursos.json, preservado: o nome parece da US, mas pode ser de outro board (registre com orq recursos gravar --pane)"})
	}
	return itens
}

// nomeDePaneDaUS diz se o nome segue o padrão dos panes da US: tg-<ref>_* ou <projeto>-tg-<ref>_*.
func nomeDePaneDaUS(nome string, u refs.US) bool {
	n := strings.ToLower(nome)
	base := "tg-" + strconv.Itoa(u.Ref) + "_"
	return strings.HasPrefix(n, base) || strings.HasPrefix(n, strings.ToLower(u.Projeto)+"-"+base)
}

// conferirBoard devolve o motivo de preservar quando o Taiga não mostra a US concluída: aberta
// (reaberta para correção) ou sem resposta. Vazio quer dizer concluída.
func (l *Limpador) conferirBoard(ctx context.Context, u refs.US) string {
	v, err := l.F.ConsultarTaiga(ctx, u.Projeto, u.Ref)
	if err != nil {
		return "não consegui conferir a US no Taiga, preservado: " + err.Error()
	}
	if !v.Fechada {
		return fmt.Sprintf("a US não está concluída no Taiga (status %q): pode ter sido reaberta", v.Status)
	}
	return ""
}

// outraUS devolve o motivo de preservar quando o pane parece hospedar outra US: o nome é de
// outra US (tg-<ref>_*) ou o Central registra o pane em outra US. Vazio quando nada indica isso.
func (l *Limpador) outraUS(ctx context.Context, u refs.US, id, nome string) string {
	if m := nomeOutraUS.FindStringSubmatch(strings.ToLower(nome)); m != nil {
		if ref, _ := strconv.Atoi(m[2]); ref != u.Ref || (m[1] != "" && m[1] != strings.ToLower(u.Projeto)) {
			return fmt.Sprintf("o nome %q é de outra US, preservado", nome)
		}
	}
	snap, err := l.St.Snapshot(ctx, false)
	if err != nil {
		return "não consegui conferir de quem é o pane, preservado: " + err.Error()
	}
	for _, c := range snap.Coordenadores {
		for _, o := range c.Ondas {
			for _, x := range o.US {
				if x.PaneID != nil && *x.PaneID == id && (x.Projeto != u.Projeto || x.Ref != u.Ref) {
					return "o Central registra este pane na US " + x.Chave + ", preservado"
				}
			}
		}
	}
	return ""
}

var nomeOutraUS = regexp.MustCompile(`^(?:([a-z0-9._-]+)-)?tg-(\d+)_`)

// fecharPane relê o Herdr logo antes de fechar: o agente pode ter voltado a trabalhar.
func (l *Limpador) fecharPane(ctx context.Context, u refs.US, id, nome string) error {
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
	if motivo := l.outraUS(ctx, u, id, nome); motivo != "" {
		return preservar(motivo)
	}
	if motivo := l.conferirBoard(ctx, u); motivo != "" {
		return preservar(motivo)
	}
	_, err = l.F.Rodar(ctx, "herdr", "pane", "close", id)
	return err
}
